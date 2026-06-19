# internal/qsoservice code review - 2026-06-19

## Scope

Fresh review of `internal/qsoservice` at `3f4cd5a8`, approached as a new
package review. I read the service, package tests, API callers, database
transaction helpers, FT8 logging path, importer path, forwarding queue
contracts, and relevant docs.

Focus areas: correctness, performance, security, test coverage, and
documentation. This is review-only; no source fixes were made.

Primary files reviewed:

- `internal/qsoservice/service.go`
- `internal/qsoservice/submit.go`
- `internal/qsoservice/update.go`
- `internal/qsoservice/delete.go`
- `internal/qsoservice/dedupe.go`
- `internal/qsoservice/validation.go`
- `internal/qsoservice/forwarders.go`
- tests under `internal/qsoservice`
- API boundary in `internal/api/handler_qso.go`
- FT8 and importer callers in `cmd/smd/main.go`, `cmd/smd/import.go`, and
  `internal/ft8/qsolog.go`
- SQLite tx helpers in `internal/database/sqlite/api_context.go`
- edit-overlay client contract under `frontend/logging/src/lib`

## Summary

The service has a solid transactional core: submit, update, and delete all use
one transaction for the authoritative QSO mutation plus upload-queue fan-out;
update/delete also append `qso_history` in the same transaction. Dedupe conflict
races are handled by the database unique index and mapped back to domain errors.
Upload fan-out uses configured forwarder name/type and action filters, and the
forwarder worker has adjacent coverage for insert/update/delete queue behavior.

The main risks are boundary mismatches rather than the transaction mechanics.
Update validation is stricter than submit validation for FT8 reports, frequency
edits can leave `BAND` and `FREQ` inconsistent, and the logbook-callsign
invariant is enforced in the HTTP handler instead of the shared service entry
point. These are all reachable from current callers and explainable by coverage
gaps: most lifecycle tests live in `internal/api`, while `internal/qsoservice`
itself mostly tests pure helpers.

## Findings

### M1. FT8 QSOs with an intentionally empty report cannot be edited

**Area:** correctness / API behavior / FT8 integration  
**Files:** `internal/qsoservice/submit.go:203-215`,
`internal/qsoservice/update.go:129-134`,
`internal/api/ft8_qsolog_test.go:80-118`,
`frontend/logging/src/lib/states/qsoEdit.svelte.ts:134-135`,
`frontend/logging/src/lib/states/qsoEdit.svelte.ts:220-227`,
`frontend/logging/src/lib/validators/rst.ts:72-80`,
`docs/ft8.md:799-804`

`Submit` deliberately leaves FT8 reports empty when they are absent. The comment
at `submit.go:203-207` explains why: FT8 reports are signed dB SNR values, and
the caller-side bare-roger path can complete a valid QSO without an
`RST_RCVD`. The regression test at `internal/api/ft8_qsolog_test.go:80-118`
pins that behavior and expects `rst_rcvd == ""`.

`Update` then rejects the same stored row on any edit because it requires
`rst_sent` and `rst_rcvd` to be non-empty for every mode:

- `rst_sent cannot be empty` at `update.go:129-131`
- `rst_rcvd cannot be empty` at `update.go:132-134`

This is user-facing. The edit overlay populates empty report fields from the
daemon response (`qsoEdit.svelte.ts:134-135`) and always sends them in the PATCH
body (`qsoEdit.svelte.ts:220-227`). The FT8 validator explicitly allows an
empty signed signal report (`validators/rst.ts:72-80`), and the FT8 docs say the
overlay supports FT8 report editing without treating FT8 reports like phone/CW
RST (`docs/ft8.md:799-804`). A no-op edit or a comment edit on a bare-roger FT8
QSO therefore fails with a 400 even though the stored QSO was valid at submit
time.

**Recommendation:** make update validation mode-aware and align it with submit
semantics. For FT8 and other weak-signal modes that use signal reports, allow an
empty report when the stored/merged mode allows it; for phone/CW, keep the
non-empty requirement or defaulting policy. Add a regression at the API or
service boundary: create/log an FT8 QSO with empty `rst_rcvd`, PATCH only
`comment`, and expect 200 plus the empty report preserved.

### M2. Frequency edits can persist an impossible BAND/FREQ pair

