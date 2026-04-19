# Forwarding subsystem code review (session 13, 2026-04-19)

## Resolution status (2026-04-19, end-of-session 13)

All 13 findings triaged in the same session; 10 actionable items landed
in a single commit alongside this review, 3 accepted as-is with
rationale captured below.

### Actioned

- **M2** — `spawnForwarderWorkers` now threads a `*sync.WaitGroup`;
  `run()` calls `wg.Wait()` with a bounded timeout after
  `workerCancel()` + `server.Shutdown`, before the deferred
  `dbSvc.Close()` fires. Matches the E2E test harness shape.
- **M3** — `FetchInsertUpstreamIDWithContext` changed to
  `ORDER BY created_at DESC`; docstring updated to explain the
  semantic (newest insert row wins, stable ordering independent of
  retry bookkeeping).
- **M4** — Document-only: added invariant comments at
  `ClaimPendingUploadsWithContext` and `spawnForwarderWorkers`
  pinning "one worker per forwarder_name" and its enforcement point.
- **M5** — Swept the three sections of `forwarding-implementation.md`
  that referenced the deleted `defaultForwarderRetry`; now describe
  the per-package `DefaultRetry` + `RegisterDefaultRetry` shape.
- **M6 (partial)** — Added HTML-proxy-body test and multi-line
  `REASON` test in `qrz/submit_test.go`. Spawn-path coverage for
  `cmd/smd/main.go` parked as task #29 (separate effort).
- **L1** — Deleted `response.Fields` and the
  `TestParseResponse_ExtraFieldsPreserved` test.
- **L2** — Refactored: `action.Parse(row.Action)` now happens once at
  the top of `processRow`; `fetchQsoForAction` takes a typed
  `action.Action` and switches on the typed value.
- **L3** — Deleted `itoa`, `containsSubstring`, `indexOf` helpers in
  `worker_test.go`; imported `strconv` and `strings`.
- **L5** — Added `"QRZCOMé"` (multi-byte UTF-8) to the invalid-prefix
  test case slice.
- **L7** — Hardcoded contact callsign in `live_test.go` changed from
  `2E0TEST` to `W1AW/T` (ARRL HQ portable-temporary suffix).

### Accepted as-is

- **M1** — Worker wedge on DB-write failure is theoretical (requires
  a tx-commit failure or raw-sqlboiler-Update failure which SQLite
  makes vanishingly rare). The daemon-restart `ResetOrphanedUploadsWithContext`
  sweep is the documented safety net; adding in-loop fallbacks would
  be complexity for diminishing return.
- **L4** — Concurrent-safety docstring on `qrz.Forwarder` is slightly
  imprecise but not misleading. Editing for this is noise.
- **L6** — `bodySnippet` byte-boundary truncation is purely theoretical;
  QRZ responds in ASCII. If a future forwarder surfaces multi-byte
  bodies in error paths, the rune-boundary fix is three lines.

Full test suite green under `-race` after all ten fixes. The two
accepted items (M1, L6) each have an in-code comment pinning the
design decision so future readers don't re-relitigate them.

## Scope

Read-only review of the v2 forwarding subsystem after the 8-stage QRZ
port landed in session 13. Covered:

- `internal/forwarding/forwarding.go`, `registry.go`, `registry_test.go`
- `internal/forwarding/qrz/` — `qrz.go`, `response.go`, `qrz_test.go`,
  `response_test.go`, `submit_test.go`, `live_test.go`
- `internal/forwarding/stub/` — `stub.go`, `stub_test.go`
- `internal/forwarding/worker/` — `worker.go`, `backoff.go`,
  `worker_test.go`, `backoff_test.go`
- `internal/database/sqlite/api_context.go` — the new
  `FetchInsertUpstreamIDWithContext`,
  `MarkUploadSuccessWithAdifStampWithContext`, and the stage-5/6 test
  blocks in `service_test.go`
- `internal/database/sqlite/internal.go` — the `cache=shared` fix
- `cmd/smd/main.go` — `run()`, `spawnForwarderWorkers`, shutdown
  ordering
