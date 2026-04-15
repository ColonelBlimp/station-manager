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

## Current state (as of 2026-04-16 end-of-session)

### main is now at the v2 milestone-1 layout (working tree, uncommitted)

Session 3 executed the restructure commit that reshapes main into the v2 milestone-1 layout specified in `docs/v2-design/structure.md`. The working tree has ~720 file changes — mostly deletions — and the v2 tree builds clean, vets clean, and tests clean across every package. **Not yet committed.** The commit is waiting on user review and any last artifact-cleanup decisions.

Shape of main's tree after session 3:

```
station-manager/
├── cmd/
│   ├── smd/       # daemon binary entry point (NEW, stub)
│   ├── server/    # reserved slot (empty)
│   └── tools/     # reserved slot (empty)
├── internal/
│   ├── adif/            # carry-forward (ADIF parser)
│   ├── api/             # NEW — HTTP handler layer stub
│   ├── config/          # REWRITTEN fresh (minimal daemon config)
│   ├── database/sqlite/ # carry-forward (with session-1 simplified adapters/)
│   ├── enums/           # carry-forward
│   ├── errors/          # carry-forward
│   ├── iocdi/           # carry-forward
│   ├── logging/         # carry-forward
│   ├── qsoservice/      # NEW — daemon domain layer stub
│   ├── types/           # PRUNED — QSO core only
│   └── utils/           # carry-forward
├── docs/
│   ├── v1-analysis/
│   ├── v2-design/
│   └── session-handoff.md
├── CLAUDE.md, DEVELOPING.md, README.md, RELEASING.md
├── go.mod, go.sum
```

**Single root `go.mod`** at module path `github.com/ColonelBlimp/station-manager`. No `go.work`, no per-package `go.mod` files. Import paths for carry-forward packages are unchanged because `internal/` was already at the right relative depth under the v1 `internal/` module — the module-root move was semantically transparent to the import strings.

**11 internal packages** (was ~25 in v1). Everything in this tree is either a deliberate carry-forward (subject to code-review-as-we-go during v2 construction) or a v2 rewrite stub.

### v2 structural decisions are captured in `docs/v2-design/structure.md`

Session 2 produced the first durable v2 design document, and session 3 executed it. Before touching any v2 code, read `docs/v2-design/structure.md` — it captures the repo layout, module boundaries, release model, and milestone-1 vs milestone-2 target trees with rationale for every decision. Any future session that questions "why is v2 shaped this way" should find the answer there.

### The v2 rewrite decision is made (unchanged from 2026-04-14)

After completing a collaborative v1 analysis effort (five documents in
`docs/v1-analysis/`), the author chose the **v2 rewrite** path over
incremental refactoring. Rationale and the full entry are in
`docs/v1-analysis/design-decisions-log.md` → "v2 rewrite vs. v1 incremental
refactor." The short version:

- Roughly half the problem list is architecture-level (daemon split, serial
  bridge, forwarder fan-out, multi-rig), which is ~80% of a rewrite's work
  anyway, just spread across phased commits that are harder to reason about.
- Personal/learning project with a single user — the refactor-safety-net
  argument doesn't apply.
- The analysis docs give v2 an unusually concrete spec, mitigating the
  "interminable 90%" failure mode.
- v1 can be preserved as a frozen reference and a working maintenance branch
  while v2 is built on main.

### Repo state

**Tags:**
- `pre-ft8-removal` at commit `1ae516d` — snapshot of the tree including the
  FT8 experiments (`internal/ft8`, `internal/ft8x`, `cmd/ft8`, `cmd/ft8test`).
  If anything in that experiment code turns out to be useful later, it's
  recoverable from here.
- `v1.0.0` at commit `0e158ec` — the frozen v1 reference point. Post-cleanup:
  FT8 code removed, legacy docs removed, workspace down to 5 modules.

**Branches:**
- `main` — at `v1.0.0` as of 2026-04-14. This is where v2 construction work
  happens going forward. It will diverge from `v1` as soon as v2 work starts.
- `v1` — created at `v1.0.0`. This is what the author checks out to build
  and run Station Manager for day-to-day ham radio operations. Any bug
  surfaced while running v1 should be fixed on this branch, then the fix can
  inform v2 design but does not need to be merged anywhere.

**Pushed to origin 2026-04-15** (session 2): `main` (including the session-1 doc-update commit `66e0af3`), the `v1` branch (tracking `origin/v1`), and both tags (`v1.0.0`, `pre-ft8-removal`) are all on origin. Nothing v1-related is local-only anymore.

