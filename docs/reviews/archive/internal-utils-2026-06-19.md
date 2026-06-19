# internal/utils Code Review - 2026-06-19

## Scope

Reviewed `internal/utils` as a new codebase, then followed its direct contracts into `internal/api`, `internal/qsoservice`, `internal/ft8`, `internal/database/sqlite`, `internal/config`, `internal/lookup`, `internal/logging`, `cmd/smd`, and the frontend frequency/time mirrors where they define expected behaviour.

Reviewed revision: `f6d0d361`. `internal/utils` had no local diff at review time. The working tree already contained unrelated local changes outside this package; they were not reviewed as part of this artifact and were not modified.

## Summary

No critical or high-severity issues found. The package is mostly small, pure code with good package-level coverage and no obvious secret-handling, unsafe, shelling-out, or TLS-bypass risks.

The main risk is contract drift: several helpers accept or derive values that their downstream callers treat as stronger guarantees. The two most important examples are frequency-to-band mapping and ADIF time storage.

## Findings

### M1. Frequency-to-band validation uses string prefixes, not the configured frequency ranges

`internal/utils/frequency.go` defines real band ranges (`frequencyRanges`, lines 21-40), but both `GetFrequencyRange` and `FrequencyToBand` only test `strings.HasPrefix` (`GetFrequencyRange`, lines 74-83; `FrequencyToBand`, lines 85-94). That means values inside a MHz prefix but outside the band allocation are classified as valid band frequencies.

Examples that currently classify incorrectly:

- `14.999` returns `20m`, even though the configured 20m range ends at `14.350`.
- `7.999` returns `40m`, even though the configured 40m range ends at `7.200`.
- `5.000` returns `60m`, even though the configured 60m range starts at `5.351500`.
- `24.100` returns `12m`, even though the configured 12m range starts at `24.890`.

This leaks into runtime contracts:

- FT8 start/work/CQ validation says `operating_freq_mhz` must be a positive known-band dial frequency, but it delegates to `utils.FrequencyToBand(fmt.Sprintf("%.6f", mhz))` (`internal/api/handler_ft8_qso.go`, lines 26-39 and 80-83/131-134/193-196). In-prefix out-of-band values therefore pass before an on-air sequenced QSO is started.
- FT8 logging derives `q.Band` from the dial with the same helper (`internal/ft8/qsolog.go`, lines 27-40), so a completed QSO can be logged with the wrong band.
- QSO update explicitly intends to leave out-of-band frequencies alone so band validation catches them, but the prefix helper overwrites the band before validation (`internal/qsoservice/update.go`, lines 87-101).
- The frontend mirror uses numeric ranges and returns `''` between bands (`frontend/logging/src/lib/utils/frequency.ts`, lines 16-46), with tests for between-band values (`frontend/logging/src/lib/utils/frequency.test.ts`, lines 38-45). The daemon therefore disagrees with the UI on values such as 5 MHz and in-prefix upper overrun cases.

The test gap is visible in `internal/utils/frequency_test.go`: `TestFrequencyToBand` only covers happy prefixes and `999.999` (`lines 19-31`), so it never exercises in-prefix out-of-range values.

Suggested direction: make the daemon helper parse a numeric MHz value and compare it against the actual low/high range table. If callers need a string API, keep the existing signature but implement it by parsing and range-checking. Add tests for band edges and just-outside values (`14.350`/`14.351`, `7.200`/`7.201`, `5.000`, `5.357`, `24.889`/`24.890`), then add API/qsoservice regressions for in-prefix out-of-band values.

### M2. HHMMSS times validate successfully but cannot be stored in the QSO schema

The time helpers correctly model ADIF's optional seconds form: `IsValidTimeADIF` accepts both `HHMM` and `HHMMSS` (`internal/utils/date_time.go`, lines 55-77), and `SanitizeTimeToADIF` returns `HHMMSS` for inputs such as `23:59:58` (`lines 110-172`). The package tests pin that behaviour (`internal/utils/time_validation_test.go`, lines 5-35; `internal/utils/time_sanitize_test.go`, lines 5-39).

