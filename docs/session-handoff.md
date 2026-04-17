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

## Current state (as of 2026-04-17 end-of-session 9)

### Milestone 1 complete, milestone 1b 4 of 6 landed

Milestone 1 (submit a QSO) is complete and CI-green. Milestone 1b
(QSO CRUD and logbook management) is 4 of 6 done — logbook CRUD,
QSO fetch/edit/delete, QSO list with cursor pagination, and the
contest-dupe endpoint have all landed.

### Milestone 1b progress

| Step | Scope | Status |
|------|-------|--------|
| 1. Logbook CRUD | `GET/POST/PATCH/DELETE /v1/logbook` | **done** (session 8) |
| 2. QSO fetch/edit/delete | `GET/PATCH/DELETE /v1/qso/:id` | **done** (session 9) |
| 3. QSO list with pagination | `GET /v1/logbook/:id/qso` | **done** (session 9) |
| 4. Contest dupe check | `GET /v1/contest-dupe` | **done** (session 9) |
| 5. Contact history | `GET /v1/contact-history` | not started |
| 6. Version | `GET /v1/version` | not started |

### FREQ added to dedupe-key inputs (session 9)

The dedupe-key hash was expanded from
`CALL|BAND|MODE|QSO_DATE|TIME_ON` to
`CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON`. Aligns with ADIF-spec
guidance on QSO identity and distinguishes same-call/same-time
contacts on different frequencies (net ops, split, frequency
hopping). FREQ is the normalized integer-kHz string so "14.074" /
"14074" / "14.0740" all hash to the same key.

No schema change — `dedupe_key` is just a hash column. No migration
needed pre-1.0.

### PATCH design decisions (session 9)

- **Immutable fields:** `id`, `logbook_id`, `station_callsign`,
  `dedupe_key`, forwarding state (`sm_*`, `qrzcom_*`), enrichment
  (`country_details`, `contact_history`). Always restored from the
  existing row after `json.Unmarshal` overlay. Clients cannot rewrite
  them via PATCH even if they include those keys in the body.
- **Dedupe-key recompute:** if any of CALL/BAND/MODE/FREQ/QSO_DATE/
  TIME_ON change, the key is recomputed. A new key that collides with
  another QSO in the same logbook returns 409 `duplicate_key`. No
  `force=true` bypass on edit — edit is never allowed to create a
  duplicate.
- **No parallel patch struct.** PATCH accepts a JSON body matching
  the canonical `types.Qso` shape. `json.Unmarshal` overlays present
  keys onto a copy of the existing QSO; missing keys leave fields
  alone. Adding an ADIF field to `types.Qso` automatically becomes
  editable via PATCH with no second change.

### DELETE is soft-delete only (session 9)

`DELETE /v1/qso/:id` flips `deleted_at`. The daemon's job is "log +
forward"; any hard-delete / purge tooling is a logbook-app concern.
Soft-deleted rows are hidden from `FindQso` (sqlboiler's generated
WHERE clause filters `deleted_at IS NULL`), so subsequent GETs
return 404. The partial unique index on `dedupe_key` is scoped
`WHERE deleted_at IS NULL`, so soft-deleting a QSO frees its dedupe
key — the same (call, band, mode, freq, date, time) can be re-logged
after deletion.

### FREQ is MHz on the external surface (session 9)

`types.Qso.Freq` was storing the integer-kHz string, leaking a
storage unit out through the HTTP API and the "QSO stored" log line.
Fixed: `types.Qso.Freq` is the ADIF-native MHz decimal string
(e.g. `"14.074"`) everywhere above the adapter; the sqlite adapter
is the only place that knows about integer-kHz storage.

- `utils.ParseFreqMHz(string) (int64, error)` and
  `utils.FormatFreqMHz(int64) string` are the kHz↔MHz bridge,
  co-located with the other freq helpers.
- The old `qsoservice.FreqMHzToKHzString` helper was removed.
- The sqlite `freq` column is still INTEGER kHz (per v2-design
  decision: SQLite likes integers for sortable/indexable storage;
  translation happens in the daemon).
