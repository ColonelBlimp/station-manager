# Internal API and wire-contract audit

**Status:** review complete; actions open  
**Reviewed:** 2026-08-14  
**Scope:** daemon HTTP routes, JSON/ADIF request boundaries, response and error
envelopes, pagination and request limits, general/rig/FT8 SSE contracts, SM Cloud
HTTP ingest/export contracts, and the internal types serialized across those
boundaries; frontend code read only to identify current consumers  
**Code changes:** none; this document is the review deliverable

## Executive summary

The HTTP surfaces have a strong base. Daemon bodies are read through one bounded
helper that correctly distinguishes 413 from ordinary read errors; JSON decoding
rejects malformed and trailing documents; QSO pagination is forward-cursor based,
existence-aware, and capped; the large operator-driven request paths have explicit
row caps; handler 5xx responses do not expose internal causes; SSE subscription and
slow-reader behavior are explicit; and SM Cloud export is streamed from one snapshot
behind its own connection-preserving concurrency gate.

The review found **six action themes**, all P2. There is no new P0 or P1 issue.
The most consequential behavior defects are false-success command requests and a
QSO PATCH documented as a no-op that actually advances the row revision, appends
history, emits an event, and can re-arm forwarding. The remaining work completes
the already-planned UUID wire migration, brings router-generated errors inside the
JSON contract, fixes SM Cloud's oversized-body classification, and puts a row-count
bound on cloud QSO batches.

Four overlay-only probes reproduced the current behavior. They were removed after
the run. No production source was changed.

## Findings at a glance

| ID | Priority | Area | Disposition |
|---|---:|---|---|
| AW-1 | P2 | Canonical UUID migration stopped at paths while response/SSE contracts retain local QSO ids | Known gap whose stated trigger has arrived |
| AW-2 | P2 | Missing or misspelled command booleans execute the `false` operation and return 202 | New |
| AW-3 | P2 | Empty/no-effective-change QSO PATCHes create real mutations | New |
| AW-4 | P2 | Router-generated 404/405 responses bypass the JSON error contract | New |
| AW-5 | P2 | SM Cloud reports oversized JSON as 400 and exposes decoder detail | New on the cloud surface; same class already fixed on the daemon |
| AW-6 | P2 | SM Cloud QSO batches have no row-count cap | New |

Priority meanings follow the other internal reviews: P0 is release-gate work, P1
should be closed before a serious release, P2 is important correctness or
operability work, and P3 is useful hardening.

## AW-1 — canonical UUID migration stopped at paths, not wire identities (P2)

ADR 0016 says the SQLite integer is a storage detail, the submit `id` survives for
one release only, and SSE gains `qso_uuid` when a live consumer appears at
[`docs/decisions/0016-sm-cloud-deferred-with-prep.md:101`](../decisions/0016-sm-cloud-deferred-with-prep.md).
The path migration did land: QSO fetch/edit/delete routes use UUID. The rest of the
external shape did not:

- `types.Qso.ID` remains `json:"id,omitempty"`, so every full QSO response and
  the SM Cloud `types.Qso` payload can carry the local row id at
  [`internal/types/qso.go:19`](../../internal/types/qso.go);
- `SubmitResult.ID` is still unconditional at
  [`internal/qsoservice/service.go:78`](../../internal/qsoservice/service.go);
- contact-history items still contain both integer ID and UUID at
  [`internal/types/history.go:3`](../../internal/types/history.go);
- all five general event payloads contain only `qso_id` at
  [`internal/events/events.go:35`](../../internal/events/events.go); and
- the cloud forwarder embeds the full `types.Qso` in its payload at
  [`internal/forwarding/smcloud/smcloud.go:181`](../../internal/forwarding/smcloud/smcloud.go).
  Restore explicitly zeros the old local id because it is not portable at
  [`internal/qsoservice/restore.go:72`](../../internal/qsoservice/restore.go),
  which is direct evidence that it does not belong in a durable cross-instance
  payload.

