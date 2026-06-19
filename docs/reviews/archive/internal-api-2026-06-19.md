# `internal/api` - code review (2026-06-19)

## Scope

Review-only pass over `internal/api` as a fresh package: routing, middleware,
request limits, JSON/body handling, QSO/logbook/config/session-email handlers,
FT8/rig-control API entry points, SSE behavior, tests, and the adjacent contracts
that those handlers expose directly (`internal/email`, `internal/ft8`,
`internal/config`, `internal/database/sqlite`, `internal/qsoservice`, and the
frontend/docs that pin the wire shape).

No production code changes were applied during this review. This document is the
only artifact from the pass.

Headline counts: **0 Critical**, **0 High**, **2 Medium**, **3 Low**.

## Medium findings

### M1. Session-email recipient validation is too weak and fails after side effects

**Files:**

- `internal/api/handler_session_email.go:82`
- `internal/api/handler_session_email.go:91`
- `internal/api/handler_session_email.go:135`
- `internal/api/handler_session_email.go:175`
- `internal/api/handler_session_email.go:188`
- `internal/email/email.go:138`
- `internal/email/email.go:193`
- `internal/email/email.go:232`
- `internal/email/email.go:233`
- `internal/api/handler_session_email_test.go:104`

`POST /v1/session/email` trims `to` and accepts anything containing `@`. It then
fetches every QSO, composes ADIF, archives the local copy, and only later calls
`email.Service.Send`.

That means syntactically invalid recipients such as `a@\r\nBcc: x@y` or other
non-address strings with an `@` pass API validation. The stdlib SMTP envelope
path rejects CR/LF before `RCPT TO`, but by then the handler has already created
the archive and the response is a 502 `smtp_failure`, not a client-side 400. The
mailer also writes the `To:` MIME header from `msg.To` directly, so the package
boundary relies on the SMTP envelope validation rather than validating the
message shape before composing mail data.

Impact:

- Bad client input can create local archive files for emails that should never
  have reached the compose/archive/send path.
- Operators get a transport-failure diagnosis for invalid request data.
- The API boundary is not defending the email package against control
  characters or multi-address/display-name shapes.

Suggested fix:

- Parse `req.To` with `net/mail.ParseAddress` at the API boundary.
- Require exactly one mailbox and reject CR/LF/control characters before QSO
  fetch, ADIF composition, or archive writes.
- Pass the normalized address to `email.Message`.
- Add handler tests for CRLF, comma-separated addresses, display-name addresses
  if intentionally unsupported, and a valid single mailbox.
- Consider mirroring the same validation inside `email.Service.Send`, since
  that package is a second public boundary inside the process.

### M2. FT8 QSO handler exposes raw service errors and classifies unknown failures as 400

**Files:**

- `internal/api/handler_ft8_qso.go:57`
- `internal/api/handler_ft8_qso.go:153`
- `internal/api/handler_ft8_qso.go:193`
- `internal/api/handler_ft8_qso.go:216`
- `internal/api/handler_ft8_qso.go:218`
- `internal/ft8/sequencer.go:202`
- `internal/ft8/work_sequencer.go:41`
- `internal/api/response.go:47`
- `internal/api/handler_ft8_qso_test.go:43`

`writeFt8QsoError` handles the known FT8 sentinel errors, but its default branch
returns:

```go
"could not start the QSO: " + err.Error()
```

with HTTP 400 `invalid_field_value`.

Today, malformed `slot_utc` reaches `time.Parse` in the FT8 sequencer and sends
Go's parse error text back to the client. The same default path will also expose
future non-sentinel service errors and incorrectly label them as client
validation failures.

Impact:

- The HTTP API leaks internal implementation detail in the error envelope.
- Unknown FT8 service faults are reported as 400s, which can hide real server or
  hardware-control defects from logs, clients, and tests.
- The behavior is inconsistent with `writeServerError`, which deliberately logs
  server detail while returning stable client messages.

Suggested fix:

- Validate `slot_utc` in the handler and return a stable
  `invalid_field_value` message such as `slot_utc must be RFC3339 UTC`.
- Keep known FT8 sentinel errors mapped to their current stable codes.
- Log and return a generic 500 for unknown errors instead of sending
  `err.Error()` to clients.
