# Milestone 1b code review (session 10, 2026-04-17)

## Scope

Read-only review of the v2 daemon at the milestone-1 / 1b checkpoint, with CI green and the forwarder subsystem next on the roadmap. Covered: `cmd/smd/`, `internal/api/` (handlers, server, response envelope, handler + stress tests), `internal/qsoservice/` (Submit, Update, dedupe, validation), `internal/database/sqlite/` (service lifecycle, API methods, migrations), `internal/config/`, plus a scan of `internal/adif/`, `internal/types/`, `internal/utils/frequency.go`, and surface-level checks against `internal/errors` / `internal/logging` / `internal/iocdi` for regressions. Deliberately **not** covered: `internal/database/sqlite/models/` (sqlboiler-generated), the already-reviewed `internal/errors` and `internal/logging` packages beyond regression spotting, the `apps/` tree, `v1` branch, the forwarder subsystem (does not yet exist), and performance micro-optimizations — the session-9 SQL call-site audit already landed the main fixes and this review only looks for what remains.

## Summary

The daemon is in good shape for its age. The handler layer is small, the qsoservice contract is clean, and the convention adherence (errors.Op, types.X as DTO, no-magic-numbers) is high. Tests are substantial (handler_test.go alone is ~1100 lines) and exercise realistic paths; the stress test is a genuine integration exercise, not a unit-test-in-loop. Headline counts: **2 high**, **9 medium**, **12 low**. The high-severity items are (a) a real dedupe race under concurrent submit and (b) stale `FreqMHzToKHzString` still present as live code that session-9 notes claim was removed — indicating the tree and the handoff have drifted. Nothing blocks the forwarder work, but the dedupe race is worth plugging before any second ingest source (importer, UDP bridge) lands, and the doc/code drift is worth a sweep while context is fresh.

## High-severity findings

### H1. Concurrent submit can return 500 on a legitimate duplicate

**File:** `internal/qsoservice/submit.go:181-218`

**What's wrong:** The dedupe check (`FetchQsoByDedupeKeyWithContext`) is a separate read before the transaction that inserts the row. Two submits with identical dedupe-key inputs landing concurrently both see "not found", both begin their transactions, and both try to `InsertQsoTx`. The second loses the race on `uq_qso_logbook_dedupe` and `InsertQsoTx` returns a plain `errors.New(op).WithErr(err)` (line 203). Because it is not a `*SubmitError`, the handler's `IsSubmitError` check fails and the response is `500 submit_failed` — not the expected `200 duplicate`.

**Why it matters:** This contradicts "nothing blocks logging a QSO" semantics from the operator's point of view. The dedupe-key promise is "an idempotent submit path that clients can safely replay." The reconciliation flow described in `docs/v1-analysis/invariants.md` ("Nothing blocks logging a QSO" → reconciliation flow) relies on this exact idempotency; a text-file-fallback replay that happens concurrently with a forward-queue retry would hit this race and surface as an error to the operator. The stress test doesn't catch it because each QSO is written with a unique minute, so no two workers collide on the same dedupe key.

**Suggested direction:** In the insert-error path, translate the sqlite unique-constraint error into the duplicate outcome. The cleanest shape is a follow-up SELECT-by-dedupe-key after catching the UNIQUE error — if the row is there, return `SubmitResult{Status:"duplicate", ID: existing.ID}`; if not, return a real 500. Equivalent alternatives: rely on sqlite's `RETURNING` with an `ON CONFLICT DO NOTHING` upsert, or detect the error via the sqlite-driver-specific constraint code. Whichever you pick, write a deterministic concurrency test (two goroutines, same inputs, `sync.WaitGroup`, assert both responses are either "stored"/"duplicate" with matching IDs).

### H2. `qsoservice.FreqMHzToKHzString` is live code claiming to be removed

**File:** `internal/qsoservice/submit.go:238-257`, referenced at `internal/qsoservice/dedupe.go:13`

**What's wrong:** `docs/session-handoff.md:101` says "The old `qsoservice.FreqMHzToKHzString` helper was removed", and line 252 again "qsoservice.FreqMHzToKHzString removed". The function is still in the tree — unchanged — and `dedupe.go`'s doc comment still names it as the normalization source. No production caller exists (callers in `submit.go` and `update.go` both use `utils.ParseFreqMHz` / `strconv.FormatInt`), but the function is exported so a future caller could re-grab it, defeating the intent of centralising the MHz↔kHz bridge in `internal/utils`. Also uses `fmt.Errorf` (line 246, 253) in a package that otherwise uses `internal/errors` — violates the convention in `feedback_error_op_convention`.

