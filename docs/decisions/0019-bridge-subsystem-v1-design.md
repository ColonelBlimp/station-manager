---
number: 0019
title: Bridge subsystem v1 — stateless pass-through, SPA-only frontend, single-asserter PTT
date: 2026-05-10
status: Accepted
---

# 0019 — Bridge subsystem v1 design

## Context

ADR 0013 settled the bridge as a daemon subsystem (`internal/bridge`)
in the default single-binary deployment. ADR 0010 settled the wire shape
between the bridge and the SPA (SSE on `/v1/rig/events`, three event
types, AUTO-mode CAT push-state, passive liveness via continuous data
flow). What neither resolved was the bridge's **internal design**: cache
or no cache, frontends to ship, transport choices, PTT model, multi-rig
posture.

A focused 2026-05-10 design conversation walked through the open
questions in order and reached decisions for each. This ADR consolidates
those decisions in one place so a future implementer (or a future
operator returning to revisit) has a single anchor for the v1 bridge
shape, rather than having to reconstruct intent from the trail of ADR
0010 / 0013 / `bridge.md` (parts of which are now superseded).

The design conversation surfaced three observations that shape the
decisions below:

1. **The logging SPA is the only first-class CAT consumer in v1.**
   Other in-house clients (a future CAT-control SPA, the FT8 stack,
   contest-mode logger) and third-party clients (WSJT-X, fldigi via
   rigctld) are speculative or deferred. Building infrastructure for
   them ahead of need is exactly the v1-analysis trap (`internal/adapters/`)
   the project committed to avoiding.
2. **Multi-rig is a known future shape, not a current shape.** The
   operator runs one rig today; SO2R / contest multi-rig is plausible
   later. The internal API should be rig-ID-aware so the v1 → multi-rig
   transition doesn't require a redesign, but the v1 implementation
   only needs to handle one rig.
3. **AUTO-mode CAT (push-state / transceive) is the assumed protocol
   shape.** The rig is a state broadcaster; the bridge is a filter.
   This is the load-bearing assumption that lets us drop the cache
   (next section) and get away with it.

## Decision

The v1 bridge is a **read-only stateless filter** between the rig and
the SPA: rig pushes state via AUTO-mode CAT → bridge filters and
forwards via SSE → SPA displays. One SSE frontend, no inbound command
path, no PTT awareness, multi-rig-aware internal API serving exactly
one rig today.

Concretely:

### Bridge is stateless w.r.t. rig state

- **No persistent rig-state cache.** The bridge does not store the rig's
  current state between events. It decodes data as it arrives from the
  rig, filters to SPA-relevant fields, emits an SSE event, and forgets.
- **No delta computation.** The bridge does not compare incoming pushes
  against previous state to figure out what changed. It emits whatever
  fields the current decode produced. The SPA's `catState` (Svelte 5
  `$state` proxy) merges the partial payload field-by-field; missing
  fields retain prior values naturally.
- **Snapshot-on-connect is active, not cached.** When the SPA opens an
  EventSource, the bridge fires a CAT poll command at the rig (Yaesu/
  Kenwood `IF;` for VFO+mode+status, Icom CI-V equivalent), waits for
  the response, decodes it, emits as the first SSE event. Then
  continues passive forwarding. No state held between connections.
- **Last-known values persistence is SPA-side.** ADR 0010 had the bridge
  cache "survive `rig-disconnected`" so the SPA could show last-known
  values. The SPA already does this — `catState` is a Svelte 5 `$state`
  proxy whose values persist until overwritten. `rig-disconnected`
  flips `bridgeState.rigResponding` to `false` (which marks values
  visually stale via the existing `editable` derivation, ADR 0006/0009)
  but `catState` itself is not cleared.

This is a revision of ADR 0010's "Bridge-side current-state cache"
section. The wire shape on `/v1/rig/events` is unchanged (same three
event types, same payload format, same merge-into-`catState` semantic);
the bridge's *internal* implementation no longer maintains a cache.

### One SSE frontend, others deferred

- **Ship `/v1/rig/events` SSE only.** The logging SPA's `bridge.svelte.ts`
  is the sole consumer in v1.
- **rigctld-compat TCP frontend deferred.** WSJT-X / fldigi own their
  own rig's port today (per `bridge.md` §1's analysis) — no third-party
  app currently needs the bridge's CAT mediation. Adding rigctld-compat
  in v1 is upstream-protocol-emulation work for a non-existent consumer.
