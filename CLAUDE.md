# Station Manager — project instructions for Claude Code

## Repository structure

- **`main`** is where v2 construction happens. v2 is a clean-slate rewrite of Station Manager around a daemon + clients + bridges model (decided 2026-04-14). As of 2026-04-15, main is still at the v1.0.0 layout; the restructure commit that reshapes it into v2 milestone-1 layout has not yet landed.
- **`v1` branch** (and the `v1.0.0` tag) preserves the last v1 release. Day-to-day ham radio operations run from this branch; v1 bug fixes land here.
- **`pre-ft8-removal` tag** preserves the FT8 experiment tree in history for recovery if needed.

## Where the durable project context lives

Read these before making non-trivial design choices. They are the reference, not the scratchpad.

- **`docs/v1-analysis/invariants.md`** — load-bearing rules that must carry forward into v2. Check proposals against this list.
- **`docs/v1-analysis/lessons-for-v2.md`** — synthesis: patterns to apply, patterns to avoid, what v1 got right. The single most important read before any v2 design choice.
- **`docs/v1-analysis/design-decisions-log.md`** — keep/change/delete verdicts on every v1 major shape decision, plus the v2 rewrite decision.
- **`docs/v1-analysis/bug-inventory.md`** — known v1 issues, fixed and open. The "do not recreate these" list for v2.
- **`docs/v1-analysis/architecture-map.md`** — what v1 actually contains, module by module.
- **`docs/v2-design/`** — v2 design decisions as they're made. `structure.md` is the first; siblings (`api.md`, `milestones.md`, `db-layer.md`, etc.) appear as the corresponding questions get answered.
- **`docs/session-handoff.md`** — rolling cross-session state. Read at session start; update at session end.

## Load-bearing invariants (headlines)

Full list in `docs/v1-analysis/invariants.md`. Never violate these without explicit discussion.

- **Enrichment never blocks logging.** External lookups (hamnut, QRZ) degrade the QSO draft's completeness when they fail; they never prevent the operator from logging. The only thing that should stop logging is a broken local DB.
- **One-fails-all-fail for QSO writes.** The QSO row and its upload-queue row(s) are atomic in a single transaction; cache/enrichment writes live outside the transaction as best-effort.
- **Narrow daemon scope.** The v2 daemon does log + forward only. Rig control, CAT, PTT, audio, FT8 decoding, and capture UX all live in clients or bridges. A proposal that pushes any of those into the daemon contradicts a decided constraint.
- **`types.Qso` follows the ADIF specification.** The data model mirrors ADIF; adding a new ADIF field is a one-line change. The shape is load-bearing — do not propose flattening or reshaping without checking the invariants doc first.

## Lessons to apply (headlines)

Full list in `docs/v1-analysis/lessons-for-v2.md`. These patterns were explicitly extracted from v1's mistakes; each one has a cautionary tale attached.

- **Build specific, not generic.** Three specific implementations are easier to read, debug, and evolve than one clever abstraction. A small amount of duplication is cheaper than a framework. Don't DRY things up prematurely. Cautionary tale: v1 `internal/adapters/` — 30+ test files of reflection-based adapter framework, abandoned as "too complicated to maintain and use correctly."
- **Integration tests over mocks.** Real `&sqlite.Service{}` with in-memory databases is the pattern v1 uses correctly. If an interface's only consumer is a mock, delete the interface and the mock. Cautionary tale: v1 `DatabaseServiceInterface` drifted out of sync with the concrete type because nothing real ever used it.
- **Document intent, not mechanism.** Code shows what; doc comments should explain why. Non-obvious design decisions deserve a `doc.go`. Cautionary tale: the `additional_data` blob pattern had no `doc.go`, so the ADIF-alignment intent had to be rediscovered during analysis.
- **Asymmetric round-trips are a clue.** If a converter pair has one elegant side and one manual/complicated side, the complicated side is overbuilt. Cautionary tale: the `QsoAdditionalData` intermediate struct had a 100-line hand-mapped forward direction next to a 20-line `json.Unmarshal` reverse direction; the asymmetry was the code telling us the forward direction was wrong.
- **Explicit fallbacks for every external dependency.** The first test to write for enrichment code is "what happens when the service is down," not the second. Cautionary tale: hamnut-blocks-logging — a failed hamnut lookup propagated as fatal and prevented QSO entry entirely.
- **Transaction boundaries are conscious design.** Multi-table writes need an explicit decision about what's atomic and what's best-effort. Cautionary tale: v1 `LogQso` ran four sequential non-transactional writes and could leave a QSO stored without its upload-queue row.
- **Characterization tests before refactoring.** Write tests that freeze current observable behavior first; then refactor against them. Don't trust "CI passes" as evidence the refactor is correct unless CI was exercising the behavior you touched.
- **Enumerate all API consumers before designing any endpoints.** The three Wails apps (logging, logbook, config) have different needs; a logging-centric API sketch will miss the logbook-management surface entirely.

## Code style

Standard Go idioms (`gofmt`, `goimports`, Effective Go). Project-specific notes that override generic advice:

