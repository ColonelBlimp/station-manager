---
number: 0073
title: Add a HamQTH callsign lookup provider and an insert-only HamQTH forwarder
status: Accepted (operator-ratified 2026-08-19)
date: 2026-08-19
---

# 0073 — Add a HamQTH callsign lookup provider and an insert-only HamQTH forwarder

## Context

A backlogged dogfood item (P3) asks for a second callsign-enrichment provider as a
fallback link that catches calls QRZ does not know. HamQTH (`hamqth.com`) is a free
service that offers **both** a callsign lookup and, separately, a QSO upload API, so
one service can supply a lookup-chain provider **and** a forwarder. Its callsign
lookup (`xml.php`) is session-key authenticated (a one-hour session), and its
real-time upload (`qso_realtime.php`) takes username/password per request; a separate
whole-log endpoint (`prg_log_upload.php`) accepts an entire ADIF file and the docs
state *"you always have to upload whole log"* and *"Do NOT batch upload"* on the
realtime endpoint.

Two existing decisions bound the design. ADR 0017 makes hamnut the exclusive owner of
country/DXCC/CQ/ITU data, and the callsign-provider boundary strips those fields from
every provider result (`internal/lookup/lookup.go:116`), so a HamQTH lookup can only
contribute operator-shaped fields. ADR 0068 makes the callsign chain a priority-ordered,
fill-only accumulator, so "prioritised" is already the chain's model — a new provider
just needs a priority. Both must obey the standing invariant that enrichment and
forwarding never block logging.

The upload side is constrained by what the forwarding worker gives a forwarder. HamQTH
requires `OLD_QSO_DATE`, `OLD_TIME_ON`, `OLD_CALL`, `OLD_BAND`, and `OLD_MODE` to update
or delete a remote record, but `Forwarder.Submit` receives only the **current** QSO and
an optional upstream id — never the audit before-image
(`internal/forwarding/forwarding.go:94`, `internal/forwarding/worker/worker.go:385`).
HamQTH's realtime endpoint also returns no upstream id, only a status.

## Decision

Add two independent, config-registered components for HamQTH:

1. A **callsign lookup provider** (`internal/lookup/hamqth/`, service/source id
   `hamqthlookupservice`) that joins the ADR 0068 chain. It maps HamQTH's
   operator-detail fields only — `nick`→Name, `qth`→QTH, `grid`→Gridsquare,
   `latitude`/`longitude`→Lat/Lon, `email`→Email, `iota`→Iota, `web`→Web, `adr_*`→Address
   — and lets the provider boundary strip country/DXCC/CQ/ITU. Session-key auth against
   `xml.php`, mirroring the QRZCQ provider, with these failure semantics: a startup login
   failure **warns and does not fail `Initialize`**; authentication is retried **lazily
   after a 30-second cooldown**; a session reported expired (*"Session does not exist or
   expired"*) **clears the cached key, re-authenticates, and retries the lookup exactly
   once**; *"Callsign not found"* **wraps `errors.ErrNotFound`** (advancing the chain);
   credentialed endpoints **require HTTPS**, with loopback HTTP permitted only for tests.
   The descriptor sets `MinUsernameLen: 1` / `MinPasswordLen: 1` — presence-only
   validation (a zero minimum would accept empty credentials,
   `internal/config/config.go:1760`). Registry seeding gives it the next contiguous
   priority **disabled** (ADR 0068 / ADR 0062; `internal/config/config.go:926`) — no
   provider-specific priority.

2. An **insert-only forwarder** (`internal/forwarding/hamqth/`, type `hamqth`) that POSTs
   each new QSO to `qso_realtime.php` (username/password/`adif`/`prg`/`cmd=insert`,
   including the QSO's canonical DXCC as HamQTH recommends). It registers **only the
   Insert action**; `Submit` keeps defensive `Update`/`Delete` branches that return
   `OutcomeTerminal` so no unsupported-action row is silently queued. It registers as a
   no-bulk-backfill type (HamQTH's realtime prohibition), so the retry-only manual-upload
   workflow applies — its `skipped_no_history` **server** semantics unchanged, but the SPA
   no-bulk set and stamp/status mappings gain `hamqth` (see Consequences).
   Outcome mapping: an HTTP client error with **no response** → `OutcomeUnreachable`; a
   failure **after** a response is received (e.g. reading its body) → `OutcomeTransient`;
   `200 QSO OK` → success; `400 QSO Rejected` → `OutcomeTerminal`; `403 Forbidden` →
   `OutcomeTerminal` and trip the auth circuit-breaker; `500 Internal error` →
   `OutcomeTransient` — HamQTH conflates a server fault with invalid ADIF under 500, so
   500 is conservatively transient but stays bounded by the retry budget. Credentials
   are `{username, password, callsign?}`; the optional `callsign` is `Clearable` and blank
   means HamQTH uses the username. The constructor rejects a blank username or password;
   forwarder credential descriptors have no minimum-length field, so no additional length
   policy is imposed. It registers a default retry policy — required, since an enabled
   instance without one aborts startup (`cmd/smd/main.go:629`): 5 attempts, 60-second
   initial backoff, 1,800-second cap, over a 30-second HTTP timeout. This matches QRZ and
   ClubLog; it is not a HamQTH-published limit, and the generic worker cadence is unchanged
   because HamQTH publishes no rate limit.

The wire application name is a stable `prg=station-manager` (HamQTH forbids spaces on
lookup and a version on realtime); the full configured value is used only as the HTTP
User-Agent. Success stamps a `HAMQTH` ADIF prefix (`HAMQTH_QSO_UPLOAD_DATE` /
`HAMQTH_QSO_UPLOAD_STATUS`, standard ADIF 3.1.6 QSO fields). Because
`types.Qso` does not yet model those fields (`internal/types/qso.go:60`), the forwarder
slice must first add them to `types.Qso`, the ADIF conversion + spec tests, and the
edit-path protection **before** registering the prefix.

## Alternatives considered

### Support update and delete now (not just insert)

HamQTH's realtime API documents `update` and `delete`, so the forwarder could map all
three actions. Rejected: the worker supplies only the current QSO and an optional
upstream id, not the before-image, and HamQTH keys update/delete on the *old* QSO
tuple. After a key-field edit (e.g. a callsign correction), current-key `OLD_*` values
are wrong; a delete keyed on the current tuple could even target a different remote
record that now matches. Correct update/delete needs a generic worker/queue change that
carries an immutable remote identity or the before-image — a cross-forwarder
improvement, not a HamQTH detail. Insert-only ships a safe, independently useful subset.

### Use the whole-log endpoint (`prg_log_upload.php`) as the forwarder

It accepts a full ADIF file, but only a full one — no partial upload — and imports in
the background with an email notification. Rejected as the forwarder transport: it is
not a per-QSO real-time destination and cannot express the queue's insert/outcome
model. It remains the documented ADIF-export **backfill** path that the no-bulk-backfill
retry-only workflow points the operator at.

### Let HamQTH contribute country/DXCC/zones

HamQTH returns `adif`/`itu`/`cq`/`country`/`continent`. Rejected: ADR 0017 makes hamnut
the exclusive country source and the provider boundary already strips these. Reopening
that is an ADR 0017 decision, not a HamQTH one. (Sending the QSO's own canonical DXCC in
a *forwarding* payload is unrelated and does not touch hamnut ownership.)

### Session auth for the forwarder

The lookup is session-based, so the forwarder could reuse a session. Rejected/N-A: the
realtime endpoint authenticates with username/password per request, so the forwarder
mirrors ClubLog's per-request credentials — simpler, and no forwarder precedent uses
session re-auth.

### A provider-specific default priority

Seeding HamQTH at a fixed priority (e.g. after QRZ) was considered. Rejected: ADR 0068
already seeds a newly discovered provider at the next contiguous priority, disabled, so
an existing QRZ/QRZCQ chain naturally receives HamQTH afterward without a magic constant.

## Consequences

- The lookup adds a free fallback link that can fill QRZ-absent calls that HamQTH
  contains, contributing only
  operator-detail fields; worst-case cold/stale latency grows by one more sequential
  provider call when earlier providers leave a completion field blank (ADR 0068).
- Insert-only forwarding means later edits or deletions of a HamQTH-forwarded QSO do not
  propagate; edits and deletes require correction through HamQTH's supported operator
  workflow, and `prg_log_upload.php` is only the documented backfill route — HamQTH does
  not state that omitting a record deletes it, or that re-upload replaces an existing one.
  No update/delete rows are ever queued for the type, and defensive Terminal branches make
  a mis-queued one fail fast rather than corrupt a remote record.
- Registering the type as no-bulk-backfill reuses the `skipped_no_history` **server**
  semantics, but the SPA retry-only surface is NOT capability-driven today — it hardcodes
  `clublog` in the no-bulk set (`frontend/app/src/lib/logbook/logbook.svelte.ts:31`), only
  QRZ/ClubLog in the stamp-field map (`uploadStatus.ts:46`), and no HamQTH status field in
  the upload parse (`api/logbooks.ts:62`). The forwarder slice must add `hamqth` to those
  three mappings and their tests, unless a follow-up makes forwarder capabilities
  server-driven.
- The forwarder slice must extend `types.Qso` with the two `HAMQTH_QSO_UPLOAD_*` ADIF
  fields (two fields carried through `additional_data`, per the ADIF-mirror
  invariant) plus their conversion/spec tests and edit-path protection before the prefix
  is registered — otherwise a success stamp would have nowhere to live.
- Lookup and forwarder are independent config surfaces (`lookup.chain[]` and
  `forwarders[]`); an operator using both enters the same HamQTH account credentials in
  each. Both remain best-effort: a HamQTH outage degrades enrichment completeness and
  defers uploads, never blocking logging.

## Triggers to revisit

- If the forwarding worker gains an immutable remote identity or an audit before-image,
  enable HamQTH `update` and `delete` (register the additional actions; drop the
  defensive Terminal branches).
- If HamQTH publishes rate limits or minimum credential lengths, add worker-pacing
  defaults and descriptor validation to match.
- If operators need HamQTH's country/DXCC/zone data, reopen ADR 0017 first — this
  decision deliberately preserves hamnut ownership.
- If the realtime endpoint proves unreliable at real volume, reconsider a paced or
  whole-log strategy that still honours HamQTH's no-batch rule — not unbounded retry
  against a rejecting host.

## References

- ADR 0017 — enrichment pipeline, hamnut country ownership, callsign-provider field
  boundary.
- ADR 0038 — forwarder `Outcome` model (Unreachable/Transient/Terminal) and retry
  lifecycle.
- ADR 0062 — self-registering lookup providers and disabled registry seeding.
- ADR 0068 — prioritised, fill-only callsign chain; next-available-disabled priority
  seeding.
- HamQTH developer documentation — `https://www.hamqth.com/developers.php` (session auth,
  `xml.php` lookup fields, `qso_realtime.php` actions/statuses/old-key requirements, the
  realtime no-batch prohibition, and `prg_log_upload.php` whole-log upload).
- ADIF 3.1.6 — `https://www.adif.org/316/ADIF_316.htm` (upload-status QSO fields).
- `internal/lookup/lookup.go:116` (provider country-field boundary),
  `internal/forwarding/forwarding.go:94` + `internal/forwarding/worker/worker.go:385`
  (Submit has no before-image), `internal/config/config.go:926` (next-priority-disabled
  seeding), `internal/types/qso.go:60` (`types.Qso` does not yet model the upload-status
  fields).