- **NDJSON Unix-socket frontend deferred.** The SM-internal multiplexer
  transport described in `bridge.md` §3 was designed for a hypothetical
  CAT control app and FT8 stack. Both are real future work but not
  current. The v1 bridge package's public API should be designed so
  adding NDJSON-over-Unix-socket later is mechanical (a second
  registration step in the wiring layer); building it now is empty
  ceremony.

This contradicts `invariants.md` "Two-frontend bridge shape: rigctld-
compat TCP + SM-native event stream is canonical." That invariant was
correct as a *long-term shape*; v1 ships one frontend, the second
arrives when its driver does. The invariant is updated to reflect this
ordering in the same commit as this ADR landing.

### Read-only v1 — no PTT, no inbound command path

The v1 bridge is **read-only from the SPA's perspective.** Rig pushes
state via AUTO-mode CAT → bridge filters and forwards via SSE → SPA
displays. The SPA does not send commands to the rig in v1. The rig dial
remains the source of truth for frequency / mode / PTT changes; the
operator interacts with the rig directly, the SPA observes.

This means **everything PTT-related is deferred** to a later milestone:

- No PTT-tracking per SSE connection.
- No disconnect-safety-release on EventSource close.
- No PTT arbitration logic.
- No inbound command path (no `POST /v1/rig/cmd` or equivalent for
  set-freq / set-mode / PTT).

The rationale for this scope reduction (operator decision, 2026-05-10):
the SPA's existing UX already treats the rig as authoritative — when
CAT is connected, `displayedState` reads from `catState` (rig-pushed),
not from operator clicks. There's no current SPA flow that needs to
push state TO the rig. Adding inbound commands speculatively before a
real consumer exists is the same v1-analysis trap (`internal/adapters/`)
the project committed to avoiding.

When PTT becomes real (FT8 stack TX cycles, future voice keyer,
remote-station scenario), the bridge package's API gets an inbound
command method, the daemon adds an HTTP endpoint, the bridge tracks
per-connection PTT-asserted state, and disconnect-safety-release fires.
That's the full PTT story — built whole when its driver appears, not
piecemeal speculatively.

### Multi-rig API-aware, single-rig implementation

- **Internal API is rig-ID-aware from day one.** The bridge package's
  exported functions (`CurrentRigState`, `SendCommand`, event emit
  channel selectors, etc.) accept a rig identifier as their first
  argument, even though v1 only ever passes one value. This avoids
  the trap of hardcoding a single rig assumption into the API surface
  and having to refactor every call site when multi-rig lands.
- **HTTP route stays singular for v1.** `/v1/rig/events` (no `{id}`
  segment) since v1 only has one rig. When multi-rig ships, the route
  grows to `/v1/rig/{id}/events` and `/v1/rig/events` becomes a deprecated
  alias for the single-rig case (or is dropped if no users remain).
  ADR 0010 already flagged this evolution as a "trigger to revisit."
- **Configuration is one-rig-flat for v1.** `bridge.serial.port`,
  `bridge.serial.baud`, `bridge.cat.driver` (yaesu / kenwood / icom)
  at the top level of the bridge config block. Multi-rig configuration
  becomes a list of rigs; v1's flat config is the degenerate one-element
  case.

### Layering and package boundaries (unchanged from ADR 0013)

- **`internal/serial`** — port I/O, no protocol knowledge. Already exists.
- **`internal/cat`** — CAT codec + per-rig command DB. Already exists
  with Yaesu FT-710 and FTDX10 drivers.
- **`internal/bridge`** — new package. Owns serial-port acquisition,
  AUTO-mode CAT data flow, the `/v1/rig/events` SSE handler, the
  per-SSE-client PTT-asserted tracking, the SSE-open poll-bootstrap.
- **Package-import discipline.** `internal/storage`, `internal/forwarder`
  MUST NOT import `internal/bridge`, and vice versa. Shared imports are
  limited to `internal/types` + the HTTP server wiring layer that
  registers the SSE route. Same protection ADR 0013 named, restated
  for clarity.

### Performance is not a design risk

SSE over loopback TCP delivers per-event end-to-end latency under 1ms
(loopback TCP single-digit microseconds + HTTP framing trivial + browser
EventSource parser microseconds + Svelte `$state` merge sub-ms).
Realistic CAT event rates (10-30/sec during active dial use) are well
inside human-perception margins. The v1 lag concern recorded in
`bridge.md` §7b was Wails IPC (10-100× the TCP loopback hop) — not
applicable to the Svelte 5 + EventSource architecture.

The implementation MUST call `http.Flusher.Flush()` after each
SSE-event write so events don't queue in Go's response-writer buffer.
Standard SSE hygiene; the existing `/v1/events` (QSO-stored SSE)
handler already does this and is the pattern to copy.