- Dedupe-key hash still uses the int-kHz string internally for
  deterministic numeric normalization ("7.050" / "7.0500" / "7050"
  all collapse to the same integer).

### Cursor-based QSO list pagination (session 9)

`GET /v1/logbook/{id}/qso?after=<cursor>&limit=<N>` per api.md §4.4.
Forward-only, DESC sort on `(qso_date, time_on, id)` — newest first.
Cursor is opaque base64url-encoded JSON `{"d","t","i"}`. Response
shape: `{"items": [...], "next_cursor": null | "<token>"}`.

- `ServerConfig.DefaultPageLimit` (50) and `ServerConfig.MaxPageLimit`
  (500) added. Clients that omit `?limit` get the default; values
  above max are silently clamped; non-positive values are 400.
- Soft-deleted rows are hidden (sqlboiler default).
- Opt-in "show deleted" is deferred — logbook-app concern per the
  narrow-daemon invariant. When the logbook-app is built we'll add
  `?include_deleted=true` symmetrically across GET/LIST.

### Contest-dupe endpoint (session 9)

`GET /v1/contest-dupe?logbook=<id>&call=<callsign>&band=<band>&mode=<mode>`
returns `{"duplicate": bool}`. Mode is optional: omit for band-only
contests (ARRL DX), include for band+mode contests (CQ WW).

- Narrow purpose-built endpoint rather than filters on the list
  endpoint — contest operators hit this path hard and the answer
  is a single boolean.
- **Contest isolation via logbook-per-contest, not separate DB
  file.** The logbook abstraction is designed for exactly this
  partitioning; contest-dupe queries are `WHERE logbook_id = ?` so
  they're inherently scoped to the contest's logbook with no
  cross-contamination. See the `project_sm_session_scope.md` memory
  for the related "logging session stays client-side" decision.
- `IsContestDuplicateByLogbookIDWithContext` widened to take an
  optional `mode` argument.

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

## What happened in session 9 (2026-04-17)

### Goals set for the session

- Implement milestone 1b step 2 (QSO fetch/edit/delete)
- Extend the stress test to exercise new read/edit/delete paths

### What got done

1. **`GET /v1/qso/{id}`** — `handleGetQso` in
   `internal/api/handler_qso.go`. Parses `{id}`, calls
   `FetchQsoByIdWithContext`, maps `ErrNotFound` → 404. Soft-deleted
   rows return 404 because `FindQso` already filters
   `deleted_at IS NULL`.

2. **`PATCH /v1/qso/{id}`** — `handleUpdateQso` + `qsoservice.Update`
   in `internal/qsoservice/update.go`. First iteration built a
   `QsoPatch` struct with pointer fields per editable attribute; was
   torn out and rebuilt to use `types.Qso` directly via
   `json.Unmarshal` overlay + stash-restore of immutables. The
   rewrite prevents drift with `types.Qso` and honors the "adding an
   ADIF field is a one-line change" invariant. Validation errors
   come back as `*SubmitError`; collision → `duplicate_key` → 409.
   No `force=true` bypass.

3. **`DELETE /v1/qso/{id}`** — `handleDeleteQso` + sqlite
   `DeleteQsoByIDWithContext`. Soft-delete via sqlboiler's
   `qso.Delete(ctx, h, false)`. Returns 404 if the QSO doesn't exist
   or is already soft-deleted. No FK check — QSO is the child. Test
   `TestDeleteQso_FreesDedupeKey` locks in the behavior that
   soft-deletion frees the dedupe-key slot (thanks to the partial
   unique index `WHERE deleted_at IS NULL`).

4. **FREQ added to dedupe-key inputs** —
   `ComputeDedupeKey(call, band, mode, freq, qsoDate, timeOn)`.
   Tests, call sites (submit + update), and the `dedupeChanged`
   check in `Update` all updated in lockstep. See "FREQ added to
   dedupe-key inputs" above for the rationale.

