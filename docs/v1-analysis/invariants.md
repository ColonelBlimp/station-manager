# Station Manager — Architectural Invariants

**Status:** v1 analysis document, 2026-04-14. Load-bearing rules that have been explicitly articulated and agreed during the analysis effort. These must carry forward into v2 (rewrite or refactor), and any future proposal should be checked against this list — if a proposal contradicts one of these, flag it explicitly rather than silently accepting.

**Purpose:** Some rules aren't details, they're constraints the design must satisfy. Detail decisions can be revisited; invariants generally cannot without revisiting the whole design. This document keeps the distinction clear.

**How this document relates to others:**
- `design-decisions-log.md` — individual decisions with keep/change/delete/undecided verdicts. Invariants here are upstream of those decisions.
- `lessons-for-v2.md` — design patterns and principles. Invariants here are the non-negotiables; lessons-for-v2 has the softer guidance.
- `bug-inventory.md` — any bug that violates an invariant here is load-bearing, not cosmetic.

---

## Logging service / daemon core

### Nothing blocks logging a QSO, except catastrophic local failure

This is the master rule from which several more specific invariants below are derived. **The operator must always be able to log a QSO.** The only legitimate reason the logging app can refuse to accept a QSO is a catastrophic failure in its own local storage — disk full, filesystem read-only, or equivalent hardware/OS conditions that prevent writing *anywhere* on the local machine. Anything short of that is survivable and must not block logging.

**Specifically, the following conditions must never block logging:**
- External enrichment services (hamnut.com, QRZ.com, any callsign/country lookup) being down or slow.
- The daemon process being unavailable — either because it crashed, because it hasn't started yet, because it's being restarted, or because it's on another machine and the network is down.
- Upstream forwarding services (QRZ logbook, ClubLog, future SM-Online) being down or rejecting submissions.
- Disk write errors affecting non-critical paths (log files, caches) while the authoritative log path is still writable.
- Any transient database lock contention, connection pool exhaustion, or other recoverable infrastructure hiccup.

