# Forwarder Subsystem — Implementation Guide

**Companion to:** [`forwarding.md`](forwarding.md) (the design).
**Scope:** where each decision lives in the code, and how the moving parts
fit together end-to-end.

## 1. Purpose and audience

The forwarder is the daemon's biggest subsystem by a wide margin: ~10 files
across 4 packages, with subtle interactions between transactions, triggers,
goroutines, and a plugin registry. The design doc (`forwarding.md`) answers
*what and why*. This document answers *where in the code and what exactly*,
so that you — or a new joiner returning to the code in six months — can
re-orient without re-reading the whole codebase.

Assumed reading: `CLAUDE.md`, `docs/v1-analysis/invariants.md`, and
`forwarding.md`. This guide does not repeat those; it points to the code
that implements the decisions they describe.

Not covered here: rig control, CAT, capture UX. Those live in clients or
bridges, not the daemon. The "narrow daemon scope" invariant is absolute.

## 2. System map

```
                     ┌──────────────────────────────────────┐
  HTTP client        │          cmd/smd (the daemon)        │
  ──────────────▶    │                                      │
   POST /v1/qso      │  ┌─────────────────────────────────┐ │
   PATCH /v1/qso     │  │ internal/api (HTTP handlers)    │ │
   DELETE /v1/qso    │  └──────────────┬──────────────────┘ │
   GET   /v1/qso/.../uploads           │                    │
                     │                 ▼                    │
                     │  ┌─────────────────────────────────┐ │
                     │  │ internal/qsoservice             │ │
                     │  │  Submit / Update / Delete       │ │
                     │  │  (one-fails-all-fail tx)        │ │
                     │  └──────────────┬──────────────────┘ │
                     │                 │ same tx            │
                     │                 ▼                    │
                     │  ┌─────────────────────────────────┐ │
                     │  │ sqlite: qso + qso_upload rows   │ │
                     │  │  (one row per enabled forwarder)│ │
                     │  └──────────────▲──────────────────┘ │
                     │        claim/mark│                   │
                     │                  │                   │
                     │  ┌───────────────┴─────────────────┐ │
                     │  │ internal/forwarding/worker      │ │
                     │  │  ticker → claim → Submit →      │ │
                     │  │  persist outcome                │ │
                     │  └──────────────┬──────────────────┘ │
                     │                 │ forwarding.Forwarder│
                     │                 ▼                    │
                     │  ┌─────────────────────────────────┐ │
                     │  │ internal/forwarding/stub (today)│ │
                     │  │ internal/forwarding/qrz (later) │ │
                     │  └──────────────┬──────────────────┘ │
                     └─────────────────┼────────────────────┘
                                       ▼
                                upstream HTTP
                                (QRZ, ClubLog, ...)
```

The daemon's goroutine topology:

- `main` — lifecycle coordination.
- one HTTP server goroutine (accept loop inside `http.Server`).
- **one worker goroutine per enabled forwarder**, each wrapped in
  `safego.Go(respawn=true)` so a panic logs + restarts rather than
  silently disabling a destination.

## 3. Anatomy of one QSO submit

Trace an `insert` from curl to `status=uploaded`. File paths and line
numbers are stable as of the end-of-session-11 tree; names are more
stable than numbers.

1. **Client sends `POST /v1/qso?logbook=1`** with an ADIF record in the
   body.

2. **`internal/api` parses and dispatches.** `handleSubmitQso`
   (`handler_qso.go`) reads the body, decodes the ADIF record, validates
   the logbook, and calls `qsoservice.Submit`.

3. **`qsoservice.Submit`** (`internal/qsoservice/submit.go`):
   - Validates required fields, normalises callsign / band / mode / date
     / time, and canonicalises `FREQ` to MHz.
   - Computes the dedupe key
     (`CALL|BAND|MODE|FREQ(kHz)|QSO_DATE|TIME_ON`) and checks for a
     collision pre-transaction.
   - Opens a tx, calls `InsertQsoTx`, and **inside the same tx** iterates
     `s.Config.Forwarders()` calling `InsertQsoUploadTx(...)` for each
     entry that passes `shouldEnqueue(fc, action.Insert)`
     (`forwarders.go`).
   - Commits. Either the QSO row **and** every matching `qso_upload`
     row land together, or neither does. This is the one-fails-all-fail
     invariant in code form.

4. **`qso_upload` rows land in `status='pending'`**, `next_attempt_at`
   defaulting to `now()` via the column default in
   `migrations/0001_init.up.sql`.

5. **A worker goroutine picks them up.** Each forwarder has its own
   `worker.Worker` running under `safego.Go`. On its next tick
   (default 120 s; 50 ms in E2E tests) it calls
   `ClaimPendingUploadsWithContext(forwarderName, batchSize)`
   — a single `UPDATE qso_upload SET status='in_progress' ... RETURNING *`
   scoped to this forwarder.

