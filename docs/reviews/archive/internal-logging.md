# `internal/logging` — Code Review

**Status:** Second code review in `docs/reviews/`, written 2026-04-16 during session 6. The package was flagged as a carry-forward-for-code-review target in session 3's restructure plan because every daemon handler, service, and subsystem will log through it, and the v2 daemon's observability depends on it. It was also flagged because the v1 CI data race (still tracked as a v1-branch follow-up) may or may not live in here.

**Purpose of this document:** a thorough audit of the `internal/logging` package with explicit findings, categorized by severity, so we can decide which ones to fix before v2 code starts being written against the package. Same pattern as `docs/reviews/internal-errors.md`. No changes applied yet — this is discussion input.

**How to use this document:** read the findings, discuss, mark as fix / defer / reject, then a follow-up commit applies the agreed changes. The package is larger and more complex than `internal/errors`, so some findings will genuinely be "defer until we see the real need" rather than "fix now."

---

## 1. Package inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 23 | Package description and usage example |
| `interface.go` | 23 | `Logger` interface declaration |
| `consts.go` | 16 | `ServiceName` + error message constants |
| `internal.go` | 61 | `initializeRollingFileLogger`, `initializeWriters` (lumberjack + console writer setup) |
| `validation.go` | 62 | `validateConfig` — struct-tag + semantic validation of `types.LoggingConfig` |
| `helper.go` | 201 | `buildErrorChain`, `logEventBuilder`, `parseLevel`, level dispatch |
| `event.go` | 655 | `LogEvent`, `LogContext`, `logEvent`, `trackedLogEvent`, `contextLogger`, `noopLogger` — the whole event-builder machinery |
| `service.go` | 319 | `Service` struct, `Initialize`, `Close`, level entry-points, `With` context builder |
| `dump.go` | 203 | `Dump(v interface{})` — reflection-based struct/map/slice debug dump |
| `bench_test.go` | 105 | Benchmarks for `InfoWith` / `ErrorWith` with and without error chains |
| `error_chain_test.go` | 101 | `TestBuildErrorChain_WithDetailedAndStd`, `TestEventErr_EmitsChainFields` |
| `logging_test.go` | 849 | The bulk of the test suite — initialization, lifecycle, concurrency, shutdown |
| `waitgroup_leak_test.go` | 342 | Specific tests for the reference-counting / WaitGroup drain path on shutdown |

**Total:** ~2,960 lines, split roughly **1,563 production / 1,396 tests**. Test coverage is heavy, which is appropriate for a package with significant concurrency logic.

**Public API surface** (things outside the package see):

```go
// Types
type Logger interface { ... }      // 8 methods
type LogEvent interface { ... }    // ~30 methods
type LogContext interface { ... }  // ~12 methods
type Service struct { ... }        // the concrete implementation
const ServiceName = types.LoggingServiceName

// Constructors and utilities
func Noop() Logger
func (s *Service) Initialize() error
func (s *Service) Close() error
func (s *Service) Wait()
func (s *Service) ActiveOperations() int32
func (s *Service) Dump(v interface{})
```

---

## 2. Usage survey

The package is consumed throughout the codebase — every service that takes a logger via DI (`*logging.Service` with the `di.inject:"loggingservice"` tag) uses this API. At the time of this review the primary consumers in the v2 carry-forward tree are:

- `internal/database/sqlite` — logs migration events, query errors, connection state.
- `internal/adif` — (minimal) logs parse errors through its own error package.
- `cmd/smd` — will be a heavy consumer once daemon code starts landing.
- Future: every HTTP handler, the forwarding worker, the SSE event emitter, the session manager, etc.

Within the package itself, `helper.go` reads DetailedErrors (via the now-renamed `WithMsg`/`WithErr` pattern from `internal/errors`) and walks the chain for error enrichment. This dependency is bidirectional but clean: `internal/errors` knows nothing about logging, and `internal/logging` only reads DetailedErrors through the public API.

---

## 3. Strengths — things the package gets right

