---
number: 0039
title: Forwarder `enabled` gates enqueue; config is not sparse and forwarders are config-driven
status: Accepted
date: 2026-06-30
---

# 0039 — `enabled` gates enqueue; non-sparse, config-driven forwarders

## Context

ADR 0022 made queue enqueue gated by config **presence**: a forwarder defined in
`config.json` accumulated `qso_upload` rows even while `enabled: false`, and
`enabled` controlled only whether a worker goroutine ran. A present-but-disabled
forwarder therefore sat in a **"suspended" state** — QSOs queued at `pending`,
draining whenever it was re-enabled. The justification was a single workflow:
"pause a destination during an outage, catch up on re-enable."

ADR 0038 (shipped same week) removed that workflow's reason to exist. A
connectivity outage now retries **indefinitely** while a forwarder is *enabled*
(`OutcomeUnreachable`), so nobody needs to disable a destination to survive a
flaky link — you stay enabled and the queue drains when the link returns. With
that gone, the suspended state is a confusing artifact:

- It silently piles `pending` rows. Disabling QRZ for a contest weekend (to keep
  contest QSOs out of the logbook upload) *still* queued every contest QSO — the
  exact bite surfaced during the DB-manager design discussion.
- Re-enabling then auto-drains a backlog the operator may not have wanted sent.

Separately, forwarder endpoints (QRZ / ClubLog URLs) are hardcoded as Go consts
inside the forwarder packages, and the default config ships **no** forwarder
entries — the operator hand-adds an entry to opt in. Both are friction, and an
endpoint change needs a recompile. The enrichment side already solved the same
problem: lookup providers default their endpoints daemon-side (`config.Normalize`)
so the operator supplies only credentials, never a URL.

## Decision

Three coupled changes. They supersede **ADR 0022's enqueue rule** (rules 1–2);
ADR 0022's third rule — *retrospective backfill is operator-driven, never
automatic* — is kept and strengthened.

1. **Enqueue is gated by `enabled`, not presence.** Enabled → QSOs queue and
   upload (worker runs). Disabled → QSOs are **not queued**. Mechanically this
   re-adds the `enabled` check to `shouldEnqueue` that 0022 removed. Disabling a
   forwarder **discards its not-yet-uploaded rows** (option A): the authoritative
   answer to "uploaded to X?" is the ADIF upload stamp the worker writes on
   success (`<PREFIX>_QSO_UPLOAD_STATUS`), not the existence of a queue row — so a
   discarded row simply leaves the QSO showing "not uploaded to X." No lingering
   suspended state.

   **The discard happens at startup, not on a live toggle.** "Disable" is
   `enabled: false` in config plus a restart, so there is no in-process disable
   event to act on — the only place to reconcile is daemon startup. On start, for
   each **disabled** forwarder the daemon spawns no worker and **purges that
   forwarder's `pending` / `failed` / `in_progress` rows**, while **keeping its
   `uploaded` rows** (they carry `upstream_id` — the remote LOGID needed to
   forward a later *edit* as an update if the forwarder is ever re-enabled). The
   purge is logged loudly per forwarder, e.g. `forwarder "qrz" disabled; discarded
   14 queued uploads (re-upload via the logbook app)`, so it is never silent. The
   answer to "daemon stopped → forwarder disabled → daemon starts, what happens to
   the queued QSOs?" is therefore: **they are discarded (not uploaded); the QSOs
   stay in the log marked not-uploaded and are recoverable only via the logbook
   SPA's manual upload.** Leaving the rows dormant instead was rejected — a later
   re-enable would drain them, resurrecting the suspended state this ADR removes.

2. **Config is not sparse; forwarders are config-driven.** The default config
   carries an entry for **every supported forwarder type** (disabled, with the
   canonical endpoint URL + sane retry seeded daemon-side via the default
   template / `Normalize`). The operator flips `enabled` and pastes credentials —
   never hand-adds an entry, never types a URL. Endpoints live in config
   (overridable without recompile), not Go consts; the forwarder constructor
   takes its URL from the config entry. `Normalize` **adds missing forwarder
   entries** to existing configs on load, so installs converge to the non-sparse
   shape without a hand-edit (the config analogue of the reference.db bootstrap).

3. **Backfill is entirely operator-driven in the logbook SPA.** Every QSO not
   uploaded to a destination — logged while it was disabled, pre-dating it, or
   whose `pending` row was discarded on disable — is surfaced as "not uploaded to
   X" in the logbook SPA and uploaded manually there. One mechanism, every gap.

**Boundary:** config drives *which instances exist and their
endpoint/credentials/toggle*; **Go owns the type → implementation registry**.
Each forwarder type needs its own worker/client implementing the destination's
protocol/API (`Forwarder.Submit`). A brand-new destination cannot be defined
purely in config — config supplies a known type's parameters, not new behaviour.

## Alternatives considered

### Keep ADR 0022 (enqueue by presence, suspended state)
Rejected. ADR 0038 removed the only workflow it served, and the residual
behaviour is actively harmful: silent `pending` pile-ups (the contest bite) and
consent-free auto-drain on re-enable.