6. **For each claimed row, `processRow`** (`worker.go`):
   - Calls `FetchQsoByIdWithContext(row.QsoID)`.
   - Parses `row.Action`.
   - For `delete` rows, looks up the insert row's `upstream_id` (the
     upstream's record id, captured on the earlier successful insert)
     and passes it to Submit. Empty for insert/update.
   - Invokes `w.fwd.Submit(ctx, qso, act, priorUpstreamID)` — one
     synchronous network call to the upstream.
   - Dispatches on `Result.Outcome` → `markSuccess` / `markFailed` /
     `markTransientFromForwarder`. On success, if the forwarder's
     `AdifPrefix() != ""`, the worker also stamps
     `<PREFIX>_QSO_UPLOAD_STATUS="Y"` +
     `<PREFIX>_QSO_UPLOAD_DATE=today` on the QSO row in the same tx
     as the `qso_upload` row update.

7. **`markSuccess`** dispatches based on the forwarder's declarative
   metadata and the row's action:
   - `AdifPrefix() == ""` or `action == Delete` →
     `MarkUploadSuccessWithContext`. Loads the `qso_upload` row,
     sets `status='uploaded'`, bumps `attempts`, writes
     `upstream_id`, clears `last_error`. No QSO-row changes.
   - Otherwise → `MarkUploadSuccessWithAdifStampWithContext`.
     Single transaction: the same `qso_upload` transition **plus**
     `UPDATE qso SET additional_data = json_set(additional_data,
     '$.<prefix>_qso_upload_status', 'Y',
     '$.<prefix>_qso_upload_date', today)`. The stamp lives in the
     JSON blob (the project's "additional_data absorbs ADIF spec
     evolution" idiom) — no per-destination columns, no migration
     when a new forwarder lands.
   In both cases, `trg_qso_upload_set_updated_at` fires and stamps
   `qso_upload.modified_at`.

8. **Client polls `GET /v1/qso/1/uploads`** any time after step 3.
   `handleListQsoUploads` probes existence with
   `FetchQsoByIDIncludingDeletedWithContext` (so a soft-deleted QSO
   still returns its rows), reads
   `FetchUploadsByQsoIDWithContext(id)`, and returns
   `{"items": [...]}`.

The path for `Update` is identical except the ingest site is
`qsoservice.Update` and the action is `action.Update`. `Delete` uses
`qsoservice.Delete`, which soft-deletes the QSO *and* enqueues
`delete`-action rows in one tx.

## 4. The pieces in detail

### 4.1 `types.ForwarderConfig` and `types.RetryConfig`

`internal/types/forwarder.go`. The daemon-facing description of one
forwarding destination, loaded from `config.json`. Fields:

- `Name` — per-instance handle the operator picks (e.g. `"qrz-primary"`).
- `Type` — plugin identifier (e.g. `"qrz"`). Must match a registered
  constructor.
- `Enabled` — the startup switch.
- `Credentials` — `json.RawMessage`, opaque here; each forwarder package
  parses it with its own schema.
- `ActionFilter` — subset of `["insert", "update", "delete"]`.
- `TickIntervalSec`, `BatchSize` — defaulted to 120 / 5 in config
  normalisation, deliberately conservative for slow operator links.
- `Retry *RetryConfig` — optional override. When nil, the daemon
  uses the per-forwarder `DefaultRetry` registered by that
  forwarder's package via `forwarding.RegisterDefaultRetry` at
  init-time (see §5 for the resolution chain).

The `name`/`type` split exists for rename safety — historical
`qso_upload` rows stay interpretable if an operator renames an instance.
Not for multi-instance use; ham services are singleton per operator.

### 4.2 The `Forwarder` interface and registry

`internal/forwarding/forwarding.go` and `registry.go`.

```go
type Forwarder interface {
    Type() string
    AdifPrefix() string
    Submit(ctx context.Context, qso types.Qso, action Action, priorUpstreamID string) Result
}

type Result struct {
    Outcome    Outcome // success | transient | terminal
    Err        error   // populated when Outcome != success
    UpstreamID string  // optional; only used on success (QRZ: the LOGID)
}
```

Rules the implementation must follow:

- **One synchronous network call per `Submit`.** No internal retry — that
  is the worker's job.
- **Respect ctx cancellation.** Shutdown and per-request deadlines
  propagate in. The stub demonstrates the shape: bail with a transient
  `Result` before bumping any counters.
- **Classify the outcome.** Only the forwarder knows whether "upstream
  returned HTTP 429" means "try again later" (transient) or "stop"
  (terminal). This is the whole reason `Outcome` is the forwarder's
  output rather than the worker's inference.
- **Use `priorUpstreamID` for delete actions only if you need it.**
  The worker populates it from the earlier successful insert's
  `UpstreamID`. Forwarders without a delete API (no ADIF slot) ignore
  it; QRZ uses it as `LOGIDS`.
- **Return an `AdifPrefix`** if you own an ADIF upload-status field
  pair (`<PREFIX>_QSO_UPLOAD_STATUS` / `<PREFIX>_QSO_UPLOAD_DATE`).
  The worker stamps those on the QSO row on success. Return `""` if
  you don't have a slot — stub, custom webhooks, SM-internal
  destinations. The prefix is a declarative string only; the
  forwarder never touches the QSO row itself.

Registration is init-time and panics on misuse (empty type, nil
constructor, duplicate). The daemon reads the forwarder registry at
startup by blank-importing each package:

```go
import _ "github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
```

`forwarding.Build(fc)` looks up the constructor by `fc.Type` and
returns a concrete `Forwarder` or an error.

### 4.3 The `qso_upload` table

Defined in `internal/database/sqlite/migrations/0001_init.up.sql` ≈
line 150. Columns worth knowing about:

| Column            | Notes                                                 |
|-------------------|-------------------------------------------------------|
| `qso_id`          | FK to `qso(id)`, `ON DELETE CASCADE`.                 |
| `forwarder_name`  | Matches `ForwarderConfig.Name`. Row belongs to this worker only. |
| `forwarder_type`  | Matches `Forwarder.Type()`. Rename-safe diagnostics.  |
| `action`          | `CHECK IN ('insert', 'update', 'delete')`.            |
| `status`          | `CHECK IN ('pending', 'in_progress', 'uploaded', 'failed')`. |
| `attempts`        | Incremented on every terminal transition.             |
| `last_attempt_at` | Unix time; diagnostic only.                           |
| `next_attempt_at` | Unix time; **load-bearing**. Claim filter uses it.    |
| `last_error`      | Short text. Cleared on `uploaded`.                    |
| `upstream_id`     | Stored NULL when the forwarder returns `""`.          |

Constraints:

- `UNIQUE (qso_id, forwarder_name, action)` — "one pending row per
  (QSO, forwarder, action) triple" is enforced at the schema. Repeated
  ingest of the same lifecycle action is impossible to queue twice.
- `FOREIGN KEY (qso_id) REFERENCES qso (id) ON DELETE CASCADE` — hard
  deletion of a QSO (future admin op) drops its queue rows.

Indexes:

- `idx_qso_upload_pending` — partial on `(forwarder_name, next_attempt_at)`
  `WHERE status IN ('pending','in_progress')`. This is the claim path's
  index. Small because `uploaded`/`failed` rows are excluded.
- `idx_qso_upload_uploaded` — partial on `(forwarder_name, modified_at)`
  `WHERE status='uploaded'`. For future operator-facing "recent uploads"
  views, not on the hot path.

Trigger:

- `trg_qso_upload_set_updated_at` — after any UPDATE, sets
  `modified_at = now()`. **Workers never set `modified_at` manually.**
  Relies on sqlite's default `recursive_triggers=off` so the trigger's
  own UPDATE doesn't re-fire.

### 4.4 Ingest fan-out

The three domain entry points that produce `qso_upload` rows:

- `qsoservice.Submit` — `internal/qsoservice/submit.go`. Action =
  `insert`.
- `qsoservice.Update` — `internal/qsoservice/update.go`. Action =
  `update`.
- `qsoservice.Delete` — `internal/qsoservice/delete.go`. Action =
  `delete`. This is the only domain method that soft-deletes a QSO;
  the handler layer never calls `DeleteQsoByIDTx` directly.

All three use the same loop shape:

```go
for _, fwd := range s.Config.Forwarders() {
    if !shouldEnqueue(fwd, action.Insert) {
        continue
    }
    if err = s.DB.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, fwd.Name, fwd.Type); err != nil {
        _ = tx.Rollback()
        return ...
    }
}
```

`shouldEnqueue` is defined once in `forwarders.go`:

```go
func shouldEnqueue(fc types.ForwarderConfig, act action.Action) bool {
    if !fc.Enabled {
        return false
    }
    return slices.Contains(fc.ActionFilter, act.String())
}
```

Disabled forwarders and those whose `action_filter` excludes this action
get skipped. Zero configured forwarders ⇒ the loop is a no-op and only
the QSO row is committed.

### 4.5 The worker loop

`internal/forwarding/worker/worker.go`. One `Worker` per enabled
forwarder, each owning its destination's rows exclusively (scoped by
`forwarder_name`).

