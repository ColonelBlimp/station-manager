---
number: 0018
title: SQLite driver — modernc.org/sqlite (pure Go), no CGO
status: Accepted
date: 2026-05-07
---

# 0018 — SQLite driver: `modernc.org/sqlite`

## Context

The daemon's SQLite driver is `modernc.org/sqlite` — a pure-Go translation of upstream SQLite via the `cc2go` tool, no CGO dependency. The driver registers itself as `"sqlite"` and is the one `database/sql` actually opens connections through (see `internal/database/sqlite/service.go`'s `_ "modernc.org/sqlite"` blank import and `internal/database/sqlite/consts.go`'s `SqliteDriver = "sqlite"` constant).

This decision was effectively made in a sister repo — the standalone `internal/database` module that was developed separately and then squashed into the main repo via `git subtree` on 2026-03-23 (commit `16d348e`). When the v2 milestone-1 restructure landed on 2026-04-15 (commit `0010b6e`) and collapsed the multi-module workspace into the single root `go.mod`, the consolidated module file recorded `modernc.org/sqlite v1.48.1` as the direct driver dependency. **No prior ADR was written, and the project's day-to-day discussion never explicitly chose modernc — the choice arrived in this repo via subtree pull and was inherited.**

This ADR documents the choice retrospectively because:

- v1 used `mattn/go-sqlite3` (CGO). The build-time pain that came with the CGO toolchain (slow `go build`, complicated cross-compilation, distroless-image friction) was a real cost the operator remembers.
- The 2026-05-07 code review (`docs/reviews/cmd-smd-and-imports.md`, finding M5) surfaced that the codebase still imported `github.com/mattn/go-sqlite3` for typed-error detection (`sqlite3.Error`, `sqlite3.ErrConstraintUnique`) — a path that **never matched at runtime** because the driver in use was modernc, not mattn. Correctness was silently riding on the substring-match fallback. The fix (this commit) detects modernc's typed `*sqlite.Error` instead and removes the direct mattn import.
- A future contributor — including future-us — needs the "why this driver" answer in the same place as the "why these other v2 decisions" answers, not buried in a subtree squash diff.

## Decision

**Use `modernc.org/sqlite` as the daemon's SQLite driver. Detect typed errors against `*modernc.org/sqlite.Error` (codes from `modernc.org/sqlite/lib`).**

`internal/database/sqlite.IsUniqueConstraintError` is the single canonical helper. Callers needing constraint-error detection (notably `qsoservice.Submit` and `qsoservice.Update` for race-resolution paths) call this exported helper rather than duplicating the logic.

The substring-match fallback (`strings.Contains(err.Error(), "UNIQUE constraint failed")`) is retained as belt-and-braces — sqlboiler wraps errors with `friendsofgo/errors.Wrap`, and a future version that drops `Unwrap` interop would silently break the typed branch. The fallback is no longer load-bearing (the typed branch now actually matches), but it stays as a safety net.

## Alternatives considered

### Stay on `mattn/go-sqlite3` (CGO)

The default-historical Go SQLite driver. CGO-based, wraps upstream C SQLite directly.

Rejected. The build-time and cross-compilation pain was a real-world friction point: the operator builds on Linux for daily use, and CI / dev-loop iteration was noticeably slower with CGO compile time on every clean build. A pure-Go driver removes that cost. mattn is a fine driver where CGO is acceptable; for this project, CGO isn't.

### `crawshaw.io/sqlite`

A pure-Go driver with a hand-written API surface (no `database/sql` adapter by default; offers its own session/handle API).

Rejected. The project's storage layer is `database/sql` + sqlboiler — adopting crawshaw would require rewriting every helper to use its native API or reaching for a `database/sql` wrapper that adds another layer of indirection. modernc speaks `database/sql` natively. The crawshaw API is arguably nicer for direct sqlite use, but the project's choice to standardise on `database/sql` predates this decision.

### `zombiezen.com/go/sqlite`

A more recent pure-Go driver derived from crawshaw's work, with a cleaner API.

Rejected for the same reason as crawshaw — no native `database/sql` adapter, would force a wrapper layer or a wider refactor.

### Keep `mattn/go-sqlite3` AND add modernc as a "pure-Go" build option

Use Go build tags so CGO-toolchain-available builds use mattn, others use modernc.

Rejected. The complexity cost — two driver paths in `database/sql.Register`, two typed-error detection paths, dual go.mod entries, build-tag matrix in CI — outweighs the benefit. Single driver, single detection path; if someone needs CGO mattn for a specific deployment, they can fork and override.

## Consequences

**Signed up for:**