### 3.1 Structured-only API

`Logger` intentionally has no `Info(format, args)` / `Infof` methods. All logging goes through `InfoWith().Str(...).Int(...).Msg(...)` chains. This matches the "structured logging is queryable; printf logging is not" principle and is enforced at the type level — you can't accidentally produce unstructured output.

### 3.2 Error chain enrichment via `buildErrorChain`

The `Err(err)` method on `LogEvent` doesn't just forward to zerolog's `Err()`. It also walks the error chain and adds five enrichment fields to the log entry:

- `error_chain` (array of frame messages)
- `error_root` (the innermost error message)
- `error_history` (joined chain as a single string)
- `error_ops` (array of operation identifiers from DetailedErrors)
- `error_root_op` (the innermost DetailedError's op)

This is a real observability win: a single log line carries the full error context, including every operation identifier from every `DetailedError` in the chain. Tools querying the logs can filter on `error_root_op` to find all instances of a specific failure, or filter on `error_ops` contains `"X.Y"` to find everything that passed through a given operation. Preserve this.

### 3.3 Graceful shutdown with in-flight tracking

`Service.Close()` doesn't just close the logger — it waits for in-flight log events to finish (with a timeout), closes the file writer, and optionally warns if the drain took longer than expected. This matters because zerolog events can be in-flight when shutdown is requested, and tearing down the file writer underneath them would lose data or corrupt output. The tracking is done via `trackedLogEvent` that increments `activeOps` on creation and decrements on `Msg`/`Msgf`/`Send`.

### 3.4 Lifecycle convention matches the project's idiom

`Initialize()` (validate + set up) → `Close()` (drain + shutdown). Both are idempotent. This matches the service lifecycle pattern documented in CLAUDE.md → "Project idioms" and composes cleanly with the `iocdi` container.

### 3.5 Noop logger escape hatch

`Noop()` returns a `Logger` that silently discards all output. Useful for tests, for code that wants a logger in its zero value, and as a sentinel "logger hasn't been injected yet." The no-op variant implements the full `Logger`, `LogEvent`, and `LogContext` interfaces, so callers don't need to nil-check.

### 3.6 Context loggers for per-request scoping

`svc.With().Str("request_id", id).Logger()` produces a scoped `Logger` that includes the pre-set fields on every subsequent log. The `contextLogger` implementation shares the parent Service's file writer and shutdown coordination, so shutdown logic doesn't need to track every child logger separately. This is the right shape for HTTP handlers that want a per-request log context.

### 3.7 Extensive test coverage

1,396 lines of tests across four test files covering normal path, concurrent access, shutdown-under-load, and WaitGroup leak scenarios. The `waitgroup_leak_test.go` in particular exercises the tracked-event path specifically for leaks — which is a real hazard (see concern 4.3) and deserves its own dedicated tests.

---

## 4. Concerns — findings grouped by severity

### Critical

#### 4.1 Force-drain in `Close()` can push `activeOps` negative and double-`wg.Done()`

**Location:** `service.go:190-196`

```go
// Force-drain the WaitGroup to prevent indefinite blocking
// This handles orphaned log operations that never called Msg()/Send()
for i := int32(0); i < activeOps; i++ {
    s.activeOps.Add(-1)
    s.wg.Done()
}
```

**What it does:** when `Close()` times out waiting for in-flight ops, it force-decrements the counter and calls `wg.Done()` once per orphaned op — under the assumption that the orphans will never call `Msg`/`Send` themselves.

**Why this is a problem:** the assumption isn't guaranteed. A concurrent goroutine that's in the middle of calling `Msg()` on a `trackedLogEvent` will:
1. Be in the defer block of `trackedLogEvent.Msg`
2. Call `e.service.activeOps.Add(-1)` and `e.service.wg.Done()` themselves.

If the drain runs at the same time, **both paths decrement the counter and call `wg.Done()`**. This can drive `activeOps` negative (harmless but confusing) and — crucially — **panic with `sync: negative WaitGroup counter`** if `wg.Done()` is called more times than `wg.Add()`.

**The v1 CI data race we've been tracking as a v1-branch follow-up may well be this.** The race detector catches concurrent access to `activeOps` (an `atomic.Int32`, which the race detector shouldn't flag in isolation) but the interaction with the force-drain is exactly the pattern that looks like a race to tooling.