**Why it matters:** Doc/code drift at this scale is a signal that the session handoff and the tree are out of sync. When the forwarder work starts and someone goes looking for "the kHz converter", they may land on `qsoservice.FreqMHzToKHzString` (because it's the name the older handoff entries use) rather than `utils.ParseFreqMHz`. That reintroduces the leaking-storage-unit bug the session-9 fix set out to stop.

**Suggested direction:** Delete the function and the comment reference in `dedupe.go`. Confirm no test imports it (a quick `grep` says none does). If something does want a helper wrapping `ParseFreqMHz` for a qsoservice caller, inline it locally where it's used.

## Medium-severity findings

### M1. Logbook POST / PATCH have no body-size cap

**File:** `internal/api/handler_logbook.go:58, 122`

**What's wrong:** The two JSON handlers use `json.NewDecoder(r.Body).Decode(&req)` with no `http.MaxBytesReader` in front of the body. QSO submit and update do use `MaxBytesReader(..., s.maxBodyBytes)`. A malicious or broken client could stream unbounded JSON into either logbook endpoint.

**Why it matters:** The attack is mild for a Unix-socket-only deployment (the abuser is already local), but it's free inconsistency: `Server.MaxBodyBytes` is configured precisely to cap request sizes, and only half the handlers honour it. This will be a real concern the moment the TCP protocol path (already supported via `ServerConfig.Protocol = "tcp"`) gets used.

**Suggested direction:** Wrap the body in `MaxBytesReader` in both logbook handlers, following the same pattern as `handleSubmitQso`. Consider extracting the read-capped-body helper (the `MaxBytesReader` + deferred Close + "body too large" error mapping) into `internal/api` so handlers don't each re-implement it.

### M2. `err.Error() == "http: request body too large"` is a fragile stdlib-string match

**File:** `internal/api/handler_qso.go:86, 136`

**What's wrong:** Both the submit and the update handlers detect "body too large" by string-comparing `err.Error()`. Since Go 1.19 the stdlib has exported `*http.MaxBytesError` which can be matched via `errors.As`. If the stdlib rewrites that exact message string (has happened in patch releases), the body-too-large path silently becomes a generic 400 `read_error`.

**Why it matters:** Fragile — will break silently on some future Go upgrade.

**Suggested direction:** Replace with `var maxBytesErr *http.MaxBytesError; if stderr.As(err, &maxBytesErr) { ... }`. Extract the pattern into the body-read helper proposed in M1 so only one place has to know.

### M3. Migration `dedupe_key` comment says CALL|BAND|MODE|QSO_DATE|TIME_ON, missing FREQ

**File:** `internal/database/sqlite/migrations/0001_init.up.sql:53-55`; same drift in `internal/types/qso.go:10-14`

**What's wrong:** Session 9 expanded the dedupe-key inputs to include FREQ (see `docs/session-handoff.md:47-56`), but the SQL comment in the migration and the `types.Qso.DedupeKey` doc comment were not updated. Anyone reading the schema or the type will believe the key is time-coarse when it's actually freq-discriminating.

**Why it matters:** Schema comments are near-impossible to fix cleanly after a `0001_init` migration has been applied in production (they'd need a whole new migration or a doc-only update that can't reach existing databases). Better to get this right before 1.0 when the schema is still rewritable. The type comment is cheap to fix.

**Suggested direction:** Rewrite both comments to name all six inputs: `CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON`. The SQL comment can be updated in-place since `migrations/0001_init.up.sql` is not yet applied to any shipping database.

### M4. `sqlite.Service.Close` doesn't reset `initOnce`; re-init after Close is silently no-op

**File:** `internal/database/sqlite/service.go:156-182`, `37-85`

**What's wrong:** `Initialize()` is guarded by `s.initOnce.Do(...)`. After `Close()`, calling `Initialize()` again returns `nil` immediately (the `s.isInitialized.Load()` pre-check) but actually does nothing. If `Close` is followed by a config-reload attempt that wants to re-prepare the service, the config re-read is silently skipped. `s.isInitialized` itself stays true across Close — which is semantically wrong (the service is no longer initialised against any live handle) but happens to be safe because `getOpenHandle` separately checks `isOpen`.

**Why it matters:** Violates the documented "all lifecycle methods idempotent" contract in CLAUDE.md, and the service-lifecycle expectation in `structure.md`. For milestone 1 this doesn't bite because the daemon is shut down by process exit, but a SIGHUP-style reload (deferred per api.md §3) would land here.

**Suggested direction:** In `Close`, reset `s.initOnce = sync.Once{}` and `s.isInitialized.Store(false)` so `Initialize()` actually re-executes on the next call. Write a test that asserts Initialize → Open → Close → Initialize → Open cycles through successfully with a fresh config.

### M5. `QueryContext` drops the cancel function from `withDefaultTimeout` — context leak

**File:** `internal/database/sqlite/service.go:315-318`

**What's wrong:**
```go
_, ok := ctx.Deadline()
if !ok {
    ctx, _ = s.withDefaultTimeout(ctx)
}
```
`withDefaultTimeout` returns a `(context.Context, context.CancelFunc)` pair; the cancel is thrown away. Rows-based query APIs need the context alive while the caller iterates, so a naive defer-cancel is wrong — but discarding the cancel entirely means the context's resources live until the timeout fires instead of being released when iteration ends. Go's `context` vet warning flags this exact pattern.

**Why it matters:** Every `QueryContext` call that didn't already have a deadline leaks a context goroutine until the timeout expires. At daemon scale this is survivable; at stress-test scale it accumulates fast enough to be visible under `-race`. Also, `QueryContext` has **no production callers** I found — the handler layer goes through sqlboiler model methods. If it's dead code, deleting it is the cleanest answer.

**Suggested direction:** Either (a) delete `QueryContext` and `ExecContext` if nothing uses them (the dead-method sweep in session-handoff:581 calls this out), or (b) have them return the cancel function to the caller alongside `*sql.Rows` so the caller can cancel when done iterating. Matching comment in the adjacent `ExecContext` is redundant too: "Holding s.mu.RLock() while performing s.handle.ExecContext(...)" — copy-pasted from the exec description into Query, narrating what the obvious code does.

### M6. `Update` is not transactional even though Submit is

**File:** `internal/qsoservice/update.go:180-183`, compare `internal/qsoservice/submit.go:193-218`

**What's wrong:** `Submit` wraps the QSO insert and its upload-queue rows in a `BeginTxContext` transaction per the "one-fails-all-fail" invariant. `Update` calls `s.DB.UpdateQsoWithContext(ctx, merged)` directly, no transaction. Today that's fine because no upload-queue rows are produced on edit. When the forwarder lands and edits are expected to re-queue `action.Update` uploads (per `docs/v1-analysis/bug-inventory.md` → "v1 hardcoded QRZ" and the upload-queue action enum), the invariant will apply to updates and this code will violate it.

**Why it matters:** It's about to matter. Catching this now is cheap — it's a two-line change to swap `UpdateQsoWithContext` for `UpdateQsoTx` in a `BeginTxContext` / Commit envelope. Catching it after the forwarder lands is where the v1 `LogQso` bug came from.

**Suggested direction:** Proactively convert `Update` to the same tx shape as `Submit` so there's one obvious place to add the queue insert when the forwarder arrives. If you'd rather not change the shape until the queue rows have a reason to exist, leave a TODO comment at the write site naming the invariant so the forwarder PR remembers to close the gap.

### M7. Eight `fmt.Errorf` usages in packages that use `internal/errors`

**Files:** `internal/database/sqlite/internal.go:61, 73, 126, 133, 158, 166`; `internal/database/sqlite/service.go:258, 290, 322`; `internal/database/sqlite/migrations.go:24, 29, 49, 61`

**What's wrong:** The convention captured in `feedback_error_op_convention` and CLAUDE.md explicitly says no `fmt.Errorf` in packages that use `internal/errors`. The sqlite package uses `internal/errors` pervasively and should use `errors.New(op).WithErr(err).WithMsgf(...)` (or `WithErr(fmt.Errorf("...: %w", err))` where a wrap-with-format is needed per the guidance). Several of these are literally `errors.New(op).WithErr(fmt.Errorf("label: %w", err))` — the `fmt.Errorf` wrapping adds no information that `WithMsg("label")` wouldn't and burns an extra allocation per call.

**Why it matters:** Convention drift — the sqlite package is the single largest contributor of `fmt.Errorf` wraps in the daemon. This is a pattern that should either get a single cleanup pass now or become the norm everywhere else; leaving it half-applied is the worst option.

**Suggested direction:** Sweep all thirteen call sites to the canonical `errors.New(op).WithErr(err).WithMsg("label")` form. The only legitimate use of `fmt.Errorf` inside an `errors.New(op).WithErr(...)` chain is when you genuinely need `%w`-wrapped formatting below the tagged error, and none of the thirteen cases here meet that bar.

### M8. `uploadRetryCooldown` and `defaultUploadBatchLimit` are code constants, not config

**File:** `internal/database/sqlite/consts.go:31-40`

**What's wrong:** Both values are hardcoded constants. `defaultUploadBatchLimit` is at least documented as a fallback default when `QsoForwardingRowLimit` is zero, which is OK per the no-magic-numbers rule. `uploadRetryCooldown = 5 * time.Minute` has no config equivalent and is consulted from `FetchPendingUploadsWithContext:1126` — it controls the forwarder's retry cadence.

**Why it matters:** The forwarder subsystem is the next thing being built, and the `feedback_no_magic_numbers` note specifically calls out retry counts and intervals as examples of what must come from config. Landing the forwarder with a hardcoded 5-minute cooldown means every operator who wants a different cadence has to rebuild the daemon.

**Suggested direction:** When the forwarder subsystem gets its `docs/v2-design/forwarding.md`, add a `RetryCooldownSec` field to the forwarder's config struct (or to `RequiredConfigs` if that's where the forwarder sits) with the 5-minute default as a code fallback. `uploadRetryCooldown` in `consts.go` becomes the fallback constant the config field falls back to.