- `internal/adif/consts.go` — `ProgramVersion` const→var flip
- `docs/v2-design/forwarding.md` and `forwarding-implementation.md` —
  spot-checked against code behaviour

Deliberately **not** covered: the `internal/safego` package beyond
reading the one source file (session 10/11 territory); sqlboiler
generated models; the `apps/` tree and v1 branch; the unrelated
`qsoservice`/API handler changes from milestone 1b; and design
decisions already settled in the session-handoff "Resolved design
decisions" list.

## Summary

The subsystem is in **good shape**. The plugin boundary is cleanly
honoured (forwarders don't know about `qso_upload`; the worker doesn't
know upstream protocols), the one-fails-all-fail invariant is real
(stage 6's tx rolls back verifiably, and the stage-6 test
`TestMarkUploadSuccessWithAdifStamp_MissingQso_RollsBack` pins it),
and the outcome classification has a per-action matrix that actually
matches what QRZ documents. The test layers are deliberate: pure-
function tests for `computeBackoff` and `parseResponse`,
`httptest.NewServer` fixtures for transport classification, real-
sqlite worker integration tests, and a gated manual live harness that
round-trips a real QRZ logbook. None of that is performative — each
layer catches a class of bug the others can't.

Headline counts: **0 high**, **6 medium**, **7 low**. No correctness
bugs, no invariant violations, no credential leakage. The medium
items are operational / resilience concerns — workers can leave rows
wedged `in_progress` until restart if a terminal-transition DB write
fails, the daemon shutdown path doesn't wait for workers to drain
before closing the DB handle, and a couple of tests or docs have
drifted from the current code shape. The low items are tidy-ups
(duplicate action-parsing, unused `response.Fields` map, test helper
reimplementing `strconv.Itoa`).

Nothing in this review needs to block the next phase (SSE stream, or
the follow-on forwarders). The operator can triage M1–M4 together the
next time the subsystem gets touched; M5–M6 are documentation sweeps
that take minutes.

## M — Medium

### M1. Worker can wedge a row `in_progress` when a terminal-transition DB write fails

**File:** `internal/forwarding/worker/worker.go:324-342, 356-380`

Every `mark*` helper treats the DB call's error as a log-and-return.
If `MarkUploadSuccessWithAdifStampWithContext` fails at `tx.Commit()`
— or `MarkUploadFailedWithContext` fails on the underlying sqlboiler
Update — the row stays in `in_progress` until the next daemon restart
triggers `ResetOrphanedUploadsWithContext`. Until that restart, the
row is invisible to every worker tick (claim filters on status =
pending), and the operator can't see the upstream outcome via
`GET /v1/qso/:id/uploads` (status still reads `in_progress`, no
`last_error`, no `attempts` bump).

The stage-6 test `TestMarkUploadSuccessWithAdifStamp_MissingQso_RollsBack`
proves the tx rolls back, but it's only exercising the "qso missing"
path. A commit-failure path (disk full, WAL corruption) or an
sqlboiler Update failure in `MarkUploadFailedWithContext` would leave
the row wedged without any test catching it.

**Suggested direction:** On a mark-call failure, the worker currently
has only one safe action — log and drop — because retrying in-loop
could spam the DB, and there's no obvious transient/terminal
classification for "the DB itself is broken". Two reasonable shapes:
(a) on mark-success failure, fall back to `MarkUploadFailedWithContext`
with the DB error as `last_error`, so at least the row reaches a
terminal state visible to the operator; (b) accept the restart-only
recovery but add a periodic "stale in_progress" sweep (every N
minutes, reset rows whose `last_attempt_at` is > M seconds old). Not
urgent — the restart sweep is the safety net — but worth a design
decision before it bites.

### M2. Graceful shutdown doesn't wait for worker goroutines before closing the DB

**File:** `cmd/smd/main.go:183-228`

