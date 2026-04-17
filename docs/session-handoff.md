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

## Current state (as of 2026-04-16 end-of-session 7)

### Milestone 1 is complete

The v2 daemon serves its first QSO. `POST /v1/qso` accepts raw ADIF
over a Unix domain socket, validates all required fields and values,
computes a dedupe key, atomically stores the QSO in sqlite, and
returns a JSON envelope (`stored` or `duplicate`). All error paths
return structured JSON error envelopes. `GET /v1/healthz` pings
sqlite. Graceful shutdown on SIGINT/SIGTERM. GitHub Actions CI runs
vet + build + test with race detector on every push to main.

### All carry-forward packages reviewed and in v2 state

All eight carry-forward library packages have been through thorough
code review in sessions 5-7:

| Package | Status | Session |
|---------|--------|---------|
| `internal/errors` | v2 final | 5 |
| `internal/logging` | v2 final | 6 |
| `internal/types` | reviewed, fixes applied | 7 |
| `internal/utils` | reviewed, dead code removed | 7 |
| `internal/enums` | reviewed, CAT packages deleted | 7 |
| `internal/config` | reviewed, then rewritten for daemon | 7 |
| `internal/iocdi` | reviewed, cosmetic fixes | 7 |
| `internal/adif` | reviewed, fixes + tests added | 7 |
| `internal/database/sqlite` | reviewed, session/apikey removed, dedupe added | 7 |

### New packages written in session 7

| Package | Purpose | Lines |
|---------|---------|-------|
| `internal/qsoservice` | Domain service: validation, dedupe, atomic QSO write | ~220 |
| `internal/api` | HTTP handlers, error envelope, server lifecycle | ~200 |
| `internal/config` | Rewritten: file loader, defaults, server config | ~190 |
| `cmd/smd` | Daemon entry point: iocdi wiring, signal handling | ~165 |

### Schema changes in session 7

The `0001_init.up.sql` migration was updated (no v2 data exists yet):
- Removed `session` table and `session_id` FK from `qso`
- Removed `api_key` from `logbook`
- Added `dedupe_key TEXT NOT NULL` column to `qso` with unique index
  `(logbook_id, dedupe_key) WHERE deleted_at IS NULL`
- Removed `qso_data_no_duplicates` CHECK constraints from `qso` and
  `contacted_station` (they contradicted the `json.Marshal(qso)` blob
  strategy decided in `design-decisions-log.md`)
- sqlboiler models regenerated to match

### Milestones doc written

`docs/v2-design/milestones.md` defines concrete acceptance tests for
each milestone. Milestone 1 acceptance test passes end-to-end.

### Dev environment

- `build/config.json` — dev config with debug logging
- `build/db/` — sqlite database
- `build/log/` — daemon log files
- `task run` — builds and runs the daemon using `SM_WORKING_DIR` from `.env`
- `task build` — compiles all packages + daemon binary into `build/bin/smd`
- `.github/workflows/ci.yml` — CI passes cleanly on GitHub

### Repo state

**Branches:**
- `main` — milestone 1 complete. CI green.
- `v1` at `0e158ec` — unchanged since session 2.

### Config design decision (session 7)

The daemon owns its own config file, loaded from `--config` flag,
`$SM_WORKING_DIR/config.json`, `./config.json`, or built-in defaults.
On single-machine deployments the config app *can* share the same
file, but this is a convenience, not an architectural assumption. The
design keeps the door open for network deployment (e.g., daemon on a
Raspberry Pi) where the config app cannot access the daemon's
filesystem. Runtime config API is deferred.

### Enrichment / contacted_station decision (session 7)

The `contacted_station` table exists in the schema but is not
populated by the submit path. Enrichment (online callbook lookups,
caching in `contacted_station`) is a client-side concern that belongs
in the logging app (milestone 2). The daemon's submit path is
deliberately fast: parse → validate → dedupe → atomic write → done.
No network calls, no cache lookups, no enrichment blocking the
operator.

---

## What happened in the 2026-04-16 session (session 7)

