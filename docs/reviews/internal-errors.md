# `internal/errors` — Code Review

**Status:** First code review in `docs/reviews/`, written 2026-04-16 during session 5 and resolved 2026-04-16 during session 6. The package was flagged as a carry-forward-for-code-review target in session 3's restructure plan, because `docs/v2-design/api.md` relies on its operation-tagging pattern for the error response envelope and we need to know whether the package is ready to rely on before the first handler is written.

> **Resolution (2026-04-16, session 6):** every finding in this document has been applied. All "must-fix," "should-fix," and "add-after" items landed in a single review-and-fix commit along with the full `With*` method rename (4.10) and comprehensive test coverage (4.9). The package is now in its v2 final state and should not need revisiting except for genuine bug fixes. The notes below are preserved as a historical record of what the package looked like going into v2 and why the changes were made. Section 6's action plan was executed in full; Section 7's summary (*"yes, with three must-fix items"*) is obsolete because all the fixes are now in.

**Purpose of this document (historical):** a thorough audit of the `internal/errors` package with explicit findings, categorized by severity, so we can work through them together and decide which ones to fix before v2 code starts being written against the package. Every finding has an explanation of *why* it's a finding and a recommended action.

**How to use this document (historical):** read through the findings, discuss each one, mark them as fix / defer / reject. Once we've agreed on which actions to take, a follow-up commit applies the agreed changes to the package.

---

## 1. Package inventory

| File | Lines | Purpose |
|---|---|---|
| `errors.go` | 149 | Core package — `DetailedError` type, builder methods, `Root`, `PrintChain`, `AsDetailedError` |
| `sentinals.go` | 6 | Sentinel errors (currently just `ErrNotFound`). **Note filename typo** — should be `sentinels.go`. |
| `errors_test.go` | 42 | Two tests, both for `Root` only |
| `README.md` | 2 | Empty placeholder |

**Total package size:** ~200 lines across four files. Small, focused scope.

**Public API surface:**

```go
// Types
type Op string
type Error interface { error }
type DetailedError struct { ... }

// Constructors
func New(op Op) *DetailedError

// Builder methods (all chainable, all nil-safe)
func (e *DetailedError) Msg(msg string) *DetailedError
func (e *DetailedError) Msgf(format string, a ...any) *DetailedError
func (e *DetailedError) Err(err error) *DetailedError
func (e *DetailedError) Errorf(format string, a ...any) *DetailedError

// Accessors
func (e *DetailedError) Error() string
func (e *DetailedError) Op() Op
func (e *DetailedError) Cause() error
func (e *DetailedError) Unwrap() error

// Helpers
func AsDetailedError(err error) (*DetailedError, bool)
func Root(err error) error
func PrintChain(err error)

// Sentinels
var ErrNotFound error
```

That's 4 types (including the interface), 1 constructor, 5 builder methods, 4 accessors, 3 helpers, and 1 sentinel. Every symbol is exported.

---

## 2. Usage survey

**The package is heavily depended on.** `grep -R "errors\\.(Op|New|...)"` returns **323 occurrences across 27 files**. The main consumers in the v2 carry-forward tree:

- `internal/database/sqlite/` (all sub-packages) — heavy user: wraps DB errors with op tags throughout `api_context.go`, `service.go`, `internal.go`, `helpers.go`, `migrations.go`, `validation.go`, adapters, models. Dozens of call sites.
- `internal/logging/` — uses the package for its own error context **and** is the one place that reads the package's error chain via `AsDetailedError` (in `helper.go:40` → `buildErrorChain`, which extracts ops for structured log output).
- `internal/iocdi/` — uses `errors.Op` + `New` for DI container errors.
- `internal/adif/` — uses the pattern.
- `internal/utils/` — uses the pattern for frequency parsing and network error tests.

**What's actually called from outside the package:**

