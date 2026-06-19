# Station Manager — Daemon HTTP API Endpoint Reference

> **Status:** canonical, complete endpoint list (audited 2026-06-14). This is the
> **reference**; the rationale/decision narrative lives in [api.md](api.md) (the API
> *design brief*). When the two disagree, the **handler source is authoritative** —
> routes are registered in `internal/api/server.go`, handlers in
> `internal/api/handler_*.go`. Update this file in the same commit as any route change.

All application endpoints live under the `/v1/*` prefix (API versioning — unrelated to
the project's v1/v2 distinction). The embedded SPAs are served at `/` and `/config/`;
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
unregistered, the path falls through to the SPA catch-all (or 404 on a headless daemon).

---

## QSO

### `POST /v1/qso`
- **Purpose:** Submit exactly one QSO as ADIF — the primary logging write path (logging SPA, `curl`, import tooling). Bulk import is a separate tool (`cmd/importer`), not this endpoint.
- **Gating:** Always-on; additionally wrapped in `limitSubmitRate`.
- **Request:** Content-Type `application/x-adif` / `text/plain` / empty (other → 415 `unsupported_media_type`). Body = a single-record ADIF doc, must contain `STATION_CALLSIGN`. Query: `logbook` (int, **required**, ≥1; must exist and its callsign must match `STATION_CALLSIGN`), `force` (bool, optional — bypass dedupe).
- **Response:** **201** on store / **200** on duplicate. Body `{"status": "stored"|"duplicate", "uuid": string, "id": int64}`.
- **Errors:** 400 `invalid_adif`, `too_many_records`, `missing_required_field`, `invalid_field_value`, `missing_required_param`, `invalid_id`, `callsign_mismatch`, `invalid_query_param`, `invalid_time_range`; 404 `logbook_not_found`; 429 `rate_limited`; 503 `server_busy`; 500 `submit_failed`/`db_error`.
- **Notes:** One-fails-all-fail atomic write (QSO row + upload-queue rows in one tx). Idempotent dedupe — a known duplicate returns 200 with the existing UUID/ID.

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
- **Purpose:** Delete a logbook (refuses if it holds QSOs). **Always-on.** **204**. Errors: 400 `invalid_id`; 404 `not_found`; 409 `has_qsos`; 500 `db_error`.

### `GET /v1/logbook/{id}/qso`
- **Purpose:** Cursor-paginated QSO list for a logbook (logbook-browse views).
- **Gating:** Always-on.
- **Request:** Path `{id}`. Query: `limit` (int, default `Server.DefaultPageLimit`, clamped to `MaxPageLimit`, ≥1), `after` (opaque base64url cursor over `{qso_date, time_on, id}`).
- **Response:** **200**, body `{"items": types.QsoSlice, "next_cursor": string|null}` (`next_cursor` set only when more rows exist).
- **Errors:** 400 `invalid_id`/`invalid_limit`/`invalid_cursor`; 404 `logbook_not_found`; 500 `db_error`.

### `GET /v1/logbook/{id}/count`
- **Purpose:** QSO count for a logbook. **Always-on.** **200** `{"logbook_id": int64, "count": int64}`. Errors: 400 `invalid_id`; 404 `logbook_not_found`; 500 `db_error`.

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
- **Notes:** Daemon rebuilds ADIF from live DB rows (not the client blob), archives it under `<workingDir>/exports/sent-adif/` (best-effort), then stamps `sm_fwrd_by_email_*` on the rows. Unknown UUIDs are skipped with a warning.

---

## Config & hardware

### `GET /v1/config`
- **Purpose:** Operator-relevant config + joined display details (config / My Station SPA).
- **Gating:** Always-on.
- **Response:** **200**, body = `ConfigResponse`: `setup_complete` (bool), `logging_station` (`types.LoggingStation`), `default_logbook` (`types.Logbook`, DB-joined), `default_rig` (`types.RigConfig`, active rig), `station` (`types.StationConfig`), `bridge` (`BridgeInfo`: `enabled`, `driver?`, `rig_name?`, `rig_modes?`, `ops?`, `tune?`, `mode_mappings?` — merged rigdef-defaults + overrides for the active driver — and `ft8_mode?`, the rig CAT mode literal for FT8 (rigdef default + per-rig override) the FT8 band buttons assert), `mailer` (`MailerInfo`: `enabled`, `default_recipient?`), `qsl` (`types.QslDefaults`: standing outgoing-QSL defaults `qsl_via` / `qslmsg` / `qsl_sent_via`), `ft8_display` (resolved `types.Ft8DisplayConfig`), `ft8_frequencies` (resolved `map[string]int`).
- **Errors:** 500 `db_error`.
- **Notes:** Mailer/Bridge are read-only projections — SMTP creds and the serial port are never on the wire.

### `PUT /v1/config`
- **Purpose:** Persist the operator-writable config subset (My Station / Settings save).
- **Gating:** Always-on.
- **Request:** Body = `ConfigResponse` shape. Writable: `logging_station`, `station`, `qsl` (presence-aware — omit to leave untouched), `ft8_display` (presence-aware — omit to leave untouched), `bridge.mode_mappings` (diffed against rigdef defaults; only deviations stored, on the active rig). `setup_complete`, `mailer`, `ft8_frequencies` are server-managed/ignored.
- **Response:** **200**, body = freshly-built `ConfigResponse`.
- **Errors:** 400 `invalid_json`/`invalid_field_value`, plus the config validator's stable `{code, message}` findings (config.md §12 / ADR 0010); 500 `config_write_error`/`db_error`.
- **Notes:** Candidate runs `config.Normalize` + `config.Validate` (same pipeline as Load). The first PUT with a non-empty callsign completes setup (seeds default logbook, flips `setup_complete`). Rewrites the whole `config.json` from in-memory state — **don't hand-edit config.json while the daemon runs.**

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
- **Notes:** Read-only. The write path (save a profile / set the default rig) lands with the editor; until then rigs are edited in `config.json` (stop → edit → restart).

---

## Operational

### `GET /v1/healthz`
- **Purpose:** Liveness/readiness (DB reachability). **Always-on.** **200** `{"status": "ok"}`; **503** `db_unavailable` if the DB ping fails.

### `GET /v1/version`
- **Purpose:** Daemon build / Go runtime / DB schema version. **Always-on.** **200** `{"daemon": string, "go": string, "schema": {"version": uint64, "dirty": bool}?}` (always 200; `schema` omitted + warn-logged if the schema query fails).

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
- **Request:** Body either single `{"op": string, "value": <scalar>}` **or** atomic batch `{"commands": [{op, value}, …]}` (not both). `value` is a JSON scalar.
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

---

## FT8 (ADR 0024 / 0029 / 0030 / 0031 / 0033)

### `GET /v1/ft8/events`
- **Purpose:** FT8 subsystem SSE stream (served by `ft8.Service.HTTPHandler`).
- **Gating:** **Only when FT8 is enabled.** Wrapped in `limitEventSubscribers`.
- **Response:** **200** SSE stream. Events:
  - `ft8-occupancy` → `OccupancyReport` `{slot: {start_utc, period}, passband: Band, signal_width_hz, occupied: [Band], suggested: [int]}` (`Band` = `{low_hz, high_hz, source?, level?}`).
  - `ft8-decode` → `DecodeReport` `{slot: SlotRef, decodes: [{text, freq_hz, dt_s, snr}]}`.
  - `ft8-tx` → `TxState` `{armed, transmitting, message?, offset_hz?, error?}`.
  - `ft8-qso` → `QsoStatus` `{active, role?, their_call?, state?, next_message?, repeats?, our_report?, their_report?}`.
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
- **Request:** Body `{"their_call" (req), "their_grid"?, "slot_utc"?, "offset_hz"?, "operating_freq_mhz"?}`. Our own callsign/grid are resolved server-side from station config.
- **Response:** **202 Accepted** (progress via `ft8-qso` SSE).
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`no_station_callsign`/`ft8_no_offset`/`ft8_bad_offset`/`ft8_no_frequency`; 409 `ft8_tx_not_armed`/`ft8_qso_in_progress`.
- **Notes:** `slot_utc` fixes the worked station's parity. `offset_hz` is daemon-validated against the usable passband (M1) and `operating_freq_mhz` must be a positive known-band dial frequency (M2 — refused before the on-air exchange, since a QSO is logged only at completion). Logged QSO `FREQ`/`BAND` come from `operating_freq_mhz` (the rig dial); `offset_hz` is TX audio placement only, never folded into FREQ.

### `POST /v1/ft8/qso/work`
- **Purpose:** Begin working a station that is calling US, picked from the pile-up (ADR 0033 "work a caller"). Caller-style exchange (we report first → RR73 → log), then **idle** (does not loop to CQ). For tail-enders / answerers that call us when we're not in a Call-CQ session.
- **Gating:** **Only when FT8 is enabled.** Requires TX armed + no session in flight.
- **Request:** Body `{"their_call" (req), "their_grid"?, "their_snr"?, "slot_utc"?, "offset_hz"?, "operating_freq_mhz"?}`. `their_snr` is the SPA's SNR of the picked decode — the report we send (RST_SENT). Our own callsign/grid resolved server-side.
- **Response:** **202 Accepted** (progress via `ft8-qso` SSE).
- **Errors:** 400 `invalid_json`/`invalid_field_value`/`no_station_callsign`/`ft8_no_offset`/`ft8_bad_offset`/`ft8_no_frequency`/`ft8_tx_bad_message`; 409 `ft8_tx_not_armed`/`ft8_qso_in_progress`; 503 `rig_not_ready`.
- **Notes:** `slot_utc` fixes the caller's parity. Logged QSO `FREQ`/`BAND` come from `operating_freq_mhz` (the rig dial); `offset_hz` is TX audio placement only, never folded into FREQ.

### `POST /v1/ft8/cq/start`
- **Purpose:** Begin a sequenced Call-CQ session that works answerers (ADR 0033). Answerer-selection mode = daemon config `ft8.tx.caller_answer_mode` (default `auto_first`).
- **Gating:** **Only when FT8 is enabled.** Requires TX armed.
- **Request:** Body `{"offset_hz": float64, "operating_freq_mhz": float64}`. Callsign/grid resolved server-side.
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

---

## Diagnostics & SPA

### `GET|POST /debug/pprof/*`
- **Purpose:** Go runtime profiling (goroutine/heap/CPU/trace). Development affordance, not a stable contract; lives outside `/v1/*`.
- **Gating:** **Only when `Server.EnableProfiling` is true** (off by default; logs a warning at mount).
- **Routes:** `GET /debug/pprof/`, `/cmdline`, `/profile`, `/symbol` (GET+POST), `/trace`. Standard `net/http/pprof` semantics (`profile?seconds=N` blocks N seconds — a DoS vector, so it stays off by default).
- **Notes:** Registered on this mux (not `http.DefaultServeMux`); method-specific GET registration avoids a ServeMux conflict with the `GET /` SPA catch-all.

### `GET /` and `GET /config/` (embedded SPAs)
- **Purpose:** Serve the embedded Svelte SPAs — logging app at `/`, config app at `/config/` — with client-side-routing fallback.
- **Gating:** **Only when `Protocol == "tcp" && *ServeSPA`** (browsers need TCP; a headless Unix-socket daemon leaves these unregistered).
- **Routes:** `GET /config/` → `StripPrefix("/config", spaHandler(ConfigFS()))` (subtree pattern; bare `/config` 301→`/config/`); `GET /` → `spaHandler(LoggingFS())` (catch-all). Go 1.22 ServeMux gives `/v1/*`, `/config/`, and pprof patterns priority, so `/` is naturally bounded to unmatched paths.
- **Response:** **200** with the static asset, or `index.html` when the path doesn't resolve to a file (SPA-router fallback, so a refresh on `/log` etc. doesn't 404). `Cache-Control: no-cache` on every asset (the entry bundle has a stable, hash-free name).

---

## Related

- [api.md](api.md) — the API design brief (rationale, cross-cutting decisions, the decision trail).
- [frontend-spa.md](frontend-spa.md) — SPA embed/serving (the `/` and `/config/` routes).
- ADRs: [0010](../decisions/0010-rig-sse-wire-shape.md) (SSE/error shape), [0013](../decisions/0013-bridge-as-daemon-subsystem.md), [0017](../decisions/0017-enrichment-pipeline-domain-table-cache.md), [0026](../decisions/0026-inbound-rig-command-path.md), [0027](../decisions/0027-tune-carrier-tx.md), [0028](../decisions/0028-rig-profiles.md), [0029](../decisions/0029-ft8-transmit.md)/0030/0031/0033 (FT8).