**Area:** correctness / forwarding data quality / documentation  
**Files:** `internal/qsoservice/update.go:72-93`,
`internal/qsoservice/update.go:102-107`,
`internal/qsoservice/update.go:158-177`,
`frontend/logging/src/lib/ui/components/QsoEditOverlay.svelte:29-31`,
`frontend/logging/src/lib/ui/components/QsoEditOverlay.svelte:385-391`,
`frontend/logging/src/lib/states/qsoEdit.svelte.ts:220-232`,
`frontend/logging/src/lib/ui/components/QsoEditOverlay.svelte:253-259`,
`internal/api/handler_test.go:1186-1221`

The edit overlay documents that band is "derived from frequency by the daemon's
normalize step on PATCH" (`QsoEditOverlay.svelte:29-31`). The actual service does
not do that. `Update` trims and validates the merged `band` field
(`update.go:72-75`, `102-107`), separately parses/canonicalizes `freq`
(`update.go:87-93`), and then recomputes the dedupe key from whatever band is
present (`update.go:158-177`).

The UI path can send a stale band:

- VFO-A commit changes only `qsoEditState.freq` (`QsoEditOverlay.svelte:385-391`).
- `toPatchBody` still sends `band: this.band` (`qsoEdit.svelte.ts:220-232`).
- After a successful PATCH, the session row display is updated with a
  frontend-derived band (`QsoEditOverlay.svelte:253-259`), which can hide the
  persisted mismatch until a reload or forwarder upload.

Current tests miss the cross-band case. `TestUpdateQso_FreqChangeRecomputesDedupe`
changes `7.050` to `7.120`, which stays on 40m, and only asserts `freq` and
dedupe-key change (`handler_test.go:1186-1221`). A change from `7.050` to
`14.250` would be accepted as `freq=14.250, band=40m`, and the dedupe key would
include that wrong band. Forwarders would then send contradictory ADIF.

**Recommendation:** make `qsoservice.Update` own one source of truth for the
stored band. Either derive `band` from canonical `freq` during update, or reject
a mismatched band/freq pair. The submit path should be reviewed at the same time
because it also accepts caller-supplied `BAND` and `FREQ` independently. Add a
regression for a cross-band frequency edit and assert the stored response,
dedupe key, and upload payload use the derived/validated band consistently.

### M3. Direct submit callers bypass the API's logbook-callsign invariant

**Area:** correctness / forwarding compatibility / boundary ownership  
**Files:** `internal/api/handler_qso.go:197-230`,
`internal/qsoservice/submit.go:113-119`,
`internal/qsoservice/submit.go:185-195`,
`cmd/smd/main.go:486-538`,
`cmd/smd/import.go:172-180`,
`cmd/smd/import.go:222-228`,
`internal/api/handler_config.go:231-251`,
`internal/api/handler_logbook.go:115-118`,
`docs/v2-design/forwarding.md:184-189`

`POST /v1/qso` enforces that the target logbook exists and its callsign matches
`STATION_CALLSIGN` before calling the service (`handler_qso.go:197-230`). That
invariant is not owned by `qsoservice.Submit`: the service validates only that
`STATION_CALLSIGN` is present and syntactically valid (`submit.go:113-119`) and
then stores the QSO under the caller-supplied `logbookID` (`submit.go:185-195`).

That split is risky because not all production callers go through
`handleSubmitQso`:

- FT8 completed-QSO logging calls `qsoSvc.Submit` directly with
  `snap.DefaultLogbookID` and `snap.LoggingStation` (`cmd/smd/main.go:486-538`).
- `smd import` verifies only that the target logbook row exists
  (`cmd/smd/import.go:172-180`) and then calls `SubmitImport`
  (`cmd/smd/import.go:222-228`).

This can create behavior that differs by mode or entry point. The config setup
path seeds a default logbook callsign only on the first setup transition
(`handler_config.go:231-251`), while logbook PATCH ignores callsign changes
(`handler_logbook.go:115-118`). If station callsign and default logbook drift,
Phone/CW submit via HTTP rejects with `callsign_mismatch`, but FT8 direct submit
can still store the QSO under that logbook. Forwarders such as QRZ rely on this
callsign match and reject mismatched QSOs server-side (`forwarding.md:184-189`).

**Recommendation:** move the logbook existence and callsign check into the
shared service path, returning a `SubmitError` such as `logbook_not_found` or
`callsign_mismatch`. If import needs a different policy for historical/mixed
logs, make that explicit with a separate service method or option and document
the trust boundary. Add tests for FT8/direct-submit mismatch and importer
wrong-logbook mismatch so the API and non-API callers cannot diverge.

### L1. Service dependency failures are delayed until first QSO action