The condition that justified deferral is no longer true. The consolidated SPA now
opens `/v1/events` and rejects QSO event payloads without a numeric `qso_id` at
[`frontend/app/src/lib/api/log-events.ts:19`](../../frontend/app/src/lib/api/log-events.ts).
Its logbook row type makes `id` required and UUID optional at
[`frontend/app/src/lib/api/logbooks.ts:28`](../../frontend/app/src/lib/api/logbooks.ts),
and selection is keyed by the integer at
[`frontend/app/src/lib/logbook/logbook.svelte.ts:193`](../../frontend/app/src/lib/logbook/logbook.svelte.ts).
Meanwhile the still-current API note says there is no live SSE consumer at
[`docs/v2-design/api.md:214`](../v2-design/api.md).

This is not an immediate data-loss defect: current path operations use UUID, and
the SPA's integer-keyed selection is ephemeral. It is nevertheless a contract
fault. Local ids can change after restore or database reconstruction, the backup
payload carries instance-specific noise, and every release that leaves the live
consumer on `qso_id` makes the promised removal more disruptive.

### Action

Complete the migration additively:

1. Add `qso_uuid` to `qso.*` and `forward.*` event payloads, populate it at every
   publisher, and make current clients consume UUID while tolerating the old field
   during one explicit compatibility window.
2. Make UUID required on QSO-facing frontend row types and key selection/rendering
   by it. Do not replace `logbook_id`; logbooks still deliberately use local ids.
3. Introduce an API projection for full QSO responses rather than changing
   `types.Qso` tags in place. That type is also the SQLite additional-data and cloud
   payload shape, so a tag-only cleanup would silently change several persistence
   contracts at once. The external projection should omit local `id`, derived
   `dedupe_key`, and any other server-only fields not deliberately promised.
4. Remove `SubmitResult.ID`, contact-history `id`, and the legacy event field after
   the migration window. Clear the local QSO id before cloud serialization, or use a
   dedicated cloud payload projection, so backups are stable across instances.
5. Update ADR/API text to name the actual compatibility version or removal release;
   “one release” without a recorded start/end has allowed the shim to become
   permanent.

### Required tests

Pin additive old/new event decoding, UUID presence on every QSO and forwarding event,
absence of local ids/dedupe keys from public and cloud payloads, UUID-keyed selection
across pages, and a restore into differently numbered local rows. The restored and
freshly backed-up logical payload must not vary merely because SQLite allocated a
different primary key.

## AW-2 — absent command booleans execute `false` and return success (P2)

The shared JSON helper converts an empty body to `{}` and uses ordinary
`json.Unmarshal` at
[`internal/api/httpkit/httpkit.go:133`](../../internal/api/httpkit/httpkit.go).
That makes an omitted boolean indistinguishable from an explicit `false`, and unknown
keys are ignored. Its comment says callers needing an empty-body error should test
before calling, but callers cannot do that: the helper itself consumes the body.

Most request handlers subsequently validate required strings/numbers and turn this
into a 400. Three state-control endpoints act directly on a value-typed boolean:

- rig tune calls `StopTune` when `active` is absent at
  [`internal/api/handler_rig_tune.go:15`](../../internal/api/handler_rig_tune.go);
- FT8 TX arm calls `ArmTx(false)` when `armed` is absent at
  [`internal/api/handler_ft8_tx.go:14`](../../internal/api/handler_ft8_tx.go); and
- FT8 skip calls `SetQsoSkip(false)` at
  [`internal/api/handler_ft8_qso.go:539`](../../internal/api/handler_ft8_qso.go).

An empty tune request or `{"actve":true}` therefore stops tune and returns 202.
An empty FT8-arm request or `{"armd":true}` disarms TX and returns 202. The skip
route has the same decode contract; disarm is deliberately accepted while idle.

This fails safe with respect to RF—it stops/disarms rather than starting a
transmitter—which is why it is P2 rather than P1. It still acknowledges an operation
the caller did not express, hides client schema typos, and makes 202 “applied” false
as an intent contract.

Global strict decoding is not a drop-in fix. Two FT8 request types deliberately
tolerate the retired `auto_work` key from older clients at
[`internal/api/handler_ft8_qso.go:137`](../../internal/api/handler_ft8_qso.go) and
[`internal/api/handler_ft8_qso.go:419`](../../internal/api/handler_ft8_qso.go).
That compatibility exception should be explicit rather than setting the policy for
every command body.

### Action

Make command booleans presence-aware (`*bool` or a small custom request decoder) and
return 400 `missing_required_field` when the named field is absent. Split the body API
into honest operations such as `ReadJSONRequired` and, if needed, an explicitly
optional-object helper; remove the impossible “caller should test first” contract.