**The logging app owns its own durability independent of the daemon.** When the daemon is reachable and healthy, the normal submission path goes `logging app → daemon HTTP API → sqlite + upload queue`. When the daemon is not reachable, the logging app falls back to writing the QSO to its own local text file (plain-text ADIF on disk, owned by the logging app, outside the daemon's sqlite database). The operator sees the QSO as logged in both cases — the only difference is which storage path handled it.

**The logging app should visibly note the degraded state** when it's operating in text-file fallback mode so the operator knows the daemon is unavailable and action may be needed later. It must not require the operator to do anything different to keep logging.

**Why this matters:** the logger is the operator's real-time tool during a contest, a session, a fleeting band opening. The cost of refusing to log a single QSO because a background service is flaky is catastrophic for the workflow (the contact is gone, the opportunity is lost). The cost of deferring to a text-file fallback and reconciling later is nothing — the QSO is recorded, the operator moves on, the sync happens later. **This is the single most load-bearing operational requirement in the system.**

**How to apply in v2:**
- Every path in the logging app that touches the daemon's HTTP API must have an explicit fallback that writes to the local text file and returns success to the UI. A failed daemon submit is a *warning*, not an error.
- Every path in the daemon that touches an external service must have an explicit fallback (cached value, synthesized default, or skip-and-continue). A failed external call is a *warning*, not an error.
- When designing new features, the first test to write is "what does this do when the daemon is down / the external service is broken / the network is gone." The answer must be "continue logging."
- Catastrophic local failure is detectable (the write syscall returns an error) and should be surfaced to the operator immediately — this is the one case where the UI does say "stop, fix this now."

**Related:** the existing "Enrichment never blocks logging" and "Forwarding never blocks logging" (below) are specific applications of this master rule. The text-file fallback architecture in the logging app is the mechanism that extends the protection all the way to "the daemon is down."

**Reconciliation flow (deferred feature):** when the daemon comes back online after an outage, the logging app should resubmit any QSOs it captured only to its text-file fallback during the outage. The submission uses the daemon's normal submit API (`POST /v1/qso` or whatever the endpoint ends up being) — not a special import endpoint, not a direct file-merge path. The dedupe key (see "Contest dupe check and general ingest dedupe" below, and `docs/v2-design/api.md` when it exists) silently absorbs any accidental double-submissions from overlapping reconciliation attempts. This flow is not a day-one requirement; it is a recognized future enhancement to investigate, and is tracked in `docs/session-handoff.md` → "Deferred features to investigate."

### Enrichment never blocks logging

(This invariant is a specific application of the master rule above. Preserved here because it has a concrete v1 fix history.)

External lookups (hamnut.com, QRZ.com, any future callsign/country service) are **best-effort**. Any failure in enrichment degrades the QSO draft's completeness but never prevents the operator from logging.

**Why this matters:** this principle was violated in v1's `initCountrySection`, where a failed hamnut.com lookup propagated as a fatal error and prevented the operator from starting a QSO *at all*. Fixed 2026-04-14. The bug was subtle because the local country table had valid rows for the callsign — the code just refused to use them if the online confirmation failed. The fix synthesizes `types.Country{Name: "Unknown"}` when there's no local row and no online data, so logging proceeds with whatever the operator can type.

**How to apply in v2:** every enrichment path must have an explicit fallback. When writing a new enrichment caller, the first test to write is "what does this do when the external service is down," and the answer had better be "log a warning and continue, not return an error to the caller."

### Forwarding never blocks logging

Upstream forwarding services (QRZ logbook, ClubLog, future SM-Online) are **best-effort downstream** of the authoritative local write. A submitted QSO is considered logged the instant the local transaction commits — before any attempt is made to push it to any online service.

The daemon's forwarding worker runs asynchronously, picks up pending `qso_upload` rows after the logging response has already been sent, attempts each configured destination, and writes the outcome (succeeded / failed / retry-pending) back to the row. Forwarding failures are surfaced to clients via SSE events and via re-queries, never by blocking the submit path.

**Why this matters:** the same reason as the master rule. QRZ being down or slow must not affect the operator's ability to log. A flaky upstream does not get to take the logger offline. This invariant extends the same non-blocking guarantee to the "after I write locally, what happens next" part of the pipeline.

**How to apply in v2:** `POST /v1/qso` (or whatever the submit endpoint becomes) must return its response as soon as the local sqlite transaction has committed — not after any forwarder has accepted the QSO. Forwarding state is a separate observable via SSE and `GET /v1/qso/:id/uploads` or similar, pulled on demand, never coupled to submit latency.

### One-fails-all-fail for QSO writes

The QSO row and its upload-queue row(s) are **atomic** — a single database transaction, all commit or none do. A write that succeeds in the QSO table but fails in the upload-queue table must roll back the QSO, because otherwise the QSO exists but will never be forwarded, and the caller will retry the write and produce a duplicate.

**Cache-table writes (`ContactedStation`, `Country`) live *outside* the transaction and are best-effort**, because they are caches/enrichment, not authoritative. A failed cache write logs a warning and returns success to the caller, because the cache is rebuildable from hamnut.com on next access.

**Why this matters:** v1 shipped with this bug (identified and fixed 2026-04-14). `LogQso` originally ran four sequential non-transactional writes: `InsertQso` → `insertOrUpdateContactedStation` → `insertOrUpdateCountry` → `InsertQsoUpload`. The first succeeded durably; the fourth could fail; the caller retried and got duplicates. Fixed by wrapping insert + upload-insert in a `BeginTxContext` transaction and moving the cache writes outside the transaction as best-effort.

**How to apply in v2:** the atomic unit is (authoritative write + its side-effect state in authoritative tables). Cache writes and other derived state are outside the transaction boundary and are explicitly non-fatal.

### Core concern is log + forward, nothing else

Station Manager's core is **logging QSOs to a local database and forwarding them to online services**. Everything else — manual data entry, FT8 capture, WSJT-X ingest, rig control, audio, CAT, PTT — is a *client* of this core, not part of it.

**Do not let rig control, capture UX, or protocol decoding bleed into the daemon's responsibilities.** The daemon is stable and narrow; the clients are replaceable and many.

**Why this matters:** v1 conflated data capture with storage/forwarding. Adding WSJT-X UDP ingest and FT8 support exposed this as a structural mistake — FT8 had to be extracted to its own repo, WSJT-X integration hit a serial-contention wall, and the logging app accumulated responsibilities that don't belong together. v2's decomposition (daemon + clients + bridge) exists specifically to avoid re-making this mistake.

### Contest dupe check and general ingest dedupe are two different things

**General idempotency:** "did I already ingest this exact QSO" — hash on `(call, band, mode, time)` at ingest time, applies to bulk import and UDP bridges. Used to prevent double-logging from multiple ingest sources.

**Contest dupe:** "per contest rules, is this call already worked on this band in this logbook" — different semantics, different scope, different caller (manual logger mid-entry, synchronous). Contest rules vary by contest, often per-band or per-band-per-mode.

**Do not conflate these.** They share the word "dupe" but nothing else.

### Authoritative data vs cache of remote

The local database has **two data categories** and they must be treated differently:

- **Authoritative:** `logbook`, `qso`, `session`, `qso_upload`. Source of truth. Backup matters. Loss = real data loss. Must be part of transactional writes.
- **Cache of remote (hamnut.com, etc.):** `country`, `contacted_station`. Rebuildable. Can be nuked on schema migration without ceremony. Writes are best-effort, outside the log/forward transaction.

Backups, migrations, and "blow away to force refresh" operations should treat these categories differently.

## Data model

### `types.Qso` follows the ADIF specification

**`types.Qso` is shaped to mirror the ADIF spec.** Every ADIF tag the software supports has a corresponding field in `types.Qso` (or one of its nested sub-structs — the nesting is a Go-level organizational convenience; ADIF itself is flat). In the long run, `types.Qso` will follow ADIF faithfully.

**Why this matters:** ADIF is an actively-evolving specification. New tags are added periodically by the maintainers. Station Manager needs a data shape that can absorb spec evolution without churning the schema or the codebase, and the design answer is "make the application type track ADIF directly, and make the storage absorb the overflow via the `additional_data` pattern below."

### The `additional_data` JSON blob absorbs ADIF spec evolution

Database tables have **real columns only for fields that are queryable, indexed, or frequently filtered on** — `call`, `band`, `mode`, `freq`, `qso_date`, `time_on`, etc. Everything else (the vast majority of ADIF fields) is serialized into an `additional_data` JSON blob column.

This keeps schema changes to a minimum: **adding a new ADIF field should be a one-line change** (add a field to `types.Qso`), and the storage layer carries it through automatically via JSON marshaling. Schema migrations happen only when a field is *promoted* to a real column — which is a deliberate feature decision (you want to query on it, index it, or filter by it), not a spec-tracking obligation.

**Empty fields are omitted from the blob (per ADR 0015).** Every field on `types.Qso` and its embedded sub-structs is tagged `,omitempty`, so a freshly-marshalled blob carries operator-set / enriched data only — not "field exists but empty" noise. Read-back via `json.Unmarshal` is unaffected (missing fields → zero value), so the round-trip is symmetric. Adding a new ADIF field is still a one-line change, and the new field will only appear in blobs of QSOs that actually use it.

**Why this matters:** without this pattern, every new ADIF tag is a schema migration. That's a lot of operational friction for a specification that changes on the maintainers' schedule, not yours. The pattern also keeps the schema small and focused on what actually gets queried, rather than being a giant flat table that mirrors ADIF.

**Constraint on the adapter layer:** the adapter's job is to translate between `types.Qso` (full ADIF shape) and the storage row (promoted columns + blob). It must preserve the property that adding a new field to `types.Qso` requires no other change. If the adapter has a second struct type that mirrors `types.Qso` and must be manually kept in sync (as `types.QsoAdditionalData` does in v1), the property is violated and the adapter needs to be fixed. See `design-decisions-log.md` and `bug-inventory.md` for the specific v1 violation and the planned simplification.

### Per-database adapter pattern, not generic-across-backends

If multiple database backends are ever supported, each gets its **own adapter package** (`internal/database/sqlite/adapters/`, `internal/database/postgres/adapters/`, etc.), and they share nothing at the Go level beyond `internal/types`.

**The attempt to build a generic reflection-based adapter framework (`internal/adapters/` in v1) is dead. Full stop.** It was abandoned as too complicated to maintain and too hard to use correctly, and it will be deleted from the repo as part of v1 cleanup or not carried into v2. Do not try to resurrect it, do not try to build a simpler generic version — per-driver is the settled pattern.

**Why this matters:** the cost of a small amount of code duplication between per-driver adapters (the field-copying is mostly mechanical) is far less than the cost of a generic framework that tries to abstract over different backends' conventions, column types, and JSON handling. The generic version looks clean at the design stage and becomes unmaintainable in practice.

## Serial / CAT bridge

### CAT, PTT, and audio are three separate contention problems

Do not design solutions as if they're unified:

- **CAT** — solved by the rigctld-compat TCP bridge.
- **PTT** — has its own arbitration (lease model with auto-release on disconnect; stuck-PTT-on-disconnect is a hard safety requirement).
- **Audio** — usually shareable via pipewire/pulseaudio on Linux; different OS story on Windows/macOS. Not a serial-contention problem, different problem class entirely.

Any design that treats these as one problem will get one of them wrong.

### Multi-rig is a first-class assumption from day one

Serious stations routinely run more than one rig (SO2R, contest setups, FT8 on rig A while phone/CW on rig B). **Each rig is on its own physical port and is an independent contention domain.** One rig's PTT lease, CAT stream, and AUTO/transceive state has nothing to do with the other's.

The bridge must be **multi-rig capable from the start**: one bridge process managing N ports, per-rig state isolation, rigctld frontend exposing each rig on its own TCP port (4532, 4533, etc. per hamlib convention), SM-native event stream carrying a rig identifier so clients can subscribe per-rig or to all rigs.

**Corollary:** FT8 and phone/CW on the same rig at the same time is **not a real scenario** and the bridge does not need to arbitrate it. If both modes are active simultaneously at a station, they are on different rigs on different ports.

### CAT is push-state / AUTO mode, not strict request/response

Modern rigs (Yaesu AUTO, Icom CI-V transceive) **broadcast state changes unsolicited**. Clients are observers that occasionally poke the rig; broadcasting rig state to multiple clients is a feature, not corruption. The user's own CAT library leans into this.

Do not reason about CAT as strict request/response. The rig is a state broadcaster, and "client B received a frequency update it didn't ask for" is a feature — B's local picture stays in sync with reality.

### Stuck-PTT safety is a hard requirement

On **any** client disconnect (TCP drop, Unix socket close, client process death), the bridge must force PTT to RX immediately.

Stuck key-down damages the rig's finals and violates continuous-transmission regulations. Only the bridge can guarantee this because clients cannot clean up after their own crashes. This is not a design choice — it's a safety requirement.

### PTT arbitration is a simple lease model

Bridge grants PTT to the first requester, auto-releases on explicit unkey / release / client disconnect, rejects the second requester cleanly until released.

Does not need a sophisticated fairness algorithm because in practice contention is rare:
- WSJT-X/JTDX holds PTT during FT8 TX slots (~12.6s each).
- SM's logging app treats PTT as off-by-default, only claimed on-demand for the optional voice-keyer feature (which is purely ergonomic, not a contest requirement — voice-keyer just saves the operator's voice during long sessions).