5. **`IsSubmitError` uses `errors.As`** instead of a direct type
   assertion. Future-proofs against anyone wrapping a `*SubmitError`
   with `%w` or the `internal/errors` builder.

6. **Stress test expanded** — `TestStress_20Clients_50QSOs` now runs
   submit → fetch (verify call) → PATCH(freq) (verify dedupe-key
   recomputed) → DELETE (verify 204, verify subsequent GET 404) per
   QSO. 1000 QSOs, zero errors across all four operations, race
   detector clean. End-to-end round trip ~17–18 ms.

7. **Types-package audit** — searched for exported types outside
   `internal/types` that cross package boundaries. Concluded (with
   user agreement) that types move into `internal/types` only when
   there is actual cyclic-dependency risk, not prophylactically. No
   migrations made; `adif.Record`, `qsoservice.SubmitResult`,
   `qsoservice.SubmitError` stay in their own packages.

8. **FREQ leak fix — MHz is the canonical external form.**
   `types.Qso.Freq` was holding the integer-kHz string, so HTTP
   responses and log lines returned `"14074"` instead of `"14.074"` —
   violating the ADIF-follows-spec invariant. Fix: added
   `utils.ParseFreqMHz` / `utils.FormatFreqMHz`, moved the
   MHz↔kHz boundary into the sqlite adapter, made `types.Qso.Freq`
   canonical MHz everywhere above the adapter. DB column stays
   INTEGER kHz (user decision: SQLite prefers integers for sortable
   storage). `qsoservice.FreqMHzToKHzString` removed; dedupe-key
   hash still uses the int-kHz string internally for determinism.
   Adapter tests had nonsense `Freq: 14250000` values (14.25 GHz
   in kHz) — fixed to realistic `14250` / `"14.250"` in the process.

9. **`IsContestDuplicateByLogbookIDWithContext` widened** to take
   an optional `mode` argument for band+mode contests, in
   preparation for the contest-dupe endpoint.

10. **`GET /v1/logbook/{id}/qso`** — forward-cursor pagination.
    New sqlite method `FetchQsoPageByLogbookWithContext` uses a
    tuple predicate on `(qso_date, time_on, id)` DESC and fetches
    `limit+1` to detect "has more" cheaply. Handler encodes/decodes
    an opaque base64url JSON cursor. `ServerConfig.DefaultPageLimit`
    (50) and `ServerConfig.MaxPageLimit` (500) added. Nine tests
    including a three-page walk with full ordering reconstruction
    and soft-delete-hidden assertion.

11. **`GET /v1/contest-dupe`** — narrow purpose-built endpoint.
    Validates `logbook`, `call`, `band` (required) and `mode`
    (optional). Returns `{"duplicate": bool}`. 15 tests covering
    band-only / band+mode hits and misses, soft-delete exclusion,
    logbook-scoping (hit in logbook A must NOT match in logbook B —
    the whole point of the logbook-per-contest pattern), and all
    validation error paths.

### Coverage summary end-of-session

| Package | Coverage |
|---------|----------|
| `internal/api` | full CRUD + list + contest-dupe handler tests; 1000-QSO stress round trip |
| `internal/qsoservice` | `Update` and `Submit` exercised via api tests; dedupe unit tests cover freq |
| `internal/database/sqlite` | new `DeleteQsoByIDWithContext`, `FetchQsoPageByLogbookWithContext`; widened contest-dupe method |
| `internal/utils` | new freq-conversion helpers with round-trip tests |

Full suite race-detector clean.

### Design decisions made

- **`types.Qso` is the canonical DTO** for HTTP/service boundaries.
  Do not build parallel `XPatch` / `XRequest` structs that duplicate
  field lists from `types.X`. Use `json.Unmarshal` overlay + stash-
  restore of immutables instead. Captured as a memory.
- **types package rule is pragmatic, not prophylactic.** Exported
  types move to `internal/types` only when an actual cycle could
  form, not as a preventive measure. Captured as a memory.
