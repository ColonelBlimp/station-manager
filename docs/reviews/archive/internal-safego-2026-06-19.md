# internal/safego code review - 2026-06-19

## Scope

Fresh review of `internal/safego` at `1802fae6`, approached as a new
package review. I read the package implementation, package tests, direct
production callers, shutdown/lifecycle call sites, and the design docs that
describe the safego contract.

Focus areas: correctness, performance, security, test coverage, and
documentation. This is review-only; no source fixes were made.

Primary files reviewed:

- `internal/safego/safego.go`
- `internal/safego/safego_test.go`
- `cmd/smd/main.go`
- `internal/ft8/service.go`
- `internal/ft8/servicetx.go`
- `internal/lookup/orchestrator.go`
- `internal/lookup/refresher/refresher.go`
- `internal/pskreporter/service.go`
- `internal/forwarding/worker/worker.go`
- `docs/v2-design/forwarding.md`
- `docs/v2-design/forwarding-implementation.md`
- `docs/v2-design/enrichment.md`

## Summary

The core `safego` implementation is small and mostly sound. It recovers
panics from `fn`, protects the panic handler from crashing the daemon, uses a
loop rather than recursive respawn, keeps `GoTracked`'s `WaitGroup` count
non-zero across respawn cooldowns, and avoids the old `time.After` cancellation
timer leak. The direct package tests cover the intended happy paths and panic
paths well; package statement coverage is high.

The highest risks are caller-contract issues. `safego` recovers a panic outside
the caller's function body, so code after the panic point is skipped unless the
caller put cleanup in a `defer`. One current `ft8.tx` caller does not do that,
which can leave the transmit service permanently marked in-flight after a
recovered panic. A second FT8 TX call-site issue exposes in-flight state before
calling `GoTracked`, opening a narrow window where disarm can pass `txWg.Wait`
before the goroutine has been counted.

## Findings

### H1. A recovered `ft8.tx` panic can leave transmit permanently in-flight

**Area:** correctness / hardware-facing service state / test coverage  
**Files:** `internal/safego/safego.go:96-111`,
`internal/ft8/servicetx.go:329-367`,
`internal/ft8/servicetx.go:166-219`,
`internal/ft8/servicetx.go:518-550`,
`internal/ft8/servicetx_test.go:212-255`

`runWithRespawn` recovers panics from `fn` and then returns when
`respawn=false` (`safego.go:96-111`). That is the right primitive, but it means
caller code after the panic point does not resume. Callers that own state must
install cleanup defers before invoking panic-prone work.

`startTransmission` sets the FT8 service state to in-flight before launching
the safego wrapper:

- `txCancel`, `txInFlight`, `txMessage`, and `txOffsetHz` are set at
  `servicetx.go:329-334`.
- The safego body then calls `err := fn(txCtx, ctrl)` at `servicetx.go:339-341`.
- The state cleanup is ordinary code after that call, not a defer
  (`servicetx.go:346-359`).

If `fn` panics, safego logs and recovers it, but the cleanup block is skipped.
`defer cancel()` runs, yet `txInFlight` remains true, `txCancel` remains set,
the message/offset remain cached, no final `ft8-tx` state is published, and
`onDone` is not called. `disarmTx` does not clear those fields itself; its
comment explicitly relies on the TX goroutine to clear `txCancel/txInFlight`
(`servicetx.go:166-219`, `servicetx.go:518-525`). After the safego goroutine
has already exited, disarm waits on an already-done `txWg`, closes the device,
and can still publish `Transmitting: true` because `publishTxState` snapshots
the uncleared flag (`servicetx.go:540-550`).

The likely operator-visible outcome is a TX path wedged until daemon restart:
future transmit attempts hit `ErrTxInFlight`, and re-arming does not clear the
stale in-flight fields. The controller's own defers and bridge auto-off should
still drop PTT if the panic happens after keying, but the daemon state remains
incorrect.

**Recommendation:** make the `ft8.tx` safego closure install its state cleanup
defer before calling `fn`. Track whether `fn` completed normally so the defer
can clear `txInFlight`, clear `txCancel`, publish a final state, set
`ft8_tx_failed` for panic/error cases, and call `onDone(false)` for panics.
Add a regression in `internal/ft8` that calls `startTransmission` with a
panicking `fn` and asserts `txInFlight=false`, `txCancel=nil`, a final state is
published, and a later transmission is not rejected as `ErrTxInFlight`.