`run()` sets up `workerCtx` / `workerCancel`, spawns workers under
`safego.Go`, and on shutdown calls `workerCancel()` then
`server.Shutdown(ctx)` — but never waits for the workers to actually
exit. The deferred `dbSvc.Close()` at line 154 then fires as soon as
`run` returns. A worker mid-`processRow` (inside `FetchQsoByIdWithContext`,
say, or the `MarkUploadSuccessWithAdifStampWithContext` tx) can still
be holding the handle when Close is called.

This is survivable because `sql.DB.Close` returns an error rather
than panicking, but the worker's in-flight query fails with a cryptic
"database is closed" that gets logged as "mark success failed" — the
row is wedged per M1, and the operator sees a spurious error on every
shutdown.

The E2E test harness (`internal/api/handler_e2e_test.go:42-85`)
gets this right — it uses a `sync.WaitGroup` to join workers before
`dbSvc.Close` fires. The production daemon should match that shape.

**Suggested direction:** Thread a `*sync.WaitGroup` into
`spawnForwarderWorkers`, have each worker `Add(1)` before launching
and `Done()` after `Run` returns. In `run()`, after `workerCancel()`
and `server.Shutdown`, `wg.Wait()` with a bounded timeout before
returning. `safego.Go` currently has no return value — wrapping the
`Run` call in a closure that signals a channel before the safego
wrapper exits would work, though a WaitGroup-aware overload of
`safego.Go` would be cleaner.

### M3. `FetchInsertUpstreamIDWithContext` does not scope to the live QSO

**File:** `internal/database/sqlite/api_context.go:1528-1574`

The query filters on `qso_id = ? AND forwarder_name = ? AND action = 'insert' AND status = 'uploaded' AND upstream_id IS NOT NULL`, but
it does not filter on whether the QSO is still live (soft-delete
state) or whether the insert row has been superseded by a later
retry. In practice this is OK because the `UNIQUE(qso_id,
forwarder_name, action)` constraint makes the `ORDER BY modified_at
DESC LIMIT 1` defensive-only (the docstring even says so), and a
soft-deleted QSO still has a legitimate delete to forward.

But: if an operator hard-deletes a QSO (future admin op — FK CASCADE
drops the qso_upload rows) and then re-inserts the same dedupe-key
QSO at a new id, an unrelated historical row would never match
because the FK cascade took everything with it. That case is fine.
The case that is *not* tested is: what if the qso_upload table ever
gets a second insert row for the same (qso, forwarder) triple — the
docstring says "defensive `LIMIT 1`" but there's no test that
actually exercises the multi-row fallback. The unique constraint is
the guarantee; if it ever relaxes, the `ORDER BY modified_at DESC`
picks the most recently-modified row, which isn't necessarily the
most recently *uploaded* one (failed retries touch modified_at via
the trigger).

**Suggested direction:** Change the ORDER BY to `created_at DESC`
(or keep modified_at DESC but add a test asserting the most-recently-
uploaded row wins even if a later retry bumped modified_at without
changing status). Not an actual bug today, but a correctness trap
waiting for the constraint to be relaxed.

### M4. Parallel-writer assumption is baked into the claim query's docstring but not enforced

**File:** `internal/database/sqlite/api_context.go:1099-1118`,
`internal/forwarding/worker/worker.go:100-121`

The `ClaimPendingUploadsWithContext` docstring says "two workers for
different destinations never compete for a row." `cmd/smd/main.go`'s
`spawnForwarderWorkers` enforces one worker per `forwarder_name` by
iterating `cfg.Forwarders` once, but `types.ForwarderConfig.Name`
uniqueness is only validated by `config.Load` — the `worker.New`
constructor never checks for name collisions across the set, and
nothing in `forwarding.Register` tracks "which names have active
workers."

If a future refactor spawns two workers with the same
`forwarder_name` (say, a load-balancing experiment, or a test that
forgets to tear down), the claim query would still return rows but
both workers would process each row independently, producing
double-submits upstream. The claim is race-free per-row (single
writer, RETURNING), so neither worker sees the other's row — they
both see fresh rows from the same queue. That's the specific failure
mode: both claim disjoint subsets of the pending set and both submit
them.

