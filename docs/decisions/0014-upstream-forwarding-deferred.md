---
number: 0014
title: Upstream forwarding (federation) deferred — prep-work captured, speculative work foreclosed
status: Accepted
date: 2026-05-02
---

# 0014 — Upstream forwarding deferred; prep-work captured

## Context

While settling ADR 0013 (daemon owns the bridge as an internal subsystem), a related but distinct topology emerged in the operator's thinking: a future shape where multiple local daemons each log QSOs on their own machines and forward those QSOs to an upstream "master" daemon, which is responsible for forwarding to online services (QRZ, ClubLog, LoTW), de-duplication across local daemons, and aggregating the canonical log. The local daemons keep enrichment local; the master handles the once-per-station online-service ceremony.

The use cases that motivate this shape are real but currently dormant:

- **Multi-operator stations** — Field Day, contest setups, club operations where multiple operators log simultaneously on separate machines and the club wants one canonical log.
- **The operator's own multi-machine reality eventually** — main shack PC daemon plus a portable laptop daemon used during portable operating, both forwarding to a home-server master.
- **Backup / continuity** — a master daemon as the durable archive while each local daemon comes and goes with the operating session.

None of these has a current driver. The operator runs a single station with a single daemon today.

The forcing question for this ADR: how do we keep the codebase open to this future without falling into the design-by-anticipation trap that `lessons-for-v2.md` warns against ("Build specific, not generic — three specific implementations are easier to read, debug, and evolve than one clever abstraction")?

The answer this ADR settles: **build only the prep-work items that are justified by today's scope on their own merits**, and explicitly foreclose on the rest until a real driver appears.

## Decision

**Upstream forwarding is deferred.** No upstream-forwarder driver, no federation protocol, no master/worker discovery, no cluster config, no multi-daemon UI is built in v1.

**Four prep-work items are accepted as standing requirements** because each is justified by v1 scope independently of any future federation. If/when upstream forwarding becomes a real driver, adding it should be a one-driver change, not a refactor.

The four prep items:

### 1. Driver-shaped forwarder layer

The daemon's outbound forwarder layer is built driver-based from day one. Each existing destination (QRZ, ClubLog, LoTW) is a driver implementing a forwarder interface (submit, retry semantics, credentials, error classification). The forwarder worker dispatches per-driver from the upload-queue rows.

**Justified today by:** v1 already has three forwarder destinations with different APIs, different retry behaviour, different credential models, and different idempotency keys. Building this driver-shaped is the right shape for what exists today, not a future-anticipation shape. v1's monolithic forwarder code is exactly the v1-mistake `lessons-for-v2.md` warns against generalizing prematurely — but in this case the generalization (a `Forwarder` interface with three concrete implementations) is *less* abstract than the v1 code, not more.

**Cluster-readiness payoff:** a future `smd-upstream` driver is one more `Forwarder` implementation. The destination URL is operator-configured; the credentials are an operator token; the wire format is the daemon's own QSO submission shape. No new layer required.

### 2. Auth header threaded through every daemon HTTP request from day one

Every request the SPA makes to the daemon includes an `Authorization: Bearer <token>` header if `configState` has a token configured; the daemon's HTTP middleware looks for the header, validates if a check function is registered, and rejects if invalid. In the default deployment the check is a no-op (LAN-only, single operator, no token configured) — so the header is sent and ignored.

**Justified today by:** the moment the operator runs the daemon on a Pi or VPS reachable from anywhere outside the LAN, a token is required. ADR 0010 / `topology.md` already name "static token in config" as the authentication story for the network-deployed-daemon case. Building the header-threading from day one means turning auth on later is a config change (set the token, register the validator), not a refactor of every fetch call in the SPA.

**Cluster-readiness payoff:** local daemon → master daemon traffic uses the same header. The master daemon's check function validates the local daemon's operator-supplied token. No new auth shape; one more validator hook.

### 3. Subsystem disable-flag (the bridge case, generalized)