| Symbol | External callers | Notes |
|---|---|---|
| `errors.Op` (type) | All 27 files | The canonical operation-tag type |
| `errors.New(op)` | All 27 files | The main constructor |
| `.Err(err)` | Most wrapping call sites | Attach a cause |
| `.Msg(str)` | Most wrapping call sites | Set the message |
| `.Msgf(fmt, args)` | Common | Formatted message variant |
| `.Errorf(fmt, args)` | Common | See concern below — this has surprising semantics |
| `errors.AsDetailedError(err)` | **1 caller** (`internal/logging/helper.go`) | Used only by structured-log error-chain introspection |
| `errors.ErrNotFound` | **1 file** (`internal/database/sqlite/api_context.go`, 9 lines) | Used for "row not found" returns from the DB layer |
| `errors.Root(err)` | **0 external callers** — only referenced by its own tests | Effectively dead code |
| `errors.PrintChain(err)` | **0 callers anywhere** | Dead code |
| `errors.Error` (interface) | **0 callers** | Dead code |
| `.Cause()` | **0 external callers**; only referenced by `internal/logging/helper.go:46` | Duplicates `Unwrap()` |

**Key observation:** the package's public API is much larger than what's actually used. The production usage pattern is **`errors.New(op).Err(err).Msg(...)` or `errors.New(op).Err(err).Msgf(...)`** — that's about 95% of the calls. Everything else is either dead or used by exactly one consumer.

---

## 3. Strengths — things the package gets right

These are things I'd preserve as-is; noting them explicitly so we don't accidentally lose them in a rewrite.

### 3.1 Stdlib compatibility via `Unwrap()`

`DetailedError.Unwrap() error` is implemented correctly (line 105-107). This means `errors.Is`, `errors.As`, and `errors.Unwrap` from the stdlib all work on a DetailedError chain, including across mixed chains where a DetailedError wraps a non-DetailedError or vice versa. This is the right shape and it should not change.

The `TestRoot_SimpleChain` test exercises a mixed chain (`DetailedError → fmt.Errorf(%w) → stderr.New`) and confirms `Root()` can walk it. That test actually validates the stdlib interop and is the most valuable test in the file.

### 3.2 Nil-safe method chaining

Every builder method handles a nil receiver cleanly:

```go
func (e *DetailedError) Msg(msg string) *DetailedError {
    if e == nil {
        return nil
    }
    e.msg = msg
    return e
}
```

This means callers can write `return errors.New(op).Err(f()).Msg("x")` without worrying about chain explosions if something in the chain is unexpectedly nil. It's defensive but useful — the pattern composes well.

### 3.3 `Op` as a distinct exported type

```go
type Op string
```

Using a named string type rather than plain `string` is the right call. Consumers declare `const op errors.Op = "pkg.FuncName"` and the type system prevents accidentally passing a non-op string. This is a small but real ergonomic win and it should stay.

### 3.4 The operation-tagging idiom itself

The pattern `const op errors.Op = "pkg.Func"` + `errors.New(op).Err(err).Msg("...")` is a clean way to thread operation context through the error chain. It's the foundation that `docs/v2-design/api.md` Section 4.6 relies on for the error response envelope's `"op"` field. This is the package's core value proposition and it works.

### 3.5 Cycle detection in `Root()` — the intent is right

`Root()` guards against infinite loops in the error chain (line 121-137) by tracking visited errors. Even if no real code produces cyclic chains today, the guard is appropriate defensive programming for a walker function that would otherwise hang on malformed input. **But see concern 4.6 for a bug in how it's implemented.**

---

## 4. Concerns — findings grouped by severity

### Critical

**No critical findings.** The package works for its main use case (the chainable error pattern) and has no known data-corruption or runtime-panic bugs in the normal usage paths. The findings below are design and maintenance issues, not bugs that block usage.

### High

#### 4.1 Default message leaks into user-facing output

**Location:** `errors.go:26`