- **No CGO toolchain required** for any developer build, CI run, or distroless-style image. `go build` works with stock Go installed; cross-compilation is a single `GOOS`/`GOARCH` flip.
- **Pure-Go driver bugs are upstream's problem.** modernc translates upstream SQLite C source via `cc2go`; bugs introduced by the translation surface in the Go layer, not the C layer. Mostly stable in practice (the project has been on modernc since 2026-03-23 with no driver-attributable failures), but the failure mode does exist as a class.
- **Typed-error detection uses modernc's `*sqlite.Error` and codes from `modernc.org/sqlite/lib`.** Specifically `SQLITE_CONSTRAINT_UNIQUE` (`2067`) for UNIQUE-index violations and `SQLITE_CONSTRAINT` (`19`) as the primary-code fallback. Test `TestIsUniqueConstraintError_TypedBranchMatches` (`internal/database/sqlite/unique_error_test.go`) pins this — if a future driver swap reintroduces a type mismatch, the test fails loud.
- **`mattn/go-sqlite3` remains in `go.mod` as `// indirect`** because `golang-migrate/migrate/v4/database/sqlite3` (the migration driver) imports it transitively. This is orthogonal to the runtime driver — migrate's sqlite3 adapter only uses the `*sql.DB` interface and doesn't care which driver registered "sqlite". Cleaning that up would mean either replacing migrate's sqlite3 driver with a pure-Go variant or vendoring our own migration runner; out of scope here.

**Accepted costs:**

- **No real-world load testing of modernc yet.** v2 isn't in daily operator use (v1 branch is daily-use). The driver has 86+ tests in `internal/database/sqlite/` and is exercised indirectly by every dependent package's test suite (qsoservice, api, lookup, forwarding/worker, refresher), all running against `:memory:` databases. Stable for the test surface; long-running production behaviour is unknown until v2 ships.
- **Slightly larger binary.** modernc bundles the full SQLite C source as Go-translated code, ~5 MB of generated code. Not a concern for the project's binary-size posture.
- **Performance is "broadly comparable" to mattn for sqlite's typical workloads** but not identical. cc2go-translated code can be 10-30% slower on tight loops in some benchmarks. Not measured here; flagged as a fact, not a finding. At personal-operator write rates (~hundreds of QSOs per evening at most) the difference is invisible.

**Gained:**

- **Faster local development.** No CGO compile cost on rebuilds.
- **Trivial cross-compilation.** Operators on different architectures can build their own daemon binary without a working CGO toolchain.
- **Typed-error detection that actually works.** The M5 finding's "silent reliance on substring match" is closed; the typed branch matches the runtime error type and the fallback is genuine belt-and-braces.

## Triggers to revisit

- **Production-load issues attributed to the driver.** v2 enters daily operator use, the daemon runs for days under contest-night load, and we hit a panic / hang / corruption that traces back to modernc translation. Switching to mattn (CGO) becomes attractive if the issue is reproducible and upstream modernc doesn't fix it quickly. Document the specific failure shape before flipping.
- **Performance under upload-queue burst.** If the forwarder workers' bulk-upsert path becomes a real bottleneck and the difference is attributable to the driver (not query plans, not sqlite contention), revisit. Today this is hypothetical — the worker poll defaults are operator-conservative anyway.
- **modernc.org/sqlite goes unmaintained.** Currently active (v1.48.x cadence is regular). If upstream stops releasing for a year, evaluate alternatives — likely zombiezen or fork-then-maintain.
- **A second SQLite driver gets pulled into the daemon's hot path.** Today the `// indirect` mattn dep is fine because it's only used by `golang-migrate`'s sqlite3 adapter, which goes through `*sql.DB`. If anything in the daemon ever needs mattn's typed errors directly, that's the trigger to either remove migrate's sqlite3 driver from the dep tree or switch to a single-driver migration runner.

## References

- ADR 0017 (`0017-enrichment-pipeline-domain-table-cache.md`) — depends on the sqlite cache helpers; their typed-error detection path is what this ADR documents.
- `docs/reviews/cmd-smd-and-imports.md` — the 2026-05-07 review that surfaced finding M5 and triggered this ADR.
- `internal/database/sqlite/service.go` — the `_ "modernc.org/sqlite"` blank import that registers the driver.
- `internal/database/sqlite/consts.go` — `SqliteDriver = "sqlite"` (the registration name modernc uses).
- `internal/database/sqlite/internal.go` — `IsUniqueConstraintError` exported helper using modernc's typed error.
- `internal/database/sqlite/unique_error_test.go` — regression tests pinning the typed-error path against the registered driver.
- `internal/qsoservice/submit.go`, `internal/qsoservice/update.go` — call sites that previously duplicated the helper, now use the exported sqlite-package version.
- Subtree squash commit `16d348e` (2026-03-23) — the moment modernc arrived in this repo via `git subtree`, before the v2 milestone-1 restructure.
- v2 restructure commit `0010b6e` (2026-04-15) — first commit where modernc is a direct dependency in the consolidated root `go.mod`.
