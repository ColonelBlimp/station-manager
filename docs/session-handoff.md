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

## Current state (as of 2026-04-19, session 13 in progress)

### QRZ port: stages 1–4 complete

The 8-stage QRZ port is past the halfway mark. Stages 1–4 are
committed and under test, with stage 4 validated against real
QRZ via a manual live harness. Stages 5–8 remain.

**Stage 1 — Forwarder interface extension** (session 12, committed):

- `Forwarder.Submit` gained a `priorUpstreamID string` parameter so
  the worker can pass QRZ's LOGID (captured on the earlier successful
  insert's `Result.UpstreamID`) through to the delete call.
- New `AdifPrefix() string` method: declarative metadata telling the
  worker which ADIF upload-status field pair to stamp on the QSO row
  on success (`QRZCOM_QSO_UPLOAD_STATUS` / `QRZCOM_QSO_UPLOAD_DATE`
  for QRZ, `CLUBLOG_*` for ClubLog, `""` for stub / custom webhooks).
  v1 did this stamp from inside the QRZ service; v2 moves it to the
  worker so forwarders stay pure plugins.

**Stage 2 — QRZ package skeleton** (session 13):

- `internal/forwarding/qrz/qrz.go`: `Type = "qrz"`,
  `AdifFieldPrefix = "QRZCOM"`, registry `init()`, `New` with
  credentials validation, stubbed `Submit` that returns Terminal
  until stage 4 lands the real HTTP call.
- `internal/forwarding/qrz/qrz_test.go`: 9 tests covering registry
  round-trip, happy path, malformed/missing credentials, ctx
  cancellation.
- **Credentials shape decided**: `{"api_key": "..."}` — only.
  QRZ enforces the callsign/logbook match server-side (every QSO's
  `STATION_CALLSIGN` must match the logbook's callsign, or QRZ
  rejects the record); keeping a local copy of the callsign would
  only introduce config-drift risk without a correctness guarantee.
  `forwarding.md` §2 updated.

**Stage 3 — Response parser + classifier** (session 13):

- `internal/forwarding/qrz/response.go`: `parseResponse(body)` (pure
  function, `net/url.ParseQuery`-based) and `classifyResponse(act,
  resp)` split into per-action helpers (`classifyInsert`,
  `classifyUpdate`, `classifyDelete`). `AUTH` short-circuits across
  all actions. No substring matching on `REASON` text — QRZ's
  documented per-action RESULT sets are unambiguous.
- `internal/forwarding/qrz/response_test.go`: 26 tests covering the
  full per-action matrix (see `forwarding-implementation.md` §8.1).
- **Key classification refinement**: for `action=delete`, QRZ's
  single-LOGID delete makes `RESULT=FAIL` unambiguously mean "LOGID
  not found". We reclassify it as `OutcomeSuccess` — the record's
  absence upstream matches intent. `RESULT=PARTIAL` on a
  single-LOGID delete is treated as Terminal (shouldn't occur in
  practice).

**Stage 4 — HTTP Submit for insert + update** (session 13):

- `internal/forwarding/qrz/qrz.go`: real `Submit` implementation
  with `buildForm` (insert = `ACTION=INSERT + ADIF`; update =
  `ACTION=INSERT + OPTION=REPLACE + ADIF`) and `classifyHTTPStatus`
  (408/429/5xx → Transient; other non-2xx → Terminal; 2xx falls
  through to body parse). Delete still returns Terminal "deferred
  to stage 5".
- Package-level knobs: `DefaultEndpoint = "https://logbook.qrz.com/api"`,
  `DefaultHTTPTimeout = 30 * time.Second`, `var UserAgent =
  "station-manager/dev"` (to be overridden from `cmd/smd/main.go`
  alongside the blank import in stage 8).
- Package-internal `newWithEndpoint(apiKey, endpoint, client)` —
  tests use it to point at `httptest.NewServer.URL`; production
  code goes through public `New` with the real endpoint.
- `submit_test.go`: 18 httptest-based tests covering transport
  class (network error, ctx cancel, 408/429/500/400/401), body
  class (OK/FAIL/AUTH/REPLACE on insert+update), malformed bodies,
  request-shape assertions (method, KEY, ACTION, OPTION=REPLACE
  on update, ADIF payload, User-Agent), delete-deferred guard,
  unknown-action fallthrough.
- **Live harness** at `internal/forwarding/qrz/live_test.go`
  (`//go:build manual`, gated by `QRZ_TEST_API_KEY` +
  `QRZ_TEST_CALLSIGN` env vars loaded from `.env`):
  - `TestLive_InsertThenUpdate` — quick round-trip with t.Cleanup
    delete; `task test:qrz-live`.
  - `TestLive_InteractiveFlow` — insert → pause → update → pause →
    delete, with `[Enter]` prompts between steps so the operator
    can inspect the record on QRZ.com. `task test:qrz-live-interactive`.
  - **Gotcha recorded**: `go test` feeds the test binary a closed
    stdin, so `bufio.Scanner(os.Stdin)` returns EOF immediately.
    Interactive test opens `/dev/tty` directly to read the
    controlling terminal — Unix-only (Linux/macOS is fine for the
    operator's setup).
- **Live-validated end-to-end**: insert → LOGID returned, update
  with `OPTION=REPLACE` returns the same LOGID (confirming in-place
  update rather than new record), raw delete cleans up. Real QRZ
  response shapes match our parser's assumptions exactly.
- **DB-level verification in the live harness is deferred to
  stage 6** — when `MarkUploadSuccessWithAdifStampWithContext`
  lands, that's the fresh multi-table tx code that earns a
  real-stack check. Today's layered tests (worker + SQLite in-memory
  with stub forwarder; QRZ unit + live with no DB) cover the seam
  transitively.