If an operator triggers voice-keyer mid-WSJT-X-transmission they get a clean rejection — that's a user mistake, not something the bridge has to arbitrate elegantly.

### Two-frontend bridge shape

> **2026-05-10 ordering qualifier (ADR 0019).** Two frontends is the *eventual* canonical shape. **v1 ships ONE frontend** — SSE on `/v1/rig/events` for the logging SPA — because the SPA is the only first-class CAT consumer today. rigctld-compat TCP and NDJSON Unix-socket frontends are deferred until a real driver appears (third-party app needing bridge-mediated CAT, or a non-browser in-house client like the FT8 stack or a CAT control SPA). The invariant remains correct as a long-term shape; the v1 path is one frontend, then the second when its consumer materialises.
>
> ADR 0010 settled the SM-native event stream as **SSE over the daemon's HTTP server**, not NDJSON over Unix socket. The §"Serial/CAT bridge SM-native frontend: NDJSON over Unix socket" entry below is the pre-ADR-0010 thinking; treat it as historical for the SPA path. NDJSON over Unix socket may still ship later as a separate frontend for non-browser in-house clients (their drivers determine).

The serial/CAT bridge has **two frontends on one internal pipeline**:

1. **rigctld-compat TCP** for third-party interop (WSJT-X, JTDX, anything speaking "Hamlib Net rigctl"). Zero config change required on their side — they already support "Hamlib Net rigctl" as a rig type.
2. **SM-native event stream** over Unix socket for in-house clients (manual logger, FT8 client, future SM clients). Delivers rig state in the CAT library's native vocabulary, not hamlib's — SM clients are not forced to inherit hamlib's limitations, mode vocabulary, or VFO model.