```go
func New(op Op) *DetailedError {
    return &DetailedError{
        op:    op,
        cause: nil,
        msg:   "Internal system error.",  // ← hardcoded default
    }
}
```

**What it does:** every call to `New(op)` starts with the message pre-populated to the literal string `"Internal system error."`. If the caller forgets to call `.Msg(...)` or `.Errorf(...)` before returning, this string propagates to `.Error()` and eventually to anything that displays the error to an end user.

**Why this is a problem:**

1. **It's an English-language, user-facing string hardcoded in a low-level utility package.** The package has no business making presentation decisions for end-user output.
2. **It's not config-driven.** The "no magic numbers, everything configurable" project rule (see `CLAUDE.md` → "Project idioms" when that gets added) applies to hardcoded strings too — and this one is a specific kind of magic value that can never be overridden by the consumer.
3. **It silently papers over missing `.Msg()` calls.** A reviewer reading `return errors.New(op).Err(err)` (no `.Msg(...)`) has no visible cue that the resulting error's message will be "Internal system error." — they'd have to know the default. This is the kind of implicit behavior that causes production bugs.
4. **It's unlocalized** — not a concern for a personal project today, but if Station Manager ever grows multi-language support the default needs to come out.
5. **For the HTTP error envelope in `api.md` Section 4.6**, this means an HTTP response might contain `{"message": "Internal system error.", "op": "qsoservice.Submit"}` for an error that had no real message — which is worse than either a blank message or an empty string, because it looks like a real message and is uselessly generic.

**Recommended action:** make `msg` empty by default. Callers that want a fallback message can set one explicitly via `.Msg(...)`. Callers that forget will produce errors with an empty `Error()` string, which is at least an obvious "you forgot to set a message" signal rather than a plausible-looking fake one.

```go
func New(op Op) *DetailedError {
    return &DetailedError{
        op: op,
    }
}
```

Alternatively: the default message could come from the op itself — e.g., `fmt.Sprintf("%s failed", op)` — but that's speculative design and I'd prefer the simpler "empty by default" approach.

**Impact of the fix:** any call site that relied on the default would now produce an error with a blank message. Grep should find them (if any exist). Most real call sites explicitly set `.Msg(...)` or `.Msgf(...)` so the blast radius is likely small, but this needs verification before the fix lands.

---

#### 4.2 `DetailedError.Error()` only returns `msg`, losing op and cause context

**Location:** `errors.go:40-45`

```go
func (e *DetailedError) Error() string {
    if e == nil {
        return ""
    }
    return e.msg
}
```

**What it does:** calling `err.Error()` on a DetailedError returns only the leaf message string. The op, the cause chain, and any nested DetailedError context are dropped.

**Why this is a problem:**

1. **It loses information.** A `fmt.Println(err)` or `log.Println(err)` on a DetailedError looks like `"Internal system error."` (or whatever the leaf message is) with no indication of where in the codebase the error originated. Without calling `AsDetailedError(err)` and inspecting `.Op()`, there's no way to know the operation context.
2. **It breaks `%+v` formatting expectations.** Many Go error libraries (pkg/errors, cockroachdb/errors, etc.) implement a richer `Error()` or support `Format()` so that printing the error gives a useful representation. DetailedError does neither.
3. **It means the `op` field is only useful if consumers go out of their way to read it.** In `internal/logging/helper.go`, `buildErrorChain` does exactly this — it walks the chain via `AsDetailedError` and extracts op per-frame. Every other consumer that just prints the error loses the op.
4. **For the HTTP error envelope**, this is fine — the handler reads `.Op()` explicitly when writing the response. But for debugging, for log output that bypasses the structured logger, and for any `fmt.Sprintf("%v", err)` usage, the current behavior is quietly wrong.

**Recommended action:** change `Error()` to return a richer representation that includes the op and the cause chain.

Option A (format like most stdlib error chains):