### M9. Handler tests assert on human-readable English message substrings

**File:** `internal/api/handler_test.go:645, 672`; similar patterns at 790, 1007

**What's wrong:** Examples:
```go
if !strings.Contains(w.Body.String(), "TIME_ON is after TIME_OFF") { ... }
if !strings.Contains(w.Body.String(), "must be the day after") { ... }
```
These are matching against the `message` field of the error envelope — the human-readable detail. `api.md` §4.6 explicitly says clients must not string-match on `message`, only on `code`. The tests violate the same rule they're protecting.

**Why it matters:** Any reword of the error message ("QSO_DATE_OFF must follow QSO_DATE when …") breaks these tests without any behaviour change. The stable signal (the error `code`, e.g. `invalid_time_range`) is already carried by the envelope and is what the tests should assert on.

**Suggested direction:** Decode the `ErrorResponse` JSON and assert on `.Code == "invalid_time_range"`. Also line 322 is a negative "should NOT contain M0CMC" assertion which relies on the current response body not containing that string anywhere incidentally — still fragile.

## Low-severity / nit findings

### L1. `api/response.go:20` ignores `json.NewEncoder.Encode` error after `WriteHeader`

**File:** `internal/api/response.go:17-21`

Status is already written so a late encode failure can't be signalled to the client, but the error is silently dropped with `_ =`. At minimum log it via `s.logger` if the server were accessible. Not worth re-plumbing for, but worth noting.

