# Station Manager v2 — Forwarding Subsystem

**Status:** Fourth entry in `docs/v2-design/`, written 2026-04-18 at the
start of milestone 1c work. Draft — everything here is revisable until
code contradicts it.

**Purpose:** Settle the internal shape of the forwarding subsystem
before writing code. The API-facing side of forwarding is already
decided in `api.md §4.3` and `api.md §4.5`; this document covers the
pieces that are internal to the daemon: fan-out config, the
`Forwarder` plugin boundary, worker topology, retry policy, queue-row
lifecycle, reload semantics, and the crash-safety helpers the workers
need.

**Why this document exists:** The v1 forwarder was hardcoded to QRZ at
the ingest site. v2 needs multi-destination fan-out (QRZ, ClubLog,
LoTW, eQSL, future SM-Online). That is one structural change, and
three other concerns need answers before any code lands: how a
destination is declared in config, how the daemon dispatches to it,
and how retries survive a daemon restart. Settling these in prose
first prevents the worker topology being "discovered" late.

**How this document relates to others:**

- `docs/v1-analysis/invariants.md` — "Forwarding never blocks logging"
  and "one-fails-all-fail for QSO writes" are the two rules this
  design is built around.
- `docs/v2-design/api.md` — `§4.3` (async forward lifecycle),
  `§4.5` (SSE vocabulary — `forward.succeeded` / `forward.failed`
  are the only events this subsystem emits), `§5` (the
  `GET /v1/qso/:id/uploads` pull endpoint).
- `docs/v2-design/milestones.md` — milestone 1c is the milestone this
  design enables. The acceptance test there is the finish line.
- `docs/v1-analysis/design-decisions-log.md` → "Hardcoded QRZ forwarder
  in `LogQso` / `UpdateQso`" — the decision to redesign fan-out.
- `docs/v1-analysis/bug-inventory.md` → "Hardcoded QRZ forwarder" —
  the v1 problem this subsystem exists to not recreate.
- `docs/reviews/milestone-1b-review.md` → M8 (forwarder retry
  cooldown) — deferred TODO that lands here.
- `docs/session-handoff.md` — the `safego` worker-recovery helper
  was parked there; it has since landed as `internal/safego`
  (stage 5).

---

## Terminology

A few terms are used throughout. Defined here in one place so later
sections can use them without re-explaining.

- **Forwarder** — a plugin implementing the `Forwarder` interface
  (§3) that pushes QSOs to one specific upstream destination (QRZ,
  ClubLog, LoTW, …). "The QRZ forwarder" = `internal/forwarding/qrz`.

- **`forwarder_name`** — the **per-instance** label the operator
  picks in config (defaults to the type name, e.g. `"qrz"`). Used
  to key the `qso_upload.forwarder_name` column and scope the
  worker's claim query. Ham online services are effectively
  singleton per operator — one QRZ account, one ClubLog, one LoTW
  cert — so in practice `name` and `type` are usually the same
  string. The split exists for rename safety (see below), not
  because operators run multiple instances of the same type.

- **`forwarder_type`** — the **plugin identifier** returned by
  `Forwarder.Type()` (e.g. `"qrz"`, `"clublog"`). Stored on the
  row alongside `forwarder_name` so historical queue entries
  remain interpretable after the operator renames or deletes the
  instance from config — if `name` were the only handle, renaming
  would orphan every existing row for that destination.

- **Outcome** — one of three classifications returned by
  `Forwarder.Submit` (§3):
  - `OutcomeSuccess` — upstream accepted.
  - `OutcomeTransient` — try again later (network error, rate limit,
    temporary upstream outage). The worker re-queues per the retry
    policy in §5.
  - `OutcomeTerminal` — upstream definitively rejected; **retrying
    will not help** (malformed data, revoked credentials, etc.). The
    worker marks the row `failed` immediately, no retries. Only the
    forwarder knows enough about its upstream to make this call.

- **Terminal state** — a row whose `status` is `uploaded` or
  `failed`. No more work will be done on it. Contrast with `pending`
  (waiting) and `in_progress` (actively being submitted).

- **Terminal transition** — the moment a row enters a terminal
  state: `in_progress → uploaded` or `in_progress → failed`. These
  are the **only** two transitions that emit SSE events (§7).

- **Claim** — the atomic `UPDATE ... RETURNING` (§4) that flips a
  row from `pending` to `in_progress` and hands it to the worker.

- **Tick** — the periodic wake-up of a worker goroutine (§4).
  Defaults to 120 s, per v1 operational experience and the
  operator's slow/unreliable link.

**Cause vs state:** `OutcomeTerminal` is a cause the forwarder
reports. A terminal transition is the row-state effect. A terminal
transition can be caused by `OutcomeSuccess` (→ `uploaded`),
`OutcomeTerminal` (→ `failed`), or `OutcomeTransient` hitting
`max_attempts` (→ `failed`). Don't conflate the two.