### Workspace shape (post-cleanup)

Five Go modules in `go.work`:

- `./apps/config` — Wails app, configuration editor
- `./apps/logbook` — Wails app, logbook management and historical QSO editing
- `./apps/logging` — Wails app, the main real-time QSO entry application
- `./cmd/importer` — ADIF bulk importer CLI
- `./internal` — shared library packages (~25 packages after FT8 removal)

Empty reservation slots (kept on purpose, not dead code):
- `cmd/server/` — for a future SM-Online public server binary
- `cmd/tools/` — for future dev/ops/admin CLI tools

### Documentation inventory

**`docs/v1-analysis/`** — the durable v1 analysis record (the spec source for
v2 design work):
- `architecture-map.md` — what v1 actually contains, module by module
- `bug-inventory.md` — known issues, fixed and outstanding
- `design-decisions-log.md` — keep/change/delete verdicts on every major
  shape decision, plus the "v2 rewrite vs. refactor" execution-path entry
- `invariants.md` — load-bearing rules that must carry forward
- `lessons-for-v2.md` — synthesis document: patterns to apply, patterns to
  avoid, what v1 got right, provisional v2 scope

The **synthesis document** (`lessons-for-v2.md`) is the single most
important read before any v2 design choice.

**`docs/v2-design/`** — v2 design decisions as they're made. Scope is
deliberately kept separate from `v1-analysis/`:
- `structure.md` (2026-04-15) — repo layout, module boundaries, shared
  `internal/`, source-vs-wire compatibility axes, milestone 1 and 2 target
  trees, migration-from-main plan. Six decisions with full rationale.
- Future siblings: `api.md` (HTTP API shape), `milestones.md` (milestone
  plans), `db-layer.md` (ORM/generator choice), `forwarding.md` (fan-out
  redesign), etc. — added as the corresponding design questions get answered.

**`docs/session-handoff.md`** — this file. Rolling cross-session state.

**Memory files** (`~/.claude/projects/.../memory/`) — durable facts and
invariants used across all sessions. Key entries: `project_sm_restructure`,
`project_sm_v2_analysis`, `project_sm_design_invariants`, `project_sm_overview`,
`user_profile`, `feedback_design_patterns`, `project_sm_serial_bridge`,
`project_ft8_library`. See `MEMORY.md` index for the full list.

---

## What happened in the 2026-04-16 session (session 3)

### Goals set for the session

- Execute the restructure commit that reshapes main into the v2 milestone-1 layout, per `docs/v2-design/structure.md`.
- Verify the resulting tree builds, vets, and tests clean before committing.
- Do not start writing daemon code yet — structure first, implementation next.

### What got done

1. **Session start — verified git state.** Read `docs/session-handoff.md`, confirmed session 2's housekeeping commit (`5ef55c1`) and the v1 branch had been pushed upstream. Working tree clean, main up to date with origin. Natural next action per the handoff was the restructure commit.

2. **Pre-flight verification checks.** Before touching the tree, verified four things from the restructure plan:
   - `internal/audio` reverse-dep check — only self-references, safe to delete.
   - `internal/listeners` reverse-dep check — external consumers only in `apps/logging/` which is being deleted anyway.
   - `internal/serial` / `cat` / `ptt` reverse-dep check — external consumers only in `apps/logging/`; the packages carry forward only for the v1 branch.
   - `internal/database/` structural check — confirmed the surgical deletion shape (top-level `*.go` files + `postgres/` subdirectory go; `sqlite/` subdirectory stays).

3. **Confirmed the `go.mod` collapse preserves import paths.** Current v1 module at `internal/go.mod` has path `github.com/ColonelBlimp/station-manager/internal`, so `./internal/types` is imported as `github.com/ColonelBlimp/station-manager/internal/types`. New root module at path `github.com/ColonelBlimp/station-manager` produces the same import path for the same package. No rewrite of import statements in carry-forward code needed.

4. **Mapped out the restructure plan** — one big commit with explicit delete list, create list, scaffold list, and migration sequence. Got user go-ahead.