```go
func (e *DetailedError) Error() string {
    if e == nil {
        return ""
    }
    if e.cause == nil {
        if e.msg == "" {
            return string(e.op)
        }
        return fmt.Sprintf("%s: %s", e.op, e.msg)
    }
    if e.msg == "" {
        return fmt.Sprintf("%s: %s", e.op, e.cause.Error())
    }
    return fmt.Sprintf("%s: %s: %s", e.op, e.msg, e.cause.Error())
}
```

This gives output like `api.v1.qso.submit: parse adif: missing <call> tag at position 42` when you print the error — the op, the current frame's message, and the root cause all visible.

Option B: add a separate `Format()` method supporting `%+v` for verbose output and keep `Error()` minimal. More complex; only worth it if there's a specific need.

**I'd recommend Option A.** It aligns `DetailedError` with stdlib conventions (`fmt.Errorf("op: %w", cause)` produces similar output) and removes the need for callers to use `AsDetailedError` just to see the op when printing.

**Impact of the fix:** structured log output via `buildErrorChain` in `internal/logging/helper.go` is unaffected because it extracts op and msg explicitly via the API — it doesn't call `.Error()` on DetailedError instances. Any unstructured log output (`fmt.Println(err)`, `log.Printf("%v", err)`) becomes more informative. No known consumer depends on the current "leaf msg only" behavior.

---

#### 4.3 `Root()` cycle detection uses error-as-map-key, which can panic

**Location:** `errors.go:115-138`

```go
func Root(err error) error {
    if err == nil {
        return nil
    }

    current := err
    visited := map[error]struct{}{}  // ← error as map key

    for current != nil {
        if _, seen := visited[current]; seen {
            return current
        }
        visited[current] = struct{}{}  // ← panic risk here
        // ...
    }
    return err
}
```

**What it does:** tracks visited errors in a map keyed by `error` interface values to detect cycles.

**Why this is a problem:**

Go maps require comparable keys. The `error` interface is comparable **only if the concrete type behind it is comparable** — and comparability is checked at runtime when you insert into the map. If a non-comparable concrete type (e.g., a struct containing a slice, a map, or a function) is returned as an error and gets inserted into the visited map, **it panics at runtime** with something like `panic: runtime error: hash of unhashable type`.

This is not hypothetical — many real-world Go error types are slices of causes, maps of context, or structs containing unhashable fields. `pkg/errors`, `cockroachdb/errors`, and `hashicorp/go-multierror` all use types that are **not** comparable. If Station Manager ever adopts one of these libraries (or writes its own multi-error aggregator), `Root()` becomes a latent time bomb that panics the first time a non-comparable error type shows up in a chain.

**The current tests don't catch this** because they only use `*DetailedError` (pointer, always comparable) and `fmt.Errorf(%w)` wrappers (`*fmt.wrapError`, also pointer).

**Recommended action:** replace the map-based cycle detection with a simpler depth limit. The existing `buildErrorChain` in `internal/logging/helper.go` already uses the safer approach:

```go
const maxDepth = 50
visited := 0

for err != nil && visited < maxDepth {
    visited++
    // ... walk one step
}
```

Depth limiting is slightly less precise (a long-but-not-cyclic chain of 100 wrappers would stop at 50) but in practice cycles are rare and chains longer than a few frames are rarer still. The simpler approach is panic-safe and uses less memory. Recommended limit: 50 or 100.

An alternative that preserves cycle detection without map-key issues: track visited errors by their `reflect.ValueOf(err).Pointer()` for pointer types, skip tracking for non-pointer types. More complex; I don't think it's worth it for the marginal precision gain.

**Impact of the fix:** `Root()` becomes slightly less precise (depth-limited rather than truly cycle-aware) but is no longer a potential runtime panic. Nothing currently depends on `Root()` outside the package's own tests, so the blast radius is zero today.

---

### Medium

#### 4.4 `Errorf` has surprising semantics — does two things at once

**Location:** `errors.go:84-91`

