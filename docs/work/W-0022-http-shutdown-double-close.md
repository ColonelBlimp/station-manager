# W-0022 — HTTP shutdown closes the listener twice

**Status:** Committed `c1b9fbbd` (option A, [ADR 0093](../decisions/0093-http-listener-closes-once-and-keeps-its-result.md)); closes once CI is green
**Selected:** 2026-10-13 (operator), as the shutdown-correctness item after W-0017 A
**Outcome:** A benign repeated close of the HTTP listener no longer fails the daemon's `http`
node, whichever order its listener is untracked in, so a clean shutdown drains the dependent nodes.
Genuine close errors and drain timeouts remain failures, and still skip the dependents.

## Evidence

- CI run 38356688161, attempt 1 (`3968d269`, 2026-10-13), the `-race -short` step:
  `cmd/smd` `TestIdentityWire_SW6_NoRetryWhileHeld` failed at `lifecycle_identity_test.go:518`
  (a restart mid-test). The `http` node failed its drain with
  `close unix …/smd.sock: use of closed network connection`. Because of that failure, `eventhub`,
  `referencedb`, `qsoservice`, `sqliteservice` and `loggingservice` were skipped. The failed log is
  kept locally from attempt 1. One re-run of the failed job (attempt 2) passed; a green retry
  does not close the defect, which the reproducer shows deterministically.
- The consequence is more than a misleading message: a failed `http` node skips its dependent
  nodes' teardown, so cleanup does not run.

## Cause

`api.Server.StopAccepting` closes the raw listener directly (`internal/api/server.go:684–689`).
That bypasses the close-once wrapper `http.Server.Serve` puts around the listener it tracks
(net/http, go1.26 `server.go:3409`). `http.Server.Shutdown` closes every listener it still tracks
and returns the first close error (`server.go:3154`), then waits, with no deadline, for `Serve` to
return. `Serve` untracks its listener only when it returns. So after `StopAccepting`:

- if `Serve` has already returned, `Shutdown` has nothing to close: clean;
- if `Serve` still tracks the listener, `Shutdown` closes it a second time and returns
  "use of closed network connection".

The mechanism is established by source and by the reproducer below. That CI took the second
ordering is inferred.

## Reproducer (deterministic; `internal/api/server_shutdown_order_test.go`, uncommitted)

`serveGated` mirrors `ListenAndServe` with the bound listener wrapped in `gatedListener`. That
wrapper holds `Accept`'s error return, so `Serve` keeps tracking the listener, and releases the hold
at the second close, which is `Shutdown`'s own: that close happens while the listener is still
tracked, and `Serve` can then return.

- SO1 (`Serve` returned before `Shutdown`): passes, 5 of 5.
- SO2 (`Shutdown` while `Serve` still tracks the listener): fails 5 of 5 with
  `close tcp 127.0.0.1:…: use of closed network connection`, the TCP form of CI's text.

## Options (compared after the failing test, as ruled)

- **A. Close once.** Wrap the bound listener in our own close-once listener before both
  `s.listener` and `Serve` see it. `StopAccepting` and `Shutdown` then reach one close; every
  later close returns the first close's result. A genuine first-close error is kept and surfaces
  from `Shutdown`, where `StopAccepting` now ignores it. This involves no error classification.
- **B. Classify in `Shutdown`.** Treat `errors.Is(err, net.ErrClosed)` from `http.Server.Shutdown`
  as success once `StopAccepting` has closed the listener. This is not text matching, but it would
  also accept a listener closed by anything else, and it makes the error path depend on which
  sentinel the platform returns.

**Proposal: A.** It removes the second close rather than tolerating it, and it keeps genuine close
errors visible.

## Failing tests and guards (RED first; the existing ones must stay green)

- SO1 and SO2, as above. SO2 is RED now.
- SO3: a genuine close error surfaces. The listener's first close fails with a distinct error, and
  `Shutdown` returns it, in either ordering.
- SO4: a drain timeout stays a failure. A request is still in its handler when `Shutdown`'s context
  expires, and `Shutdown` returns `context.DeadlineExceeded`.
- SO5: an in-flight request drains. It is in its handler when `StopAccepting` and `Shutdown` run;
  it completes with 200, and `Shutdown` returns nil after it. This is a characterization (it should
  pass before the fix).
