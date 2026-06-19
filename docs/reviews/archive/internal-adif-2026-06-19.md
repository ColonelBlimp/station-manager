# `internal/adif` - code review (2026-06-19)

## Scope

Thorough review of `internal/adif`, focused on correctness, performance,
security, and documentation. I approached the package from the current tree,
then checked the callers that make the ADIF boundary load-bearing.

Covered:

- `internal/adif/adif.go`
- `internal/adif/parse.go`
- `internal/adif/string.go`
- `internal/adif/consts.go`
- `internal/adif/doc.go`
- `internal/adif/*_test.go`

Adjacent contracts checked:

- Public ADIF submit path: `internal/api/handler_qso.go`
- CLI import path: `cmd/smd/import.go`
- Submit/import normalization: `internal/qsoservice/submit.go`
- Session email ADIF export: `internal/api/handler_session_email.go`
- QRZ forwarding ADIF export: `internal/forwarding/qrz/qrz.go`
- Embedded DTOs: `internal/types/{qso,qso_details,contacted_station,logging_station,qsl}.go`
- Prior review context: `docs/reviews/archive/internal-adif-2026-06-04.md`

Only this review document was added; no source code changes were made.

## Headline verdict

The June 4 review's main correctness fixes are present: UUIDs are restored for
trusted import, length-aware parsing avoids `<EOR>` and `<EOH>` inside field
values, and the bulk-parse allocation regression has a benchmark guard. The
current package is small and well-covered for common Station Manager flows.

I found one availability/security issue in malformed length handling, two
data-fidelity issues around ADIF round-trips, and stale comments that now
misdescribe current behavior.

Headline counts: **0 Critical**, **1 High**, **2 Medium**, **1 Low**.

## High findings

### H1. Oversized ADIF field lengths can panic the parser

**Files:**

- `internal/adif/parse.go:53`
- `internal/adif/parse.go:55`
- `internal/adif/parse.go:57`
- `internal/adif/parse.go:64`
- `internal/api/handler_qso.go:153`
- `cmd/smd/import.go:80`

`Parse` accepts untrusted ADIF from both `POST /v1/qso` and `smd import`.
When the scanner sees a field header, it parses the declared length with
`strconv.Atoi` but ignores the error:

```go
length, _ := strconv.Atoi(...)
valEnd := valStart + length
if valEnd > n {
    valEnd = n
}
val := string(data[valStart:valEnd])
```

For a field such as `<CALL:999999999999999999999999999999>x`, `Atoi` returns a
range error and a saturated `int` value. The subsequent `valStart + length`
can overflow before the `valEnd > n` clamp runs, producing a negative or
otherwise invalid slice bound. That turns a tiny malformed ADIF body into a
runtime panic instead of a structured parse failure.

Why it matters:

- The public API body-size cap does not help; the malicious input can be very
  small because the danger is in the declared length, not the request size.
- `net/http` will usually recover per connection, but the request still
  becomes an unclean handler panic and log-noise DoS vector.
- `smd import` can crash the whole import process before reporting a per-record
  error.

Suggested fix:

Check the `Atoi` error and reject or safely skip field headers whose declared
length does not fit in `int`. Also guard the addition with `if length > n-valStart`
before computing `valEnd`, so the truncation policy cannot be bypassed by
integer overflow.

Regression tests should cover:

- absurdly large field length returns an error or safely ignores the malformed
  field, with no panic;
- a length just larger than the remaining buffer still follows the existing
  tolerant truncation policy, if that policy remains intentional;
- `POST /v1/qso` maps the parse failure to `400 invalid_adif` rather than a
  handler panic.

## Medium findings

### M1. Several QSL-related ADIF fields still do not round-trip

**Files:**