For new command endpoints, reject unknown fields. For compatibility-bearing requests,
decode a declared legacy alias/allow-list and reject every other unknown key. Optional
booleans with a documented default, such as backfill `force`, can remain value-typed
but should have that default pinned in their endpoint tests.

### Required tests

For tune, TX arm, and skip, cover empty body, `{}`, misspelled key, explicit false,
explicit true, duplicate JSON keys, and an unrelated unknown key. Empty/missing/typo
must not invoke the service and must return the same stable 400 envelope. Retain a
separate compatibility test proving the retired FT8 key is accepted only on the two
routes that promise it.

## AW-3 — empty QSO PATCHes create real mutations (P2)

The canonical endpoint reference calls an empty QSO PATCH a no-op at
[`docs/v2-design/api-endpoints.md:66`](../v2-design/api-endpoints.md). The handler turns
the empty body into `{}` and always invokes the update service at
[`internal/api/handler_qso.go:92`](../../internal/api/handler_qso.go). After merge,
normalization and validation, `qsoservice.Update` has no equality/no-fields exit. It
always:

- writes the QSO, causing `modified_at` and `revision` to advance;
- re-arms every enabled update-capable forwarder
  ([`internal/qsoservice/update.go:316`](../../internal/qsoservice/update.go));
- appends a `qso_history` update row
  ([`internal/qsoservice/update.go:332`](../../internal/qsoservice/update.go)); and
- emits `qso.updated`
  ([`internal/qsoservice/update.go:346`](../../internal/qsoservice/update.go)).

The same happens for `{}`, a body containing only unknown keys, an immutable-only
patch whose values are restored, and values that normalize back to the stored state.
This creates false audit history, version churn and external forwarding work for a
request whose response body appears unchanged. It also makes a client retry or stray
Save click look like a real operator edit.

### Reproduction

An overlay-only API test submitted one QSO, recorded its revision, sent an empty PATCH,
then fetched the row and history. The request returned 200, revision advanced by one,
and one update-history row existed. The probe asserted current behavior and was
removed after the run.

### Action

Define no-effective-change semantics at the service boundary, because API is not the
only possible caller. A request with no editable fields should either:

- return 400 for an empty/unknown-only body; or
- retain the currently documented 200 no-op and return the existing row without
  opening a transaction, enqueueing, appending history, logging “QSO updated,” or
  publishing an event.

The second option is the least disruptive. Compare an explicit editable-field
projection after normalization and immutable restoration; do not use whole-struct
equality, because QSO contains storage/enrichment fields outside the edit contract.
Treat a syntactically present but canonically equivalent edit as the same no-op.

### Required tests

Cover an empty byte body, `{}`, unknown-only JSON, immutable-only JSON,
case/whitespace/canonical-frequency equivalence, and a real edit. For every no-op,
assert unchanged revision/modified time, no history, no upload re-arm, no event, and
no “QSO updated” record. The real edit must retain all existing atomicity and revision
CAS tests.

## AW-4 — router-generated 404/405 responses bypass the JSON contract (P2)

The endpoint reference says non-2xx responses use the stable JSON envelope at
[`docs/v2-design/api-endpoints.md:24`](../v2-design/api-endpoints.md). Handler and
middleware failures do, through `httpkit.WriteError`, which also gives the access log
its error classification at
[`internal/api/httpkit/httpkit.go:73`](../../internal/api/httpkit/httpkit.go).

Unknown paths and method mismatches are produced by `http.ServeMux` itself. The daemon
registers only method-qualified patterns at
[`internal/api/server.go:198`](../../internal/api/server.go), and SM Cloud does the
same at
[`internal/cloud/server/server.go:73`](../../internal/cloud/server/server.go).
Neither installs an API not-found/method-not-allowed boundary. The result is stdlib
plain text, not JSON, for both surfaces. Daemon access logging records the status and
bytes but receives no `code`/`message`/`op`, defeating the enriched-error path at
[`internal/api/middleware.go:341`](../../internal/api/middleware.go).

This includes intentionally gated subsystem routes while disabled: the documented
404 status is correct, but its body still violates the stated contract. Non-API
static/manual/pprof 404s need not be normalized.

### Reproduction