### Goals set for the session

- Complete the carry-forward package code-review track
- Write `docs/v2-design/milestones.md`
- Write the first real daemon code (milestone 1)

### What got done

**Package reviews (first half of session):**

1. **Reviewed and fixed `internal/types`** — stripped postgres dead
   code, removed `adapter:"ignore"` tags, added `doc.go`.

2. **Reviewed and cleaned `internal/utils`** — deleted 5 dead items
   (10 files, ~1300 lines), hoisted regexps, added `doc.go`.

3. **Reviewed and pruned `internal/enums`** — deleted 3 CAT
   subpackages, fixed `action.Parse` error handling, added doc comments.

4. **Reviewed `internal/config`** — clean stub. Deleted dead v1
   integration test in `adif/slice_test.go`.

5. **Reviewed and cleaned `internal/iocdi`** — deleted commented-out
   code, fixed typos, added `doc.go`.

6. **Reviewed and fixed `internal/adif`** — renamed `Marshal` →
   `Parse`, fixed QSL mapping, removed dead checks, extracted
   constants, added `doc.go`, added 11 parser tests.

7. **Reviewed `internal/database/sqlite`** — the biggest review:
   - Removed `session` table and all Session methods
   - Removed `Logbook.APIKey` and `Logbook.UserID`
   - Removed `FetchQsoSliceNotForwarded` (queried JSON blob)
   - Removed `CheckDefaultLogbookExists` (hardcoded "Default")
   - Removed `OrderingNames` Wails TS artifact
   - Deleted `meta/` subpackage (dead multi-database feature)
   - Deleted stale `README.md`, added `doc.go`
   - Consolidated error message constants
   - Fixed `sqlboiler.toml` db path
   - Schema: added `dedupe_key` column, removed `qso_data_no_duplicates`
     constraints, regenerated sqlboiler models

**Milestones doc:**

8. **Wrote `docs/v2-design/milestones.md`** defining milestones 1,
   1b, 1c, 2, and 3 with concrete acceptance tests.

**Milestone 1 daemon code (second half of session):**

9. **`internal/config`** rewritten with `Load()`, `DefaultConfig()`,
   `ServerConfig` struct, `applyDefaults()`.

10. **`internal/qsoservice`** — `Submit()` with full validation
    (required fields, band/mode/date/time, time coherence across
    midnight, frequency MHz→kHz conversion), dedupe key computation,
    `force=true` bypass with random nonce, atomic QSO + upload-queue
    transaction.

11. **`internal/api`** — `Server` struct with `handleSubmitQso` and
    `handleHealthz`, error envelope helpers, logbook auto-creation by
    `STATION_CALLSIGN`, `MaxBytesReader` with proper close.

12. **`cmd/smd/main.go`** — full daemon wiring: config loading, iocdi
    container, service resolution, sqlite Open/Migrate, HTTP server on
    Unix socket, signal-based graceful shutdown.

13. **Dev environment** — `build/config.json`, `task run`, `.gitignore`
    updates, `Taskfile.yml` updated with `run` task and full `build`.

14. **Tests** — 25 new tests across 4 packages:
    - `qsoservice`: dedupe (5), frequency conversion (4), empty-field
      panic (1)
    - `config`: load/defaults/service (6)
    - `api`: healthz, submit happy/error/edge cases including
      midnight-crossing time coherence (14)

15. **CI** — `.github/workflows/ci.yml` with vet + build + test-race.
    Passes cleanly on GitHub.

16. **Bugs found and fixed during testing:**
    - `force=true` needed random dedupe key for UNIQUE index
    - Malformed ADIF returned 500 instead of 400 (STATION_CALLSIGN
      check moved before logbook resolution)
    - `MaxBytesReader` resource leak (deferred close added)
    - `ComputeDedupeKey` now panics on empty fields (programming error)
    - `TIME_ON > TIME_OFF` validation added for midnight-crossing QSOs

---

## Sessions 4–6 (compressed summaries)

Per the maintenance rule, older session entries are compressed.

