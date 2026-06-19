# `internal/adif` - code review (2026-06-04)

## Scope

Read-only review of the `internal/adif` package, focused on data
loss, ADIF parser/emitter correctness, import/export round trips, and
performance on bulk imports.

Covered:

- `internal/adif/adif.go`
- `internal/adif/parse.go`
- `internal/adif/string.go`
- `internal/adif/consts.go`
- `internal/adif/doc.go`
- `internal/adif/*_test.go`

Context checked:

- QSO submit path: `internal/api/handler_qso.go`,
  `internal/qsoservice/submit.go`
- Import path: `cmd/smd/import.go`
- Session email export path: `internal/api/handler_session_email.go`
- Storage adapters: `internal/database/sqlite/adapters/*`
- ADIF-related DTOs in `internal/types`
- UUID ADIF contract in `docs/v2-design/api.md` and ADR 0016

No code changes were applied during this review.

## Headline verdict

The package is small and the main happy paths are tested, but there
are real round-trip gaps at the boundaries. The highest-impact issue is
identity loss: Station Manager emits `APP_SM_QSO_ID` but never restores
it, so a re-imported export gets new UUIDs despite the documented
contract saying the ADIF surface preserves canonical identity.

Headline counts: **0 Critical**, **2 High**, **2 Medium**, **1 Low**.

## High findings

### H1. `APP_SM_QSO_ID` is emitted but not restored

**Files:**

- `internal/adif/adif.go:104`
- `internal/adif/adif.go:122`
- `internal/qsoservice/submit.go:192`
- `docs/v2-design/api.md:692`

`QsoToRecord` writes `q.UUID` into `Record.AppSmQsoID`, and the API
docs describe `APP_SM_QSO_ID` as the ADIF field that lets exported
records preserve canonical identity across re-imports. `RecordToQso`,
however, never copies `rec.AppSmQsoID` into `types.Qso.UUID`.

The live submit path then unconditionally assigns `qso.UUID =
utils.NewUUIDv7()` before insert. That means a Station Manager export
imported into a new database loses the original QSO UUIDs and receives
fresh identities.

Why it matters:

- ADIF export advertises identity preservation but import does not honor it.
- Restore/migration and future inter-daemon sync cannot correlate the
  imported rows to the original records by UUID.
- The current behavior also makes the `APP_SM_QSO_ID` field one-way
  metadata: it is visible on the wire but not consumed by the system
  that produced it.

Suggested fix:

Define the trust boundary explicitly. The importer/restore path should
preserve a valid `APP_SM_QSO_ID` after UUIDv7 validation. Public
`POST /v1/qso` may need to keep daemon-generated UUIDs only, or accept
caller-supplied UUIDs only under a separate restore/import path to avoid
identity spoofing on the live submit endpoint.

Add regression tests for:

- `Parse` + `RecordToQso` preserves `APP_SM_QSO_ID` into `types.Qso.UUID`.
- `smd import` stores the source UUID when the record carries one.
- Public submit behavior for supplied `APP_SM_QSO_ID`, whichever policy
  is chosen.

### H2. `<EOR>` inside a field value corrupts parsing

**Files:**

- `internal/adif/parse.go:90`
- `internal/adif/string.go:90`
- `internal/adif/string.go:115`

`parseRecords` scans the raw body for `<EOR>` before parsing
length-delimited fields. The emitter writes field values as raw text,
with only a length prefix and no escaping. If a valid free-text value
contains the literal substring `<EOR>`, for example in `COMMENT`,
`NOTES`, or `QSLMSG`, the parser treats it as a record terminator
inside the field payload.

Example shape:

```adif
<CALL:5>M0CMC<COMMENT:20>contains <EOR> token<BAND:3>40m<EOR>
```

The ADIF length prefix says the `<EOR>` text belongs to `COMMENT`, but
the record splitter sees it first. The result can be a truncated record
plus a second partial record.

Why it matters:

- The package can emit a record it cannot parse back correctly.
- Operator-entered comments, notes, or QSL messages can corrupt imports
  or round trips if they happen to include ADIF-looking marker text.
- The same underlying issue applies to `<EOH>` if a header/body split is
  ever attempted on content that can contain it before the real marker.

Suggested fix:

Replace the marker-first record splitter with a length-aware scanner.
The scanner should parse tags, skip exactly the field payload byte
length, and only recognize `<EOR>` when scanning outside a field value.
This same scanner can also avoid the repeated lowercasing allocation
called out in M2.

Add regression tests for:

- `COMMENT` containing `<EOR>` round-trips as one record.
- Lowercase/mixed-case real `<EOR>` still terminates records.
- Malformed/incomplete fields behave according to the package's chosen
  tolerant or strict policy.

## Medium findings

### M1. `QSL_SENT_VIA` is lost on import/round-trip

**Files:**

- `internal/adif/adif.go:92`
- `internal/adif/adif.go:128`
- `internal/types/qsl.go:20`

`QsoToRecord` exports `types.Qsl.QslSendVia` into
`Record.QslSection.QslSentVia`, which emits `QSL_SENT_VIA`. The reverse
mapping in `RecordToQso` does not copy
`rec.QslSection.QslSentVia` back into `types.Qsl.QslSendVia`.

Why it matters:

- Imported ADIF records carrying `QSL_SENT_VIA` silently lose that
  routing metadata.