An overlay-only test sent `GET /v1/restart` and `GET /v1/no-such-route` through the
real daemon middleware/mux. They returned 405 and 404 respectively, both
`text/plain` and neither decodable as the error envelope. The probe was removed.

### Action

Give both API routers explicit JSON fallbacks under `/v1/`. Preserve correct HTTP
semantics: method mismatch should remain 405 with an accurate `Allow` header and a
stable code such as `method_not_allowed`; unknown or disabled routes should be 404
`not_found`. A small route-registration helper can register each path's methodless
fallback alongside its real method without introducing another router dependency.

Route those errors through the ordinary writer so daemon access logs retain their
classification. Keep the fallback scoped to `/v1/` so browser/static 404 behavior is
unchanged.

### Required tests

Table-test known path/wrong method and unknown path on daemon and cloud, including a
disabled bridge/FT8 route. Assert status, content type, exact envelope fields, `Allow`,
and access-log classification. Retain a control proving an unknown manual/static path
keeps its non-API behavior.

## AW-5 — SM Cloud maps oversized JSON to 400 and returns decoder detail (P2)

The daemon's shared reader detects `*http.MaxBytesError`, returns 413
`body_too_large`, and hides reader detail at
[`internal/api/httpkit/httpkit.go:102`](../../internal/api/httpkit/httpkit.go).
SM Cloud independently decodes through `http.MaxBytesReader`, but maps every first
decode error to 400 `invalid_body` and concatenates `err.Error()` into the response:

- QSO ingest at
  [`internal/cloud/server/server.go:219`](../../internal/cloud/server/server.go); and
- evidence ingest at
  [`internal/cloud/server/evidence.go:24`](../../internal/cloud/server/evidence.go).

The second “exactly one document” decode also collapses any size error into a generic
400. This makes a transport limit look like malformed syntax, returns implementation-
specific decoder text, and leaves the two Station Manager HTTP surfaces with different
classification for the same failure.

### Reproduction

An overlay-only cloud test sent an unterminated JSON string just beyond the 32 MiB cap
to each ingest handler. Both returned 400 and their client-visible message contained
`http: request body too large`. The focused cloud probe passed and was removed.

### Action

Extract one cloud JSON-body decoder used by both handlers. It should:

- recognize `*http.MaxBytesError` on either decode and return 413
  `body_too_large` with a generic message;
- preserve exact-one-document enforcement;
- return generic 400 `invalid_body` for syntax/type errors while logging any detail
  needed for diagnostics; and
- close the body consistently.

Use the daemon's current `body_too_large` vocabulary. The older design brief and
frontend narrative still say `payload_too_large` at
[`docs/v2-design/api.md:687`](../v2-design/api.md) and
[`docs/v2-design/frontend-spa.md:821`](../v2-design/frontend-spa.md); update them in
the same change so the canonical reference and client guidance agree.

### Required tests

For both endpoints, cover oversize during the first decode, oversize trailing content
during the second decode, malformed JSON, two documents, empty body, and a boundary-
sized valid body. Assert that no store/provisioning method is called on every rejected
request and that error bodies never include the raw decoder string.

## AW-6 — SM Cloud QSO batches have no row-count cap (P2)

SM Cloud accepts a 32 MiB QSO envelope at
[`internal/cloud/server/server.go:21`](../../internal/cloud/server/server.go), decodes
the entire `qsos` array, allocates a second `store.Record` slice sized directly from
the caller's count, and validates every member at
[`internal/cloud/server/server.go:241`](../../internal/cloud/server/server.go).
There is no maximum `len(req.Qsos)`.

A minimally shaped accepted row needs little more than UUID and `modified_at`, so the
body cap permits hundreds of thousands of entries. `store.Upsert` then holds one
transaction and executes one prepared statement per row at
[`internal/cloud/store/store.go:183`](../../internal/cloud/store/store.go). Several
authenticated accidental/hostile requests can therefore pin all database connections
and consume substantially more heap and CPU than the 32 MiB wire size suggests.
The global request semaphore limits concurrency, not per-request work.

The adjacent evidence endpoint already applies the right policy: its real client sends
500 rows and the server caps at 1,000 at
[`internal/cloud/server/evidence.go:12`](../../internal/cloud/server/evidence.go).
The current daemon SM Cloud forwarder sends one QSO per request, so a generous QSO cap
will not affect the shipped path.

### Action

