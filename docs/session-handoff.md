# Station Manager — Session Handoff

**Purpose:** rolling handoff document across Claude sessions. Captures what was
done in the previous session, where the repo currently is, and **what the next
session should pick up**. Read this first when starting a session — it exists
precisely so we don't re-derive state or redo finished work.

**How to use this document:**

- **At session start:** read top-to-bottom. The "Current state" section tells
  you where the repo is. The "Next steps" section tells you what to do. If the
  next session's goals have already been set, work from them.
- **At session end:** the assistant updates this document before stopping.
  Move anything in "Next steps" that was completed into "What happened this
  session" with a date. Leave anything unfinished in "Next steps" and add new
  items discovered during the session.
- **Rolling window:** keep roughly the last 2–3 sessions of history in "What
  happened." Older entries can be summarized or elided — the long-form record
  lives in the git history, the v1-analysis docs, and the memory files.
- **Durable facts go in memory files,** not here. This document is for
  transitory session-to-session state. If something is stable across all
  future sessions (a project invariant, a user preference, a design rule),
  capture it in a memory file under `~/.claude/projects/.../memory/`.

---

## Current state (as of 2026-04-18 end-of-session)

### Both carry-forward library packages are in their v2 final state

Sessions 5 and 6 completed full code reviews and fix passes on
`internal/errors` and `internal/logging` — the two most load-bearing
carry-forward library packages. Both are now audited, comprehensively
tested, cleanly documented, and in a state that should not need
revisiting except for genuine bug fixes. The full review documents
with their resolution annotations live in `docs/reviews/`.

### v2 daemon HTTP API design is captured in `docs/v2-design/api.md`

Session 5 produced the HTTP API design brief: a ~270-line document
covering consumer enumeration, dedupe key shape, async forward
lifecycle, pagination model, SSE event vocabulary, and error response
envelope. Every load-bearing cross-cutting decision for the daemon's
HTTP API surface is in that document, with explicit "design brief,
not spec" framing and an anti-waterfall commitment that every
decision is revisable against running code.

### HTTP framework decision settled: stdlib `net/http`

Session 5 settled the framework question: **stdlib `net/http`**, with
`github.com/go-chi/chi/v5` as an optional small router if path-parameter
routing gets awkward. No Gin, no Fiber, no Echo. Rationale: Unix socket
support is native and clean in stdlib; the API surface is ~15-20
endpoints; testing via `httptest` is first-class; middleware composes
universally; the project convention (CLAUDE.md) favors lightweight
home-grown or stdlib choices over heavy framework dependencies; and
frame performance is irrelevant at personal-operator scale. Documented
in `docs/v2-design/api.md` Section 2.

### v2 milestone-1 tree shape (unchanged since session 3)

```
station-manager/
├── cmd/
│   ├── smd/       # daemon binary entry point (stub)
│   ├── server/    # reserved slot (empty)
│   └── tools/     # reserved slot (empty)
├── internal/
│   ├── adif/            # carry-forward (ADIF parser)
│   ├── api/             # NEW — HTTP handler layer stub
│   ├── config/          # REWRITTEN fresh (minimal daemon config)
│   ├── database/sqlite/ # carry-forward (with simplified adapters/)
│   ├── enums/           # carry-forward
│   ├── errors/          # AUDITED and reviewed (session 5 — v2 final state)
│   ├── iocdi/           # carry-forward
│   ├── logging/         # AUDITED and reviewed (sessions 5-6 — v2 final state)
│   ├── qsoservice/      # NEW — daemon domain layer stub
│   ├── types/           # PRUNED — QSO core only
│   └── utils/           # carry-forward
├── docs/
│   ├── v1-analysis/
│   ├── v2-design/
│   ├── reviews/
│   └── session-handoff.md
├── CLAUDE.md, DEVELOPING.md, README.md, RELEASING.md
├── Taskfile.yml
├── go.mod, go.sum
```

**Single root `go.mod`** at module path `github.com/ColonelBlimp/station-manager`.
Import paths for carry-forward packages are unchanged because `internal/`
was already at the right relative depth under the v1 `internal/` module.

**11 internal packages.** Everything in this tree is either a deliberate
carry-forward (two of which — errors and logging — have been formally
audited and brought to v2 final state) or a v2 rewrite stub.

### Repo state — all commits pushed to origin

**Tags** (pushed): `pre-ft8-removal` at `1ae516d`, `v1.0.0` at `0e158ec`.