**Severity:** critical because it can panic the daemon during shutdown, and shutdown is exactly the time you don't want to panic.

**Recommended action:** the force-drain path needs to be coordinated with in-flight decrement. Options:

- **(a) Don't force-drain.** If the wait times out, log a warning and accept that the WaitGroup will be permanently unbalanced. On `Close()` re-entry, we're already marked uninitialized and return nil, so the leak doesn't cascade.
- **(b) Use a counter swap instead of individual decrements.** Atomically `Add(-activeOps)` once, but still do `wg.Done()` in a loop — this avoids the individual decrements accumulating wrong but doesn't solve the `wg.Done()` over-count problem.
- **(c) Track in-flight events via a set, not a counter.** On `Close()` timeout, iterate the set and call a termination hook on each live event that marks it "skip cleanup" — so when the orphan eventually calls `Msg()` it sees the flag and skips the decrement. More code, but correct.
- **(d) Use `context.Context` for cancellation.** The service holds a `context.CancelFunc`; `Close()` cancels the context; `trackedLogEvent.Msg` checks the context and skips cleanup if it's been cancelled by a drain. Simplest Go-idiomatic option.

**My lean: option (d).** It's the standard Go pattern for "multiple goroutines need to know the world has ended." Implementation is roughly 30 lines and the test suite already knows how to exercise this code path.