```go
func (e *DetailedError) Errorf(format string, a ...any) *DetailedError {
    if e == nil {
        return nil
    }
    e.cause = fmt.Errorf(format, a...)
    e.msg = e.cause.Error()
    return e
}
```

**What it does:** sets both the cause and the message in one call. The cause becomes a `fmt.Errorf`-produced error, and the message becomes that cause's `.Error()` string.

**Why this is a problem:**

1. **It's surprising behavior.** The other builder methods (`.Msg`, `.Err`, `.Msgf`) each set exactly one field. Users reading `.Errorf(...)` will reasonably expect the same — but it silently overwrites both.
2. **It clobbers a previously-set `.Err(...)`.** `errors.New(op).Err(dbErr).Errorf("wrapped: %w", otherErr)` discards `dbErr` entirely. If the caller meant to attach both, they get only `otherErr`.
3. **The comment on the method is misleading:** `"Syntactically the same as fmt.Errorf."` It's not. `fmt.Errorf` returns a stdlib error. This returns a DetailedError builder. And it has the "set both fields" side effect that `fmt.Errorf` doesn't.
4. **There's an alternative that already exists:** `.Err(fmt.Errorf(format, a...)).Msgf(format, a...)` is explicit and equivalent. Slightly more typing, but obvious.

**Recommended action:** two options.

Option A: **remove `Errorf` entirely.** Callers that need its behavior use `.Err(fmt.Errorf(format, a...))` or `.Msgf(format, a...)` explicitly depending on which field they want to set. More typing, clearer intent, fewer footguns.

Option B: **clarify the method** — rename it, fix the comment, maybe split into `.WithErrf(format, ...)` (sets cause) and `.Msgf(format, ...)` (sets message, already exists). Preserves backward compatibility but requires explaining the split.

**I'd recommend Option A (remove).** `.Errorf` has 103 call sites across the codebase (per grep), so removing it is a non-trivial migration, but the resulting calls are more explicit and the surprise goes away. The migration is mechanical and each call site becomes two lines of explicit `.Err(fmt.Errorf(...))` or just `.Msgf(...)`.

**If the migration cost is too high**, Option B is an acceptable compromise — rename to `SetErrAndMsg` or similar so the behavior is in the name.

---

#### 4.5 `Error` interface is pointless dead code

**Location:** `errors.go:10-12`

```go
type Error interface {
    error
}
```

**What it does:** declares an interface identical to the stdlib `error`. Adds no methods. No code anywhere references it (0 callers in grep).

**Why this is a problem:** it's just noise. A reader encountering `errors.Error` in import-like context has to mentally resolve "is this different from `error`? why does it exist?" and the answer is "no and it doesn't." Unused exported symbols are a documentation burden for no benefit.

**Recommended action:** delete it.

---

#### 4.6 `PrintChain` is unused debug code

**Location:** `errors.go:140-149`

```go
func PrintChain(err error) {
    for i := 0; err != nil; i++ {
        if d, ok := AsDetailedError(err); ok {
            fmt.Printf("[%d] op=%s msg=%s\n", i, d.Op(), d.Error())
        } else {
            fmt.Printf("[%d] %s\n", i, err)
        }
        err = stderr.Unwrap(err)
    }
}
```

**What it does:** prints an error chain to stdout. Debug helper.

**Why this is a problem:**

1. **No callers.** Grep confirms zero uses anywhere in the tree.
2. **Writes to stdout, not a logger.** For a daemon that will have structured logging via `internal/logging`, direct stdout output from a library function is wrong — it bypasses the logger entirely and couldn't be configured away.
3. **`%s` on `d.Error()` now gets the leaf message only** (see 4.2). If 4.2 is fixed and `Error()` returns a richer representation, `PrintChain` becomes double-work because it's printing the chain explicitly AND each item's `.Error()` already contains the chain.

**Recommended action:** delete it. If debugging ever needs it back, three lines reconstruct it at the call site, or the `buildErrorChain` helper already does the same job within the logging package.