`Run(ctx)` runs an initial tick immediately — so rows already pending at
startup are picked up without waiting a full tick period — then selects
on a `time.Ticker` until ctx cancels.

`tickOnce`:

1. `ClaimPendingUploadsWithContext(forwarderName, batch)` — atomic
   `UPDATE ... RETURNING *`. Rows transition `pending → in_progress`.
2. For each row, `processRow`.
3. Ctx-cancellation errors from claim are silently suppressed — shutdown
   is noise, not an operational failure.

`processRow`:

1. `fetchQsoForAction` — the soft-delete-aware QSO fetch. Usually
   returns the row from `FetchQsoByIdWithContext`; for `delete` rows
   with a soft-deleted QSO, falls back to
   `FetchQsoByIDIncludingDeletedWithContext` so the upstream still gets
   the CALL/DATE/TIME it needs. For `insert`/`update` on a
   soft-deleted QSO, marks the row `failed` immediately (the delete
   row supersedes).
2. Parse `row.Action`. Unknown action ⇒ `markFailed` (bad data on disk,
   retrying won't help).
3. For `delete` action, look up the prior insert row's `upstream_id`
   via `FetchInsertUpstreamIDWithContext(qsoID, forwarderName)`. If
   empty (no prior insert row, or the insert row exists but its
   `upstream_id` is blank — forwarder never returned one), mark the
   delete row `failed` with "no upstream id for delete". For
   insert/update the lookup is skipped and `priorUpstreamID` is "".
4. `w.fwd.Submit(ctx, qso, act, priorUpstreamID)` — the real work.
5. `persistOutcome` switches on `res.Outcome`:
   - `OutcomeSuccess` → `markSuccess(row, res.UpstreamID)`; worker
     also stamps the QSO row's ADIF upload-status field pair when
     `w.fwd.AdifPrefix() != ""` and `row.Action != "delete"` (a
     soft-deleted QSO won't export, so stamping it is pointless).
   - `OutcomeTerminal` → `markFailed(row, errText(res.Err))`.
   - `OutcomeTransient` → `markTransientFromForwarder` — bumps a
     retry slot if there's budget, otherwise `markFailed`.

DB-layer fetch failures (not ctx-cancel) become
`markTransientInternal`: treated as transient so the row is re-queued
rather than stranded `in_progress`, and budgeted against the same
retry limit so a chronic internal problem doesn't spin forever.

### 4.6 Retry policy and backoff

`internal/forwarding/worker/backoff.go`. Formula — §5 of the design
doc:

```
backoff = min(initial * 2^(attempt-1), max)
jitter  = uniform_random in [0, backoff * 0.2)
delay   = backoff + jitter
```

`attempt` is the **post-increment** `attempts` value, so the first
retry uses `attempt=1` and the exponent is zero (raw initial +
jitter). `maxBackoffShift = 30` caps the bit-shift against overflow.
Jitter uses `math/rand/v2.Int64N` with a floor of 1 ns so the PRNG
doesn't panic on tiny initial values.

Budget enforcement is in `markTransientFromForwarder` /
`markTransientInternal`:

```go
nextAttempts := row.Attempts + 1
if nextAttempts >= int64(w.cfg.Retry.MaxAttempts) {
    w.markFailed(ctx, row, errText(cause))
    return
}
delay := computeBackoff(nextAttempts, w.cfg.Retry)
nextAt := time.Now().Add(delay).Unix()
w.markTransientRetry(ctx, row, nextAt, errText(cause))
```

Retry defaults live with each forwarder package as a `DefaultRetry`
var, registered in that package's `init()` via
`forwarding.RegisterDefaultRetry(Type, DefaultRetry)`. For
example:

- `internal/forwarding/qrz/qrz.go` exports
  `DefaultRetry = {MaxAttempts: 5, InitialBackoffSec: 60, MaxBackoffSec: 1800}` —
  tuned for QRZ's web API tolerances on a slow operator link.
- `internal/forwarding/stub/stub.go` exports
  `DefaultRetry = {MaxAttempts: 3, InitialBackoffSec: 1, MaxBackoffSec: 5}` —
  tight values so stub-backed tests don't linger.

`cmd/smd/main.go`'s `spawnForwarderWorkers` resolves retry config
per instance: operator's `fc.Retry` wins if non-nil, otherwise
`forwarding.DefaultRetryFor(fc.Type)` supplies the package default.
A type with neither is a setup error and startup fails loudly with
a clear message naming the forwarder instance and type. See §8.1
for the full recipe.

### 4.7 Soft-delete handling per action

This is the subtle part. A `qso_upload` row can outlive the QSO it
points at — the operator deletes the QSO, the delete row is enqueued,
but the insert/update rows from earlier may still be pending.

The rules (`fetchQsoForAction` in `worker.go`):

| Row action | QSO absent/soft-deleted                 | Outcome                         |
|------------|-----------------------------------------|---------------------------------|
| `insert`   | Mark `failed`                           | "soft-deleted before insert forwarded" |
| `update`   | Mark `failed`                           | "delete row supersedes"         |
| `delete`   | Fetch *including* soft-deleted          | Forward the delete upstream     |

The delete case is why `FetchQsoByIDIncludingDeletedWithContext` exists
(`api_context.go` ≈ line 316): sqlboiler's generated `FindQso` and
`models.Qsos(...)` bake `WHERE deleted_at IS NULL` into every query, so
the worker needs an explicit way to see the row. The method uses
`models.NewQuery` + `qm` mods to build the SELECT from scratch —
sqlboiler's re-exported query builder at the models package level —
and stays type-safe via generated column constants. See §9 on
`models.NewQuery` vs `FindQso`.

### 4.8 Panic recovery via `safego`

`internal/safego/safego.go`. Wraps `go` with a recovered panic handler
and optional respawn:

```go
safego.Go(ctx, fc.Name, panicHandler, func() {
    workerRef.Run(ctx)
}, true)
```

Design choices that matter:

- **Callback, not a logger.** `PanicHandler` is a func; `safego` has
  no dependency on `internal/logging`. (The original plan had `safego`
  in `internal/utils`, but `logging` already imports `utils` — hence
  the cycle-avoidance.)
- **Respawn cooldown is ctx-aware.** The sleep between a panic and a
  respawn attempt is interrupted by `ctx.Done()`, so shutdown doesn't
  get held up by a deterministic panic loop.
- **Cooldown is `atomic.Int64`.** Tests dial it down from one goroutine
  while another reads it — needed for the race detector.
- **`respawn=true` requires restart-safe `fn`.** Worker loops drive off
  the DB, not local state, so they're restart-safe by construction.
  The orphan sweep at startup covers the rare case where a panic
  happens mid-`processRow`: any `in_progress` row left behind is reset
  to `pending`.

## 5. Lifecycle operations

### 5.1 Startup sequence

`cmd/smd/main.go: run()`:

1. Load config, build the DI container, resolve services.
2. `dbSvc.Open()` and `dbSvc.Migrate()`.
3. **Orphan sweep.** `ResetOrphanedUploadsWithContext(ctx)` flips any
   `in_progress` rows back to `pending`. Either the previous daemon
   crashed mid-`processRow` or shutdown didn't wait for a worker
   finishing a submit. Either way, resetting is safe: upstreams that
   dedupe server-side won't get confused, and forwarders for ones that
   don't should classify the resulting dedupe response as success —
   see the design doc §7 for the rationale.
4. `workerCtx, workerCancel := context.WithCancel(...)` and a deferred
   `workerCancel()`.
5. `spawnForwarderWorkers(workerCtx, cfg.Forwarders, dbSvc, loggerSvc)`:
   - Skip disabled entries (and log a line so the operator sees them).
   - `forwarding.Build(fc)` per remaining entry.
   - Resolve retry config (operator override or fallback).
   - `worker.New(cfg, fwd, dbSvc, loggerSvc)` — validates config.
   - `safego.Go(workerCtx, fc.Name, panicHandler, w.Run, true)`.
6. `server.ListenAndServe(...)` in its own goroutine.
7. Wait for SIGINT/SIGTERM or a server error.

### 5.2 Graceful shutdown

Order matters:

```go
workerCancel()                 // 1. stop workers first
ctx, cancel := context.WithTimeout(...)
defer cancel()
server.Shutdown(ctx)           // 2. then stop the HTTP server
// deferred dbSvc.Close()      // 3. then close the DB
// deferred loggerSvc.Close()  // 4. then the logger (so earlier defers can still log)
```

Rationale: the worker may be mid-`Submit` against an upstream HTTP
endpoint when shutdown arrives. Cancelling its context first aborts
that request promptly (well-implemented forwarders respect ctx), and
stops the worker from starting new DB ops against a handle that's
about to close. The HTTP server shuts down next so in-flight API
requests can drain. The DB closes last on the defer chain.

### 5.3 What a crash looks like

If the daemon is killed hard (SIGKILL, OOM, panic that escapes
`safego`'s recovery):

- Any in-flight `Submit` is abandoned. Whether the upstream saw it
  depends on timing; the row is left in `status=in_progress`.
- On restart, the orphan sweep resets those rows to `pending`.
- On the next tick, they are re-claimed. `attempts` is **not** reset
  — if the upstream did succeed silently, the eventual dedupe response
  keeps it in that count.

Operator-visible diagnostic for a stuck row: `attempts` is high,
`last_error` names the upstream's dedupe message, and `modified_at`
is recent.

## 6. Observability

### 6.1 The pull endpoint

`GET /v1/qso/{id}/uploads` — `internal/api/handler_uploads.go`.

Envelope:

```json
{
  "items": [
    {
      "id": 42,
      "qso_id": 1,
      "forwarder_name": "clublog",
      "forwarder_type": "clublog",
      "action": "insert",
      "status": "uploaded",
      "attempts": 1,
      "last_attempt_at": 1713456789,
      "next_attempt_at": 1713456789,
      "last_error": "",
      "upstream_id": "stub-ok",
      "created_at": "2026-04-18T12:00:00Z",
      "modified_at": "2026-04-18T12:00:03Z"
    }
  ]
}
```

Rules:

- 400 on malformed id.
- 404 **only** for genuinely unknown ids. Soft-deleted QSOs still
  return their rows because the delete-action forwarding work is
  legitimate and must remain observable.
- Nil-to-empty normalisation: the DB returns a zero-length slice when
  there are no rows; the handler forces `[]` so clients never get
  `"items": null`.
- Rows are ordered by `(forwarder_name, action)` from the DB layer.

### 6.2 Log lines to grep for

All through `logging.Service` (zerolog under the hood):

| Message                                              | Where                              | When                               |
|------------------------------------------------------|------------------------------------|------------------------------------|
| `"forwarder: orphaned in_progress rows reset to pending"` | `main.go`                       | Startup, only if sweep found rows  |
| `"forwarder worker started"`                         | `main.go`                          | Per enabled forwarder              |
| `"forwarder disabled, skipping"`                     | `main.go`                          | Per disabled forwarder             |
| `"forwarder: rows claimed"` (DEBUG)                  | `worker.go:tickOnce`               | Per tick with `count > 0`          |
| `"forwarder: claim failed"`                          | `worker.go:tickOnce`               | Non-ctx claim error                |
| `"forwarder: mark {success,transient retry,failed} failed"` | `worker.go:mark*`           | DB write failure mid-processRow    |
| `"forwarder worker panic recovered"`                 | `main.go:panicHandler`             | Panic escaped `processRow`         |
| `"QSO stored" / "QSO updated" / "QSO soft-deleted"` | `qsoservice/*.go`                  | Ingest success                     |

Structured fields always include `forwarder`, `upload_id`, `qso_id`
where relevant — filter on those rather than message-body substring
matches.

## 7. Testing layers

The test surface is layered deliberately: each layer proves a specific
set of properties without the noise of the layers below it.

### 7.1 Unit — `internal/forwarding/worker/backoff_test.go`

Pure-function tests of `computeBackoff`: base/growth, attempt
clamping, zero initial, no overflow at pathological attempts values.
Nothing else the worker does is pure, so this is the only unit-level
file.

### 7.2 Registry — `internal/forwarding/registry_test.go`

The init-time contract: register panics on empty/nil/duplicate; build
returns a useful error on unknown types; `IsRegistered` reports what
you'd expect.

### 7.3 Stub — `internal/forwarding/stub/stub_test.go`

Every mode, ctx-cancel short-circuit, and round-trip via
`forwarding.Build`. The stub is infrastructure for other layers'
tests, so its own test file is thorough.

### 7.4 Worker integration — `internal/forwarding/worker/worker_test.go`

Real `&sqlite.Service{}` with an in-memory database, real stub
forwarder, real `Worker.Run` driven by a test helper. Covers:

- Happy path: insert reaches `uploaded`, `attempts=1`, `upstream_id`
  populated.
- Scoping: worker A does not touch worker B's rows.
- Transient retry schedules `next_attempt_at` in the future.
- Terminal marks `failed` immediately.
- Retry budget exhaustion → `failed`.
- Soft-deleted QSO × each action type.
- `next_attempt_at` in the future is not claimed early.
- Flap-then-succeed.
- `Run` returns on ctx cancel.

Test helpers: `runUntil(t, w, h, qsoID, match)` polls at 10 ms until a
predicate holds; `runFor(t, w, duration)` runs for a bounded time for
negative assertions. Both replaced an earlier fixed-sleep helper that
flaked under `-race` load.

### 7.5 Handler integration — `internal/api/handler_uploads_test.go`

Handler wired to real sqlite, forwarders configured but **no workers
running**. Proves the handler's own logic: envelope shape, 404/400
codes, nil-to-empty normalisation, soft-deleted-QSO-still-returns-rows.

### 7.6 E2E — `internal/api/handler_e2e_test.go`

The whole spine end-to-end: real HTTP handlers, real `qsoservice`,
real sqlite, real `worker.Worker` goroutines. The tests use a 50 ms
tick so convergence takes tens of milliseconds instead of the 120 s
production default.

Three scenarios: insert, update (after settle), delete (after settle).
Each asserts the final `uploaded` state and the specific `attempts` /
`upstream_id` values the stub forwarder produces. The delete scenario
additionally asserts that `GET /v1/qso/{id}` 404s while the uploads
endpoint still returns the rows.

Shutdown in E2E tests uses plain `go` + `sync.WaitGroup` rather than
`safego.Go` — `safego`'s respawn behaviour is tested in its own
package; the E2E tests want deterministic "all goroutines stopped
before `dbSvc.Close`" semantics.

## 8. How to extend

### 8.1 Adding a real forwarder (recipe for QRZ)

The QRZ forwarder lives at `internal/forwarding/qrz/`. The shape
below reflects the as-built structure after stages 2–5.

1. **Type + credentials** (`qrz.go`):
   ```go
   const Type            = "qrz"
   const AdifFieldPrefix = "QRZCOM"

   type credentials struct {
       APIKey string `json:"api_key"`
   }
   ```
   Only `api_key` is required — it both authenticates the caller
   and selects the logbook. The callsign match (QRZ rejects a QSO
   whose `STATION_CALLSIGN` doesn't match the logbook's callsign) is
   enforced server-side; keeping a local copy of the callsign would
   only introduce drift risk without adding a guarantee.
2. **Interface implementation** (`qrz.go`, stage 4):
   ```go
   const DefaultEndpoint    = "https://logbook.qrz.com/api"
   const DefaultHTTPTimeout = 30 * time.Second
   var UserAgent = "station-manager/dev" // overridden from cmd/smd/main.go in stage 8

   func New(fc types.ForwarderConfig) (forwarding.Forwarder, error) { ... }
   func (f *Forwarder) Type() string       { return Type }
   func (f *Forwarder) AdifPrefix() string { return AdifFieldPrefix }
   func (f *Forwarder) Submit(ctx context.Context, qso types.Qso, act forwarding.Action, priorUpstreamID string) forwarding.Result {
       // ctx.Err → Transient; buildForm → POST to endpoint; classifyHTTPStatus
       // (408/429/5xx → Transient, other non-2xx → Terminal); 2xx → parseResponse
       // + classifyResponse(act, parsed).
   }

   // Package-internal constructor used by tests to swap in httptest.NewServer.URL.
   // Production code goes through New, which wraps this with DefaultEndpoint.
   func newWithEndpoint(apiKey, endpoint string, client *http.Client) *Forwarder
   ```
   `buildForm` assembles the form body per action: insert =
   `ACTION=INSERT + ADIF`; update = `ACTION=INSERT + OPTION=REPLACE +
   ADIF`; delete = `ACTION=DELETE + LOGIDS=priorUpstreamID`.

   For `action=delete` the worker resolves `priorUpstreamID` from a
   prior successful insert's `upstream_id` before calling Submit:

   ```go
   // worker.resolvePriorUpstreamID (abbrev):
   //   if act != Delete                      → ("", not-handled)
   //   upstreamID, err := FetchInsertUpstreamIDWithContext(...)
   //   if err != nil                         → markTransientInternal
   //   if upstreamID == ""                   → markFailed
   //                                           "no upstream id for delete
   //                                            — no successful insert found"
   //   return (upstreamID, not-handled)
   ```

   Empty `priorUpstreamID` reaching `buildForm` is a caller bug
   (the worker should have short-circuited) — surfaced as Terminal
   with no HTTP fired.
3. **Response parser + classifier** (`response.go`, stage 3):
   - `parseResponse(body []byte) (response, error)` — splits the
     application/x-www-form-urlencoded body into its fields
     (`RESULT`, `REASON`, `LOGID`, `COUNT`, and anything else QRZ
     returns). Empty body or missing `RESULT` is an error.
   - `classifyResponse(act, resp) forwarding.Result` — pure function
     mapping a parsed response to an outcome. The per-action matrix
     is the shape the forwarder's tests target:

   | QRZ `RESULT` | insert | update (INSERT+REPLACE) | delete |
   |---|---|---|---|
   | `OK`       | Success (LOGID required)   | Success (LOGID if present)    | Success              |
   | `REPLACE`  | Success (LOGID required)   | Success (LOGID required)      | Terminal (undocumented) |
   | `FAIL`     | Terminal                   | Terminal                      | **Success** (idempotent — row gone upstream) |
   | `PARTIAL`  | Terminal (undocumented)    | Terminal (undocumented)       | Terminal (shouldn't occur with single LOGID) |
   | `AUTH`     | Terminal (global — api_key rejected across all actions)                             ||| 
   | other      | Terminal                   | Terminal                      | Terminal             |

   Transport-level classification (network errors, HTTP 4xx/5xx,
   ctx cancellation → Transient) is handled at the `Submit` call
   site in stage 4, not inside `classifyResponse`.
4. **Registration** (`qrz.go`, stage 2 + stage 7):
   ```go
   var DefaultRetry = types.RetryConfig{
       MaxAttempts: 5, InitialBackoffSec: 60, MaxBackoffSec: 1800,
   }
   func init() {
       forwarding.Register(Type, New)
       forwarding.RegisterDefaultRetry(Type, DefaultRetry)
   }
   ```
   Retry values are upstream-specific — tune them to the API's
   tolerances (respect rate limits, avoid hammering slow batch
   services). `RegisterDefaultRetry` validates the shape and
   panics on `MaxAttempts < 1`, `InitialBackoffSec < 1`, or
   `MaxBackoffSec < InitialBackoffSec` — same guard
   `worker.New` applies, so bad defaults never survive to
   spawn.
5. **Blank-import** from `cmd/smd/main.go` (stage 8):
   `_ "github.com/ColonelBlimp/station-manager/internal/forwarding/qrz"`.
   `main.go` resolves retry config per instance:
   `fc.Retry` (operator override) → `forwarding.DefaultRetryFor(fc.Type)`
   (package default) → startup error. No per-type switch in
   `main.go`; adding a new forwarder requires zero changes there
   (beyond the blank-import).
7. **Tests**: three layers.
   - **Unit** (`response_test.go`): pure-function tests for
     `parseResponse` + `classifyResponse`. No network.
   - **HTTP** (`submit_test.go`): `httptest.NewServer` fixtures
     per RESULT class via the `newWithEndpoint` hook. Transport
     classification (network errors, 408/429/5xx, other non-2xx),
     body classification (OK/FAIL/AUTH/REPLACE), malformed bodies,
     request-shape assertions (method, form fields, User-Agent).
   - **Live manual** (`live_test.go`, `//go:build manual`): talks
     to real QRZ, gated behind `QRZ_TEST_API_KEY` +
     `QRZ_TEST_CALLSIGN` env vars and the `manual` build tag so it
     never runs in `go test ./...`. Two modes:
     - `TestLive_InsertThenUpdate` — quick round-trip with
       `t.Cleanup` delete. `task test:qrz-live`.
     - `TestLive_InteractiveFlow` — insert → pause → update →
       pause → delete, with `[Enter]` prompts between steps so the
       operator can inspect the record on QRZ.com. Opens
       `/dev/tty` directly because `go test` feeds the test
       binary a closed stdin. `task test:qrz-live-interactive`.
   Do **not** add the live tests to CI — the operator's logbook
   is not a shared fixture.

The pattern is specifically designed to be mechanical after the
first implementation. The plumbing above doesn't need to change for
subsequent forwarders (ClubLog, LoTW, …): swap `Type`,
`AdifFieldPrefix`, credentials schema, and the classifier matrix.

### 8.2 SSE emit points

`forwarding.md` §7 names the three terminal transitions as SSE emit
sites:

- `in_progress → uploaded` → `forward.succeeded`
- `in_progress → failed` (terminal outcome) → `forward.failed`
- `in_progress → failed` (retry exhausted) → `forward.failed`

In `worker.go`, the sites are `markSuccess` and `markFailed`. Today
these just log; the SSE work adds a publish call after each successful
DB write:

```go
if err := w.db.MarkUploadSuccessWithContext(...); err == nil {
    w.events.Publish(EventForwardSucceeded{ ... })
}
```

The `qsoservice` layer's `qso.stored` / `qso.updated` / `qso.deleted`
events emit after the tx commits in each of `Submit`/`Update`/`Delete`.

## 9. Gotchas and invariants

### 9.1 Transaction boundaries

`qso` insert/update/delete and all matching `qso_upload` rows must
share a single tx. This is **the** one-fails-all-fail invariant. If
you find yourself calling `InsertQsoUploadWithContext` (non-Tx) from
the ingest path, it is a bug. The worker layer is the only legitimate
non-Tx caller of the queue methods.

### 9.2 `models.NewQuery` vs `FindQso`

sqlboiler's `FindQso(ctx, h, id)` and `models.Qsos(qm.Where(...))`
auto-apply `WHERE deleted_at IS NULL`. This is usually what you want.
For the worker's `delete`-action path and the uploads handler's
existence probe, it is not — they need soft-deleted rows.

Use `models.NewQuery(...).Bind(ctx, h, &model)` with explicit
`qm.Select/From/Where/Limit` for those cases. Column and table names
still come from the generated `models.TableNames` / `models.QsoColumns`
constants, so the call stays refactor-safe. See
`FetchQsoByIDIncludingDeletedWithContext` in `api_context.go` for the
pattern.

### 9.3 Soft-delete × delete-action interaction

`qsoservice.Delete` soft-deletes the QSO **and** enqueues delete rows
in one tx. The worker then needs the QSO body (CALL, DATE, TIME) to
tell the upstream which record to remove. If the worker's QSO fetch
applied the default soft-delete filter, every delete-action row would
loop on "not found" and exhaust its retry budget. This is why the
`IncludingDeleted` fallback exists and why it is specifically scoped
to `row.Action == "delete"`.

### 9.4 `priorUpstreamID` is only for delete

The worker populates `priorUpstreamID` from the prior insert row's
`upstream_id` **only for `action=delete`**. For insert/update it is
always `""`. A forwarder that tries to use it on insert or update is
relying on empty-string behaviour and will break silently if that
ever changes. Gate usage in the forwarder on `act == Delete`.

If a delete row's prior insert has no `upstream_id` (blank — the
forwarder never returned one), the worker marks the delete row
`failed` before calling `Submit`. Forwarders that issue deletes
without an upstream id (hypothetically, via CALL+DATE+TIME) would
need a different shape — today no forwarder works that way.

### 9.5 `AdifPrefix` is declarative, not a hook

`AdifPrefix()` returns a string. The **worker** uses it to decide
which QSO-row columns to stamp. The forwarder must not touch the
QSO row itself — that breaks the plugin boundary and the
one-tx-per-success guarantee. If you find yourself calling
`UpdateQsoTx` from a forwarder package, you have the wrong shape.

### 9.6 Trigger-driven `modified_at`

Never write `modified_at` from application code on `qso_upload`. The
`trg_qso_upload_set_updated_at` trigger does it after every UPDATE.
Writing it manually is harmless but misleading (the trigger overwrites
anyway) and obscures the intent. SQLite's default
`recursive_triggers=off` keeps the trigger's own UPDATE from re-firing
the trigger; don't turn that setting on without thinking through the
consequences.

### 9.7 Forwarder must not retry internally

A `Forwarder.Submit` that loops on its own transient errors breaks the
worker's retry accounting. `attempts` would reflect "worker-visible
attempts" instead of "upstream attempts", and the cooldown between
retries would be whatever the forwarder's internal loop does — not the
exponential backoff the design specifies. If an upstream SDK retries
by default, disable that behaviour in the SDK config.

### 9.8 Ctx cancellation semantics in `ClaimPendingUploadsWithContext`

When the daemon shuts down, the claim query's ctx is cancelled mid-
flight. The driver returns a `context canceled` error. `tickOnce`
filters this case out (`if ctx.Err() == nil`) so shutdown doesn't
produce spurious ERROR logs. If you add new claim-style queries,
follow the same pattern.

### 9.9 Worker panic does not leave rows wedged

A panic in `processRow` exits the worker goroutine with rows stuck
`in_progress`. `safego.Go(respawn=true)` brings the worker back; the
startup orphan sweep is the safety net for the case where the whole
daemon crashed instead of just the goroutine. Either way, rows get
reclaimed on the next tick.

### 9.10 `uq_qso_forwarder_action` UNIQUE constraint

Prevents double-queuing the same (QSO, forwarder, action) triple. The
happy path never triggers it — ingest sites only enqueue once per
action. If a future code path does, it shows up as an
`InsertQsoUploadTx` error, which aborts the tx. Don't paper over it
with `ON CONFLICT DO NOTHING`; the constraint failure is telling you
the caller has a bug.

### 9.11 Claim query is race-free by construction

SQLite is single-writer, and the claim query is a single `UPDATE ...
RETURNING *` scoped by `forwarder_name`. Two workers for different
destinations can run in parallel without coordination; two workers for
the same destination would step on each other, but the "one worker per
forwarder_name" rule is enforced at spawn time (we don't loop-create
them). The per-forwarder scope is load-bearing — don't drop it
"because sqlite is single-writer anyway."

### 9.12 Retry defaults belong to the forwarder package

Each forwarder package owns its `DefaultRetry` and registers it via
`forwarding.RegisterDefaultRetry` in `init()`. Upstream-specific
tolerances (QRZ's web API rate limits, ClubLog's daily batch
windows, LoTW's slow acknowledgement loop) belong with the code
that knows them, not with `cmd/smd/main.go`. The registry's
validation mirrors `worker.New`'s constructor checks, so a
malformed default panics at package init rather than surviving to
first use.

---

*This doc and [`forwarding.md`](forwarding.md) are siblings. If you
change the code in a way that invalidates either, update both.*