Also update the `safego` package comment to state the caller rule explicitly:
cleanup, semaphore release, channel sends, status publication, and callbacks
that must run after a panic need to live in defers inside `fn`.

### M1. `ft8.tx` exposes in-flight state before `GoTracked` increments `txWg`

**Area:** correctness / concurrency / shutdown accounting  
**Files:** `internal/safego/safego.go:74-90`,
`internal/ft8/servicetx.go:329-339`,
`internal/ft8/servicetx.go:166-219`,
`internal/lookup/refresher/refresher.go:189-193`,
`internal/lookup/refresher/refresher.go:223-227`

`GoTracked` calls `wg.Add(1)` synchronously before spawning the goroutine
(`safego.go:85-90`). That is the key lifecycle guarantee, but it only helps if
the caller invokes `GoTracked` before another goroutine can reach the matching
`Wait`.

`startTransmission` sets `txCancel` and `txInFlight`, unlocks `txMu`, publishes
the transmitting state, and only then calls `safego.GoTracked`
(`servicetx.go:329-339`). A concurrent `ArmTx(false)` / `disarmTx` can run in
that window:

1. It observes `txCancel != nil` / `txInFlight=true`.
2. It cancels the context.
3. It calls `s.txWg.Wait()` while the counter is still zero
   (`servicetx.go:206-209`).
4. It closes the output device and publishes disarmed state.
5. `startTransmission` finally calls `GoTracked`, incrementing the `WaitGroup`
   after the waiter has already passed.

That violates the documented disarm contract: "drains the TX goroutine, and
closes the output device" (`servicetx.go:166-168`). Current controller behavior
usually returns quickly after the cancellation, so this is narrower than a
direct RF safety issue. It is still incorrect shutdown accounting, and it makes
the TX path depend on the controller never touching a closed player after a
pre-start cancellation.

`internal/lookup/refresher` shows the safer pattern: it holds `mu` across the
state check, semaphore acquisition, and the `GoTracked` call specifically so
`Stop` cannot reach `wg.Wait` before the Add (`refresher.go:189-193`,
`refresher.go:223-227`).

**Recommendation:** make FT8 TX call `GoTracked` before exposing waitable
in-flight state to `disarmTx`, or otherwise perform the `txWg.Add(1)` while
holding the same state lock that gates disarm. A practical fix is to move the
`GoTracked` call into the `txMu`-protected setup before publishing state, with
the goroutine's cleanup blocked on `txMu` until setup releases it. Add a
concurrency regression that stresses `TransmitNext` racing `ArmTx(false)` under
`-race` and asserts disarm does not return until the launched TX attempt has
cleared state.

### M2. Long-lived loops use `respawn=false` without a recovery policy

**Area:** correctness / availability / documentation  
**Files:** `internal/safego/safego.go:61-64`,
`internal/ft8/service.go:287-308`,
`internal/ft8/service.go:385-424`,
`internal/pskreporter/service.go:120-145`,
`internal/pskreporter/service.go:172-183`,
`docs/v2-design/enrichment.md:115-118`

The `safego` API docs say to use `respawn=false` for one-shot goroutines and
`respawn=true` for long-lived workers whose absence silently breaks a feature
(`safego.go:61-64`). Some current long-lived subsystem loops intentionally use
`respawn=false`, but they do not have an alternate health or restart path.

Examples:

- FT8 capture starts `ft8.scheduler` and `ft8.decoder` with `respawn=false`
  (`service.go:302-308`). If a panic escapes from `decodeLoop` after a sink,
  occupancy, or sequencer call (`service.go:392-424`), the decoder goroutine
  exits while the scheduler can continue dropping slots. If the scheduler
  panics, `Scheduler.Run` closes its slots channel and the decoder exits. In
  both cases the service can remain marked as capturing until subscriber
  release or Stop.
- PSK Reporter starts `pskreporter.flush` with `respawn=false`
  (`service.go:143-145`). If a panic escapes the flush loop, future FT8 spots can
  continue buffering with no background flush worker (`service.go:172-183`).

Not every long-lived loop is safe to respawn with the existing function. FT8's
scheduler and decoder are a coupled pair; `Scheduler.Run` closes the channel on
exit, so respawning the same `sch.Run` is not a valid group restart. That is the
point: the current code uses safego to protect the daemon process but does not
define what happens to the feature after the recovered panic.

The refresher documentation is clearer about one-shot behavior and why
`respawn=false` is acceptable (`enrichment.md:115-118`). The long-lived FT8 and
PSK Reporter paths need the same explicit policy, or a restart/health mechanism.