---

#### 4.7 `Cause()` duplicates `Unwrap()` with a slightly different signature

**Location:** `errors.go:93-102` and `errors.go:105-107`

```go
func (e *DetailedError) Cause() error {
    if e == nil {
        return nil
    }
    if e.cause == nil {
        return nil
    }
    return e.cause
}

func (e *DetailedError) Unwrap() error {
    return e.cause
}
```

**What it does:** `Cause()` and `Unwrap()` both return `e.cause`. `Cause()` has nil-safety (nil receiver → nil). `Unwrap()` panics on a nil receiver (though in practice callers go through the stdlib `errors.Unwrap()` wrapper which checks for nil first).

**Why this is a problem:**

1. **Two methods for the same thing.** One is idiomatic Go stdlib convention (`Unwrap`); the other is a historical naming choice from pre-1.13 Go libraries (`Cause`, borrowed from `pkg/errors`).
2. **The only external caller of `.Cause()`** is `internal/logging/helper.go:46` (in `buildErrorChain`). That call could trivially be `errors.Unwrap(err)` instead — same behavior, stdlib-idiomatic.
3. **Keeping both invites confusion** about which one is the "real" way to walk the chain.

**Recommended action:** delete `Cause()`, update the one caller in `internal/logging/helper.go` to use `errors.Unwrap()` instead.

**Impact of the fix:** one 2-line change in `helper.go`. The builder pattern is unaffected (no builder methods call `Cause()`). Nothing else uses it.

---

#### 4.8 Filename typo: `sentinals.go` → `sentinels.go`

**Location:** `internal/errors/sentinals.go`

**What it is:** the filename is misspelled. "Sentinals" is not a word; "sentinels" is.

**Why this is a problem:** cosmetic, but it's the first thing that will annoy anyone who greps for sentinels and comes up empty, and it suggests to a reader that the package has been lightly maintained (which may or may not be fair, but the impression matters).

**Recommended action:** rename via `git mv`. Trivial.

---

### Low

#### 4.9 Test coverage is very sparse

**Location:** `errors_test.go`

**What's tested:**
- `Root()` with a simple mixed chain (DetailedError → fmt.Errorf(%w) → stderr.New)
- `Root()` with a pure DetailedError chain

