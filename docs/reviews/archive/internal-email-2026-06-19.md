# Code Review: internal/email

Date: 2026-06-19

Scope: `internal/email`, including SMTP transport, MIME envelope generation, package tests, and the closest caller/config contracts in `internal/api`, `internal/config`, `internal/types`, and `cmd/smd`.

## Summary

I reviewed the current tree as a fresh codebase. I did not use archived review documents as evidence for the findings below.

The package is small and the core transport shape is sound: each send opens a fresh SMTP session, applies a single operation deadline to the dial and connection, uses STARTTLS when configured, writes through `net/smtp`'s DATA writer, and keeps SMTP credentials out of `/v1/config` responses. The current session-email API path also has good caller-side validation around recipients, archive filenames, duplicate UUIDs, request body size, local archive behavior, and credential redaction.

The remaining risk is mostly at the package boundary. `internal/email` is documented as a general-purpose mailer for current and future callers, but most validation currently lives in the HTTP handler or startup config validator, not in `Service.Send` or MIME construction. The secure default path, STARTTLS, is also untested by the package's fake SMTP server.

## Findings

### M1 - Test Coverage / Security: the default STARTTLS path is not exercised

Evidence:
- `DefaultConfig` seeds `smtp.starttls=true` for the first-run config (`internal/config/config.go:557-563`), and `types.SmtpConfig` documents STARTTLS as the default and cleartext as an explicit opt-out (`internal/types/email.go:16-19`).
- `Service.Send` has a distinct STARTTLS branch with TLS server-name verification and TLS 1.2 minimum (`internal/email/email.go:173-180`).
- The package happy-path fake deliberately disables STARTTLS because the fake server speaks plaintext only (`internal/email/email_test.go:221-230`), and the no-attachment test also omits `StartTLS`, leaving it false (`internal/email/email_test.go:283-289`).
- The API fake documents the same constraint: it does not offer STARTTLS and callers must set `StartTLS=false` (`internal/api/handler_session_email_test.go:507-512`).
- `go test ./internal/email -list .` lists nil/disabled validation, plaintext send, no-attachment, default recipient, and dial failure tests, but no STARTTLS test.

Impact:
The production default is the path with no focused test coverage. A regression in `client.StartTLS`, the TLS config, server-name handling, or the post-STARTTLS SMTP handshake would not be caught by the current package or API tests. That is both a correctness risk for operators using normal SMTP submission and a security regression risk because STARTTLS is the only encrypted transport mode in this package.

Recommendation:
Add an `internal/email` test fake that advertises STARTTLS, upgrades the connection with a test certificate, then accepts AUTH/MAIL/RCPT/DATA after TLS. Use a certificate with an IP SAN or DNS SAN matching the configured `Host`, and assert the send succeeds with `StartTLS=true`. A negative test for a non-STARTTLS server with `StartTLS=true` would also pin the intended fail-closed behavior.

### M2 - Correctness / Security: message and MIME validation is split across callers instead of owned by the mailer boundary

Evidence:
- `Service.Send` validates only `msg.To != ""` and `msg.Subject != ""` before opening the network connection (`internal/email/email.go:135-143`).
- The current API caller does stricter recipient validation and normalization with `net/mail.ParseAddress` before building `email.Message` (`internal/api/handler_session_email.go:92-107`), but that protection is not part of the `internal/email` contract.
- Config validation requires `smtp.from` to be non-empty, but does not parse it as a single mailbox or reject control characters up front (`internal/config/config.go:1234-1249`).
- `buildMimeEnvelope` writes `From` and `To` headers from raw strings, and writes attachment `ContentType` directly into a MIME header (`internal/email/email.go:231-265`). Filenames are quoted with `fmt`'s `%q`, not with MIME parameter formatting.
- The package comment says future alert paths will call the same `Send` primitive directly with their own subjects, bodies, and attachments (`internal/email/email.go:6-12`).

Impact:
Today's `/v1/session/email` path is mostly protected by API-side recipient and filename validation, and `net/smtp` rejects CR/LF in SMTP envelope addresses before DATA. The exported package boundary is still weaker than its documented future use. A future internal caller can hand `Send` an invalid recipient or malformed attachment content type and get a transport-shaped error, or generate malformed MIME, rather than a deterministic `ErrInvalidMessage` before network I/O. A malformed `smtp.from` can pass startup validation and fail only when the operator sends mail.