**Suggested direction:** Have `worker.New` (or `spawnForwarderWorkers`)
maintain a package-level or service-level registry of active
forwarder names and refuse to spawn a second worker for an already-
active name. Alternatively, move the uniqueness check from config
validation into the spawn path so the guarantee is enforced at the
point of use rather than at config-parse time. Low risk today (config
validates uniqueness, tests respect it), but a `//TODO` comment at
the claim-query site would at least flag the invariant for anyone
tempted to experiment.

### M5. Docs reference `defaultForwarderRetry` as if it still exists

**File:** `docs/v2-design/forwarding-implementation.md:171,
393-398, 908-914`

Stage 7 deleted `defaultForwarderRetry` from `cmd/smd/main.go` and
replaced it with `forwarding.DefaultRetryFor(fc.Type)` — each
forwarder package registers its own retry default via
`RegisterDefaultRetry` in `init()`. But three sections of
`forwarding-implementation.md` still reference `defaultForwarderRetry`
as the current fallback:

- §4.1 — "When nil, the daemon falls back to `defaultForwarderRetry`
  (see §5 on why this is temporary)."
- §4.6 — "Retry defaults today live in `cmd/smd/main.go` as
  `defaultForwarderRetry = {MaxAttempts: 5, …}`."
- §9.12 — "Retry defaults are a temporary shape. `defaultForwarderRetry`
  in `cmd/smd/main.go` exists because…"

This is exactly the doc/code drift that milestone-1b-review H2
warned about — the tree and the handoff diverging. Someone coming to
the code via the implementation guide will grep for
`defaultForwarderRetry`, find nothing, and not know whether the doc
is wrong or the code is.

**Suggested direction:** Sweep those three sections to describe the
current `forwarding.RegisterDefaultRetry` / `DefaultRetryFor` shape
and delete the "temporary" framing. The session-handoff entry for
stage 7 already has the right description; it's just a copy-paste
from there into the implementation guide.

### M6. Test coverage gaps in areas that actually matter

**Files:** `internal/forwarding/qrz/submit_test.go`,
`internal/forwarding/worker/worker_test.go`

Spot-check of the test surface against the axes this review asked
about:

- **Partial HTTP body read / ctx cancellation mid-body-read** — not
  tested. The transport test closest is `TestSubmit_Insert_CtxCancelled_IsTransient`,
  which cancels ctx *before* Submit. An httptest server that hangs
  mid-body-write would exercise the `io.ReadAll` path at
  `qrz.go:216-223`. Low impact because the path is two lines, but
  it's the shape most likely to behave weirdly under a real flaky
  operator link.
- **Multi-line `REASON` field from QRZ** — not tested. QRZ has been
  observed to return `REASON=some message\nwith newlines` in error
  cases. `url.ParseQuery` handles it, `bodySnippet` truncates at 200
  chars after trimming whitespace, but no test pins the behaviour.
- **HTML body from a transparent proxy / CDN error page** — not
  tested. `parseResponse` on `<html>500 Internal</html>` would:
  `url.ParseQuery` accepts it as one key with empty value, missing
  RESULT → Terminal (good). Worth a one-line test to freeze the
  behaviour.
- **Commit-stage failure in `MarkUploadSuccessWithAdifStamp`** — not
  tested. `TestMarkUploadSuccessWithAdifStamp_MissingQso_RollsBack`
  covers the step-2 rowsAffected=0 path, but a test for
  `tx.Commit()` returning an error (SQL injection a closed DB
  handle, etc.) would catch any regression that ignores the commit
  error. Related to M1.
- **Concurrent-worker claim race on the same `forwarder_name`** —
  not tested. Probably not worth adding (per M4 the invariant lives
  outside the claim query), but if M4's uniqueness guard lands, it
  gets its own test.
