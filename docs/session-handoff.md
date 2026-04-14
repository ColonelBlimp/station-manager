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

## Current state (as of 2026-04-14 end-of-session)

### The v2 rewrite decision is made

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

**Not pushed to origin yet:** the new commit `0e158ec`, the `v1` branch, and
the `v1.0.0` and `pre-ft8-removal` tags are all local. Push when ready:

```
git push origin main
git push origin v1
git push origin v1.0.0 pre-ft8-removal
```

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

**`docs/v1-analysis/`** — the durable v1 analysis record (carry forward into
v2 design work):
- `architecture-map.md` — what v1 actually contains, module by module
- `bug-inventory.md` — known issues, fixed and outstanding
- `design-decisions-log.md` — keep/change/delete verdicts on every major
  shape decision
- `invariants.md` — load-bearing rules that must carry forward
- `lessons-for-v2.md` — synthesis document: patterns to apply, patterns to
  avoid, what v1 got right, provisional v2 scope

All five were updated on 2026-04-14 after the v2 decision to reflect the
current state. The **synthesis document** (`lessons-for-v2.md`) is the single
most important read before any v2 design choice.

**`docs/session-handoff.md`** — this file. Rolling cross-session state.

**Memory files** (`~/.claude/projects/.../memory/`) — durable facts and
invariants used across all sessions. Key entries: `project_sm_restructure`,
`project_sm_v2_analysis`, `project_sm_design_invariants`, `project_sm_overview`,
`user_profile`, `feedback_design_patterns`, `project_sm_serial_bridge`,
`project_ft8_library`. See `MEMORY.md` index for the full list.

---

## What happened in the 2026-04-14 session

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
script. Items near the top are smaller and can land anytime; items lower down
are larger and are the core of v2 construction.

### Immediate / mechanical

1. **Push the v1.0.0 milestone to origin** when ready. Exact commands in the
   "Repo state" section above. After pushing, the v1 freeze point and the v1
   maintenance branch are visible upstream.

2. **Decide when to start running v1 from the `v1` branch** for day-to-day
   use. The branch exists; switching to it is `git checkout v1` in a working
   copy you actually use. Once you do, any bug you hit while using SM becomes
   a candidate commit on that branch.

### Remaining v1 code-level cleanup (can land on main before v2 starts, or
later on the v1 branch if they block a real workflow)

These surfaced during the analysis but haven't been acted on yet. None are
urgent. Some are mechanical and would make v1 a cleaner reference point for
v2.

3. **Delete `internal/listeners/handlers/wsjtx/`.** The WSJT-X UDP listener
   never ran in a working configuration — WSJT-X/JTDX require exclusive
   serial access, which conflicts with Station Manager's own serial library.
   It's dead code and a false start. See `bug-inventory.md` → "WSJT-X UDP
   listener is dead code."

4. **Verify `internal/listeners/` framework has no other consumers** after
   wsjtx is removed. If wsjtx was the only handler, the framework itself is
   also dead and can be deleted in the same commit.

5. **Reverse-dependency check on `internal/audio/`** now that FT8 is gone.
   If its only remaining consumers were FT8, it's dead weight. If it has
   other users (voice keyer? SSB playback? general WAV handling?), it stays.
   A quick `grep -r "internal/audio"` across the 5 modules answers this.

6. **Resolve the `DatabaseServiceInterface` vs `*sqlite.Service` mismatch**
   in `apps/logging/backend/facade/`. Interface signatures don't match the
   concrete type; mocks built on the interface are never actually
   instantiated; all tests use real `&sqlite.Service{}` with in-memory DBs.
   Simplest fix: delete the interface and the mocks. See
   `bug-inventory.md` → "DatabaseServiceInterface vs `*sqlite.Service`."

### v2 design work (this is where the real work begins)

7. **Write `docs/v1-analysis/external-surfaces.md`** before designing v2
   APIs. Cover: what the Wails frontends actually bind to (check
   `apps/*/frontend/src/lib/wailsjs/go/models.ts`), ADIF formats supported,
   online service API shapes, the serial/CAT subsystem's touchpoints. This
   is the "things I cannot break silently" list. Skipping this means some of
   those things get broken silently during v2 construction.

8. **Enumerate daemon API consumers before designing any endpoints.** Per
   `lessons-for-v2.md` → "Enumerate all API surfaces before designing any of
   them." The three Wails apps have different needs:
   - `apps/logging` — real-time, high-frequency, needs QSO draft init, log,
     update, dupe check, session state, CAT status, forwarding events.
   - `apps/logbook` — low-frequency, bulk operations, needs logbook CRUD,
     batch QSO edit, list with paging, export to ADIF.
   - `apps/config` — rare-use, needs config read/write and validation.
   Earlier daemon API sketches were logging-centric and missed the logbook
   and config surfaces. Make a table of all three consumers' required
   operations before designing any endpoint URIs.

9. **Define the v2 minimum viable milestone.** The "interminable 90%"
   failure mode is the main risk. Narrow the first milestone to: daemon +
   `apps/logging` thin client + QRZ forwarder working end-to-end. Defer
   `apps/logbook`, `apps/config`, the serial/CAT bridge, the `wsjtx-bridge`
   client, and multi-destination forwarder fan-out to later milestones. Get
   the day-one-usable slice working before adding anything.

10. **Pick the ORM/generator approach for v2's DB layer.** sqlboiler (what
    v1 uses), Bob (sqlboiler successor), sqlc (query-first generation), or
    hand-rolled. The transformation layer between DB rows and `types.Qso`
    exists regardless of choice — the generator affects ergonomics, not
    whether the adapter exists. Not urgent; settle it when v2 DB design
    actually starts.

11. **Think about multi-rig as a first-class assumption** before the serial
    bridge design starts. v1 has no support for multiple rigs; v2's bridge
    is the place to bake it in. Do the data-model thinking early — does
    `types.Qso` carry a rig identifier? Does the logbook schema need a rig
    table? — because retrofitting multi-rig after the daemon API is frozen
    is expensive.

### Maintenance of this handoff document

12. **Update this file at the end of every session.** Move completed items
    from "Next steps" into "What happened," add new items as they surface,
    prune "What happened" to keep it to the last 2–3 sessions of history.
    The git history and the v1-analysis docs are the long-form record; this
    file is the quick-reference for cross-session continuity.
