# Station Manager — Daemon HTTP API Endpoint Reference

> **Status:** canonical, complete endpoint list (audited 2026-06-14). This is the
> **reference**; the rationale/decision narrative lives in [api.md](api.md) (the API
> *design brief*). When the two disagree, the **handler source is authoritative** —
> routes are registered in `internal/api/server.go`, handlers in
> `internal/api/handler_*.go`. Update this file in the same commit as any route change.

All application endpoints live under the `/v1/*` prefix (API versioning — unrelated to
the project's v1/v2 distinction). The consolidated operator SPA (ADR 0044, the sole
embedded client) is served at the **canonical root** `/`: `GET /` and every unmatched
path fall through to its `index.html`, so `/operate`, `/logbook`, `/config`, and
deep-link reloads are client-side routes (W-0003). The retired `/app/` transition mount's
URLs **301-redirect** to their root equivalents (`/app`,`/app/` → `/`; `/app/{path}` →
`/{path}`, query preserved) for saved bookmarks. The operator manual is at `/manual/`;
profiling at `/debug/pprof/*`.

## Conventions

**Transport / auth.** Single-operator, loopback-bound by default (`127.0.0.1:8080` TCP, or
a Unix socket). No authentication — the trust boundary is the loopback interface. A
non-loopback bind emits a startup warning.

**Content negotiation.** JSON in/out except `POST /v1/qso`, which takes ADIF
(`application/x-adif` / `text/plain` / empty).

**Error envelope.** Non-2xx JSON responses use `{"code": string, "message": string,
"op": string}` (`response.go` `ErrorResponse`). `code` is a stable machine string (the SPA
renders it via its i18n catalogue — see ADR 0010); `message` is human-readable; 5xx bodies
carry only a generic message (the real cause is logged server-side). Some subsystems
(bridge, FT8 errors over SSE) carry a richer `{code, details}` shape.

**Middleware chain** (outermost first, `internal/api/limits.go`):
`logRequests` → `limitConcurrent` (non-SSE concurrent-request semaphore; over budget →
**503 `server_busy`**; the SSE routes are exempt) → `recoverPanic` (panic → **500
`internal_error`**) → mux. Body-reading endpoints cap at `Server.MaxBodyBytes`
(**413 `body_too_large`** / **400 `read_error`**); malformed JSON → **400 `invalid_json`**.
`POST /v1/qso` additionally has a per-endpoint token bucket (**429 `rate_limited`**). The
three SSE streams share one subscriber cap (**503 `server_busy`**).

**SSE frame format.** `/v1/events` emits `id: <n>\nevent: <name>\ndata: <json>\n\n`;
`/v1/rig/events` and `/v1/ft8/events` omit the `id:` line. All three send a `: keepalive`
comment every 30s, clear/re-arm write deadlines so long-lived streams survive
`WriteTimeout`, and return promptly on graceful shutdown.

**Gating.** "Always-on" = registered whenever the server runs. Subsystem routes are
registered only when their subsystem is enabled (bridge / FT8 / profiling / SPA); when
unregistered, the path is a **404**. Since W-0003 the SPA IS the root catch-all (`GET /`),
so an unmatched *non-namespace* path serves `index.html` for client routing — but the
server namespaces stay honest 404s: an unmatched `/v1/*` or a profiling-off `/debug/pprof*`
is explicitly `404`ed, never SPA HTML. A headless (non-TCP or `ServeSPA` off) daemon
registers no SPA routes, redirects, or root mount at all, so those paths `404` there too.

---

## QSO