- Kept and run: `TestServer_StopAccepting_ReleasesPort` (the port is released immediately) and
  `TestServer_RejectWhenDraining` (503 while draining), plus the shutdown logging tests.
- SO6 (daemon level, `cmd/smd`): with the SO2 ordering forced inside the `http` node, a generation
  stops with every node drained under the strict `stopGen`. Forcing that order inside the daemon
  needs a test seam for wrapping the API listener; where it lives is a question for the operator.
  `stopGen` and the lifecycle skip rules stay strict.
- Reversions: SO2 against the fix removed; SO3 against a close-once that drops the first error; SO6
  against the fix removed.

## Questions for the operator

1. Option A (close once) or B (classify `net.ErrClosed`)? Proposal: A.
2. The SO6 seam: an unexported hook in `internal/api` reached through an exported test-only setter
   (for example `SetListenerWrapperForTest`), or SO6 kept at the `internal/api` level with the node
   drain proved by a smaller `cmd/smd` test that does not force the order?

## Rulings (operator, 2026-10-13)

1. **Option A: close once.** The first close's error is retained and reported by `Shutdown`, even
   when `Serve` has untracked the listener first. Drain errors and timeouts are preserved. The
   close-once wrapper alone is not enough for SO3: the server records `StopAccepting`'s close
   result and reports it.
2. **No exported test setter.** Both orderings are forced in `internal/api` through the production
   setup path (`ListenAndServe` itself, through an unexported hook on the bound listener). SO6
   becomes a smaller daemon test: establish a live HTTP request, then require every node to drain
   under the strict `stopGen`. SO6 is an integration characterization, with no deterministic
   reversion claimed.
3. W-0017 A closes; W-0022 stays open.

**Amended plan (tests first):**
1. Add the unexported listener hook (`nil` in production) applied in `ListenAndServe` after the
   bind, and rewrite the reproducer onto it. SO1 and SO2 then go through `ListenAndServe`,
   `StopAccepting` and `Shutdown` exactly as the daemon runs them.
2. SO3a/SO3b: a genuine first-close error is reported by `Shutdown`, with `Serve` untracked first
   and with `Serve` still tracking. SO4: a drain timeout returns `context.DeadlineExceeded`. SO5
   (characterization): an in-flight request completes and drains.
3. Implement: a close-once wrapper around the bound listener (a later close neither repeats the
   close nor its error); `StopAccepting` records its close result; `Shutdown` reports the drain
   result and the recorded close error together (`errors.Join`, so neither is lost).
4. SO6 (`cmd/smd`): a live request, then strict `stopGen`.
5. Reversions: SO2 against the close-once removed; SO3a against the recording removed; SO4 against
   a `Shutdown` that drops the drain error. None for SO6.

## Built (2026-10-13; uncommitted)

- `internal/api/server.go`:
  - `ListenAndServe` wraps the bound listener in `closeOnceListener`. Only the first `Close` closes
    it; every later one returns nil, because the first caller has the result.
  - `StopAccepting` records its close result in `closeErr`, under `listenerMu`.
  - `Shutdown` returns `errors.Join(<http.Server.Shutdown's result>, closeErr)`, so neither a drain
    error or timeout nor the listener's close error is lost.
  - The unexported `listenerHook` (nil in production) wraps the bound listener after the close-once
    wrapper, so the tests drive the real `ListenAndServe` / `StopAccepting` / `Shutdown` sequence.
- Tests (`internal/api/server_shutdown_order_test.go`), all through `ListenAndServe`:
  - SO1 `Serve` untracks first, and SO2 `Shutdown` while it still tracks the listener: both
    return nil.
  - SO3a/SO3b: a genuine first-close error is reported by `Shutdown` in both orderings.
  - SO4: a drain timeout returns `context.DeadlineExceeded`.
  - SO5: an in-flight request completes with 200 and `Shutdown` waits for it.
  - The existing port-release, 503-while-draining and shutdown-logging tests are unchanged and
    green.