5. **First execution wave** (before hitting complications):
   - Deleted `go.work`, all five v1 `go.mod`/`go.sum` files, `apps/`, `Taskfile.yml`, `Taskfile.wails.yml`.
   - Deleted the server-side DB cluster: top-level `internal/database/*.go`, `internal/database/postgres/`, `internal/adapters/`. Kept `internal/database/sqlite/`.
   - Deleted `internal/listeners/`, `internal/audio/`, `internal/serial/`, `internal/cat/`, `internal/ptt/`.
   - Fixed the session-1 `internal/adif/slice_test.go` wrinkle by swapping its import from the deleted `internal/adapters` framework to the simplified client-side `internal/database/sqlite/adapters.QsoModelToType`.
   - Scaffolded `cmd/smd/main.go` + `doc.go`, `internal/api/doc.go`, `internal/qsoservice/doc.go` with intent comments referencing the invariants.
   - Wrote a minimal root `go.mod` with module path `github.com/ColonelBlimp/station-manager`.

6. **Hit the Go module cache ambiguity problem.** First `go mod tidy` run failed with:
   ```
   ambiguous import: found package github.com/ColonelBlimp/station-manager/internal/database/sqlite in multiple modules:
     github.com/ColonelBlimp/station-manager (local)
     github.com/ColonelBlimp/station-manager/internal/database (cached v1 pseudo-version)
   ```
   Root cause: every `internal/*` subdirectory in v1 had been its own Go module at various points (each with its own `go.mod`), and the proxy/cache had recorded all of them as valid modules with their own pseudo-versions. Go's longest-prefix resolver was matching the cached v1 `internal/database` module before falling back to the local root module. Every `internal/database/sqlite/...` import was ambiguous, every `internal/database/sqlite/adapters/...` import was ambiguous, etc.