- **FREQ is part of QSO identity.** Dedupe key now includes FREQ per
  ADIF-spec guidance. Schema unchanged.
- **FREQ on the external surface is MHz** (ADIF-native). kHz is the
  sqlite storage unit; translation lives in the adapter, not
  anywhere above it.
- **PATCH design:** immutable fields always restored server-side,
  dedupe inputs recomputed on change, collision rejected with 409,
  no force bypass on edit.
- **DELETE is always soft-delete at the daemon.** Hard-delete stays
  a logbook-app concern.
- **Pagination is forward-cursor only**, newest-first, opaque token.
  Soft-deleted rows are always hidden. Opt-in "show deleted" is
  deferred until the logbook-app needs it.
- **Contest isolation via logbook-per-contest, not separate DB
  file.** `logbook_id` partition gives the contest-dupe endpoint
  false-positive-free scoping by construction.
- **Logging session is entirely client-side.** No `session_id`
  column, no `/v1/session` endpoints. The logging app keeps an
  in-memory list of QSOs submitted since Start, uses existing
  PATCH/DELETE for edits, and a future `POST /v1/logbook/{id}/export?ids=…`
  for end-of-session email. Captured as a memory.

---

## What happened in session 8 (2026-04-17)

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

3. **Code style fixes:**
   - `server.go` — `fmt.Errorf` → `errors.New(op).WithErr(err)`
   - All handler ops standardised to `errors.Op` type with
     `api.FuncName` pattern
   - `parsePathID` — proper `const op errors.Op = "api.parsePathID"`
   - `writeError` and `ErrorResponse.Op` — `errors.Op` type
   - `ServerConfig.Protocol` field added (default `"unix"`)
   - Listener protocol now config-driven in `ListenAndServe`

4. **Logbook update tightened:**
   - Callsign is immutable after logbook creation — PATCH only
     accepts `name` and `description`. A callsign field in the
     request body is silently ignored.
   - SubmitError messages cleaned up — no programming symbols,
     consistent `invalid_time_range` code for time coherence errors.

5. **Callsign validation added:**
   - `IsValidCallsign()` in `qsoservice` — minimum 3 chars, at
     least 1 digit, maximum 32 chars. Accepts `/` for portable
     suffixes and secondary prefixes.
   - Enforced at three levels: schema CHECK, API handler
     (logbook creation + STATION_CALLSIGN), and domain service
     (CALL + STATION_CALLSIGN in Submit).
   - Tests for valid callsigns (K1A, G4ABC, 7Q5MLV, 7Q5MLV/T)
     and invalid (too short, no digit, empty).

6. **Logbook delete with QSOs fix:**
   - Soft-delete didn't trigger FK RESTRICT. Added explicit QSO
     count check in `DeleteLogbookByIDWithContext` before
     soft-deleting.

7. **Coverage gaps plugged:**
   - Delete logbook with QSOs (409 conflict)
   - Body too large (413)
   - Invalid CALL callsign in ADIF
   - Invalid STATION_CALLSIGN in ADIF

8. **Stress test:**
   - 20 concurrent clients, 50 QSOs each, 1000 total
   - Clients 0-9: CW (RST 599), clients 10-19: SSB/USB (RST 59)
   - Each QSO includes non-promoted fields (comment, name, qth,
     gridsquare, my_gridsquare) exercising the additional_data
     JSON blob
   - Results: 1000/1000 stored, 0 errors, ~146 QSOs/sec, ~6.8ms
     avg latency, race detector clean
   - Benchmark: daemon has ~100x headroom over peak operator load

9. **sqlite package coverage (0% → 66.9%):**
   - 28 new tests in `internal/database/sqlite/service_test.go`
   - Covers: service lifecycle (Initialize/Open/Close/Ping
     idempotency, error paths), logbook CRUD, QSO insert/fetch/update,
     dedupe key lookup, contest dupe check, contacted station CRUD,
     country CRUD, QSO list and count, contact history, paging,
     upload queue insert/fetch/update status, upsert
   - Found and fixed latent bug: `UpsertLogbookWithContext` had
     `updateOnConflict=false` — silently no-op'd on existing rows.
     Now correctly set to `true`.