### Option B — disable leaves `pending` rows, resume on re-enable
Rejected. Re-introduces a small suspended state and keeps the queue row (not the
ADIF stamp) as a second, conflicting source of truth for "uploaded?". Option A is
cleaner and the stamp is already authoritative.

### Auto-backfill when a forwarder is enabled
Queue all prior QSOs the moment a destination is enabled. Rejected for the same
reason ADR 0022 rejected it: it removes operator consent — you may not want a
year of history pushed to a newly-enabled destination. Backfill stays a
deliberate logbook-SPA action.

### Keep endpoints as Go consts / keep config sparse
Rejected. Endpoint changes would need a recompile; hand-adding entries is
friction; and it's inconsistent with the enrichment-provider pattern that already
defaults endpoints daemon-side and exposes only credentials.

### Fully config-defined forwarders (type behaviour in config too)
Rejected. A destination's submit mechanics (QRZ form POST vs ClubLog realtime vs
LoTW signed upload) are code, not data. Config can parameterise a known client;
it can't express a new protocol.

## Consequences

**Signed up for:**
- A clean **two-state** model: enabled = active (queue + upload, outage-durable
  per 0038); disabled = off (don't queue). No confusing third state.
- Disabling is now the correct way to **not send** a set of QSOs (e.g. a contest)
  — no pile, no manual clear. The DB-manager "clear pending queue" feature we had
  sketched is now largely unnecessary.
- Endpoints editable without a recompile; the config SPA shows a fixed list of
  forwarder toggles rather than an add-an-entry flow.
- The logbook SPA's "uploaded?" status + manual upload becomes **load-bearing** —
  the sole backfill path. (Already a roadmap item; this pins it.)

**Accepted costs:**
- Disabling discards `pending` rows, so re-enabling does not resend what was
  logged while off — those QSOs need a deliberate manual re-upload. Acceptable:
  it's the operator-consent principle applied consistently.
- **"Disable" changes meaning from *pause* to *stop + drop*.** Under ADR 0022 a
  disabled forwarder's backlog survived and drained on re-enable; under 0039 the
  startup reconciliation discards it (pending/failed/in_progress; uploaded rows
  kept for provenance). For the dogfood operator this is a non-event (he never
  disables). For an external operator who disables expecting a pause, the backlog
  evaporates on the next restart — mitigated by the loud per-forwarder startup log
  line and by full recoverability through the logbook SPA, and to be called out
  in the operator manual.
- `Normalize` must add missing forwarder entries to existing configs **without
  clobbering operator edits** — a one-time convergence, same shape as the
  reference.db bootstrap.
- Any code that reads `pending` rows as "will eventually upload" must treat the
  **ADIF stamp** as the truth for "uploaded?", and must not read a disabled
  forwarder's absent rows as a gap.

**Gained:**
- Coherence with ADR 0038; the contest pile-up bite is fixed by construction;
  forwarders become data-driven and consistent with the enrichment providers.

## Triggers to revisit

- If an operator genuinely wants "disable temporarily, auto-catch-up on
  re-enable," reconsider an explicit opt-in suspended mode. Today nobody needs it
  — outages stay enabled per ADR 0038.
- If a forwarder type needs structured per-type config beyond URL + credentials
  (multiple endpoints, an auth mode, a station-location selector), the
  `ForwarderConfig` entry schema grows.
- Multi-operator / master-aggregator (per the field-master topology) turns "which
  destinations" into a per-station-then-aggregated question.
- If `Normalize`-adds-missing-entries proves surprising (operators who *want* a
  forwarder type absent), reconsider seeding-on-first-run-only vs every-load.

## References

- ADR 0022 (`0022-forwarder-enqueue-by-config-presence.md`) — **superseded in
  part**: its enqueue rule (presence-gated) is reversed here; its
  operator-driven-backfill rule is kept and strengthened.
- ADR 0038 (`0038-forwarder-durable-connectivity-retry.md`) — the trigger:
  forever-retry-while-enabled removed the suspended state's only use case.
- ADR 0014 (driver-shaped forwarder layer), ADR 0016 (cloud deferred — backfill
  sits in single-operator scope).
- `internal/qsoservice/forwarders.go` — `shouldEnqueue` (re-add the `enabled`
  check) + callers `submit.go`/`update.go`/`delete.go`.
- `internal/forwarding/` — `Forwarder` contract + the `qrz`/`clublog` clients
  (move endpoint consts → config-supplied), the type registry.
- `cmd/smd/main.go` — the startup disabled-forwarder reconciliation (purge
  pending/failed/in_progress, keep uploaded, log loudly) lands beside the existing
  orphan-reset sweep (`ResetOrphanedUploadsWithContext`); a new sqlite method
  (e.g. `DiscardQueuedUploadsForForwarder`) backs it.
- `internal/config` — default-config template, `Normalize` (seed default
  endpoints, add missing entries), `types.ForwarderConfig` (carry endpoint URL).
- `docs/v2-design/forwarding.md` (Tier 2, historical — not updated).
- Memory `project_sm_forwarder_enqueue_policy`, `project_sm_forwarder_durability`,
  `feedback_logging_vs_logbook_scope`, `feedback_kiss_frictionless_for_operator`.
