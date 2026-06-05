# internal/forwarding code review (2026-06-05)

## Scope

Reviewed the forwarding subsystem as it exists on 2026-06-05:

- `internal/forwarding/forwarding.go`, `registry.go`
- `internal/forwarding/worker/worker.go`, `backoff.go`
- `internal/forwarding/qrz/qrz.go`, `response.go`
- `internal/forwarding/stub/stub.go`
- forwarding-adjacent queue/runtime code in `internal/database/sqlite/api_context.go`,
  `internal/qsoservice/{submit,update,delete,forwarders}.go`, `cmd/smd/main.go`,
  `internal/safego/safego.go`, and the related API/E2E tests

Not reviewed in depth: generated sqlboiler model code, unrelated frontend changes in
the working tree, and future forwarder implementations not yet present.

## Summary

The package boundary is generally clean: forwarders do not know about queue rows,
workers do not know QRZ protocol details, retry defaults live with the forwarder
package, and the queue persistence tests cover the main state transitions. The
focused suite is green, including race coverage for `internal/forwarding/...`.

Headline findings: 0 critical, 3 medium, 2 low. The two important themes are
recovery and lifecycle ordering. A worker panic can still strand an already-claimed
row until a full daemon restart, despite the respawn path. Separately, the queue
stores insert/update/delete as independent rows, but the worker does not yet enforce
per-QSO lifecycle ordering or supersession semantics when several rows are pending
at once.

## M - Medium

### M1. Worker respawn after panic does not recover rows already claimed by that worker

Files:

- `internal/forwarding/worker/worker.go:50`
- `internal/forwarding/worker/worker.go:157`
- `internal/safego/safego.go:96`
- `cmd/smd/main.go:724`
- `internal/database/sqlite/api_context.go:2006`

`Worker.Run` is launched through `safego.GoTracked(..., respawn=true)` in
`spawnForwarderWorkers`. If a panic happens inside `processRow`, `safego` recovers
the panic and respawns the worker loop. However, the row was already claimed by
`ClaimPendingUploadsWithContext`, so its status is `in_progress`. The respawned
worker only claims `pending` rows, and `ResetOrphanedUploadsWithContext` only runs
at daemon startup.

That means the documented "respawn" recovery does not actually recover the row that
was in flight when the panic happened. It remains hidden from the worker until an
operator restarts the daemon or manually resets it. This also contradicts the worker
comment saying panic restart is idempotent because orphaned rows are reset at daemon
startup: in-process respawn is not daemon startup.

Suggested direction: add a per-row recovery boundary around `processRow`, or add a
worker/forwarder-scoped in-progress reset on respawn. Treating the panic like the
existing crash-recovery path is reasonable: reset the row to `pending` with a panic
message and retry later, accepting the same duplicate-upstream risk already accepted
for daemon crash recovery. Add a test with a forwarder whose `Submit` panics after
the row is claimed, then assert the row becomes claimable again without restarting
the daemon.

### M2. Multiple lifecycle rows for the same QSO can be processed without defined insert/update/delete ordering

Files:

- `internal/database/sqlite/api_context.go:1633`
- `internal/database/sqlite/api_context.go:2159`
- `internal/database/sqlite/api_context.go:2313`
- `internal/forwarding/worker/worker.go:157`
- `internal/forwarding/worker/worker.go:226`
- `internal/forwarding/qrz/qrz.go:263`
- `internal/forwarding/qrz/response.go:182`
- `internal/api/handler_e2e_test.go:200`
- `internal/api/handler_e2e_test.go:251`

The queue stores one row per `(qso_id, forwarder_name, action)`, so a fast operator
can create `insert`, `update`, and `delete` rows for the same QSO before the worker
has forwarded the original insert. This is realistic with the default 120 second
tick.

The claim query orders only by `next_attempt_at`, which is stored at second
precision by the default/UPSERT path. Same-second ties are common, and `UPDATE ...
RETURNING *` does not provide an explicit returned-row ordering. The worker then
processes the returned rows as-is.