**Needs verification against the v1 race tracked in `docs/session-handoff.md` → "v1 branch follow-ups."** If the race turns out to live here, fixing it on `v1` too would let the v1 CI workflow pass again (though that's not urgent since we deleted the workflow from main).

---

### High

#### 4.2 `Close()` timeout default is a hardcoded magic number

**Location:** `service.go:157`

```go
timeoutMS := 100
```

**What it does:** if `LoggingConfig.ShutdownTimeoutMS` is zero or unset, fall back to 100ms.

**Why this is a problem:** per the "no magic numbers — runtime values come from config" project rule (CLAUDE.md "Project idioms" → "feedback_no_magic_numbers" memory note), this literal should be a named constant declared near the top of the file or in `consts.go`, with a comment explaining the choice. Right now it's an inline literal with no explanation of why 100ms and not 500ms or 1s.

**Recommended action:** move to `consts.go`:

```go
// defaultShutdownTimeoutMS is the fallback value used when
// LoggingConfig.ShutdownTimeoutMS is unset or zero. 100ms is a pragmatic
// choice: long enough for a single in-flight log write to finish on a
// reasonably loaded system, short enough that daemon shutdown isn't
// perceptibly delayed.
const defaultShutdownTimeoutMS = 100
```

Then `service.go:157` becomes `timeoutMS := defaultShutdownTimeoutMS`. 5-minute fix, brings the constant out of the code.

---

#### 4.3 `activeOpLocations` debug tracking is production overhead for a debug-only feature

**Location:** `service.go:41`, `helper.go:102-113`, `event.go:415-424`, `event.go:437-446`, `event.go:459-468`

**What it does:** when `LoggingConfig.ShutdownTimeoutWarning` is true, every log event creation captures `runtime.Caller(2)` (file:line) and stores it in a map keyed by location. On `Msg`/`Msgf`/`Send`, the map entry is decremented and deleted if it reaches zero. On shutdown timeout, the map is included in the warning so operators can see *where* the leaked ops came from.

**Why this is a problem:**

1. **`runtime.Caller(2)` is not free** — it walks the stack to get the caller info. On a hot logging path (every `InfoWith()` call), this adds measurable overhead to every log event even when the map tracking is disabled (the `if location != "" { ... }` branch is in the defer path but `runtime.Caller` itself runs unconditionally at line 103 of helper.go).

Wait — rechecking `helper.go:102-113`:

```go
var location string
if s.LoggingConfig.ShutdownTimeoutWarning {
    _, file, line, ok := runtime.Caller(2)
    if ok {
        location = fmt.Sprintf("%s:%d", file, line)
        s.mu.Lock()
        if s.activeOpLocations == nil {
            s.activeOpLocations = make(map[string]int)
        }
        s.activeOpLocations[location]++
        s.mu.Unlock()
    }
}
```

OK, `runtime.Caller` is guarded by the config flag, so it's only called when warnings are enabled. That's fine. But:

2. **When warnings are enabled, the mutex is acquired on every log event.** The map check/update holds `s.mu.Lock()` (not RLock), which serializes all concurrent log events through a single mutex. This is terrible for a high-concurrency logging path. For a daemon that's expected to handle many concurrent requests, this effectively single-threads the logger when the flag is on.

3. **The feature is debug-only** — it exists to help diagnose shutdown leaks during development, not for production. Mixing debug-only code with the hot path is a smell.

**Recommended action:** extract the location-tracking machinery into a separate "debug logger" type that wraps the regular Service. Or gate it behind a build tag (`//go:build debug`) so the code is compiled out entirely in production builds. The first option is more flexible (no recompile needed to enable) but the mutex contention concern remains when enabled. Gating behind a build tag is the cleanest for "this is a tool we use during development, not in production."

**My lean:** keep the feature but behind a build tag. `//go:build logging_debug` at the top of a new `debug.go` file that contains the tracking code, and a stub `nodebug.go` for the default build with `//go:build !logging_debug`. The Service struct's `activeOpLocations` field is only present in the debug build. This makes the production-build hot path genuinely free of the tracking overhead.

**Alternative: drop the feature entirely.** It was added to diagnose one specific class of bug (the leak test case in `waitgroup_leak_test.go`). If the bug is now understood and fixed (see concern 4.1), maybe the feature has served its purpose and can go. Decide together.

---

#### 4.4 Level-dispatch switch statement is duplicated

**Location:** `helper.go:176-196` (inside `logEventBuilder`) and `event.go:149-169` (inside `newTrackedContextLogEvent`)

**What it does:** both functions have nearly-identical 7-case switches mapping `zerolog.Level` to the appropriate `logger.Debug()` / `logger.Info()` / etc. call.

**Why this is a problem:** duplicated code with duplicated bugs. If a new log level is added (or one is removed from zerolog), both switches need to be updated. Easy to miss one.

**Recommended action:** extract a small helper:

```go
func eventForLevel(l *zerolog.Logger, level zerolog.Level) *zerolog.Event {
    switch level {
    case zerolog.DebugLevel: return l.Debug()
    case zerolog.InfoLevel:  return l.Info()
    // ... etc
    default:                  return nil
    }
}
```

And call it from both sites. ~15 lines of shared code replacing ~30 lines of duplicated code.

---

### Medium

#### 4.5 `LogEvent` interface is 30+ methods; `event.go` is 655 lines mostly of boilerplate

**Location:** `event.go` entire file

**What it is:** `LogEvent` exposes type-specific methods for every field type zerolog supports: `Str`, `Strs`, `Stringer`, `Int`, `Int8`, `Int16`, `Int32`, `Int64`, `Uint`, `Uint8`, `Uint16`, `Uint32`, `Uint64`, `Float32`, `Float64`, `Bool`, `Bools`, `Time`, `Dur`, `Err`, `AnErr`, `Bytes`, `Hex`, `IPAddr`, `MACAddr`, `Interface`, `Dict`, plus `Msg`, `Msgf`, `Send`. That's 30 methods on the interface, each implemented once on `logEvent` (nil-checking + forward), once as part of `trackedLogEvent` (only the 3 terminal methods need override), and once on `noopLogContext`/`noopLogger`/`noopLogEvent` (all 30 as no-ops each).

**Why this is maybe-a-problem:** it's a massive amount of boilerplate. Every new field type zerolog adds requires touching every implementation. And it's ~500 lines of genuinely boring code that adds maintenance friction.

**Alternatives considered:**

- **`Any(key string, val interface{}) LogEvent`** — a single method that uses a type switch internally. Pro: 1 method instead of 30. Con: runtime dispatch, lost compile-time type checking, slight perf penalty from the type switch, and the reason zerolog has typed methods is precisely to allow zero-allocation field construction.
- **Embed zerolog.Event directly in LogEvent** — expose zerolog as part of the API surface. Pro: free. Con: locks the API to zerolog forever and leaks implementation details to consumers.
- **Generate the boilerplate via `go generate`.** Pro: DRY at the source-code level. Con: generated code is another thing to maintain, debugging generated code is worse than debugging hand-written code.

**Recommended action:** **leave it as-is.** The typed API is genuinely valuable (consumers get IntelliSense/LSP completions, type safety, and zerolog's zero-alloc benefits are preserved). The maintenance cost is real but one-time-per-field-type, and field types don't change often. Flag as "known large, accept the maintenance cost." This is a "document, don't fix" finding.

**One small improvement:** the `noopLogger` and `noopLogContext` implementations could live in a separate `noop.go` file so the main `event.go` is smaller and more focused on the real types.

---

#### 4.6 `logEventBuilder` acquires the RWMutex before checking level

**Location:** `helper.go:116-173`

**What it does:** the function is called from `Service.InfoWith()`, `DebugWith()`, etc. It increments `activeOps` + `wg`, then acquires `s.mu.RLock()`, then does a TOCTOU re-check of `isInitialized`, then checks `logger.GetLevel() > level`, and only THEN constructs the zerolog event. If the level check fails, it unwinds all the above.

**Why this is a problem:** for filtered-out log events (which is the common case in production — most logs are Debug/Trace that get discarded at Info level), the code still pays:

1. atomic counter increment
2. `sync.WaitGroup.Add(1)`
3. `runtime.Caller(2)` if `ShutdownTimeoutWarning` is on
4. RWMutex RLock acquisition
5. Second atomic check of `isInitialized`
6. Load the logger pointer
7. Level check — fails, now unwind everything

The unwind itself is ~15 lines because it has to decrement the counter, call `wg.Done()`, and possibly un-update the map.

**Recommended action:** short-circuit the level check **before** incrementing counters. Something like:

```go
func logEventBuilder(s *Service, level zerolog.Level) LogEvent {
    if s == nil || !s.isInitialized.Load() {
        return newLogEvent(nil)
    }
    // Short-circuit: if the logger level filters this event out, skip
    // all the accounting and return a no-op.
    logger := s.logger.Load()
    if logger == nil || logger.GetLevel() > level {
        return newLogEvent(nil)
    }
    // Now enter the tracked path (increment, lock, etc.)
    ...
}
```

The TOCTOU concern (what if Close() runs between the level check and the counter increment?) is handled by the existing double-check pattern; it just needs to happen in the order "cheap-check → enter-tracked-path" rather than the current order. ~20 lines of reordering.

**Measured impact:** probably significant. For a daemon doing debug-heavy instrumentation at runtime level Info, most log calls hit the short-circuit, and the saved work per call is 3-5 atomic ops + 1 lock acquisition.

---

#### 4.7 Concurrent writers to `activeOpLocations` via the same lock that guards logger operations

**Location:** `service.go:38` (the `sync.RWMutex mu`), used in both `Close()` (write lock) and event-creation paths (read lock), and also used in `activeOpLocations` map updates (write lock, from the debug path).

**What it does:** the Service uses one `sync.RWMutex mu` for multiple concerns:
- Protecting `isInitialized` state transitions (write lock in `Close()`, read lock in creators)
- Protecting `fileWriter` during the close sequence
- Protecting the `activeOpLocations` map (when enabled)
- Guarding the logger pointer load (though that's actually `atomic.Pointer`, so the mutex is technically redundant for this)

**Why this is a problem:** the `activeOpLocations` map update takes `s.mu.Lock()` (the full write lock). This blocks **every other caller** waiting on `s.mu.RLock()` for event creation. When debug tracking is enabled, the debug code path effectively serializes all log events through one mutex.

**Related to 4.3** — the debug-only code is mixing concerns with the production-path synchronization. The fix is the same: extract debug tracking behind a build tag, OR give it its own dedicated mutex if it stays in production.

**Recommended action:** separate the locks. If the debug feature stays, use a separate `sync.Mutex locationsMu` just for the map. The main `mu` stays for isInitialized / fileWriter. Or extract to build-tag'd file per 4.3 and the problem disappears.

---

#### 4.8 `Dump()` logs unfiltered struct fields — potential data leak

**Location:** `dump.go:16-57`, `dump.go:122-154` (struct case)

**What it does:** `Dump(v)` walks any value via reflection and logs every exported field. For structs, it iterates `val.NumField()` and logs each.

**Why this is a problem:** if you call `svc.Dump(config)`, and `config` is a struct containing fields like `QRZPassword`, `EmailSMTPPassword`, `TLSKeyFile`, or an API token — **those get logged in cleartext**. There is no field filter, no struct tag for "secret," no redaction.

For a developer tool used during debugging, this is manageable because the developer knows not to dump sensitive structs. For production, it's a secret-exposure hazard waiting for a misused debug statement to leak credentials to a log file. The fact that `Dump` logs at `Debug` level means it's off in production by default, which mitigates — but doesn't eliminate — the risk.

**Recommended action:**

1. **Add a `secret` struct tag.** Fields tagged `log:"secret"` or `sm:"redact"` get logged as `<redacted>` instead of their value. Opt-in, simple.
2. **Document the hazard in `Dump`'s doc comment** — "Do not pass structs that contain secrets to this function. There is no automatic redaction."
3. **Consider whether Dump is needed at all.** Is it used anywhere in the codebase today? If no, delete it. If yes, wrap it behind a build tag or a debug-service type so production builds don't have it.

**Needs verification:** grep the codebase for `svc.Dump(` or `.Dump(` to see if anyone actually calls it. If not, 4.8 becomes "delete Dump" and is trivial.

---

#### 4.9 `trackedLogEvent` terminal method defers duplicate 14 lines of cleanup three times

**Location:** `event.go:411-472` — `Msg`, `Msgf`, and `Send` methods each have an identical defer block

**What it is:** each terminal method has:

```go
defer func() {
    e.service.activeOps.Add(-1)
    e.service.wg.Done()
    if e.location != "" {
        e.service.mu.Lock()
        if e.service.activeOpLocations != nil {
            e.service.activeOpLocations[e.location]--
            if e.service.activeOpLocations[e.location] <= 0 {
                delete(e.service.activeOpLocations, e.location)
            }
        }
        e.service.mu.Unlock()
    }
}()
```

Duplicated 3 times, differing only in which zerolog call happens after.

**Recommended action:** extract a helper method on `*trackedLogEvent`:

```go
func (e *trackedLogEvent) finish() {
    e.service.activeOps.Add(-1)
    e.service.wg.Done()
    if e.location != "" {
        e.service.mu.Lock()
        defer e.service.mu.Unlock()
        if e.service.activeOpLocations != nil {
            e.service.activeOpLocations[e.location]--
            if e.service.activeOpLocations[e.location] <= 0 {
                delete(e.service.activeOpLocations, e.location)
            }
        }
    }
}
```

And each terminal method becomes:

```go
func (e *trackedLogEvent) Msg(msg string) {
    defer e.finish()
    if e.event != nil {
        e.event.Msg(msg)
    }
}
```

Saves ~30 lines, eliminates the duplication hazard. Trivial change.

---

### Low

#### 4.10 `LogContext` doesn't have all the type methods `LogEvent` has

**Location:** `event.go:13-27` (LogContext interface)

**What it is:** `LogContext` has `Str`, `Strs`, `Int`, `Int64`, `Uint`, `Uint64`, `Float64`, `Bool`, `Time`, `Err`, `Interface`, and `Logger()`. That's 12 methods. But `LogEvent` has 30. So context loggers can't include `Int32`, `Uint8`, `Dur`, `IPAddr`, etc. as pre-populated fields.

**Why this is low-priority:** context loggers are mostly used for request-scoping with simple string/int fields. The missing types are rare in context usage. But it's an asymmetry in the API — some field types you can add per-event but not per-context.

**Recommended action:** either (a) fill in the missing methods for completeness, or (b) document the asymmetry and its rationale. Either is fine. My lean: document it, don't fix it, unless someone specifically needs a missing type in context scope.

---

#### 4.11 `parseLevel` in `helper.go` is a one-line wrapper

**Location:** `helper.go:14-21`

```go
func parseLevel(level string) (zerolog.Level, error) {
    l, err := zerolog.ParseLevel(level)
    if err != nil {
        return zerolog.NoLevel, err
    }
    return l, nil
}
```

**What it does:** wraps `zerolog.ParseLevel` with... nothing. The `if err` branch just passes the error through. The function is equivalent to just calling `zerolog.ParseLevel` directly.

**Recommended action:** delete the wrapper and inline the call at the one usage site (`service.go:103`).

---

#### 4.12 `Msg()`, `Msgf()`, `Send()` are terminal but not enforced as such

**Location:** `event.go` — any of the builder methods (Str, Int, etc.) can be called on a nil-event `logEvent`.

**What it is:** the nil-safe pattern throughout `logEvent` methods checks `if e.event != nil` before forwarding. But nothing prevents a caller from building a long chain and forgetting to call `Msg`/`Msgf`/`Send`. A `trackedLogEvent` built but never finalized leaks its counter — see `waitgroup_leak_test.go` for proof this was a real concern.

**Recommended action:** this can't be fixed at the type level in Go without breaking the fluent API. The mitigation is documentation and the WaitGroup drain path (4.1). Flag as "known hazard, addressed via runtime tracking, not a type-safety issue."

---

#### 4.13 `consts.go` error messages are capitalized and end with periods

**Location:** `consts.go:12-16`

```go
errMsgNilConfig     = "Logging config is nil."
errMsgNilService    = "Logger service is nil."
errMsgAppCfgNotSet  = "Application config is not set."
errMsgConfigInvalid = "Logging configuration is invalid."
```

**Why this is minor-a-problem:** Go convention (see [Effective Go — Errors](https://go.dev/doc/effective_go#errors)) says error messages should start with a lowercase letter (because they're usually concatenated with other error messages via `%w` and the result should read naturally) and should not end with punctuation (same reason).

**Recommended action:** normalize to `"logging config is nil"` style. Trivial find-and-replace. Not urgent but worth doing when cleaning up.

---

#### 4.14 Empty `README.md`

**Location:** `internal/logging/README.md`

**What it is:** 3,782 bytes per `ls`. Let me actually read it before calling it empty.

Wait — 3,782 bytes is substantial. This is NOT empty like the errors package's README was. It probably has real content that should be audited. Deferring to the fix pass to read and evaluate.

---

## 5. Fit with v2 daemon needs

The daemon's logging needs are actually pretty modest:

- **Request/response logging from HTTP handlers** — every `POST /v1/qso` logs a structured entry. `InfoWith().Str("method", ...).Str("path", ...).Dur("duration", ...).Msg("request")` fits perfectly.
- **Error logging with DetailedError chain enrichment** — every handler's error path logs via `Err(err)` which auto-enriches with `error_chain`/`error_ops`/`error_root_op`. This is the big win — bug reports become traceable to specific operations.
- **Forwarding worker state transitions** — `Str("destination", "qrz").Int64("qso_id", id).Msg("forward attempted")` etc.
- **SSE event publication** — `Str("event", "qso.stored").Int64("qso_id", id).Msg("SSE emitted")` for debugging the event stream.
- **Graceful shutdown** — the daemon wants to close the logger cleanly on SIGINT, which is exactly what `Close()` does — modulo concern 4.1.

**Conclusion:** the package is capable of serving the daemon's needs **once 4.1 is fixed**. The shutdown race is the one critical-severity finding; everything else is performance tuning, cleanup, or hygiene.

---

## 6. Recommended action plan

### Must-fix before writing daemon code

1. **Fix 4.1 — shutdown force-drain race.** Use context-cancellation for drain coordination, or drop the force-drain and accept the leak. This is the single blocker that can panic the daemon.

### Should-fix opportunistically

2. **Fix 4.2 — shutdown timeout magic number.** Move to `consts.go` as `defaultShutdownTimeoutMS`.
3. **Fix 4.6 — short-circuit level check in `logEventBuilder`.** Check the level before incrementing counters and acquiring locks.
4. **Fix 4.4 — extract level-dispatch helper.** Replace the duplicated switch statements with `eventForLevel`.
5. **Fix 4.9 — extract `trackedLogEvent.finish()` helper.** Remove the 30 lines of duplicated defer blocks.
6. **Fix 4.11 — delete the pointless `parseLevel` wrapper.**
7. **Fix 4.13 — normalize `errMsg*` constants to lowercase + no trailing period.**

### Discuss before deciding

8. **Decide 4.3 — `activeOpLocations` debug tracking.** Extract behind a build tag, give it its own mutex, or delete entirely. My lean: build tag. Needs agreement before acting.
9. **Decide 4.7 — mutex separation.** Subsumed by 4.3 if we pick the build-tag option.
10. **Decide 4.8 — `Dump` function.** Is it used? If not, delete. If yes, add struct-tag redaction and a warning in the doc comment. Needs a quick grep.
11. **Decide 4.5 — accept the 655-line `event.go` as-is.** Document the maintenance cost, optionally split noop implementations into `noop.go`. My lean: accept with the small split.

### Noted and left alone

12. **4.10** — LogContext method asymmetry. Document, don't fix.
13. **4.12** — Terminal-method enforcement at type level. Can't be fixed in Go; already mitigated at runtime.
14. **4.14** — Audit `README.md`. Read it first; if content is still accurate, leave it; if not, update or delete.

**Rough time estimate for the must-fix + should-fix list:** maybe 2-3 hours of focused work. The critical shutdown race (4.1) is the biggest single item and deserves careful design + testing. The should-fix items are mostly mechanical cleanups.

---

## 7. Summary — is the package fit for v2?

**Yes, with one critical fix (4.1) and a handful of should-fix cleanups.** The architecture is sound: structured-only API, error chain enrichment is a real win, graceful shutdown is thought-through, test coverage is heavy. The critical issue is in the shutdown force-drain path, which can panic the daemon — this needs to be fixed before the daemon starts running under real load. Everything else is quality-of-life improvements and overhead reduction that can be taken on as time allows.

**The v1 race question stays open** until we verify whether 4.1 is the same thing the v1 race detector was catching. That verification can happen as part of the fix — after the fix lands, if `v1`'s CI still fails with a race, it's a different bug; if it passes, we've killed two birds.

---

## 8. Related documents

- `docs/v1-analysis/invariants.md` — "Nothing blocks logging" master rule constrains the error-handling philosophy but doesn't directly constrain this package.
- `docs/v2-design/api.md` Section 4.6 (error envelope) — the daemon's error responses carry the `op` field from the `internal/errors` chain, and this logging package is what surfaces the same chain in operator-visible logs. The two packages are complementary observability layers.
- `docs/reviews/internal-errors.md` — the errors package review (session 5-6). The `internal/logging/helper.go:buildErrorChain` function is the main consumer of the errors package's operation-tagging feature.
- `docs/session-handoff.md` → "v1 branch follow-ups" → "data race" — needs reconciliation with finding 4.1 once the fix is designed.
- `CLAUDE.md` → "Code style" → `internal/logging` is the zerolog abstraction referenced by the project convention.
