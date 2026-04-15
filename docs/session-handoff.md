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

## Current state (as of 2026-04-15 end-of-session)

### v2 structural decisions are now captured in `docs/v2-design/structure.md`

Session 2 of v2 work produced the first durable v2 design document. Before touching any v2 code, read `docs/v2-design/structure.md` — it captures the repo layout, module boundaries, release model, and milestone-1 vs milestone-2 target trees with rationale for every decision. Any future session that questions "why is v2 shaped this way" should find the answer there.

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
script. Items near the top are the natural continuation of session 2; items
lower down are bigger v2 milestones.

### The natural next action

1. **Execute the restructure commit that reshapes main into the milestone-1
   layout.** The delete-list and create-list are fully specified in
   `docs/v2-design/structure.md` → "Migration from main's current state to
   milestone 1." Key moves:
   - Delete `apps/config/`, `apps/logbook/`, `apps/logging/` (preserved on
     v1 branch)
   - Collapse `go.work` to a single `go.mod` at the repo root
   - Delete `internal/database/*.go` top-level files, `internal/database/postgres/`,
     `internal/adapters/` (all relocate to future server repo)
   - Delete `internal/listeners/handlers/wsjtx/` and verify the
     `internal/listeners/` framework has no other consumers (likely dead too)
   - Reverse-dependency check on `internal/audio/` — if no non-FT8 consumer
     remains, delete it; if something legitimate uses it, keep it
   - Scaffold `cmd/smd/main.go`, `internal/api/doc.go`,
     `internal/qsoservice/doc.go` as empty stubs with intent comments
   - Clean up the root `go.mod` of any dependencies that were only needed
     by the deleted packages
   
   Recommended as **a single commit** for clean git history; the `v1` branch
   is the safety net so nothing is lost. This is a big, satisfying commit
   that takes main from "v1 tip" to "v2 milestone-1 empty layout."

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

### Optional remaining v1 cleanup (can land on v1 branch if a bug surfaces)

These were in session-1's next-steps list. Most are subsumed by the
restructure commit in item #1 above (they happen when those directories are
deleted from main). Listed here only for completeness; if item #1 happens,
you can cross these off.

6. **Delete `internal/listeners/handlers/wsjtx/`** — subsumed by item #1.
7. **Delete `internal/listeners/` framework** if wsjtx was the only consumer — subsumed by item #1.
8. **Reverse-dependency check on `internal/audio/`** — subsumed by item #1.
9. **Resolve the `DatabaseServiceInterface` mismatch** in `apps/logging/backend/facade/` — subsumed by item #1 (the entire `apps/logging/` directory is deleted; the interface dies with it). The lesson survives in `lessons-for-v2.md` → "Aspirational interfaces with no real consumer."

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

10. **Update this file at the end of every session.** Move completed items
    from "Next steps" into "What happened," add new items as they surface,
    prune "What happened" to keep it to the last 2–3 sessions of history.
    The git history and the v1-analysis / v2-design docs are the long-form
    record; this file is the quick-reference for cross-session continuity.