ADR 0013 introduces `bridge.enabled` as a daemon config flag. The principle generalizes: any subsystem that holds a host-bound resource or an operator-specific role (bridge for serial, future enrichment-cache for online lookups, future SPA-host for embedded UI) gets an `enabled` flag in its config section.

**Justified today by:** ADR 0013 already requires `bridge.enabled` for the network-deployed daemon case. ADR 0001 already has a `cfg.Server.ServeSPA` flag for headless deployments. The pattern exists today and is being used for two real cases. Naming it as a pattern (rather than per-subsystem ad-hoc flags) means future subsystems follow the same shape.

**Cluster-readiness payoff:** an upstream-forwarder-only daemon is just a daemon with `bridge.enabled: false`, possibly `serveSPA: false`, plus an upstream-forwarder driver enabled. No new daemon mode, no new binary, no new deployment shape — just a config combination.

### 4. `additional_data` provenance metadata

When a QSO arrives at a daemon, the daemon may stash provenance metadata in the QSO's `additional_data` JSON blob — fields like `received_from` (which client/daemon submitted it), `received_at`, `forwarded_to` (which destinations have already accepted it), `originated_by` (the operator station identity from the originating daemon). None of these fields are real columns in the schema; all live in the blob, consistent with the v1 invariant that `additional_data` absorbs spec evolution and out-of-spec metadata.

**Justified today by:** v1 already uses `additional_data` for ADIF spec overflow. Adding provenance fields uses the same mechanism. There is also v1-shaped value today (auditing "did this QSO come from WSJT-X UDP, the SPA, or a bulk ADIF import?" is a real debugging concern even on a single daemon).

**Cluster-readiness payoff:** when a master daemon receives a QSO from a local daemon, it can read `forwarded_to` to know whether the local already pushed to QRZ/ClubLog. It can read `originated_by` to know which station this QSO is from when consolidating multi-operator logs. It can stash its own `received_from` to mark "this came from a local, not a direct submit." All without schema migration.

## What is NOT built (foreclosed)

Each of the following has zero drivers today and is explicitly out of scope until a real driver appears. **Do not build any of these speculatively.**

- **Master discovery / mDNS / Bonjour / zeroconf.** When upstream forwarding becomes real, the operator types in the master's URL (same shape as typing in QRZ's API URL — it's just a forwarder destination). Network discovery is overkill for personal use.
- **Cluster config schema.** No `cluster.master`, `cluster.peers`, `cluster.role` fields. The relevant config is the upstream-forwarder driver's own destination URL, which goes under `forwarders` like every other forwarder driver.
- **Federation routing / dedup-across-daemons protocol.** No "which daemon owns which QSO" arbitration, no cross-daemon Lamport clocks, no eventual-consistency machinery. The dedup story for the future is the same as the existing v1 dedup story (hash on `(call, band, mode, time)` per the `invariants.md` "Contest dupe check and general ingest dedupe" entry) applied at the master.
- **Multi-daemon UI in the SPA.** No "select daemon" picker, no per-daemon connection status, no aggregated cross-daemon QSO views. The SPA talks to one daemon at a time. If the operator wants to view the master's aggregated log, the operator points the SPA at the master's URL.
- **Master-daemon-specific code paths.** The master is *just a daemon* with `bridge.enabled: false` and an upstream-forwarder destination configured to forward outbound to QRZ/ClubLog/LoTW. It does not need its own binary, its own role flag, or its own startup mode. The shape is "daemons forward to other daemons" not "masters and workers."
- **Conflict resolution for double-logged QSOs from two locals.** The dedup hash absorbs duplicates silently, same as today. If two operators at the same station log the same callsign in the same minute, the master accepts the first and discards the second; resolving "actually we had two QSOs and the timestamps are wrong" is a manual edit, not a protocol concern.

If a real driver appears for any of these, write a new ADR proposing the specific shape. **Do not pre-emptively scaffold any of them.**

## Alternatives considered

### Build cluster infrastructure now

