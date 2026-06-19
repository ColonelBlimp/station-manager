# Code Review: internal/errors

Date: 2026-06-19

Scope: `internal/errors`, including `DetailedError`, sentinel errors, package tests, and the closest consumers in `internal/logging`, `internal/api`, and `internal/database/sqlite`.

## Summary

I reviewed the current tree as a fresh codebase. I did not use archived review documents as evidence for the findings below.

The package is compact and generally in good shape. `DetailedError` preserves operation context, implements `Unwrap`, works with `errors.Is` / `errors.As`, and has strong focused test coverage. The current sentinel errors are also being used cleanly by database and API handlers to keep machine-readable API responses out of raw SQL/string-matching territory.

The main runtime concern is at the boundary between `internal/errors` and `internal/logging`: `AsDetailedError` intentionally searches through the whole chain, but `logging.buildErrorChain` uses it while walking one frame at a time. That can make mixed `fmt.Errorf("%w")` + `DetailedError` chains show duplicated or skipped frames in structured logs. The rest of the findings are documentation drift from earlier package-shape changes.

## Findings

### M1 - Correctness: mixed stdlib-wrapper error chains are misrepresented in structured logging

Evidence:
- `AsDetailedError` uses `errors.As`, so it extracts a `*DetailedError` from anywhere under the supplied error, not necessarily from the current error frame (`internal/errors/errors.go:26-31`). The package docs confirm that chain-search behavior (`internal/errors/doc.go:70-76`).
- `logging.buildErrorChain` walks `err` frame by frame, but calls `smerrors.AsDetailedError(err)` on each current frame and then advances with `errors.Unwrap(err)` (`internal/logging/helper.go:20-34`).
- When the current frame is a stdlib wrapper such as `fmt.Errorf("wrap: %w", detailedErr)`, `AsDetailedError` returns the nested `DetailedError`, while `errors.Unwrap(err)` advances to that same nested `DetailedError` for the next loop. The stdlib wrapper does not get its own `error_ops == ""` frame, and the nested `DetailedError` can be recorded twice.
- The repo has a benchmark helper that builds exactly this mixed shape: a `DetailedError` whose cause is `fmt.Errorf("wrap %d: %w", i, err)` (`internal/logging/bench_test.go:35-43`).
- The logging docs say stdlib wrapped errors are traversed correctly for message strings and contribute empty strings to `error_ops` (`internal/logging/doc.go:61-68`).
- The existing mixed-wrapper test only asserts the first string prefix, that `"wrap: "` appears somewhere, and root equality; it does not assert the full `chain` / `ops` sequence or check for duplicated frames (`internal/logging/error_chain_test.go:42-54`).

Impact:
The error itself still works with `errors.Is` / `errors.As`; API behavior is not affected. The impact is observability correctness: `error_chain`, `error_ops`, and `error_history` can misstate the real wrapper stack when code mixes stdlib context wrappers with `DetailedError`. That is exactly the path operators use to diagnose failures, so a misleading chain costs debugging time.

Recommendation:
Keep `AsDetailedError` as the chain-search helper, but do not use it as a "current frame is DetailedError" predicate. Either:
- add a narrowly named helper such as `AsCurrentDetailedError(err)` / `DetailedFrame(err)` that performs a direct `*DetailedError` type assertion only; or
- change `logging.buildErrorChain` to use a direct type assertion for the current frame and reserve `AsDetailedError` for one-shot classification checks.

Then add a regression test with a chain like `DetailedError -> fmt.Errorf("wrap: %w", DetailedError)` and assert the exact `chain` and `ops` arrays, including the stdlib wrapper's empty op entry and no duplicated detailed frame.

### L1 - Documentation: package and logging docs reference APIs and paths that no longer exist

Evidence:
- `DetailedError.Error` docs tell callers that need only this frame's message to use `LocalMsg()` (`internal/errors/errors.go:46-49`), but the current package has no `LocalMsg` method.
- `WithErr` docs cite `docs/reviews/internal-errors.md` for historical background (`internal/errors/errors.go:101-114`), and the package doc repeats that link (`internal/errors/doc.go:87-88`). There is no `docs/reviews/internal-errors.md` in the current non-archive tree.
- `internal/logging/helper.go` says traversal prefers `DetailedError.Cause()` before falling back to stdlib unwrap (`internal/logging/helper.go:11-19`), but `DetailedError` no longer has `Cause()` and the implementation uses `errors.Unwrap`.
- `internal/logging/doc.go` also links to `docs/reviews/internal-errors.md` even though that file now lives only under `docs/reviews/archive` (`internal/logging/doc.go:61-68`).
- The package doc says every function declares a package-scoped op constant and that typed-string `Op` lets the compiler catch typos and renames (`internal/errors/doc.go:21-25`). Current code commonly uses local `const op` values, and a typed string does not make string-literal typos or function renames compiler-checked.

Impact:
This does not break runtime behavior, but it misleads new callers about available APIs and the actual review/documentation location. The missing `LocalMsg` reference is especially confusing because local-frame messages are exactly what logging would need if it ever stops using recursively expanded `Error()` strings.

Recommendation:
Update the docs to match the current surface:
- remove or implement the `LocalMsg()` reference;
- update the `Cause()` wording to `Unwrap`;
- point historical links at `docs/reviews/archive/internal-errors.md` or remove them from package docs;
- describe `const op errors.Op = "..."` as a convention, not compiler-enforced rename safety.

If local-frame extraction is desired, add explicit accessors such as `Msg()` / `LocalMsg()` and test them; otherwise document that `Error()` intentionally returns the full recursive chain.