## Alternatives considered

### Bridge maintains a current-state cache (per ADR 0010 original draft)

Bridge stores last-known rig state per field. Computes deltas against
the cache. Sends cached snapshot as the first event on SSE-open. Survives
`rig-disconnected` so when the rig comes back, the cache continues to
be updated by new pushes.

Rejected. Three observations made the cache empty ceremony:

1. **AUTO-mode rigs push deltas natively.** The rig already sends only
   what changed; the bridge doesn't need to compute deltas. "Just emit
   what arrived" produces the same wire output with less code.
2. **The SPA's `catState` already provides the persistence the cache
   was solving for.** Svelte 5 `$state` proxies retain values until
   overwritten. `rig-disconnected` doesn't clear `catState`; it just
   marks values visually stale via `bridgeState.rigResponding=false`.
3. **Snapshot-on-connect is solvable without cache.** Active poll on
   SSE-open (one CAT query, response forwarded as first event) gives
   the same SPA UX with no inter-connection state.

The cache was solving for a problem the SPA's reactive model already
handles. Removing it is a net simplification with no UX regression.

### Pure passive snapshot-on-connect (Option A from the design discussion)

Same stateless model, but no active poll on SSE-open. Bridge accepts
the connection and waits for the next rig push. SPA sees nothing until
operator wiggles the dial.

Rejected for SPA-reload UX. AUTO-mode rigs don't all push continuously
— some only push on change. A SPA tab opened on a rig parked at 14.250
USB for 10 minutes would show hardcoded defaults until the operator
touched the dial, even though the rig is fine. One CAT poll command at
SSE-open closes that gap with negligible cost.

### Two frontends in v1 (rigctld TCP + SSE)

`invariants.md`'s "two-frontend" canonical shape: ship rigctld-compat
TCP for WSJT-X / fldigi alongside the SPA's SSE.

Rejected for v1. WSJT-X and fldigi own their own rig's port directly in
the operator's current setup; no third-party app needs bridge mediation.
rigctld-compat is non-trivial (hamlib's protocol has quirks; a partial
implementation that's "almost rigctld" is worse than no rigctld). Build
when there's a real consumer.

### Full PTT arbitration in v1

Bridge tracks PTT requests across clients, enforces one-asserter-at-a-
time, rejects contested requests, possibly queues them.

Rejected. v1 has one client (the SPA), and per the read-only-v1 scope
reduction (above), the SPA doesn't even send commands to the rig in
v1. Arbitration logic that arbitrates against zero asserters is empty
ceremony. The design space for "what arbitration should do" only makes
sense once both an inbound command path exists AND multiple clients
contend for PTT — and at that point we'll know the actual contention
shape (does FT8 want exclusive PTT during a TX cycle? does a future
voice-keyer pause for FT8 TX?) rather than speculating.

### Single-asserter PTT convention + disconnect-safety-release in v1

Bridge has no PTT logic at the protocol level, but tracks per-SSE-
client PTT-asserted state and force-releases on close. Convention is
"only the SPA asserts PTT"; safety net catches stuck-key-on-crash.

Rejected. The v1 SPA doesn't have an inbound command path (read-only
v1, above), so there's nothing to track. The safety-release rule was
solving a real problem (stuck PTT on client crash → continuous TX →
fried rig finals), but that problem only exists once the SPA can
assert PTT in the first place. Until then, the rig stays in whatever
state the operator's dial puts it in; there's no "client asserted PTT
and crashed" failure mode because no client asserts PTT. Build PTT
tracking + safety release together with the inbound command path, when
both are real.

### Multi-rig single-rig-hardcoded API

API takes no rig ID; v1 hardcodes the assumption that there's one rig
and the bridge knows which one. Refactor the API when multi-rig lands.

Rejected. Refactoring an API used by every CAT call site (poll, send,
event subscription) is a non-trivial change; threading a rig ID through
every signature is the API's natural shape. Doing it now (with a
single-element implementation) costs nothing and avoids a future
refactor that would touch every consumer.

### Periodic active polling for liveness in v1

Bridge polls the rig at a defined interval (e.g. every 5 seconds) and
treats poll failure as a stronger liveness signal than ADR 0010's 30s
data-flow timeout. Closes the "wedged-but-streaming-waterfall" gap.

Deferred, not rejected. v1 ships passive liveness only (per ADR 0010).
Active polling is a real future improvement — closes the wedged-rig
detection gap and gives faster reconnect on power blips — but the
implementation has its own design questions (cadence trade-off: too
fast burns CAT bandwidth, too slow is slow detection; collision
serialization with operator-initiated CAT commands; per-rig
configurability). Worth its own design pass when we have observed
operating use to inform the cadence.