Downstream storage is narrower. `qsoservice.Submit` accepts the sanitized value and assigns it directly to `qso.TimeOn`/`qso.TimeOff` (`internal/qsoservice/submit.go`, lines 149-163 and 204-214). `Update` does the same (`internal/qsoservice/update.go`, lines 82-83 and 129-137). The sqlite schema, however, requires `length(time_on) = 4` and `length(time_off) = 4` (`internal/database/sqlite/migrations/0001_init.up.sql`, lines 56-63). The adapter only strips colons; it does not truncate seconds (`internal/database/sqlite/adapters/type_to_model.go`, lines 39-51).

Result: an ADIF body with `<TIME_ON:6>084500<TIME_OFF:6>085000` passes utility and service validation, then fails at insert/update as a storage error. The HTTP handler maps non-`SubmitError` submit failures to `500 submit_failed` (`internal/api/handler_qso.go`, lines 241-254), so client-valid-looking input becomes a server error instead of a deterministic 400 or normalized stored value.

Documentation is also split: `docs/v2-design/api.md` says API time fields are `HHMM` or `HHMMSS` (`lines 696-704`), while the frontend design says the SPA uses `HHMM` and does not use `HHMMSS` (`docs/v2-design/frontend-spa.md`, lines 596-604). FT8 logging already documents the storage contract as exactly `HHMM` (`internal/ft8/qsolog.go`, lines 42-50).

Suggested direction: choose one storage boundary. Given the current schema, FT8 path, and frontend contract, the least disruptive fix is to normalize accepted `HHMMSS` to `HHMM` before `types.Qso` reaches sqlite, or reject seconds at the service boundary with a `SubmitError`. Then update `docs/v2-design/api.md` and add API/qsoservice tests for `TIME_ON`/`TIME_OFF` with seconds.

### M3. `ParseFreqMHz` accepts zero and negative frequencies, causing bad data or late storage failures

`ParseFreqMHz` trims and parses decimal MHz or bare integer kHz, but it never enforces a positive value (`internal/utils/frequency.go`, lines 118-140). `FormatFreqMHz` formats whatever integer it receives (`lines 142-148`). `qsoservice.Submit` treats any parse success as valid and formats the value for storage (`internal/qsoservice/submit.go`, lines 188-203); `Update` does the same (`internal/qsoservice/update.go`, lines 87-92).

This produces two bad outcomes:

- `FREQ=0` parses as `0`, formats as `0.000`, and can be inserted because the sqlite constraint allows `freq >= 0` (`internal/database/sqlite/migrations/0001_init.up.sql`, line 48), even though `FREQ` is required and the older `IsValidFrequencyMHz` helper treats zero as invalid (`internal/utils/frequency_validation_test.go`, lines 25-34 and 53-67).
- Negative values parse successfully. They can format into malformed MHz text before the sqlite adapter tries to parse them again (`internal/database/sqlite/adapters/type_to_model.go`, lines 31-37), which turns a client input error into a late `failed to insert QSO` / `failed to update QSO` path rather than a `SubmitError`.

The current tests cover invalid syntax only (`internal/utils/frequency_convert_test.go`, lines 43-50) and explicitly bless formatting zero (`lines 63-78`), but there are no service/API tests for `FREQ=0` or negative values.

Suggested direction: add a QSO-facing parser that rejects `<= 0`, or make `ParseFreqMHz` itself enforce positivity if no current caller needs zero as a sentinel. Consider tightening the sqlite `freq` check to `freq > 0` if zero is not a valid stored QSO. Add unit and handler tests for zero and negative values so they return `invalid_field_value`.

### L1. Coordinate formatting accepts impossible coordinates

`ConvertToXDDDMMM` parses a float, chooses a direction, and formats degrees/minutes, but it does not verify finite values or latitude/longitude bounds (`internal/utils/lat_long.go`, lines 14-55). `IsXDDDMMM` validates a generic `N/S/E/W DDD MM.MMM` shape with `deg <= 180`, but it cannot enforce latitude's `<= 90` limit (`lines 58-99`).

Examples:

- `ConvertToXDDDMMM("91", true)` returns a syntactically valid north latitude even though latitude 91 is impossible.
- `IsXDDDMMM("N180 00.000")` returns true by the generic 180-degree rule, even though `N180` is not a valid latitude.