- **Disabled-forwarder config path** — tested at ingest (the
  forwarders_test.go proves rows aren't enqueued for disabled
  forwarders) but not at worker-spawn (main.go's `if !fc.Enabled`
  skip). A small test against `spawnForwarderWorkers` with a
  disabled entry would pin the log line and the no-goroutine
  behaviour.
- **Unregistered forwarder type** — tested via
  `TestBuild_UnknownType` at the registry layer, but not via the
  startup path where `spawnForwarderWorkers` would surface a
  `"build forwarder"` error. Low priority — the error string is
  descriptive — but main.go has no test coverage at all right now.

**Suggested direction:** Pick the two that matter most — the
commit-failure rollback test (pairs with M1) and the HTML-proxy-body
test (freezes behaviour against a real-world failure mode QRZ's
infrastructure actually produces). The others are nice-to-haves.

## L — Low

### L1. `response.Fields` map is allocated but never read

**File:** `internal/forwarding/qrz/response.go:32-38, 61-80`

`parseResponse` builds a `Fields map[string]string` with every k=v
pair from the response body, stores it on the returned struct, and…
nothing ever reads `resp.Fields`. `classifyInsert`/`Update`/`Delete`
all read the typed fields directly (`resp.Result`, `resp.LogID`, etc.).
`TestParseResponse_ExtraFieldsPreserved` verifies the map is
populated but nothing in production consumes it.