Design the federation protocol now, build the master/worker startup modes now, ship a multi-daemon SPA now. "If we're going to need it, why wait?"

Rejected: this is exactly the design-by-anticipation pattern `lessons-for-v2.md` calls out. v1's `internal/adapters/` (30+ test files of reflection-based adapter framework, abandoned) is the cautionary tale. The cost of building speculative infrastructure isn't just the code that gets built — it's the design constraints that infrastructure imposes on every subsequent feature, plus the maintenance burden when the assumed-future doesn't materialize the way the speculation predicted. The operator doesn't have a multi-station today; speculating about its shape will get the shape wrong.

### Build nothing, refactor when needed

Don't accept the four prep items. If upstream forwarding ever becomes real, refactor the forwarder layer, add auth, generalize the disable flag, and retrofit provenance at that point.

Rejected: each of the four prep items is justified by v1 scope independently. The forwarder layer needs to be driver-shaped because it has three drivers today, not because of cluster mode. Auth needs to be threaded because a network-deployed daemon needs auth, not because of cluster mode. The disable flag is needed for ADR 0013's headless case, not because of cluster mode. Provenance fits into `additional_data`, which already absorbs spec overflow. Skipping these prep items means building v1 code that will need rework for v1 reasons, not just for cluster reasons.

### Halfway-house: build the prep items but also write a draft federation protocol

Spec the federation protocol now, even if no code is built, so we have a target. "Architecture without code."

Rejected: a draft protocol with no implementation is a constraint with no validation. It will be wrong in ways that won't be discovered until implementation, and it will pre-bias the implementation toward the wrong shape. The four prep items are concrete (each is testable, each is exercised by today's scope); a federation protocol draft is not. Worse, a written-down protocol is sticky — future-us will feel the pull to implement what's documented, even if the implementation suggests a better shape. Better to have nothing written down than to have a wrong sketch that becomes load-bearing by accident.

## Consequences

**Signed up for:**

- **The forwarder layer must be driver-shaped from day one.** Concrete API surface: a `Forwarder` interface with `Submit(qso) (result, error)` and a registry that the worker reads from per-row. Three drivers exist (QRZ, ClubLog, LoTW); each is a small file. v1 monolithic forwarder code is rebuilt this way from scratch for v2; not a port.
- **Every SPA fetch call sends `Authorization: Bearer <token>` if a token is configured.** Default deployment has no token configured; header is absent. Daemon middleware checks for a token if a validator is registered; default deployment registers no validator. Adding auth later is config + validator registration; no fetch-call changes.
- **`enabled` flags become a recognized config pattern.** Bridge has one (ADR 0013). SPA-hosting has one (ADR 0001). Future subsystems follow the same shape — namespaced `<subsystem>.enabled: bool` with sensible default. Documented as a pattern in `topology.md` so it's not re-invented per-subsystem.
- **`additional_data` carries provenance fields.** When a QSO is submitted, the daemon may stash `received_from` etc. in the blob. These fields have no schema definition (they are free-form within the blob); their meaning is documented where they're written. Consumer code (future master daemon, current debugging) reads them when present, tolerates absence.

**Accepted costs:**

- **Marginal complexity from driver-shaping.** Three forwarder drivers + a registry + a worker is more code than three function calls. Worth it because v1 already has three destinations and the v1 monolithic shape is hard to test per-destination.
- **Marginal complexity from threaded auth.** Every fetch call has an extra header line. Daemon middleware has a no-op token check by default. Worth it because adding auth later otherwise touches every fetch site.
- **Provenance fields are undocumented in v1.** Until a master daemon exists that reads them, `received_from` etc. are written for debugging only. The cost is "blob fields with no consumer"; the benefit is "no schema migration when the consumer arrives."

**Gained:**

