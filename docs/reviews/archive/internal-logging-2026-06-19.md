# internal/logging code review - 2026-06-19

Scope: fresh review of `internal/logging` as a new codebase, plus the closest
callers and contracts in `cmd/smd`, `internal/config`, `internal/errors`,
`internal/forwarding/worker`, and the `types.LoggingConfig` shape. Reviewed at
`d9d9c4ed`.

Focus areas: correctness, performance, security, test coverage, and
documentation. This is a review artifact only; no production code was changed.

## Summary

`internal/logging` is a small zerolog wrapper that adds structured-only logging,
lumberjack rotation, context loggers, error-chain enrichment, and shutdown
tracking for in-flight log events. The everyday startup path through `cmd/smd`
is clear: config is loaded, the logger is initialized by DI, and the logger is
closed last so later subsystem defers can still report errors.

The highest-risk issues are in the lifecycle/shutdown contract. The primary
event builder still has a narrow WaitGroup ordering race with `Close`, the
timeout path can close the rotating writer while pre-built events can still
write to it, and terminal methods are not idempotent. The package also does not
prove at initialization time that the configured log file can actually be
opened, which weakens startup observability.

## Findings

### H1 - The primary event builder can call WaitGroup.Add concurrently with Close.Wait

`logEventBuilder` does a fast initialized/logger check, then increments
`activeOps` and calls `s.wg.Add(1)` before it takes `s.mu.RLock()`
(`internal/logging/helper.go:74-107`). `Close` takes `s.mu.Lock()`, flips
`isInitialized=false`, stores a nil logger, releases the lock, and then calls
`waitTimeout(&s.wg, ...)` (`internal/logging/service.go:171-202`).

That leaves this interleaving:

1. A logging goroutine observes `isInitialized=true` and a non-nil logger.
2. `Close` acquires the write lock, marks the service closed, releases the lock,
   and starts waiting on a WaitGroup whose counter may still be zero.
3. The logging goroutine resumes and calls `wg.Add(1)` before it can acquire the
   read lock and discover that the service is closed.

Positive `WaitGroup.Add` calls that start from zero must happen before `Wait`;
doing them concurrently is a documented misuse and can panic with
`sync: WaitGroup misuse: Add called concurrently with Wait`. Even when it does
not panic, `Close` can return before that goroutine has unwound its no-op event
path. The context-logger path avoids this by taking the parent read lock before
adding to the WaitGroup (`internal/logging/event.go:166-191`), so the two paths
currently have different safety properties.

Impact: shutdown can panic or observe inconsistent in-flight operation state
under a close/log race. This is a correctness issue in a cross-cutting service.

Recommendation: make the primary builder follow the context-builder ordering:
after the fast disabled-level check, acquire `s.mu.RLock()`, re-check
initialized/logger/level, then call `wg.Add(1)` while the read lock prevents
`Close` from starting its wait. Add a targeted stress test that coordinates a
goroutine between the fast check and `wg.Add` while another goroutine calls
`Close`.

### M1 - Initialize can succeed even when the file log cannot be opened

`Initialize` validates config and creates the log directory
(`internal/logging/service.go:67-101`), then builds the writer list
(`internal/logging/service.go:103-110`). For file logging,
`initializeWriters` only creates and stores a `lumberjack.Logger`
(`internal/logging/internal.go:34-48`). Lumberjack opens the actual file lazily
on the first `Write`, not during logger construction.

Impact: an existing but unwritable log directory, unwritable log file, bad
ownership, or similar filesystem problem can let `Initialize` return nil and
allow daemon startup to continue. The first real log write may fail inside the
writer path after the service has already reported itself initialized. Zerolog
writer errors are not surfaced back through the `InfoWith().Msg(...)` API, so
the operator can silently lose the startup log that would diagnose the problem.

Recommendation: when file logging is enabled, proactively prove the target is
writable during `Initialize`. Use the same final filename that lumberjack will
use, open it with create/append permissions, close it, and return a startup
error on failure. Cover an existing unwritable log directory/file in tests.

### M2 - Close timeout can allow late events to write after the writer is closed

`Close` deliberately returns after a bounded drain timeout and documents that
stragglers may complete later (`internal/logging/service.go:149-160`). After the
timeout check, it closes `fileWriter` unconditionally when present
(`internal/logging/service.go:199-228`). A pre-built `zerolog.Event` still
contains the original multi-writer. If the event's terminal method runs after
`Close` has closed the lumberjack writer, lumberjack's next write can reopen the
file because `Close` leaves its internal file pointer nil.