### `POST /v1/qso`
- **Purpose:** Submit exactly one QSO as ADIF — the primary logging write path (consolidated app, `curl`, import tooling). Bulk import is a separate tool (`cmd/importer`), not this endpoint.
- **Gating:** Always-on; additionally wrapped in `limitSubmitRate`.
- **Request:** Content-Type `application/x-adif` / `text/plain` / empty (other → 415 `unsupported_media_type`). Body = a single-record ADIF doc. Query: `logbook` (int, **required**, ≥1; must exist), `force` (bool, optional — bypass dedupe). **`STATION_CALLSIGN` is derived from the logbook** (ADR 0055): the daemon stamps the QSO with the logbook's own callsign, so a record's own `STATION_CALLSIGN` is ignored on a live submit and need not be present (an **import** keeps the record's value for historical fidelity).
- **Response:** **201** on store / **200** on duplicate. Body `{"status": "stored"|"duplicate", "uuid": string, "id": int64}`.
- **Errors:** 400 `invalid_adif`, `too_many_records`, `missing_required_field`, `invalid_field_value`, `missing_required_param`, `invalid_id`, `invalid_query_param`, `invalid_time_range`; 404 `logbook_not_found`; 429 `rate_limited`; 503 `server_busy`; 500 `submit_failed`/`db_error`.
- **Notes:** One-fails-all-fail atomic write (QSO row + upload-queue rows in one tx). Idempotent dedupe — a known duplicate returns 200 with the existing UUID/ID. **Dedupe is minute-precision** (`CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON`, TIME_ON truncated to HHMM), so two contacts in the same minute collide — even though times are **stored and exported at full `HH:MM:SS`** where supplied (ADIF `HHMMSS` preserved since migration 0003; seconds matter to QSL managers matching on exact timestamp). A **midnight-crossing** QSO (`TIME_ON` > `TIME_OFF`) must carry `QSO_DATE_OFF` on the **following day** or it's rejected `invalid_time_range`; SM's own clients (consolidated app + FT8 daemon) **always populate `QSO_DATE_OFF`** — the date at `TIME_OFF`, equal to `QSO_DATE` for a same-day contact.

### `GET /v1/qso/{uuid}`
- **Purpose:** Fetch one QSO by canonical UUIDv7 (SPA edit/detail).
- **Gating:** Always-on.
- **Request:** Path `{uuid}` (UUIDv7).
- **Response:** **200**, body = full `types.Qso`. Excludes soft-deleted rows.
- **Errors:** 400 `invalid_uuid`; 404 `not_found`; 500 `db_error`.

### `PATCH /v1/qso/{uuid}`
- **Purpose:** Update fields of an existing QSO (SPA edit flow).
- **Gating:** Always-on.
- **Request:** Path `{uuid}`. JSON body overlaid on the existing QSO; immutable fields (ids, upload statuses) are stash-restored. A body with **no effective change** — empty, `{}`, unknown-only, immutable-only, or values that normalize back to the stored ones — is a **true no-op**: it returns the existing row unchanged, with no revision/modified bump, audit row, forwarder re-arm, or `qso.updated` event (AW-3, decided at the service boundary on an editable-field projection).
- **Response:** **200**, body = updated `types.Qso`.
- **Errors:** 400 `invalid_uuid`/`invalid_json`/`missing_required_field`/`invalid_field_value`/`invalid_time_range`; 409 `duplicate_key` (dedupe collision) / `edit_conflict` (the QSO changed since this request fetched it — revision CAS; re-fetch and re-apply); 404 `not_found`; 500 `update_failed`/`db_error`.

### `DELETE /v1/qso/{uuid}`
- **Purpose:** Soft-delete a QSO and enqueue a delete-forward.
- **Gating:** Always-on.
- **Request:** Path `{uuid}`.
- **Response:** **204**.
- **Errors:** 400 `invalid_uuid`; 404 `not_found` (missing or already soft-deleted); **409 `delete_conflict`** (the QSO changed since this request fetched it — revision CAS, parallel to `edit_conflict`; re-fetch and retry); 500 `db_error`.
- **Notes:** Soft-delete + upload-queue delete row + audit-history row, all atomic. The delete is revision-guarded (ADR 0050): it removes only the row still at the fetched revision, and the audit `before_image` is the authoritative pre-delete state read inside the transaction. A stale delete racing a concurrent edit is refused (`delete_conflict`), never applied with a stale history preimage (PT-2).

### `GET /v1/qso/{uuid}/uploads`
- **Purpose:** Per-forwarder upload-status snapshot for one QSO (pull counterpart to the `forward.*` SSE events).
- **Gating:** Always-on.
- **Request:** Path `{uuid}`.
- **Response:** **200**, body `{"items": [types.QsoUpload, …]}` (never null; ordered by forwarder_name, action). Includes soft-deleted QSOs (delete forwarding stays observable).
- **`origin` (added 2026-08-01, migration 0007):** each item carries `origin` — **what CAUSED the queue entry to exist**, distinct from `action`, which says what mutation is being forwarded. One of `live` · `import` · `edit` · `manual` · `stamp_sync` · `reconcile` · `legacy`. A QSO the operator deleted is `action: "delete", origin: "edit"`; a reconcile repairing that same missed delete re-enqueues the row as `action: "delete", origin: "reconcile"`. `legacy` marks rows that pre-date the column and is never assigned by a producer. **Additive and backward-compatible**, and deliberately **not** `omitempty`: an absent field and an unknown provenance must not look the same, so the key is always present and never empty. A re-enqueue by a different cause REPLACES origin (an ordinary retry does not).
- **Errors:** 400 `invalid_uuid`; 404 `not_found`; 500 `db_error`.

### `POST /v1/forwarder/{name}/uploads`
- **Purpose:** Manual upload backfill (ADR 0039) — queue already-stored QSOs for upload to one **enabled** forwarder. The load-bearing path for QSOs logged while a forwarder was disabled, pre-dating it, or otherwise never auto-queued; the logbook SPA selects rows and pushes them to one destination at a time. The existing per-destination worker drains the rows.
- **Gating:** Always-on (the destination must itself be enabled — see Errors).
- **Request:** Path `{name}` (the forwarder's config name). Body `{"uuids": ["…", …], "force": false}`. `uuids` are deduplicated; `force` (default false) re-sends QSOs already uploaded to this destination instead of skipping them.
- **Behaviour:** action is always `insert`. Each UUID lands in exactly one bucket — enqueued, `skipped_uploaded` (already on this destination, no force), `skipped_deleted` (soft-deleted; not backfilled), or `not_found` (unknown/malformed). Per-QSO best-effort: one bad UUID never fails the rest. "Already uploaded" = an existing insert-upload row at status `uploaded` for this forwarder (keyed by name, so N-agnostic across forwarder types).
- **Response:** **200**, body `{"enqueued": N, "skipped_uploaded": M, "skipped_deleted"?: ["…"], "not_found"?: ["…"]}`.
- **Errors:** 400 `invalid_forwarder` (empty name); 400 `missing_required_field` (empty `uuids`); 400 `batch_too_large` (> 5000 uuids); 400 `forwarder_unavailable` (forwarder unknown, **disabled**, or doesn't forward inserts — a disabled forwarder has no worker and gets its queue rows discarded at startup, so enqueuing would strand them); 400 malformed body; 500 `enqueue_failed`.

### `GET /v1/forwarder-queues`
- **Purpose:** Per-forwarder upload-queue readout for Settings → Forwarding (W-0005) — the clearable backlog vs the in-flight batch, so an operator can see what a "Clear queue" would drop and what is still being sent.
- **Gating:** Always-on.
- **Request:** No body.
- **Behaviour:** One entry per **configured** forwarder (enabled or disabled, in config order). `clearable` = rows at status `pending` + `failed` (what a clear removes); `in_flight` = rows at status `in_progress` (the currently-claimed batch, never cleared); `uploaded` history is counted in neither. A forwarder with no queued rows reads `{clearable: 0, in_flight: 0}`.
- **Response:** **200**, body `{"forwarders": [{"name": "…", "clearable": N, "in_flight": N}, …]}`.
- **Errors:** 500 `queue_counts_failed`.

### `POST /v1/forwarder/{name}/queue/clear`
- **Purpose:** Operator-triggered queue clear (W-0005) — "drop the remaining backlog, finish the currently claimed batch." Discards only the named forwarder's `pending` + `failed` rows; `in_progress` (the batch a live worker is processing) and `uploaded` (history, carries the remote upstream_id) are left untouched, so the clear is race-free against a running worker with no coordination. Independent of enable/disable — an enabled or disabled forwarder may both be cleared.
- **Gating:** Always-on.
- **Request:** Path `{name}` (the forwarder's config name). No body.
- **Behaviour:** Per-forwarder (a `forwarder_name` equality), never global. The affected QSOs revert to "not uploaded to X" — the ADIF upload stamp, not the queue row, is the source of truth — and are recoverable via manual backfill (`POST /v1/forwarder/{name}/uploads`).
- **Response:** **200**, body `{"discarded": N}` (rows removed).
- **Errors:** 400 `invalid_forwarder` (empty name); 404 `unknown_forwarder` (not a configured forwarder); 500 `clear_failed`.

### `POST /v1/smcloud/reconcile`
- **Purpose:** On-demand SM Cloud reconcile (ADR 0040 S4) — run one detect+heal pass NOW instead of waiting for the hourly loop: compute the local live-row `{count, hash}` (the shared `internal/cloud/reconcile` summary), compare with the cloud's, and on mismatch diff the two manifests and re-enqueue diverged UUIDs through the smcloud forwarder's queue (upserts via the backfill path, missed tombstones via delete rows). The operator's "back up / check now" button.
- **Gating:** Wired only when an **enabled `smcloud` forwarder** exists at startup (cmd/smd injects the reconciler); otherwise the route answers 503.
- **Request:** No body.
- **Behaviour:** Synchronous, bounded at 25 s. Local is authoritative: cloud-only rows (e.g. a previous DB generation) and cloud-newer rows are counted + logged, never touched. Heal batches cap at 5000 per run (`truncated: true` → the next run continues). Safe alongside the periodic loop — duplicate queue rows are absorbed by the cloud's idempotent UUID upsert.
- **Response:** **200**, body `{"in_sync": bool, "local_count": N, "cloud_count": N, "cloud_logbook_id": id, "enqueued_upserts": N, "enqueued_deletes": N, "cloud_only": N, "cloud_newer": N, "truncated": bool, "local_hash": "…"}`. `cloud_logbook_id: 0` = the logbook doesn't exist cloud-side yet (the first backfill was just enqueued).
- **Errors:** 503 `smcloud_unavailable` (no enabled smcloud forwarder configured); 500 `reconcile_failed` (cloud unreachable / local read failed — the periodic loop retries regardless).

---

## Logbook

### `GET /v1/logbook`
- **Purpose:** List all logbooks. **Always-on.** Response **200**, JSON array of `types.Logbook`. Errors: 500 `db_error`.

### `GET /v1/logbook/{id}`
- **Purpose:** Fetch one logbook. **Always-on.** Path `{id}` (positive int). **200** `types.Logbook`. Errors: 400 `invalid_id`; 404 `not_found`; 500 `db_error`.

### `POST /v1/logbook`
- **Purpose:** Create a logbook. **Always-on.** Body `{"name" (req), "callsign" (req), "description"?}` (callsign validated/uppercased). **201** `{"id": int64}`. Errors: 400 `missing_required_field`/`invalid_field_value`/`invalid_json`; 409 `duplicate_name`; 500 `db_error`.

### `PATCH /v1/logbook/{id}`
- **Purpose:** Update name/description (callsign not editable here). **Always-on.** Body `{"name": *string, "description": *string}` (presence-aware) — a **field-level partial write**: only supplied members change (`UPDATE … RETURNING`), so two overlapping PATCHes to different fields both survive with no lost update. **200** is the committed `types.Logbook` (the stored row, not the request's pre-update snapshot). Errors: 400 `invalid_id`/`invalid_field_value`/`invalid_json`; 404 `not_found` (absent or concurrently soft-deleted); 409 `duplicate_name`; 500 `db_error`. No revision-conflict response — the logbook row carries no revision.

### `DELETE /v1/logbook/{id}`
- **Purpose:** Delete a logbook (refuses if it holds QSOs, or if it is the configured default). **Always-on.** **204**. Errors: 400 `invalid_id`; 404 `not_found`; 409 `has_qsos`/`default_logbook` (the configured default — set another default first); 500 `db_error`.

### `GET /v1/logbook/{id}/qso`
- **Purpose:** Cursor-paginated QSO list for a logbook (logbook-browse views).
- **Gating:** Always-on.
- **Request:** Path `{id}`. Query: `limit` (int, default `Server.DefaultPageLimit`, clamped to `MaxPageLimit`, ≥1), `after` (opaque base64url cursor over `{qso_date, time_on, id}`), `missing_from` (forwarder **name**) — filter to QSOs **not yet uploaded** to that destination (ADR 0039 backfill). "Not uploaded" = the destination's ADIF upload-status stamp (`<prefix>_qso_upload_status`) is absent or not `"Y"` — the durable, import-surviving signal (same source as the SPA's tri-state colour + the enqueue skip-check). Resolved name→type→ADIF-prefix server-side.
- **Response:** **200**, body `{"items": types.QsoSlice, "next_cursor": string|null}` (`next_cursor` set only when more rows exist).
- **Errors:** 400 `invalid_id`/`invalid_limit`/`invalid_cursor`; 400 `invalid_missing_from` (names no configured forwarder) or `missing_from_unsupported` (the forwarder exists but its type records no per-QSO upload status, so "missing from it" is undefined; it remains a valid upload target). Note "no stamp" does NOT imply the destination mirrors rows — SM Cloud does, the dev stub does not; row mirroring is a separate registered capability. These were ONE code with one message, which read as "you got the name wrong" for a name that was perfectly good; clients must be able to tell the two apart. 404 `logbook_not_found`; 500 `db_error`.

### `GET /v1/logbook/{id}/count`
- **Purpose:** QSO count for a logbook. **Always-on.** Query: `missing_from` (forwarder name) applies the same not-yet-uploaded filter as the QSO list, so the SPA's "of N" matches the filtered page. **200** `{"logbook_id": int64, "count": int64}`. Errors: 400 `invalid_id`/`invalid_missing_from`/`missing_from_unsupported` (same meanings as the QSO list); 404 `logbook_not_found`; 500 `db_error`.

---

## Draft support / lookup

### `GET /v1/contest-dupe`
- **Purpose:** "Worked this call on this band (and mode) in this logbook?" — hot-path contest/FT8 dupe check.
- **Gating:** Always-on.
- **Request:** Query `logbook` (int, **req**), `call` (**req**, validated), `band` (**req**, validated), `mode` (optional, validated).
- **Response:** **200** `{"duplicate": bool}`.
- **Errors:** 400 `missing_required_param`/`invalid_id`/`invalid_field_value`; 404 `logbook_not_found`; 500 `db_error`.

### `GET /v1/contact-history`
- **Purpose:** "Worked this callsign before, and where?" — recent-contacts panel.
- **Gating:** Always-on.
- **Request:** Query `call` (**req**, validated), `logbook` (int, optional — default all logbooks).
- **Response:** **200** `{"items": [types.ContactHistory, …]}` (newest-first, capped at `MaxContactHistoryResults`; "never worked" = empty list, not 404).
- **Errors:** 400 `missing_required_param`/`invalid_field_value`/`invalid_id`; 404 `logbook_not_found`; 500 `db_error`.

### `GET /v1/enrich/callsign`
- **Purpose:** Enrichment-pipeline lookup (country + station data); SPA fires it on Tab-out of the callsign field (ADR 0017).
- **Gating:** Always-on (tolerates a nil orchestrator).
- **Request:** Query `call` (**req**, validated, uppercased), `refresh` (optional; only `"true"` bypasses cache).
- **Response:** **Always 200** (ADR 0017 #12), body = `lookup.Result` (country/station fields + `CountrySource`/`StationSource`). Nil orchestrator → empty result, `source=none`.
- **Errors:** Only malformed input: 400 `missing_required_param`/`invalid_field_value`. Upstream failures fold into empty fields, never non-2xx (enrichment-never-blocks-logging). Request context propagates so the SPA's AbortController cancels in-flight upstream calls.

---

## Session

### `POST /v1/session/email`
- **Purpose:** Email a session's QSOs as an ADIF attachment via the daemon's SMTP (SessionPanel "send").
- **Gating:** Always-on route; refuses if the mailer is disabled.
- **Request:** Body `{"to" (req, exactly one RFC 5322 mailbox — `net/mail.ParseAddress`; comma-lists and CR/LF rejected, display-name normalized to the bare address), "subject"?, "uuids": []string (req, non-empty), "filename"?}` (subject/filename defaulted from UTC time).
- **Response:** **200** `{"status": "sent", "emailed": []string, "date": "YYYYMMDD"}`. `emailed` lists only the UUIDs whose durable forwarded-by-email flag was actually written (and `date` is empty when none were) — it certifies the DURABLE outcome, not merely that the mail was sent.
- **Errors:** 503 `mailer_disabled`; 400 `invalid_json`/`missing_required_field`/`invalid_field_value`/`no_qsos`; 500 `fetch_failed`/`adif_compose_failed`; 502 `smtp_failure`.
- **Notes:** Daemon rebuilds ADIF from live DB rows (not the client blob), archives it under `<workingDir>/exports/sent-adif/` (best-effort, exclusive-create with a `-N` collision suffix so a reused/same-second name never overwrites a prior backup), then stamps `sm_fwrd_by_email_*` on the rows. The stamp is REVISION-GUARDED (PT-3): only rows still at the revision composed into the sent attachment are marked and returned in `emailed`, so a QSO edited or deleted between compose and stamp is left unmarked — it stays re-sendable and is never over-reported; a post-send stamp failure returns `emailed: []` with an empty `date` (the mail left; the Logbook SPA or a re-send reconciles). Unknown UUIDs are skipped with a warning. `uuids` is capped at 10000 per request (`invalid_field_value` 400).

### `POST /v1/session/export`
- **Purpose:** Download a session's QSOs as an ADIF file (Export dialog "Download ADIF"). Same rebuild-from-DB path as `email` minus the SMTP send — so the download carries the fully enriched stored record, not the SPA's pre-submit subset.
- **Gating:** Always-on route (no mailer gating).
- **Request:** Body `{"uuids": []string (req, non-empty), "filename"?}` (filename defaulted `session-YYYYMMDD-HHMMSS.adi`; an operator-supplied name is a bare attachment name — traversal rejected).
- **Response:** **200** `application/x-adif` with `Content-Disposition: attachment; filename="…"`; body is the composed ADIF document.
- **Errors:** 400 `invalid_json`/`missing_required_field`/`invalid_field_value`/`no_qsos`; 500 `fetch_failed`/`adif_compose_failed`.
- **Notes:** Daemon rebuilds via `adif.ComposeToAdifString(FetchQsoByUUID…)` and archives a backup under `<workingDir>/exports/sent-adif/` (best-effort, same dir as email — backup-on-export, exclusive-create with a `-N` collision suffix). Unknown UUIDs are skipped with a warning. `uuids` is capped at 10000 per request (`invalid_field_value` 400). Does **not** stamp rows (only an email marks "forwarded"). Fetch loop shared with `email` via `Server.fetchSessionQsos`.

### `GET /v1/notifications`
- **Purpose:** Read the durable operator notification history — the newest events the SPA rail shows so a failure survives its transient toast and a page reload (W-0001 / ADR 0076).
- **Gating:** Always-on.
- **Request:** Query `?limit=N` (optional; default 50) — must be an integer in `[1,500]` (the per-category retention ceiling).
- **Response:** **200** `{"items": [OperatorEvent…]}`, newest first. Each `OperatorEvent` = `{id, category, kind, severity, occurred_at (RFC3339), build, detail (embedded JSON object)}`. Empty history is `{"items": []}` (never null).
- **Errors:** 400 `invalid_field_value` (limit non-integral or out of range); 500 `db_error`.
- **Notes:** Reads the `notification` category via `FetchOperatorEventsByCategoryWithContext`; `detail` is the stored typed metadata verbatim (never raw provider text). Only the `notification` category is exposed today.

### `POST /v1/notifications`
- **Purpose:** Record a durable, browser-originated operator notification that must survive its transient toast and a page reload (W-0001 / ADR 0076). The only wired kind is a failed ADIF export (Export dialog).
- **Gating:** Always-on route; same-origin/CSRF protected by the mux-wide gate like every unsafe method.
- **Request:** Body `{"kind": "export.adif_failed" (req, allowlisted), "count": int (req, ≥1 — the UUIDs the browser actually submitted), "outcome": "no_qsos"|"invalid"|"server"|"network" (req)}`. Decoded **strictly** (`DisallowUnknownFields`): unknown keys (e.g. `message`, `reason`, `code`), non-integral/overflowing/non-positive counts, and any other kind/outcome are rejected. The client never supplies severity, time, or build.
- **Response:** **204** No Content.
- **Errors:** 400 `invalid_json`/`invalid_field_value`; 413 `body_too_large`; 500 `record_failed`.
- **Notes:** The daemon stamps category `notification`, severity `error`, occurrence time, and the build version, and builds the canonical stored `detail` (`{count, outcome}`) server-side — the client's bytes are never persisted and the free-text export `message` is deliberately dropped. `count` is **not** capped at the export endpoint's 10000 (a 10001-QSO request may be exactly the invalid export being reported). Persisted via `RecordOperatorEvent` into the `operator_event` store (last 500 per category, oldest-first), outside any QSO transaction. The daemon-originated `forward.failed` kind is written directly at the forwarding boundary, not through this endpoint.

---

## Config & hardware

### `GET /v1/config`
- **Purpose:** Operator-relevant config + joined display details for the consolidated app.
- **Gating:** Always-on.
- **Response:** **200**, body = `ConfigResponse`: `setup_complete` (bool), `logging_station` (`types.LoggingStation`), `default_logbook` (`types.Logbook`, DB-joined), `default_rig` (`DefaultRigInfo`: `{id, model?, port?}` for the active rig), `station` (`types.StationConfig`), `bridge` (`BridgeInfo`: `enabled`, `driver?`, `rig_name?`, `rig_modes?`, `ops?`, `tune?`, `mode_mappings?` — merged rigdef-defaults + overrides for the active driver — and `ft8_mode?`, the rig CAT mode literal for FT8 (rigdef default + per-rig override) the FT8 band buttons assert), `mailer` (`MailerInfo`: `enabled`, `default_recipient?`), `qsl` (`types.QslDefaults`: standing outgoing-QSL defaults `qsl_via` / `qslmsg` / `qsl_sent_via`), `ft8_display` (resolved `types.Ft8DisplayConfig`), `ft8_audio` (resolved `types.Ft8AudioLevels` — the RX level meter's dBFS window; read-only here, calibration is a config.json edit), `ft8_meter` (resolved `types.Ft8MeterLevels` — the TX-drive ALC threshold, raw 0–255: `alc_amber` where green/healthy ends and the terminal amber "reduce drive" state begins, RATIFIED 30 (2026-08-07 green band; 2026-08-08 red folded into amber — the RM ALC answer saturates at ~30, ADR 0064 §4); read-only here, same calibration path), `ft8_frequencies` (resolved `map[string]int`), `ft8_caller_answer_mode` (resolved string, default `operator_pick` since 2026-08-08 — automation is an explicit opt-in; since ADR 0066 this is only the SEED for the session's Answer selector), `ft8_max_repeats` (resolved int, default 6 — the FT8 sequencer's repeat cap: unanswered pre-final rungs, and also the CLOSING rung on the ladders where the partner is still waiting for it, bounding a rung that fails to transmit — see `internal/ft8/finalrung.go`), `ft8_field_day` (`types.Ft8FieldDayConfig` — operator's Field Day class+section, empty `{}` when unset), `bridge_timeouts` (resolved `types.BridgeTimeoutsConfig`) and `bridge_tune` (resolved `types.BridgeTuneConfig`) — both **sparse on disk, served resolved** (defaults filled, ceilings applied via `bridge.ResolveTimeouts`/`ResolveTune`) so the SPA reads effective values config.json doesn't materialise (config.md §15).
  Also `forwarders` (`[]ForwarderInfo`, **masked** — present only when ≥1 configured): each `{name, type, enabled, action_filter?, credentials_set?}` where `credentials_set` lists the credential keys that hold a value — **never the values** (masked-on-GET).
  Also `smtp` (`SmtpInfo`, the app Settings → Email section and the config SPA's Email tab, **masked**): `{enabled, host?, port?, username?, from?, default_recipient?, starttls, timeout_sec?, password_set}` — every field except the password, with `password_set` reporting whether a password is stored (the value is **never on the wire**, masked-on-GET). `password_clear` is PUT-only and **never emitted here**: it is a command, not state, so a client that echoes a GET body straight back cannot wipe the secret by accident. `port`/`timeout_sec` are served **resolved** (`config.Normalize` fills 587/30 when unset), so a client can show what is actually in effect. Distinct from the read-only `mailer` projection: `mailer` is the *logging* SPA's running-mailer view (enabled + recipient), `smtp` is the persisted-intent edit surface (they can diverge until the daemon restarts to pick up a saved change).
  Also `lookup` (`LookupInfo`, the enrichment providers, **masked**): `{hamnut, chain, continue_if_blank}` where each entry is a `LookupProviderInfo` `{name, priority?, label?, enabled, url?, username?, timeout_sec?, view_url?, password_set}` — `priority` is present on callsign-chain entries and is their exclusive numeric authority/order (ADR 0068; hamnut is outside the chain), while `continue_if_blank` is the chain-wide completion-field list. `label` is the operator's config.json display name, **served on GET, ignored on PUT** (same contract as the forwarder `label`; `mergeLookupProvider` carries it from the stored entry, so an unrelated save cannot delete it) — provider passwords are masked exactly like `smtp`'s, with `password_set` reporting whether one is stored, and `password_clear` likewise **never emitted** (PUT-only command). Plus `country_ttl_days`, `station_ttl_days` (both `*int`, served **resolved** — `config.Normalize` fills 365/90 when unset) and `refresh_max_in_flight` (cache freshness, ADR 0017).
  Also `psk_reporter` (`types.PskReporterConfig`, the config SPA's FT8 tab): `{enabled?, host?, port?}` — opt-in public upload of FT8 reception spots. No secrets (receiver identity comes from `logging_station`), so the canonical type rides the wire unmasked. Served **raw/sparse**: an empty host/port means "use the production collector default" (`report.pskreporter.info:4739`, resolved daemon-side), so the SPA shows those as placeholders rather than materialising them. Also `ft8_decode_log` (`types.Ft8DecodeLogConfig`, the config SPA's FT8 tab): `{enabled, path?}` — the JTDX `ALL.TXT`-style decode log. No secrets. A nil block in config (never enabled) is served as a disabled zero value so the SPA form binds; an empty `path` means "use the default" (`$SM_WORKING_DIR/log/ft8-all.txt`, resolved daemon-side at open). Also `restore_rig_on_mode_switch` (`*bool`): whether a Phone/CW ↔ FT8 operating-mode switch auto re-tunes a CAT-live rig back to that mode's last freq/mode (SPA behaviour). Served **resolved** (always a definite bool — `true` when unset, the default ON).
- **Errors:** 500 `db_error`.
- **Notes:** Mailer/Bridge are read-only projections. The bridge projection omits serial configuration; the narrow `default_rig` join intentionally includes only id/model/port. The SMTP **password** is never on the wire. The rest of the SMTP block is editable via the masked `smtp` surface. Forwarder/lookup credentials are likewise never emitted (only which keys are set).

### `PUT /v1/config`
- **Purpose:** Persist the operator-writable config subset (My Station / Settings save).
- **Gating:** Always-on.
- **Request:** Body = `ConfigResponse` shape. Writable: `logging_station`, `station`, `qsl` (presence-aware — omit to leave untouched), `ft8_display` (presence-aware — omit to leave untouched), `ft8_caller_answer_mode` (presence-aware string — the session selector's DEFAULT; all three literals accepted since ADR 0066 fork 4, junk → `invalid_field_value` 400; sets `ft8.tx.caller_answer_mode`), `ft8_max_repeats` (presence-aware int for the app Settings → FT8 section — the repeat cap — unanswered pre-final rungs, plus the closing rung on ladders where the partner awaits it (`internal/ft8/finalrung.go`); validated `[1, 10]` → `invalid_field_value` 400; sets `ft8.tx.max_repeats` and is **applied live** to the running sequencer — the one `/v1/config` field with a live side-effect, so the operator can drop a dead FT8 contact sooner mid-pile-up without a restart), `ft8_field_day` (presence-aware; the operator's Field Day class+section for answering CQ FD — normalised upper-case; class strict / section loose → `invalid_field_value` 400; sets `ft8.field_day`), `bridge.mode_mappings` (diffed against rigdef defaults; only deviations stored, on the active rig), and — for the config SPA's Rigs tab — `rigs` (`[]types.RigConfig`, replaces the whole catalogue) + `default_rig_id` (the active rig), both **presence-aware** and validated via `validateRigs`. `rigs`/`default_rig_id` are **write-only here**: never emitted on GET (the catalogue read surface is `GET /v1/rigs`; the active rig's narrow read view is the `default_rig` join). Also `forwarders` (`[]ForwarderInfo`) for the config SPA's Forwarding tab — **presence-aware** (omit to leave the list untouched; carry it to REPLACE the whole list), validated via `validateForwarders` (dup name / unknown type / unsupported action). Per entry, `credentials` (key→value) carries only the fields the operator typed; the daemon **merges** them onto the stored secrets by `name`, so a field that is **omitted OR blank** keeps its stored value — a client never has to strip empties to avoid destroying a credential. The exception is a field the type marks `CredentialField.Clearable` (currently only smcloud's `logbook`, and the dev stub's `mode`), where empty is a meaningful value the constructor defaults; a clearable blank is stored as the canonical `""`, never the whitespace as sent. The advanced knobs `tick_interval_sec`/`batch_size`/`retry` carry over from the stored entry. Every **enabled** forwarder in the merged list is then probed with `forwarding.Build` — the same call `spawnForwarderWorkers` makes at startup — so the PUT accepts exactly what the daemon can start with; a failure is 400 `forwarder_unusable` and the write is abandoned. The 400 message is **stable and sanitised** (forwarder name only) — constructors format the offending value into their own error and a stored `credentials.url` can carry userinfo, so the real cause goes to the daemon log, never the wire or the access log. Disabled entries are skipped (startup skips them too), so a destination stays saveable while half-configured. The probe runs only when the body carried `forwarders`, so one pre-existing bad destination can't block unrelated saves. During **first-run setup** it also runs in the pre-seed dry run, so a rejected setup PUT can't leave an orphaned default logbook behind. Also `smtp` (`SmtpInfo`) for the app Settings → Email section (and the config SPA's Email tab) — **presence-aware** (omit to leave the SMTP block untouched; carry it to REPLACE the block), validated via `validateSmtp` (enabled ⇒ host+from required, address + port + timeout sane → `invalid_smtp` 400). The `password` field carries only a freshly-typed value; the daemon **merges** it onto the stored secret, so a blank password keeps the stored one (`password_set` is ignored on PUT). **`password_clear` (bool) removes the stored password** — blank deliberately goes on meaning KEEP (it is what an operator editing the host sends every save), so removal needs its own signal rather than overloading the empty string the way forwarder `Clearable` fields do; unauthenticated submission is a valid configuration, so a cleared password on an enabled block still saves. If a payload carries both, **clear wins** (only the flag can have been set deliberately; a stale password field is what a client-side form bug leaves behind). `port`/`timeout_sec` of **0 mean "use the default"**: `config.Normalize` resolves them to 587/30 before `validateSmtp` runs, so a blanked number saves cleanly and the response echoes the resolved value — it no longer stores 0 and silently changes at the next daemon start. Also `lookup` (`LookupInfo`) for the app Settings → Enrichment section (and the config SPA's Enrichment tab) — **presence-aware** (omit to leave untouched; carry to REPLACE the block). Each provider's `password` is merged onto the stored secret by NAME with the same keep-on-blank rule, and `password_clear` removes it (same contract as `smtp`'s; both merges call `resolveMaskedPassword`, so the rule cannot drift between them — clear wins if a payload carries both). **The `chain` is replaced WHOLE**: `mergeLookup` rebuilds it purely from the payload with no merge-by-name for absent entries, so a client must carry EVERY provider on every save — omitting one deletes it, along with its `url`/`timeout_sec` (those are taken as-sent; `config.Normalize` re-stamps defaults only for the two names it knows, `hamnut` and QRZ). `country_ttl_days`/`station_ttl_days` are `*int` with meaningful nil: **omit to mean "use the default"** (365/90, filled by `config.Normalize` so the resolved value rides back in the response), **send an explicit 0 to mean "trust this cache indefinitely"** — `lookup.Orchestrator.isStale` reads a non-positive TTL that way. The two are opposite instructions and a client must not send 0 for a blank field. A negative is still `invalid_lookup` 400. `refresh_max_in_flight` stays a plain int (0 = package default in both the accessor and the defaults pass, so it has no absent-vs-zero conflict). Also `psk_reporter` (`types.PskReporterConfig`) for the config SPA's FT8 tab — **presence-aware** (omit to leave untouched; carry to REPLACE the block), validated via `validatePskReporter` (port in 0..65535, 0 = default → `invalid_psk_reporter` 400). No secrets, so it's taken as-sent. Also `ft8_decode_log` (`types.Ft8DecodeLogConfig`) for the config SPA's FT8 tab — **presence-aware** (omit to leave untouched; carry to REPLACE the block), no secrets, taken as-sent (no validation; an empty path resolves to the default at open). Restart-only — the log file opens at FT8 service start. Also `restore_rig_on_mode_switch` (`*bool`) — **presence-aware** (omit → untouched; carry to set), no validation; gates the SPA's CAT-live re-tune on a Phone/CW ↔ FT8 switch (default ON). `setup_complete`, `mailer`, `ft8_frequencies`, `bridge_timeouts`, `bridge_tune` are server-managed/ignored.
  ADR 0068 adds two lookup write rules: every callsign entry carries an exclusive positive `priority` (including disabled entries; unique and contiguous), and `continue_if_blank` carries the chain-wide completion policy. Config normalisation sorts by priority, so JSON array order has no authority. An explicit empty completion list requests legacy first-substantive behavior; omission preserves the stored policy, allowing pre-ADR clients to save unrelated lookup settings without resetting it. A genuinely absent policy in legacy config is still normalised to `["name", "gridsquare"]`. Invalid priorities and duplicate/unknown completion fields return `invalid_lookup` without writing.
- **Response:** **200**, body = freshly-built `ConfigResponse`. On the rare **applied-but-durability-unconfirmed** save — the write's atomic rename succeeded so the new config is the live file, but the parent-directory fsync then failed — the response carries an optional `durability: "unconfirmed"` field (omitted on an ordinary durable save and on GET, so it is backward-compatible), which the SPA renders as "saved, durability unconfirmed" and which is also logged. Still a **200**: the change is in effect (PT-6).
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`forwarder_unusable` (an enabled forwarder whose credentials won't construct — would abort daemon startup), plus the config validator's stable `{code, message}` findings (config.md §12 / ADR 0010); **409 `callsign_locked_to_logbook`** (a post-setup station-callsign change that would orphan the default logbook — reconcile logbooks first) / **`default_logbook_callsign_mismatch`** (setup found a logbook already at the default id under a different callsign); 500 `config_write_error`/`db_error`.
- **Notes:** Candidate runs `config.Normalize` + `config.Validate` (same pipeline as Load). The first PUT with a non-empty callsign completes setup (seeds default logbook, flips `setup_complete`). A **post-setup callsign change is rejected** when the new callsign no longer matches the default logbook's callsign — the operating callsign is currently a config field the submit gate checks against the logbook, so silently changing it would break every subsequent live submit; a logbook-scoped operating identity is deferred (review 2026-07-22 #1). Rewrites the whole `config.json` from in-memory state via a **crash-durable replace** — a unique temp file in the config dir (no fixed `.tmp` for concurrent writers to collide on), fsync of the temp before an atomic rename, then fsync of the parent directory so the rename survives a crash (PT-6). In-memory is published only after the rename, so runtime and disk stay coherent; a rename-then-directory-fsync failure is the applied-but-unconfirmed caveat above, not a failure. **Don't hand-edit config.json while the daemon runs.**

### `GET /v1/hardware`
- **Purpose:** Enumerate serial ports + audio capture/playback devices for the config SPA's rig-profile pickers — friendly labels, no hand-typed ids (ADR 0028).
- **Gating:** Always-on.
- **Response:** **Always 200**, body `{"serial_ports": [SerialPort], "audio": {"available": bool, "capture": [AudioDevice], "playback": [AudioDevice]}}` (all slices non-nil → `[]` not null). `SerialPort` = `{id, label, usb, vid?, pid?, serial_number?, product?}`; `AudioDevice` = `{name, is_default}`.
- **Errors:** None on the wire — degrades gracefully: serial enum failure → empty list + warn; audio unavailable (CGO-free static build, `ErrAudioUnavailable`) → `available:false` (not an error); other audio failure → `available:false` + warn.
- **Notes:** Live enumeration (no cache); listing audio never grabs a device, so it's safe alongside active FT8 capture. Friendly serial labels depend on udev populating the USB product string (may fall back to the bare path).

### `GET /v1/rigs`
- **Purpose:** The config SPA's rig-profiles editor surface (ADR 0028 / config.md §10) — the operator's configured rigs paired with the embedded rigdef catalogue, so the editor can render "inheriting rigdef default X" vs "operator override Y."
- **Gating:** Always-on (config editing, independent of the bridge subsystem).
- **Response:** **200**, body `{"default_rig_id": int64, "rigs": [types.RigConfig], "catalogue": [RigDefSummary]}`.
  - `rigs` is the stored config (overrides intact — a `null`/absent `ft8_mode`/`my_rig`/`mode_mappings`/`overrides` means "inherit the rigdef default"). `rigs` is non-null (`[]` when none configured).
  - `RigDefSummary` = `{id, name, manufacturer, model, family, description?, ft8_mode?, rig_modes?, mode_mappings?, serial}` — the editor-facing projection of a rigdef; **omits the large `commands`/`states` CAT tables**.
  - The SPA joins `rig.model` → `catalogue[].id` to compute default-vs-override per field, and derives the default MY_RIG from the matched `catalogue[].name`.
- **Errors:** None on the wire (always 200).
- **Notes:** **Read-only.** The **write path is `PUT /v1/config`** (presence-aware `rigs` + `default_rig_id` — see above), used by the config SPA's Rigs tab: it GETs the catalogue here, edits a whole-catalogue draft, and PUTs it back. The SPA re-GETs this endpoint after a save (the PUT response doesn't carry the catalogue) to re-hydrate the canonical view.

### `GET /v1/lookup-types`
- **Purpose:** Data-driven descriptors for every enrichment provider compiled into this daemon, so Settings → Enrichment renders labels, help text and credential fields without a hardcoded map (ADR 0062). The direct counterpart of `/v1/forwarder-types`.
- **Gating:** Always-on (config editing, independent of whether enrichment is enabled).
- **Response:** **200**, body `{"types": [...], "completion_fields": [...]}` where each provider entry is `{name, display_name, help?, kind, needs_credentials, min_username_len?, min_password_len?, default_url?, default_view_url?, default_timeout_sec?}` and each completion entry is `{name, display_name}`. `kind` is `"country"` (the single prefix provider) or `"callsign"` (the priority chain). `needs_credentials` is **always present, never omitted** — a client must be able to tell "anonymous by design" from "field absent", because hamnut sprouting login boxes is the failure it prevents. The completion catalogue is the same one config validation accepts (initially `name` and `gridsquare`).
- **Notes:** Read-only. Only providers registered via `lookupdef.RegisterProvider` appear, i.e. those this build can actually wire. A provider present in the operator's config but ABSENT here is the "unrecognised" case: the section still renders it (the `LookupProviderInfo` shape is uniform) with its raw name and generic fields, which is what a config from a newer build looks like. An empty registry serves a `types: []` member, never `null`; `completion_fields` remains available because it is independent of provider registration. Write path for the providers and completion policy is `PUT /v1/config`'s `lookup` block.

### `GET /v1/forwarder-types`
- **Purpose:** Data-driven descriptors for the config SPA's Forwarding tab, so the add-forwarder credential form renders without hardcoded per-type forms (adding a forwarder type in Go needs zero SPA change).
- **Gating:** Always-on.
- **Response:** **200**, body `{"types": [TypeDescriptor]}` where `TypeDescriptor` = `{type, display_name, supported_actions: []string, credential_fields: [CredentialField]}` and `CredentialField` = `{key, label, kind: "text"|"password", help?}`. Sorted by `type`. Only types registered via `forwarding.RegisterForwarderType` (the real forwarders compiled into the binary — QRZ, ClubLog, + stub in dev builds) appear.
- **Errors:** None on the wire (always 200).
- **Notes:** Read-only. `kind:"password"` fields drive masked entry; they are merged-not-echoed by `PUT /v1/config` (see the `forwarders` notes above). Write path for the destinations themselves is `PUT /v1/config`'s `forwarders` block.

---

## Operational

### `GET /v1/healthz`
- **Purpose:** Liveness/readiness (DB reachability). **Always-on.** **200** `{"status": "ok"}`; **503** `db_unavailable` if the DB ping fails.

### `GET /v1/version`
- **Purpose:** Daemon build / Go runtime / build environment / DB schema version. **Always-on.** **200** `{"daemon": string, "go": string, "env": "dev"|"release", "schema": {"version": uint64, "dirty": bool}?}` (always 200; `schema` omitted + warn-logged if the schema query fails). `env` is `dev` for any source build (incl. `task run:smd`) and `release` only for a packaged binary (the RPM build stamps `-X …/internal/buildinfo.Env=release`); the app flags a `dev` daemon with a Sidebar DEV pill and a `DEV · ` tab-title prefix so it's distinguishable from the deployed one on the same `:8080`. Only the exact literal `dev` marks DEV — a missing or unexpected `env` is treated as `release`, so a release daemon is never falsely labelled. The Sidebar footer shows the running `daemon` build itself (not a source constant); an unreachable/malformed response reads "Version unavailable" and drops the marker.

### `POST /v1/restart`
- **Purpose:** ATTENDED, operator-triggered graceful daemon restart — applies the "Requires a restart" config-apply changes (active rig, connection, mode mappings, serial overrides) without an on-box `systemctl`. The daemon runs its normal graceful shutdown (releasing the tune/FT8 carrier — TX-safe), then exits `ExitRestart` (3); systemd (`smd.service` `Restart=on-failure` + `RestartForceExitStatus=3`) respawns it ~`RestartSec` (5s) later and SSE clients auto-reconnect.
- **Response:** **202** (no body) — accepted; shutdown then respawn follows.
- **Errors:** **409** `tx_active` (a tune carrier / FT8 transmission is CURRENTLY keyed — stop transmitting first; a stuck/*unconfirmed* TX is NOT refused, so a recovery restart stays possible); **503** `restart_unavailable` (no service-manager restart wired — split-host / non-systemd / a bare `./smd` run, where nothing would bring it back up).
- **Notes:** The 202 flushes before the shutdown begins (the handler writes it, then signals a guarded channel). Wired only when the managing unit sets `SM_SELF_RESTART=1` (the bundled `smd.service` does, kept in lockstep with its respawn config) — `cmd/smd` gates `api.Server.SetRestart` on it, so a bare run or a unit that won't respawn returns 503. CSRF: covered by the API-wide same-origin middleware (`requireSameOrigin`, `csrf.go`) that guards every mutating method — a cross-origin drive-by POST is rejected **403** `cross_origin`, while the same-origin SPA, the loopback dev proxy, and non-browser clients (no Origin header) pass; still unauthenticated (single-operator loopback service). SPA control is the Settings "Restart daemon" button.

---

## Rig / bridge (ADR 0013 / 0019 / 0026 / 0027)

### `GET /v1/rig/events`
- **Purpose:** SSE stream of rig CAT state for the consolidated app (served by `bridge.Service.HTTPHandler`).
- **Gating:** **Only when the bridge subsystem is enabled.** Wrapped in `limitEventSubscribers`.
- **Response:** **200** SSE stream. Events:
  - `rig-state` → `RigStatePayload` `{rigIdentity?, vfoA?, vfoB?, mode?, subMode?, selectedVfo?, splitOverride? (*bool), power?}` — omitted fields leave SPA state untouched; first event after connect is a full snapshot.
  - `rig-disconnected` → `{code: "rig_no_data"|"serial_port_error", details?}`.
  - `bridge-error` → `{code: "unknown_driver"|"serial_config_invalid"|"missing_init_command"|"missing_read_command"|"serial_open_failed"|"init_write_failed"|"identity_unrecognised"|"identity_mismatch", details?}`.
  - `tune-state` → `{active: bool}`.
  - `rig-meters` → `{meter: "ALC"|"PO", value: 0-255}` (ADR 0064) — one decoded `RM4;`/`RM5;` poll answer, raw rig scale (the SPA owns thresholds/rendering). Flows only while an FT8 capture session is live AND the rigdef declares a `METERPOLL` command (FTdx10 today); deliberately NOT replay-cached — a stale reading is worse than none, and the next answer is ≤ one poll interval (default 250 ms) away. Clients infer "no meter data" from staleness, distinct from a zero reading.
- **Errors:** 503 `server_busy`.
- **Notes:** Hub one-slot replay cache for `bridge-error`/`rig-disconnected`/`tune-state`; disconnect cache cleared on next `rig-state`. Codes carry `{code, details}` for SPA i18n (no human strings on the wire).

### `POST /v1/rig/command`
- **Purpose:** Inbound rig control — freq/mode/VFO/band (ADR 0026).
- **Gating:** **Only when the bridge is enabled.**
- **Request:** Body either single `{"op": string, "value": <scalar>}` **or** atomic batch `{"commands": [{op, value}, …]}` (not both, max 32 commands per batch → `invalid_field_value` 400). `value` is a JSON scalar.
- **Response:** **202 Accepted**. For the `kenwood` family this is "written," not "rig is now at X" — the resulting state is confirmed out-of-band via the `rig-state` SSE push (confirm-by-push). For `icom_civ` (ADR 0034 wait-for-ACK) the daemon waits for the rig's per-command FB/FA ACK before responding: a 202 means the rig **accepted** the command (and the daemon has synthesized the matching `rig-state` push, since CI-V never broadcasts a commanded change); a rejected/timed-out command returns an error below.
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`missing_required_param`/`rig_unsupported_command`/`rig_invalid_value`; 503 `rig_not_connected`; 409 `rig_identity_unverified`; 422 `rig_command_rejected` (CI-V only — rig answered FA/NG, e.g. value out of range); 504 `rig_command_no_ack` (CI-V only — no FB/FA within `bridge.timeouts.civ_ack_ms`); 409 `rig_tx_stop_failed`; 500 `rig_command_failed`.
- **Notes:** A `kenwood` batch becomes one atomic CAT line; an `icom_civ` batch is sent frame-by-frame, each awaiting its ACK (the bus is half-duplex), so a mid-batch FA leaves earlier ops applied. `tx_on`/`tx_off` are never exposed — TX can't be keyed here. **A RETUNING command (`set_band`/`set_freq`) stops EVERY SM-owned transmitter first** — the tune carrier (bridge) and FT8 (both are keyed by SM and owned by different subsystems): PTT down, FT8 session ended, TX disarmed, and only then is the command written — the rig is never switched while keyed (switching relays under RF damages amplifiers). It is therefore no longer refused while transmitting; the FT8 session ends with `end_reason: band_change` so the operator is told it was their own band change and not the rig drifting. If TX cannot be confirmed stopped the retune does NOT proceed → 409 `rig_tx_stop_failed`. Non-retuning commands (e.g. `set_mode`) never tear down a session. Spec: `internal/api/handler_rig_retune_test.go` (dogfood 2026-07-27).

### `POST /v1/rig/tune`
- **Purpose:** Drive the daemon-owned tune-carrier TX state machine (ADR 0027) — a reduced-power RTTY carrier for amp tuning.
- **Gating:** **Only when the bridge is enabled.**
- **Request:** Body `{"active": bool}` — `active` is **required**: a missing/absent boolean is a 400, never a silent stop; unknown or duplicate keys are rejected (AW-2). Both directions idempotent.
- **Response:** **202 Accepted** (state confirmed via `tune-state` SSE).
- **Errors:** 400 `invalid_json`/`missing_required_field`; 503 `rig_not_connected`/`rig_state_unknown`; 409 `rig_identity_unverified`; 500 `rig_tune_failed`.
- **Notes:** Daemon owns the guaranteed stop (hard auto-off timer + release-on-disconnect + single-flight shared with FT8-TX). Refuses to start without a mode/power restore snapshot (`rig_state_unknown`).

### `POST /v1/rig/tx/recheck`
- **Purpose:** Re-ask the rig for its transmit state so a standing stuck-TX alarm can be resolved on evidence (2026-07-21 incident: the alarm latches itself out of every clear path, because every issuer of the TX-status query is gated by the same `txUncertain` flag the alarm holds).
- **Gating:** **Only when the bridge is enabled.**
- **Request:** No body.
- **Response:** **200** `{"asked": true, "alarm_active": bool}`.
- **Errors:** 503 `rig_not_connected`; 409 `rig_identity_unverified`; 501 `rig_tx_recheck_unsupported` (rigdef has no `read_tx_status`); 500 `rig_tx_recheck_failed`.
- **Notes:** Writes a **READ only** (`read_tx_status`) — it carries no TX intent, which is why it may run outside the key/command gates while the transmitter's state is unknown. It **cannot clear the alarm**, and no endpoint may: only the rig's own "in RX" answer, via `observeTxStatus` → `confirmTxIdle`, retires it. `alarm_active` is therefore a status snapshot taken after the query went out, NOT a safety verdict — the answer arrives asynchronously, so expect `alarm_active: true` followed by the authoritative `tx-alarm` SSE clear. Operator acknowledgement stays client-side (the SPA banner hides locally without touching daemon state). The daemon also re-probes automatically on a bounded schedule when an alarm is raised; this endpoint is the manual path for after that expires.

---

## FT8 (ADR 0024 / 0029 / 0030 / 0031 / 0033)

### `GET /v1/ft8/events`
- **Purpose:** FT8 subsystem SSE stream (served by `ft8.Service.HTTPHandler`).
- **Gating:** **Only when FT8 is enabled.** Wrapped in `limitEventSubscribers`.
- **Response:** **200** SSE stream. Events:
  - `ft8-occupancy` → `OccupancyReport` `{slot: {start_utc, period}, passband: Band, dial_mhz?, signal_width_hz, occupied: [Band], suggested: [int]}` (`Band` = `{low_hz, high_hz, source?, level?}`). `dial_mhz` is the rig dial the slot was CAPTURED on. As with `ft8-qso`'s `dial_freq_mhz`, clients must attribute the report to a band from THIS, never from live rig state: publication lags capture by the decode, so a report in flight across a band change would otherwise be labelled with the new band and its `suggested` offsets — measured elsewhere — could steer a transmission. A CAT-attached daemon emits **only** reports it can place: a slot is skipped when the dial moved during its window (sampled every audio batch, so an excursion that returns to the same frequency is still caught) or was unknown at any point in it. A slot whose dial MOVED also has its `ft8-decode` list suppressed — the event still fires, empty, so the slot clock ticks — because every consumer of a decode resolves it against the CURRENT dial: rendering a station heard elsewhere as workable here would key an answer at nothing and spot a wrong frequency to PSK Reporter. Empty decodes are not a sequencer no-op either — they read as silence, repeat the rung and key it — so a moved slot is not fed to the sequencer at all. Session TX safety is enforced separately and directly: a rung is refused (and its session ended, `ft8-qso` pushing `active:false`) when the daemon cannot POSITIVELY confirm the rig is still on the dial that session pinned — a mismatch **or** an unreadable dial. Refusing RF never discards a contact: a rung carrying a completion runs its policy first, so a Group A QSO is still recorded. With no CAT at all the check is inert (that deployment cannot key). `dial_mhz` is therefore absent only when the daemon has no CAT at all — a deployment that cannot transmit (FT8 keying requires a writable rig), so its panel is display-only and a client may fall back to its own view of the band there. Not replay-cached (see `internal/ft8/hub.go`).
  - `ft8-decode` → `DecodeReport` `{slot: SlotRef, decodes: [{text, freq_hz, dt_s, snr}], dial_mhz?}`. `dial_mhz` (added 2026-08-07, review P1) is the rig dial the slot was CAPTURED on, 0/absent when unknown — clients must attribute the decodes to a band from THIS, never from live rig state, for the same reason as `ft8-occupancy`'s: publication lags capture by the decode (~0.7–1.6 s), so a QSY in that gap otherwise files a whole slot's stations on the wrong band (wrong PSK Reporter spots, wrong-band Band Activity rows — the in-window case is already handled by the moved-slot suppression above; this covers the move that POSTDATES the window). The daemon's own PSK Reporter sink spots from this stamp and skips unattributable slots; the SPA withholds rows whose capture band differs from the band the view is on.
  - `ft8-tx` → `TxState` `{armed, transmitting, message?, offset_hz?, error?, disarm_cause?}`. `disarm_cause` is the stable code for the disarm the frame reports (`operator` | `unattended` | `cat_lost` | `shutdown` | `band_change` | `dial_moved`), `""`/absent while armed — cleared when a new arm commits, so a replayed frame can never pair a stale cause with a live arm. It exists because an ARM-ONLY safety teardown (dial nudge with no session active) has no terminal `ft8-qso` frame to carry a reason, so it was invisible outside `smd.log` (dogfood 2026-08-07). All causes ride the wire, `operator` included: which deserve a notice is the client's decision (the SPA is silent for `operator`, for a frame replay with no observed armed→disarmed edge, and for the same-teardown duplicate of a session-end notice — one knob turn, one notice, in BOTH frame orders: live publication is qso-then-tx, but the reconnect replay is tx-then-qso, `lastTx` before `lastQso` in `hub.subscribe`).
  - `ft8-qso` → `QsoStatus` `{active, role?, fd?, type4?, their_call?, state?, next_message?, repeats?, max_repeats?, our_report?, their_report?, dial_freq_mhz?, answer_mode?, answerers?}` `answer_mode` (caller frames only) names the run's answerer-selection mode so a client can tell an `operator_pick` run from an auto one before any answerer arrives; `answerers` (operator_pick caller frames, ADR 0065) is the candidate list `[{call, snr}]` — stations currently answering the CQ that the run can actually work (encodable, heard within the 3-min staleness bound), oldest first, in BOTH phases (it keeps accumulating while a popped contact is worked); pop one via `POST /v1/ft8/cq/pick`. Since ADR 0067 the list rides EVERY pick session/listing run (not just CQ runs), and pick frames also carry `queue` [{call, snr}] — the BAGGED stations, in bag order, auto-worked by the drain — and `drain_paused` (Stop paused the drain; Resume continues). `dial_freq_mhz` is the rig dial PINNED to the session at start — the frequency the contact will be logged on. Clients must attribute a contact to a band from THIS, not from live rig state: the rig and FT8 status are independent streams, so a band change mid-contact (or a skew between them) otherwise files the contact under the wrong band. (`fd:true` marks an ARRL Field Day session; `type4:true` marks a reduced type-4 nonstandard/compound-call session — bare-calls→RR73→73, no grid/report rungs, ADR 0048). `end_reason` appears ONLY on a terminal `active:false` frame and only when the operator did not cause the end. Stable codes, two families: the daemon could no longer confirm the rig's frequency (`dial_moved` | `dial_unknown`), or it could not KEY (`tx_not_armed` — TX was no longer armed; `tx_bad_message` — the next message will never encode, e.g. a compound call SM cannot answer). `band_change` is the one exception to "the operator did not cause it": they moved the rig, and saying it drifted would be a small lie. Absent for an abandon, a TX disarm, or a completed contact — all three are the operator's own doing, and those are named in `smd.log` instead (`reason: operator` | `tx_disarmed`). Clients must render unknown codes through a CAUSE-AGNOSTIC fallback: a frequency-specific default became false the moment the `tx_*` codes existed (2026-08-04). Clients should say it out loud: without it a deliberate safety stop is indistinguishable from a hang (dogfood 2026-07-27 — the first on-air read of a working guard was "moving the dial does not stop TX"). `max_repeats` is present exactly when the CURRENT rung is bounded, so a countdown shows iff `max_repeats > 0`: it is absent while plain calling CQ (uncapped by design) and on a "send once" closing rung whose partner has already rogered (`internal/ft8/finalrung.go`).
  - `ft8-logged` → `LoggedQso` `{uuid, callsign, freq_hz, band, rst_sent, rst_rcvd, mode, time_on:"HH:MM", qso_date:"YYYY-MM-DD", gridsquare, country}`.
  - `ft8-audio-level` → `AudioLevel` `{peak_dbfs, rms_dbfs}` (added 2026-08-06): the RX capture level, measured per 250 ms window, dBFS rounded to 0.1. Delivery is PULL + latest-wins per subscriber (~4 Hz per writer ticker; review d22eff6b — pushed meter ticks could fill the subscriber buffer during a write stall and get the stream evicted): a slow client receives the NEWEST reading, never a backlog. Published only while a capture session is live — silence arrives as the finite −120 floor, which is what distinguishes "silent but alive" from "no capture" (nothing published; clients should treat a >~2 s gap as stale). Classification (good/low/high) is the client's, against the `ft8_audio` window served on `/v1/config`.
- **Errors:** 503 `server_busy`.
- **Notes:** All events except `ft8-logged` and `ft8-audio-level` are replay-cached for late subscribers (`ft8-logged` is not — replay would dup a session row; `ft8-audio-level` is not — the next window lands within 250 ms and a replayed stale level would defeat the client's staleness check). **Demand-driven:** the first subscriber acquires the audio capture device, the last (after a linger) releases it. Live decode needs a CGO build; the static build keeps the subsystem idle (keepalives only).

### `POST /v1/ft8/tx/arm`
- **Purpose:** Arm/disarm the FT8 transmit path — explicit operator gate before any FT8 RF (ADR 0030 e1).
- **Gating:** **Only when FT8 is enabled.**
- **Request:** Body `{"armed": bool}` — `armed` is **required**: a missing boolean is a 400, never a silent disarm; unknown or duplicate keys are rejected (AW-2). Idempotent.
- **Response:** **202 Accepted** (state confirmed via `ft8-tx` SSE).
- **Errors:** 400 `invalid_json`/`missing_required_field`/`ft8_tx_bad_message`; 503 `ft8_tx_unavailable`/`rig_not_ready`/`rig_dial_unknown`; 409 `ft8_tx_not_armed`/`ft8_tx_in_flight`; 500 `ft8_tx_failed`.
- **Notes:** Arming acquires the output device + builds the slot controller; disarming aborts any in-flight TX (PTT drops) + releases the device.

### `POST /v1/ft8/tx/send`
- **Purpose:** Queue one standard FT8 message for the next UTC slot at a chosen audio offset (ADR 0030 e1).
- **Gating:** **Only when FT8 is enabled.** Requires TX armed.
- **Request:** Body `{"message": string, "offset_hz": float64}`.
- **Response:** **202 Accepted** (completion/failure via `ft8-tx` SSE).
- **Errors:** 400 `invalid_json`/`ft8_tx_bad_message`/`ft8_no_offset`/`ft8_bad_offset`; 503 `ft8_tx_unavailable`/`rig_not_ready`/`rig_dial_unknown`; 409 `ft8_tx_not_armed`/`ft8_tx_in_flight`; 500 `ft8_tx_failed`. (`offset_hz` is daemon-validated against the usable passband — review 2026-06-19 M1.)

### `POST /v1/ft8/qso/start`
- **Purpose:** Begin a manual answer-a-CQ exchange the daemon auto-advances CQ→73 (ADR 0031 e3).
- **Gating:** **Only when FT8 is enabled.** Requires TX armed.
- **Request:** Body `{"their_call" (req), "their_grid"?, "slot_utc"?, "offset_hz"?, "operating_freq_mhz"?, "mode"?, "their_snr"?, "answer_mode"?}`. Our own callsign/grid are resolved server-side from station config. `mode:"fd"` answers a `CQ FD` with the operator's ARRL Field Day exchange (class/section from `ft8.field_day` config; `ft8_field_day_unset` 400 if unset) — see ft8.md "ARRL Field Day operating". `mode:"type4"` answers a NONSTANDARD/compound-call CQ (`CQ PJ4/NA2AA`, `CQ K1ABC/D`) with the reduced type-4 ladder (bare-calls→RR73→73, no grid/report on the wire — ADR 0048); needs no config identity (our own call is standard). "" / "standard" is the normal grid/report answer. `their_snr` (our SNR of the clicked CQ) is logged as RST_SENT for `mode:"fd"` and `mode:"type4"` (neither exchanges a report; ignored for a standard answer, whose report comes from the exchange). `answer_mode` (ADR 0066/0067 — the SESSION's Answer mode, the ONE run input: an auto mode arms a hands-off run alongside this contact, `operator_pick` leaves a listing run that transmits at nobody until a pop/bag; absent/empty → the config default, junk → `invalid_field_value` 400). A fresh start replaces any previous run. Standard exchange mode only — `mode:"fd"`/`"type4"` never arm a run. (The ADR 0065 per-click `auto_work` intent field retired with ADR 0067; it is the one legacy key still accepted — an old client that sends it is tolerated, while any OTHER unknown field is now rejected with 400 `invalid_json` (AW-2).)
- **Response:** **202 Accepted** (progress via `ft8-qso` SSE).
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`no_station_callsign`/`ft8_no_offset`/`ft8_bad_offset`/`ft8_no_frequency`; 409 `ft8_tx_not_armed`/`ft8_qso_in_progress`; 503 `db_unavailable`. **503 `db_unavailable`** when the logbook datastore cannot be read: the station identity is unresolvable, so no sequencer session starts and no PTT is keyed; the existing TX-arm state is unchanged (this route requires TX already armed, and the refusal does not disarm it). Distinct from 400 `no_station_callsign`, which means the callsign is genuinely unconfigured — the two demand opposite actions and were collapsed into the 400 until 2026-08-01 (audit finding A7).
- **Notes:** `slot_utc` fixes the worked station's parity. `offset_hz` is daemon-validated against the usable passband (M1) and `operating_freq_mhz` must be a positive known-band dial frequency (M2 — refused before the on-air exchange, since a QSO is logged only at completion). Logged QSO `FREQ`/`BAND` come from `operating_freq_mhz` (the rig dial); `offset_hz` is TX audio placement only, never folded into FREQ.

### `POST /v1/ft8/qso/work`
- **Purpose:** Begin working a station that is calling US, picked from the pile-up (ADR 0033 "work a caller"). Caller-style exchange (we report first → RR73 → log), then **idle** (does not loop to CQ). For tail-enders / answerers that call us when we're not in a Call-CQ session.
- **Gating:** **Only when FT8 is enabled.** Requires TX armed + no session in flight.
- **Request:** Body `{"their_call" (req), "their_grid"?, "their_snr"?, "slot_utc"?, "offset_hz"?, "operating_freq_mhz"?, "mode"?, "their_class"?, "their_section"?, "answer_mode"?}`. `their_snr` is the SPA's SNR of the picked decode — the report we send (RST_SENT). Our own callsign/grid resolved server-side. `mode:"fd"` works a caller who called us with an ARRL Field Day exchange — the SPA parsed `their_class`/`their_section` from `<ourCall> <theirCall> <class> <section>`, and our class/section come from `ft8.field_day` config (`ft8_field_day_unset` 400 if unset). `mode:"type4"` works a NONSTANDARD/compound caller with the reduced type-4 ladder (single RR73 rung, no report — ADR 0048); needs no config identity. "" / "standard" is the normal grid/report work. Both bodies also accept `allow_duplicate` (bool, optional, default false) — the operator's EXPLICIT "work this station again" intent. QSO storage deduplicates on call+band+mode+freq+date+HH**MM**, so a deliberate second contact with the same station inside one minute would otherwise hash to the first's key and be silently discarded: the operator transmits a full exchange and no row appears (reachable on the short ladders — work-a-caller and the single-rung type-4 work path). The flag is pinned to the session AT ARM TIME (like the logbook, ADR 0055), stamped onto the completed QSO, and passed to `qsoservice.Submit` as `force`. The SPA sets it only from an operator action on a station it already shows as worked this session; `/v1/ft8/cq/start` does NOT take it (a Call-CQ run works whoever answers, so there is no per-station intent to express). Both bodies also accept `answer_mode` (ADR 0066/0067 — the SESSION's Answer mode, the ONE run input, same semantics as on `qso/start`: an auto mode arms a hands-off run, `operator_pick` leaves a listing run, absent/empty → the config default, junk → `invalid_field_value` 400; the retired ADR 0065 `auto_work` intent field is the one legacy key still accepted if an old client sends it, while any OTHER unknown field is now rejected with 400 `invalid_json` — AW-2). Standard exchange mode only — FD/type-4 never arm a run.
- **Response:** **202 Accepted** (progress via `ft8-qso` SSE).
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`no_station_callsign`/`ft8_no_offset`/`ft8_bad_offset`/`ft8_no_frequency`/`ft8_tx_bad_message`; 409 `ft8_tx_not_armed`/`ft8_qso_in_progress`; 503 `rig_not_ready`/`rig_dial_unknown`/`db_unavailable`. **503 `db_unavailable`** when the logbook datastore cannot be read: the station identity is unresolvable, so no sequencer session starts and no PTT is keyed; the existing TX-arm state is unchanged (this route requires TX already armed, and the refusal does not disarm it). Distinct from 400 `no_station_callsign`, which means the callsign is genuinely unconfigured — the two demand opposite actions and were collapsed into the 400 until 2026-08-01 (audit finding A7).
- **Notes:** `slot_utc` fixes the caller's parity. Logged QSO `FREQ`/`BAND` come from `operating_freq_mhz` (the rig dial); `offset_hz` is TX audio placement only, never folded into FREQ.

### `POST /v1/ft8/cq/start`
- **Purpose:** Begin a sequenced Call-CQ session that works answerers (ADR 0033). Answerer-selection mode = the SESSION's `answer_mode` on this request (ADR 0066; absent → the `ft8.tx.caller_answer_mode` default, `operator_pick` since 2026-08-08): `auto_first`; `auto_strongest`; `operator_pick` per ADR 0065 — answerers are LISTED on the `ft8-qso` frames (`answerers`) instead of auto-committed, the CQ keeps calling until the operator pops one via `POST /v1/ft8/cq/pick`, and the run works that station then resumes CQ.
- **Gating:** **Only when FT8 is enabled.** Requires TX armed.
- **Request:** Body `{"offset_hz": float64, "operating_freq_mhz": float64, "tx_parity"?: "even"|"odd", "answer_mode"?: "auto_first"|"auto_strongest"|"operator_pick"}`. Callsign/grid resolved server-side. `tx_parity` is the operator's chosen CQ slot parity (WSJT-X "Tx even/1st" — `even` = :00/:30, `odd` = :15/:45); **omitted/empty/any-other value = call CQ on the next slot regardless of parity** (the default, fastest first CQ). `answer_mode` is the SESSION's answerer-selection mode (ADR 0066, the TX control bar's Answer selector) — omitted/empty = the config default; junk → `invalid_field_value` 400. Both are operating state, sent per session — not persisted daemon settings. Caller-side only (answering a CQ forces the opposite parity).
- **Response:** **202 Accepted** (progress via `ft8-qso` SSE).
- **Errors:** 400 `invalid_json`/`no_station_callsign`/`ft8_no_offset`/`ft8_bad_offset`/`ft8_no_frequency`/`invalid_field_value`; 409 `ft8_tx_not_armed`/`ft8_qso_in_progress`; 503 `db_unavailable`. (`ft8_caller_mode_unsupported` 501 retired 2026-08-07 — operator_pick is implemented, ADR 0065.) **503 `db_unavailable`** when the logbook datastore cannot be read: the station identity is unresolvable, so no sequencer session starts and no PTT is keyed; the existing TX-arm state is unchanged (this route requires TX already armed, and the refusal does not disarm it). Distinct from 400 `no_station_callsign`, which means the callsign is genuinely unconfigured — the two demand opposite actions and were collapsed into the 400 until 2026-08-01 (audit finding A7).
- **Notes:** One session at a time. Stopped via the shared abandon route below.

### `POST /v1/ft8/cq/pick`
- **Purpose:** Commit a listed answerer into an **operator_pick** Call-CQ run (ADR 0065 decision 3): the run's next slot evaluation transmits our report to that station instead of the CQ; on RR73 the QSO logs and the run resumes CQ. The candidate list the client picks from rides the `ft8-qso` frames (`answerers`, call+SNR; grid/offset/dial stay daemon-side). Sequencing rules: `internal/ft8/operatorpick_test.go`.
- **Gating:** **Only when FT8 is enabled.**
- **Request:** Body `{"call": string}` (required).
- **Response:** **202 Accepted** — the commit is confirmed by push (the `ft8-qso` frame the pop publishes: the contact on the reporting rung, the popped call gone from `answerers`).
- **Errors:** 400 `invalid_json`/`invalid_field_value` (missing call); **409 `ft8_no_cq_pick_run`** (idle, or the live run is an auto mode — a pop against auto is client drift); **404 `ft8_answerer_not_listed`** (never heard, or unheard past the 3-min staleness bound — the station left; wait for a fresh answer); **409 `ft8_cq_contact_in_flight`** (finish the contact or press Next first — a pop never silently ends one). The three are deliberately distinct: the operator's next action differs.
### `POST /v1/ft8/cq/bag`

- **Purpose:** Bag a LISTED pick-run caller into the queue (ADR 0067 slice B) — the operator's explicit "work this one too". Bagged stations are auto-worked by the drain in bag order (each was individually chosen, so the drain keeps every transmission operator-selected); stale entries (unheard past the 3-min bound) are expired at drain, never transmitted at.
- **Request:** Body `{"call": string}`.
- **Response:** **202**; the moved station rides the next `ft8-qso` frame (`queue` vs `answerers`). Refusals mirror cq/pick: 409 `ft8_no_cq_pick_run` / 404 `ft8_answerer_not_listed`.

### `POST /v1/ft8/cq/unbag`

- **Purpose:** Return a bagged station to the listed set (its heard-time intact — normal expiry governs it there).
- **Request:** Body `{"call": string}`.
- **Response:** **202**. Refusals as bag.

### `POST /v1/ft8/cq/resume`

- **Purpose:** Continue a paused pick-queue drain (the run surface's Stop pauses — queue kept — per ADR 0067; this is the drawer's Resume).
- **Request:** empty body.
- **Response:** **202**. 409 `ft8_no_cq_pick_run` when no pick context exists.


### `POST /v1/ft8/qso/path`
- **Purpose:** Record the operator's antenna-path choice for the active FT8 exchange (the short/long radio in the FT8 enrichment panel). **Logging-only** — it annotates the logged QSO (ADIF `ANT_PATH`, plus the matching great-circle `ANT_AZ` + `DISTANCE`) and never touches the on-air signal. FT8 QSOs are built daemon-side, so unlike Phone/CW (where the SPA stamps it at submit) the choice is sent here.
- **Gating:** **Only when FT8 is enabled.**
- **Request:** Body `{"path": "S"|"short"|"L"|"long"}` (case-insensitive). Lenient — anything other than long is treated as short.
- **Response:** **202 Accepted.**
- **Notes:** Settable any time during a contact; read once when the exchange completes. Resets to short at the start of each exchange, so a prior contact's "long" never carries over. `ANT_AZ`/`DISTANCE` are only stamped when both grids resolve; `ANT_PATH` is stamped regardless.

### `POST /v1/ft8/qso/abandon`
- **Purpose:** Drop any active sequenced session (answer-a-CQ or Call-CQ). **Only when FT8 is enabled.** No body. **202 Accepted**, idempotent (no-op while idle). No error codes.

### `POST /v1/ft8/autowork/stop`
- **Purpose:** Disarm an armed auto-work run **without ending any active contact** (ADR 0065 — the Auto-work pill's click action). Abandon stops run AND contact; this stops only the run. **Only when FT8 is enabled.** No body. **202 Accepted**, idempotent (stopping a stopped run publishes nothing). No error codes.
- **Notes:** The cleared state rides the `ft8-qso` SSE: with a contact active it is the live session frame with `auto_work_armed:false`; idle-and-armed it is a terminal-shaped frame (`active:false`, no `end_reason` — an operator action on the RUN, not a session end).

### `POST /v1/ft8/qso/next`
- **Purpose:** Short-circuit the repeat cap on a **stuck Call-CQ contact**: park this answerer at the next slot evaluation, then work another live answerer from that slot or resume calling CQ. The run **continues** — ending it is Abandon's job. **Only when FT8 is enabled.**
- **Request:** No body.
- **Response:** **202 Accepted**. The pending state rides the `ft8-qso` SSE as `next_armed` (confirm-by-push); it clears when the park happens or the contact advances.
- **Errors:** **409 `ft8_no_answerer`** when no answerer is being worked — idle, a Call-CQ run that is merely calling, or an answer/work session (whose Next is `qso/skip`). A third distinct refusal alongside `ft8_no_active_qso` and `ft8_rung_not_skippable`.
- **Notes:** Deliberately **not** the skip route. Skip fires on a **silent** cycle; this exists for a station that keeps transmitting the same rung and never advances, so a skip-shaped trigger would never fire — the trigger is "did not advance", not "did not transmit". It is the repeat cap fired early, through the same `parkAnswererLocked` off-ramp, so the parked station gets the cap's **per-round** exclusion (cleared when the rescan is empty or a contact completes) — not a session-long lockout. Fires on **both** capped rungs; on the closing RR73 the contact is dropped **without logging** (Group B — the partner never got the roger). The transmission already on the air finishes: the park is deferred to the next slot evaluation, since a replacement can only be picked from a slot's decodes.

### `POST /v1/ft8/qso/skip`
- **Purpose:** Arm/disarm **skip-if-silent** on the active sequenced session (the operator's deferred Next, daemon-side): armed, a silent cycle on an already-transmitted rung ends the session **instead of keying the repeat** — no RF at a station the operator has decided to drop. **Only when FT8 is enabled.**
- **Request:** Body `{"armed": bool}` — `armed` is **required**: a missing boolean is a 400, never a silent disarm; unknown or duplicate keys are rejected (AW-2).
- **Response:** **202 Accepted**. The armed state rides the `ft8-qso` SSE as `skip_armed` (confirm-by-push); the skip firing publishes the idle status.
- **Errors:** 400 `missing_required_field`/`invalid_json`; **409 `ft8_no_active_qso`** when nothing is running; **409 `ft8_rung_not_skippable`** when a session IS running but sits on a rung with no skip path — a terminal RR73/73, or a Call-CQ run (whose Next is `POST /v1/ft8/qso/next`, below). Skippability is a property of the RUNG, not the session mode. Disarm is always accepted (idempotent, including when idle).
- **Notes:** The arm clears itself when the partner replies (they came back), on session start, and on Abandon. Applies to answering + working sessions, standard and FD.

---

## Evidence capture (spot-network design §4.1)

### `GET /v1/evidence/status`
- **Purpose:** The local honesty surface for FT8 evidence capture (the default-off consent layer, config `evidence.capture`): capture state, physical disk usage against the cap/watermark, archive counts, and drops.
- **Gating:** **Always-on** — "disabled" is itself the answer when capture is off or no writer exists; gating the route would make "capture off" indistinguishable from "endpoint missing".
- **Response:** **200** `{"enabled": bool, "state": "disabled"|"capturing"|"drop_new", "degraded": bool, "status_error"?: string, "cap_bytes": int, "watermark_bytes": int, "usage_bytes": int|null, "observations": int|null, "unprofiled_observations": int|null, "dropped_slots": int, "profiles": {...}, "sync": {...}}`. `usage_bytes` is physical (evidence.db + WAL + shm), and is **`null` when the archive could not be measured** — evidence then fails a slot write closed as `measurement_error` rather than emitting a false zero (L2); `drop_new` means usage crossed the soft watermark and new capture is being counted as loss intervals rather than written; `unprofiled_observations` is the §5.4 missing-profile tally. `degraded` and `status_error` identify a failed database-derived group; that group's values are `null`, never plausible zeroes or partial totals. The endpoint remains 200 so capture state and in-memory loss information stay useful.
- **`profiles`** (§4.2 station profiles, 2026-08-10): `{"state": "disabled"|"none_declared"|"active"|"degraded", "reason"?: string, "lineages": int|null, "versions": int|null, "active"?: {"<band>": {"name", "profile_uuid", "version", "valid_from"}}, "unprofiled": {"<reason>": int}|null}`. When `state` is `disabled` the lineage/version counts are **`null`, never 0** — the store is deliberately not open. A failed profile aggregate makes `lineages`, `versions`, and `unprofiled` unavailable together; an empty `{}` means a successful grouping with no rows. `reason` appears only with `degraded` (activation/reconciliation failed; new rows carry `unprofiled_reason: "profile_error"` and the stale prior mapping is not used). `unprofiled` splits the NULL tally by reason: `legacy_unprofiled | no_declaration | band_unmapped | dial_unattributed | profile_error`. This surface is the quiet home of unprofiled data (PR7): no toast or banner elsewhere; only the *transition into* `degraded` logs, once.
- **`sync`** (§5 sync slice, 2026-08-10): `{"enabled": bool, "state"?: "idle"|"backoff", "last_success_utc"?: string, "last_error"?: string, "unsynced": {"<kind>": int}|null, "quarantined": int|null}`. `enabled` mirrors config `evidence.sync` (consent layer 2 — reuses the smcloud forwarder's credentials); `backoff` = SMC currently unreachable or refusing, with `last_error` saying why — distinguishable from consent-off, which shows `enabled: false` and never attempts. `unsynced` counts offerable rows per kind (`observation | coverage | loss_interval | profile`); `quarantined` counts rows refused permanently by SMC (kept locally, never re-offered — SY2's honesty surface). A failed count query makes both fields `null`; it never omits one kind or understates quarantine. Like `profiles`, per-attempt sync noise stays out of the log: only the *transition into* `backoff` warns, once.
- **`retention`** (retention slice, 2026-08-10): `{"purged_observations": int|null, "purged_coverage": int|null, "records": int|null, "metadata_bytes": int|null, "pressure"?: "cap"|"metadata"}`. These database-derived fields are all `null` when their group cannot be read (including `metadata_bytes`, L2). At cap pressure the writer PURGES instead of dropping (cloud-present rows first, then oldest unsynced — current capture wins), each chunk receipted atomically; `records`/`purged_*` are those receipts' totals. `pressure` appears only while capture is in `drop_new` and says WHY: `cap` = nothing purgeable remains; `metadata` = the loss/retention receipt budget is exhausted and compaction cannot shrink it — an unreceipted purge never happens, so capture stops instead. `metadata_bytes` tracks the logical receipt footprint against its 4 MiB budget.

---

## Diagnostics & SPA

### `GET|POST /debug/pprof/*`
- **Purpose:** Go runtime profiling (goroutine/heap/CPU/trace). Development affordance, not a stable contract; lives outside `/v1/*`.
- **Gating:** **Only when `Server.EnableProfiling` is true** (off by default; logs a warning at mount).
- **Routes:** `GET /debug/pprof/`, `/cmdline`, `/profile`, `/symbol` (GET+POST), `/trace`. Standard `net/http/pprof` semantics (`profile?seconds=N` blocks N seconds — a DoS vector, so it stays off by default).
- **Notes:** Registered on this mux (not `http.DefaultServeMux`); method-specific GET registration keeps these patterns clean under Go 1.22 ServeMux. Since W-0003 the root `GET /` is the SPA catch-all, so a profiling-off `/debug/pprof*` path falls through to it — but `spaHandler` explicitly **404**s the `/debug/pprof` namespace (like `/v1`) rather than serving SPA HTML, so an unmounted pprof path stays a plain 404.

### `GET /` (embedded SPA at the canonical root); `GET /app`, `GET /app/` (bookmark redirects)
- **Purpose:** Serve the embedded Svelte **app SPA** (ADR 0044) — the SOLE embedded operator client — at the **canonical root** `/` (W-0003), with client-side-routing fallback. `GET /` is the catch-all: any GET not matched by a more-specific pattern (`/v1/*`, `/debug/pprof/*`, `/manual/*`, `/app`, `/app/`) falls through to `index.html`, so `/operate`, `/logbook`, `/config`, and deep-link reloads are client-side routes. The retired `/app/` transition mount's URLs **301-redirect** (permanent — saved bookmarks) to their root equivalents; the former `/config`,`/logbook` 307-compat redirects are gone (those are now real shell routes served by `index.html`).
- **Gating:** **Only when `Protocol == "tcp" && *ServeSPA`** (browsers need TCP; a headless Unix-socket daemon leaves the root mount AND the `/app` redirects unregistered, so `/`, `/config`, `/logbook`, `/app` are all plain 404s there).
- **Routes:** `GET /` → `spaHandler(AppFS())` (the catch-all — loses to every more-specific pattern). `GET /app`, `GET /app/` → `redirectAppToRoot` (**301**): `/app`,`/app/` → `/`; any `/app/{path}` → `/{path}`, query string preserved. `spaHandler` explicitly **404**s an unmatched server-namespace path (`/v1`, `/v1/*`, `/debug/pprof`, `/debug/pprof/*`) rather than SPA-falling-through — load-bearing now that every unmatched path reaches the root catch-all (there is no `/app` prefix quarantining it).
- **Response:** **200** with the static asset, or `index.html` when the path doesn't resolve to a file (SPA-router fallback, so a refresh on `/logbook` etc. doesn't 404). `Cache-Control: no-cache` on every asset (stable, hash-free entry bundle). `GET /app[/{path}]` → **301** to the root equivalent, query preserved.

### `GET /manual/` (embedded operator manual)
- **Purpose:** Serve the embedded operator manual — a single self-contained, zero-JS Hugo page (ADR 0036). Distinct from the SPAs: plain static files, **no** client-side-router fallback.
- **Gating:** Same as the SPAs — **only when `Protocol == "tcp" && *ServeSPA`**. The on-disk copy (`/usr/share/doc/station-manager/manual/`, shipped in the RPM) covers reading it from `file://` when the daemon isn't serving.
- **Routes:** `GET /manual/` → `StripPrefix("/manual", manualHandler(manual.FS()))` (subtree pattern; bare `/manual` 301→`/manual/`). A subtree pattern, matched by ServeMux precedence ahead of the root `GET /` SPA catch-all.
- **Response:** **200** with the static page, **404** for any unresolved path (no SPA fallback — it's static). `Cache-Control: no-cache` (the manual is rebuilt with the daemon, so the served copy always matches the running version).

---

## Related

- [api.md](api.md) — the API design brief (rationale, cross-cutting decisions, the decision trail).
- [frontend-spa.md](frontend-spa.md) — SPA embed/serving (the root `/` mount and its client-routing fallback).
- [decisions/0036-operator-manual-embedded-zero-js-site.md](../decisions/0036-operator-manual-embedded-zero-js-site.md) — the operator manual at `/manual/` (Hugo, zero-JS, embedded + on-disk).
- ADRs: [0010](../decisions/0010-rig-sse-wire-shape.md) (SSE/error shape), [0013](../decisions/0013-daemon-owns-bridge-as-subsystem.md), [0017](../decisions/0017-enrichment-pipeline-domain-table-cache.md), [0026](../decisions/0026-rig-command-path-freq-mode.md), [0027](../decisions/0027-tune-carrier-control.md), [0028](../decisions/0028-rig-profiles-single-active-hotswap.md), [0029](../decisions/0029-ft8-transmit-manual-sequencing.md)/0030/0031/0033 (FT8).
