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

## Current state (as of 2026-04-17 mid-session 8)

### Milestone 1 complete, milestone 1b in progress

Milestone 1 (submit a QSO) is complete and CI-green. Milestone 1b
(QSO CRUD and logbook management) is in progress — logbook CRUD is
done (step 1 of 6 in the workflow-driven implementation order).

### Milestone 1b progress (reprioritised session 8)

| Step | Scope | Status |
|------|-------|--------|
| 1. Logbook CRUD | `GET/POST/PATCH/DELETE /v1/logbook` | **done** |
| 2. QSO fetch/edit/delete | `GET/PATCH/DELETE /v1/qso/:id` | not started |
| 3. QSO list with pagination | `GET /v1/logbook/:id/qso` | not started |
| 4. Contest dupe check | `GET /v1/contest-dupe` | not started |
| 5. Contact history | `GET /v1/contact-history` | not started |
| 6. Version | `GET /v1/version` | not started |

### QSO submit path tightened (session 8)

The submit endpoint now requires `?logbook=<id>` and validates:
- The logbook exists (404 if not)
- The logbook's callsign matches STATION_CALLSIGN (400
  `callsign_mismatch` if not)
- Auto-create logic removed — logbooks must be created explicitly
  via `POST /v1/logbook` before QSOs can be submitted

### Code style decisions (session 8)

- **`errors.Op` convention standardised:** all ops use
  `const op errors.Op = "package.FuncName"` pattern. Handler ops
  changed from URL-path-style (`api.v1.qso.submit`) to function-name
  style (`api.handleSubmitQso`).
- **`writeError` uses `errors.Op`** not plain string for the op
  parameter and the `ErrorResponse.Op` field.
- **No `fmt.Errorf` in packages that use `internal/errors`** — the
  `errors.New(op).WithErr(err).WithMsg(...)` pattern is the standard
  for all error paths.

### Listener protocol is config-driven

`ServerConfig.Protocol` (default `"unix"`, alternative `"tcp"`)
controls `net.Listen`. The stale-socket cleanup only runs for Unix
protocol. This keeps the door open for network deployment (daemon on
a Pi) without any code changes — just a config change.

### Dev environment

- `build/config.json` — dev config with debug logging
- `build/db/` — sqlite database
- `build/log/` — daemon log files
- `task run` — builds and runs the daemon using `SM_WORKING_DIR`
  from `.env`
- `task build` — compiles all packages + daemon binary
- `.github/workflows/ci.yml` — CI passes cleanly on GitHub

### Repo state

**Branches:**
- `main` — milestone 1b in progress. CI green.
- `v1` at `0e158ec` — unchanged since session 2.

---

## What happened in the 2026-04-17 session (session 8, in progress)

### Goals set for the session

- Implement milestone 1b following the workflow-driven order
- Code quality review and tightening

### What got done

1. **Logbook CRUD endpoints** (commit `6680386`):
   - `GET /v1/logbook` — list all logbooks
   - `GET /v1/logbook/{id}` — fetch by ID, 404 if not found
   - `POST /v1/logbook` — create with name + callsign validation,
     409 on duplicate name
   - `PATCH /v1/logbook/{id}` — partial update with pointer fields,
     404 if not found
   - `DELETE /v1/logbook/{id}` — soft-delete, 404 if not found,
     409 if logbook has QSOs (FK constraint)
   - Added `UpdateLogbookWithContext` to the sqlite layer
   - 11 logbook CRUD tests

2. **QSO submit path tightened** (same commit):
   - `?logbook=<id>` query parameter now required
   - Logbook existence + callsign match validated before submit
   - Auto-create logic and `resolveLogbook` method removed
   - 3 new tests: missing logbook param, logbook not found,
     callsign mismatch
   - All existing submit tests updated to create logbooks first

3. **Code style fixes** (in progress):
   - `server.go` — `fmt.Errorf` → `errors.New(op).WithErr(err)`
   - All handler ops standardised to `errors.Op` type with
     `api.FuncName` pattern
   - `parsePathID` — proper `const op errors.Op = "api.parsePathID"`
   - `writeError` and `ErrorResponse.Op` — `errors.Op` type
   - `ServerConfig.Protocol` field added (default `"unix"`)
   - Listener protocol now config-driven in `ListenAndServe`

### Design decisions made

- **Logbooks are created explicitly** — no auto-creation during QSO
  submit. The client creates logbooks via `POST /v1/logbook` first.
- **QSO submit validates logbook ID + callsign match** — the daemon
  fetches the logbook by ID and verifies its callsign matches
  STATION_CALLSIGN. Both must match; either failing returns a clear
  error.