---

## 1. Constraints carried forward

Everything below must satisfy these. They are decided elsewhere; this
list exists so that proposals in later sections can be checked against
them at a glance.

1. **Forwarding never blocks logging.** `POST /v1/qso` returns as soon
   as the local sqlite transaction commits. Nothing in this subsystem
   runs on the request path.
2. **One-fails-all-fail for the ingest transaction.** The QSO row and
   one `qso_upload` row per configured destination are inserted in a
   single sqlite transaction. Forwarders **consume** queue rows; they
   never write them. If queue-row insert fails, the whole ingest
   fails and the submit returns an error.
3. **Local log is authoritative.** A QSO is "logged" the moment it is
   in sqlite. Forward status is metadata on an already-logged QSO,
   never a gate on whether the QSO exists.
4. **Narrow daemon scope.** The daemon owns log + forward. Anything
   UX-y (spinners, badges, per-operator retry buttons) lives in the
   client, driven by pull (`/v1/qso/:id/uploads`) or push (SSE).
5. **Personal-operator scale.** At most tens of QSOs per active
   session, a handful of destinations, a single user. Simple and
   sequential beats clever and concurrent wherever the tradeoff is
   neutral.
6. **Home-grown over framework.** No job queue library, no cron
   library, no retry framework. Standard library + a goroutine per
   destination. v1's retry loop, goroutine topology, and upload-queue
   polling shapes are the reference point; the thing being replaced
   is the fan-out, not the mechanics.

---

## 2. Fan-out config shape

**Decision:** Each forwarder is a named entry in a `forwarders` array
in `config.json`. Every entry has a `type`, a human-friendly `name`,
an `enabled` flag, a `credentials` object whose shape is
type-specific, and an optional `action_filter` restricting which QSO
actions this destination cares about.

```jsonc
{
  "forwarders": [
    {
      "name": "qrz",
      "type": "qrz",
      "enabled": true,
      "credentials": {
        "api_key": "..."
      }
    },
    {
      "name": "clublog",
      "type": "clublog",
      "enabled": false,
      "credentials": { "email": "...", "password": "...", "callsign": "..." }
    }
  ]
}
```

**Why an array, not a map keyed by type:** arrays preserve order and
give the operator a natural place to hang per-instance overrides
(`tick_interval_sec`, `batch_size`, `retry`). In everyday use `name`
and `type` will be the same string — ham online services are
effectively singleton per operator (one QRZ account, one ClubLog,
one LoTW cert), so a second instance of the same type is vanishingly
rare. The split exists so that `qso_upload` rows remain interpretable
when an operator *renames* a destination (e.g. after rotating an API
key they relabel `qrz` → `qrz-2026`): `forwarder_type` stays `"qrz"`,
historical rows still make sense.

**Why credentials are a nested object, not flat fields:** each
forwarder type has its own authentication shape. QRZ wants just
`api_key` (each QRZ logbook has a unique key that both authenticates
the caller and selects the logbook — QRZ itself enforces the
callsign match and rejects QSOs whose `STATION_CALLSIGN` doesn't
match the logbook); ClubLog wants `email`/`password`/`callsign`;
LoTW wants a certificate path. Nesting the type-specific fields
under `credentials` keeps the top-level shape uniform and the
type-specific unmarshaling local to the forwarder's own package.

**Why `action_filter` is explicit:** v1 uploaded everything to QRZ
including deletes. Some destinations don't support updates or deletes
cleanly (LoTW is famously write-once). The filter lets the operator
restrict a destination to just inserts without changing code. When
omitted from config, defaults to `["insert","update","delete"]`.

**Why retry defaults live in the forwarder package, not config.** QRZ
and LoTW have very different tolerance for repeated submits (QRZ is
happy with retries every minute or two, LoTW should not be hammered).
Each forwarder's own package carries sensible upstream-specific
defaults — `qrz.New` knows what QRZ can tolerate — so the common-case
config doesn't carry a `retry` block at all. Operators who need to
deviate drop an explicit `retry` object into the entry, and it
overrides. This keeps operator-facing config lean without violating
the no-magic-numbers rule: the fallback values are still named
constants in code, just scoped to the forwarder that knows what
they mean.

**Validation at startup.** `config.Load` validates that every
forwarder has a non-empty `name` and `type`, unique names across the
array, valid entries in `action_filter`, and (if present) sane
`retry` bounds. Type-specific credential validation happens later
when the forwarder package's constructor is called — only that
package knows what its credentials object should contain. Invalid
config → daemon refuses to start, with an error naming the offending
forwarder.

**Alternative considered:** a single global `enabled_forwarders`
array of type-strings, with credentials pulled from environment
variables. Rejected — it couples the config model to env-var naming
conventions and loses the per-forwarder override slots (action
filter, tick interval, retry override) without a compensating
benefit for a single-operator deployment.