- Add HTTP-handler tests for malformed `slot_utc`, no offset, in-progress,
  unencodable calls, unsupported caller mode, and an unknown error path.

## Low findings

### L1. Duplicate UUIDs in session-email requests duplicate ADIF records and confuse stamp accounting

**Files:**

- `internal/api/handler_session_email.go:108`
- `internal/api/handler_session_email.go:111`
- `internal/api/handler_session_email.go:125`
- `internal/api/handler_session_email.go:126`
- `internal/api/handler_session_email.go:127`
- `internal/api/handler_session_email.go:215`
- `internal/database/sqlite/api_context.go:1980`
- `internal/database/sqlite/api_context.go:2006`
- `internal/database/sqlite/api_context.go:2022`
- `internal/adif/adif.go:194`

The handler preserves `req.UUIDs` exactly. If a client sends the same UUID twice,
the same QSO is fetched twice, appended twice to the `types.QsoSlice`, emitted
twice in the ADIF attachment, and returned twice in `emailed`.

Stamping is set-based SQL (`WHERE id IN (...)`), so duplicate IDs affect
`len(stampIDs)` but not `RowsAffected`. A duplicate request can therefore log
"fewer rows stamped than sent" even though every unique row was stamped.

Suggested fix:

- Deduplicate trimmed UUIDs before fetching rows, preserving first occurrence
  order for the attachment.
- Compare stamp counts against the number of unique IDs.
- Add a handler test with `["uuid", "uuid"]` proving only one ADIF record and
  one response UUID are emitted.

### L2. FT8 QSO HTTP tests stop before the service-error mapping surface

**Files:**

- `internal/api/handler_ft8_qso_test.go:15`
- `internal/api/handler_ft8_qso_test.go:43`
- `internal/api/handler_ft8_qso_test.go:79`
- `internal/api/handler_ft8_qso.go:193`
- `internal/ft8/sequencer_test.go:244`
- `internal/ft8/work_sequencer_test.go:82`

The handler tests use an enabled but keyer-less FT8 service. That is useful for
basic routing and `ft8_tx_not_armed`, but it means most of
`writeFt8QsoError` is only indirectly covered by `internal/ft8` package tests.
There is no HTTP-level regression pin for malformed `slot_utc`, `ft8_no_offset`,
`ft8_qso_in_progress`, `ft8_tx_bad_message`, `ft8_caller_mode_unsupported`, or
future unknown-error behavior.

Suggested fix:

- Add a small fake/stub around the FT8 service boundary, or factor the error
  mapper so it can be tested directly.
- Keep sequencer tests in `internal/ft8`, but add API tests that assert the
  actual HTTP status/code/message emitted for each service error class.

### L3. FT8 frequency documentation still says QSO frequency is dial plus offset

**Files:**

- `docs/v2-design/api-endpoints.md:266`
- `docs/v2-design/api-endpoints.md:274`
- `frontend/logging/src/lib/api/ft8qso.ts:61`
- `frontend/logging/src/lib/api/ft8qso.ts:85`
- `internal/ft8/sequencer.go:115`
- `internal/ft8/qsolog.go:17`
- `internal/ft8/qsolog_test.go:37`
- `internal/api/ft8_qsolog_test.go:70`

The implementation now logs FT8 QSO `FREQ` as the rig dial frequency, not
dial-plus-audio-offset. `BuildQso` documents that convention and the regression
tests pin it. However, the API endpoint document, the frontend API wrapper
comments, and the `CompletedQso.DialFreqMHz` comment still say the logged
frequency is `operating_freq_mhz + offset_hz` or that the band is derived from
the sum.

Impact:

- Future API/client work can reintroduce the old dial-plus-offset behavior.
- Developers reading the endpoint docs get the opposite contract from the
  current code and tests.

Suggested fix:

- Update the endpoint docs and comments to state that QSO `FREQ` and `BAND` are
  derived from the dial frequency; `offset_hz` is TX placement only.
- Preserve the PSK Reporter docs that say spot frequency is dial plus audio
  offset; that is a different reporting contract.

## Known deferred item not counted again