- **Short single-letter names are encouraged** for tight scopes and method receivers — consistent with Go convention (`r Reader`, `s *Service`, `t *T`). Avoid generic names like `data`, `item`, or `value` unless the context makes purpose obvious.
- **Use `internal/errors` for operation-tagged errors.** The canonical idiom is a package-scoped `const op errors.Op = "pkg.Func"` followed by `errors.New(op).WithErr(err).WithMsg("context")` (or `.WithMsgf(...)` for formatted messages). All builder methods are nil-safe and chainable. Prefer this over `fmt.Errorf` in code that belongs to the tagged-error paths. There is no `Errorf` convenience method — if you need to wrap a formatted cause, use `.WithErr(fmt.Errorf("...: %w", err))` explicitly. See `internal/errors/doc.go` for the full pattern and `docs/reviews/internal-errors.md` for the review that shaped it.
- **Use `internal/iocdi` for service lifecycle and dependency wiring.** The `di.inject:"..."` struct tag convention on Service fields is how dependencies are resolved. Don't hand-wire services in new code; register them with the container.
- **Comments explain *why*, not *what*.** Well-named code documents its own mechanism. Document non-obvious constraints, invariants the code relies on, and the reason a specific approach was chosen over alternatives. Don't narrate what a function obviously does; don't reference specific callers or tickets (that belongs in PR descriptions).
- **Keep functions short and focused** on a single responsibility. Avoid deep nesting and long parameter lists.
- **Line length 100–120 characters** is a reasonable ceiling; `gofmt` doesn't enforce it but readability suffers past that.
- **For Svelte frontends:** prefer Svelte 5 runes (`$state`, `$derived`, `$props`) over Svelte 4 syntax. Use snippets for reusable UI chunks within components when logic is tightly coupled; reach for separate files when it isn't.

## Project idioms

Conventions that carry forward from v1 and that new v2 code should follow. These are real project rules, not general Go advice.

- **`internal/types` only imports the Go standard library.** It is deliberately dependency-free to prevent cyclic dependencies — the whole project imports from `types`, so anything `types` imports is transitively imported everywhere. Adding a non-stdlib import to `types` is almost certainly a mistake; push the dependency outward into the consumer package instead.
- **Service lifecycle pattern.** Services follow `Initialize()` → `Open()` / `Start(ctx)` → `Close()` / `Stop()`, with all lifecycle methods idempotent. `Initialize()` validates config and dependencies; `Start` / `Open` begins operation; `Stop` / `Close` does graceful shutdown. New services should match this shape for composability with existing DI wiring.
- **DI `ServiceName` constants.** Each service exposes a `ServiceName` constant used as its bean ID in the `iocdi` container. Shared service names live in `internal/types/services.go` (e.g. `types.SqliteServiceName`); app-specific services define their own locally (e.g. `facade.ServiceName = "logging-app-facade"`). This convention keeps DI wiring discoverable by grep.
- **sqlboiler-generated models are not hand-edited.** Files under `internal/database/sqlite/models/` are regenerated from the schema; hand edits get clobbered on the next run. The ORM choice itself is undecided for v2 (see `docs/v1-analysis/design-decisions-log.md` → "ORM / query generator choice"), but while sqlboiler is still in use, treat generated output as read-only.
- **Runtime data path resolution via `utils.WorkingDir()`.** Anything that needs the on-disk data directory (SQLite DB, logs, config) calls this helper. Resolution order: explicit argument → `SM_WORKING_DIR` env var → the executable's own directory. Don't hand-roll path resolution elsewhere.

## Error handling

- **Handle errors explicitly.** No silent failures. Log errors with operation context using the `errors.Op` pattern.
- **Enrichment errors are never fatal.** External service failures log a warning and fall through to a cached value or a default. See the "Enrichment never blocks logging" invariant.
- **Wrap errors with meaningful context** as they propagate; don't return naked errors from deep call stacks.

## Testing

- **Integration tests are the default** for anything touching storage, services, or cross-package flows. Use real `&sqlite.Service{}` with in-memory SQLite databases. Do not introduce mock interfaces for internal services.
- **Unit tests for pure logic** — parsing, math, format conversions, adapter round-trips. No mocks needed.
- **Test the error path first for enrichment code.** Per the invariant: "what does this do when hamnut/QRZ is down" is the test that should exist before the happy-path test is written.
- **Characterization tests before refactoring** untested code. Freeze the current behavior with tests, then refactor.
- **Cover edge cases and error paths,** not just the happy path.
- **Go:** standard `testing` package. `github.com/stretchr/testify` is fine when it makes assertions cleaner.
- **TypeScript/Svelte:** Vitest for unit and component tests; Playwright for E2E.

## Dependencies

- **Minimize external dependencies.** Prefer the standard library. Document why a non-obvious dependency is needed.
- **Home-grown choices are deliberate.** `internal/iocdi` (DI), `internal/serial` and `internal/cat` (rig control), the custom ADIF parser — all chosen after considering alternatives and judged lighter or better-fit than the popular options. Don't suggest replacing them with `uber-go/fx`, `google/wire`, hamlib, etc. without a concrete reason specific to this project's needs.
- **Keep dependencies current** but don't upgrade prophylactically during unrelated work — version bumps get their own commit.

## Commits and version control

- **Atomic commits with meaningful messages.** One logical change per commit. Commit message explains the *why*, not just the *what*.
- **Don't commit secrets.** `.env`, credentials, configuration with real tokens are gitignored; don't add them.
- **Don't touch the `v1` branch** unless fixing a v1 bug the user surfaced while running the software. Main is where v2 work happens; v1 is frozen except for user-reported fixes.