**Branches** (pushed):
- `main` at `af46e43` — session 6 end-of-session state. 10 commits ahead
  of where session 2 left origin (`5ef55c1`).
- `v1` at `0e158ec` — unchanged since session 2. The day-to-day working
  branch for ham radio operations.

**Working tree:** clean. Nothing uncommitted, nothing stashed.

### Workspace shape — one Go module, no `go.work`

Five binaries are expected in the eventual v2 tree (cmd/smd plus four
reserved or future slots), but currently only `cmd/smd` exists as a
stub. No `go.work` file. Single root `go.mod`.

Empty reservation slots (kept on purpose, not dead code):
- `cmd/server/` — for a future SM-Online public server binary
- `cmd/tools/` — for future dev/ops/admin CLI tools

### Documentation inventory

**`docs/v1-analysis/`** — the durable v1 analysis record (the spec source
for v2 design work):
- `architecture-map.md` — what v1 actually contained, module by module
- `bug-inventory.md` — known issues, fixed and outstanding
- `design-decisions-log.md` — keep/change/delete verdicts on every major
  shape decision, including the "v2 rewrite vs. refactor" execution-path
  entry
- `invariants.md` — load-bearing rules. Master rule: "Nothing blocks
  logging a QSO, except catastrophic local failure." See also
  "Enrichment never blocks logging" and "Forwarding never blocks
  logging" as specific applications.
- `lessons-for-v2.md` — synthesis document: patterns to apply, patterns
  to avoid, what v1 got right, provisional v2 scope

**`docs/v2-design/`** — v2 design decisions as they're made. Scope is
deliberately kept separate from `v1-analysis/`:
- `structure.md` — repo layout, module boundaries, shared `internal/`,
  source-vs-wire compatibility axes, milestone 1 and 2 target trees
- `api.md` — the HTTP API design brief (new in session 5). Contains all
  six cross-cutting decisions plus a provisional endpoint sketch.
- Future siblings: `milestones.md`, `db-layer.md`, `forwarding.md`,
  `multi-rig.md`, `logging-app-resilience.md` — added as their
  corresponding design questions get answered.

**`docs/reviews/`** — code review documents (new tree created in session
5). Each review is a thorough audit of a package with categorized
findings, action plan, and resolution annotation once fixes land:
- `internal-errors.md` — session 5. **All 11 findings applied and
  resolved.** Preserved as historical record.
- `internal-logging.md` — sessions 5 (review written) through 6
  (all fixes applied). **All 14 findings actioned** (12 fixed, 1
  accepted as-is with documentation, 1 noted and deferred).

**`docs/session-handoff.md`** — this file. Rolling cross-session state.

**Memory files** (`~/.claude/projects/.../memory/`) — durable facts and
invariants used across all sessions. Session 5 added two new feedback
memories: `feedback_one_question_at_a_time` (ask one design question
per turn in exploratory conversations) and `feedback_no_magic_numbers`
(runtime values come from config, code constants only as fallback
defaults). See `MEMORY.md` index for the full list.

---

## What happened in the 2026-04-18 session (session 6)

### Goals set for the session

- Apply the findings from `docs/reviews/internal-logging.md` to bring
  the logging package to its v2 final state before daemon code starts.
- Verify that the shutdown race fix (finding 4.1) actually resolves
  the v1 CI data race — or at least plausibly does.
- Don't revisit the package after this; whatever's going to be fixed
  should land in this session's commits.

### What got done

1. **Applied review findings 4.1, 4.2, 4.3 + discovered and fixed a
   pre-existing embedding bug** (commit `7359b6c`):
   - **4.1 (critical):** dropped the shutdown force-drain loop in
     `Service.Close()` that was double-decrementing the counter when
     in-flight goroutines called their own `trackedLogEvent.Msg()`
     defer cleanup. The panic-on-`wg.Done()`-past-`wg.Add()` pattern
     is gone.
   - **Embedding bug (discovered while implementing 4.1):** the old
     `trackedLogEvent` type embedded `logEvent` and overrode only the
     terminal methods (Msg/Msgf/Send). But because the typed field
     methods (Str, Int, etc.) are defined on `*logEvent` and return
     `*logEvent` via `return e`, calling any of them on a
     `*trackedLogEvent` dispatched through method promotion to the
     embedded `*logEvent` and returned the inner pointer — losing the
     trackedLogEvent identity. Any subsequent terminal call hit the
     non-tracking logEvent method, so counters never decremented for
     chained calls. The old force-drain was masking this. Fix: merged
     the two types into one `logEvent` with optional tracking fields
     (`service`, `location`). Terminal methods conditionally run
     `finish()` based on whether `service != nil`. Also closed
     finding 4.9 (duplicated defer cleanup across three terminal
     methods) as a side effect.
   - **4.2 (high):** moved the hardcoded `timeoutMS := 100` literal to
     a named `defaultShutdownTimeoutMS` constant in `consts.go`.
   - **4.3 (high):** extracted debug location tracking
     (`activeOpLocations`, `trackLocation`, `untrackLocation`,
     `snapshotLocations`) into two build-tagged files:
     `debug_tracking.go` (`//go:build logging_debug`) with the real
     implementation and `debug_tracking_nop.go` (`//go:build
     !logging_debug`) with no-op stubs. Release builds now pay zero
     overhead for the debug-only tracking.