### L2. ListenAndServe does not remove the Unix socket on shutdown

**File:** `internal/api/server.go:86-114`

`ListenAndServe` removes a stale socket before binding, but `Shutdown()` and main.go's cleanup path don't remove the socket after a clean shutdown. The next startup's stale-socket cleanup covers it, but an orphaned socket file between runs is confusing for operators grepping `/tmp` for daemon state. Consider an `os.Remove` in a `Close` step on the server, conditioned on `s.protocol == "unix"`.

### L3. `cmd/smd/main.go:140` logs after `loggerSvc.Close()`

**File:** `cmd/smd/main.go:136-140`

```go
if err = loggerSvc.Close(); err != nil { ... }
loggerSvc.InfoWith().Msg("smd stopped")
```
The `InfoWith()` call after Close may be a no-op (depending on how the logging package handles post-Close events — the logging review from session 5 addressed this; worth a quick sanity check). Either move the "smd stopped" log above the Close, or switch it to a plain `fmt.Fprintln(os.Stderr, ...)`.

### L4. `fatal()` helper tears down the process without Closing DB or logger

**File:** `cmd/smd/main.go:165-168`

If `dbSvc.Migrate()` fails after `dbSvc.Open()` succeeded, `fatal("run migrations", err)` calls `os.Exit(1)` with the database still open. The OS cleans up the handle, so no real harm, but the "service lifecycle is Open → Close" contract is broken in the failure path. Minor.