## Security Review

No security findings surfaced in this pass.

Positive observations:
- API 5xx responses avoid returning raw `err.Error()` and instead log the detailed chain server-side (`internal/api/response.go:47-60`).
- Sentinel errors are compared with `errors.Is`, not string-matched, in handlers such as logbook create/update/delete (`internal/api/handler_logbook.go:84-90`, `internal/api/handler_logbook.go:136-149`, `internal/api/handler_logbook.go:164-174`).
- The package has no I/O, reflection over untrusted data, goroutines, or shared global mutable state.

Residual risk:
- Error strings can still carry internal details by design. That is fine for logs and debugging, but 4xx handler paths that intentionally surface parse/validation details should continue to be reviewed case by case.

## Performance Review

No package-level performance finding surfaced.

`DetailedError.Error()` recursively expands the full tail of the error chain. That is intentional and tested, but consumers should be aware that repeatedly calling `Error()` while walking each frame can duplicate work. Current logging benchmarks show the expected cost is still small for realistic chain depths:

```text
BenchmarkErrorWith_DetailedChain3-8        1646 ns/op   864 B/op   21 allocs/op
BenchmarkErrorWith_DetailedChain6-8        4104 ns/op  3369 B/op   59 allocs/op
BenchmarkErrorWith_StdWrap6-8              3926 ns/op  2649 B/op   31 allocs/op
```

If the logging chain output is corrected per M1, use the existing benchmarks to keep allocation churn visible.

## Coverage Notes

Strong current coverage:
- construction, builder mutation, `Error()` format combinations, nested `DetailedError` output, nil receiver behavior, `AsDetailedError`, `Unwrap`, `errors.Is`, `errors.As`, `Root`, depth-limit behavior, unhashable error safety, and `ErrNotFound`.
- Adjacent API/database coverage for `ErrNotFound`, `ErrDuplicateName`, and `ErrLogbookHasQsos` mappings.
- Adjacent logging coverage for basic `error_chain` field emission.

Missing focused coverage:
- mixed stdlib-wrapper + `DetailedError` logging chains with exact `chain` / `ops` assertions.
- direct package tests for `ErrDuplicateName` and `ErrLogbookHasQsos` as sentinels; they are covered indirectly through database/API behavior.
- self-referential or cyclic `DetailedError.Error()` behavior. `Root` is depth-limited, but `Error()` itself has no cycle guard. I did not find current code constructing cyclic errors, so this is a robustness test gap rather than a live bug.

## Verification

Commands run:

```text
go test ./internal/errors -count=1
go test -race ./internal/errors -count=1
GOCACHE=/tmp/go-build go test ./internal/errors -cover
go test ./internal/logging -run 'TestBuildErrorChain|TestEventErr' -count=1
go test -race ./internal/logging -run 'TestBuildErrorChain|TestEventErr' -count=1
go test ./internal/api -run 'Test(CreateLogbook_DuplicateName|DeleteLogbook_WithQSOs_Rejected|SubmitQso_MalformedADIF|SubmitQso_TimeOnAfterTimeOff)' -count=1
go test ./internal/database/sqlite -run 'Test(IsUnique|Unique|Duplicate|Logbook|ErrNotFound|Review)' -count=1
go vet ./internal/errors ./internal/logging ./internal/api ./internal/database/sqlite
go test ./internal/logging -bench . -run '^$' -benchtime=100ms
```

Result:
- Focused package, race, coverage, adjacent logging, adjacent API, adjacent database, vet, and logging benchmark runs passed.
- A first `go test ./internal/errors -cover` run without the repo's usual cache override failed inside Go's coverage tooling with `internal/coverage/cfile: package testmain: cannot find package`; rerunning with `GOCACHE=/tmp/go-build` passed with `coverage: 100.0% of statements`.

## Resolution (2026-06-19)

Both findings fixed.

- **M1 (fixed).** Added `errors.DetailedFrame` — a direct `*DetailedError` type
  assertion (no chain search) — and switched `logging.buildErrorChain` to use it
  as the per-frame predicate instead of the chain-searching `AsDetailedError`.
  Now a `DetailedError → fmt.Errorf("%w") → DetailedError` chain records the
  stdlib wrapper as its own frame (empty op) and the nested DetailedError exactly
  once. `AsDetailedError`'s doc now states it's for one-shot classification, not
  a current-frame predicate. Regression test
  `TestBuildErrorChain_StdWrapperFrameNotDuplicated` asserts the exact
  chain/ops; the existing mixed-chain test still passes.
- **L1 (fixed).** Doc corrections: `Error()` now documents that it intentionally
  returns the full recursive chain (no `LocalMsg()` method exists; how to add one
  if ever needed); the three `docs/reviews/internal-errors.md` links repointed to
  `docs/reviews/archive/internal-errors.md`; `buildErrorChain`'s comment now says
  it unwraps via stdlib `errors.Unwrap` + `DetailedFrame` (was the non-existent
  `Cause()`); and `errors/doc.go` now describes `const op errors.Op` as a
  convention whose literal is NOT compiler-verified against the function name
  (was an overstated "compiler catches typos and renames").

Verified: `gofmt`/`go vet` clean; `go build ./...`; `internal/errors`,
`internal/logging`, `internal/api`, `internal/database/sqlite` pass; `go test
-race ./internal/errors ./internal/logging` clean.

## Worktree Note

I did not modify production code. The worktree was clean at the start of this review; this review adds only `docs/reviews/internal-errors-2026-06-19.md`.