---

## 3. The `Forwarder` interface

**Decision:** Every forwarder implements a small, synchronous
interface. Each destination lives in its own package under
`internal/forwarding/<type>/`. The worker layer calls `Submit` and
interprets the result; the forwarder has no knowledge of the queue
row, the retry policy, or the SSE event vocabulary.

```go
package forwarding

// Forwarder is the plugin boundary between the worker layer and a
// concrete destination (QRZ, ClubLog, LoTW, …). Implementations are
// stateless with respect to the queue; they make one network call
// per Submit and return a Result describing the outcome.
type Forwarder interface {
    // Type returns the forwarder's type identifier, matching the
    // "type" field in config.json. Used for logging and for
    // qso_upload.forwarder_type.
    Type() string

    // AdifPrefix returns the ADIF field-name prefix that the worker
    // stamps on the QSO row on successful submit — e.g. "QRZCOM" for
    // QRZ (producing QRZCOM_QSO_UPLOAD_STATUS / QRZCOM_QSO_UPLOAD_DATE).
    // Return "" for forwarders with no corresponding ADIF slot; the
    // worker then skips the QSO-row stamp and only updates qso_upload.
    AdifPrefix() string

    // Submit attempts to push one QSO + action pair to the upstream
    // service. It MUST respect ctx cancellation. It MUST NOT retry
    // internally — retries are the worker's job.
    //
    // priorUpstreamID is the UpstreamID recorded on a prior successful
    // Submit for the same (QSO, forwarder) pair — populated by the
    // worker only for action=Delete, empty otherwise. Forwarders that
    // need the upstream's record id to issue a delete (e.g. QRZ, which
    // takes LOGIDS) read it from here; forwarders that don't, ignore it.
    Submit(ctx context.Context, qso types.Qso, action Action, priorUpstreamID string) Result
}

type Action string // "insert" | "update" | "delete"

type Outcome string
const (
    OutcomeSuccess      Outcome = "success"      // upstream accepted, mark uploaded
    OutcomeTransient    Outcome = "transient"    // try again later per retry policy
    OutcomeTerminal     Outcome = "terminal"     // upstream rejected; mark failed
)

type Result struct {
    Outcome    Outcome
    Err        error  // set when Outcome != success; stored in qso_upload.last_error
    UpstreamID string // optional — some services return a remote ID (QRZ: LOGID)
}
```