- A Station Manager QSO can emit `QSL_SENT_VIA`, but a parse/export
  round trip through `RecordToQso` drops it.

Suggested fix:

Add `QslSendVia: rec.QslSection.QslSentVia` in the `types.Qsl` literal
inside `RecordToQso`, with a focused round-trip test.

Related follow-up:

`types.Qsl` also contains `QslMsgRcvd`, `QslRcvdVia`, and
`QslRcvdNotes`; `QslSection` does not currently cover them. That may be
intentional, but the package comment already notes structural drift
between `QslSection` and `types.Qsl`. Audit whether those fields should
be represented before more QSL behavior is added.

### M2. Multi-record parsing repeatedly lowercases the remaining file

**Files:**

- `internal/adif/parse.go:90`
- `internal/adif/parse.go:228`

Every `parseRecords` loop calls `indexOfCaseInsensitive` on
`body[start:]`. That helper converts the entire remaining byte slice to
a string and lowercases it before searching for `<EOR>`.

For an N-record import, this repeatedly copies and lowercases nearly the
whole remaining ADIF file. The package comments document a 50k-QSO stress
case, which is exactly where this becomes expensive.

Why it matters:

- Bulk imports allocate far more than the input size.
- The hot-path optimization in `parseRecords` reduces record-slice
  allocation, but the marker search still does avoidable whole-suffix
  allocations.

Suggested fix:

The best fix is the length-aware scanner from H2. If that is split into
a smaller change, lowercase the body once and search index positions in
that normalized buffer, or use an ASCII case-insensitive byte search that
does not allocate a copy per record.

Add a benchmark for multi-record parse with enough records to catch
allocation regressions.

## Low findings

### L1. Malformed field lengths are silently truncated

**File:** `internal/adif/parse.go:130`

`parseFields` calculates `valEnd` as `min(valStart+n, len(b))`. If a
field declares a length longer than the remaining input, the parser
silently accepts the truncated value and returns no error.

Why it matters:

- The API/import callers cannot distinguish a valid partial value from a
  malformed truncated file.
- A corrupted ADIF download can import with silently damaged optional
  fields.

Suggested fix:

Keep the tolerant parser behavior only if it is an intentional product
choice, but expose the condition somehow: return warnings, add a strict
mode for import/API validation, or at least add tests documenting the
current truncation behavior. For user-triggered imports, strict failure
is usually better than silently persisting corrupted data.

## Verification

Commands run:

```sh
go test -count=1 ./internal/adif
go vet ./internal/adif
go test ./...
```

Results:

- `go test -count=1 ./internal/adif` passed.
- `go vet ./internal/adif` passed.
- `go test ./...` was attempted but failed in unrelated packages because
  the sandbox blocks local socket listeners used by `httptest` and fake
  SMTP servers. The failures were in packages such as `internal/api`,
  `internal/bridge`, `internal/email`, `internal/forwarding/qrz`,
  `internal/lookup/hamnut`, and `internal/lookup/qrz`, all reporting
  `socket: operation not permitted` from local listener creation.

## Resolution — Batch A (2026-06-04)

Validity was independently re-verified against the source before any change;
all five findings were confirmed (H2 was, if anything, under-stated on
reachability — COMMENT/NOTES are operator-settable free text). Fixes landed
the same day with full tests; `go test ./internal/adif ./internal/qsoservice
./cmd/smd ./internal/api`, `go vet`, and `gofmt` are clean.

- **H2 + M2 — fixed together.** `internal/adif/parse.go` rewritten as a single
  length-aware forward scan: it skips each field value by its declared byte
  length and recognises `<EOR>` / `<EOH>` only at tag boundaries, so a marker
  inside a value can no longer split a record, and the per-record whole-suffix
  lowercasing (`indexOfCaseInsensitive`) is gone. The `<EOH>` header/body split
  is length-aware too. New tests: `TestParse_EorInFieldValueRoundTrips`,
  `TestParse_EorInValueDoesNotSplit_Handcrafted`; `BenchmarkParse_ManyRecords`
  guards the allocation profile. All prior `Parse` tests still pass
  (behaviour-preserving).
- **M1 — fixed.** `RecordToQso` now restores `QslSendVia` from `QSL_SENT_VIA`.
- **H1 — fixed; trust boundary split by entry point.** `RecordToQso` restores
  `UUID` from `APP_SM_QSO_ID` (faithful inverse). `qsoservice.SubmitImport`
  (used by `smd import`) preserves a valid supplied UUIDv7; the public
  `qsoservice.Submit` (`POST /v1/qso`) always mints — never trusting a client
  UUID. Tests: `TestRecordToQso_RestoresUUIDAndQslSentVia`,
  `TestQsoRecordRoundTrip_PreservesUUIDAndQslSentVia`, `TestResolveSubmitUUID`.
- **L1 — characterized; strict mode deferred.** The scanner keeps the tolerant
  truncation but is the single point a future strict/warn mode would hook.
  `TestParse_OverlongFieldLengthTruncatesTolerantly` documents the current
  behaviour. Whether `smd import` should fail-strict on a truncated/corrupt
  file is an open product decision (not taken in Batch A).

The follow-up noted under M1 (`QslMsgRcvd` / `QslRcvdVia` / `QslRcvdNotes`
missing from `QslSection`) is left as a separate audit — out of Batch A scope.