Recommendation:
Move the reusable validation and formatting rules into `internal/email`:
- parse `cfg.From` and `msg.To` as single mailboxes before dialing, and return `ErrInvalidMessage` for invalid input;
- reject CR/LF and other control characters in all header-bearing fields;
- validate or default attachment media types with `mime.ParseMediaType`;
- build `Content-Type` and `Content-Disposition` parameters with `mime.FormatMediaType`;
- add table tests for bad `From`, bad `To`, CR/LF-bearing fields, invalid content types, non-ASCII filenames, and defaulted content types.

Config validation should either reuse the same mailbox checks for `smtp.from` and non-empty `smtp.default_recipient`, or explicitly document that those values are validated only at send time.

### L1 - Documentation: several comments and docs still describe the old host-based mailer gate

Evidence:
- `Service.Enabled()` gates on `cfg.Enabled` (`internal/email/email.go:105-107`), and `validateSmtp` treats `Enabled=false` as the disabled state (`internal/config/config.go:1226-1236`).
- The `internal/email` package comment still says an empty `Host` disables the mailer (`internal/email/email.go:14-16`).
- `Config.Smtp` comments still say empty `Host` disables the mailer (`internal/config/config.go:143-146`).
- `cmd/smd` lifecycle comments still say `Enabled()` comes from `cfg.Smtp.Host` (`cmd/smd/main.go:122-124`, `cmd/smd/main.go:413-418`).
- The session-email handler docs and 503 message still describe the disabled case as `smtp.host empty` (`internal/api/handler_session_email.go:57-64`, `internal/api/handler_session_email.go:72-75`).
- The endpoint reference says `to` must contain `"@"`, but the handler actually requires a single `net/mail.ParseAddress` mailbox and rejects comma-separated lists and CR/LF (`docs/v2-design/api-endpoints.md:144-150`, `internal/api/handler_session_email.go:92-107`).

Impact:
This does not change runtime behavior, but it is enough to mislead a new maintainer or operator. A config with `enabled=false` and a populated host is disabled; a config with `enabled=true` and an empty host is invalid at startup. The comments currently point reviewers toward the old invariant.

Recommendation:
Update the package, config, API handler, `cmd/smd`, and endpoint-reference wording to describe the current contract: `smtp.enabled` is the kill switch; `smtp.host`, `smtp.from`, `smtp.port`, and `smtp.timeout_sec` are required only when enabled; API recipient validation accepts exactly one RFC 5322 mailbox.

## Security Review

No high-severity security issues surfaced.

Positive observations:
- SMTP password, username, host, and from address are deliberately omitted from `/v1/config`; tests assert the password, host, and from address do not appear in the response (`internal/api/handler_config_test.go:723-772`).
- `Service.Send` derives one deadline and applies it to the TCP dial and established connection, bounding SMTP reads and writes (`internal/email/email.go:145-157`).
- STARTTLS uses a `ServerName` and `MinVersion: tls.VersionTLS12` (`internal/email/email.go:173-177`).
- Go's `smtp.PlainAuth` refuses to send credentials on arbitrary plaintext connections unless the server is localhost, so the current auth path has a stdlib guard even when `StartTLS=false`.

Remaining security-sensitive gap:
- M2 should be fixed before adding new non-API callers, because the mailer package should own header and address safety rather than relying on each caller to remember the same rules.

## Performance Review

No performance findings surfaced for the current expected volume.

The one-connection-per-send model is deliberate and appropriate for "a handful of session emails per day" (`internal/email/email.go:78-86`). MIME construction buffers the whole message in memory (`internal/email/email.go:227-272`), but the current API request path is capped by the shared body-size helper (`internal/api/body.go:23-43`) and the session ADIF attachment is generated from selected QSO rows, not an arbitrary client-uploaded blob.

If this package later sends large logs or bulk exports, consider streaming MIME directly to the DATA writer and adding attachment size limits at the caller boundary. That is not needed for today's session-email path.

## Coverage Notes