**Why three outcomes, not just error/nil:** the classification
`transient vs terminal` is the forwarder's call, not the worker's.
Only the QRZ module knows that "Invalid session" is transient (log
in again and retry) but "CALL/QSO_DATE required" is terminal
(rejecting won't help). Lifting this into the interface prevents
error-classification logic being duplicated across types or — worse —
guessed wrong in the worker layer.

**Why Submit takes `types.Qso`, not the `qso_upload` row:** the
queue row is an implementation detail of the worker layer.
Forwarders should be testable in isolation with nothing but a QSO
and HTTP fixtures.

**Why no `Initialize` / `Close` on the interface:** forwarders are
stateless. Any per-forwarder setup (HTTP client construction,
credential validation) happens in the package's constructor
(`qrz.New(cfg Config) (*Forwarder, error)`). Shutdown is just
letting the goroutine exit; no explicit teardown.

**Why `AdifPrefix`:** the ADIF spec defines per-service upload-status
fields (`QRZCOM_QSO_UPLOAD_STATUS`, `CLUBLOG_QSO_UPLOAD_STATUS`, …).
When a submit succeeds, the corresponding QSO-row field should be
stamped `"Y"` + today's date so ADIF exports and client UIs see
accurate state without joining against `qso_upload`. The **worker**
performs the stamp in the same sqlite tx as `MarkUploadSuccess`;
`AdifPrefix` is just declarative metadata telling the worker which
field pair to write. Forwarders without an ADIF slot (custom
webhooks, SM-private destinations) return `""` and the QSO stamp is
skipped. See §6 for how this shows up at the storage layer. v1 did
this write from inside the QRZ service, which broke the plugin
boundary; the v2 shape keeps forwarders pure by moving the write to
the worker.

**Why `priorUpstreamID` on Submit:** some upstreams (QRZ, ClubLog)
need the remote record id to issue a delete. That id was captured on
the earlier successful insert as `Result.UpstreamID` and stored in
`qso_upload.upstream_id`. For a delete action, the worker looks up
that insert row's `upstream_id` and passes it through. For
insert/update the param is empty and forwarders ignore it. The
alternative shapes considered (stashing it on `types.Qso`, or letting
the forwarder query sqlite itself) either abused the DTO or broke
the plugin boundary; a single extra interface param is the least
invasive honest shape.

**Package layout:**

```
internal/forwarding/
├── forwarding.go       # interface + Result, Outcome, Action types
├── registry.go         # type-name → constructor registry
├── worker/             # worker/dispatcher implementation (§4)
│   ├── worker.go
│   └── worker_test.go
├── qrz/
│   ├── doc.go
│   ├── qrz.go          # implements forwarding.Forwarder
│   └── qrz_test.go
├── clublog/            # future, milestone 1c+
├── lotw/               # future
└── ...
```

**v1 reference.** The v1 QRZ code in `internal/upload/qrz/` is good —
the XML request/response shapes, the session-key caching, the
response parsing. The v2 port keeps the HTTP and parsing logic and
rewraps it behind this `Forwarder` interface. It is **not** a
ground-up rewrite.

---

## 4. Worker topology

**Decision:** One goroutine per enabled forwarder. Each goroutine owns
its destination's `qso_upload` rows exclusively: it claims, submits,
updates status, sleeps. The goroutines are independent — there is no
central dispatcher and no shared work queue.

```
┌──────────────┐    claims rows WHERE forwarder_name='qrz'
│ qrz          │──▶  submits                     ────▶ qso_upload
│ goroutine    │    writes status / attempts back
└──────────────┘

┌──────────────┐    claims rows WHERE forwarder_name='clublog'
│ clublog      │──▶  submits                     ────▶ qso_upload
│ goroutine    │    writes status / attempts back
└──────────────┘
```

**Each goroutine's loop:**

```
for {
    select {
    case <-ctx.Done():
        return
    case <-ticker.C:
    }

    rows := claim_up_to(N, forwarder_name=self, status = 'pending',
                        next_attempt_at <= now)
    for _, row := range rows {
        qso := fetch_qso(row.qso_id)         // soft-deleted handled below
        priorID := ""
        if row.action == "delete" {
            priorID = fetch_insert_upstream_id(row.qso_id, self)
        }
        res := forwarder.Submit(ctx, qso, row.action, priorID)
        persist_outcome(row, res)            // updates status/attempts/last_error;
                                             // on success, also stamps the qso row's
                                             // <AdifPrefix>_QSO_UPLOAD_{STATUS,DATE}
        maybe_emit_sse(row, res)             // only on terminal outcomes
    }
}
```

**Why per-destination goroutines, not a central dispatcher:**

1. **Isolation.** One slow/failing destination can't back up the
   others. QRZ being down doesn't delay ClubLog pushes.
2. **Simplicity.** No shared work queue, no dispatch logic, no
   fairness policy. Each goroutine reads only its own rows via a
   `service = ?` filter.
3. **Natural rate limiting.** Per-destination retry policy is
   enforced by that destination's own loop. No cross-destination
   coordination needed.
4. **Personal-operator scale.** There will be 1–4 destinations. A
   fancy dispatcher saves nothing.

**Why not a goroutine per row / per QSO:** needless concurrency.
Sequential submission within a destination is the correct default —
many upstream APIs rate-limit per account anyway, and parallelism
within a single destination just creates new ways to get throttled.

**Claim semantics.** `claim_up_to` transitions `pending` →
`in_progress` in a single `UPDATE` statement and returns the claimed
rows. This makes the "what happens if the daemon crashes mid-flight"
question tractable: any row stuck in `in_progress` is reset to
`pending` at next startup (see §7).

**Batch size and tick interval.** Both come from per-forwarder
config, with package-level defaults that match the values v1
settled on in operation:

- `tick_interval_sec`, default **120** (2 minutes)
- `batch_size`, default **5**

These are deliberately conservative because the operator's internet
is slow and unreliable. Tight tick cadence or large batches would
concentrate all the network-time variance into a single tick, make
graceful shutdown sluggish (shutdown waits for the current batch to
finish), and hammer upstream services that a flaky link can't keep
up with anyway. A batch of 5 on a flaky connection fits comfortably
inside 120 s of wall time in the worst case.

The tick keeps the loop responsive to `ctx.Done()` — a worker
processing up to 5 rows can shut down within one in-flight network
call of being told to. Any operator with better connectivity can
tune either value down in config.

**Soft-deleted QSOs.** A `qso_upload` row may point at a QSO that has
since been soft-deleted. The worker handles this as follows:

- `action = 'insert'` with a soft-deleted QSO: **skip** — mark the
  row `failed` with `last_error = "qso soft-deleted before insert
  forwarded"`. The operator deleted before we got to the upstream,
  so there's nothing worth pushing.
- `action = 'update'` with a soft-deleted QSO: **skip** — mark the
  row `failed` with `last_error = "qso soft-deleted; delete row
  supersedes"`. `qsoservice.Delete` has already enqueued a
  dedicated `delete` row (for each destination whose `action_filter`
  includes `"delete"`), which is the correct vehicle for the delete
  signal. Rerouting update → delete in-memory was considered and
  rejected as unnecessarily clever.
- `action = 'delete'`: always forward — that's the point.

---

## 5. Retry policy and backoff

**Decision:** Exponential backoff with jitter, per-forwarder, capped.
Every non-success outcome updates `attempts`, `last_attempt_at`,
`next_attempt_at`, and `last_error`. When `attempts >= max_attempts`
on a transient error, the row is promoted to terminal `failed` and an
SSE event fires.

**Backoff formula:**

```
backoff = min(
    initial_backoff_sec * 2^(attempts - 1),
    max_backoff_sec
)
jitter = random_between(0, backoff * 0.2)   // ±20% jitter
next_attempt_at = now + backoff + jitter
```

**Why jitter:** two forwarders recovering from the same outage
otherwise synchronize their retry waves. With jitter they drift
apart. Twenty percent is the standard rule-of-thumb.

**Why exponential, not linear:** upstream services that go down tend
to stay down longer than a few minutes. Linear backoff retries too
aggressively during a multi-hour outage; exponential gives the
upstream time to breathe.

**Why a per-forwarder `max_attempts`, not a global one:** discussed in
§2 — different upstreams tolerate different retry patterns.

**Terminal vs transient at max_attempts.** Once attempts exhausts, the
row transitions to `failed` regardless of the last outcome's
classification. The operator can re-queue it manually (endpoint
deferred — see §11).

**New column: `next_attempt_at INTEGER`.** The schema already has
`last_attempt_at`; deriving `next_attempt_at` on the fly requires
replaying the backoff formula inside every claim query, which is
awkward in SQL. A pre-computed `next_attempt_at` column lets the
claim query be a simple `WHERE next_attempt_at <= ? ORDER BY
next_attempt_at`. This adds one more column to `0001_init.up.sql`
before any QSO data exists — we're pre-milestone-1c, the schema is
not yet frozen.

**M8 lands here.** The milestone 1b review's M8 finding (forwarder
retry cooldown) is this section. The TODO pointer in code becomes
the retry-policy implementation described here.