Full suite green under `-race` after each stage. Stage 5 (delete
via `Submit` + worker-side LOGID lookup) is the immediate next
action.

### Forwarder subsystem thin-slice complete (session 11)

Design: `docs/v2-design/forwarding.md` is the authoritative shape;
the 11-stage thin slice below implements it end-to-end. All 11
stages landed in session 11. The spine — POST → queue → worker →
forwarder submit → persist outcome → pull endpoint — is covered by
a regression test at `internal/api/handler_e2e_test.go` for the
insert / update / delete actions.

**What's still deferred from milestone 1c** (tracked below as
follow-ups, not thin-slice scope):

- **Real QRZ forwarder implementation.** The stub exercises the
  plumbing; porting v1's `internal/upload/qrz/` into
  `internal/forwarding/qrz/` is milestone-1c work but was not part
  of the 11-stage slice.
- **SSE event stream (`GET /v1/events`).** Terminal transitions
  (`in_progress → uploaded` / `failed`) are the emit sites per
  forwarding.md §7, but the stream itself hasn't been built yet.
  The worker code has comments marking the emit points.
- **Manual re-queue / dead-letter cleanup endpoints** (forwarding.md
  §11). Deferred by design; no design pressure yet.

### Session 11 progress (2026-04-18)

**Design doc landed.** `docs/v2-design/forwarding.md` settles the
internal shape of the forwarder subsystem: constraints, terminology,
fan-out config, `Forwarder` interface, per-destination worker
topology, retry policy, queue-row data shape (§6), lifecycle and
status transitions, `SafeGo` recovery, v1 migration, acceptance.
Walked through the flow step-by-step with the user, which surfaced
several refinements:

- **Ham services are effectively singleton per operator.** One QRZ,
  one ClubLog, one LoTW per operator; `forwarder_name` defaults to
  the type string. The `name`/`type` split exists for rename safety
  (historical rows stay interpretable when an operator relabels a
  destination), not because we expect multi-instance deployments.
  Memory: `project_sm_ham_services_singleton.md`.
- **Retry defaults live in the forwarder package, not config.** Each
  upstream's tolerances are different; `qrz.New` knows what QRZ can
  take. Operators only write a `retry` block in config when they
  need to override.
- **Config reload is off the table.** Restart required for config
  changes. Live reload introduces real complexity (in-flight
  attempts, credential rotation) without matching operator benefit.
- **Slow-link operator-environment defaults** went into the doc:
  `tick_interval_sec=120`, `batch_size=5`, matching v1 operational
  values. Memory: `project_sm_operator_network.md`.

**Implementation plan: 11-stage thin slice**, each stage a
committable unit:

| # | Stage | Status |
|---|-------|--------|
| 1 | Schema update — split `service` into `forwarder_name`+`forwarder_type`, add `next_attempt_at`, `upstream_id` | **done** |
| 2 | Config surface — `ForwarderConfig`/`RetryConfig` in types, `Forwarders[]` on `Config`, defaults + validation, `Forwarders()` accessor | **done** |
| 3 | `internal/forwarding/` — `Forwarder` interface, `Outcome`/`Result`, `Action` alias, init()-time `Register`/`Build` registry | **done** |
| 4 | Stub forwarder — `internal/forwarding/stub/`, modes: `always_success`, `always_transient`, `always_terminal`, `flap_n` | **done** |
| 5 | `safego` helper — landed as `internal/safego/` (not `internal/utils`; cycle avoided), callback-based, ctx-aware cooldown | **done** |
| 6 | DB methods — `ClaimPendingUploadsWithContext` (atomic `UPDATE ... RETURNING`), `MarkUpload{Success,TransientRetry,Failed}WithContext`, `ResetOrphanedUploadsWithContext`, `FetchUploadsByQsoIDWithContext` | **done** |
| 7 | Wire ingest — `submit.go`/`update.go` loops read `config.Forwarders` filtered by enabled + action_filter; new `qsoservice.Delete` atomically soft-deletes + enqueues delete rows | **done** |
| 8 | Worker loop — `internal/forwarding/worker/` per-forwarder tick + claim + submit + persist | **done** |
| 9 | Startup wiring — `main.go` orphan sweep + spawn workers via SafeGo | **done** |
| 10 | Pull endpoint — `GET /v1/qso/:id/uploads` | **done** |
| 11 | E2E integration test — POST → observe row transition to `uploaded` | **done** |

**Stage 1 cleanup (incidentally resolved):**
- `uploadRetryCooldown` + `defaultUploadBatchLimit` constants
  deleted from `sqlite/consts.go` — the M8 `TODO(forwarder)` is
  closed. Retry cadence now lives in per-forwarder config / the
  forwarder package's own defaults.
- `types.RequiredConfigs` + `config.Service.RequiredConfigs()`
  deleted (its one field `QsoForwardingRowLimit` was consumed only
  by the now-deleted legacy worker code; the replacement lives in
  `ForwarderConfig.BatchSize`).
- Legacy v1 worker methods (`InsertQsoUploadWithContext`,
  `FetchPendingUploadsWithContext`, `UpdateQsoUploadStatusWithContext`
  and their non-ctx wrappers) deleted from the sqlite package;
  their three tests likewise removed. Stage 6 added the new
  purpose-built replacements.