7. **Resolved the ambiguity** after several iterations:
   - First tried clearing the station-manager entries from the Go module cache. Tidy re-downloaded them from the proxy, same ambiguity.
   - Then tried `GOPROXY=off go mod tidy` with an explicit `require` list. That surfaced real missing-dep errors (because some carry-forward code still imported packages like `go-playground/validator/v10` that hadn't been added to the explicit list yet) but confirmed the ambiguity path disappears when the proxy isn't consulted.
   - Combined: clear the station-manager cache entries, use an explicit `require` list recovered from v1's `internal/go.mod`, then let `go mod tidy` populate indirect deps from the concrete direct set. That worked.

8. **Hit the scope issue that the restructure plan had not anticipated.** During tidy iteration, build errors revealed that `internal/types/serial.go` imports `go.bug.st/serial` — meaning the v1 types package had a non-stdlib import (a violation of the "types only imports stdlib" invariant we wrote into CLAUDE.md). Further investigation showed `internal/config` was deeply wired into the v1 type universe (rig configs, CAT state values, audio playback, FT8, server config, listener configs), and its dependency chain pulled in a lot of v1-specific types.

9. **Paused for a user decision on scope.** Proposed three options:
   - **A:** Restore serial/cat/ptt to main and keep v1 types + config as-is. Violates "types only stdlib" invariant; violates narrow-daemon-scope thinking. Smallest change.
   - **B:** Prune `internal/config` and `internal/types` to a v2-minimal shape. Medium change.
   - **C:** Delete v1 config and types entirely, write both fresh. Biggest change, most philosophical purity.

10. **User chose a refined version of C:** keep `errors`, `logging`, `adif`, `database/sqlite`, `iocdi`, `enums`, `utils`, and a pruned `types` for later code-review-as-we-go (explicitly framing carry-forward as "carry forward to code-review," not "carry forward as gospel"). Delete `cmd/importer` (defer to milestone 2 as a thin ADIF-to-daemon tool). Rewrite `internal/config` fresh. Accept "structured copy-and-prune" as the valid form of `internal/types` rewrite (fast, preserves ADIF domain knowledge).

11. **Second execution wave — the scope-corrected deletions and rewrites:**
    - Deleted `cmd/importer/`, `internal/lookup/`, `internal/forwarding/`, `internal/email/`, `internal/maidenhead/`, `internal/apikey/`.
    - Pruned `internal/types/` grab-bag: deleted `audio.go`, `cat.go`, `ft8.go`, `listener.go`, `ptt.go`, `rig.go`, `serial.go`, `server.go`, `user.go`, `apikey.go`, `app_config.go`, `email.go`, `forwarding.go`, `lookup.go`, `optional.go`, `required.go` (restored minimal version with one field), `json_test.go` (1019 lines of v1-shape tests), `types_test.go` (214 lines of the same). Pruned `services.go` to drop DI bean names for deleted services.
    - Deleted `internal/config/` entirely. Wrote fresh `internal/config/config.go` + `doc.go` with a minimal `Config` struct, a `Service` wrapper providing the four getters the kept v1 code actually calls (`LoggingConfig()`, `DatastoreConfig()`, `RequiredConfigs()`, `WorkingDir()`), idempotent lifecycle methods, and a `New()` constructor.
    - Deleted `internal/database/sqlite/example/` (dead code that imported the deleted server-side `internal/database` package).
    - Fixed `internal/enums/upload/services.go` to inline the QRZ constant as `"qrzforwardingservice"` instead of importing from types.

12. **Chased down the remaining compile/vet errors:**
    - Two test files had references to deleted types: `internal/enums/upload/services_test.go` and `internal/logging/logging_test.go`. Both fixed with surgical edits (inline literal + update test helper to use the new `config.Service{Cfg: config.Config{...}}` shape).
    - Restored minimal `internal/types/required.go` with just the `QsoForwardingRowLimit` field that the carry-forward sqlite service actually reads at Open time.

13. **Verified the resulting tree.** Clean runs:
    - `go mod tidy` — populates indirect deps, no ambiguity.
    - `go build ./...` — clean.
    - `go vet ./...` — clean.
    - `go test ./...` — all packages passing. adif, adapter round-trip, meta, bands, cmds, events, modes, tags, upload/*, errors, iocdi, logging (14.8s for concurrency tests), utils. Stub packages (api, config, cmd/smd, qsoservice, types, database/sqlite, database/sqlite/models) have no test files which is expected.

14. **Did not commit.** Waiting on user review of the roughly 720-file diff.

### What did NOT get done this session

- **Did not commit the restructure.** The working tree has the full v2 milestone-1 layout applied and verified, but no git commit exists. User is reviewing and cleaning up some stray artifacts before the commit lands.
- **Did not start writing daemon code.** `cmd/smd/main.go` is a stub with a `TODO(v2 milestone 1)` comment and an empty `main()`. Real daemon construction begins next session.
- **Did not write `docs/v2-design/api.md`.** This is the next-steps item — enumerate daemon API consumers before designing any endpoints. Still pending.
- **Did not write `docs/v2-design/milestones.md`.** Concrete definition of milestone 1 "done" still pending.

## What happened in the 2026-04-15 session (session 2)

### Goals set for the session

- Push the v1.0.0 milestone upstream.
- Start thinking about v2 structure — not implementation, just shape and module boundaries.
- Capture whatever gets decided so future sessions don't re-derive it.

### What got done

1. **Pushed v1 artifacts upstream.** User ran the push commands for `main`,
   the `v1` branch (with `-u` to set tracking), and both tags
   (`v1.0.0`, `pre-ft8-removal`). All session-1 work is now on origin.

2. **Explored v2 repo structure collaboratively.** Discussed monorepo shape,
   whether to start with single `go.mod` vs `go.work`, which binaries earn
   their own modules, whether `internal/*` should be split into sub-modules,
   and how source-sharing interacts with release coordination.

3. **Settled six structural decisions with explicit rationale:**
   - Monorepo on main; v1 preserved on the `v1` branch.
   - Single `go.mod` at milestone 1; `go.work` returns at milestone 2 only
     when Wails clients come back.
   - Only Wails apps get their own modules; pure Go binaries (smd, importer,
     future bridges) stay in the root module.
   - Shared `internal/` — all binaries import from one shared library tree.
     Four-point rationale: types.Qso coherence is load-bearing, solo-dev
     release coordination is trivial, shared bugfixes are a feature,
     emergency one-binary builds still work.
   - Source sharing and wire compatibility are separate axes — the HTTP API
     is the real compatibility boundary, version the wire not the source.
   - `internal/*` packages split into sub-modules only when (a) published
     externally or (b) they have exotic deps — both rare; default is flat.

4. **New package names chosen for v2:** `cmd/smd` (unix daemon convention),
   `internal/qsoservice` (domain service layer), `internal/api` (HTTP
   handlers), `internal/smclient` (Go HTTP client for daemon consumers).

5. **Milestone 1 target tree specified.** Deliberately absent list for
   milestone 1 enumerated: no `apps/*` (Wails comes back milestone 2), no
   rig control packages (narrow daemon scope), no server-side database
   cluster (relocates to future server repo), no dead listeners, no
   milestone-1 multi-destination forwarder work.

6. **Migration plan from current main to milestone-1 layout drafted.**
   Single restructure commit is the recommended shape (cleaner history than
   phased), with the `v1` branch as the safety net. The explicit delete-list
   and create-list are captured in `docs/v2-design/structure.md` →
   "Migration from main's current state to milestone 1."

7. **Created `docs/v2-design/` directory and wrote `structure.md`** — the
   first durable v2 design document. About 250 lines. Captures all six
   decisions above with full rationale, both milestone target trees, the
   deliberately-absent list for milestone 1, the migration plan, and
   pointers to future sibling documents (`api.md`, `milestones.md`, etc.)
   for design questions explicitly deferred.

8. **Added cross-reference in `docs/v1-analysis/design-decisions-log.md`**
   pointing at `docs/v2-design/` so readers know where v2-scope decisions
   live. Keeps v1-analysis cleanly scoped to v1.

9. **Updated this handoff document** (currently being read) to reflect
   session 2 progress.

10. **Reviewed and rewrote `CLAUDE.md`** from scratch. The original file was
    a generic code-style guide that didn't mention Station Manager at all
    and contained several bullets that actively contradicted project
    lessons (most notably "avoid duplicate code" conflicting with "build
    specific, not generic" from `lessons-for-v2.md`, and "ALWAYS include
    unit tests" conflicting with the integration-tests-over-mocks
    preference). The new 91-line file is Station Manager-specific: it
    covers repo structure (v1/v2 split), points at the durable doc
    inventory (`docs/v1-analysis/`, `docs/v2-design/`,
    `docs/session-handoff.md`), captures load-bearing invariants and
    applied lessons as headlines, and has Go-specific code style notes
    that are internally consistent and match the project's existing
    idioms. Dead bullets (composition-vs-inheritance in Go, FP-vs-OOP,
    redundant pairs) were dropped.

11. **Reviewed and deleted `AGENTS.md`.** It was a v1-era agent
    instructions file that duplicated ~half of CLAUDE.md and described a
    workspace/module layout that was already stale (FT8 task references,
    listener handler plugin patterns for now-deleted code, v1 multi-module
    structure that's about to be reshaped). Five genuinely useful
    conventions that weren't already in CLAUDE.md were extracted into a
    new **"Project idioms"** section of CLAUDE.md before deletion:
    `internal/types` stdlib-only rule, the service lifecycle pattern
    (`Initialize()` → `Open()`/`Start(ctx)` → `Close()`/`Stop()`,
    idempotent), the DI `ServiceName` constants convention, the
    sqlboiler-generated-models-are-read-only rule, and the
    `utils.WorkingDir()` path resolution convention. AGENTS.md's v1 content
    is preserved on the `v1` branch. CLAUDE.md final size: ~91 lines.

12. **Diagnosed GitHub Actions failure emails** the user had been getting
    on every push. Cause: `go test -race ./... -short` in `validate.yml`
    was catching a data race in v1 code. The race fix belongs on the `v1`
    branch (not main), because the racing code is about to be restructured
    or deleted during the v2 milestone-1 reshape. Fixing it on main would
    be pointless churn.

13. **Deleted both workflow files from main:**
    `.github/workflows/validate.yml` (stops the failure email noise
    immediately) and `.github/workflows/release.yml` (prevents it from
    running v1 build steps on a future v2 tag push). Both files are
    preserved on the `v1` branch. Tag-triggered workflows use the
    workflow file present at the tagged commit, so any future `v1.x.y`
    tag pushed from the `v1` branch will correctly use the preserved
    `release.yml` on that branch.

14. **Replaced `RELEASING.md` and `DEVELOPING.md` with short stubs** on
    main. The originals described v1's Taskfile + Wails + nfpm pipeline
    and v1's developer setup — entirely v1-specific content that doesn't
    apply to v2 milestone 1 (no Wails apps, no packaging, no release
    process yet). The new stubs are ~20 lines each: they explain that v2
    is under construction, point at the current durable documentation
    (`CLAUDE.md`, `docs/v1-analysis/`, `docs/v2-design/`,
    `docs/session-handoff.md`), and tell any reader looking for v1 details
    to check out the `v1` branch. v1 originals are preserved on the `v1`
    branch.

15. **Added a "v1 branch follow-ups" section to this handoff document**
    (see below). The data race is its first item, with a short
    how-to-chase checklist and notes on typical race-prone patterns in
    this codebase. This keeps v1-track work separated from v2-track work
    in the next-steps list going forward.

16. **`.config/ai/rules/general.md` was deleted by the user** earlier in
    the session (they mentioned removing the whole `.config` directory).
    This file was unrelated to the session's work but appears in the
    commit-candidate set.

### What did NOT get done this session

- **Did not execute the restructure commit.** The milestone-1 layout is
  specified in `structure.md` but main has not yet been reshaped. The big
  deletions (apps/, server-side DB cluster, listeners, etc.) and
  scaffolding (`cmd/smd`, `internal/api`, `internal/qsoservice`) still
  need to happen. That is the natural next action for session 3.
- **Did not start any v2 code.** No `cmd/smd/main.go`, no `internal/api`,
  no `internal/qsoservice`. Structure and housekeeping were the goals.
- **Did not touch `Taskfile.yml` or `Taskfile.wails.yml`.** These are
  v1-specific and the next domino in the same chain as the workflows and
  docs cleared out this session. Left alone because the natural time to
  delete them is during the restructure commit, not as an isolated move.
- **Did not fix the v1 data race.** Belongs on the v1 branch; captured in
  "v1 branch follow-ups" below.
- **Did not commit any of session 2's work.** At session end, the working
  tree has the full session 2 cleanup staged or ready-to-stage but
  uncommitted, pending the user's review.

## What happened in the 2026-04-14 session (session 1)

### Goals set for the session

- Complete the v1 analysis effort far enough to make the v2-vs-refactor call.
- Act on any code-level cleanup that was clearly needed regardless of path.

### What got done

1. **Reviewed the analysis state.** Started by reading `docs/v1-analysis/` and
   the relevant memory notes to ground the discussion. All five analysis docs
   were already drafted from prior work in this session; this session worked
   from them rather than producing them.

2. **Made the v2 rewrite decision.** After discussing the tradeoffs, the
   author chose the v2 rewrite path. Key reasoning recorded in
   `design-decisions-log.md` → "v2 rewrite vs. v1 incremental refactor."

3. **Decided on the tag-and-branch workflow.** Main reflects where the project
   is going (v2); the `v1` branch is the frozen-plus-maintenance container
   the author runs day-to-day. v2 work happens on main. Bug fixes for v1 land
   on the `v1` branch.

4. **Cleaned up mid-state in the working tree.** Three FT8 files were in a
   staged-as-new-but-deleted-from-disk limbo state at session start. Ran
   `git add` to reconcile the index with the working tree, leaving only the
   expected untracked `ft8_live_window.wav` file (which was about to be
   swept away with the rest of `internal/ft8/service/`).

5. **Tagged `pre-ft8-removal`** at commit `1ae516d` to preserve the full FT8
   experiment tree in git history before deletion.

6. **Removed FT8 code and legacy docs** in commit `0e158ec`:
   - Deleted `internal/ft8/`, `internal/ft8x/`, `cmd/ft8/`, `cmd/ft8test/`
   - Updated `go.work` (7 modules → 5)
   - Deleted top-level `docs/*.md` files: `ft8-*.md`, `whats-next.md`,
     `context-handoff.md`, `usb-serial-setup.md` (kept `docs/v1-analysis/`)
   - Removed the FT8/FT4 section from `README.md`
   - Replaced the `internal/ft8/synth` example in
     `internal/audio/README.md` with a generic caller-supplied samples
     example
   - Removed FT8 patterns from `.gitignore`
   - 132 files touched: 4 edits + 128 deletions; 33,138 lines deleted,
     4 inserted

7. **Verified the build.** Ran `go build` and `go vet` across all five
   workspace modules. Both passed clean.

8. **Tagged `v1.0.0`** at the cleanup commit.

9. **Created the `v1` branch** at the `v1.0.0` tag. This is now the author's
   day-to-day working branch.

10. **Updated documentation and memory** to reflect the v2 decision and the
    post-cleanup repo state:
    - Memory files `project_sm_restructure.md` and `project_sm_v2_analysis.md`
      updated (decision state, repo state, post-decision guidance).
    - `MEMORY.md` index entries updated.
    - `docs/v1-analysis/architecture-map.md` — module table fixed (5 modules),
      FT8 section marked removed, cleanup targets split into done/pending.
    - `docs/v1-analysis/bug-inventory.md` — FT8 entry and dead-docs entry
      marked FIXED.
    - `docs/v1-analysis/design-decisions-log.md` — new "Execution path" entry
      records the v2 rewrite decision.
    - `docs/v1-analysis/lessons-for-v2.md` — "Current read" paragraph updated
      from speculative to decided; FT8 items in the "Delete, don't carry
      forward" list marked done.
    - `docs/v1-analysis/invariants.md` — no changes needed (invariants are
      stable).

11. **Wrote this handoff document** so the next session starts with full
    context.

### What did NOT get done this session

- **Did not push anything to origin.** All commits, tags, and the `v1` branch
  are local. Push is a deliberate-action step — decide when you're ready and
  run the commands in the "Repo state" section above.
- **Did not start any v2 design work.** The decision was made; no code
  written for v2 yet.
- **Did not address the remaining code-level cleanup items** that surfaced
  during the analysis — see "Next steps" below.
- **Did not write `docs/v1-analysis/external-surfaces.md`.** This was proposed
  as part of the original analysis plan but deferred. It covers the Wails
  frontend's binding surface, ADIF formats used, online service APIs, and the
  serial/CAT subsystem's external touchpoints — "things I can't just change
  without breaking something observable." Useful before v2 design starts in
  earnest; not urgent.

---

## Next steps (priority order)

The author picks what to work on next — this is a suggestion list, not a
script. Items near the top are the natural continuation of session 3; items
lower down are bigger v2 milestones.

### The natural next action

1. **Commit the restructure that's currently in the working tree.** Session 3
   executed the full v2 milestone-1 reshape and verified it (`go build ./...`,
   `go vet ./...`, `go test ./...` all clean), but did not commit — the user
   is reviewing and doing some final artifact cleanup. The commit is a
   ~720-file diff, most of which is deletions (v1 workspace scaffolding,
   apps, Taskfiles, FT8 already gone from session 1, server-side DB
   cluster, rig control packages, listener framework, audio, enrichment and
   forwarding packages, v1 types grab-bag, v1 config). New files: `go.mod`
   (single root), `cmd/smd/main.go` + `doc.go` stub, `internal/api/doc.go`
   stub, `internal/qsoservice/doc.go` stub, `internal/config/config.go` + 
   `doc.go` (fresh minimal daemon config). Modified files: a handful of
   test helpers and one enum constant that used deleted types.

   Proposed commit message shape:
   ```
   Restructure main into v2 milestone-1 layout

   Collapse the v1 multi-module workspace into a single root Go module
   and clear v1 code that doesn't belong in v2 milestone 1. v2 is now a
   clean-slate daemon-plus-stubs shell on main; v1 continues on the v1
   branch for day-to-day operational use.

   ... (full breakdown in structure.md and in session 3's notes above)
   ```

   Once committed, main is at the v2 milestone-1 layout and v2 construction
   work can begin.

### v2 design work (follow-ups to structure.md)

2. **Write `docs/v2-design/api.md`** before designing any daemon endpoints.
   Per `lessons-for-v2.md` → "Enumerate all API surfaces before designing
   any of them." The three Wails apps have different needs:
   - `apps/logging` — real-time, high-frequency, needs QSO draft init, log,
     update, dupe check, session state, CAT status, forwarding events.
   - `apps/logbook` — low-frequency, bulk operations, needs logbook CRUD,
     batch QSO edit, list with paging, export to ADIF.
   - `apps/config` — rare-use, needs config read/write and validation.
   Earlier daemon API sketches were logging-centric and missed the logbook
   and config surfaces. Make a table of all three consumers' required
   operations *before* deciding endpoint URIs. This enumeration is the
   main content of `api.md`.

3. **Write `docs/v2-design/milestones.md`** to concretely define what
   milestone 1 "done" means. The risk `structure.md` explicitly flagged is
   the "interminable 90%" — mitigated only by narrow initial scope.
   Proposed milestone 1: daemon + `curl`-based HTTP API exercise + one
   carry-forward client (`cmd/importer` adapted to go through the daemon
   instead of writing sqlite directly). Then milestone 2 adds the first
   Wails client. Then multi-destination forwarding, then bridges, then
   multi-rig. Writing it down forces commitment.

4. **Pick the ORM/generator approach** — this becomes `docs/v2-design/db-layer.md`
   when v2 DB work starts. sqlboiler (v1's choice), Bob (successor), sqlc
   (query-first), or hand-rolled. Not urgent; the current carry-forward
   plan is "sqlboiler stays until there's a reason to change." Make it an
   explicit decision when you're actually touching DB code for v2.

5. **Think about multi-rig as a first-class assumption** before the serial
   bridge design starts — this becomes `docs/v2-design/multi-rig.md` when
   it's a real concern. v1 has no multi-rig support. The bridge (milestone
   3+) is the place to bake it in. Data-model questions to answer: does
   `types.Qso` carry a rig identifier? Does the logbook schema need a rig
   table? These are cheaper to answer before the daemon API is frozen than
   after.

6. **Expand `CLAUDE.md` frontend guidance when Wails clients return in
   milestone 2.** The current file has deliberately thin TypeScript/Svelte
   5 coverage (runes preference, Vitest/Playwright, snippets-for-tightly-
   coupled-UI) because there's no frontend code in the v2 tree yet. When
   the first Wails app lands, expand with concrete guidance on: component
   organization, Svelte 5 state/store patterns, TS strictness level, Wails
   bindings conventions between the Go backend and Svelte frontend, form
   handling patterns, the API client layer that wraps `internal/smclient`
   calls for the Svelte side, error surfacing, and loading/optimistic-UI
   patterns. Flagged 2026-04-15 — don't forget.

### Carry-forward package code-review track (runs in parallel with v2 design)

The carry-forward packages were kept because rewriting them would be too expensive, but they are **not** blessed as final — each one is a code-review candidate during v2 construction. The user explicitly framed this as "carry forward to code-review," not "carry forward as gospel." Each review may land as its own commit, separate from v2 feature work.

7. **Audit `internal/database/sqlite`.** User flagged this as "probably has a smarter implementation." The session-1 adapter simplification was one pass; the rest of the package (the `Service` struct, the `api.go`/`api_context.go` split, the migrations infrastructure, the error wrapping, the `requiredCfgs` field tied to the v1 forwarder row-limit) hasn't been reviewed at that depth. A dedicated pass is likely to find more wins. **Probably the highest-value review target.**

8. **Audit `internal/iocdi`.** The home-grown DI container. Generally considered a "keep," but worth a read to confirm it still makes sense for the v2 daemon's smaller service graph. If the daemon ends up with only 4–5 services, manual wiring might be cleaner than reflection-based DI.

9. **Audit `internal/adif`.** ~2000 lines of ADIF parser. Load-bearing for milestone 1. Review for any v1-era shortcuts that could be cleaned up, and for test coverage gaps around the "ADIF is down" error paths (see `lessons-for-v2.md` → "Explicit fallbacks for every external dependency").

10. **Audit `internal/types` (pruned version).** 13 files left after the session 3 prune. Some (like `adif.go`, `datastore.go`, `logging.go`) may still be pruneable. Worth a read-through once the daemon is far enough along that the actual minimal surface is clearer.

11. **Audit `internal/errors`, `internal/logging`, `internal/enums`, `internal/utils`.** Less urgent than the above — these are "maturing" per the user's framing — but each one should get at least a quick pass when its first v2 consumer lands.

### v1 branch follow-ups (distinct track from v2 main work)

v1 is a live maintenance branch. Bug fixes and improvements for v1 land
there directly, not on main. These items are tracked separately because
main is about to diverge significantly from v1 and the two tracks should
not bleed into each other.

- **Track down and fix a data race in v1.** The failing
  `go test -race ./... -short` in the v1 `validate.yml` workflow is caught
  by the race detector. The race is in v1 code that will be restructured
  or replaced during the v2 milestone-1 reshape, so fixing it on main is
  pointless churn — but v1 needs to be correct for day-to-day operational
  use. To chase it:
  - `git checkout v1`
  - `go test -race ./... -short` in each module to find which test triggers it
  - Typical culprits in this codebase: concurrent cache/map access in
    lookup/enrichment paths, goroutine leaks in forwarding workers,
    unsynchronized state in the CAT listener event emitter
  - Fix on the v1 branch only; tag a v1.0.1 if the fix is worth shipping
- **Known-but-deferred items from the bug inventory that still apply to v1:**
  - Hardcoded QRZ forwarder in `LogQso` / `UpdateQso` (see
    `docs/v1-analysis/bug-inventory.md` → "Hardcoded QRZ forwarder"). Not
    a crash, but blocks multi-destination forwarding. Unlikely to be fixed
    on v1 since the redesign is a v2 concern.
  - `DatabaseServiceInterface` vs `*sqlite.Service` mismatch — cosmetic,
    unused scaffolding. Could be deleted from v1 if it's bothering
    anything, but probably not worth the churn.

### Maintenance of this handoff document

12. **Update this file at the end of every session.** Move completed items
    from "Next steps" into "What happened," add new items as they surface,
    prune "What happened" to keep it to the last 2–3 sessions of history.
    The git history and the v1-analysis / v2-design docs are the long-form
    record; this file is the quick-reference for cross-session continuity.

13. **Prune session-1's "What happened" entry next session.** The handoff
    is now carrying three sessions of history. Per the maintenance rule
    above, only 2–3 sessions stay in full detail. After session 4 lands,
    consider compressing session 1 to a one-paragraph summary referencing
    the relevant commit hashes (`5288983`, `1ae516d`, `0e158ec`,
    `66e0af3`) and the session-2 handoff entry that already contains its
    work, so this file doesn't grow unbounded.