Bad outcomes follow from that:

- A QRZ `update` is sent as `ACTION=INSERT&OPTION=REPLACE`. If it runs before the
  insert row, QRZ can create the upstream record and return a `LOGID`, but the LOGID
  is stored on the `update` row. Later deletes only look at uploaded `insert` rows,
  so the worker may be unable to delete an upstream record it created via update.
- If a QSO is deleted before the initial insert has uploaded, the insert row is
  marked failed because the QSO is soft-deleted, then the delete row is marked
  failed because no uploaded insert row has an upstream id. The final upstream state
  may be correct (nothing exists), but the local queue shows a terminal failure for
  a no-op delete.

The current E2E tests avoid this by waiting for the insert to settle before PATCH or
DELETE. That tests the normal path but not the backlog/supersession path.

Suggested direction: define and test the per-QSO lifecycle rule. At minimum, claim
and return rows in a deterministic order such as `(next_attempt_at, qso_id,
action-priority, id)`, with action priority `insert < update < delete`. More
importantly, add explicit supersession behavior: a delete before any upstream insert
may be an idempotent success/no-op, and a delete should consider any prior successful
upstream-creating action if QRZ update can create records.

### M3. Startup orphan reset can interfere with an already-running daemon

Files:

- `cmd/smd/main.go:330`
- `cmd/smd/main.go:362`
- `cmd/smd/main.go:447`
- `internal/database/sqlite/api_context.go:2006`

`run` resets every `in_progress` upload row before it proves this daemon instance is
the only active process using the datastore. It then spawns forwarder workers before
the HTTP listener is started. If a second daemon is accidentally launched against
the same DB, it can reset rows currently being processed by the first daemon, spawn
workers, and submit duplicate work before its listener fails to bind and shutdown
begins.

SQLite's atomic claim protects against two workers claiming the same `pending` row
in one process, but the unconditional startup reset breaks that protection across
processes by turning active `in_progress` rows back into `pending`.

Suggested direction: acquire the singleton runtime resource before the orphan sweep
and before spawning workers. Binding/listening earlier may be enough for the TCP
case, but a datastore/process lock is clearer. A fallback mitigation is to reset
only rows whose `last_attempt_at` is older than a conservative stale threshold,
though that still does not prove single ownership.

## L - Low

### L1. ADR 0022 enqueue semantics are implemented, but several comments/docs still say "enabled forwarder"

Files:

- `internal/qsoservice/submit.go:272`
- `internal/qsoservice/update.go:235`
- `internal/qsoservice/delete.go:14`
- `docs/v2-design/forwarding.md:557`
- `docs/v2-design/forwarding.md:876`

`shouldEnqueue` correctly ignores `Enabled` per ADR 0022; configured-but-disabled
forwarders accumulate pending rows and only worker spawning is gated by `Enabled`.
The tests pin this behavior. Several nearby comments and design-doc acceptance
bullets still describe row creation as "per enabled forwarder", which is now wrong
and can easily lead to a future regression.

Suggested direction: update the comments/docs to say "configured forwarder whose
action_filter includes the action"; reserve "enabled" for worker lifecycle.

### L2. Worker trusts non-success forwarder results to carry an error message

Files:

- `internal/forwarding/forwarding.go:41`
- `internal/forwarding/worker/worker.go:337`
- `internal/forwarding/worker/worker.go:384`
- `internal/forwarding/worker/worker.go:477`

The interface documents that `Result.Err` is set when `Outcome` is not success, but
the worker does not normalize this if a future forwarder returns `OutcomeTerminal`
or `OutcomeTransient` with `Err == nil`. The row can then be marked failed or
requeued with an empty `last_error`, and the `forward.failed` SSE payload can carry
an empty `reason`.

Suggested direction: make `errText` outcome-aware or add fallback text at the call
sites, for example "forwarder returned terminal outcome without error". This is a
small defensive improvement for future forwarder packages.

## Test Results

Passed:

```text
go test ./internal/forwarding/... ./internal/qsoservice ./internal/api ./internal/database/sqlite
go test -race ./internal/forwarding/...
```