**Stage 4 — stub forwarder.** `internal/forwarding/stub/` implements
`Forwarder` with four modes (`always_success`, `always_transient`,
`always_terminal`, `flap_n`) selected via the credentials blob. Ctx
cancellation short-circuits before the call counter bumps so tests
can assert on "how many real submits happened" cleanly. Registers
under type `"stub"` via `init()`; 11 tests covering validation,
each mode, flap transition, ctx-cancel, and round-trip via
`forwarding.Build`.

**Stage 5 — `internal/safego/`.** Deviation from the draft doc:
lives in its own package, not `internal/utils`. Cause: `logging`
already imports `utils`, so putting `*logging.Service` in utils
would create a cycle. The landed shape takes a `PanicHandler`
callback instead of a concrete logger — zero dependency on logging,
callers wire the log format. Signature also gained a `ctx` parameter
so the cooldown sleep is interrupted by shutdown rather than
spawning a final respawn that immediately exits. Cooldown is an
`atomic.Int64` (nanoseconds) after the race detector caught a real
race between `t.Cleanup` and still-running goroutines. `docs/v2-
design/forwarding.md §9` rewritten to match as-implemented shape.

**Stage 6 — upload-queue DB surface.** Six methods, all worker-
facing. `ClaimPendingUploadsWithContext` is the atomic
`UPDATE ... RETURNING *` from the design doc, scoped to a single
forwarder so two workers never compete. `modified_at` is driven by
`trg_qso_upload_set_updated_at` so the mark/claim statements don't
touch it manually; SQLite's default `recursive_triggers=off` prevents
the trigger's own UPDATE from re-firing. Empty `upstream_id` is
stored as NULL rather than the empty string. New
`QsoUploadModelToType` adapter flattens nullable columns for
callers that don't care about null-vs-value. 13 integration tests
cover claim ordering, forwarder scoping, future-`next_attempt_at`
gating, each mark method, orphan sweep, and pull-endpoint fetch.

**Stage 6b — sqlboiler refactor (post-review).** User flagged that
four of the Stage-6 methods were using raw SQL where sqlboiler's
type-safe builders would do. Refactored `MarkUploadSuccess`,
`MarkUploadTransientRetry`, `MarkUploadFailed` to the load-then-save
pattern (`FindQsoUpload` → mutate fields → `Update(ctx, h, boil.Infer())`);
refactored `ResetOrphanedUploads` to `models.QsoUploads(...).UpdateAll(...)`.
`ClaimPendingUploadsWithContext` kept as raw with an expanded doc
comment naming the two sqlboiler limitations that justify the
exception (`UPDATE ... RETURNING *`, `WHERE id IN (SELECT ... LIMIT N)`
subquery-same-table). Bonus: Mark* now correctly surface
`errors.ErrNotFound` for nonexistent row IDs — the raw version was
silently no-oping, a latent bug. Preference saved as
`feedback_sqlboiler_default.md` memory.

**Stage 7 — ingest → forwarders wired.** `qsoservice.Service` gains
a `Config *config.Service` DI field. New
`internal/qsoservice/forwarders.go` helper
(`shouldEnqueue(fc, action) bool`) centralises the enabled-and-
action-filter check for all three ingest sites. `submit.go` loop
swaps the stub for `s.Config.Forwarders()`; `update.go` activates
its commented hook with the same pattern. New
`internal/qsoservice/delete.go` introduces the first domain-level
`Delete(ctx, id)` that atomically soft-deletes the QSO and enqueues
`delete`-action queue rows under one tx (one-fails-all-fail). DB
layer gains `DeleteQsoByIDTx(ctx, tx, id)` for the tx-scoped
soft-delete; the old `DeleteQsoByIDWithContext` is deleted (its one
caller, `handleDeleteQso`, now goes through `qsoservice.Delete`).
`testServer` split into `testServerWithCfg(t, mutate)` so tests can
populate `cfg.Forwarders` before construction. 6 new HTTP-level
tests verify enabled→row-inserted, disabled→skipped, action-filter
exclusion, update-path enqueue, delete-path enqueue + soft-delete,
and delete-with-zero-forwarders.

**Stage 8 — worker loop.** `internal/forwarding/worker/` lands the
per-destination goroutine the design calls for. `Worker` holds a
resolved `Config` (name, tick, batch, retry) plus references to
`*sqlite.Service`, `*logging.Service`, and a `forwarding.Forwarder`.
`Run(ctx)` runs an initial tick then selects on a `time.Ticker`
until ctx cancels; each tick calls `ClaimPendingUploadsWithContext`
for its forwarder_name and iterates rows, calling the forwarder's
`Submit` and persisting the outcome via `MarkUpload{Success,
TransientRetry,Failed}`. Soft-delete handling per forwarding.md §4
is implemented: `insert`/`update` with a soft-deleted QSO marks the
row failed; `delete` falls back to
`FetchQsoByIDIncludingDeletedWithContext` so the upstream still gets
told. Backoff (`backoff.go`) implements §5's exponential +20% jitter
with an overflow cap at `maxBackoffShift=30`. 16 tests across
`worker_test.go` (positive outcomes via real sqlite + stub
forwarder) and `backoff_test.go` (pure-function bounds). Test
helpers `runUntil(t, w, h, qsoID, match)` and `runFor(t, w, d)`
replace an earlier fixed-sleep `runOnce` shape that flaked under
`-race` load; the polling approach is deterministic regardless of
scheduler latency. New sqlite method
`FetchQsoByIDIncludingDeletedWithContext` uses `models.NewQuery` +
`qm` mods — sqlboiler's re-exported query builder — to sidestep the
auto-filter on `deleted_at IS NULL` that `FindQso` and
`models.Qsos(...)` bake in; column/table references still come from
generated constants. Memory `feedback_sqlboiler_default.md`
expanded with the `models.NewQuery` idiom so future sessions reach
for it before `queries.Raw`.