- **Workflow-driven implementation order** — endpoints are implemented
  in the order an operator uses them, not alphabetically or by
  resource type. Logbook CRUD first (can't log without a logbook),
  then contact history and contest dupe (needed while building a QSO),
  then QSO fetch/edit/delete (post-logging corrections), then
  pagination (browsing), then version (diagnostic).
- **`errors.Op` convention** — all ops follow `package.FuncName`,
  typed as `errors.Op`, not plain strings.
- **Listener protocol configurable** — `ServerConfig.Protocol`
  defaults to `"unix"` but supports `"tcp"` for network deployment.

---

## What happened in session 7 (2026-04-16)

### Goals

- Complete carry-forward package review track
- Write milestones doc
- Write milestone 1 daemon code

### What got done

Reviewed all 8 carry-forward packages (types, utils, enums, config,
iocdi, adif, database/sqlite). Wrote `docs/v2-design/milestones.md`.
Implemented milestone 1: daemon entry point, config loader, qsoservice
with validation/dedupe/atomic write, API handlers, error envelope,
healthz. Set up dev environment (build/config.json, task run) and
GitHub Actions CI. Found and fixed 5 bugs during testing (force dedupe,
malformed ADIF status, MaxBytesReader leak, empty dedupe fields,
time coherence). 25 tests across 4 packages. Schema updated: removed
session table/apikey, added dedupe_key column, removed contradictory
CHECK constraints.

---

## Sessions 1–6 (compressed summaries)

### Session 1 (2026-04-14)

v2 rewrite decision. Completed v1 analysis (5 docs). Tagged
`pre-ft8-removal` and `v1.0.0`. Created `v1` branch. Three v1 bug
fixes before tagging.

### Session 2 (2026-04-15)

Wrote `docs/v2-design/structure.md` (6 structural decisions). Rewrote
`CLAUDE.md`. Deleted v1 CI workflows. Commit `5ef55c1`.

### Session 3 (2026-04-16)

Big restructure: reshaped main into v2 milestone-1 layout. 730 files
changed, 68,934 deletions. Scaffolded `cmd/smd`, `internal/api`,
`internal/qsoservice`, `internal/config`. Commit `0010b6e`.

### Session 4 (2026-04-16)

Short session. Added `Taskfile.yml`. Deleted remaining v1 leftovers.
Commit `1ee2ced`.

### Session 5 (2026-04-17)

Wrote `docs/v2-design/api.md`. Strengthened invariants. Full
`internal/errors` review (11 findings). Wrote `internal/logging`
review doc (14 findings). Two feedback memories.

### Session 6 (2026-04-18)

Applied all 14 `internal/logging` findings. Fixed embedding bug.
Both `internal/errors` and `internal/logging` reached v2 final state.

---

## Next steps (priority order)

### Continue milestone 1b — reprioritised workflow

Priority reordered (session 8) to focus on what the operator
actually needs to log and manage QSOs. Enrichment and draft-building
features are nice-to-have, not essential — especially during a
pile-up.

2. **QSO fetch/edit/delete** — operator needs to correct mistakes
   immediately after logging.
   - `GET /v1/qso/{id}`
   - `PATCH /v1/qso/{id}`
   - `DELETE /v1/qso/{id}`
   - sqlite layer has `FetchQsoByIdWithContext`, `UpdateQsoWithContext`
   - Need soft-delete method (schema has `deleted_at`)

3. **QSO list with pagination** — operator needs to see what they've
   logged.
   - `GET /v1/logbook/{id}/qso` (forward-cursor pagination)
   - Existing offset-based `FetchQsoSlicePaging` needs rewriting to
     cursor-based per api.md Section 4.4

4. **Contest dupe check** — essential for contesting.
   - `GET /v1/contest-dupe?logbook=<id>&call=<callsign>&band=<band>&mode=<mode>`
   - sqlite layer already has `IsContestDuplicateByLogbookIDWithContext`

5. **Contact history** — nice to have, not essential.
   - `GET /v1/contact-history?call=<callsign>`
   - sqlite layer already has `FetchQsoSliceByCallsignWithContext`

6. **Version** — diagnostic, lowest priority.
   - `GET /v1/version`

### v2 design work

- **Pick the ORM/generator approach** → `docs/v2-design/db-layer.md`.
  sqlboiler stays until there's a reason to change.
- **Multi-rig as first-class assumption** →
  `docs/v2-design/multi-rig.md` when bridge design starts.

### Deferred features

- **Logging-app text-file fallback reconciliation** — milestone 2+.
- **Enrichment / contacted_station population** — milestone 2.
  Client-side concern; daemon submit path stays fast and network-free.
- **Daemon dashboard / monitoring UI** — post-milestone 2.

### v1 branch follow-ups

- Data race candidate fix (session 6) not yet verified on v1 branch.
- Hardcoded QRZ forwarder — v2 concern, unlikely to be fixed on v1.

### Maintenance

- Compress session 7 after session 9 lands.
- Update this file at end of every session.