2. **Applied finding 4.4** (commit `ba5f8ba`): extracted the 7-case
   zerolog level-dispatch switch into an `eventForLevel` helper,
   eliminating the duplication between `logEventBuilder` and
   `newTrackedContextLogEvent`. Small but hygienic.

3. **Applied findings 4.6, 4.7, 4.8, 4.10–4.14 plus added regression
   tests** (commit `af46e43`):
   - **4.6 (medium):** reordered `logEventBuilder` to short-circuit
     the level check before incrementing counters or acquiring locks.
     Filtered-out events now return an untracked no-op after two
     atomic reads (logger pointer load + GetLevel) instead of the
     full counter+lock dance. Real performance win in the common case
     of Debug-at-runtime-Info logging.
   - **4.7 (medium):** added a dedicated `debugMu sync.Mutex` field to
     Service. Debug tracking uses it exclusively; the main mutex
     `s.mu` is now only for isInitialized transitions and fileWriter
     close. Completely isolates the two concerns.
   - **4.8 (medium):** deleted `internal/logging/dump.go` entirely.
     Zero production callers (grep confirmed), significant security
     hazard (walks any struct via reflection, logs every exported
     field at Debug — could leak credentials). Removed the
     corresponding `TestService_Dump` from `logging_test.go`.
   - **4.10 (low):** documented the deliberate method asymmetry
     between `LogContext` (12 methods) and `LogEvent` (~30 methods)
     in the `LogContext` interface doc comment. No code change.
   - **4.11 (low):** deleted the pointless `parseLevel` wrapper in
     `helper.go` (it was a 6-line passthrough to
     `zerolog.ParseLevel`). Updated two callers to call
     `zerolog.ParseLevel` directly.
   - **4.12 (low):** documented the terminal-method hazard (chains
     built but never terminated leak their tracked counter) in the
     `LogEvent` interface doc comment. Go has no way to enforce
     terminal-method calls at the type level, so documentation is
     the fix.
   - **4.13 (low):** normalized the four `errMsg*` constants in
     `consts.go` from title-case-with-period ("Logging config is
     nil.") to Go-convention lowercase-without-period ("logging
     config is nil"). The assert.Contains tests that reference these
     constants pass unchanged because they reference the constant
     names, not the literal strings.
   - **4.14 (low):** audited `internal/logging/README.md` (3782
     bytes). Found two pieces of content not duplicated in `doc.go`:
     the error-chain enrichment field reference (error_chain,
     error_root, error_history, error_ops, error_root_op, plus AnErr
     prefixing and a concrete JSON example) and notes on
     DetailedError vs. stdlib error traversal. Found three stale
     items: a "Dump helper" section referencing the function we just
     deleted, a Testing note mentioning Dump, and a wrong import path
     (`github.com/Station-Manager/errors`). **Migrated the useful
     content into `doc.go`** (grew from ~23 to ~78 lines). **Deleted
     `README.md`** — one documentation home, no drift risk.
   - **Regression tests:** added three new tests in `logging_test.go`
     to lock in the behavioral guarantees of sessions 5-6's refactors:
     `TestLogEventBuilder_FilteredLevelSkipsCounters` (bursts 1000
     filtered events and asserts activeOps stays at 0 + Close returns
     in under 100ms — catches regressions of both 4.6 and the
     embedding bug), `TestEventForLevel_AllKnownLevels` (direct test
     for the 4.4 helper across all 7 zerolog levels + unknown),
     `TestNoop_FullChainIsNoOp` (closes the biggest existing coverage
     gap — `Noop()` and its underlying `noopLogger`/`noopLogContext`
     had zero tests before this).

4. **Pushed to origin.** All ten session 3-6 commits are now on
   `origin/main`. The race fix question (does session 6's work fix the
   v1 CI race?) is now testable: the same shutdown-race fix could be
   cherry-picked or reapplied to the `v1` branch, and if `go test
   -race -short ./...` passes there, we've confirmed both are the
   same bug.

5. **Logging test suite runtime dropped from ~15s to ~1.7s.** This is
   not a false speedup — it's a direct consequence of fixing the
   embedding bug. Previously, tests that exercised chained logging
   calls left leaked counters, which caused `Close()` to hit the
   timeout (1–10 seconds depending on config) before proceeding.
   With the real bug fixed, the tests run at their actual speed.

### Review findings status — internal/logging review complete

| Finding | Severity | Status |
|---|---|---|
| 4.1 | critical | DONE (shutdown race + embedding bug, commit `7359b6c`) |
| 4.2 | high | DONE (magic number → defaultShutdownTimeoutMS, `7359b6c`) |
| 4.3 | high | DONE (debug tracking behind logging_debug build tag, `7359b6c`) |
| 4.4 | medium | DONE (eventForLevel helper, commit `ba5f8ba`) |
| 4.5 | medium | noted — accept event.go size, no action |
| 4.6 | medium | DONE (level check short-circuit, commit `af46e43`) |
| 4.7 | medium | DONE (separate debugMu, commit `af46e43`) |
| 4.8 | medium | DONE (Dump deleted, commit `af46e43`) |
| 4.9 | medium | DONE (subsumed by embedding bug fix) |
| 4.10 | low | DONE (LogContext asymmetry documented, `af46e43`) |
| 4.11 | low | DONE (parseLevel deleted, `af46e43`) |
| 4.12 | low | DONE (terminal-method hazard documented, `af46e43`) |
| 4.13 | low | DONE (errMsg constants normalized, `af46e43`) |
| 4.14 | low | DONE (README audited, useful content → doc.go, `af46e43`) |

### What did NOT get done this session

- **Did not write any v2 daemon code.** `cmd/smd/main.go` is still a
  stub with a TODO comment. Real daemon construction is the natural
  next step for session 7.
- **Did not verify the v1 race fix empirically.** The shutdown-race
  fix almost certainly IS the v1 CI race, but nobody has applied the
  fix to the `v1` branch and run `go test -race` there. Parked as a
  v1-branch follow-up.
- **Did not start the internal/adif or internal/database/sqlite
  reviews.** Both are still queued on the carry-forward
  code-review track.

---

## What happened in the 2026-04-17 session (session 5)

### Goals set for the session

- Settle enough of the v2 daemon's HTTP API surface that the first
  handler can be written without accidentally painting into a corner.
- Review and fix the `internal/errors` package so the api.md error
  envelope has a solid foundation.
- Start the `internal/logging` review (but not necessarily finish
  applying its findings).

### What got done

1. **Wrote `docs/v2-design/api.md`** (commit `7fdc010`). A ~270-line
   design brief structured around a single idea: enumerate all
   consumers before designing any endpoints. The document covers:
   - Consumer list: `apps/logging`, `apps/logbook`, `cmd/importer`,
     `cmd/udp-bridge`. Explicit non-consumers: `apps/config` (reads
     config file directly), serial/CAT bridge (independent
     subsystem), SM-Online (forwarding destination, not consumer).
   - Transport: HTTP over Unix domain socket, listener config-driven.
   - Dedupe key: `hash(call + band + mode + start_time_rounded_to_minute)`
     with `?force=true` override for contest edge cases.
   - Async forward lifecycle: `POST /v1/qso` returns on local commit,
     forwarding runs in background, SSE publishes terminal outcomes.
   - Pagination: forward-cursor-only at the API, client-side windowed
     buffer for backward navigation.
   - SSE event vocabulary: `qso.{stored,updated,deleted}`,
     `forward.{succeeded,failed}`. Payload shapes and reconnect
     semantics explicitly deferred to implementation time.
   - Error envelope: JSON with `code`, `message`, `op` fields.
     Handler-layer mapping from internal errors to HTTP responses
     lives in `internal/api`, not `internal/errors`.
   - Explicit "design brief, not spec" framing and an anti-waterfall
     commitment in Section 7.

2. **Strengthened `docs/v1-analysis/invariants.md`** with a new master
   rule: **"Nothing blocks logging a QSO, except catastrophic local
   failure."** The existing "Enrichment never blocks logging" and a
   new "Forwarding never blocks logging" became specific applications.
   The rule requires the logging app to maintain its own local
   text-file fallback independent of the daemon. Captured in the same
   commit as api.md.

3. **Added "Deferred features to investigate" section to the
   handoff** with the logging-app text-file fallback + reconciliation
   feature as the first entry.

4. **Full `internal/errors` code review and fix application** (commit
   `376a3dd`). 11 findings applied in one coherent commit:
   - 4.1 default `"Internal system error."` message removed from
     `New()`.
   - 4.2 `Error()` rewritten to return a rich `"op: msg: cause"`
     format matching stdlib wrapped-error conventions.
   - 4.3 `Root()` uses a depth limit of 100 instead of map-based
     cycle detection (which could panic on non-comparable error
     types).
   - 4.4 `Errorf` deleted entirely. Its dual-role semantics were a
     footgun. All ~20 call sites migrated to either
     `.WithErr(fmt.Errorf("...: %w", err))` for wrapping or
     `.WithMsgf("...", args)` for formatted messages — per-site
     judgment based on whether the format string used `%w`.
   - 4.5 dead `Error` interface type removed.
   - 4.6 unused `PrintChain` debug helper deleted.
   - 4.7 `Cause()` method deleted (duplicated `Unwrap()`). The one
     external caller in `internal/logging/helper.go` was updated to
     use `stderrs.Unwrap`.
   - 4.8 `sentinals.go` renamed to `sentinels.go`.
   - 4.9 ~30 comprehensive tests added — covering construction, all
     builder methods, nil-safety, `AsDetailedError`
     positive/negative/wrapped/nil cases, `errors.Is`/`errors.As`
     interop through mixed chains, `Error()` format across every
     op/msg/cause combination, `Root()` simple/nested/depth-limit/
     unhashable-error cases, and `ErrNotFound` sentinel propagation.
   - 4.10 **Full rename of the builder method family to the `With*`
     convention** to eliminate the naming collision with zerolog's
     terminal `.Msg()`. `Msg → WithMsg`, `Msgf → WithMsgf`,
     `Err → WithErr`. Sweep touched 16 consumer files across
     `internal/adif`, all of `internal/database/sqlite/*`, and
     `internal/logging`. A handful of zerolog/LogEvent chain calls
     and one `context.Context.Err()` call site were incorrectly
     caught by the initial bulk rename and restored manually.
   - 4.11 added a short `doc.go` describing the package's
     operation-tagging philosophy, canonical usage pattern, builder
     semantics, and pointers to api.md and the review doc.
   - Updated `CLAUDE.md` "Code style" section to reference the new
     `With*` pattern and the explicit `.WithErr(fmt.Errorf(...))`
     idiom that replaces `Errorf`.
   - Updated `docs/v2-design/api.md` Section 4.6 to use the new
     method names in its example.
   - Annotated `docs/reviews/internal-errors.md` with a resolution
     note; the original audit text is preserved as historical record.

5. **Wrote `docs/reviews/internal-logging.md`** (commit `a19e802`). A
   ~535-line review with 14 findings categorized by severity (1
   critical, 3 high, 5 medium, 5 low), a strengths list, a fit
   assessment against v2 daemon needs, and a detailed action plan. The
   critical finding (4.1 shutdown force-drain race) was explicitly
   flagged as the likely v1 CI race, with a reconciliation note.

6. **Saved two new feedback memory files** during the api.md
   discussion:
   - `feedback_one_question_at_a_time.md` — during design
     conversations, pose one question at a time and wait for the
     answer before introducing the next. Don't stack multi-question
     messages.
   - `feedback_no_magic_numbers.md` — runtime values come from
     configuration; code constants only as fallback defaults. All
     socket paths, timeouts, sizes, intervals, retry counts, etc.
     are config-driven.

### What did NOT get done this session

- **Did not apply any `internal/logging` review findings.** The review
  document was written but the fix pass was deferred to session 6.
- **Did not write any v2 daemon code.**

---

## What happened in the 2026-04-16 session (session 4)

Short session picking up right after the session 3 restructure
commit. Two goals: write a basic `Taskfile.yml` so `task`-based
vet/build/test works on the new layout, and clear the last v1-era
leftovers (the `scripts/` directory, `assets/xdg/`, `web/shared-utils/`)
that the session 3 restructure had left behind.

**Commit `1ee2ced`** — "Add Taskfile.yml and remove remaining v1
leftovers." Introduced a minimal 5-task `Taskfile.yml` (vet, build,
test with race detector, tidy, plus a default that runs vet + build +
test as a CI-equivalent smoke check). Deleted `scripts/` (the old
Wails-bindings pre-commit hook, dead now that apps are gone),
`assets/xdg/` (Linux desktop entry files for the deleted Wails apps),
and `web/shared-utils/` (TypeScript lib consumed only by the deleted
Wails frontends). `assets/logo.png` stays because `README.md`
references it.