**Recommendation:** decide the recovery policy per long-lived caller. PSK
Reporter can likely use `respawn=true` because the loop drives off shared
buffer/socket state. FT8 likely needs a capture-session restart helper rather
than respawning the two closures independently. At minimum, record a health
state and publish/log a terminal feature failure so the operator is not left
with a live-looking but dead subsystem. Add tests that inject a panic in the
decode sink / PSK flush path and assert the chosen recovery behavior.

### L1. The public contract is under-documented and the forwarding docs are stale

**Area:** documentation / maintainability  
**Files:** `internal/safego/safego.go:44-69`,
`internal/safego/safego.go:74-85`,
`docs/v2-design/forwarding.md:745-803`,
`docs/v2-design/forwarding.md:918-923`,
`docs/v2-design/forwarding-implementation.md:447-501`,
`cmd/smd/main.go:842-846`,
`cmd/smd/main.go:924-934`

The `safego` comments document respawn and `WaitGroup` behavior, but they do not
spell out two caller obligations that are now load-bearing:

- Code after a panic-prone call does not run; required cleanup must be deferred
  inside `fn`.
- `GoTracked` must be called before the goroutine becomes visible to any path
  that can call the matching `Wait`.

The implementation docs in `docs/v2-design/forwarding-implementation.md` show
both `Go` and `GoTracked` accurately (`forwarding-implementation.md:447-501`),
but `docs/v2-design/forwarding.md` still describes the implemented shape as
only `Go` and shows a `safego.Go(...)` forwarder snippet
(`forwarding.md:745-803`). The later checklist also says forwarder panics are
caught by `safego.Go` while the actual spawn site uses `GoTracked`
(`forwarding.md:918-923`, `cmd/smd/main.go:924-934`). `cmd/smd`'s
`spawnForwarderWorkers` header has the same stale "launches each under
safego.Go" wording (`cmd/smd/main.go:842-846`).

**Recommendation:** update `safego`'s package comments with the cleanup and
Add-before-Wait caller contracts. Refresh the forwarding design doc and
`spawnForwarderWorkers` header so future code copies the `GoTracked` pattern,
not the older untracked example.

## Security Review

No direct external attack surface was found in `internal/safego`: it does not do
I/O, parse user input, or manage credentials. It improves process resilience by
recovering panics in child goroutines that HTTP middleware and `main` recovery
cannot reach.

The security-relevant boundary is logging. `PanicHandler` receives the raw panic
value and stack trace (`safego.go:26-30`, `safego.go:138-143`), and current
handlers log both. Stack traces expose internal paths and function names; panic
values can expose whatever the panicking code included in the value. That is
acceptable for internal daemon logs, but handlers should continue to avoid
panicking with secrets and should not forward these logs to a less-trusted sink
without redaction.

## Performance Review

The normal path cost is one goroutine and one function call wrapper. Stack
capture (`debug.Stack`) and the atomic cooldown read happen only after a panic,
so there is no meaningful steady-state performance issue in `internal/safego`
itself.

The main performance/operability risk is a deterministic panic loop with
`respawn=true`: it logs and retries every five seconds with no jitter,
exponential backoff, or circuit breaker. The forwarding design doc already
acknowledges this as a future panic-loop detector candidate
(`docs/v2-design/forwarding.md:797-803`). That is acceptable for the current
small number of workers, but if more respawning workers are added, consider a
per-worker backoff or "stop after N panics in M minutes" policy.

## Test Coverage Notes

Strong coverage observed:

- `internal/safego` covers normal return, panic with no respawn, panic with
  respawn, panic handler payload/stack capture, context cancellation during
  cooldown, nil handlers, panicking handlers, and `GoTracked` `WaitGroup`
  lifecycle.
- `internal/lookup/refresher` has the right concurrency pattern around
  `GoTracked` and tests the Stop/Schedule race under the race detector.
- `internal/lookup/orchestrator` uses defers for panic-safe channel sends before
  doing panic-prone work, which is the right safego caller pattern.

Coverage gaps:

- No test covers caller-owned cleanup when `fn` panics under safego. The FT8 TX
  bug above is exactly this missing case.
- No test covers `GoTracked` callers exposing state to a matching `Wait` before
  `GoTracked` has been called.
- No test covers long-lived `respawn=false` loop behavior after a recovered
  panic: whether the feature restarts, reports unhealthy, or intentionally stays
  down.