Either use it (e.g. for richer `last_error` diagnostics — "QRZ
returned RESULT=FAIL with COUNT=0 and EXTRA=…") or delete it.
Right now it's dead surface area that a future reader will have to
learn is dead.

### L2. `fetchQsoForAction` duplicates the action string switch that `processRow` then does via `action.Parse`

**File:** `internal/forwarding/worker/worker.go:163-185, 227-272`

`fetchQsoForAction` switches on `row.Action` (as a raw string) to
decide soft-delete policy. Then `processRow` calls `action.Parse`
again on the same string to get a typed value. The two switches have
to agree on the set of valid actions — if one grows support for a
new action and the other doesn't, the worker silently does the wrong
thing.

One shape that avoids the duplication: parse the action once at the
top of `processRow`, pass the typed `action.Action` into
`fetchQsoForAction`. The soft-delete switch inside fetchQsoForAction
then becomes a strongly-typed switch rather than string compares
against `.String()`.

### L3. `worker/worker_test.go` reimplements `strconv.Itoa`

**File:** `internal/forwarding/worker/worker_test.go:253-273`

The `itoa` helper is a hand-rolled int64→string that exists so the
test file "doesn't pull strconv just for this." `strconv` is already
in the stdlib and is imported by plenty of other test files in the
tree. The hand-rolled version is ~20 lines of manual base-10 digit
extraction with a sign-flip trick that a reader has to verify before
moving on. Import `strconv`; delete the helper.

Same pattern at `containsSubstring` / `indexOf` (lines 876-887) —
`strings.Contains` is already in the stdlib; the avoidance saves one
import and adds two home-rolled functions that future refactors have
to understand.

### L4. `qrz.Forwarder` has nothing that would benefit from pointer receivers

**File:** `internal/forwarding/qrz/qrz.go:101-152`

The struct has three fields (`apiKey`, `endpoint`, `client`) — all
set at construction, none mutated. Pointer receivers are correct for
avoiding the copy, but the docstring ("Fields are set at construction
and read-only thereafter, so it's safe for concurrent use") claims
concurrent safety based on immutability — not on the receiver
choice. Nothing to change here; flagging it only because the comment
is stronger than it needs to be. The `http.Client` is the only
shared mutable state and it's goroutine-safe by its own contract.

### L5. `adifPrefixPattern` compilation is per-package-load, fine — but no test exercises the Unicode/emoji rejection explicitly

**File:** `internal/database/sqlite/api_context.go:1228`

The regex `^[A-Z][A-Z0-9]*$` rejects anything that isn't ASCII
uppercase/digit. Tests cover `qrzcom`, `QRZ com`, `QRZCOM!`, `1QRZ`,
`Q;DROP` (nice). What they don't cover: a UTF-8 multi-byte prefix
like `QRZCOMé` or an emoji. `regexp` by default runs in Unicode mode,
so the character class is ASCII-scoped — the anchoring `$` against a
multi-byte character would reject correctly. Not a bug, just an
untested edge case. One-line addition.

### L6. `bodySnippet` truncates on byte boundaries, not rune boundaries

**File:** `internal/forwarding/qrz/qrz.go:330-336`

`s[:max]` slices at byte 200, which can split a multi-byte UTF-8
sequence mid-codepoint. The "…" suffix then produces a last_error
message with a malformed rune in it. `last_error` is a TEXT column
with no validation, and zerolog handles the bytes by emitting the
replacement rune — not a crash, but potentially confusing in logs.

QRZ responds in ASCII so this is a theoretical concern. If it ever
matters, `utf8.RuneStart(s[max])` + back off to the preceding rune
boundary is a three-line fix.

### L7. `live_test.go` hardcodes `2E0TEST` as the contact callsign

**File:** `internal/forwarding/qrz/live_test.go:59-77`

`2E0TEST` isn't an FCC/Ofcom-assigned callsign. The QRZ side may
accept it (it's syntactically valid prefix + suffix shape), but an
operator running the live harness against a callsign-validating
upstream could get a spurious fail. `TEST` suffixes on UK 2E0 prefix
are effectively unused so conflict risk is low, but using a callsign
explicitly documented as "test" (`K6TEST`, `W1AW/T`, or a formally-
reserved contest callsign) would be more defensible. Not urgent —
the operator ran the live harness successfully in session 13.

## Positive observations

1. **The `AdifPrefix()` plugin-boundary decision is exactly right.**
   v1 had the forwarder package doing the QSO-row stamp directly,
   which tangled plugin code with storage layout. v2 makes
   `AdifPrefix` a declarative string and moves the write to the
   worker, where it shares a transaction with the `qso_upload`
   transition. The one-fails-all-fail invariant is upheld; forwarder
   packages stay pure; adding a new destination is five lines of
   metadata plus the classifier matrix. This is a genuinely clean
   resolution of an interface-design problem.
2. **The per-action classifier matrix actually matches QRZ's
   documented behaviour.** `classifyInsert` / `classifyUpdate` /
   `classifyDelete` treat `RESULT=FAIL` on delete as idempotent
   success, require `LOGID` on insert, accept `REPLACE` on update as
   the happy path. Each table cell has a test; the live harness
   validated the matrix against real QRZ. This is how classification
   should look — pure functions, explicit table, per-action
   specialisation rather than one clever generic path.
3. **The registry's panic-on-misuse contract is correct.**
   `Register` and `RegisterDefaultRetry` panic on empty names, nil
   constructors, duplicate registration, and invalid retry bounds.
   All four are bugs in the binary, not runtime conditions — panic
   surfaces them at startup before the first QSO touches the queue.
   Tests verify each panic. This is "fail loudly at startup" applied
   correctly.
4. **The `newWithEndpoint` package-internal constructor is the right
   test seam.** `httptest.NewServer` fixtures plug straight in; the
   production `New` wraps it with the real endpoint. No mock HTTP
   client, no interface over `*http.Client`, just an internal
   function that takes a URL. The integration-over-mocks lesson from
   v1 is applied faithfully.
5. **Stage 6's tx boundary is real and verified.**
   `MarkUploadSuccessWithAdifStampWithContext` wraps both updates in
   a single sqlite tx; the deferred `tx.Rollback()` is correct (it's
   a no-op after Commit); both error paths (qso_upload row missing,
   qso row missing) return `ErrNotFound` and the deferred rollback
   undoes any partial writes. `TestMarkUploadSuccessWithAdifStamp_MissingQso_RollsBack`
   specifically asserts the qso_upload row did not transition to
   `uploaded` when the qso stamp failed. This is one-fails-all-fail
   honoured in code AND pinned by test — the exact pattern the v1
   `LogQso` bug would have been fixed into.