---

## Sessions 1–3 (compressed summaries)

Per the maintenance rule, older session entries are compressed to
one-paragraph summaries. Full detail lives in git history.

### Session 1 (2026-04-14)

The v2 rewrite decision session. Completed the v1 analysis effort
(five documents in `docs/v1-analysis/`: `architecture-map.md`,
`bug-inventory.md`, `design-decisions-log.md`, `invariants.md`,
`lessons-for-v2.md`), chose the v2 rewrite path over incremental
refactoring, decided the repo layout (main = v2, `v1` branch = v1
maintenance), tagged the `pre-ft8-removal` and `v1.0.0` reference
points, created the `v1` branch, and landed three v1-on-main bug
fixes before tagging v1.0.0: the hamnut-blocks-logging fix
(`5288983`), the LogQso/UpdateQso atomicity fix (same commit), and
the sqlite adapter simplification via `QsoAdditionalData` deletion
(`1ae516d`). Ended with the FT8 experiment tree removed from main
(`0e158ec`) and tagged `v1.0.0`, plus a session-handoff doc added
(`66e0af3`).

### Session 2 (2026-04-15)

Housekeeping and v2 structural design session. Pushed session 1's
artifacts upstream. Wrote the first durable v2 design document —
`docs/v2-design/structure.md`, with six structural decisions
(monorepo on main, single go.mod at milestone 1, only Wails apps get
their own modules, shared `internal/`, source-vs-wire compatibility
as separate axes, `internal/*` split only when forced) and migration
plan for the upcoming session 3 restructure. Reviewed and rewrote
`CLAUDE.md` from a generic code-style guide to a Station Manager-
specific project instructions file. Consolidated `AGENTS.md` into
`CLAUDE.md` under a new "Project idioms" section and deleted
`AGENTS.md`. Deleted both v1-era `.github/workflows/*.yml` files
(they were failing on every push due to the v1 data race) and
replaced `RELEASING.md`/`DEVELOPING.md` with short stubs pointing at
the v1 branch. **Commit `5ef55c1`** covers all of session 2's work.