Internally, the bridge consumes the rig's AUTO/transceive stream once, parses it once via the user's CAT library, and fans it out on both frontends. Commands from both frontends go through one serialization queue to the rig.

## Transport / API

### Log/forward daemon: HTTP+JSON/ADIF over Unix domain socket + SSE for events

**Settled direction.** Unix socket for filesystem-permissions auth (single-user desktop), ADIF as raw POST body for submit (no JSON wrapping — lets any ADIF-producing tool POST directly), SSE for `qso.stored` / `forward.succeeded` / `forward.failed` / etc.

gRPC was considered and rejected as ceremony for an all-Go stack with a small, stable API surface.

### Serial/CAT bridge SM-native frontend: NDJSON over Unix socket

**Different service, different workload** (continuous bidirectional streaming vs. request/response with occasional events), so a different protocol is justified. One bidirectional connection per client, each line a JSON object with a `type` field, no HTTP layer, connection-lifetime = lease-lifetime for free (PTT release, subscription cleanup on socket close).

Not reused from the log/forward daemon's HTTP+SSE stack because the rig bridge's traffic profile is continuous and bidirectional rather than request/response-dominant; forcing HTTP+SSE on it would cost performance and code clarity without any consistency benefit.

## Decomposition

### The three-concerns split across `apps/logging` / `apps/logbook` / `apps/config` is deliberate and must carry forward