## Consequences

**Signed up for:**

- **`internal/bridge` package.** New package owning serial acquisition,
  AUTO-mode CAT data flow, SSE handler, per-SSE-client PTT tracking,
  SSE-open poll-bootstrap, disconnect-safety-release. Imports
  `internal/serial`, `internal/cat`, `internal/types`, `internal/logging`,
  `internal/iocdi`. Does NOT import `internal/storage`, `internal/forwarder`,
  `internal/qsoservice`.
- **Daemon config grows a `bridge` section.** `bridge.enabled` (already
  decided in ADR 0013, default `true`), `bridge.serial.port`,
  `bridge.serial.baud`, `bridge.cat.driver`, `bridge.serial.poll_command`
  (the bootstrap CAT query — defaulted per-driver, operator-overridable).
- **SPA's `bridge.svelte.ts` gets fleshed out.** Replaces the current
  stub with a real EventSource consumer subscribing to the three event
  types. Maintains `bridgeState.connected` and `bridgeState.rigResponding`
  per ADR 0010. Merges `rig-state` payloads into `catState` field-by-field.
  Surfaces `bridge-error` and `rig-disconnected` reasons via the toast
  system (ADR 0008).
- **HTTP route registration in `cmd/smd/main.go`.** Bridge's wiring
  layer registers `/v1/rig/events` on the daemon's mux when
  `bridge.enabled: true`. Same pattern the SPA / `/v1/events` SSE / etc.
  follow.
- **`internal/cat` may need new poll-command coverage.** The SSE-open
  bootstrap fires a query that returns VFO+mode+status in one shot
  (Yaesu/Kenwood `IF;`, Icom CI-V equivalent). If those aren't already
  in `internal/cat/rigs/*.json`, they get added.

**Accepted costs:**

- **No persistent state across daemon restart.** Operator restarts the
  daemon → bridge re-acquires the rig → SSE clients reconnect → bootstrap
  poll fires → SPA receives current state. No "remembered last position"
  across restarts. Acceptable: the rig itself remembers; the bridge is
  just a window onto it.
- **The first event on SSE-open is poll-bootstrap latency, not push
  latency.** Fresh SPA tab waits for the rig's response to the bootstrap
  CAT command (sub-100ms typical). One-time cost per SSE connection;
  steady-state pushes are <1ms each.
- **`bridge.md` §3 (the SM-internal Unix-socket multiplexer) is parked,
  not built.** Future CAT control app / FT8 stack will need it (or a
  similar in-process API). Their drivers determine the shape; building
  speculatively now would lock in the wrong shape.
- **`invariants.md`'s "Two-frontend bridge shape" is now ordering-
  qualified.** Updated in the same commit as this ADR landing: the
  invariant is the eventual shape, v1 ships one frontend.

**Gained:**

- **The bridge implementation is small.** Stateless filter +
  per-connection PTT tracking + bootstrap poll + SSE handler. No cache
  to maintain, no delta logic, no arbitration state machine, no
  multi-frontend dispatch. Days of work, not weeks.
- **The wire shape (ADR 0010) is unchanged.** SPA-side
  `bridge.svelte.ts` consumes exactly the events ADR 0010 specified;
  the bridge's internal simplification is invisible to the SPA.
- **Future-extensible without redesign.** Multi-rig is API-ready;
  rigctld TCP / NDJSON Unix socket / arbitration / active polling all
  slot in alongside without breaking the v1 surface.
- **Decision trail consolidated.** A future contributor reading this
  ADR understands the v1 shape, what was rejected and why, and what
  triggers reopening each rejection — without having to chase ADR 0010
  and 0013 separately or reconcile contradictions with `bridge.md`.

## Triggers to revisit

- **The SPA needs to drive the rig.** Any SPA flow that requires
  setting frequency, mode, or PTT from the UI (click-to-tune, voice
  keyer, future macro buttons) opens the inbound command path: bridge
  package gets command-input methods, daemon gets an HTTP endpoint
  (likely `POST /v1/rig/cmd` for symmetry with the rest of the API),
  bridge tracks per-connection PTT-asserted state, disconnect-safety-
  release fires on EventSource close. All built whole when this
  driver appears.