**Area:** correctness / operability  
**Files:** `internal/qsoservice/service.go:17-26`,
`internal/qsoservice/submit.go:299-307`,
`internal/qsoservice/submit.go:313-328`,
`internal/qsoservice/update.go:241-249`,
`internal/qsoservice/update.go:269-283`,
`internal/qsoservice/delete.go:61-69`,
`internal/qsoservice/delete.go:85-95`

`Service.Initialize` returns nil without checking injected dependencies
(`service.go:24-26`). The write paths then dereference `DB`, `Config`,
`Logger`, and `Hub` during QSO operations. For example, submit ranges
`s.Config.Forwarders()` (`submit.go:299`), logs through `s.Logger`
(`submit.go:313`), and publishes through `s.Hub` (`submit.go:325`).
Update/delete have the same shape.

Normal daemon wiring supplies all four dependencies, so this is not an observed
startup failure in the current tree. The issue is that a future DI tag change or
partial test harness can build successfully and then panic on the first QSO
submit/update/delete instead of failing during startup with a useful dependency
error.

**Recommendation:** make `Initialize` validate the four required dependencies
and return a structured error when any are nil. If nil `Config` is meant to be a
supported test mode, keep that explicit and make the forwarder fan-out helper
nil-safe rather than having only the MY_RIG stamp be nil-safe.

### L2. Update documentation still describes upload fan-out as future work

**Area:** documentation  
**File:** `internal/qsoservice/update.go:203-208`,
`internal/qsoservice/update.go:235-249`

The transaction comment says "No upload-queue rows are produced on edit today"
and describes forwarder rows as future work (`update.go:203-208`). The code now
does enqueue `action.Update` rows for configured forwarders inside the same
transaction (`update.go:235-249`).

**Recommendation:** update the comment so it describes the current one
transaction containing QSO update, update-action upload rows, and `qso_history`.

## Test Coverage Notes

Strong coverage observed:

- `internal/api` covers normal submit, duplicate handling, update, delete,
  history rows, upload queue rows, SSE lifecycle events, body limits, UUID
  routing, and several FT8 submit regressions.
- `internal/database/sqlite` covers transactional helpers, active-row update
  behavior, upload queue lifecycle, and migration/table coverage.
- `internal/forwarding/worker` covers the queue consumer side of insert/update
  and delete actions.
- `internal/ft8` covers QSO assembly and sequencer paths, and the API package
  has FT8 submit integration tests.

Coverage gaps worth adding:

- `internal/qsoservice` package-level integration tests for `Submit`,
  `SubmitImport`, `Update`, and `Delete` against in-memory SQLite. The current
  package tests mostly cover pure helpers: UUID policy, dedupe key, callsign
  validation, and forwarder action filtering.
- FT8 PATCH with empty `rst_rcvd` and/or `rst_sent`.
- Cross-band frequency edit, proving persisted `BAND` and `FREQ` stay
  consistent and dedupe uses the corrected band.
- Direct-submit logbook callsign mismatch for FT8 and importer callers, not only
  the HTTP handler.
- Dependency validation for a partially wired `qsoservice.Service`.

## Performance Notes

The service shape is reasonable for the daemon's expected write rate:

- Submit/update/delete use bounded transactions and do not perform network I/O
  inside the transaction.
- Duplicate handling uses a preflight dedupe lookup plus the unique index as the
  race backstop.
- Upload fan-out iterates over a defensive config snapshot, and zero forwarders
  are a no-op.
- The best-effort contacted-station upsert happens after the QSO commit, so a
  cache write failure does not roll back the logged QSO.

No high-confidence performance defects surfaced. The only latency note is that
the post-commit contacted-station upsert still runs before `Submit` returns; that
is acceptable for the current API/FT8 goroutine wiring, but worth remembering if
submit latency becomes visible on the operator path.

## Security Notes

No credential handling or direct external network I/O lives in this package.
Inputs are bounded by the API body limit before reaching `Update`, ADIF submit
parsing happens in the API/importer boundary, SQL lookups are parameterized, and
callsign validation rejects `%` and `_` to protect LIKE-based downstream queries.

Security-relevant residual risk is mostly data integrity: bad or divergent
callers can currently store QSO data that the public API would reject
(especially M2 and M3), and that can later be sent to forwarders.

## Documentation Notes

The package-level documentation is accurate about the core invariants:
enrichment does not block logging, authoritative QSO writes are transactional,
and rig/audio concerns stay outside this package.

Docs/comments needing attention:

- `docs/ft8.md` correctly describes mode-aware FT8 report editing, but the
  backend update path does not satisfy it for empty FT8 reports.