- `runWithRespawn` does not check `ctx.Err()` before the first `fn()` call. That
  may be intentional because `fn` usually captures and observes the context, but
  a small unit test should pin the contract either way.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test ./internal/safego` - pass
- `GOCACHE=/tmp/go-build go test -race ./internal/safego` - pass
- `GOCACHE=/tmp/go-build go test -cover ./internal/safego` - pass,
  96.6% statement coverage
- `GOCACHE=/tmp/go-build go test ./internal/ft8` - pass when rerun outside the
  sandbox; the sandboxed run failed only because `httptest.NewServer` could not
  bind a localhost listener
- `GOCACHE=/tmp/go-build go test ./internal/lookup ./internal/lookup/refresher`
  - pass
- `GOCACHE=/tmp/go-build go test -race ./internal/lookup/refresher` - pass
- `GOCACHE=/tmp/go-build go test ./internal/forwarding/worker` - pass
- `GOCACHE=/tmp/go-build go test -race ./internal/forwarding/worker` - pass
- `GOCACHE=/tmp/go-build go test ./internal/pskreporter` - pass when rerun
  outside the sandbox; the sandboxed run failed only because the UDP listener
  test could not bind

Not counted as safego failures:

- `GOCACHE=/tmp/go-build go test ./internal/lookup/...` does not pass in the
  current dirty worktree because unrelated in-progress `internal/lookup/qrz`
  edits leave `secureOrLoopbackURL` undefined and `net` unused. The safego
  relevant lookup packages were run directly and passed.

## Resolution (2026-06-19)

All findings addressed. The `safego` package itself was sound — every fix is in a
caller (ft8.tx, ft8 capture, pskreporter) or docs. M2's FT8 recovery policy was
an operator decision (health-flag + log; pskreporter → respawn=true).

- **H1 (fixed).** `startTransmission`'s tracked goroutine now does its state
  cleanup in a `defer` (tracking a `normal` flag), so a safego-recovered panic in
  `fn` still clears `txInFlight`/`txCancel`, publishes a final state, sets
  `ft8_tx_failed`, and calls `onDone(false)` — the TX path can no longer wedge
  in-flight until restart. Test: `TestStartTransmission_PanicClearsInFlight`.
- **M1 (fixed).** `GoTracked` (the `txWg.Add(1)`) now runs while
  `startTransmission` still holds `txMu`, before the in-flight state is exposed —
  so a concurrent `disarmTx` (which reads the state under `txMu`, then
  `txWg.Wait` outside it) can't pass Wait with a zero counter. Mirrors the
  hold-the-lock-across-Add pattern in `internal/lookup/refresher`. Test:
  `TestStartTransmission_DisarmRaceClearsState` (50× launch-vs-disarm, asserts
  disarm drained the goroutine; also a `-race` guard).
- **M2 (fixed — operator policy).** **pskreporter** flush loop → `respawn=true`
  (long-lived worker on shared buffer/socket state; Stop still drains via
  runCtx). **FT8** scheduler/decoder stay `respawn=false` (the coupled pair can't
  be respawned independently) but each now defers `onCaptureLoopExit`: an
  unexpected exit (recovered panic / early return while the session is live)
  marks the subsystem not-capturing, cancels the session (winding down the
  sibling), and logs a terminal ERROR — so the operator (FT8 is attended) isn't
  left with a live-looking but dead capture. No auto-restart: re-opening the FT8
  view re-acquires. The helper never waits on `s.wg`, so it can't deadlock a
  concurrent `releaseCaptureLocked` drain.
- **L1 (fixed, docs).** `safego`'s package comment gained an explicit
  **caller-contract** section (defer cleanup after panic-prone calls;
  GoTracked-before-Wait). `cmd/smd`'s `spawnForwarderWorkers` header corrected to
  `safego.GoTracked`. `docs/v2-design/forwarding.md`'s stale `safego.Go` example
  is already covered by its Tier-2 historical banner (per `docs/README.md`), so it
  was left as a frozen brief rather than freshened.

The "ctx.Err() before first fn()" coverage note (Test Coverage Notes) is a pin of
existing behavior, not a defect; left as-is.

Verified: `gofmt`/`go vet` clean; `internal/ft8` (incl. the new H1/M1 tests under
`-race`), `internal/pskreporter` (+`-race`), `internal/safego`, and `cmd/smd`
pass; CGO build of ft8/pskreporter/smd + CGO-free `go build ./...` succeed.