---

## 6. Queue-row data shape

**The row is the contract between ingest, worker, and client query
endpoints.** It must describe unambiguously: which QSO and destination
this work belongs to, what action is being forwarded, where the
attempt stands, when to try again, and what came back from the
upstream.

### Schema

```
id                INTEGER PK
created_at        default now
modified_at       trigger-maintained
qso_id            FK → qso.id  (ON DELETE CASCADE; soft-delete does not cascade)
forwarder_name    TEXT   -- per-instance handle, matches config.forwarders[].name
forwarder_type    TEXT   -- plugin identifier, matches Forwarder.Type()
action            TEXT   -- 'insert' | 'update' | 'delete'
status            TEXT   -- 'pending' | 'in_progress' | 'uploaded' | 'failed'
attempts          INTEGER default 0
last_attempt_at   INTEGER (unix) — diagnostic only, workers do not read
next_attempt_at   INTEGER (unix) — load-bearing for the claim query (§5)
last_error        TEXT   -- most recent Result.Err message when not success
upstream_id       TEXT   -- optional; set from Result.UpstreamID on success
UNIQUE(qso_id, forwarder_name, action)
```

### Changes from v1

- **`service` split into `forwarder_name` + `forwarder_type`.** v1 had
  one destination and a free-text `service` column was enough. v2's
  multi-destination model needs both a per-instance handle (so
  workers can claim exclusively) and a stable type identifier (so
  rows remain interpretable after an operator renames or removes a
  destination from config). Two columns, ~10 bytes per row, row is
  self-describing.
- **Added `next_attempt_at`.** See §5. Lets the pending index be
  ordered by "when should this next be tried" without replaying the
  backoff formula in SQL.
- **Added `upstream_id`.** Optional. Destinations that return a
  handle (QRZ's log-entry ID, ClubLog's upload ref) get it persisted
  so future UI can link out and future `update` / `delete` calls can
  target the right upstream record.
- **Clarified `last_attempt_at` as diagnostic-only.** v1 used it
  ambiguously; v2 workers read `next_attempt_at` for scheduling and
  use `last_attempt_at` only for operator-facing "last tried at"
  display.

### Row creation: one per (enabled destination × matching action)

At each QSO lifecycle event, `qsoservice` inserts one row per enabled
forwarder whose `action_filter` includes that event's action:

```
-- POST /v1/qso (all three destinations accept 'insert'):
qso_upload(qso_id=42, forwarder_name='qrz',     forwarder_type='qrz',     action='insert', …)
qso_upload(qso_id=42, forwarder_name='clublog', forwarder_type='clublog', action='insert', …)
qso_upload(qso_id=42, forwarder_name='lotw',    forwarder_type='lotw',    action='insert', …)

-- PATCH /v1/qso/42 — lotw's action_filter is ["insert"], so it gets no row:
+ qso_upload(qso_id=42, forwarder_name='qrz',     forwarder_type='qrz',     action='update', …)
+ qso_upload(qso_id=42, forwarder_name='clublog', forwarder_type='clublog', action='update', …)
```