Strong current coverage:
- Disabled/nil mailer behavior, missing recipient/subject, plaintext SMTP happy path, no-attachment MIME branch, default recipient, and dial-failure classification in `internal/email`.
- API session-email validation for missing fields, invalid recipients, CR/LF recipient injection, multiple recipients, display-name normalization, duplicate UUIDs, malformed JSON, no matching QSOs, SMTP failure, successful send/stamp, ADIF header ordering, local archive creation, and oversize request bodies.
- `/v1/config` mailer projection tests for nil mailer, configured mailer, credential redaction, and ignored client-supplied `mailer` blocks.

Missing focused coverage:
- STARTTLS success and fail-closed behavior in `internal/email`.
- `ErrInvalidMessage` classification for invalid `From`, invalid `To`, CR/LF-bearing header fields, and invalid attachment content types.
- MIME parameter formatting for unusual but valid attachment filenames.
- SMTP config validation for malformed `smtp.from` and non-empty malformed `smtp.default_recipient`.

## Verification

Commands run:

```text
go test ./internal/email -count=1
go test -race ./internal/email -count=1
GOCACHE=/tmp/go-build go test ./internal/api -run 'Test(SessionEmail|HandleGetConfig_Mailer|HandlePutConfig_Mailer|SafeArchiveFilename)' -count=1
GOCACHE=/tmp/go-build go test -race ./internal/api -run 'Test(SessionEmail|HandleGetConfig_Mailer|HandlePutConfig_Mailer|SafeArchiveFilename)' -count=1
GOCACHE=/tmp/go-build go test ./internal/config -run 'Test(DefaultConfig|Load|Validate|Unknown|Smtp|SMTP)' -count=1
GOCACHE=/tmp/go-build go vet ./internal/email ./internal/api ./internal/config
```

Result:
- The focused package, adjacent API, adjacent config, race, and vet checks passed.
- A first sandboxed API test run failed because the SMTP fake could not bind `127.0.0.1:0` (`socket: operation not permitted`). Rerunning the same focused API command with localhost listener permission passed.

## Resolution (2026-06-19)

All three findings fixed.

- **M1 (fixed).** Added STARTTLS coverage in `internal/email`: a self-signed
  cert (127.0.0.1 IP SAN) + a STARTTLS-upgrading test fake drive
  `TestSend_StartTLS_HappyPath` (cert verified, DATA completes over TLS), and
  `TestSend_StartTLS_FailsClosed` proves `StartTLS=true` against a non-STARTTLS
  server errors rather than falling back to plaintext. A minimal unexported
  `Service.tlsRoots *x509.CertPool` hook (nil in production → system roots) lets
  the test trust the cert; ServerName + TLS 1.2 minimum still apply.
- **M2 (fixed).** `Service.Send` now validates at the package boundary before
  any network I/O via `validateMessage`: `cfg.From` and `msg.To` must each parse
  as one `net/mail` mailbox (used as the SMTP envelope, so CR/LF can't reach
  it), the subject must be control-char-free, and attachment content types must
  be valid media types — all returning `ErrInvalidMessage`. `buildMimeEnvelope`
  now builds `Content-Type`/`Content-Disposition` params with
  `mime.FormatMediaType` (RFC 2231 for non-ASCII filenames; defaults empty →
  `application/octet-stream`). Config-side, `validateSmtp` parses `smtp.from`
  and a non-empty `smtp.default_recipient` as mailboxes at startup/PUT. Tests:
  `TestSend_RejectsInvalidInputBeforeNetwork`,
  `TestBuildMimeEnvelope_AttachmentFormatting`,
  `TestValidateSmtp_RejectsMalformedFrom`.
- **L1 (fixed).** Replaced the stale "empty Host disables the mailer" wording
  with the `smtp.enabled` kill-switch contract in `email/email.go`,
  `config/config.go`, `cmd/smd/main.go` (both spots), and the session-email
  handler doc + 503 message; updated `api-endpoints.md` so `to` is documented as
  exactly one RFC 5322 mailbox (not just "contains @").

Verified: `gofmt`/`go vet` clean; `go build ./...`; `internal/email`,
`internal/config`, `internal/api`, `cmd/smd` pass; `go test -race
./internal/email` clean.

## Worktree Note

I did not modify production code. The worktree already contained unrelated `internal/database/sqlite` edits and `docs/reviews/internal-database-2026-06-19.md`; this review adds only `docs/reviews/internal-email-2026-06-19.md`.