Current production impact appears limited because the main daemon caller, `MaidenheadToADIFLatLon`, feeds coordinates derived from a validated Maidenhead locator (`internal/utils/maidenhead.go`, lines 103-120). The helper is still exported within the package's shared utility surface, and its tests only cover normal values, invalid syntax, zero, and rounding carry (`internal/utils/lat_long_test.go`, lines 5-89).

Suggested direction: validate `math.IsNaN`/`math.IsInf`, enforce `abs(coord) <= 90` for latitude and `<= 180` for longitude in `ConvertToXDDDMMM`, and split validation helpers or add a coordinate-kind parameter if callers need to validate existing ADIF strings.

### L2. Package documentation and test coverage have drifted around newer helpers

The package-level doc says HTTP client factories were removed (`internal/utils/doc.go`, lines 19-22), but `NewHTTPClient` now exists and is used by lookup providers (`internal/utils/http_client.go`, lines 26-64). The `FormatDate` and `FormatTime` comments also say the error return is the placeholder `"YYYY-MM-DD"` / `"HH:MM"`, while the implementation returns an empty string (`internal/utils/date_time.go`, lines 17-32). `DecodeStringToUTF8` says it resolves encoding issues "if possible", but it always constructs a UTF-8 reader (`internal/utils/utf8_decoder.go`, lines 9-24).

Test coverage is numerically strong (`91.5%` statements), but the function profile shows important gaps:

- `NewHTTPClient`: `0.0%` direct package coverage.
- `XDGDataDir`: `0.0%` direct package coverage.
- Lower coverage on environment/process helpers such as `WorkingDir` (`73.3%`) and `AbsDirPathForExecutable` (`71.4%`) is understandable, but these helpers define daemon startup behaviour.

Suggested direction: refresh `doc.go` and the stale comments, add direct `NewHTTPClient` tests that assert timeout defaulting and transport knobs, and add a small `XDGDataDir` test with `XDG_DATA_HOME` and `HOME` isolation.

## Security Review

No direct high-severity security issue was found in `internal/utils`.

Positive observations:

- UUID generation uses `crypto/rand` and validates UUIDv7 shape without accepting non-canonical casing (`internal/utils/uuid.go`, lines 47-63 and 86-112).
- The shared HTTP client sets finite overall, dial, TLS handshake, idle, and expect-continue timeouts, keeps TLS defaults, and uses `http.ProxyFromEnvironment` (`internal/utils/http_client.go`, lines 40-64).
- Working-directory creation is centralized and errors are propagated (`internal/utils/working_dir.go`, lines 33-59).

Security-relevant caveat: the frequency and time findings are input-validation issues at public/API boundaries. They do not look exploitable for data disclosure or code execution, but they can produce wrong persisted data or turn malformed client input into generic 500s.

## Performance Review