**N is dynamic per action**, not fixed per QSO. A QSO that goes
through insert + edit + delete with three fully-filtered destinations
accumulates at most 9 rows over its lifetime — a small, bounded audit
trail of forwardable events.

### Re-queue semantics under the UNIQUE constraint

`UNIQUE(qso_id, forwarder_name, action)` means at most one row exists
per (QSO, destination, action) triple. This prevents duplicate work
but needs a defined behavior when a new instance of the same action
arrives before the existing row has terminated.

**The rule: reset the existing row, don't insert a new one.** On
PATCH, if an `update` row already exists for a (QSO, destination)
pair:

- If it is `pending` or `in_progress`: bump `attempts=0`,
  `next_attempt_at=now`, clear `last_error`. Worker picks it up on
  the next tick and forwards whatever the current QSO state is.
- If it is `failed` or `uploaded`: reset to `pending` with
  `attempts=0`. Fresh retry cycle starts.

This is the correct ham-radio behavior: the operator's latest edit
is the truth to forward. An in-flight "update from two edits ago"
doesn't need to complete in isolation — its only job was to push
the current state, and there's still a worker whose only job is to
push the current state.

### No snapshot on the row

The row does **not** carry a copy of the QSO body at forward-queue
time. Workers re-read `types.Qso` by `qso_id` at claim time. If the
operator edits between claim and the next tick, the worker submits
the newer state. This:

- Keeps the row lean (row size stays bounded and predictable).
- Matches operator intent (forward the latest truth).
- Sidesteps snapshot-vs-current ambiguity that would otherwise need
  a design decision every time the question comes up.

The only caveat is the soft-delete handling already documented in
§4: an `insert` row whose QSO is soft-deleted before the worker
claimed it is terminal (no point inserting upstream something we've
decided to remove); an `update` row whose QSO is soft-deleted is
re-routed to a `delete` at claim time.

### Migration note

No data had shipped, so the three schema changes above (split
`service`, add `next_attempt_at`, add `upstream_id`) landed as edits
to `0001_init.up.sql` in place rather than as a `0002_*.up.sql`
migration (stage 1, session 11). This matches the pattern used when
the composite index was added in milestone 1b. If the daemon has
ever been started and the schema migrated locally, a
`DROP TABLE qso_upload` before rerun
is enough — no production data exists.

---

## 7. Row lifecycle and status transitions

The existing schema already has the right states:

```
pending → in_progress → uploaded   (success)
pending → in_progress → pending    (transient, backoff)
pending → in_progress → failed     (terminal or max_attempts)
```

**Ingest writes `pending`.** The submit transaction inserts one row
per configured destination with `status='pending'`, `attempts=0`,
`next_attempt_at=now`. This is the only place non-forwarder code
writes `qso_upload`.

**Worker transitions.**

| From | To | When |
|---|---|---|
| `pending` | `in_progress` | Claimed by the worker, before Submit |
| `in_progress` | `uploaded` | Submit returned `OutcomeSuccess` |
| `in_progress` | `pending` | Submit returned `OutcomeTransient` and `attempts < max_attempts` |
| `in_progress` | `failed` | Submit returned `OutcomeTerminal`, OR `OutcomeTransient` with `attempts >= max_attempts` |

**Crash recovery on startup.** The daemon resets any
`status='in_progress'` row back to `status='pending'` during service
initialization (one `UPDATE` per startup). This covers the case where
the daemon crashed between claim and persist-outcome — the row was
"being worked on" but the result never got written, so we treat it
as never-claimed. Duplicates on the upstream are acceptable because
most forwarders are idempotent (QRZ dedupes on CALL+QSO_DATE+TIME_ON
server-side, ClubLog the same). For the few that aren't, the
upstream returns a dedupe error classified as `OutcomeSuccess` with
`last_error` noting the dedupe hit.

**SSE emission points.** Only terminal transitions emit events:

- `in_progress` → `uploaded`: emit `forward.succeeded`.
- `in_progress` → `failed`: emit `forward.failed`.

`pending` → `pending` (backoff) is silent. Clients that want spinner
UX query `GET /v1/qso/:id/uploads` and show a spinner for any row in
`pending` or `in_progress`; the spinner clears when a terminal event
arrives.

**Every terminal transition emits an event**, regardless of how many
attempts were needed. A one-shot success and a five-retry success
both emit `forward.succeeded`. The event payload carries the
`attempts` count, so clients that want to distinguish "succeeded
first try" from "succeeded after retries" can do so without the
daemon having to care. Under-emission (gating on `attempts > 1` or
similar) would force clients to poll to detect first-try successes,
which defeats the purpose of the event stream.