Both commands needed to be rerun outside the sandbox because `httptest` localhost
listeners are blocked in the sandbox. The reruns passed.

## Resolutions

### Batch A — M1 worker panic recovery. **DONE 2026-06-05.**
All 5 findings validated against the code first (4 parallel read-only passes) — **all code-accurate**. M1 fixed:
`internal/forwarding/worker/worker.go` — `tickOnce` now calls a new `processRowSafely`
that wraps `processRow` in a per-row `recover`. A panic mid-row is recovered, logged
(forwarder/upload_id/qso_id/action/panic/stack), and the already-claimed `in_progress`
row is reset via `markTransientInternal` (respects retry budget + backoff + emits events;
chronic panic → exhausts budget → `markFailed`). The worker keeps draining the batch
instead of unwinding `Run` and stranding the row until a daemon restart. During shutdown
(`ctx` cancelled) the reset is skipped — the startup orphan sweep reclaims it. Corrected
the false `Worker` doc comment that claimed startup-reset made panic-restart idempotent
(it doesn't for in-process safego respawn). Test `TestWorker_PanicInSubmit_RowReclaimableWithoutRestart`
(a forwarder whose `Submit` panics → row returns to `pending`, attempts≥1, last_error
mentions the panic; without the boundary the panic crashes the test goroutine).
`go test ./... ` + `-race ./internal/forwarding/...` green; `go vet` + `gofmt` clean.

### Batch B — M2 tractable half (delete lookup + deterministic ordering). **DONE 2026-06-05.**
`internal/database/sqlite/api_context.go` + `worker.go` + tests:
- **Delete upstream-id lookup broadened** (real fix for outcome (a)): renamed
  `FetchInsertUpstreamIDWithContext` → `FetchPriorUpstreamIDWithContext`, filter changed from
  `action=insert` to `action IN (insert, update)` (both are upstream-creating: insert=`INSERT`,
  update=`INSERT&OPTION=REPLACE`). A delete after an out-of-order update now finds the LOGID the
  update parked on its own row. Renamed the prod caller + qrz comments. Test
  `_IgnoresNonInsertActions` inverted → `_ConsidersUpdateAction` + new `_IgnoresDeleteAction`.
- **Deterministic claim ordering** (outcome (b) + general): subquery `ORDER BY (next_attempt_at,
  qso_id, CASE action insert<update<delete, id)` + Go-side re-sort of the batch on the same key
  (`UPDATE ... RETURNING` order is unspecified in SQLite). Test
  `TestClaimPendingUploads_OrdersLifecycleRowsInsertUpdateDelete` (reverse-enqueued, pinned same
  next_attempt_at). `go test ./...` + `-race` green; vet+gofmt clean.

**DEFERRED (operator, 2026-06-05): M2 enqueue-time supersession** (delete-before-insert-uploaded as
an idempotent no-op rather than two terminal-failure rows). Touches ADR 0022; outcome (b) is benign
(upstream ends correct), so tidiness not correctness — revisit with an ADR if the noisy rows bother.

**DEFERRED (operator, 2026-06-05): M3** (second-daemon orphan reset). LOW for the single-daemon
systemd topology; working-dir flock lockfile is the candidate fix when picked up.

### Triage summary (validation outcomes, for the remaining findings)
- **M2** (lifecycle ordering) — tractable half **DONE (Batch B)**; enqueue-time supersession
  **DEFERRED** (ADR-level; outcome (b) benign).
- **M3** (startup orphan-reset vs 2nd daemon) — code-accurate; LOW for single-daemon/systemd.
  **DEFERRED** (flock lockfile candidate).
- **L1** (stale "enabled forwarder" comments/docs) — accurate; trivial doc edit (3 comments
  + 3 forwarding.md spots). **Queued (Batch D).**
- **L2** (worker trusts non-success Result.Err) — accurate; latent (only stub ships).
  Small `errText`/outcome-aware fallback. **Queued (Batch D).**
