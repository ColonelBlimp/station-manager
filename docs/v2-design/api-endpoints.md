# Station Manager — Daemon HTTP API Endpoint Reference

> **Status:** canonical, complete endpoint list (audited 2026-06-14). This is the
> **reference**; the rationale/decision narrative lives in [api.md](api.md) (the API
> *design brief*). When the two disagree, the **handler source is authoritative** —
> routes are registered in `internal/api/server.go`, handlers in
> `internal/api/handler_*.go`. Update this file in the same commit as any route change.

All application endpoints live under the `/v1/*` prefix (API versioning — unrelated to
the project's v1/v2 distinction). The embedded SPAs are served at `/`, `/config/`,
`/logbook/`, and `/app/` (the consolidated full-replacement client, ADR 0044); the
operator manual at `/manual/`; profiling at `/debug/pprof/*`.

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
unregistered, the path is a **404** (there is no root SPA catch-all as of 2026-07-21 —
`GET /` exact-redirects to `/app/`; a headless daemon has no SPA routes at all).

---

## QSO

### `POST /v1/qso`
- **Purpose:** Submit exactly one QSO as ADIF — the primary logging write path (logging SPA, `curl`, import tooling). Bulk import is a separate tool (`cmd/importer`), not this endpoint.
- **Gating:** Always-on; additionally wrapped in `limitSubmitRate`.
- **Request:** Content-Type `application/x-adif` / `text/plain` / empty (other → 415 `unsupported_media_type`). Body = a single-record ADIF doc. Query: `logbook` (int, **required**, ≥1; must exist), `force` (bool, optional — bypass dedupe). **`STATION_CALLSIGN` is derived from the logbook** (ADR 0055): the daemon stamps the QSO with the logbook's own callsign, so a record's own `STATION_CALLSIGN` is ignored on a live submit and need not be present (an **import** keeps the record's value for historical fidelity).
- **Response:** **201** on store / **200** on duplicate. Body `{"status": "stored"|"duplicate", "uuid": string, "id": int64}`.
- **Errors:** 400 `invalid_adif`, `too_many_records`, `missing_required_field`, `invalid_field_value`, `missing_required_param`, `invalid_id`, `invalid_query_param`, `invalid_time_range`; 404 `logbook_not_found`; 429 `rate_limited`; 503 `server_busy`; 500 `submit_failed`/`db_error`.
- **Notes:** One-fails-all-fail atomic write (QSO row + upload-queue rows in one tx). Idempotent dedupe — a known duplicate returns 200 with the existing UUID/ID. **Dedupe is minute-precision** (`CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON`, TIME_ON truncated to HHMM), so two contacts in the same minute collide — even though times are **stored and exported at full `HH:MM:SS`** where supplied (ADIF `HHMMSS` preserved since migration 0003; seconds matter to QSL managers matching on exact timestamp). A **midnight-crossing** QSO (`TIME_ON` > `TIME_OFF`) must carry `QSO_DATE_OFF` on the **following day** or it's rejected `invalid_time_range`; SM's own clients (logging SPA + FT8 daemon) **always populate `QSO_DATE_OFF`** — the date at `TIME_OFF`, equal to `QSO_DATE` for a same-day contact.

### `GET /v1/qso/{uuid}`
- **Purpose:** Fetch one QSO by canonical UUIDv7 (SPA edit/detail).
- **Gating:** Always-on.
- **Request:** Path `{uuid}` (UUIDv7).
- **Response:** **200**, body = full `types.Qso`. Excludes soft-deleted rows.
- **Errors:** 400 `invalid_uuid`; 404 `not_found`; 500 `db_error`.

### `PATCH /v1/qso/{uuid}`
- **Purpose:** Update fields of an existing QSO (SPA edit flow).
- **Gating:** Always-on.
- **Request:** Path `{uuid}`. JSON body overlaid on the existing QSO (empty → no-op); immutable fields (upload statuses) stash-restored.
- **Response:** **200**, body = updated `types.Qso`.
- **Errors:** 400 `invalid_uuid`/`invalid_json`/`missing_required_field`/`invalid_field_value`/`invalid_time_range`; 409 `duplicate_key`; 404 `not_found`; 500 `update_failed`/`db_error`.

### `DELETE /v1/qso/{uuid}`
- **Purpose:** Soft-delete a QSO and enqueue a delete-forward.
- **Gating:** Always-on.
- **Request:** Path `{uuid}`.
- **Response:** **204**.
- **Errors:** 400 `invalid_uuid`; 404 `not_found`; 500 `db_error`.
- **Notes:** Soft-delete + upload-queue delete row, atomic.

### `GET /v1/qso/{uuid}/uploads`
- **Purpose:** Per-forwarder upload-status snapshot for one QSO (pull counterpart to the `forward.*` SSE events).
- **Gating:** Always-on.
- **Request:** Path `{uuid}`.
- **Response:** **200**, body `{"items": [types.QsoUpload, …]}` (never null; ordered by forwarder_name, action). Includes soft-deleted QSOs (delete forwarding stays observable).
- **Errors:** 400 `invalid_uuid`; 404 `not_found`; 500 `db_error`.

### `POST /v1/forwarder/{name}/uploads`
- **Purpose:** Manual upload backfill (ADR 0039) — queue already-stored QSOs for upload to one **enabled** forwarder. The load-bearing path for QSOs logged while a forwarder was disabled, pre-dating it, or otherwise never auto-queued; the logbook SPA selects rows and pushes them to one destination at a time. The existing per-destination worker drains the rows.
- **Gating:** Always-on (the destination must itself be enabled — see Errors).
- **Request:** Path `{name}` (the forwarder's config name). Body `{"uuids": ["…", …], "force": false}`. `uuids` are deduplicated; `force` (default false) re-sends QSOs already uploaded to this destination instead of skipping them.
- **Behaviour:** action is always `insert`. Each UUID lands in exactly one bucket — enqueued, `skipped_uploaded` (already on this destination, no force), `skipped_deleted` (soft-deleted; not backfilled), or `not_found` (unknown/malformed). Per-QSO best-effort: one bad UUID never fails the rest. "Already uploaded" = an existing insert-upload row at status `uploaded` for this forwarder (keyed by name, so N-agnostic across forwarder types).
- **Response:** **200**, body `{"enqueued": N, "skipped_uploaded": M, "skipped_deleted"?: ["…"], "not_found"?: ["…"]}`.
- **Errors:** 400 `invalid_forwarder` (empty name); 400 `missing_required_field` (empty `uuids`); 400 `batch_too_large` (> 5000 uuids); 400 `forwarder_unavailable` (forwarder unknown, **disabled**, or doesn't forward inserts — a disabled forwarder has no worker and gets its queue rows discarded at startup, so enqueuing would strand them); 400 malformed body; 500 `enqueue_failed`.

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
- **Purpose:** Update name/description (callsign not editable here). **Always-on.** Body `{"name": *string, "description": *string}` (presence-aware). **200** updated `types.Logbook`. Errors: 400 `invalid_id`/`invalid_field_value`/`invalid_json`; 404 `not_found`; 409 `duplicate_name`; 500 `db_error`.

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
- **Response:** **200** `{"status": "sent", "emailed": []string, "date": "YYYYMMDD"}`.
- **Errors:** 503 `mailer_disabled`; 400 `invalid_json`/`missing_required_field`/`invalid_field_value`/`no_qsos`; 500 `fetch_failed`/`adif_compose_failed`; 502 `smtp_failure`.
- **Notes:** Daemon rebuilds ADIF from live DB rows (not the client blob), archives it under `<workingDir>/exports/sent-adif/` (best-effort, exclusive-create with a `-N` collision suffix so a reused/same-second name never overwrites a prior backup), then stamps `sm_fwrd_by_email_*` on the rows. Unknown UUIDs are skipped with a warning. `uuids` is capped at 10000 per request (`invalid_field_value` 400).

### `POST /v1/session/export`
- **Purpose:** Download a session's QSOs as an ADIF file (Export dialog "Download ADIF"). Same rebuild-from-DB path as `email` minus the SMTP send — so the download carries the fully enriched stored record, not the SPA's pre-submit subset.
- **Gating:** Always-on route (no mailer gating).
- **Request:** Body `{"uuids": []string (req, non-empty), "filename"?}` (filename defaulted `session-YYYYMMDD-HHMMSS.adi`; an operator-supplied name is a bare attachment name — traversal rejected).
- **Response:** **200** `application/x-adif` with `Content-Disposition: attachment; filename="…"`; body is the composed ADIF document.
- **Errors:** 400 `invalid_json`/`missing_required_field`/`invalid_field_value`/`no_qsos`; 500 `fetch_failed`/`adif_compose_failed`.
- **Notes:** Daemon rebuilds via `adif.ComposeToAdifString(FetchQsoByUUID…)` and archives a backup under `<workingDir>/exports/sent-adif/` (best-effort, same dir as email — backup-on-export, exclusive-create with a `-N` collision suffix). Unknown UUIDs are skipped with a warning. `uuids` is capped at 10000 per request (`invalid_field_value` 400). Does **not** stamp rows (only an email marks "forwarded"). Fetch loop shared with `email` via `Server.fetchSessionQsos`.

---

## Config & hardware

### `GET /v1/config`
- **Purpose:** Operator-relevant config + joined display details (config / My Station SPA).
- **Gating:** Always-on.
- **Response:** **200**, body = `ConfigResponse`: `setup_complete` (bool), `logging_station` (`types.LoggingStation`), `default_logbook` (`types.Logbook`, DB-joined), `default_rig` (`types.RigConfig`, active rig), `station` (`types.StationConfig`), `bridge` (`BridgeInfo`: `enabled`, `driver?`, `rig_name?`, `rig_modes?`, `ops?`, `tune?`, `mode_mappings?` — merged rigdef-defaults + overrides for the active driver — and `ft8_mode?`, the rig CAT mode literal for FT8 (rigdef default + per-rig override) the FT8 band buttons assert), `mailer` (`MailerInfo`: `enabled`, `default_recipient?`), `qsl` (`types.QslDefaults`: standing outgoing-QSL defaults `qsl_via` / `qslmsg` / `qsl_sent_via`), `ft8_display` (resolved `types.Ft8DisplayConfig`), `ft8_frequencies` (resolved `map[string]int`), `ft8_caller_answer_mode` (resolved string, default `auto_first`), `ft8_max_repeats` (resolved int, default 6 — the FT8 sequencer's repeat cap: unanswered pre-final rungs, and also the CLOSING rung on the ladders where the partner is still waiting for it, bounding a rung that fails to transmit — see `internal/ft8/finalrung.go`), `ft8_field_day` (`types.Ft8FieldDayConfig` — operator's Field Day class+section, empty `{}` when unset), `bridge_timeouts` (resolved `types.BridgeTimeoutsConfig`) and `bridge_tune` (resolved `types.BridgeTuneConfig`) — both **sparse on disk, served resolved** (defaults filled, ceilings applied via `bridge.ResolveTimeouts`/`ResolveTune`) so the SPA reads effective values config.json doesn't materialise (config.md §15).
  Also `forwarders` (`[]ForwarderInfo`, **masked** — present only when ≥1 configured): each `{name, type, enabled, action_filter?, credentials_set?}` where `credentials_set` lists the credential keys that hold a value — **never the values** (masked-on-GET).
  Also `smtp` (`SmtpInfo`, the config SPA's Email tab, **masked**): `{enabled, host?, port?, username?, from?, default_recipient?, starttls, timeout_sec?, password_set}` — every field except the password, with `password_set` reporting whether a password is stored (the value is **never on the wire**, masked-on-GET). Distinct from the read-only `mailer` projection: `mailer` is the *logging* SPA's running-mailer view (enabled + recipient), `smtp` is the *config* SPA's persisted-intent edit surface (they can diverge until the daemon restarts to pick up a saved change).
  Also `psk_reporter` (`types.PskReporterConfig`, the config SPA's FT8 tab): `{enabled?, host?, port?}` — opt-in public upload of FT8 reception spots. No secrets (receiver identity comes from `logging_station`), so the canonical type rides the wire unmasked. Served **raw/sparse**: an empty host/port means "use the production collector default" (`report.pskreporter.info:4739`, resolved daemon-side), so the SPA shows those as placeholders rather than materialising them. Also `ft8_decode_log` (`types.Ft8DecodeLogConfig`, the config SPA's FT8 tab): `{enabled, path?}` — the JTDX `ALL.TXT`-style decode log. No secrets. A nil block in config (never enabled) is served as a disabled zero value so the SPA form binds; an empty `path` means "use the default" (`$SM_WORKING_DIR/log/ft8-all.txt`, resolved daemon-side at open). Also `restore_rig_on_mode_switch` (`*bool`): whether a Phone/CW ↔ FT8 operating-mode switch auto re-tunes a CAT-live rig back to that mode's last freq/mode (SPA behaviour). Served **resolved** (always a definite bool — `true` when unset, the default ON).
- **Errors:** 500 `db_error`.
- **Notes:** Mailer/Bridge are read-only projections — the SMTP **password** and the serial port are never on the wire. The rest of the SMTP block IS editable via the masked `smtp` surface (config SPA). Forwarder/lookup credentials are likewise never emitted (only which keys are set).

### `PUT /v1/config`
- **Purpose:** Persist the operator-writable config subset (My Station / Settings save).
- **Gating:** Always-on.
- **Request:** Body = `ConfigResponse` shape. Writable: `logging_station`, `station`, `qsl` (presence-aware — omit to leave untouched), `ft8_display` (presence-aware — omit to leave untouched), `ft8_caller_answer_mode` (presence-aware string for the logging SPA's FT8 Settings tab — only `auto_first`/`auto_strongest` accepted; `operator_pick` or junk → `invalid_field_value` 400; sets `ft8.tx.caller_answer_mode`), `ft8_max_repeats` (presence-aware int for the logging SPA's FT8 Settings tab — the repeat cap — unanswered pre-final rungs, plus the closing rung on ladders where the partner awaits it (`internal/ft8/finalrung.go`); validated `[1, 10]` → `invalid_field_value` 400; sets `ft8.tx.max_repeats` and is **applied live** to the running sequencer — the one `/v1/config` field with a live side-effect, so the operator can drop a dead FT8 contact sooner mid-pile-up without a restart), `ft8_field_day` (presence-aware; the operator's Field Day class+section for answering CQ FD — normalised upper-case; class strict / section loose → `invalid_field_value` 400; sets `ft8.field_day`), `bridge.mode_mappings` (diffed against rigdef defaults; only deviations stored, on the active rig), and — for the config SPA's Rigs tab — `rigs` (`[]types.RigConfig`, replaces the whole catalogue) + `default_rig_id` (the active rig), both **presence-aware** and validated via `validateRigs`. `rigs`/`default_rig_id` are **write-only here**: never emitted on GET (the catalogue read surface is `GET /v1/rigs`; the active rig's narrow read view is the `default_rig` join). Also `forwarders` (`[]ForwarderInfo`) for the config SPA's Forwarding tab — **presence-aware** (omit to leave the list untouched; carry it to REPLACE the whole list), validated via `validateForwarders` (dup name / unknown type / unsupported action). Per entry, `credentials` (key→value) carries only the fields the operator typed; the daemon **merges** them onto the stored secrets by `name`, so a field that is **omitted OR blank** keeps its stored value — a client never has to strip empties to avoid destroying a credential. The exception is a field the type marks `CredentialField.Clearable` (currently only smcloud's `logbook`, and the dev stub's `mode`), where empty is a meaningful value the constructor defaults; a clearable blank is stored as the canonical `""`, never the whitespace as sent. The advanced knobs `tick_interval_sec`/`batch_size`/`retry` carry over from the stored entry. Every **enabled** forwarder in the merged list is then probed with `forwarding.Build` — the same call `spawnForwarderWorkers` makes at startup — so the PUT accepts exactly what the daemon can start with; a failure is 400 `forwarder_unusable` and the write is abandoned. The 400 message is **stable and sanitised** (forwarder name only) — constructors format the offending value into their own error and a stored `credentials.url` can carry userinfo, so the real cause goes to the daemon log, never the wire or the access log. Disabled entries are skipped (startup skips them too), so a destination stays saveable while half-configured. The probe runs only when the body carried `forwarders`, so one pre-existing bad destination can't block unrelated saves. During **first-run setup** it also runs in the pre-seed dry run, so a rejected setup PUT can't leave an orphaned default logbook behind. Also `smtp` (`SmtpInfo`) for the config SPA's Email tab — **presence-aware** (omit to leave the SMTP block untouched; carry it to REPLACE the block), validated via `validateSmtp` (enabled ⇒ host+from required, address + port + timeout sane → `invalid_smtp` 400). The `password` field carries only a freshly-typed value; the daemon **merges** it onto the stored secret, so a blank password keeps the stored one (`password_set` is ignored on PUT). Also `psk_reporter` (`types.PskReporterConfig`) for the config SPA's FT8 tab — **presence-aware** (omit to leave untouched; carry to REPLACE the block), validated via `validatePskReporter` (port in 0..65535, 0 = default → `invalid_psk_reporter` 400). No secrets, so it's taken as-sent. Also `ft8_decode_log` (`types.Ft8DecodeLogConfig`) for the config SPA's FT8 tab — **presence-aware** (omit to leave untouched; carry to REPLACE the block), no secrets, taken as-sent (no validation; an empty path resolves to the default at open). Restart-only — the log file opens at FT8 service start. Also `restore_rig_on_mode_switch` (`*bool`) — **presence-aware** (omit → untouched; carry to set), no validation; gates the SPA's CAT-live re-tune on a Phone/CW ↔ FT8 switch (default ON). `setup_complete`, `mailer`, `ft8_frequencies`, `bridge_timeouts`, `bridge_tune` are server-managed/ignored.
- **Response:** **200**, body = freshly-built `ConfigResponse`.
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`forwarder_unusable` (an enabled forwarder whose credentials won't construct — would abort daemon startup), plus the config validator's stable `{code, message}` findings (config.md §12 / ADR 0010); **409 `callsign_locked_to_logbook`** (a post-setup station-callsign change that would orphan the default logbook — reconcile logbooks first) / **`default_logbook_callsign_mismatch`** (setup found a logbook already at the default id under a different callsign); 500 `config_write_error`/`db_error`.
- **Notes:** Candidate runs `config.Normalize` + `config.Validate` (same pipeline as Load). The first PUT with a non-empty callsign completes setup (seeds default logbook, flips `setup_complete`). A **post-setup callsign change is rejected** when the new callsign no longer matches the default logbook's callsign — the operating callsign is currently a config field the submit gate checks against the logbook, so silently changing it would break every subsequent live submit; a logbook-scoped operating identity is deferred (review 2026-07-22 #1). Rewrites the whole `config.json` from in-memory state — **don't hand-edit config.json while the daemon runs.**

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
- **Purpose:** Daemon build / Go runtime / build environment / DB schema version. **Always-on.** **200** `{"daemon": string, "go": string, "env": "dev"|"release", "schema": {"version": uint64, "dirty": bool}?}` (always 200; `schema` omitted + warn-logged if the schema query fails). `env` is `dev` for any source build (incl. `task run:smd`) and `release` only for a packaged binary (the RPM build stamps `-X …/internal/buildinfo.Env=release`); the SPAs flag a `dev` daemon with a DEV pill + a tab-title prefix so it's distinguishable from the deployed one on the same `:8080`.

### `POST /v1/restart`
- **Purpose:** ATTENDED, operator-triggered graceful daemon restart — applies the "Requires a restart" config-apply changes (active rig, connection, mode mappings, serial overrides) without an on-box `systemctl`. The daemon runs its normal graceful shutdown (releasing the tune/FT8 carrier — TX-safe), then exits `ExitRestart` (3); systemd (`smd.service` `Restart=on-failure` + `RestartForceExitStatus=3`) respawns it ~`RestartSec` (5s) later and SSE clients auto-reconnect.
- **Response:** **202** (no body) — accepted; shutdown then respawn follows.
- **Errors:** **409** `tx_active` (a tune carrier / FT8 transmission is CURRENTLY keyed — stop transmitting first; a stuck/*unconfirmed* TX is NOT refused, so a recovery restart stays possible); **503** `restart_unavailable` (no service-manager restart wired — split-host / non-systemd / a bare `./smd` run, where nothing would bring it back up).
- **Notes:** The 202 flushes before the shutdown begins (the handler writes it, then signals a guarded channel). Wired only when the managing unit sets `SM_SELF_RESTART=1` (the bundled `smd.service` does, kept in lockstep with its respawn config) — `cmd/smd` gates `api.Server.SetRestart` on it, so a bare run or a unit that won't respawn returns 503. CSRF: covered by the API-wide same-origin middleware (`requireSameOrigin`, `csrf.go`) that guards every mutating method — a cross-origin drive-by POST is rejected **403** `cross_origin`, while the same-origin SPA, the loopback dev proxy, and non-browser clients (no Origin header) pass; still unauthenticated (single-operator loopback service). SPA control is the Settings "Restart daemon" button.

---

## Rig / bridge (ADR 0013 / 0019 / 0026 / 0027)

### `GET /v1/rig/events`
- **Purpose:** SSE stream of rig CAT state for the logging SPA (served by `bridge.Service.HTTPHandler`).
- **Gating:** **Only when the bridge subsystem is enabled.** Wrapped in `limitEventSubscribers`.
- **Response:** **200** SSE stream. Events:
  - `rig-state` → `RigStatePayload` `{rigIdentity?, vfoA?, vfoB?, mode?, subMode?, selectedVfo?, splitOverride? (*bool), power?}` — omitted fields leave SPA state untouched; first event after connect is a full snapshot.
  - `rig-disconnected` → `{code: "rig_no_data"|"serial_port_error", details?}`.
  - `bridge-error` → `{code: "unknown_driver"|"serial_config_invalid"|"missing_init_command"|"missing_read_command"|"serial_open_failed"|"init_write_failed"|"identity_unrecognised"|"identity_mismatch", details?}`.
  - `tune-state` → `{active: bool}`.
- **Errors:** 503 `server_busy`.
- **Notes:** Hub one-slot replay cache for `bridge-error`/`rig-disconnected`/`tune-state`; disconnect cache cleared on next `rig-state`. Codes carry `{code, details}` for SPA i18n (no human strings on the wire).

### `POST /v1/rig/command`
- **Purpose:** Inbound rig control — freq/mode/VFO/band (ADR 0026).
- **Gating:** **Only when the bridge is enabled.**
- **Request:** Body either single `{"op": string, "value": <scalar>}` **or** atomic batch `{"commands": [{op, value}, …]}` (not both, max 32 commands per batch → `invalid_field_value` 400). `value` is a JSON scalar.
- **Response:** **202 Accepted**. For the `kenwood` family this is "written," not "rig is now at X" — the resulting state is confirmed out-of-band via the `rig-state` SSE push (confirm-by-push). For `icom_civ` (ADR 0034 wait-for-ACK) the daemon waits for the rig's per-command FB/FA ACK before responding: a 202 means the rig **accepted** the command (and the daemon has synthesized the matching `rig-state` push, since CI-V never broadcasts a commanded change); a rejected/timed-out command returns an error below.
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`missing_required_param`/`rig_unsupported_command`/`rig_invalid_value`; 503 `rig_not_connected`; 409 `rig_identity_unverified`; 422 `rig_command_rejected` (CI-V only — rig answered FA/NG, e.g. value out of range); 504 `rig_command_no_ack` (CI-V only — no FB/FA within `bridge.timeouts.civ_ack_ms`); 500 `rig_command_failed`.
- **Notes:** A `kenwood` batch becomes one atomic CAT line; an `icom_civ` batch is sent frame-by-frame, each awaiting its ACK (the bus is half-duplex), so a mid-batch FA leaves earlier ops applied. `tx_on`/`tx_off` are never exposed — TX can't be keyed here.

### `POST /v1/rig/tune`
- **Purpose:** Drive the daemon-owned tune-carrier TX state machine (ADR 0027) — a reduced-power RTTY carrier for amp tuning.
- **Gating:** **Only when the bridge is enabled.**
- **Request:** Body `{"active": bool}` (both directions idempotent).
- **Response:** **202 Accepted** (state confirmed via `tune-state` SSE).
- **Errors:** 400 `invalid_json`; 503 `rig_not_connected`/`rig_state_unknown`; 409 `rig_identity_unverified`; 500 `rig_tune_failed`.
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
  - `ft8-occupancy` → `OccupancyReport` `{slot: {start_utc, period}, passband: Band, dial_mhz?, signal_width_hz, occupied: [Band], suggested: [int]}` (`Band` = `{low_hz, high_hz, source?, level?}`). `dial_mhz` is the rig dial the slot was CAPTURED on. As with `ft8-qso`'s `dial_freq_mhz`, clients must attribute the report to a band from THIS, never from live rig state: publication lags capture by the decode, so a report in flight across a band change would otherwise be labelled with the new band and its `suggested` offsets — measured elsewhere — could steer a transmission. A CAT-attached daemon emits **only** reports it can place: a slot is skipped when the dial moved during its window (sampled every audio batch, so an excursion that returns to the same frequency is still caught) or was unknown at any point in it. A slot whose dial MOVED also has its `ft8-decode` list suppressed — the event still fires, empty, so the slot clock ticks — because every consumer of a decode resolves it against the CURRENT dial: rendering a station heard elsewhere as workable here would key an answer at nothing and spot a wrong frequency to PSK Reporter. A dial move additionally **ends any active `ft8-qso` session** (status pushes `active:false`): an FT8 exchange lives on one dial frequency, and empty decodes are not a sequencer no-op — they read as silence, repeat the rung and key it. `dial_mhz` is therefore absent only when the daemon has no CAT at all — a deployment that cannot transmit (FT8 keying requires a writable rig), so its panel is display-only and a client may fall back to its own view of the band there. Not replay-cached (see `internal/ft8/hub.go`).
  - `ft8-decode` → `DecodeReport` `{slot: SlotRef, decodes: [{text, freq_hz, dt_s, snr}]}`.
  - `ft8-tx` → `TxState` `{armed, transmitting, message?, offset_hz?, error?}`.
  - `ft8-qso` → `QsoStatus` `{active, role?, fd?, type4?, their_call?, state?, next_message?, repeats?, max_repeats?, our_report?, their_report?, dial_freq_mhz?}` `dial_freq_mhz` is the rig dial PINNED to the session at start — the frequency the contact will be logged on. Clients must attribute a contact to a band from THIS, not from live rig state: the rig and FT8 status are independent streams, so a band change mid-contact (or a skew between them) otherwise files the contact under the wrong band. (`fd:true` marks an ARRL Field Day session; `type4:true` marks a reduced type-4 nonstandard/compound-call session — bare-calls→RR73→73, no grid/report rungs, ADR 0048). `max_repeats` is present exactly when the CURRENT rung is bounded, so a countdown shows iff `max_repeats > 0`: it is absent while plain calling CQ (uncapped by design) and on a "send once" closing rung whose partner has already rogered (`internal/ft8/finalrung.go`).
  - `ft8-logged` → `LoggedQso` `{uuid, callsign, freq_hz, band, rst_sent, rst_rcvd, mode, time_on:"HH:MM", qso_date:"YYYY-MM-DD", gridsquare, country}`.
- **Errors:** 503 `server_busy`.
- **Notes:** All events except `ft8-logged` are replay-cached for late subscribers (`ft8-logged` is not — replay would dup a session row). **Demand-driven:** the first subscriber acquires the audio capture device, the last (after a linger) releases it. Live decode needs a CGO build; the static build keeps the subsystem idle (keepalives only).

### `POST /v1/ft8/tx/arm`
- **Purpose:** Arm/disarm the FT8 transmit path — explicit operator gate before any FT8 RF (ADR 0030 e1).
- **Gating:** **Only when FT8 is enabled.**
- **Request:** Body `{"armed": bool}` (idempotent).
- **Response:** **202 Accepted** (state confirmed via `ft8-tx` SSE).
- **Errors:** 400 `invalid_json`/`ft8_tx_bad_message`; 503 `ft8_tx_unavailable`/`rig_not_ready`; 409 `ft8_tx_not_armed`/`ft8_tx_in_flight`; 500 `ft8_tx_failed`.
- **Notes:** Arming acquires the output device + builds the slot controller; disarming aborts any in-flight TX (PTT drops) + releases the device.

### `POST /v1/ft8/tx/send`
- **Purpose:** Queue one standard FT8 message for the next UTC slot at a chosen audio offset (ADR 0030 e1).
- **Gating:** **Only when FT8 is enabled.** Requires TX armed.
- **Request:** Body `{"message": string, "offset_hz": float64}`.
- **Response:** **202 Accepted** (completion/failure via `ft8-tx` SSE).
- **Errors:** 400 `invalid_json`/`ft8_tx_bad_message`/`ft8_no_offset`/`ft8_bad_offset`; 503 `ft8_tx_unavailable`/`rig_not_ready`; 409 `ft8_tx_not_armed`/`ft8_tx_in_flight`; 500 `ft8_tx_failed`. (`offset_hz` is daemon-validated against the usable passband — review 2026-06-19 M1.)

### `POST /v1/ft8/qso/start`
- **Purpose:** Begin a manual answer-a-CQ exchange the daemon auto-advances CQ→73 (ADR 0031 e3).
- **Gating:** **Only when FT8 is enabled.** Requires TX armed.
- **Request:** Body `{"their_call" (req), "their_grid"?, "slot_utc"?, "offset_hz"?, "operating_freq_mhz"?, "mode"?, "their_snr"?}`. Our own callsign/grid are resolved server-side from station config. `mode:"fd"` answers a `CQ FD` with the operator's ARRL Field Day exchange (class/section from `ft8.field_day` config; `ft8_field_day_unset` 400 if unset) — see ft8.md "ARRL Field Day operating". `mode:"type4"` answers a NONSTANDARD/compound-call CQ (`CQ PJ4/NA2AA`, `CQ K1ABC/D`) with the reduced type-4 ladder (bare-calls→RR73→73, no grid/report on the wire — ADR 0048); needs no config identity (our own call is standard). "" / "standard" is the normal grid/report answer. `their_snr` (our SNR of the clicked CQ) is logged as RST_SENT for `mode:"fd"` and `mode:"type4"` (neither exchanges a report; ignored for a standard answer, whose report comes from the exchange).
- **Response:** **202 Accepted** (progress via `ft8-qso` SSE).
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`no_station_callsign`/`ft8_no_offset`/`ft8_bad_offset`/`ft8_no_frequency`; 409 `ft8_tx_not_armed`/`ft8_qso_in_progress`.
- **Notes:** `slot_utc` fixes the worked station's parity. `offset_hz` is daemon-validated against the usable passband (M1) and `operating_freq_mhz` must be a positive known-band dial frequency (M2 — refused before the on-air exchange, since a QSO is logged only at completion). Logged QSO `FREQ`/`BAND` come from `operating_freq_mhz` (the rig dial); `offset_hz` is TX audio placement only, never folded into FREQ.

### `POST /v1/ft8/qso/work`
- **Purpose:** Begin working a station that is calling US, picked from the pile-up (ADR 0033 "work a caller"). Caller-style exchange (we report first → RR73 → log), then **idle** (does not loop to CQ). For tail-enders / answerers that call us when we're not in a Call-CQ session.
- **Gating:** **Only when FT8 is enabled.** Requires TX armed + no session in flight.
- **Request:** Body `{"their_call" (req), "their_grid"?, "their_snr"?, "slot_utc"?, "offset_hz"?, "operating_freq_mhz"?, "mode"?, "their_class"?, "their_section"?}`. `their_snr` is the SPA's SNR of the picked decode — the report we send (RST_SENT). Our own callsign/grid resolved server-side. `mode:"fd"` works a caller who called us with an ARRL Field Day exchange — the SPA parsed `their_class`/`their_section` from `<ourCall> <theirCall> <class> <section>`, and our class/section come from `ft8.field_day` config (`ft8_field_day_unset` 400 if unset). `mode:"type4"` works a NONSTANDARD/compound caller with the reduced type-4 ladder (single RR73 rung, no report — ADR 0048); needs no config identity. "" / "standard" is the normal grid/report work. Both bodies also accept `allow_duplicate` (bool, optional, default false) — the operator's EXPLICIT "work this station again" intent. QSO storage deduplicates on call+band+mode+freq+date+HH**MM**, so a deliberate second contact with the same station inside one minute would otherwise hash to the first's key and be silently discarded: the operator transmits a full exchange and no row appears (reachable on the short ladders — work-a-caller and the single-rung type-4 work path). The flag is pinned to the session AT ARM TIME (like the logbook, ADR 0055), stamped onto the completed QSO, and passed to `qsoservice.Submit` as `force`. The SPA sets it only from an operator action on a station it already shows as worked this session; `/v1/ft8/cq/start` does NOT take it (a Call-CQ run works whoever answers, so there is no per-station intent to express).
- **Response:** **202 Accepted** (progress via `ft8-qso` SSE).
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`no_station_callsign`/`ft8_no_offset`/`ft8_bad_offset`/`ft8_no_frequency`/`ft8_tx_bad_message`; 409 `ft8_tx_not_armed`/`ft8_qso_in_progress`; 503 `rig_not_ready`.
- **Notes:** `slot_utc` fixes the caller's parity. Logged QSO `FREQ`/`BAND` come from `operating_freq_mhz` (the rig dial); `offset_hz` is TX audio placement only, never folded into FREQ.

### `POST /v1/ft8/cq/start`
- **Purpose:** Begin a sequenced Call-CQ session that works answerers (ADR 0033). Answerer-selection mode = daemon config `ft8.tx.caller_answer_mode` (default `auto_first`).
- **Gating:** **Only when FT8 is enabled.** Requires TX armed.
- **Request:** Body `{"offset_hz": float64, "operating_freq_mhz": float64, "tx_parity"?: "even"|"odd"}`. Callsign/grid resolved server-side. `tx_parity` is the operator's chosen CQ slot parity (WSJT-X "Tx even/1st" — `even` = :00/:30, `odd` = :15/:45); **omitted/empty/any-other value = call CQ on the next slot regardless of parity** (the default, fastest first CQ). Operating state, sent per session — not a persisted daemon setting. Caller-side only (answering a CQ forces the opposite parity).
- **Response:** **202 Accepted** (progress via `ft8-qso` SSE).
- **Errors:** 400 `invalid_json`/`no_station_callsign`/`ft8_no_offset`/`ft8_bad_offset`/`ft8_no_frequency`/`invalid_field_value`; 409 `ft8_tx_not_armed`/`ft8_qso_in_progress`/`ft8_caller_mode_unsupported` (501 when `caller_answer_mode=operator_pick`).
- **Notes:** One session at a time. Stopped via the shared abandon route below.

### `POST /v1/ft8/qso/path`
- **Purpose:** Record the operator's antenna-path choice for the active FT8 exchange (the short/long radio in the FT8 enrichment panel). **Logging-only** — it annotates the logged QSO (ADIF `ANT_PATH`, plus the matching great-circle `ANT_AZ` + `DISTANCE`) and never touches the on-air signal. FT8 QSOs are built daemon-side, so unlike Phone/CW (where the SPA stamps it at submit) the choice is sent here.
- **Gating:** **Only when FT8 is enabled.**
- **Request:** Body `{"path": "S"|"short"|"L"|"long"}` (case-insensitive). Lenient — anything other than long is treated as short.
- **Response:** **202 Accepted.**
- **Notes:** Settable any time during a contact; read once when the exchange completes. Resets to short at the start of each exchange, so a prior contact's "long" never carries over. `ANT_AZ`/`DISTANCE` are only stamped when both grids resolve; `ANT_PATH` is stamped regardless.

### `POST /v1/ft8/qso/abandon`
- **Purpose:** Drop any active sequenced session (answer-a-CQ or Call-CQ). **Only when FT8 is enabled.** No body. **202 Accepted**, idempotent (no-op while idle). No error codes.

### `POST /v1/ft8/qso/skip`
- **Purpose:** Arm/disarm **skip-if-silent** on the active sequenced session (the operator's deferred Next, daemon-side): armed, a silent cycle on an already-transmitted rung ends the session **instead of keying the repeat** — no RF at a station the operator has decided to drop. **Only when FT8 is enabled.**
- **Request:** Body `{"armed": bool}`.
- **Response:** **202 Accepted**. The armed state rides the `ft8-qso` SSE as `skip_armed` (confirm-by-push); the skip firing publishes the idle status.
- **Errors:** 400 malformed body; **409 `ft8_no_active_qso`** when arming with nothing skippable (idle, or a Call-CQ run — its Next is an immediate takeover). Disarm is idempotent (accepted even when idle).
- **Notes:** The arm clears itself when the partner replies (they came back), on session start, and on Abandon. Applies to answering + working sessions, standard and FD.

---

## Diagnostics & SPA

### `GET|POST /debug/pprof/*`
- **Purpose:** Go runtime profiling (goroutine/heap/CPU/trace). Development affordance, not a stable contract; lives outside `/v1/*`.
- **Gating:** **Only when `Server.EnableProfiling` is true** (off by default; logs a warning at mount).
- **Routes:** `GET /debug/pprof/`, `/cmdline`, `/profile`, `/symbol` (GET+POST), `/trace`. Standard `net/http/pprof` semantics (`profile?seconds=N` blocks N seconds — a DoS vector, so it stays off by default).
- **Notes:** Registered on this mux (not `http.DefaultServeMux`); method-specific GET registration keeps these patterns clean under Go 1.22 ServeMux. Since the logging SPA's `GET /` catch-all was retired (2026-07-21), there is no root catch-all to conflict with — an unmounted pprof path (profiling off) is a plain 404.

### `GET /{$}`, `GET /app/`, `GET /config/`, `GET /logbook/` (embedded SPAs)
- **Purpose:** Serve the embedded Svelte SPAs — app shell at `/app/` (ADR 0044), config app at `/config/`, logbook app at `/logbook/` — with client-side-routing fallback. `GET /` 302-redirects to `/app/`. (The legacy logging SPA that owned `/` as a catch-all was **retired 2026-07-21** — embed + route removed, source kept for reference.)
- **Gating:** **Only when `Protocol == "tcp" && *ServeSPA`** (browsers need TCP; a headless Unix-socket daemon leaves these unregistered).
- **Routes:** `GET /app/` → `StripPrefix("/app", spaHandler(AppFS()))`, `GET /config/` → `StripPrefix("/config", spaHandler(ConfigFS()))`, `GET /logbook/` → `StripPrefix("/logbook", spaHandler(LogbookFS()))` (subtree patterns; bare `/app`,`/config`,`/logbook` 301→ trailing slash); `GET /{$}` → `RedirectHandler("/app/", 302)`. This is an **exact-match** root, not a catch-all, so any GET matching no route (incl. `/debug/pprof/*` when profiling is off) is a clean **404** rather than an SPA fallthrough.
- **Response:** For a sub-path SPA, **200** with the static asset, or `index.html` when the path doesn't resolve to a file (SPA-router fallback within the subtree, so a refresh on `/app/log` etc. doesn't 404). `Cache-Control: no-cache` on every asset (the entry bundle has a stable, hash-free name). `GET /` → **302** to `/app/`.

### `GET /manual/` (embedded operator manual)
- **Purpose:** Serve the embedded operator manual — a single self-contained, zero-JS Hugo page (ADR 0036). Distinct from the SPAs: plain static files, **no** client-side-router fallback.
- **Gating:** Same as the SPAs — **only when `Protocol == "tcp" && *ServeSPA`**. The on-disk copy (`/usr/share/doc/station-manager/manual/`, shipped in the RPM) covers reading it from `file://` when the daemon isn't serving.
- **Routes:** `GET /manual/` → `StripPrefix("/manual", manualHandler(manual.FS()))` (subtree pattern; bare `/manual` 301→`/manual/`). A subtree pattern, matched independently of the `GET /{$}` exact-root redirect.
- **Response:** **200** with the static page, **404** for any unresolved path (no SPA fallback — it's static). `Cache-Control: no-cache` (the manual is rebuilt with the daemon, so the served copy always matches the running version).

---

## Related

- [api.md](api.md) — the API design brief (rationale, cross-cutting decisions, the decision trail).
- [frontend-spa.md](frontend-spa.md) — SPA embed/serving (the `/` and `/config/` routes).
- [decisions/0036-operator-manual-embedded-zero-js-site.md](decisions/0036-operator-manual-embedded-zero-js-site.md) — the operator manual at `/manual/` (Hugo, zero-JS, embedded + on-disk).
- ADRs: [0010](../decisions/0010-rig-sse-wire-shape.md) (SSE/error shape), [0013](../decisions/0013-bridge-as-daemon-subsystem.md), [0017](../decisions/0017-enrichment-pipeline-domain-table-cache.md), [0026](../decisions/0026-inbound-rig-command-path.md), [0027](../decisions/0027-tune-carrier-tx.md), [0028](../decisions/0028-rig-profiles.md), [0029](../decisions/0029-ft8-transmit.md)/0030/0031/0033 (FT8).