- RED: SO2 failed (`close tcp …: use of closed network connection`), as did SO3a (`Shutdown` =
  nil) and SO3b (the second close's error instead of the genuine one). SO1, SO4 and SO5 passed
  before the fix (characterizations).
- SO6 (`cmd/smd/lifecycle_http_shutdown_test.go`, integration characterization; no reversion
  claimed, as ruled): with the hub's SSE stream open, the strict `stopGen` drains every node, and
  the stream ends with the generation. `-race -count=10`.
- Reversions, each file restored and hash-verified:
  - R1, no close-once wrapper: SO2 fails with "use of closed network connection".
  - R2, `Shutdown` not reporting the recorded close error: SO3a fails (nil).
  - R3, `Shutdown` dropping the drain error: SO4 fails (nil, not `DeadlineExceeded`).
  - R4, `StopAccepting` not recording its close error: SO3a fails (nil).
- The CI-failing `TestIdentityWire_SW6_NoRetryWhileHeld` passed `-race -count=20` (stability
  evidence, not a proof).
- Gates, all exit 0: gofmt (whole tree), `go vet ./...`, `go test ./...`, `-race` on
  `internal/api` and `cmd/smd`, the SO tests `-race -count=10`, maintainability, `task ci:local`
  (dist restored), and the context gate. No canonical reference changes: shutdown ordering is not
  part of the HTTP or configuration contract.

## Operator review (2026-10-13): two P2 findings, fixed

1. The close error was kept only when `StopAccepting` received it. When `Shutdown` closed first (the
   supported rollback path) and then timed out, net/http returned only the deadline, and the close
   error was lost. The result now lives in the close-once owner (`closeOnceListener.result`).
   `Shutdown` reports it whichever caller closed first (`s.boundListener`), unless the drain error
   already carries it. `StopAccepting` no longer records anything itself.
   - New test SO7: `Shutdown` closes first, the close fails (injected beneath the owner through
     `innerListenerHook`), and the drain times out. Both the close error and
     `context.DeadlineExceeded` must be reported, checked with `errors.Is`. SO3 now injects its error
     beneath the owner too, as a real close would fail.
   - The fix came before SO7 this time. Its RED is R5, which restores the earlier rule (a close
     error reported only after `StopAccepting`): SO7 then fails with the deadline alone.
2. SO4 and SO5 could hang: a failed `/block` request never signalled its handler entry. They now
   select on handler entry, request failure or a 5 s bound (`enterHandler`). `serving` always
   closes the listener and releases every hold at cleanup. SO7 releases the gate before its
   `Shutdown`, which is the only close there and waits for `Serve` with no deadline. R6 points the
   request at a route that never blocks, and the test fails fast ("ended (200) before reaching its
   handler") instead of hanging.
- Reversions after the fix, each file restored and hash-verified:
  - R1, the owner out of the chain: SO2 fails.
  - R2, `Shutdown` not reporting the result: SO3a fails.
  - R3, `Shutdown` dropping its drain error: SO4 fails.
  - R5, a close error reported only after `StopAccepting`: SO7 fails.
  - R6, a request that never reaches its handler: fails fast.
- SO1–SO5, SO7 and the existing port-release, 503-while-draining and shutdown-logging tests ran
  `-race -count=10`.
- Gates after the fixes, all exit 0: gofmt (whole tree), `go vet ./...`, `go test ./...`, `-race`
  on `internal/api` and `cmd/smd`, SO6 and SW6 `-race -count=10`, `git diff --check`,
  maintainability, `task ci:local` (dist restored), the docs catalog check and the context gate.
- Approved by the operator and committed 2026-10-13 as `c1b9fbbd` (`internal/api/server.go` and the
  two test files), then this dossier with ADR 0093, the catalog, backlog, `docs/current.md` and the
  W-0017 closure note. The clean-room review of `c1b9fbbd` found no actionable findings: it ran the
  SO tests under `-race`, but its sandbox could not run SO6, because the temporary directory's
  ownership fails the Unix socket ancestry policy. SO6 passed locally `-race -count=10`. Review
  closed and the file deleted.

## References

- [ADR 0093](../decisions/0093-http-listener-closes-once-and-keeps-its-result.md)
- `internal/api/server.go` (`ListenAndServe`, `StopAccepting`, `Shutdown`, `closeOnceListener`)
- `cmd/smd/lifecycle_adapters.go` (`httpStopAccepting`, `stopHTTP`)
- net/http go1.26 `server.go`: `Serve` (close-once wrapper, listener tracking), `Shutdown`