**Stage 9 — startup wiring in `cmd/smd/main.go`.** Blank import
`_ "internal/forwarding/stub"` registers the stub type. After
migrations run, `ResetOrphanedUploadsWithContext` sweeps any
`in_progress` rows back to `pending` with a 10s context; log line
fires only when the count is non-zero. A `workerCtx/workerCancel`
pair is constructed before the HTTP server starts, so workers live
exactly for the daemon's lifetime. A new
`spawnForwarderWorkers(ctx, fwds, db, logger) error` helper loops
`cfg.Forwarders`, skips disabled entries, builds each forwarder via
`forwarding.Build`, resolves retry (per-entry override or the
package-level `defaultForwarderRetry = {5, 60, 3600}` fallback, a
temporary stand-in until real forwarders supply their own per
forwarding.md §2), constructs a `worker.Worker`, and launches it
under `safego.Go` with `respawn=true`. Panic handler logs
structured fields (`goroutine`, `panic`, `stack`) through the
daemon logger. Shutdown ordering: `workerCancel()` fires **before**
`server.Shutdown(ctx)` so in-flight forwarder HTTP calls abort
promptly and workers stop starting new DB ops against the
about-to-close handle.

**Stage 10 — pull endpoint `GET /v1/qso/:id/uploads`.**
`internal/api/handler_uploads.go` implements a thin handler: parse
id (400 on bad), existence-probe via
`FetchQsoByIDIncludingDeletedWithContext` (404 only for
genuinely-unknown ids; soft-deleted QSOs still return their rows
because the delete-action forwarding work remains observable),
fetch via `FetchUploadsByQsoIDWithContext`, normalise nil→empty
slice, return `{"items": [...]}`. Route wired in `server.go`. Five
handler tests cover: two-forwarder happy path with stable
`(forwarder_name, action)` ordering, no-forwarders → literal
`"items":[]` (not `null`), soft-deleted QSO → still returns rows,
unknown id → 404 `not_found`, invalid id → 400 `invalid_id`.

