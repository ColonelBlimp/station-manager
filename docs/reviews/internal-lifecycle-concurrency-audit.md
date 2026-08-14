# Internal lifecycle and concurrency audit

**Status:** review complete; actions open  
**Reviewed:** 2026-08-14  
**Scope:** production Go under `internal/`, plus `cmd/smd` shutdown ordering; generated
SQLite models and test-only diagnostics excluded  
**Code changes:** none; this document is the review deliverable

## Executive summary

The concurrency foundations are generally strong. Race-enabled tests passed across
the high-risk packages, timers and tickers are stopped or generation-gated, hub
channel closure is serialized, and the shared `safego.GoTracked` helper gets the
important `WaitGroup.Add` ordering right.

The review found **five action themes**. There is no new P0 issue. The only P1 is an
extension of the existing logging review's L9: several bridge goroutines that perform
RF-safety recovery are tracked for shutdown but still have no panic boundary. A panic
there terminates the daemon at exactly the point it is trying to prove the rig is
unkeyed.

The most useful new P2 work is to make the configured graceful-shutdown deadline cover
the entire teardown, make evidence shutdown a real producer barrier, and thread
cancellation through evidence database work. The remaining lifecycle inconsistencies
are P3 hardening because the current daemon calls those packages in the documented
order.

No production source was changed during this audit.

## Findings at a glance

| ID | Priority | Area | Disposition |
|---|---:|---|---|
| LC-1 | P1 | RF-safety goroutines have no structured panic boundary | Existing L9; extend its inventory |
| LC-2 | P2 | Graceful-shutdown deadline begins after subsystem teardown | New |
| LC-3 | P2 | Evidence Stop is not an atomic producer cutoff or concurrent barrier | New |
| LC-4 | P2 | Evidence database work cannot be cancelled by request or service lifetime | New; overlaps EH-4 |
| LC-5 | P3 | Optional-service lifecycle state machines are inconsistent | New hardening |

Priority meanings follow the existing internal reviews: P0 is release-gate work, P1
should be closed before a serious release, P2 is important correctness or operability
work, and P3 is useful hardening.

## LC-1 — RF-safety goroutines lack a panic boundary (P1)