`PUT /v1/config` still has the contract issues filed from the 2026-06-14 review:
`default_logbook.id` / `default_rig.id` are documented as writable but not
copied into the candidate, and omitted `logging_station` / `station` blocks are
zeroed because those blocks are not presence-aware. This is already tracked in
`docs/backlog.md:27` and was intentionally deferred for a focused config-SPA
readthrough, so I did not count it as a new finding here.

Related current-code/doc evidence:

- `internal/api/handler_config.go:31`
- `internal/api/handler_config.go:171`
- `internal/api/handler_config.go:172`
- `docs/backlog.md:27`

## Positive notes

- Request body limiting is now centralized through `readBody`/`readJSONBody`,
  including session email.
- All three SSE paths are separated from the normal concurrent request cap and
  have per-write deadline handling in their respective handlers.
- QSO/logbook active-row update behavior has regression coverage for the prior
  soft-delete race class.
- `Server.Shutdown` is idempotent and covered.
- The route table is explicit and narrow; FT8 and rig write paths are only
  registered when the corresponding subsystem is enabled.
- Sensitive SMTP fields stay off `/v1/config`; only enabled/default recipient
  are surfaced.
- pprof is opt-in through config and is not mounted by default.

## Verification

Commands run:

```text
go test ./internal/api -count=1
go test ./internal/email -count=1
go test ./internal/api -run 'TestHandleFt8Qso|TestSessionEmail_(Missing|ToWithout|Malformed|NoQsos|Success|Archives|Mailer|Smtp)' -count=1
go test ./internal/api ./internal/email ./internal/ft8 ./internal/config ./internal/database/sqlite ./internal/qsoservice -count=1
go test -race ./internal/api -count=1
go vet ./internal/api ./internal/email ./internal/ft8 ./internal/config ./internal/database/sqlite ./internal/qsoservice
```

All commands passed. Listener-backed tests needed to be rerun outside the
filesystem/network sandbox because the sandbox rejects localhost binds with
`listen tcp 127.0.0.1:0: socket: operation not permitted`.

## Resolution (2026-06-19)

All five findings addressed.

- **M1 (fixed).** `POST /v1/session/email` now parses `to` with
  `net/mail.ParseAddress` at the boundary, BEFORE any QSO fetch / ADIF compose /
  archive write — rejecting CR/LF header-injection shapes and comma-separated
  lists as a 400, and normalizing display-name addresses to the bare mailbox
  that's sent. Tests: `TestSessionEmail_HeaderInjectionRecipient_Returns400`,
  `TestSessionEmail_MultipleRecipients_Returns400`,
  `TestSessionEmail_DisplayNameRecipient_Sends` (the last asserts no side-effect
  archive on rejection).
- **M2 (fixed).** `writeFt8QsoError`'s catch-all no longer returns
  `err.Error()` as a 400 — `slot_utc` is validated in the handlers
  (`validFt8SlotUTC`, RFC3339) and returns a stable `invalid_field_value`
  message, and unknown service faults now route through `writeServerError`
  (logged detail, generic 500 `internal_error`).
- **L1 (fixed).** Request UUIDs are deduplicated (`dedupeStrings`, first-
  occurrence order) before fetch, so a repeated id emits one ADIF record / one
  `emitted` entry and the stamp-count check is against unique ids. Test:
  `TestSessionEmail_DuplicateUuids_EmitsOnce`.
- **L2 (fixed).** `TestWriteFt8QsoError_Mapping` exercises the error mapper
  directly across every sentinel class plus the unknown→generic-500 path
  (asserting no raw-error leak); `slot_utc` malformed-input is pinned at the
  HTTP handler level for both qso/start and qso/work.
- **L3 (fixed).** Updated the stale "dial + offset" wording in
  `api-endpoints.md` (qso/start + qso/work Notes), `ft8qso.ts` (two wrapper
  comments), and the `CompletedQso.DialFreqMHz` comment in `sequencer.go` to
  state that QSO `FREQ`/`BAND` derive from the dial and `offset_hz` is TX
  placement only. The PSK Reporter dial-plus-audio-offset spot contract is a
  different reporting convention and was deliberately left unchanged.

The deferred `PUT /v1/config` presence-awareness item remains tracked in
`docs/backlog.md` for the config-SPA workstream; not touched here.