### Session 3 (2026-04-16)

The big restructure session. Executed the full v2 milestone-1 reshape
of main according to `docs/v2-design/structure.md`. Deletion scope:
all three Wails apps (`apps/config`, `apps/logbook`, `apps/logging`),
the server-side database cluster (top-level `internal/database/*.go` +
`internal/database/postgres/` + `internal/adapters/`), the dead
`internal/listeners/*` tree, the orphaned `internal/audio/`, rig
control (`internal/serial/`, `internal/cat/`, `internal/ptt/`),
enrichment and forwarding (`internal/lookup/*`, `internal/forwarding/qrz`,
`internal/email/`, `internal/maidenhead/`, `internal/apikey/`),
`cmd/importer` (deferred to milestone 2 as a thin ADIF-to-HTTP tool),
the v1 `internal/config` entirely (rewritten fresh), and a large
chunk of the v1 `internal/types` grab-bag. Collapsed the five-module
`go.work` workspace into a single root `go.mod`. Hit and resolved a
Go module cache ambiguity problem (every `internal/*` subdirectory
had been its own module at various points in v1 and the cache had
phantom versions of them). Scaffolded `cmd/smd/{main.go,doc.go}`,
`internal/api/doc.go`, `internal/qsoservice/doc.go` as stubs. Wrote
a fresh `internal/config` from scratch (minimal daemon-shaped, ~80
lines, replacing v1's ~1600-line aggregation). **Commit `0010b6e`**
covers all of session 3's work — 730 files changed, 433 insertions,
68,934 deletions.

---

## Next steps (priority order)

Session 7 picks up from here. The library-package review track is
done; the main gating question for the next session is "start writing
real v2 daemon code or keep making design decisions."

### The natural next action

1. **Write the first real daemon code.** `cmd/smd/main.go` is
   currently a stub with a TODO comment. The smallest possible first
   step that exercises the settled cross-cutting decisions end to end:
   - Load `internal/config.Config` (currently a stub) from disk or
     defaults.
   - Initialize the `internal/database/sqlite.Service` via the iocdi
     container.
   - Initialize the `internal/logging.Service` the same way.
   - Bind a `net.Listen("unix", cfg.SocketPath)` listener.
   - Serve `POST /v1/qso` accepting raw ADIF request bodies, parsing
     via `internal/adif`, calling a `qsoservice.Submit()` method that
     writes the QSO + its upload-queue rows atomically per the
     one-fails-all-fail invariant, and returning a JSON envelope
     indicating "stored" or "duplicate".
   - No SSE, no forwarding, no logbook CRUD, no pagination, no
     authentication beyond Unix-socket filesystem permissions. One
     endpoint.
   - Exercise via `curl` over the Unix socket (e.g.
     `curl --unix-socket /tmp/smd.sock -X POST --data-binary @sample.adi
     http://localhost/v1/qso`).

   Expected size: maybe 300–500 lines of new code across `cmd/smd`,
   `internal/api`, `internal/qsoservice`, and probably some filling-in
   of `internal/config`. Several sessions of work, but the first
   concrete v2 daemon milestone.

### v2 design work (follow-ups to `docs/v2-design/`)

2. **Write `docs/v2-design/milestones.md`** to concretely define what
   milestone 1 "done" means. Proposed shape: daemon + one POST endpoint
   + a `curl` test script is milestone 1a; milestone 1b adds the
   remaining QSO CRUD endpoints + contact history + contest dupe check
   + a minimal forwarder; milestone 2 reintroduces the Wails clients.
   Writing it down forces commitment and is the explicit mitigation
   for the "interminable 90%" failure mode.

3. **Pick the ORM/generator approach** → `docs/v2-design/db-layer.md`
   when v2 DB work starts. sqlboiler (v1's choice, currently
   carried-forward), Bob, sqlc, or hand-rolled. Not urgent; the
   current plan is "sqlboiler stays until there's a reason to
   change." Make it an explicit decision when the sqlite review
   happens.

4. **Think about multi-rig as a first-class assumption** →
   `docs/v2-design/multi-rig.md` when the bridge design starts. v1
   has no multi-rig support. Cheaper to answer "does `types.Qso`
   carry a rig identifier" before the daemon API is frozen than
   after.

5. **Expand `CLAUDE.md` frontend guidance when Wails clients return
   in milestone 2.** The current file has deliberately thin
   TypeScript/Svelte 5 coverage because there's no frontend code in
   the v2 tree yet. When the first Wails app lands, expand with
   concrete guidance on component organization, Svelte 5 state/store
   patterns, TS strictness level, Wails bindings conventions, form
   handling patterns, the API client layer that wraps a future
   `internal/smclient` for the Svelte side, error surfacing, and
   loading/optimistic-UI patterns.

### Carry-forward package code-review track

Two of the seven carry-forward library packages have been formally
audited and brought to v2 final state (`internal/errors`,
`internal/logging`). Five remain as carry-forward-for-code-review
candidates. Priority order from highest to lowest expected yield:

6. **Audit `internal/database/sqlite`.** Flagged as "probably has a
   smarter implementation." The session 1 adapter simplification was
   one pass; the rest of the package (the `Service` struct, the
   `api.go`/`api_context.go` split, the migrations infrastructure,
   the error wrapping, the `requiredCfgs` field tied to the v1
   forwarder row-limit) has not been reviewed at that depth.
   **Highest-value review target.** Likely to surface an ORM/driver
   decision in the process, feeding into `db-layer.md`.

7. **Audit `internal/adif`.** ~2000 lines of ADIF parser.
   Load-bearing for milestone 1 because the daemon's POST endpoint
   accepts raw ADIF bodies. Review for any v1-era shortcuts that
   could be cleaned up, and for test coverage gaps around the
   "malformed ADIF input" error paths.

8. **Audit `internal/iocdi`.** The home-grown DI container. Generally
   considered a keep, but worth a read to confirm it makes sense for
   the v2 daemon's smaller service graph. If the daemon ends up with
   only 4–5 services, manual wiring might be cleaner than reflection-
   based DI.

9. **Audit `internal/types` (pruned).** 13 files remain after the
   session 3 prune. Some (`adif.go`, `datastore.go`, `logging.go`) may
   still be prunable now that the daemon's actual needs are clearer.

10. **Audit `internal/enums`, `internal/utils`.** Less urgent — these
    are small, self-contained, and have been touched only
    incidentally. Each should get at least a quick pass when its
    first v2 consumer lands.

### Deferred features to investigate

These are features that are understood to be desirable but explicitly
*not* day-one requirements. They are captured here so they don't get
lost between sessions. When a feature reaches "ready to design," it
graduates into a real `docs/v2-design/*.md` sibling.

- **Logging-app text-file fallback reconciliation** (captured
  2026-04-17, session 5). The master invariant "Nothing blocks
  logging a QSO" (see `docs/v1-analysis/invariants.md`) requires the
  logging app to maintain its own local durability independent of
  the daemon: when the daemon is unavailable, the logging app falls
  back to writing the QSO to a local text file (plain-text ADIF).
  This part is a day-one requirement of the logging app when it
  returns in milestone 2+.

  The *reconciliation* flow is the follow-up: when the daemon comes
  back online after an outage, the logging app resubmits the QSOs
  it captured only to its text-file fallback during the outage.
  **Mechanism:** the logging app submits each queued entry via the
  daemon's normal submit API (`POST /v1/qso`, whatever the endpoint
  ends up being). It does **not** use a special import endpoint or
  a direct file-merge path. The dedupe key
  (`hash(call + band + mode + start_time_rounded_to_minute)`)
  silently absorbs any accidental double-submissions from
  overlapping reconciliation attempts.

  Not a milestone 1 concern, not a milestone 2 day-one requirement.
  Post-first-flight refinement. When it becomes real, it gets its
  own design doc (probably `docs/v2-design/logging-app-resilience.md`
  or similar).