- `QsoEditOverlay.svelte` says band is daemon-derived on PATCH, but the daemon
  currently trusts the merged band.
- `update.go` still describes update upload rows as future work.
- `docs/install.md` says the importer inherits the live submission path's
  validation and atomic write behavior. That is broadly true for field
  validation and transactions, but not for the API handler's logbook-callsign
  check; the importer policy should be clarified once M3 is resolved.

## Verification

Commands run:

```sh
GOCACHE=/tmp/go-build go test ./internal/qsoservice
GOCACHE=/tmp/go-build go test -race ./internal/qsoservice
GOCACHE=/tmp/go-build go vet ./internal/qsoservice ./internal/api ./internal/database/sqlite ./internal/forwarding/worker ./cmd/smd
GOCACHE=/tmp/go-build go test ./internal/api ./internal/database/sqlite ./internal/forwarding/worker ./cmd/smd
GOCACHE=/tmp/go-build go test -race ./internal/api ./internal/database/sqlite ./internal/forwarding/worker ./cmd/smd
GOCACHE=/tmp/go-build go test ./internal/ft8
GOCACHE=/tmp/go-build go test -race ./internal/ft8
```

The sandboxed runs of listener-backed `internal/api` and `internal/ft8` tests
failed with `httptest: failed to listen on a port: socket: operation not
permitted`. Rerunning the same focused commands in an environment that permits
localhost listeners passed.

## Resolution (2026-06-19)

All five findings fixed. M3's import policy was an operator decision: enforce in
Submit, relax import (Option A).

- **M1 (fixed).** `Update`'s RST non-empty requirement is now mode-aware: FT8
  skips it (a caller-side bare-roger QSO legitimately has no `RST_RCVD`, and
  Submit already leaves FT8 reports empty), so a no-op or comment-only edit of a
  bare-roger FT8 QSO succeeds and preserves the empty report. Phone/CW keep the
  requirement. Test: `TestUpdate_FT8EmptyReportEditable`.
- **M2 (fixed).** `Update` derives `BAND` from the canonical `FREQ` whenever the
  freq maps to a known band, so the edit overlay's stale band (sent on a VFO freq
  change) can't persist an impossible BAND/FREQ pair or a wrong dedupe key. This
  also makes the overlay's "band derived from frequency by the daemon" comment
  true (no SPA change needed). An out-of-band freq leaves the supplied band for
  the validator to catch. Tests: `qsoservice.TestUpdate_FreqEditDerivesBand` +
  `api.TestUpdateQso_FreqEditCorrectsBand`; `api.TestUpdateQso_InvalidBand`
  updated (band validation is now reached only when the freq maps to no band).
- **M3 (fixed — Option A).** The logbook-exists + callsign-match invariant moved
  into the shared `submit()`: live `Submit` (HTTP + FT8 e4 sink) enforces both
  (returning `logbook_not_found` / `callsign_mismatch`), while `SubmitImport`
  relaxes the callsign match (historical/mixed logs may carry a different
  callsign) but still requires the logbook to exist. The HTTP handler dropped its
  duplicate pre-check and maps the `SubmitError` (logbook_not_found → 404, else
  400); the FT8 sink already logs Submit errors (fail-soft, QSO not stored).
  `docs/install.md` documents the import trust boundary. Test:
  `TestSubmit_EnforcesLogbookCallsign` (live match/mismatch, missing logbook,
  import relaxation).
- **L1 (fixed).** `Service.Initialize` validates DB/Config/Logger/Hub and returns
  an op-tagged error for any nil, so a partial wiring fails at startup, not on the
  first QSO action. Test: `TestInitialize_RequiresDependencies`.
- **L2 (fixed, docs).** `update.go`'s transaction comment now describes the
  current one transaction (QSO update + `action.Update` upload rows +
  `qso_history`) instead of "no upload rows on edit today."

Added the package-level integration harness the review flagged as missing
(`internal/qsoservice/integration_test.go`: in-memory SQLite + real
config/logging/hub), which hosts the M1/M2/M3/L1 tests.

Verified: `gofmt`/`go vet` clean; `internal/qsoservice`, `internal/api`,
`cmd/smd`, `internal/database/sqlite`, `internal/forwarding/worker` pass;
`go test -race ./internal/qsoservice ./internal/api` clean; CGO-free `go build
./...` and CGO `cmd/smd` build. The API error surface (POST /v1/qso codes) is
unchanged, so `api-endpoints.md` needs no edit.