Choose and document a `maxQsoBatchRows` based on the intended restore/backfill client
(1,000 or 5,000 is already far above the current one-row producer). Reject a larger
array before allocating `recs`, provisioning a logbook, or beginning a transaction,
using one stable `batch_too_large` classification. Keep the byte cap as the independent
bound on large individual records.

If future bulk backup throughput justifies bigger batches, prefer chunking client-side
and keeping transactions bounded rather than raising this limit toward full-logbook
size. Also reconcile the current comment claiming 32 MiB is for full-logbook pushes
with the actual one-row producer.

### Required tests

Cover exactly-at-cap and cap-plus-one, prove an over-cap request provisions no logbook
and calls no store method, and run the accepted maximum against Postgres with a bounded
deadline. Retain a one-row producer integration test so a future envelope change cannot
make the ordinary forwarder incompatible.

## Cross-review ownership

- PT-1 owns equal-version divergent cloud payloads and the `Applied` acknowledgement
  semantics; AW-6 only bounds batch work.
- PT-2 owns stale QSO delete/audit ordering; AW-3 concerns a PATCH with no effective
  field change.
- PT-3 owns the session-email snapshot/stamp race; the session request size and UUID
  count caps themselves passed this review.
- CC-1/CC-4 own config unknown-field preservation and no-op persistence. AW-2 does not
  prescribe global strict decoding for `/v1/config`.
- EH findings continue to own internal error propagation and cleanup. AW-5 is the
  client-visible HTTP classification at the cloud boundary.

## Positive controls and non-findings

- Daemon `ReadBody` correctly identifies `*http.MaxBytesError`, bounds every body
  reader using it, and returns generic client text.
- `json.Unmarshal` on daemon JSON bodies rejects trailing documents. Both cloud ingest
  handlers also explicitly require exactly one document.
- Required non-boolean request fields are generally validated after decode; empty
  create/session/rig-command/FT8-start requests correctly become 400s rather than
  useful zero-value operations.
- QSO page `limit` rejects non-positive/non-numeric values, clamps to the configured
  maximum, fetches only `limit+1`, and validates opaque cursors at
  [`internal/api/handler_qso_list.go:130`](../../internal/api/handler_qso_list.go).
- Manual forwarder upload, session email/export, rig command and evidence ingest all
  have explicit row/command caps.
- The intentionally lenient FT8 antenna-path route is logging-only, cannot key RF,
  and documents that every non-long value means short. It is not grouped with AW-2.
- Handler/server errors use stable codes and generic 5xx messages; underlying causes
  are logged rather than sent to daemon clients.
- General SSE subscribes before its initial response, has bounded subscribers and
  per-subscriber buffers, disconnects slow consumers instead of silently dropping
  events, and documents that `Last-Event-ID` is not replayable.
- Cloud export streams one repeatable-read snapshot, caps simultaneous exports below
  the database pool size, and extends/bounds its write deadline.

## Verification

The review mapped all routes registered in `internal/api/server.go` and
`internal/cloud/server/server.go`, then traced their body readers, response writers,
query parsing, DTOs, publishers and current frontend consumers.

Overlay-only probes covered:

- daemon 404/405 content type and envelope;
- empty/misspelled tune and FT8-arm commands;
- QSO empty-PATCH revision/history side effects; and
- oversized QSO/evidence cloud bodies.

After the probes, the full focused packages passed:

```text
go test ./internal/api -count=1          ok (9.305s)
go test ./internal/cloud/server -count=1 ok (0.006s)
```

During the review, unrelated concurrent evidence-archive work changed
`evidence.Status.UsageBytes` to `*int64` while
`internal/api/handler_evidence_test.go` still compared it directly with zero. A
temporary compile-only adjustment was used for the API run and removed immediately;
that existing test mismatch and the concurrent `cmd/smd`/`internal/evidence` changes
were not altered or reviewed as part of Item 6.

## Recommended action order

1. **AW-2 and AW-3** — close false-success/no-op mutation behavior before client bugs
   generate misleading state or audit history.
2. **AW-5 and AW-6** — harden the authenticated cloud ingest boundary with one shared
   decoder and bounded batch work.
3. **AW-4** — make all `/v1/` failures parseable and observable through the existing
   envelope/access-log contract.
4. **AW-1** — ship the UUID additions first, migrate the live consumer, then remove
   the legacy ids under an explicit compatibility version.