No material performance issue was found. The helpers are small and allocation-light enough for their call sites. `FrequencyToBand` and `GetFrequencyRange` currently scan small maps; replacing prefix matching with a small ordered range table will not matter at daemon scale. `NewHTTPClient` creates a fresh transport per provider/service instance, which is acceptable for the current lookup-provider construction pattern and avoids shared mutable transport surprises.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test ./internal/utils`
- `GOCACHE=/tmp/go-build go vet ./internal/utils`
- `GOCACHE=/tmp/go-build go test -cover ./internal/utils`
- `GOCACHE=/tmp/go-build go test -race ./internal/utils`
- `GOCACHE=/tmp/go-build go test -coverprofile=/tmp/internal-utils.cover ./internal/utils`
- `go tool cover -func=/tmp/internal-utils.cover`
- `GOCACHE=/tmp/go-build go test ./internal/qsoservice ./internal/api ./internal/database/sqlite ./internal/config ./internal/lookup ./internal/ft8 ./cmd/smd`
- `GOCACHE=/tmp/go-build go test -race ./internal/qsoservice ./internal/api ./internal/database/sqlite ./internal/config ./internal/lookup ./internal/ft8 ./cmd/smd`

Results:

- `internal/utils` tests, vet, race, and coverage all passed.
- Package coverage: `91.5%` statements.
- The adjacent package suites initially failed in the restricted sandbox because `httptest.NewServer` could not bind a local listener. Re-running the same adjacent suites with local listener permission passed, including the race run.

## Resolution (2026-06-19)

All five findings fixed. Operator decisions: M2 → truncate accepted `HHMMSS` to
`HHMM` at the service boundary; L1 → add coordinate bounds checks now.

- **M1 (fixed — frequency→band).** `GetFrequencyRange` and `FrequencyToBand`
  no longer prefix-match (which classified `14.999`→20m, `7.999`→40m,
  `5.000`→60m, `24.100`→12m). Replaced the prefix maps with an ordered numeric
  MHz `bandRanges` table and a `bandRangeFor` parse+range-check helper, mirroring
  the frontend's allocation table (`frontend/.../frequency.ts`) so daemon and SPA
  now agree on what's in-band (incl. the wider 5.25–5.45 MHz 60m and the VHF/UHF
  bands the daemon lacked). `FrequencyToBand` returns `""` for between-band,
  past-edge, and unparseable input — the contract the three callers (FT8 dial
  validation in `handler_ft8_qso.go`, QSO band derivation in `update.go`,
  FT8 logging in `qsolog.go`) already assume. Tests:
  `TestFrequencyToBand_RangeEdges` (the old false positives + band edges +
  just-outside + unparseable) and a tightened `TestGetFrequencyRange`.
- **M2 (fixed — `HHMMSS` storage).** An ADIF body with `HHMMSS` times validated
  (the helpers correctly model ADIF's optional seconds) then failed at insert
  because the sqlite schema requires `length=4`, surfacing as a generic 500. New
  `utils.TimeToHHMM` narrows a 6-digit time to `HHMM`; `qsoservice.Submit` and
  `Update` apply it right after sanitize (before the time-coherence compare, so
  both times share precision). This keeps the documented "API accepts HHMMSS"
  contract (`docs/v2-design/api.md`, unchanged + still accurate) while storing to
  the minute — matching the schema, the FT8 path, and the SPA. Tests:
  `utils.TestTimeToHHMM`, `qsoservice.TestSubmit_HHMMSSStoredAsHHMM`.
- **M3 (fixed — non-positive freq).** `ParseFreqMHz` now rejects a zero or
  negative result. FREQ is required and no caller uses zero as a sentinel, so
  `FREQ=0`/negative was either storable bad data (the sqlite check allows `>= 0`)
  or a late insert failure; catching it in the one parse chokepoint turns it into
  the existing `invalid_field_value` (400) path in `Submit`/`Update`.
  `FormatFreqMHz(0)` is unaffected (formatting, not parsing). Tests:
  `utils.TestParseFreqMHz_RejectsNonPositive`,
  `qsoservice.TestSubmit_RejectsNonPositiveFreq` (asserts a `SubmitError`, not a
  storage error).
- **L1 (fixed — coordinate bounds).** `ConvertToXDDDMMM` now rejects non-finite
  (NaN/±Inf) and out-of-range coordinates (latitude ±90°, longitude ±180°, by
  the existing `isLat` flag) rather than formatting an impossible value like
  lat 91. The daemon's only caller feeds validated Maidenhead-derived values, but
  the helper is exported on the shared surface. Test:
  `TestConvertToXDDDMMM_RejectsOutOfRange` (out-of-range + non-finite rejected,
  exact boundaries accepted).
- **L2 (fixed — doc drift + test gaps).** `doc.go` no longer claims HTTP client
  factories were removed — it now documents `NewHTTPClient` as the deliberate
  *specific* replacement for the removed generic factory (and lists
  `XDGDataDir`). `FormatDate`/`FormatTime` comments corrected (they return an
  empty string on bad length, not a placeholder). `DecodeStringToUTF8`'s comment
  now states it's a validating UTF-8 pass-through, not a source-charset sniffer.
  New direct tests: `http_client_test.go` (`NewHTTPClient` timeout defaulting +
  transport knobs, previously 0%) and `xdg_test.go` (`XDGDataDir` both the
  `XDG_DATA_HOME`-set and `HOME`-fallback branches, previously 0%).

Verified: `gofmt`/`go vet` clean; `internal/utils`, `internal/qsoservice`,
`internal/api`, `internal/ft8`, `internal/database/sqlite` build + pass; `-race`
clean on `internal/utils`; `CGO_ENABLED=0 go build ./...` succeeds.