- **`apps/logging`** — real-time QSO entry during operating sessions. High-frequency, low-latency, focused UI.
- **`apps/logbook`** — logbook management and historical QSO editing. Creating, deleting, exporting logbooks; editing historical records. Low-frequency, higher-latency-tolerant, handles large result sets.
- **`apps/config`** — configuration editing. Rare-use, focused UI, distinct from the daily-driver apps.

These are **different workflows with different latency profiles and different UI focus**. Keeping them as three separate apps keeps each one simple and focused, and avoids cross-concern UI bloat.

**Implication for v2 daemon:** the daemon API surface must serve **all three concerns**, not just logging. Earlier API sketches in session discussions were logging-centric and were missing the entire logbook-management surface (logbook CRUD, batch QSO edit, export, etc.). A dedicated API-shape exercise for v2 needs to explicitly cover logbook-management endpoints as a first-class slice of the API, not an afterthought.

### Daemon scope is explicitly narrow

- **Daemon log/forward subsystems are narrow.** In scope: ingest (ADIF via HTTP POST and/or UDP), validate, store in local DB, forward to online services (QRZ, ClubLog, future SM-Online), emit status events, serve queries for logging/logbook/config concerns.
- **Not in the log/forward surface:** rig control, CAT, PTT, audio, protocol decoding, capture UX. Those live in clients (browser SPA) or in the daemon's bridge subsystem (ADR 0013), but they must not couple with the log/forward subsystems.

**Where the boundary is enforced (revised 2026-05-02 by ADR 0013):** the protection used to be a process boundary — the bridge ran in its own binary, so log/forward code physically could not reach into rig state. Under ADR 0013 the bridge runs as a daemon subsystem in the default deployment, so the boundary moves down to the **package-import graph**: the bridge package exposes only its public Go interface (route registration, internal-API for rig state), and the storage / forwarder packages **do not import** the bridge package. The narrow-daemon-scope rule is preserved at this lower level. Reviewers (and any future lint check) treat a forbidden import as a violation of this invariant. Process-boundary enforcement is still available for the split-host opt-in deployment (separately-built `cmd/bridge` binary), but is no longer the default mechanism.

This narrow scope is what makes the daemon's log/forward surface stable — it's the part of the system that shouldn't churn, because every client and every bridge feature plugs into it.

---

## How to apply

When discussing any architectural proposal in future sessions:

1. **Check against each invariant above.** If a proposal violates one, flag it explicitly and ask whether the invariant should be revised or the proposal reshaped. Do not silently accept a violation.
2. **Do not re-derive these from scratch.** They are settled. If new invariants emerge in future sessions, add them here.
3. **This document is source material for the v2 design specification**, along with `design-decisions-log.md` and `lessons-for-v2.md`. When v2 design starts in earnest, these three documents together constitute the starting-point spec.