- `internal/types/qsl.go:12`
- `internal/types/qsl.go:13`
- `internal/types/qsl.go:17`
- `internal/types/qsl.go:18`
- `internal/adif/adif.go:62`
- `internal/adif/adif.go:72`
- `internal/adif/adif.go:101`
- `internal/adif/adif.go:153`
- `internal/adif/doc.go:27`

`types.Qsl` contains fields that the ADIF converter still cannot preserve:

- `qslmsg_rcvd`
- `qsl_rcvd_via`
- `qsl_rcvd_notes`

`QslSection` does not include those fields, and `QsoToRecord` / `RecordToQso`
only map the smaller `QslSection` subset. `QslSection` also has QRZ download
fields and `QSLMSG_INTL`, but those are not represented on `types.Qso`, so they
are parsed into a transient `Record` and then dropped at the persistence
boundary.

Why it matters:

- A QSO already stored with `types.Qsl.QslMsgRcvd`, `QslRcvdVia`, or
  `QslRcvdNotes` will not include that metadata in session email ADIF or QRZ
  forwarding payloads.
- Imported ADIF containing those received-QSL fields loses them before storage.
- The package-level doc already says this structural drift is a known issue,
  but it is still open and now sits on active export paths.

Suggested fix:

Decide whether Station Manager intends to support the received-QSL fields. If
yes, add them to `QslSection`, map them in both converter directions, classify
them in `spec_validation_test.go`, and add parse/export round-trip tests. If
no, document the unsupported fields explicitly so operators do not assume a full
ADIF QSL round-trip.

### M2. Length-delimited values are trimmed on parse

**Files:**

- `internal/adif/parse.go:18`
- `internal/adif/parse.go:64`

The parser comments correctly describe ADIF values as length-prefixed and say
the scanner skips exactly the declared byte length. The implementation then
applies `strings.TrimRightFunc(..., unicode.IsSpace)` to every parsed value.

That means a field such as `<COMMENT:9>TNX QSO ` parses as `TNX QSO`, not the
nine bytes the sender declared. The same mutation applies to `QSLMSG`, `NOTES`,
`NAME`, `QTH`, and every other string field.

Why it matters:

- Station Manager can no longer be a faithful parser for free-text values that
  intentionally end with a space, tab, or newline.
- The behavior contradicts the length-aware scanner contract and can hide bad
  length prefixes by shaving trailing whitespace from the value.
- If strict or warning-mode parsing is added later, this trim will obscure the
  exact input that should be validated.

Suggested fix:

Remove the unconditional right-trim and let the ADIF length prefix define the
value exactly. If a caller wants display cleanup, do it at that caller boundary,
not in the parser. Add a regression test with a trailing-space `COMMENT` and a
multiline `NOTES` value.

## Low findings

### L1. Comments still describe behavior that has already changed

**Files:**

- `internal/adif/doc.go:6`
- `internal/adif/doc.go:9`
- `internal/types/qso.go:51`
- `internal/types/qso.go:56`
- `internal/adif/adif.go:35`
- `internal/adif/adif.go:127`
- `internal/adif/adif.go:175`
- `internal/adif/adif_test.go:182`

Two documentation comments are stale:

- `internal/adif/doc.go` says the parser "splits on <EOH>/<EOR> markers".
  The current parser is a length-aware scanner and no longer marker-splits
  inside values.
- `types.Qso.AppSmRequestQsl` says the ADIF parser does not surface
  `APP_SM_REQUEST_QSL` and that closing the parser gap is a separate task.
  Current `Record`, `QsoToRecord`, `RecordToQso`, and tests show that the gap
  is closed.

Why it matters:

- These comments sit on the core ADIF and canonical QSO types, so future
  reviewers and implementers will use them as contract documentation.
- The stale `APP_SM_REQUEST_QSL` comment can cause unnecessary follow-up work
  or wrong assumptions about the POST path.

Suggested fix:

Update both comments to match the current scanner and request-QSL mapping. If
`QSL_WANTED` remains a legacy user-defined field rather than the active request
flag, document that distinction near `UserDef`.