- **No speculative cluster code.** The codebase stays focused on what's actually used. Future-us reading this code in six months won't find half-built federation infrastructure that has to be removed or completed.
- **Adding upstream forwarding later is one driver, not a redesign.** Concrete: write `internal/forwarder/upstream/driver.go` implementing `Forwarder`, register it in the forwarder registry, document the operator-config shape. No new daemon mode, no new SPA, no protocol.
- **The four prep items are independently valuable.** Even if upstream forwarding never materializes, the codebase is better for having driver-shaped forwarders, threaded auth, namespaced enable-flags, and `additional_data` provenance. Each pays for itself in v1 scope.
- **Foreclosure is explicit.** Future contributors (future-us included) reading this ADR see "do not build X, Y, Z speculatively." This is anti-momentum protection: the next time someone says "wouldn't it be cool if we added a master daemon mode" the answer is "yes, write an ADR proposing the specific shape; do not start coding."

## Triggers to revisit

- **A specific multi-machine driver appears.** Examples: the operator buys a home server and wants the shack PC to forward there; the operator joins a club whose station runs N machines; the operator does a contest with friends and needs aggregated logging. When this happens, write an ADR proposing the upstream-forwarder driver shape (URL, auth, payload format, deduplication). The four prep items above mean that ADR can focus on the forwarder driver itself, not on a topology overhaul.
- **The "no master discovery" decision turns out wrong.** If operators (plural — i.e. the project gains more than one user) regularly stand up master daemons and find typing in URLs error-prone, mDNS or some discovery story becomes warranted. Today this is hypothetical.
- **Provenance fields accumulate without a consumer.** If `additional_data` keeps growing with provenance keys and nothing reads them, either prune the writes or accept they're write-only debugging info. Not a redesign trigger; a maintenance trigger.
- **Auth-header threading turns out to be the wrong shape.** E.g. if a real auth requirement needs OAuth flows, mTLS, or some other model that doesn't fit "static bearer token", the threading shape may need revision. Static token is what `topology.md` named for the network-deployed-daemon case; that decision predates this ADR and is not relitigated here.
- **Driver-shaped forwarders turn out to be over-built.** If only one forwarder destination ends up real (e.g. QRZ wins, ClubLog and LoTW get dropped), the driver layer is ceremony. Today there are three real destinations, so this is not a present concern.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — `cfg.Server.ServeSPA` flag is the precedent for the namespaced `enabled` pattern this ADR generalizes.
- ADR 0010 (`0010-rig-sse-wire-shape.md`) — names "static token via Authorization for remote-VPS deployment" as future work; this ADR pins down the threading-from-day-one shape that makes that future change small.
- ADR 0012 (`0012-daemon-and-bridge-separate-origins.md`) — superseded; preserved for the reasoning trail. Mentioned here because its rejected "in-process bridge" alternative argued network-deployability requires a separate process — that argument failed under ADR 0013, and the same disable-flag pattern is what makes upstream-forwarder deployments viable in this ADR.
- ADR 0013 (`0013-daemon-owns-bridge-as-subsystem.md`) — establishes `bridge.enabled` as the disable-flag pattern. This ADR generalizes that pattern as a subsystem convention and notes that `bridge.enabled: false` is exactly the shape a future master/upstream daemon takes.
- `docs/v1-analysis/lessons-for-v2.md` — "Build specific, not generic" lesson; the cautionary tale (`internal/adapters/`) for why speculative infrastructure is rejected as a default.
- `docs/v1-analysis/invariants.md` — "Contest dupe check and general ingest dedupe are two different things" entry; the future master's deduplication story uses the existing ingest-dedupe shape, not a new one.
- `docs/v2-design/topology.md` — single-binary default; this ADR documents `bridge.enabled: false` as the shape an upstream-forwarder daemon takes.
- `docs/v2-design/forwarding.md` and `docs/v2-design/forwarding-implementation.md` — daemon-side forwarder design; this ADR pins driver-shaping as a v1 requirement, not a future one.
- Memory `project_sm_serial_bridge` — bridge as subsystem in default deployment; this ADR adds the parallel "upstream forwarder is just another forwarder driver, deferred."