### Session 4 (2026-04-16)

Short session. Added `Taskfile.yml` (vet, build, test, tidy). Deleted
remaining v1 leftovers (`scripts/`, `assets/xdg/`, `web/shared-utils/`).
Commit `1ee2ced`.

### Session 5 (2026-04-17)

Wrote `docs/v2-design/api.md` (HTTP API design brief). Strengthened
`docs/v1-analysis/invariants.md` with master "nothing blocks logging"
rule. Full `internal/errors` code review and 11-finding fix pass
(commit `376a3dd`). Wrote `docs/reviews/internal-logging.md` (14
findings). Saved two feedback memories (one question at a time, no
magic numbers).

### Session 6 (2026-04-18)

Applied all 14 `internal/logging` review findings across 3 commits
(`7359b6c`, `ba5f8ba`, `af46e43`). Discovered and fixed a pre-existing
embedding bug in `trackedLogEvent`. Extracted debug tracking behind
build tags. Deleted `Dump`. Added 3 regression tests. Test suite
dropped from ~15s to ~1.7s. Both `internal/errors` and
`internal/logging` reached v2 final state.

---

## Sessions 1–3 (compressed summaries)

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

---

## Next steps (priority order)

### Milestone 1b — QSO CRUD and logbook management

The daemon's full read/write API surface. Still `curl`-only, no GUI.
See `docs/v2-design/milestones.md` for the full scope and acceptance
test.

**Implementation order follows the operator workflow** (decided
end-of-session 7), not the endpoint list. Each group is built and
tested before moving to the next:

1. **Logbook CRUD** — can't log a QSO without a logbook.
   - `GET /v1/logbook` — list all logbooks
   - `GET /v1/logbook/:id` — fetch a single logbook
   - `POST /v1/logbook` — create a logbook
   - `PATCH /v1/logbook/:id` — edit logbook metadata
   - `DELETE /v1/logbook/:id` — soft-delete a logbook

2. **Contact history** — needed while building a new QSO.
   - `GET /v1/contact-history?call=<callsign>`

3. **Contest dupe check** — needed while building a new QSO.
   - `GET /v1/contest-dupe?logbook=<id>&call=<callsign>&band=<band>&mode=<mode>`

4. **QSO fetch/edit/delete** — post-logging corrections.
   - `GET /v1/qso/:id`
   - `PATCH /v1/qso/:id`
   - `DELETE /v1/qso/:id`

5. **QSO list with pagination** — browsing the log.
   - `GET /v1/logbook/:id/qso` (forward-cursor pagination)

6. **Version** — diagnostic, lowest priority.
   - `GET /v1/version`

**Implementation note:** Go 1.22+ `http.ServeMux` supports method+path
patterns (`GET /v1/qso/{id}`) natively, so no router dependency is
needed for path parameters.

### v2 design work

- **Pick the ORM/generator approach** → `docs/v2-design/db-layer.md`.
  sqlboiler stays until there's a reason to change.
- **Multi-rig as first-class assumption** →
  `docs/v2-design/multi-rig.md` when bridge design starts.
- **Expand `CLAUDE.md` frontend guidance** when Wails clients return
  in milestone 2.

### Deferred features

- **Logging-app text-file fallback reconciliation** — milestone 2+.
  The logging app maintains local text-file durability when daemon is
  unreachable; reconciliation resubmits via `POST /v1/qso` + dedupe.
- **Enrichment / contacted_station population** — milestone 2. Client-
  side concern: logging app looks up callsigns online, caches in
  `contacted_station` via daemon API. The daemon's submit path stays
  fast and network-free.
- **Daemon dashboard / monitoring UI** — post-milestone 2. Would
  subscribe to the same SSE endpoint as the logging/logbook apps.

### v1 branch follow-ups

- Data race candidate fix (session 6) not yet verified on v1 branch.
- Hardcoded QRZ forwarder — v2 concern, unlikely to be fixed on v1.

### Maintenance

- Compress sessions 4-6 to summaries after session 8 lands.
- Update this file at end of every session.