## Performance notes

No new performance regression was found from inspection. The current parser has
an explicit bulk benchmark covering the June 4 allocation fix:

```text
BenchmarkParse_ManyRecords-8    19    61248203 ns/op    35125423 B/op    150072 allocs/op
```

That benchmark parses 5,000 records on the local Intel i3-10100F. The allocation
profile is still dominated by expected per-field string/map construction in the
reflection-based parser. If bulk imports become a measured bottleneck again, the
next optimization target is the intermediate `map[string][]string` and per-field
string allocation path, not marker scanning.

## Verification

Commands run:

```sh
go test -count=1 ./internal/adif
go test -race -count=1 ./internal/adif
go vet ./internal/adif
go test -count=1 ./internal/adif ./internal/qsoservice ./cmd/smd ./internal/api
go test -count=1 ./internal/api
go vet ./internal/adif ./internal/qsoservice ./cmd/smd ./internal/api
go test -race -count=1 ./internal/adif ./internal/qsoservice ./cmd/smd
go test -run '^$' -bench=Parse_ManyRecords -benchmem ./internal/adif
```

Results:

- `go test -count=1 ./internal/adif` passed.
- `go test -race -count=1 ./internal/adif` passed.
- `go vet ./internal/adif` passed.
- `go test -count=1 ./internal/adif ./internal/qsoservice ./cmd/smd ./internal/api`
  passed for `internal/adif`, `internal/qsoservice`, and `cmd/smd`; `internal/api`
  initially failed in the sandbox because `httptest` could not bind localhost
  (`socket: operation not permitted`).
- `go test -count=1 ./internal/api` was rerun with listener access and passed.
- `go vet ./internal/adif ./internal/qsoservice ./cmd/smd ./internal/api`
  passed.
- `go test -race -count=1 ./internal/adif ./internal/qsoservice ./cmd/smd`
  passed.
- `BenchmarkParse_ManyRecords` completed with the result shown above.

## Resolution (2026-06-19)

All four findings addressed.

- **H1 (fixed).** `Parse` now checks the `strconv.Atoi` error and compares the
  declared length against the remaining buffer (`length <= n-valStart`) *before*
  the `valStart+length` addition, so a malformed/oversized length collapses to
  the existing tolerant "take the available bytes" policy instead of overflowing
  into a negative slice bound. Regression test
  `TestParse_AbsurdFieldLengthDoesNotPanic` (overflow + large-but-valid cases).
- **M1 (implemented).** The incoming-QSL fields `QSLMSG_RCVD`, `QSL_RCVD_VIA`,
  and `QSL_RCVD_NOTES` were added to `QslSection` and mapped in both
  `QsoToRecord` and `RecordToQso`, classified in `spec_validation_test.go`, and
  covered by `TestRoundTrip_ReceivedQslFields` + the fully-populated emit test.
  The remaining `QslSection`→`types.Qso` drift (`QSLMSG_INTL`,
  `QRZCOM_QSO_DOWNLOAD_DATE/STATUS` — fields with no home on the canonical
  model) is now documented in `doc.go` as intentionally unsupported.
- **M2 (documented, behaviour kept).** The unconditional right-trim is retained
  deliberately — SM ingests real-world ADIF that pads within the declared
  length, and trimming keeps a padded callsign from mismatching in
  dupe-detection. The trade-off (free-text values lose intentional trailing
  whitespace) is now documented at the trim site and in `doc.go`, with
  characterization test `TestParse_TrailingWhitespaceTrimmed`. A future
  strict/import mode would make the trim opt-out.
- **L1 (fixed).** Updated the stale `doc.go` "splits on <EOH>/<EOR> markers"
  comment to describe the length-aware scanner, and the
  `types.Qso.AppSmRequestQsl` comment to reflect that both the POST (ADIF) and
  PATCH (JSON) paths now persist the flag.