**What's NOT tested:**
- `DetailedError.Error()` — no assertion about what the method returns
- `.Msg(msg)` — no test that setting a message actually works
- `.Msgf(format, args)` — no test
- `.Err(err)` — no test
- `.Errorf(format, args)` — no test (and its surprising dual-role behavior is un-verified)
- `.Op()` — no test
- `AsDetailedError` for positive and negative cases — only indirectly tested via `TestRoot_DetailedErrorChain`
- Nil-safety of every builder method (despite this being a core feature)
- `PrintChain` — no test (granted, it's unused)
- Interop with `errors.Is` and `errors.As`

**Why this is a problem:** the package is used in 27 files with 323 call sites. The tests exercise 2 functions out of ~15 public symbols. A refactor of `Error()` to fix 4.2 has no regression guard. A fix to `Errorf` semantics in 4.4 has no regression guard. This is a textbook "package with lots of consumers but almost no self-tests" situation.

**Recommended action:** after we decide which of the other fixes to apply, add tests that exercise:
1. The builder chain end-to-end (`New(op).Err(inner).Msg("x")` produces the expected `Error()`, `Op()`, `Unwrap()` results).
2. Nil-safety (`var e *DetailedError; e.Msg("x")` doesn't panic).
3. `errors.Is` across a mixed chain (`errors.Is(err, ErrNotFound)` after wrapping).
4. `errors.As` from a mixed chain extracting a `*DetailedError`.
5. Whatever new semantics we land on for `Error()` and `Errorf`.

Target: ~10 small test functions, maybe 150 lines of test code. Not huge, but a real safety net.

---

#### 4.10 `Msgf` naming collides with zerolog's terminal `.Msg()` pattern

**Location:** `errors.go:56-70` (and the `Msgf` method body on lines 64-70)

**What it is:** the builder has `.Msg(string)` and `.Msgf(format, args)` following a Go stdlib naming convention that mirrors `fmt.Print/Printf`, `log.Printf/Println`, etc.

**Why this is maybe-a-problem:** the project also uses `rs/zerolog` for logging, where `.Msg(string)` is the **terminal** method that sends the log event. A reader switching between `errors.New(op).Err(e).Msg("x")` (DetailedError builder) and `logger.InfoWith().Err(e).Msg("x")` (zerolog builder) may trip on the context-dependent semantics — in one case `.Msg` is chainable, in the other it's terminal.

This is not a bug. It's a potential ergonomic friction. Whether it rises to the level of worth-fixing depends on whether it actually causes mistakes in practice.

**Recommended action:** no action unless someone trips on it in code review. Naming it differently (`.WithMsg(...)`) would be cleaner but requires a 150+ line sweep across 27 files and creates its own consistency problem. Low priority.

---

#### 4.11 README.md is empty

**Location:** `internal/errors/README.md`

```
# Station Manager: errors package
```

That's the entire file. Two lines including the header.

**Recommended action:** either fill it in (a short doc.go-style package description, the canonical usage pattern, the philosophy) or delete it. Per CLAUDE.md's "Document intent, not mechanism" idiom — if the package has a non-obvious design intent worth preserving, a short README or `doc.go` is the right place for it.

**My preference:** write a short doc.go with:
- The operation-tagging philosophy (why `Op` exists, the `const op = "pkg.Func"` pattern)
- The canonical usage example
- The nil-safety guarantee
- A note that the package is consumed by the HTTP API layer for the error envelope (pointer to `docs/v2-design/api.md` Section 4.6)

~50 lines of doc.go is enough. Delete the README.md.

---

## 5. Fit with `docs/v2-design/api.md` Section 4.6

The api.md error envelope is:

```json
{
  "code": "invalid_adif",
  "message": "missing required <call> tag at position 42",
  "op": "api.v1.qso.submit"
}
```

**Per-field analysis of how the handler layer would populate each field from a DetailedError:**

| Envelope field | Source | Requires what from `internal/errors`? |
|---|---|---|
| `code` | Handler-layer mapping (errors.Is against sentinels, type switches, etc.) | Nothing from this package beyond `errors.Is` compatibility — which works correctly today via `Unwrap()` |
| `message` | `dErr.Error()` or a handler-specific mapping | **Currently returns leaf msg only** — this works for the envelope's purposes (the "message" field is meant to be the human-readable leaf, not the full chain). BUT the 4.1 default-message concern means it might return `"Internal system error."` by accident. |
| `op` | `dErr.Op()` via `AsDetailedError(err)` | Works correctly. This is the package's core value-add. |

**The handler layer needs:**

1. **Extract op from an error:** `dErr, ok := errors.AsDetailedError(err); if ok { op = string(dErr.Op()) }`. This works.
2. **Extract a message for display:** `err.Error()`. This works but will contain `"Internal system error."` if 4.1 isn't fixed.
3. **Map an error to a code:** via `errors.Is(err, ErrNotFound)` → `"not_found"`, `errors.As(err, &InvalidADIFError{})` → `"invalid_adif"`, etc. The current package supports this via stdlib compatibility. The handler layer will need its own sentinel vocabulary (or typed errors) to map into; the `internal/errors` package doesn't need to grow a code system.

**Conclusion:** the package is *functionally* adequate for the api.md envelope, with the one caveat that **the default message problem (4.1) will leak into HTTP responses** and should be fixed before the first handler is written.

---

## 6. Recommended action plan

Grouping by priority and discussion order. Each item is a single decision we should make together.

### Must-fix before writing the first handler

1. **Fix 4.1: remove the hardcoded default message** `"Internal system error."`. Make `msg` empty by default.
   - Blast radius: verify no call site relies on the default. Grep for `errors.New\(.*\)` chains that never call `.Msg` or `.Msgf` before returning.
   - Time cost: 10 minutes + verification + tests.

2. **Fix 4.2: make `Error()` return op + msg + cause** instead of leaf msg only.
   - Blast radius: verify structured log output in `internal/logging/helper.go` still produces the expected shape (it should — it doesn't call `.Error()`, it walks the chain explicitly).
   - Time cost: 15 minutes + tests.

3. **Fix 4.3: replace map-based cycle detection in `Root()` with depth-limit.**
   - Blast radius: zero — `Root()` has no external callers.
   - Time cost: 5 minutes + a new cycle-handling test.

### Should-fix opportunistically

4. **Fix 4.7: delete `Cause()`, update the one `helper.go` call site to use stdlib `errors.Unwrap()`.**
   - Blast radius: one 2-line change in `helper.go`.
   - Time cost: 5 minutes.

5. **Fix 4.8: rename `sentinals.go` → `sentinels.go` via `git mv`.**
   - Blast radius: zero (no code references the filename).
   - Time cost: 30 seconds.

6. **Fix 4.5: delete the `Error` interface type.**
   - Blast radius: zero (no callers).
   - Time cost: 30 seconds.

7. **Fix 4.6: delete `PrintChain`.**
   - Blast radius: zero (no callers).
   - Time cost: 30 seconds.

### Discuss before deciding

8. **Decide 4.4: keep `Errorf` with clarification, or delete and migrate 103 call sites to explicit `.Err(fmt.Errorf(...))` / `.Msgf(...)`.**
   - The migration isn't trivial but is mechanical. My lean is to delete, but it's a real amount of churn to decide on.
   - Time cost: if delete, ~45 minutes of sweep + testing. If keep with rename, ~20 minutes.

### Add after other fixes land

9. **Add missing test coverage (finding 4.9).** Target 10 small tests, ~150 lines.
   - Time cost: 30 minutes.

10. **Fill in `doc.go` (finding 4.11).** Short package description, canonical pattern, pointer to api.md.
    - Time cost: 15 minutes.

### No action

11. **Finding 4.10 (`Msgf` vs zerolog terminal `Msg` naming)** — leave alone unless a specific case of confusion surfaces.

---

## 7. Summary — is the package fit for v2?

**Yes, with three must-fix items.** The core shape of the package — `DetailedError` with op + cause + msg, builder pattern, stdlib compatibility, nil-safe chaining — is correct and should be preserved. The HTTP API's dependency on `Op()` extraction via `AsDetailedError()` works as intended.

The three items that block v2 readiness are the default-message leak (4.1), the information-losing `Error()` implementation (4.2), and the `Root()` panic risk (4.3). All three are small changes with clear fixes and minimal blast radius. Everything else is cleanup that improves the package without being strictly necessary for correctness.

**My lean on the full action plan:** do items 1–3 (must-fix), 4–7 (cheap wins), and 9–10 (tests and doc) as a single review-and-fix commit. Hold item 8 (`Errorf` decision) as a separate discussion — it's the one with real migration cost and deserves a dedicated call.

**Time estimate for the full fix commit (excluding item 8):** roughly 90 minutes of focused work. Mostly test writing; the actual code changes are small.

---

## 8. Related documents

- `docs/v2-design/api.md` → Section 4.6 (error response envelope) — the primary consumer of this package's operation-tagging feature.
- `docs/session-handoff.md` → "Carry-forward package code-review track" — this document is the first output of that track.
- `CLAUDE.md` → "Code style" → "Use `internal/errors` for operation-tagged errors" — the project convention that motivates keeping the package. Also → "Project idioms" when the no-magic-numbers rule is added (per the memory note).