This extends
[`internal-codebase-logging-gaps.md` L9](internal-codebase-logging-gaps.md#l9-critical-long-running-goroutines-bypass-structured-panic-logging);
it is not a duplicate finding. L9 already lists the bridge supervisor, evidence
writer/sync loops, FT8 capture pump, and serial reader.

The bridge has additional bare goroutines whose `WaitGroup` ownership and shutdown
ordering are correct, but whose panic behavior is not:

- the per-pipeline state poll and FT8 meter poll workers at
  [`internal/bridge/pipeline.go:411`](../../internal/bridge/pipeline.go) and
  [`internal/bridge/pipeline.go:426`](../../internal/bridge/pipeline.go);
- the defensive unkey worker at
  [`internal/bridge/txconfirm.go:439`](../../internal/bridge/txconfirm.go);
- the TX-alarm probe loop at
  [`internal/bridge/txrecheck.go:205`](../../internal/bridge/txrecheck.go); and
- the repeated stuck-TX unkey worker at
  [`internal/bridge/txrecheck.go:301`](../../internal/bridge/txrecheck.go).

The last three are specifically safety work. They run after an uncertain key/unkey
outcome and exist to keep TX blocked, prove idle, or keep asserting `tx_off`. A panic
currently bypasses the structured logger and terminates the process. Process exit does
not prove that a CAT-keyed radio dropped PTT.

The state cleanup in `retryUnkeyStillKeyed` is already deferred, and all three safety
workers register their `wg.Add(1)` while holding the same mutex that gates Stop. Those
properties should be preserved when adding recovery.

### Action

Extend L9's implementation work to every bare bridge worker. Add a bridge panic handler
and use `safego.GoTracked`, or an equivalent wrapper that does not perform a second
`WaitGroup.Add` after the existing registration.

Decide the post-panic policy explicitly per worker:

- poll loops may be allowed to unwind the current pipeline so the supervisor reconnects;
- bounded alarm probes may restart only if their generation and service context are
  still current; and
- a defensive unkey panic must leave TX blocked/uncertain and raise a durable alarm
  before any retry is considered.

Do not simply recover and return from an RF-safety worker while publishing the bridge
as healthy.

### Required tests

Inject a panic into each worker and assert a named structured panic record, balanced
WaitGroup accounting, no state mutation after Stop, and the appropriate safe state.
For unkey workers, assert `TxReady()==false` until positive idle evidence arrives.

## LC-2 — the graceful-shutdown deadline starts too late (P2)

`server.shutdown_timeout_sec` is documented as the graceful-shutdown deadline, but
`cmd/smd` constructs its context only at
[`cmd/smd/main.go:1049`](../../cmd/smd/main.go), after it has synchronously waited for:

- `bridgeSvc.Stop()`;
- `ft8Svc.Stop()`;
- `pskSvc.Stop()`; and
- `evidenceSvc.Stop()`.

The listener is correctly closed first, and the subsystem ordering is sound: bridge
and FT8 stop publishing, FT8 drains the only evidence producer, PSK Reporter performs
its final flush, then evidence drains. The gap is purely the bound. A stuck subsystem
prevents execution from ever reaching the configured timeout, HTTP shutdown, worker
drain, QSO-log drain, or the closing lifecycle record.

Most individual paths are designed to finish: serial writes have a watchdog, FT8's
non-cancellable decode is expected to finish within a slot, PSK uses UDP, and evidence
uses a two-second SQLite busy timeout. Those are useful local bounds, but they do not
form an overall deadline. SQLite statement execution is not cancelled by
`busy_timeout`, a serial driver can remain wedged after `close(2)`, and a future Stop
implementation can silently add another unbounded wait. The bundled systemd unit does
not declare a repository-owned `TimeoutStopSec` backstop.

### Action

Start one shutdown budget immediately after `server.StopAccepting()`, and make the
remaining teardown consume that budget. Prefer context-aware Stop methods for work
that can be cancelled. Where cleanup must be attempted despite cancellation—most
notably unkey—give that operation its own small safety reserve, then report which
component exceeded the budget.

The desired invariant is: after the drain gate rises, either every ordered teardown
stage completes or the daemon records the stage that exhausted the one configured
grace period. Do not let a timeout skip the initial RF-unkey attempt.

### Required tests

Use blocking seams for bridge, FT8, PSK and evidence Stop. Assert that each is named in
the timeout record, later safe cleanup still runs where possible, HTTP shutdown is
signalled, and total graceful teardown is bounded close to the configured duration.

## LC-3 — evidence Stop is not a producer barrier (P2)

Evidence capture uses an atomic `closed` flag, but the lifecycle transition is split
across unrelated operations:

1. `CaptureSlot` checks `closed`, later snapshots `started` under `s.mu`, then later
   still enqueues on `s.ch` at
   [`internal/evidence/service.go:347`](../../internal/evidence/service.go).
2. `Stop` swaps `closed`, snapshots `started`, closes `quit`, and waits for the writer
   at [`internal/evidence/service.go:321`](../../internal/evidence/service.go).
3. `writerLoop` drains until a non-blocking receive finds the channel momentarily empty,
   then returns at
   [`internal/evidence/service.go:632`](../../internal/evidence/service.go).

A `CaptureSlot` that passed the first closed check can enqueue after the writer's final
empty check. The slot is then never processed, `pending` remains elevated, and Stop has
already returned. The current daemon avoids this interleaving by calling
`ft8Svc.Stop()` before `evidenceSvc.Stop()`, and FT8 is the only configured producer.
The package itself, however, documents `CaptureSlot` as safe to drop when stopped and
Stop as draining the writer; it does not make that cutoff atomic.

Two related lifecycle edges exist:

- Stop-before-Start sets `closed=true`, but Start does not check it and can still open
  the database and spawn workers; and
- concurrent Stop callers are not a barrier: the first caller swaps `closed`, while a
  second caller sees it already true and returns before the first has drained and
  closed the database.

The bridge and FT8 services already implement the stronger project pattern with
`stopOnce` plus `stopDone`: every Stop caller waits for the same teardown, and Start
after Stop is terminal.

### Action

Give evidence one mutex-serialized lifecycle state and one completion channel. Close
producer admission under the same lock used to decide whether an enqueue is accepted,
then drain exactly the accepted work. Make Start refuse the terminal state, and make
every concurrent Stop wait for the teardown owner.

Closing `s.ch` can work only if send admission and channel closure share a lock or an
equivalent in-flight-producer count; otherwise it replaces silent late enqueue with a
send-on-closed-channel panic.

### Required tests

Add deterministic barriers around admission, the writer's final drain check, and
teardown completion. Cover:

- CaptureSlot paused before enqueue while Stop starts;
- many producers racing Stop under `-race`;
- three concurrent Stop callers, all returning after database close; and
- Stop-before-Start remaining terminal with no file, goroutine, or open handle.

## LC-4 — evidence database work lacks cancellation (P2)

The production-only `noctx` pass found 74 database/network calls without context; the
large majority are in evidence and SQLite bootstrap code. Startup-only bootstrap work
is less concerning because process signals can terminate startup. Runtime evidence
work is different: it participates in HTTP requests and service shutdown.

`GET /v1/evidence/status` discards the request at
[`internal/api/handler_evidence.go:22`](../../internal/api/handler_evidence.go) and calls
`Status()` without a context. A sync-enabled status request can execute at least 17
sequential database operations: profile totals/grouping, two queries for each of five
sync tables, retention totals/metadata, and the two observation counts. Each pooled
connection has a two-second SQLite busy timeout, but there is no shared request
deadline and client disconnect cannot cancel the remaining work. This is the
cancellation half of the partial/unknown result problem already recorded as EH-4.

The evidence writer, purge/compaction, checkpoint, profile activation, and sync mark
paths likewise use `Begin`, `Exec`, `Query`, and `QueryRow` rather than their context
forms. `syncLoop` already creates a cancellation context so Stop does not wait for the
30-second HTTP timeout, but the context reaches only the network request, not its
surrounding database work. The writer has no service context at all. Consequently an
in-flight SQLite statement can hold `evidenceSvc.Stop()` past shutdown, and LC-2's late
deadline cannot help.

### Action

Introduce an evidence service-lifetime context cancelled by Stop. Thread it through
writer, retention, profile and sync database operations using `BeginTx`, `ExecContext`,
`QueryContext`, and `QueryRowContext`. Change `Status` to accept the HTTP request
context and apply one short aggregate deadline to the entire snapshot.

Keep the deliberate background contexts in RF unkey and post-commit QSO repair paths;
those are not evidence of a general cancellation bug. Their job must outlive a
cancelled request, and they already use bounded operations.

### Required tests

Block one status query and one writer/retention statement. Cancel the request or Stop
the service and assert prompt return, transaction rollback, no goroutine remaining,
and an honest degraded/unknown status rather than zero counts. Coordinate the result
shape with EH-4 so the two fixes do not create competing status designs.

## LC-5 — lifecycle state machines should be standardized (P3)

Several packages are correct under the daemon's current ordered, single-use calls but
do not provide the terminal/concurrent guarantees now established by bridge and FT8:

- PSK Reporter documents Start as single-use, but Start does not check `stopped` and
  Start/Stop are not serialized. Stop-before-Start followed by Start can leave a live
  socket/flush worker after the only Stop returned; concurrent Starts can replace the
  stored connection and cancel function while earlier workers remain live. See
  [`internal/pskreporter/service.go:129`](../../internal/pskreporter/service.go).
- lookup refresher Stop-before-Start sets `stopped`, but Start checks only `started` and
  installs a new uncancelled parent context. Schedule still rejects because `stopped`
  remains true, so this wastes no worker today, but the state machine is contradictory.
  See [`internal/lookup/refresher/refresher.go:108`](../../internal/lookup/refresher/refresher.go).
- SQLite Close has a lock-free `!isOpen` early return. If it races an Open that holds
  `s.mu` but has not yet published `isOpen=true`, Close returns and Open can publish a
  live handle afterwards. Current wiring never performs concurrent Open/Close, and the
  service intentionally supports a later ordered reopen. See
  [`internal/database/sqlite/service.go:176`](../../internal/database/sqlite/service.go).

These are not current daemon-path failures, so P3 is appropriate. They are traps for
error-path cleanup, future reload/restart work, and tests that reasonably interpret an
idempotent Stop/Close as a completion barrier.

### Action

Adopt and document one of two contracts per service:

1. terminal single-use lifecycle: Start once, Stop once-or-concurrently, Start after
   Stop is a no-op/error; or
2. restartable lifecycle: each generation owns fresh channels/context/resources, and
   Close fully completes before the next Open/Start.

For daemon-owned background services, the first contract is simpler and matches bridge
and FT8. SQLite is the natural exception and should keep the second contract while
serializing Open/Close before lock-free fast-path returns.

## Context diagnostics reviewed and accepted

The raw context tools are intentionally conservative. The following recurring reports
are not action items:

- `context.Context` fields in bridge, FT8 and lookup refresher are service-lifetime
  roots, not request contexts retained beyond a request;
- RF unkey, tune restore and TX-alarm remediation deliberately use a fresh background
  context because cancellation must not skip the safe-state command;
- `POST /v1/ft8/tx` returns 202 and the transmission is owned by the daemon lifecycle,
  so it must not inherit client disconnect;
- QSO post-commit refetch/cache warming deliberately survives request cancellation and
  applies its own short timeout; and
- timer callbacks cannot inherit a request context directly; their generation and
  stopped-state gates are the relevant ownership mechanism.

The health handler's use of `db.Ping()` rather than a request-aware Ping is a small
follow-up candidate, not a separate finding: it is bounded by two configured database
timeouts plus a 25 ms retry backoff. Add `PingContext` when LC-4 establishes the
request-context convention.

## Reviewed mechanisms with no action

- **Timers/tickers:** `govet`, the selected Staticcheck timer/concurrency checks, and
  `fatcontext` reported no issue. Manual review found tickers stopped and `AfterFunc`
  callbacks guarded by stopped/generation state.
- **Hubs/channels:** events, bridge and FT8 hubs serialize publish/unsubscribe/close,
  return closed channels to late subscribers, and make unsubscribe idempotent.
- **Tracked workers:** `safego.GoTracked` performs `wg.Add(1)` synchronously and keeps
  the count live across respawn cooldown. Lookup refresher correctly holds its
  lifecycle mutex across scheduling and that Add.
- **Decode log:** its writer has an internal panic recovery, Close drains the writer,
  and drop-warning goroutines are exponentially bounded.
- **Audio capture:** the cancellation goroutine can race Close safely; internal
  cancellation makes its second Stop return promptly.
- **Logging drain timeout:** its waiter can outlive a timed-out Close only until active
  log events finish. The process-exit-only contract and decision not to close the
  writer underneath stragglers are explicit.
- **Serial write watchdog:** a kernel-level wedged write may retain at most one writer
  goroutine per failed Port instance; the caller still returns on time and the port is
  closed. This is an explicit driver limitation, not an accidental unbounded spawn.
- **Desktop inhibition:** the context-free D-Bus handshake can strand at most one
  acquire plus one cleanup waiter per surface; claim guards prevent repeated stacking,
  and D-Bus method calls themselves have deadlines.

## Validation performed

Production-only static analysis used `containedctx`, `contextcheck`, `fatcontext`,
`govet`, `noctx`, and concurrency-relevant Staticcheck checks. It produced:

- 3 stored-context reports, all intentional service-lifetime roots;
- 38 context-propagation reports, mostly deliberate safety/lifecycle detachments; and
- 74 `noctx` reports, concentrated in evidence and startup bootstrap database work.

`govet`, `fatcontext`, and the selected Staticcheck checks were clean.

Targeted race-enabled tests all passed:

```text
go test -race ./internal/evidence ./internal/pskreporter \
  ./internal/lookup/refresher ./internal/events ./internal/serial ./internal/safego

go test -race ./internal/bridge ./internal/ft8 ./internal/audio/... \
  ./internal/inhibit ./internal/logging ./internal/forwarding/... ./internal/api

go test -race ./internal/database/sqlite ./internal/config \
  ./internal/qsoservice ./internal/lookup/...
```

Passing race tests are useful evidence for the exercised state mutations. They do not
disprove LC-2, LC-3, or LC-5: those are ordering/completion-contract failures that use
race-free synchronization primitives.

## Recommended action order

1. Extend existing L9 to the bridge RF-safety workers (LC-1).
2. Establish the whole-process shutdown budget and service-context design together
   (LC-2 and LC-4).
3. Make evidence producer admission and Stop one atomic lifecycle transition (LC-3).
4. Standardize the remaining service state machines and add transition tables/tests
   (LC-5).