Impact: a late event can write after `Service.Close` has returned and can leave
a reopened log file outside the service's later close path. In the daemon this
usually occurs near process exit, but the package advertises a reusable service
with explicit close semantics and is used by tests and package-level helpers.

Recommendation: choose and enforce one timeout policy. If `Close` returns while
events are still outstanding, terminal methods should drop late writes after the
service is closed, or the service should keep the writer open and document that
the timeout path is process-exit-only. A close-aware writer wrapper or an event
terminal check against a closed flag would make the contract explicit. Add a
test that creates an event, forces a close timeout, calls `Msg` afterward, and
asserts that no file is reopened or written after close.

### M3 - Calling a terminal method twice can underflow the WaitGroup

Tracked events decrement `activeOps` and call `wg.Done()` in `finish`
(`internal/logging/event.go:417-424`). All three terminal methods defer
`finish()` (`internal/logging/event.go:426-445`). Nothing prevents a caller from
calling two terminal methods on the same `LogEvent`, or calling the same
terminal method twice.

Impact: the second terminal call on a tracked event calls `wg.Done()` again and
can panic with `sync: negative WaitGroup counter`. The interface documentation
says every chain must end in a terminal method, but it does not make the
"exactly once" invariant enforceable (`internal/logging/event.go:43-57`). Since
the logger is used pervasively, one accidental double-finalization can crash the
process instead of producing a duplicate or ignored log event.

Recommendation: make `finish` idempotent for tracked events. The low-overhead
option is to clear the service pointer before calling `wg.Done()` for the common
single-goroutine case; a `sync.Once` or small atomic guard is safer if events
may ever be shared. Add tests for `Msg` followed by `Send`, repeated `Msg`, and
the same cases on context loggers.

### L1 - Error-chain documentation describes local frames, but implementation emits recursive frames

The package documentation says `error_chain` is an array of frame messages from
outermost to root and shows local messages like `"startup failed"` and
`"failed to connect to database"` (`internal/logging/doc.go:24-58`). The
implementation calls `dErr.Error()` for every `DetailedError` frame
(`internal/logging/helper.go:31-37`). `DetailedError.Error()` intentionally
returns the local frame plus the entire remaining cause chain
(`internal/errors/errors.go:51-87`), and the regression test now codifies that
recursive shape (`internal/logging/error_chain_test.go:17-40`).

Impact: the docs teach a more compact and more useful shape than the package
actually emits. The current output duplicates the tail of the chain at every
DetailedError frame, increasing log size and allocation work. The clean
benchmark run shows the spread: an enabled `InfoWith` event is about
`188 ns/op, 32 B/op, 1 alloc/op`, while a depth-3 enriched error event is about
`1428 ns/op, 840 B/op, 18 allocs/op`.

Recommendation: either update the docs to state that `error_chain` entries are
recursive `Error()` strings, or preferably add a local-frame accessor to
`internal/errors.DetailedError` and emit the documented compact shape. Add an
assertion for the exact JSON values emitted by `Err` and `AnErr`, not just field
presence.

### L2 - Review-history links in comments point at a non-existent current document

Several comments cite `docs/reviews/internal-logging.md` for prior finding
numbers (`internal/logging/service.go:160`, `internal/logging/helper.go:88`,
`internal/logging/helper.go:146`, `internal/logging/doc.go:67-68`,
`internal/logging/debug_tracking_nop.go:12-14`). In the current tree, the
historical review is under `docs/reviews/archive/internal-logging.md`, and this
fresh artifact is dated as `docs/reviews/internal-logging-2026-06-19.md`.

Impact: future maintainers following those comments land on a missing path or
the wrong review generation. This matters because several behavior choices are
justified only by those comments, including shutdown-drain behavior, disabled
level short-circuiting, and debug-only location tracking.

Recommendation: update comments to cite durable dated artifacts, or add a small
current `docs/reviews/internal-logging.md` index that points to the dated review
history. Keep the comments self-contained enough that reading the review is not
required to understand the invariant.

## Security notes

I did not find a network-facing security boundary in `internal/logging`. The
main security-relevant property is observability: file logging must fail fast
when the configured target cannot be written, and error enrichment should not be
treated as a redaction boundary. New log files are created by lumberjack with
owner-only permissions, and the service-created log directory uses `0750`, which
is reasonable for local daemon logs.

## Test coverage notes

The package has useful coverage for initialization, close behavior, no-op paths,
context loggers, concurrent logging, error-chain enrichment, WaitGroup balance,
the disabled-level fast path, and both normal and `logging_debug` race builds.

Important missing coverage:

- deterministic close/log interleavings around `wg.Add` and `wg.Wait`
- file logging enabled with an existing unwritable log target
- late terminal call after `Close` times out
- double terminal calls on one `LogEvent`
- exact `Err` and `AnErr` JSON values for recursive versus local-frame chain
  shape
- a documentation or unit test around whether a closed service may be
  re-initialized

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test ./internal/logging ./internal/config ./internal/errors ./internal/forwarding/worker`
- `GOCACHE=/tmp/go-build go test -race ./internal/logging`
- `GOCACHE=/tmp/go-build go test -race -tags logging_debug ./internal/logging`
- `GOCACHE=/tmp/go-build go vet ./internal/logging ./internal/config ./internal/errors ./internal/forwarding/worker`
- `GOCACHE=/tmp/go-build go test -run '^$' -bench 'Benchmark(InfoWith_NoErr|Parallel_InfoWith|ErrorWith_DetailedChain3)' -benchtime=100ms ./internal/logging`

All of the commands above passed.

Broader caller verification with `./cmd/smd` and `./internal/ft8` is currently
blocked by an unrelated worktree build error:

- `internal/ft8/servicetx.go:6:2: "math" imported and not used`

Benchmark rows from the clean benchmark run:

- `BenchmarkInfoWith_NoErr-8`: `188.2 ns/op`, `32 B/op`, `1 allocs/op`
- `BenchmarkErrorWith_DetailedChain3-8`: `1428 ns/op`, `840 B/op`, `18 allocs/op`
- `BenchmarkParallel_InfoWith-8`: `113.9 ns/op`, `32 B/op`, `1 allocs/op`

## Resolution (2026-06-19)

All six findings fixed (these are real concurrency/lifecycle correctness in a
pervasively-used service, not contract hardening — nothing deferred). The
`internal/ft8` `"math" imported and not used` build error noted under
Verification was a stale-worktree artifact; `math` is used by the FT8 M1 offset
gate in the current tree and `./cmd/smd` builds + passes.

- **H1 (fixed).** `logEventBuilder` now acquires `s.mu.RLock()` and re-checks
  initialized/logger/level FIRST, then calls `s.activeOps.Add(1)` / `s.wg.Add(1)`
  while still holding the read lock — so `wg.Add` can never run concurrently with
  Close's `wg.Wait` (the read lock blocks Close from starting its wait). This
  mirrors the already-correct context-builder. The three early-return paths no
  longer add counters, so `releaseCounters` is gone. Test:
  `TestLogBuilder_ConcurrentWithClose_NoPanic` (runs under `-race -short`).
- **M1 (fixed).** `Initialize` proactively proves the file target is writable
  (`probeLogFileWritable` opens the lumberjack filename with create/append/0600
  and closes it), returning a startup error instead of letting lumberjack's lazy
  open fail at the first log line. Test:
  `TestInitialize_FailsWhenLogFileUnwritable` (root-proof via an EISDIR collision,
  not chmod).
- **M2 (fixed).** `Close` now closes the file writer ONLY on a successful drain;
  on a drain timeout it leaves lumberjack open so a straggler's terminal write
  lands safely instead of reopening a closed file. A timed-out Close is the
  process-exit path (OS reclaims the fd; `initOnce` prevents re-init). Test:
  `TestClose_LateEventAfterTimeoutWritesSafely`.
- **M3 (fixed).** `finish` is idempotent: it clears BOTH `e.service` (prevents a
  double `wg.Done()` → negative-counter panic) and `e.event` (prevents a second
  `e.event.Msg()` touching the pooled/recycled zerolog event → index-out-of-range
  panic). Test: `TestFinish_IdempotentOnDoubleTerminal` (Msg twice + Msg/Send).
- **L1 (fixed, docs).** `doc.go` now documents the ACTUAL recursive `error_chain`
  shape (each entry is the frame's full `Error()` = its message plus the cause
  tail) with a matching example — rather than the compact local-frame shape it
  used to claim. (Kept the recursive shape; the compact-shape optimisation the
  review floated as an alternative is a possible future change to
  `internal/errors`, not needed for a single-operator daemon's log volume.) The
  recursive values are already pinned by `error_chain_test.go`.
- **L2 (fixed, docs).** All in-tree comments citing `docs/reviews/internal-logging.md`
  now point at `docs/reviews/archive/internal-logging.md` (where the historical
  review with findings 4.x lives) — six call sites across service/helper/event/
  doc/debug_tracking_nop + two in the test file.

Verified: `gofmt`/`go vet` clean; `internal/logging` passes under the normal
build, `-race`, and `-race -tags logging_debug`; callers (`config`, `errors`,
`forwarding/worker`) and `./cmd/smd` pass.