**Stage 11 — end-to-end acceptance test.**
`internal/api/handler_e2e_test.go`, three scenarios, all using the
existing `testServerWithCfg` harness plus real `worker.Worker`
goroutines (plain `go` + `sync.WaitGroup` for deterministic
shutdown — `safego`'s respawn path is tested in its own package):
`TestE2E_InsertReachesUploaded` (POST, both upload rows reach
`uploaded`, `attempts=1`, `upstream_id=stub-ok`),
`TestE2E_UpdateReachesUploaded` (POST → settle → PATCH → wait for
update row to upload), `TestE2E_DeleteReachesUploaded` (POST →
settle → DELETE → wait for delete row to upload, asserts canonical
`GET /v1/qso/{id}` now 404s while the uploads endpoint still shows
the rows). Helpers: `startE2E(t, fwds...)` spawns workers with a
50ms tick and returns a shutdown closure registered as
`t.Cleanup` backstop; `waitForUploads(t, srv, qsoID, match)` polls
at 25 ms with a 3 s deadline, logs the last observed rows on
timeout.

### Current state (as of 2026-04-17 end-of-session 10)

### Milestones 1 and 1b both complete, full code review landed

Milestone 1 (submit a QSO) and milestone 1b (QSO CRUD, logbook
management, list, contest-dupe, contact history, version) are both
complete and CI-green under `-race`. The daemon now exposes the
full set of endpoints the logging-app and logbook-app need for
milestone 2+.

**Session 10 focus was hardening, not new features.** A full
independent code review (`docs/reviews/milestone-1b-review.md`)
surfaced 23 findings across high/medium/low severity; every one
has been addressed. The codebase is now in a "clean slate for
forwarder" state — no known bugs, no convention drift, no dead
code outstanding.

### Session 10 headline changes

- **H1 — concurrent-submit race plugged.** Pre-transaction dedupe
  check + UNIQUE-constraint catch-and-reclassify; deterministic
  test (`TestSubmitQso_ConcurrentDuplicate`) locks it in.
- **ADIF export moves entirely client-side.** `POST /v1/logbook/{id}/export`
  is dropped from the roadmap; clients that need ADIF use
  `internal/adif` as a library. Backup story is forwarding to
  online services, not file dumps. See
  `memory/project_sm_session_scope.md`.
- **SQL call-site audit items 1–2 landed.** New
  `LogbookCallsignByIDWithContext` on the submit hot path; new
  composite partial index `idx_qso_logbook_date_time` for cursor
  pagination.
- **M6 proactive fix for one-fails-all-fail.** `qsoservice.Update`
  is now transactional with a commented forwarder hook inside the
  tx envelope, so the forwarder PR just drops the
  `InsertQsoUploadTx(action.Update)` loop into the existing slot.
- **Daemon lifecycle is defer-based.** `cmd/smd/main.go` delegates
  to `run() error` with defers for logger + db cleanup; `fatal()`
  is gone. Failures at any startup step unwind cleanly through
  registered defers.
- **Dead code swept.** `qsoservice.FreqMHzToKHzString`,
  `sqlite.Service.ExecContext`, `sqlite.Service.QueryContext`,
  `fatal()`, and the unused-error-return in `adif.parseRecords` —
  all deleted. No functional change; less noise.
- **Convention sweep.** All 9 residual `fmt.Errorf` call sites in
  the sqlite package converted to `errors.New(op).WithErr(err).WithMsg(...)`.
  Four handler tests moved off English-message substring matching
  onto structured decode. Eight `fmt.Sscanf` sites converted to
  `unmarshalJSON`. Contact-history LIKE pattern anchored on slash
  (`X/%`) so coincidental prefixes no longer match.
- **Panic handling added (post-review).** `main()` has a
  `defer recover()` with `ExitError`/`ExitPanic` exit-code
  constants so a supervisor can tell a panic from a graceful
  error exit. `api.recoverPanic` middleware wraps the mux — any
  handler panic is structurally logged and returns a generic 500
  envelope (no panic-value leak). Worker-goroutine recovery is a
  noted follow-up for when the forwarder lands.
- **`goccy/go-json` dep dropped (post-review).** Adapters now use
  stdlib `encoding/json`; go.mod / go.sum cleaned. Consistency
  restored — one JSON library, fewer external deps.

Commits covering session 10 are in the `main` branch; the review
doc has a resolution note pointing at them.

### Milestone 1b progress

| Step | Scope | Status |
|------|-------|--------|
| 1. Logbook CRUD | `GET/POST/PATCH/DELETE /v1/logbook` | **done** (session 8) |
| 2. QSO fetch/edit/delete | `GET/PATCH/DELETE /v1/qso/:id` | **done** (session 9) |
| 3. QSO list with pagination | `GET /v1/logbook/:id/qso` | **done** (session 9) |
| 4. Contest dupe check | `GET /v1/contest-dupe` | **done** (session 9) |
| 5. Contact history | `GET /v1/contact-history` | **done** (session 9) |
| 6. Version | `GET /v1/version` | **done** (session 9) |

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
- `main` — milestone 1b complete, session-10 hardening landed. CI green.
- `v1` at `0e158ec` — unchanged since session 2.

---

## What happened in session 10 (2026-04-17)

### Goals set for the session

- Finish the SQL call-site audit items 1–2 (started late session 9).
- Do a full pre-forwarder code review to catch drift/bugs before
  the much larger forwarder subsystem lands.
- Address everything the review surfaces.

### What got done

1. **SQL audit wins (items 1–2 of the session-9 list).**
   - Added `LogbookCallsignByIDWithContext` — `SELECT callsign …`,
     skips full-row scan + adapter; submit hot path now uses it.
   - Added composite partial index
     `idx_qso_logbook_date_time ON qso (logbook_id, qso_date DESC,
     time_on DESC, id DESC) WHERE deleted_at IS NULL` for cursor
     pagination. `EXPLAIN QUERY PLAN` confirms the planner seeks
     directly on the index with no temp B-tree for ORDER BY.
   - Added directly to `0001_init.up.sql` rather than a new
     migration file — pre-1.0, no data to migrate.

2. **Export-endpoint reversal.** Previously-nominated
   `POST /v1/logbook/{id}/export` dropped from the roadmap.
   Rationale in the session-scope memory and in `api.md` §5.
   ADIF is a client/admin concern; daemon's backup story is
   forwarding to online services.

3. **Full milestone-1b review** (`docs/reviews/milestone-1b-review.md`).
   Independent agent pass with CLAUDE.md + memory files as context.
   23 findings: 2 high, 9 medium, 12 low. All addressed in the
   same session:

   **High:**
   - **H1**: concurrent-submit race (two workers passing the pre-tx
     dedupe check, second losing on UNIQUE index, surfacing as 500
     instead of 200-duplicate). Fixed with constraint-error catch
     and re-query. Deterministic regression test added.
   - **H2**: dead `qsoservice.FreqMHzToKHzString` still in tree
     despite session-9 handoff claiming removal. Deleted. `math`
     import dropped with it.

   **Medium:**
   - **M1 + M2**: shared `readBody` / `readJSONBody` helpers on
     `*Server`; logbook POST/PATCH now honour `MaxBodyBytes`;
     `*http.MaxBytesError` detected via `errors.As` instead of
     stdlib string match.
   - **M3**: SQL schema comment and `types.Qso.DedupeKey` docstring
     now name FREQ in the dedupe-key list.
   - **M4**: `sqlite.Service.Close` resets `initOnce` +
     `isInitialized` so re-init cycles work. Cycle test added.
   - **M5**: dead `ExecContext` + `QueryContext` deleted (also
     eliminates the context-cancel leak).
   - **M6**: `qsoservice.Update` is now transactional, mirroring
     Submit's shape. Commented forwarder hook in place for the
     future `InsertQsoUploadTx(action.Update)` loop.
   - **M7**: all 9 residual `fmt.Errorf` sites in the sqlite
     package converted to the `errors.New(op).WithErr(err).WithMsg(...)`
     pattern. Two `fmt` imports dropped.
   - **M8**: `uploadRetryCooldown` annotated with a `TODO(forwarder)`
     pointer naming the expected config shape
     (`ForwarderConfig.RetryCooldownSec`). Deferred to the
     forwarder PR by design.
   - **M9**: four handler tests moved off English-message substring
     matching onto structured decode (`ErrorResponse`/`types.Logbook`).

   **Low (all 12 addressed):**
   - **L1**: `writeJSON`/`writeError` are now methods on `*Server`
     with encode-error logging; 81 call sites converted.
   - **L2**: `Server.Shutdown` removes the Unix socket file
     (best-effort, gated on `s.protocol == "unix"`).
   - **L3**: "smd stopped" log moved above `loggerSvc.Close()`.
   - **L4**: `main` now delegates to `run() error` with
     defer-based cleanup; `fatal()` deleted.
   - **L5**: `LIMIT 1` in `SchemaVersionWithContext` annotated as
     defensive.
   - **L6 (broadened)**: contact-history LIKE pattern changed
     from `X%` to `X/%` — anchors on slash, matches portable
     variants (M0CMC/P) but excludes coincidental prefixes
     (M0CMCE). Two new regression tests.
   - **L7**: `missingCoreTables` checks `rows.Err()` after the
     `for rows.Next()` loop.
   - **L8**: `validTestQso` uses canonical MHz `"7.050"` instead
     of legacy kHz `"7050"`.
   - **L9**: sqlite `doc.go` lifecycle description corrected —
     Migrate is a distinct call, Close resets init guard.
   - **L10**: `config_test.go` now asserts `DefaultPageLimit=50`,
     `MaxPageLimit=500`, `MaxContactHistoryResults=100`.
   - **L11**: 8 `fmt.Sscanf` JSON-substring-matching sites
     converted to `unmarshalJSON` + structured decode.
   - **L12**: `adif.parseRecords` error return dropped (dead
     path; caller check collapsed).

4. **Panic handling added** (post-review, user-initiated).
   - `cmd/smd/main.go`: `ExitError` / `ExitPanic` constants (ExitOK
     is implicit — Go's default on clean return). `main()` wraps
     `run()` with a `defer recover()` that prints a `PANIC:`-prefixed
     stderr line + `debug.Stack()` and exits `ExitPanic`. `run()`'s
     own defers (logger close, dbSvc close) still fire first as the
     panic unwinds through its frame.
   - `internal/api/middleware.go`: new `recoverPanic` middleware on
     `*Server`. Wraps the mux so any panic in a handler logs through
     `logging.Service` with panic value + stack + method + path, then
     writes a generic 500 `internal_error` envelope. The panic value
     is deliberately NOT surfaced to the client (could leak
     internals; full detail stays in the log).
   - Two regression tests (`TestRecoverPanic_CatchesAndReturns500`,
     `TestRecoverPanic_NoPanicPassesThrough`) — including a canary
     assertion that the panic message doesn't bleed into the
     response body.
   - Worker-goroutine recovery (`safeGo` helper) intentionally
     deferred until the forwarder PR spawns its first worker — the
     pattern template is noted here so the forwarder author can
     copy it from `recoverPanic`.

5. **`goccy/go-json` dropped from the dependency tree** (user pref).
   - Two adapter files (`internal/database/sqlite/adapters/model_to_type.go`
     and `type_to_model.go`) switched from `github.com/goccy/go-json`
     to stdlib `encoding/json`. Drop-in — `Marshal` / `Unmarshal`
     signatures are identical. `go mod tidy` removed the dependency
     from both `go.mod` and `go.sum`.
   - Rationale: at this daemon's scale (~146 QSO/s per stress test)
     the performance delta is below the noise floor; stdlib preference
     per CLAUDE.md; one fewer external dep to carry. The adapter's
     prior use of goccy was inherited from sqlboiler-generated
     idioms, not a deliberate choice.

### Coverage summary end-of-session

All tests green under `-race` after every finding. One new test
family:
- `TestSubmitQso_ConcurrentDuplicate` — H1 regression.
- `TestService_InitOpenCloseInitOpen` — M4 cycle regression.
- `TestCreateLogbook_BodyTooLarge`, `TestUpdateLogbook_BodyTooLarge`,
  `TestCreateLogbook_InvalidJSON` — M1 regressions.
- `TestLogbookCallsignByID` — new sqlite helper.
- `TestContactHistory_PortableSuffixMatches`,
  `TestContactHistory_CoincidentalPrefixExcluded` — L6 regressions.
- `TestRecoverPanic_CatchesAndReturns500`,
  `TestRecoverPanic_NoPanicPassesThrough` — panic-handling
  middleware (post-review).

### Design decisions made / reaffirmed

- **No daemon-side ADIF export endpoint.** Captured in
  `memory/project_sm_session_scope.md` as explicit "do not propose."
- **`qsoservice.Update` shape matches Submit** (tx envelope).
  Forwarder-hook placeholder inside the tx makes the later
  extension mechanical.
- **MaxBodyBytes is enforced on every handler that reads a body.**
  `readBody` / `readJSONBody` are the single enforcement point.
- **Contact-history prefix match is portable-only** (`X` OR
  `X/suffix`). The looser `LIKE X%` shape is gone.
- **`cmd/smd/main.go` follows the `run() error` pattern.**
  Cleanups are defers; startup failures unwind them in LIFO order.
- **Panic handling is two-layered.** Top-level `main` defer catches
  anything that escapes `run()` and exits with `ExitPanic` (2) so
  process supervisors can distinguish it from startup errors
  (`ExitError`, 1). A `recoverPanic` middleware on the HTTP mux
  catches handler panics, logs them structurally, and returns a
  generic 500 envelope (panic value stays server-side).
- **`encoding/json` is the only JSON library.** Dropped
  `goccy/go-json`. At this scale stdlib is fine and the "minimise
  external deps" rule wins over marginal throughput gains.

### Parked follow-ups

- SQL audit item 3 — dead-method sweep of the six sqlite methods
  with only test callers (`FetchContactedStationByCallsign`,
  `FetchCountryByCallsign`, `FetchCountryByName`,
  `FetchPendingUploads`, `UpdateQsoUploadStatus`,
  `FetchQsoSliceByLogbookId`, `FetchQsoCountByLogbookId`). The
  last two forwarder-queue methods will get real callers when the
  forwarder lands. The enrichment ones will get real callers in
  milestone 2.
- SQL audit item 4 — optional `(call, logbook_id) WHERE
  deleted_at IS NULL` composite for contact-history queries under
  `?logbook=` filter. Flagged under "wait for a concrete problem."

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

12. **`GET /v1/contact-history`** — "have I ever worked this call"
    lookup for the logging-app's draft panel. Required: `?call=`.
    Optional: `?logbook=` to restrict to a single logbook (default
    scope is all logbooks). Returns `{"items": [...]}` capped at
    `Server.MaxContactHistoryResults` (default 100). 10 tests
    including a **latent-bug fix** in the underlying sqlite query:
    the existing `Call = ? OR Call LIKE ?%` group was not
    parenthesised, so AND-ing additional predicates (logbook_id,
    the implicit `deleted_at IS NULL`) bound tighter than the OR
    and silently leaked rows. Wrapping the OR in `qm.Expr(...)`
    fixed it. The old code had the same issue but no test
    exercised it, so nothing caught the leak.

13. **`GET /v1/version`** — diagnostic. Returns
    `{"daemon":"<build>","go":"<runtime>","schema":{"version":N,"dirty":bool}}`.
    The daemon build version comes from `cmd/smd/main.go`'s
    package-level `Version` var, overridable via
    `go build -ldflags "-X main.Version=..."`. Schema version is
    pulled from `schema_migrations` (golang-migrate's table).
    `api.New` signature extended to accept `daemonVersion string`.

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
  PATCH/DELETE for edits, and formats the end-of-session email
  payload client-side from data it already has (or re-fetches via
  `GET /v1/qso/{id}`). Captured as a memory.
- **No daemon-side ADIF export endpoint.** Export is a
  client/admin concern, not a daemon concern. Clients that need
  ADIF page through the QSO list and serialize client-side using
  `internal/adif` (which is a regular Go library, not HTTP-wrapped).
  The "real" backup story is forwarding to online services
  (QRZ, LoTW, SM-online) — that's the daemon's redundancy
  mechanism. Filesystem backup of the sqlite file is a user/OS
  concern.

---

## Session 8 (2026-04-17, compressed)

Implemented milestone 1b step 1 (full logbook CRUD: list, get,
create, update, delete with FK-safe soft-delete). Tightened QSO
submit to require `?logbook=<id>` with existence + callsign-match
validation. Standardised `errors.Op` pattern across handlers
(`api.FuncName`). Added `IsValidCallsign` at three layers (schema
CHECK, handler, domain). Fixed `UpsertLogbookWithContext` latent
no-op-on-existing bug. Listener protocol made config-driven
(`unix` / `tcp`). 20-client × 50-QSO stress test green with
~146 QSO/s baseline. sqlite package coverage 0% → 66.9%.

Design decisions fixed in this session and carried forward:
logbooks are created explicitly (no auto-create); logbook
callsign is immutable after creation; workflow-driven milestone-1b
order (fetch/edit/delete before enrichment).

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

### The immediate next action (continue QRZ port)

The QRZ port is past the halfway mark. Stages 1–4 are committed
and live-validated; stages 5–8 remain. The full plan, with design
decisions already resolved, is below — do **not** re-derive.

**QRZ API reference** (from the operator's paste of QRZ's developer
guide — use this, not an inferred version):

- Endpoint: `https://logbook.qrz.com/api`, HTTP POST with
  `application/x-www-form-urlencoded`.
- User-Agent header required (≤128 chars, should include callsign
  + app name for identifiability).
- **INSERT**: `ACTION=INSERT`, `KEY=<apikey>`, `ADIF=<single-record>`.
  Response: `RESULT=OK|FAIL|REPLACE` + `LOGID` + `COUNT`.
- **UPDATE**: no native update — use `ACTION=INSERT` +
  `OPTION=REPLACE`. Response `RESULT=REPLACE` when it overwrote a
  duplicate. This is what v1 did.
- **DELETE**: `ACTION=DELETE`, `LOGIDS=<id>` (comma list for many).
  Response: `RESULT=OK|PARTIAL|FAIL` + `COUNT`.

**Resolved design decisions** (don't re-open):

- **`Forwarder.Submit` signature**: `(ctx, qso, action, priorUpstreamID string)`
  (stage 1). Worker populates `priorUpstreamID` from the prior
  insert row's `upstream_id` for delete actions only.
- **`Forwarder.AdifPrefix()`** (stage 1). QRZ returns `"QRZCOM"`.
  Worker stamps `QRZCOM_QSO_UPLOAD_STATUS="Y"` +
  `QRZCOM_QSO_UPLOAD_DATE=today` on success (insert/update, not
  delete — soft-deleted QSOs don't export). Failures/transients
  stamp nothing.
- **Delete LOGID wiring**: option A from the session-12 discussion.
  Worker does a DB lookup before `Submit`; forwarder receives LOGID
  via `priorUpstreamID`; empty lookup → terminal "no upstream id
  for delete".
- **QRZ credentials shape**: `{"api_key": "..."}` only — QRZ
  enforces the callsign/logbook match server-side, so a local
  `callsign` field would only introduce drift risk without a
  guarantee. (stage 2, landed)
- **QRZ response classification** (stage 3, landed): per-action
  matrix in `response.go` and `forwarding-implementation.md` §8.1.
  Short form: `RESULT=AUTH` → Terminal (global); `RESULT=OK` /
  `RESULT=REPLACE` → Success with `UpstreamID = LOGID`;
  `RESULT=FAIL` on delete → **Success** (idempotent);
  `RESULT=FAIL` elsewhere → Terminal; `RESULT=PARTIAL` / unknown
  on any action → Terminal; missing `LOGID` on claimed-OK insert →
  Terminal. Transport-level errors (HTTP 4xx/5xx, network, timeout)
  are classified at the `Submit` call site in stage 4 — network
  and 5xx/429 → Transient, 4xx → Terminal.
- **Retry-defaults ownership** (stage 7): each forwarder package
  exports `var DefaultRetry types.RetryConfig`.
  `spawnForwarderWorkers` in `cmd/smd/main.go` looks it up by type.
  Delete the `defaultForwarderRetry` temporary fallback.
- **Test creds**: operator has a QRZ test logbook with `USER` and
  API key in env vars. Used for manual integration verification
  after code lands — **not** for automated tests.
- **Automated tests**: `httptest.NewServer` everywhere, hermetic
  and CI-safe.

**Remaining stages** (each is a committable unit):

| # | Stage | Status |
|---|-------|--------|
| 1 | Extend `Forwarder` interface (`AdifPrefix`, `priorUpstreamID`) | **done** (session 12) |
| 2 | `internal/forwarding/qrz/` skeleton — credentials struct (`api_key` only), `New`, `Type()="qrz"`, `AdifPrefix()="QRZCOM"`, registry init, stubbed Submit, validation tests | **done** (session 13) |
| 3 | Response parser + classification function — `parseResponse` + `classifyResponse` with per-action helpers (`classifyInsert`/`Update`/`Delete`); `AUTH` global, single-LOGID-delete `FAIL` → Success; 26 unit tests | **done** (session 13) |
| 4 | Insert + update `Submit` — real HTTP, `buildForm` + `classifyHTTPStatus`, `DefaultEndpoint`/`DefaultHTTPTimeout`/`UserAgent`, package-internal `newWithEndpoint`; 18 httptest tests + live harness (`TestLive_InsertThenUpdate` quick, `TestLive_InteractiveFlow` with `/dev/tty` pauses); live-validated against real QRZ | **done** (session 13) |
| 5 | Delete `Submit` + worker LOGID lookup: new sqlite method `FetchInsertUpstreamIDWithContext(ctx, qsoID, forwarderName)`; worker gates delete on lookup result; QRZ delete uses `LOGIDS=priorUpstreamID` | pending |
| 6 | ADIF-stamp wiring: new sqlite `MarkUploadSuccessWithAdifStampWithContext(ctx, id, upstreamID, qsoID, adifPrefix)` that updates the `qso_upload` row AND stamps the QSO row's `<PREFIX>_QSO_UPLOAD_{STATUS,DATE}` in one tx; worker calls it when prefix non-empty and action != delete | pending |
| 7 | Retry-defaults ownership refactor (see above) | pending |
| 8 | Blank-import `_ "internal/forwarding/qrz"` in `cmd/smd/main.go`; session-handoff.md, forwarding.md, forwarding-implementation.md final updates | pending |

### Follow-ups after the QRZ port

1. **SSE event stream (`GET /v1/events`)**. The QRZ terminal
   transitions are the primary consumer (`forward.succeeded` /
   `forward.failed`), plus `qso.stored` / `qso.updated` /
   `qso.deleted` from ingest. Publish/subscribe fits single-
   operator scale: one in-memory channel per connected client,
   buffered, dropped on slow-reader. Worker code has comments
   marking the emit points.

2. **Bridge / CAT design**. Separate subsystem; see
   `project_sm_serial_bridge.md` memory.

### Parked follow-ups (low priority, not blockers)

- **Dead-method sweep (SQL audit item 3).** Several sqlite methods
  have only test callers today. The former forwarder-queue
  candidates (`FetchPendingUploads`, `UpdateQsoUploadStatus`) have
  already been deleted in session 11 — they were v1 worker code,
  replaced by the stage-6 purpose-built methods. The remaining
  low-signal methods
  (`FetchQsoSliceByLogbookId`, `FetchQsoCountByLogbookId`,
  `FetchQsoByDedupeKey`'s no-context wrapper,
  `FetchContactedStationByCallsign`, `FetchCountryByCallsign`,
  `FetchCountryByName`) still need a specific "delete or keep"
  decision. Enrichment methods likely return in milestone 2; the
  QSO list helpers may be dead. Park until we know.
- **SQL audit item 4** — optional `(call, logbook_id) WHERE
  deleted_at IS NULL` composite for contact-history with
  `?logbook=` filter. Defer until a concrete performance
  complaint surfaces.

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

- Compress session 9 after session 11 lands (session 8 is already
  compressed at end of session 10).
- Update this file at end of every session.