- **A second CAT client appears** (FT8 stack, CAT control app,
  contest-mode logger). Forces both the inbound command path (above)
  AND PTT arbitration design — by then we'll know real contention
  shape (does FT8 want exclusive PTT during a TX cycle? does the
  voice-keyer pause for FT8 TX?). Also forces an answer on the
  SM-internal transport shape (NDJSON Unix socket per `bridge.md` §3,
  or some other in-process API).
- **A third-party app needs bridge-mediated CAT.** WSJT-X moves to a
  shared rig with the logging app, fldigi appears, an operator wants
  N1MM-style multi-app contest support. rigctld-compat TCP becomes
  real; we ship the second frontend.
- **Multi-rig hardware lands** (SO2R, contest 2-rig setup). Internal
  API is already rig-ID-aware so the implementation grows; HTTP route
  becomes `/v1/rig/{id}/events`. Configuration shape grows from flat
  to list-of-rigs.
- **The wedged-but-streaming-waterfall scenario bites in real operating
  use.** Operator's rig is in TX-locked state with continuous data
  still streaming; the 30s passive timeout doesn't fire. Active
  polling becomes worth designing properly — cadence, collision
  serialization, per-rig configurability.
- **A non-AUTO-mode rig is encountered.** `bridge.md` §2's AUTO-mode
  assumption breaks; the bridge needs explicit polling for state
  changes (not just liveness). May overlap with the active-polling
  trigger above.
- **SPA-reload UX feels laggy.** If the bootstrap poll's <100ms latency
  proves operator-visible (e.g. dial-spin during a reload causes
  perceptible state catch-up), the cache reappears as a possible
  optimization — but only if measurement says so.
- **Daemon restart frequency makes "no remembered state" annoying.**
  If operators restart the daemon often enough that "rig state lost on
  restart" becomes a UX issue (vs the current "rig state lost only when
  the rig itself is power-cycled"), persisting bridge state to disk on
  shutdown becomes a possible improvement. Not foreseen for personal
  use.

## References

- ADR 0001 — daemon-hosts-SPA premise; SSE rides on the daemon's HTTP
  server in the default deployment.
- ADR 0006 — CAT-state precedence rule; the three flags
  (`configState.station.enabled`, `bridgeState.connected`,
  `bridgeState.rigResponding`) and the `editable` derivation that
  consume this ADR's wire output.
- ADR 0008 — toast notification system; surfaces `bridge-error` and
  `rig-disconnected` reasons.
- ADR 0009 — CAT-state decomposition; the four-object decomposition
  this ADR's data flow populates (`catState` from `rig-state` events;
  `bridgeState` from connection / event-sequence transitions).
- ADR 0010 — rig SSE wire shape. **Revised by this ADR**: the
  "Bridge-side current-state cache" section is removed (bridge is
  stateless); snapshot-on-connect is now active poll, not cached
  send. Wire shape (three event types, payload format,
  merge-into-`catState`) is unchanged.
- ADR 0012 — superseded by ADR 0013 (still preserved as the reasoning
  trail).
- ADR 0013 — daemon owns bridge as a subsystem. **This ADR sits on top
  of 0013**, not in tension with it: the topology is settled (daemon
  hosts the bridge subsystem, single binary, package boundaries
  enforced); this ADR settles the bridge's internal v1 shape.
- ADR 0017 — enrichment pipeline; not directly related, but its
  three-state read policy was the reference shape for "what does
  stale-but-cached look like" thinking, then explicitly rejected here
  (bridge is stateless, no stale-vs-fresh distinction needed because
  there's no cache).
- `docs/v2-design/bridge.md` — bridge architecture document. Several
  sections of this document are now superseded:
  - §3 (SM-internal Unix-socket multiplexer) — parked, not v1.
  - §4 (collapses-out list) — the rigctld-TCP / PTY drops were correct
    for v1; the "complex PTT arbitration not needed" entry stays
    accurate; the "command interleaving framing" entry stays accurate.
  - §6 (YAGNI question) — closed by ADR 0013; bridge IS built.
  - §7 (performance discussion) — still canonical.
  Treat the wire shape as canonical only via ADR 0010; treat the
  internal design as canonical only via this ADR.
- `docs/v1-analysis/invariants.md` "Two-frontend bridge shape" —
  ordering-qualified by this ADR's "one frontend in v1, second when
  driver appears." Same commit updates the invariant text to reflect
  this.
- Memory `project_sm_serial_bridge` — pre-v2 thinking. Updated in the
  same commit as this ADR landing.
- Memory `project_sm_field_master_topology` — confirms CAT lives at
  every writer's host; bridge subsystem activates per-host based on
  config; this ADR's design works identically whether the host is a
  single-station portable or one slave of a multi-station contest crew.