**No `attempts_total` vs `attempts_current`.** The single `attempts`
counter is the count since the row was created. If the operator
manually re-queues a `failed` row (future endpoint), it resets to
zero. v1 overcomplicated this; v2 does not.

---

## 8. Config reload

**Decision:** Config changes require a daemon restart. Live reload
(SIGHUP, admin endpoint, file watching) is **off the table** — not
just for milestone 1c but for the foreseeable future.

**Why:** the restart is cheap (sub-second on a Unix socket with no
persistent connections worth preserving), the operator is the only
user, and live-reload introduces real complexity around the
in-flight-attempts question (drain claimed rows? abandon them?
restart the goroutine mid-batch?). Restarting sidesteps all of it.
If a genuine need appears later, this decision can be revisited —
but it isn't the kind of feature to build against speculative
future demand.

**What restart does:** the startup path walks `config.Forwarders`,
constructs each enabled forwarder, and spawns one worker goroutine
per enabled entry. Disabled forwarders are not constructed and their
queue rows sit at `pending` until the operator re-enables them and
restarts.

---

## 9. Worker-goroutine panic recovery: `safego`

**Decision:** Each worker goroutine runs under a `safego.Go` wrapper
that recovers panics, hands them to a caller-supplied logging
callback, and arranges for the worker to be respawned. This is the
third layer of panic defense after `main`'s recover (session 10) and
the HTTP `recoverPanic` middleware (session 10).

**Location:** `internal/safego/safego.go` — its own package. The
original draft proposed `internal/utils/safego.go`, but
`internal/logging` already imports `internal/utils`, so a
`*logging.Service` parameter in utils would create an import cycle.
Keeping safego in its own package and having it take a callback
(`PanicHandler`) instead of a concrete logger means zero dependency
on logging — callers wire up whatever log format they want.

**Shape (as implemented, stage 5):**

```go
package safego

type PanicHandler func(name string, panicValue any, stack []byte)

// Go runs fn in a new goroutine with panic recovery. If fn panics,
// onPanic is invoked with the recovered value and a stack trace;
// if respawn is true, Go schedules another attempt after
// respawnCooldown. Respawns are cancelled if ctx is Done before the
// cooldown elapses.
func Go(ctx context.Context, name string, onPanic PanicHandler, fn func(), respawn bool)
```

Forwarder workers build their `PanicHandler` once at startup,
binding the logger into a closure:

```go
handler := func(name string, pv any, stack []byte) {
    logger.ErrorWith().
        Str("goroutine", name).
        Interface("panic", pv).
        Bytes("stack", stack).
        Msg("goroutine panic recovered")
}
safego.Go(ctx, "qrz", handler, workerFn, true)
```

**Why ctx is a parameter (deviation from the first draft):** the
original signature was `SafeGo(name, logger, fn, respawn)`. Adding
ctx lets the cooldown sleep be interrupted by shutdown — without it,
a panicking worker might spawn one last goroutine after SIGTERM that
immediately exits. Small quality-of-shutdown improvement, free to
add.

**Why respawn is opt-in, not default:** a respawning panic loop is
worse than a dead worker if the panic is deterministic — you get a
logging storm with no progress. Forwarders use `respawn=true`
because a transient panic (e.g. weird network state) should not
permanently disable uploads. A future panic-loop detector (kill
after N respawns in M seconds) can be added if it becomes a real
problem; it is not in the initial shape.

**Why a named goroutine:** log filtering. When the forwarder
subsystem grows to multiple destinations, `goroutine=qrz` tells you
which one panicked without having to unscramble the stack.

**Testability:** the first test for `safego.Go` is "panic inside fn
does not crash the test process." The second is "respawn=true
re-runs fn." Straightforward unit tests; no goroutine leak concerns
because the respawn chain terminates when `fn` stops panicking or
when ctx is cancelled. The cooldown is an atomic var so tests can
dial it down without racing against in-flight goroutines.

---

## 10. Migration from v1

**What carries forward:**

- `internal/upload/qrz/` HTTP and XML parsing logic. Moved to
  `internal/forwarding/qrz/` and adapted to the `Forwarder`
  interface. Session-key caching, error-code classification, the
  "login-then-request" pattern — all kept.
- The `qso_upload` table shape. Already re-created in the v2 schema
  (`0001_init.up.sql`). Adding the `next_attempt_at` column (§5) is
  the only schema change needed for milestone 1c.
- The v1 retry loop's shape as a reference for the per-destination
  goroutine in §4.

**What changes:**

- The hardcoded `upload.OnlineServiceQRZ` at the ingest site is gone.
  `qsoservice.Submit` reads `config.Forwarders` and inserts one row
  per enabled destination.
- Per-forwarder retry policy (v1 was global).
- Per-destination goroutines (v1 was a single worker).
- `Forwarder` interface at the plugin boundary (v1 had no seam).

**What's deleted:**