10. **Added `logbook_id` to QSO stored log line:**
    - `submit.go` log now includes `logbook_id` alongside `qso_id`,
      `call`, `band`, `mode` for full diagnostic context.

### Coverage summary end-of-session

| Package | Coverage |
|---------|----------|
| `internal/api` | 70.0% |
| `internal/config` | 90.3% |
| `internal/database/sqlite` | **66.9%** (was 0%) |
| `internal/database/sqlite/adapters` | 94.0% |
| `internal/qsoservice` | 17.5% direct (exercised via api tests) |

Race detector clean across all 14 packages.

### Design decisions made

- **Logbooks are created explicitly** — no auto-creation during QSO
  submit. The client creates logbooks via `POST /v1/logbook` first.
- **QSO submit validates logbook ID + callsign match** — the daemon
  fetches the logbook by ID and verifies its callsign matches
  STATION_CALLSIGN. Both must match; either failing returns a clear
  error.
- **Logbook callsign is immutable** — set at creation, cannot be
  changed via PATCH. The callsign is the logbook's identity; changing
  it would break the STATION_CALLSIGN contract with existing QSOs.
- **Callsign validation at the gate** — minimum 3 chars, at least
  1 digit, max 32. The daemon is the system boundary; bad data
  rejected here doesn't reach sqlite. Callsign parsing (prefix/
  suffix structure, DXCC mapping) is a client/enrichment concern.
- **Workflow-driven implementation order** — reprioritised to focus
  on what the operator needs to log and manage QSOs. QSO
  fetch/edit/delete before enrichment features. Contact history is
  nice-to-have, not essential (especially during a pile-up).
- **`errors.Op` convention** — all ops follow `package.FuncName`,
  typed as `errors.Op`, not plain strings.
- **Listener protocol configurable** — `ServerConfig.Protocol`
  defaults to `"unix"` but supports `"tcp"` for network deployment.

---

## Session 7 (2026-04-16, compressed)

Reviewed all 8 carry-forward packages and wrote
`docs/v2-design/milestones.md`. Implemented milestone 1: daemon,
config, qsoservice (validation/dedupe/atomic write), API handlers,
error envelope, healthz. Dev environment + GitHub Actions CI.
5 bugs fixed during testing. 25 tests across 4 packages. Schema
cleanup (removed session table/apikey, added dedupe_key column).

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

### The immediate next action (session 10 start)

**Implement contact history.** Step 5 of the milestone 1b workflow.
Nice-to-have rather than essential — operator uses it outside a
pileup to see "have I worked this call before, and where?".

- `GET /v1/contact-history?call=<callsign>` — returns prior QSOs
  with a callsign across all logbooks (not scoped to a single
  logbook, unlike contest-dupe).
- sqlite layer already has `FetchQsoSliceByCallsignWithContext`
  which returns a `[]types.ContactHistory` slice. Check whether its
  current output shape is what the endpoint should return directly
  or whether any transformation is needed.
- Likely a simpler handler than the list endpoint — no pagination
  unless the result set is huge. Spec doesn't call out pagination
  for this path.

Design questions to resolve (one at a time) before coding:
- **Scope:** all logbooks, or a `?logbook=<id>` filter? My read:
  all logbooks by default (the whole point is "have I ever worked
  them"); optional `?logbook=<id>` for operators who want to restrict.
- **Soft-delete:** hide, same as everywhere else.
- **Ordering:** newest-first, same convention as list.
- **Result cap:** is there one? Per the memory, any number-with-meaning
  goes in config rather than hardcoded.

### Continue milestone 1b — remaining workflow steps

6. **Version** — diagnostic, lowest priority.
   - `GET /v1/version` — daemon build version, go version, schema
     version. Useful for diagnostics and for clients that want to
     refuse to talk to an incompatible daemon.

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

- Compress session 8 after session 10 lands.
- Update this file at end of every session.