- **Daemon dashboard / monitoring UI.** Considered out loud during
  session 5's SSE discussion as a possible future consumer of the
  daemon's event stream. Not a milestone 1 concern. If built, it
  would subscribe to the same SSE endpoint (`/v1/events`) that
  `apps/logging` and `apps/logbook` use — no new protocol design
  required.

### v1 branch follow-ups (distinct track from v2 main work)

v1 is a live maintenance branch. Bug fixes and improvements for v1
land there directly, not on main. These items are tracked separately
because main has diverged significantly from v1 and the two tracks
should not bleed into each other.

- **Data race — candidate fix identified session 6, not yet verified
  on v1.** The failing `go test -race ./... -short` in the v1
  `validate.yml` workflow (before we deleted that workflow in session
  2) is almost certainly the shutdown force-drain race fixed in
  session 6's commit `7359b6c`. The symptoms match exactly: the race
  detector tripped on concurrent access to `activeOps` during
  `Service.Close()` + in-flight logging goroutines, which is the
  exact pattern the force-drain loop was racing with. **To verify:**
  check out the `v1` branch, apply the equivalent of session 6's
  `trackedLogEvent → logEvent` merge and `Close()` force-drain
  removal (the fix is confined to `internal/logging/event.go` and
  `internal/logging/service.go`), then run `go test -race -short
  ./...` on v1. If it passes, we've fixed both. If it still fails,
  there's a second race to hunt. Either way, the v1 fix could
  justify a `v1.0.1` tag if there are other v1 bugs accumulated from
  day-to-day use.

- **Known-but-deferred items from the bug inventory that still apply
  to v1:**
  - Hardcoded QRZ forwarder in `LogQso`/`UpdateQso` (see
    `docs/v1-analysis/bug-inventory.md`). Not a crash, blocks
    multi-destination forwarding. Unlikely to be fixed on v1
    because the redesign is a v2 concern.
  - `DatabaseServiceInterface` vs `*sqlite.Service` signature
    mismatch — cosmetic, unused scaffolding. Could be deleted from
    v1 if it's bothering anything, but probably not worth the churn.

### Maintenance of this handoff document

11. **Update this file at the end of every session.** Move completed
    items from "Next steps" into "What happened," add new items as
    they surface, prune "What happened" to keep it to the last 2–3
    sessions of history. The git history and the v1-analysis /
    v2-design docs are the long-form record; this file is the
    quick-reference for cross-session continuity.

12. **Session compression applied session 6.** Sessions 1–3 are now
    one-paragraph summaries. After session 7 lands, session 4 becomes
    the next candidate for compression to a summary.