- The v1 dispatcher coupling between forwarder and logbook mutation.
  In v2 the forwarder only submits — it does not re-read or re-write
  QSO state beyond the `qso_upload` row.

---

## 11. Explicitly deferred

- **Manual re-queue endpoint** (e.g. `POST /v1/qso/:id/uploads/:name/retry`
  to reset a `failed` row to `pending` with `attempts=0`). Clients
  need it eventually; not required for milestone 1c acceptance.
- **Dead-letter handling for permanently failed rows.** Today they
  sit in `failed` forever. A future cleanup job or admin endpoint
  can prune them; no design pressure yet.
- **Operator-facing upload-queue introspection endpoints.** Listed in
  `api.md §5` as "not designed yet." The client UX for this is
  logbook-app territory and is milestone 2 work.
- **Forwarder-specific rate limiting beyond retry backoff.** If QRZ
  starts 429-ing us during bulk imports, a token-bucket per
  forwarder is the fix. Not a concern at personal-operator steady
  state.
- **Parallelism within a single destination.** §4 is explicit about
  sequential-per-destination being the default. Revisit if a
  measurement shows a real bottleneck.
- **Forwarder health metrics.** The operator can infer health from
  SSE events and the pull endpoint; a dedicated `/v1/forwarders`
  status endpoint is easy to add later if a dashboard needs it.

---

## 12. Acceptance for milestone 1c

This is a re-statement of `milestones.md §1c` with the shape above
plugged in. Per-bullet status reflects the state of `main` at end
of session 11; the thin slice (stages 1–11 in
`docs/session-handoff.md`) delivered the spine, two bullets remain
open pending the real QRZ forwarder and the SSE event stream.

- ✅ `config.json` with forwarders loads and validates.
  `internal/config` parses the `forwarders` array, applies the
  per-entry defaults (`tick_interval_sec=120`, `batch_size=5`,
  `action_filter=["insert","update","delete"]`), and rejects
  duplicate names, unknown actions, and broken retry bounds.
- ✅ A `POST /v1/qso` insert creates one `qso_upload` row per
  enabled forwarder whose `action_filter` includes `"insert"`.
  Covered by `internal/api/handler_forwarders_test.go` and the
  E2E test in `internal/api/handler_e2e_test.go`.
- ⏳ The QRZ worker picks up its row within one tick, calls the
  upstream, and writes either `uploaded` or a transient retry.
  **Pending a real QRZ forwarder port** — `internal/forwarding/qrz/`
  doesn't exist yet; v1's `internal/upload/qrz/` is the source
  material. The worker and queue plumbing are in place, so this
  is a forwarder-package-only piece of work.
- ✅ The stub worker picks up its row within one tick and
  transitions to `uploaded`. Covered end-to-end in
  `TestE2E_InsertReachesUploaded`.
- ✅ `GET /v1/qso/:id/uploads` returns all rows with their current
  status, ordered by `(forwarder_name, action)`. Handler at
  `internal/api/handler_uploads.go`; soft-deleted QSOs still
  surface their rows (the delete-action forward is legitimate
  work). Tests in `handler_uploads_test.go`.
- ⏳ An SSE client on `/v1/events` receives `forward.succeeded` /
  `forward.failed` events as terminal transitions happen.
  **Pending the SSE subsystem** — `GET /v1/events` hasn't been
  built yet. The worker code has comments at the emit sites
  (success/terminal-failure) so wiring the publisher in is
  mechanical once the stream exists.
- ✅ Killing the daemon mid-upload and restarting it does not lose
  the row: `in_progress` is reset to `pending` and the retry cycle
  resumes. Implemented in `cmd/smd/main.go`'s startup sweep
  (calls `ResetOrphanedUploadsWithContext` immediately after
  `Migrate`). DB method has its own test
  (`TestResetOrphanedUploads`), and the E2E test proves the cycle
  then proceeds to `uploaded`.
- ✅ A panic inside the forwarder's `Submit` is caught by
  `safego.Go`, logged through the daemon logger, and the worker
  respawns. `safego` has unit-test coverage of the panic-recovery
  and respawn paths in `internal/safego/safego_test.go`; the
  wire-up to real workers is in `spawnForwarderWorkers` with
  `respawn=true`.

When the two ⏳ bullets land (QRZ port + SSE stream), milestone 1c
closes.

---

## Related documents

- `docs/v2-design/api.md` — client-facing shape of forwarding.
- `docs/v2-design/milestones.md` — milestone 1c scope.
- `docs/v1-analysis/invariants.md` — the constraints §1 summarizes.
- `docs/v1-analysis/design-decisions-log.md` — why v1's QRZ
  hardcoding is being replaced.
- `docs/reviews/milestone-1b-review.md` → M8 — the deferred retry-
  cooldown TODO that §5 resolves.
- `docs/session-handoff.md` — historical parking spot for
  `safego`; the real shape now lives in §9 above and the package
  itself is `internal/safego`.