### L5. `SchemaVersionWithContext` assumes `schema_migrations` has ≤1 row

**File:** `internal/database/sqlite/api_context.go:219-226`

The query uses `LIMIT 1` so it's safe against duplicate rows; however, `golang-migrate/migrate`'s `schema_migrations` table is expected to have exactly one row, and `sql.ErrNoRows` is treated as "fresh database, version=0, dirty=false" which is correct. Worth a one-line comment that `LIMIT 1` is defensive, not a semantic choice.

### L6. `FetchQsoSliceByCallsignWithContext` LIKE-wildcard hazard

**File:** `internal/database/sqlite/api_context.go:88-90`, already flagged in session-handoff:571

The `Call LIKE ?%` pattern, combined with `qsoservice.IsValidCallsign` not rejecting `%` or `_`, means a malformed callsign like `M0%` would silently wildcard the LIKE. Risk is real-zero (valid callsigns don't contain these chars, and the value is parametrized so no injection) but the code wouldn't notice if it ever did. The wider issue `M0CMC` matching `M0CMCE` is already on the known-issues list.

### L7. `missingCoreTables` doesn't check `rows.Err()` after the loop

**File:** `internal/database/sqlite/internal.go:156-178`

Standard Go SQL idiom is to check `rows.Err()` after the `for rows.Next()` loop to catch iteration errors. The current code only checks per-row scan errors. For a local sqlite read this is unlikely to matter, but it's a gotcha for anyone porting this pattern. The `defer rows.Close()` is correct.

### L8. `validTestQso` uses `Freq: "7050"` — the legacy int-kHz form

**File:** `internal/database/sqlite/service_test.go:70`

Comment says `// kHz`. Post-session-9 the canonical external form is MHz decimal; `ParseFreqMHz("7050")` tolerates the bare integer form and treats it as kHz, so the test still passes, but the test data is no longer canonical. Replace with `"7.050"` to match the ADIF-follows-spec invariant in test fixtures.

### L9. `sqlite/doc.go:8-10` says Open runs migrations; it doesn't

**File:** `internal/database/sqlite/doc.go:6-10`

"Open connects to sqlite, sets PRAGMAs (foreign_keys, WAL, busy_timeout), pings, and runs migrations." Open does not run migrations — `Migrate()` is a separate method and `cmd/smd/main.go:93` calls it explicitly after `Open`. Minor doc drift.

### L10. `config_test.go` doesn't cover the pagination / contact-history defaults

**File:** `internal/config/config_test.go:72-117`

`TestLoad_DefaultsApplied` checks `ReadTimeoutSec`, `ShutdownTimeoutSec`, `MaxBodyBytes`, datastore defaults, and logging defaults — but not `DefaultPageLimit`, `MaxPageLimit`, or `MaxContactHistoryResults`. These were added in session 9; a default getting accidentally changed to 0 would silently break list responses without a failing test.

### L11. Handler tests parse JSON responses via `fmt.Sscanf` on literal substrings

**File:** `internal/api/handler_test.go:86, 702, 763`, `stress_test.go:96`

Every test that fetches the QSO id does:
```go
_, _ = fmt.Sscanf(w.Body.String(), `{"status":"stored","id":%d}`, &qsoID)
```
This hard-codes both the field order and the exact outer shape. If `writeJSON` ever gains an envelope field or changes encoder settings (e.g. `HTMLEscape`), the Sscanf silently returns 0 and downstream code sees `qsoID=0` (which then hits the `< 1` check). Not a bug today, but an easy upgrade to `json.Unmarshal` into a struct.

### L12. `parseRecords` in `internal/adif/parse.go` always returns nil error

**File:** `internal/adif/parse.go:73-103`

`parseRecords` returns `(records, nil)` in every code path; the error return is unreachable. Caller at `Parse` checks it anyway. Either drop the error return from `parseRecords` or make one path actually produce an error (e.g. tag with length exceeding buffer). Dead error-return paths lie to readers about what can fail.

## Positive observations

1. **The `types.Qso` + `json.Unmarshal` overlay pattern on PATCH is the right shape.** `qsoservice/update.go` does the canonical stash-restore-overlay dance described in `feedback_types_canonical_dto`. Immutable fields are listed explicitly; the editable set is implicit (everything else). Adding an ADIF field to `types.Qso` is one line. This is what the v1 `QsoAdditionalData` bug was supposed to be fixed into, and it is.
2. **Dedupe key idempotency is real.** The submit path round-trips a hash, the unique index is partial-on-`deleted_at IS NULL` so soft-delete frees the slot, and `TestDeleteQso_FreesDedupeKey` locks the behaviour. This is a mature, tested implementation of §4.2 of api.md.
3. **Integration tests over mocks is applied consistently.** Every handler test and every sqlite test uses `&sqlite.Service{}` with an in-memory DB. No mock interfaces were spotted for internal collaborators. Matches the "Integration tests over mocks" lesson exactly.
4. **Stress test is a real round trip, not a latency benchmark.** 20 clients × 50 QSOs × (submit → fetch → PATCH-freq → DELETE → verify-404) under `-race` — this exercises the whole pipeline including dedupe-key recompute and soft-delete filtering. The errCount-per-phase reporting makes failure localisable.
5. **Config surface is faithful to `no-magic-numbers`.** `ServerConfig` covers socket protocol, every timeout, the body cap, pagination limits, contact-history cap. Constants in code are fallbacks used only when config is zero. Good discipline.
6. **`LogbookExistsByID` / `LogbookCallsignByID` are lightweight probes, not full rows.** The handlers call the right one per use case (submit wants the callsign for comparison; list/contest-dupe/contact-history only want a boolean). That's the outcome of the session-9 SQL audit and it looks clean from this angle.
7. **Error envelope stability.** Every 4xx/5xx goes through `writeError(w, status, code, message, op)` with consistent shape and the `op` field carrying the `errors.Op` tag through to the client. No ad-hoc error JSON escapes the handler layer.
8. **Handler layer is genuinely thin.** `handler_qso.go`, `handler_logbook.go`, etc. do only transport concerns (param parsing, body reading, Content-Type checks, status mapping) and delegate everything domain-shaped to `qsoservice` or `sqlite`. The "business rules don't belong here" intent in `internal/api/doc.go` is actually being honoured.

## Notes and out-of-scope

- **`internal/errors` and `internal/logging`:** regression-checked only, per the review brief (sessions 5–6 did full reviews). No regressions spotted — usages across the daemon follow the pattern those reviews decided.
- **`internal/database/sqlite/models/`:** skipped entirely (sqlboiler-generated, CLAUDE.md treats as read-only).
- **Adapter test data values:** the out-of-scope carve-out was respected; I noted only that `validTestQso` in `service_test.go:70` uses the legacy kHz string form (L8), not anything inside the adapters' own tests.
- **Dead sqlite methods (the six already flagged in the session-9 audit):** not re-flagged here. Separately, `ContactedStation` and `Country` method families are also test-only today but they are expected milestone-2 enrichment consumers — noted but not listed as findings because their path forward is known.
- **Forwarder subsystem:** does not exist yet; findings that touch its shape (M6, M8) are framed as "get the shape right before the forwarder lands", not as current bugs.
- **Performance beyond what session 9's audit already addressed:** nothing new surfaced. The pagination composite index (`idx_qso_logbook_date_time`), the lightweight existence/callsign helpers, and the PRAGMA settings in `Open` are all in place.
- **Contact-history callsign match broadening (`M0CMC` matches `M0CMCE`):** already flagged in session-handoff.md:570 as a deliberate deferral. Not re-flagged here.
- **Test coverage gaps:** did not attempt a coverage-percentage audit; the tests I read cover the happy paths, most error paths, pagination walks, soft-delete semantics, and concurrency at the submit layer. What's missing in quality terms (assertion style, envelope parsing) is captured in M9 / L11.
