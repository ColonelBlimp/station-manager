---
number: 0095
title: Session exports and emails report exactly what they delivered
status: Accepted
date: 2026-10-23
---

# 0095 — Session exports and emails report exactly what they delivered

## Context

In B2-10 of the alpha.4 acceptance run, a Session panel of four rows downloaded an ADIF file
holding one record. The toast said "Exported 4 QSOs to ADIF". The daemon's shared fetch
(`fetchSessionQsos`) skips UUIDs it cannot find with only a log warning. The SPA's toast counts
the panel, including rows without a UUID that it never sent. Email shares the silent skip. Its
`emailed` list reports rows durably stamped, not records in the attachment, so a send followed by
a failed stamp reads as fewer, or none, sent. alpha.4 is held on this (Finding #6). The policy
for partial results was ruled by Codex at the operator's request.

## Decision

Deliver what can be found and say exactly what was delivered. The daemon reports requested,
delivered and omitted counts (and for email, the sent UUIDs separately from `emailed`). The SPA
builds every message from those, against a snapshot of the selection taken at the click. It
discloses rows it could not send, and never claims success from absent or inconsistent counts.
All-missing remains `no_qsos`.

## Alternatives considered

### Refuse any request with a missing row

No file and no email unless every row resolves. Rejected: a row can legitimately stop resolving
(for example a Logbook deletion), and a refusal would then block that Session's export or email
until the operator found and removed the row. A labelled partial result keeps the operator's
work usable without hiding the gap.

### Fix only the toast

Count the response somehow but leave the daemon's skip silent. Rejected: the daemon alone
knows what it omitted, and a corrected count over an unexplained short file is the same failure
with a different number.

### Count records in the downloaded ADIF in the browser

Rejected as the primary source: the daemon composes the file and knows its counts exactly, and
email has no body to count. The SPA still validates the daemon's numbers for consistency.

### Remove omitted rows from the Session list after an incomplete result

Rejected: an export or email must not silently change the Session list. Omitted rows stay
listed, and every later attempt repeats the warning.

## Consequences

`POST /v1/session/export` gains count headers, and `POST /v1/session/email` gains `sent` and
`omitted`. Both require the expected-archive header (ADR 0094 update). Duplicate selections are
reported as collapsed rather than missing. An email response must partition the submitted UUIDs
exactly before any row is marked. `api-endpoints.md` changes
with them. Messages become longer when anything is short, with every part named: could not be
found in this archive, no QSO ID, or not recorded as emailed. "Could not be found" is the
wording throughout, because a missing row does not prove deliberate deletion. The ambiguous-send
handling is unchanged. No implementation is authorised by this ADR beyond the reviewed design in
W-0021.

## Triggers to revisit

- Operators ask for a strict mode that refuses incomplete sends (for example, before submitting
  to a contest sponsor).
- Session rows gain a durable, daemon-owned identity (ADR 0049 revisited), making client
  selection accounting unnecessary.

## References

- [alpha.4 acceptance record](../reports/dogfood-acceptance-v2.0.0-alpha.4.md), B2-10 and Finding #6.
- [ADR 0094](0094-retain-session-lists-per-archive-within-each-tab.md), Session isolation.
- [W-0021](../work/W-0021-qso-archives.md), "alpha.4 hold: fix design".
- `internal/api/handler_session_export.go`, `handler_session_email.go`; `frontend/app/src/lib/operate/ExportDialog.svelte`.
