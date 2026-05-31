# Station Manager v2 — Milestones

**Status:** Third entry in `docs/v2-design/`, written 2026-04-16 during
session 7. Defines what "done" looks like for each milestone so that
construction has a clear finish line.

**Why this document exists:** The v1 analysis identified "interminable
90%" as a real risk — the project feels nearly done but the remaining
work keeps expanding because "done" was never defined. This document
is the explicit mitigation. Each milestone has a concrete acceptance
test: a thing you can do at a terminal that either works or doesn't.

**This is a plan, not a contract.** Milestones can be re-scoped as
construction reveals reality. Changes are recorded here with dates and
rationale, not silently drifted away from.

**How this document relates to others:**

- `docs/v2-design/structure.md` — repo layout and module decisions.
  Milestone boundaries align with the target layouts defined there.
- `docs/v2-design/api.md` — the HTTP API design brief. The endpoint
  sketch in Section 5 maps to milestones here.
- `docs/v1-analysis/invariants.md` — load-bearing rules that constrain
  every milestone's scope.
- `docs/session-handoff.md` — rolling session state and deferred
  features that land in later milestones.

---

## Milestone 1 — The daemon serves its first QSO

**Status: ✅ SHIPPED (audited 2026-05-02).** `cmd/smd` runs, accepts
ADIF over `POST /v1/qso`, writes atomic QSO + upload-queue rows,
returns the `{status,id}` envelope. Health endpoint, error envelope,
graceful shutdown, race-detector CI all in place. Acceptance test
below remains the spec; the running daemon satisfies it.

**Goal:** A running `smd` daemon that accepts a QSO over its Unix
socket, stores it atomically in sqlite, and responds with a JSON
envelope. Exercised entirely via `curl`. No GUI, no forwarding, no
SSE.

### Scope

- `cmd/smd/main.go` wires up the iocdi container with config,
  logging, and sqlite services, binds a Unix domain socket, and
  starts an HTTP server.
- `POST /v1/qso` accepts a raw ADIF body, parses it via
  `internal/adif`, calls `qsoservice.Submit()`, which writes the QSO
  row and its upload-queue row(s) in a single sqlite transaction (per
  the one-fails-all-fail invariant), and returns a JSON envelope
  indicating `"stored"` or `"duplicate"` (per the dedupe key design
  in `api.md` Section 4.2).
- **Error paths are in scope.** The submit endpoint must handle and
  return clear error responses (per the envelope in `api.md`
  Section 4.6) for at least these cases:
  - Empty or unparseable request body (malformed ADIF)
  - Missing required fields (no `CALL`, no `BAND`, no `MODE`,
    no `QSO_DATE`, no `TIME_ON`, no `STATION_CALLSIGN`)
  - Invalid field values (unrecognised band, unrecognised mode,
    malformed date/time)
  - Wrong Content-Type header
  - Request body exceeds configured size limit
  - Database write failure (sqlite unavailable, disk full, etc.)
  Each error case returns an appropriate HTTP status (400 for client
  errors, 500 for server errors) with the JSON error envelope.
- `GET /v1/healthz` returns 200 if the daemon is running and sqlite
  is reachable.
- Config is loaded from a JSON file or defaults. Socket path,
  sqlite path, logging config, and shutdown timeout are all
  config-driven.
- Graceful shutdown on SIGINT/SIGTERM: drain in-flight requests,
  close sqlite, close the logger.
- **GitHub Actions CI workflow.** A `.github/workflows/ci.yml` that
  runs on every push and PR to `main`:
  - `go vet ./...`
  - `go build ./...`
  - `go test -race ./...`
  - The v1 workflow was deleted in session 2 because it was failing
    on the data race. The v2 workflow starts clean with the race
    detector enabled from day one.

### Explicitly out of scope

- QSO retrieval, editing, deletion (`GET/PATCH/DELETE /v1/qso/:id`)
- Logbook management endpoints
- Contact history, contest dupe check
- SSE event stream
- Forwarding worker
- ADIF export
- Any GUI / Wails app / SPA
- Multi-rig awareness
- Network/LAN deployment

### Acceptance test

```
# Start the daemon (no --config: first run seeds ./config.json with
# DefaultConfig(cwd); subsequent runs load that file. Pass --config
# explicitly to point at a non-default path.)
./smd

# ---- Happy path ----

# Submit a valid QSO
curl --unix-socket /tmp/smd.sock \
  -X POST \
  -H "Content-Type: application/x-adif" \
  --data-binary @sample.adi \
  http://localhost/v1/qso
# Expected: 201, {"status":"stored","id":1}

# Submit the same QSO again (dedupe)
curl --unix-socket /tmp/smd.sock \
  -X POST \
  -H "Content-Type: application/x-adif" \
  --data-binary @sample.adi \
  http://localhost/v1/qso
# Expected: 200, {"status":"duplicate","id":1}

# Health check
curl --unix-socket /tmp/smd.sock http://localhost/v1/healthz
# Expected: 200 OK

# ---- Error paths ----

# Empty body
curl --unix-socket /tmp/smd.sock \
  -X POST \
  -H "Content-Type: application/x-adif" \
  http://localhost/v1/qso
# Expected: 400, {"code":"invalid_adif","message":"...","op":"..."}

# Malformed ADIF (not parseable)
echo "this is not adif" | curl --unix-socket /tmp/smd.sock \
  -X POST \
  -H "Content-Type: application/x-adif" \
  --data-binary @- \
  http://localhost/v1/qso
# Expected: 400, {"code":"invalid_adif",...}

# Missing required field (e.g. no CALL tag)
echo "<BAND:3>40m<MODE:3>SSB<QSO_DATE:8>20250508<TIME_ON:4>0845<STATION_CALLSIGN:5>G4ABC<EOR>" | \
  curl --unix-socket /tmp/smd.sock \
  -X POST \
  -H "Content-Type: application/x-adif" \
  --data-binary @- \
  http://localhost/v1/qso
# Expected: 400, {"code":"missing_required_field",...}

# Invalid field value (bad band)
echo "<CALL:5>M0CMC<BAND:5>99.9m<MODE:3>SSB<QSO_DATE:8>20250508<TIME_ON:4>0845<STATION_CALLSIGN:5>G4ABC<EOR>" | \
  curl --unix-socket /tmp/smd.sock \
  -X POST \
  -H "Content-Type: application/x-adif" \
  --data-binary @- \
  http://localhost/v1/qso
# Expected: 400, {"code":"invalid_field_value",...}

# Wrong Content-Type
curl --unix-socket /tmp/smd.sock \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"call":"M0CMC"}' \
  http://localhost/v1/qso
# Expected: 415 Unsupported Media Type

# Unknown route
curl --unix-socket /tmp/smd.sock http://localhost/v1/nonexistent
# Expected: 404
```

When **both** the happy path and the error paths work as expected,
milestone 1 is done.

### Estimated packages touched

- `cmd/smd` — daemon entry point (new code)
- `internal/config` — fill in the file loader
- `internal/api` — HTTP handler layer (new code)
- `internal/qsoservice` — domain service with `Submit()` (new code)
- `internal/database/sqlite` — carry-forward, possibly modified
- `internal/adif` — carry-forward, consumed as-is
- `internal/iocdi` — carry-forward, consumed as-is
- `internal/logging` — carry-forward, consumed as-is
- `internal/errors` — carry-forward, consumed as-is
- `internal/types` — carry-forward, consumed as-is

---

## Milestone 1b — QSO CRUD and logbook management

**Status: ✅ SHIPPED (audited 2026-05-02).** All eleven endpoints
listed in scope are wired in `internal/api/server.go` and
exercised by handler tests (`handler_e2e_test.go`,
`handler_qso_list_test.go`, etc.). Concrete shapes are
documented in `api.md` §7a.

**Goal:** The daemon serves the full read/write API surface needed by
the logging and logbook clients. Still exercised via `curl`; still no
GUI.

### Scope (additive on top of milestone 1)

- `GET /v1/qso/:uuid` — fetch a single QSO (UUIDv7-keyed per ADR 0016)
- `PATCH /v1/qso/:uuid` — edit a QSO
- `DELETE /v1/qso/:uuid` — soft-delete a QSO
- `GET /v1/logbook` — list all logbooks
- `GET /v1/logbook/:id` — fetch a single logbook
- `POST /v1/logbook` — create a logbook
- `PATCH /v1/logbook/:id` — edit logbook metadata
- `DELETE /v1/logbook/:id` — soft-delete a logbook
- `GET /v1/logbook/:id/qso` — list QSOs with forward-cursor pagination
- `GET /v1/contact-history?call=<callsign>` — contact history
- `GET /v1/contest-dupe?logbook=<id>&call=<callsign>&band=<band>&mode=<mode>`
- `GET /v1/version` — daemon version info

### Acceptance test

A `curl`-based script that exercises every endpoint: create a logbook,
submit multiple QSOs, fetch them individually, list them with
pagination, edit one, soft-delete one, check contact history, run a
contest dupe check, and verify the version endpoint.

---

## Milestone 1c — Forwarding and SSE

**Goal:** The daemon forwards QSOs to external services asynchronously
and publishes events to connected clients via SSE.

**Design doc:** `docs/v2-design/forwarding.md` (draft 2026-04-18)
settles the internal shape — fan-out config, `Forwarder` interface,
per-destination worker topology, retry policy, queue-row lifecycle,
`SafeGo` panic recovery, and the v1 → v2 migration path. Read that
before implementing.

**Status: ✅ SHIPPED (audited 2026-05-02).** The two pieces left open
at the session-11 cut have both landed. `internal/forwarding/qrz/`
exists and is wired in `cmd/smd/main.go` (sets `qrz.UserAgent` from
the build version). `GET /v1/events` is mounted in
`internal/api/server.go` and backed by `internal/events/hub.go`
with the §4.5 vocabulary, slow-reader disconnect, and
`Server.MaxEventSubscribers` cap. ADIF export remains dropped per
session 10.

**Earlier status (end of session 11, 2026-04-18):** Forwarder thin slice is
complete — stages 1–11 in `docs/session-handoff.md` delivered the
spine, from config + queue-row schema through the per-destination
worker, pull endpoint, and end-to-end regression test. Two pieces
from the original scope were open at that point: a real QRZ forwarder
port (the stub proves the plumbing, but `internal/forwarding/qrz/`
hadn't been written) and the SSE event stream (`GET /v1/events`).
The third originally-listed piece, ADIF export, was dropped in
session 10 (ADIF is client-side per the session-scope memory;
backup = forwarding to online services, not a daemon endpoint).
See `docs/v2-design/forwarding.md §12` for the per-acceptance-
criterion breakdown.

### Scope (additive on top of milestone 1b)

- ✅ Background forwarding worker: picks up pending `qso_upload`
  rows, attempts each configured destination, writes status back,
  retries with backoff on transient failures.
- ✅ QRZ forwarder (carried forward from v1, adapted to the new
  worker shape). Additional destinations (ClubLog, LoTW, eQSL) as
  separate forwarder implementations behind the fan-out config —
  ClubLog/LoTW/eQSL still TBD as additional `internal/forwarding/*`
  packages, but the registry pattern that QRZ + stub use is the
  template.
- ✅ `GET /v1/qso/:uuid/uploads` — per-destination forwarding status (UUIDv7-keyed per ADR 0016 phase 2).
- ✅ `GET /v1/events` — SSE event stream with `qso.stored`,
  `qso.updated`, `qso.deleted`, `forward.succeeded`, `forward.failed`.
- ❌ `POST /v1/logbook/:id/export` — ADIF export.
  **Dropped** (session 10) per session-scope memory: ADIF is a
  client/admin concern, not a daemon concern. Backup story is
  forwarding to online services.
- ✅ Forwarder configuration in config file (enable/disable per
  destination, credentials, action filter).

### Acceptance test

Submit a QSO, observe it forwarded to a test endpoint (or QRZ
sandbox if available), verify the forwarding status via
`GET /v1/qso/:uuid/uploads`, and verify SSE events arrive on a
connected `curl` event stream.

---

## Milestone 2 — Browser SPA clients

**Status: 🚧 IN PROGRESS — close to done (as of 2026-05-14).** Original
scope was "Wails thin clients"; per ADR 0001 (2026-04-30) the client
apps are now browser SPAs (Svelte 5 + Vite + Tailwind v4) embedded in
the daemon binary via `//go:embed` and served by the daemon at
`GET /` when `Protocol=tcp && ServeSPA=true`. The original Wails-era
scope is preserved at the bottom of this section as the historical
record.

The logging SPA (`frontend/logging/`) scaffold landed 2026-04-30 and
QSO-entry UX shipped through session 28 (2026-05-02): callsign /
mode / RST / VFOs / date-time / name / qth / comment / submit
controls all wired, ADIF formatter complete, tab-order indexed,
session timer + QSO timer running, station-profile store populated.
Session 30 (2026-05-03) wired submit to `POST /v1/qso` end-to-end
via `lib/api/qso.ts`, built the toast system per ADR 0008, added
the daemon HTTP access log, and logged the first real QSO through
the v2 stack (7Q5MLV @ 14.250 MHz USB).

**What's left in M2 (as of 2026-05-29):**

- Two deferred keyboard shortcuts: Ctrl+\ VFO swap, `?` help
  overlay. F2 lookup-only shipped 2026-05-15. Other ADR 0007
  shortcuts (ESC, Ctrl/Cmd+Enter, Tab→enrichment, Enter/Space on
  activatable elements) are shipped — see
  `docs/keyboard-shortcuts.md` for the live inventory.
- Logbook + config SPAs (deferred — not blocking on M2 closeout).

The CAT-handover toast item that was previously listed here as
deferred actually shipped 2026-05-16 (Session 66, same day as the
bridge supervisor in ADR 0020). Verified live during the 2026-05-29
dogfood session: rig power-off produces the warn `bridge.disconnected.rig_no_data`
"The rig has gone quiet — is it powered on?" after the 800ms
flash-suppression window; rig power-on dismisses the warn and
pushes the positive `bridge.reconnected` info "Rig reconnected.".
See § Scope below for the full machinery summary.

Everything else listed under "Scope" below is shipped; the code-
review tail (sessions 53–60) cleaned up the last architectural
debt on the logging SPA. Natural next step is **live operator
dogfooding on the FTdx10** — full stack works in isolation, just
hasn't been run through a real-rig session since M3a closed.

### Scope (revised per ADR 0001)

- ✅ `frontend/logging/` — Svelte 5 SPA scaffold, embed wiring,
  build pipeline.
- ✅ Daemon TCP listener + SPA-hosting (`GET /` catch-all,
  `Server.ServeSPA` flag).
- ✅ QSO-entry UX (callsign / mode / RST / VFOs / dates / submit).
- ✅ SPA → daemon `POST /v1/qso` wiring (`lib/api/qso.ts`
  discriminated-union outcome type; `QsoPanel.submitQso()`
  branches on outcome). Landed session 30, 2026-05-03.
- ✅ Toast system per ADR 0008 — `lib/states/toasts.svelte.ts` +
  `<Toasts/>` mounted at `app.svelte`; severity prefix; per-level
  TTL; click-to-dismiss; max-stack=5. Originally top-right (session
  30, 2026-05-03); placement moved to top-centre session 36
  (2026-05-05) because top-right was easy to miss when operator
  focus sits on the QSO form below. Per-event-type info-toast
  preferences (`qsoDefaults.notifyQsoStored`,
  `qsoDefaults.notifyConfigSaved`; both default true; toggles in
  the QSO sub-tab) added session 36; errors / duplicates /
  validation / server / network outcomes always toast regardless.
- ✅ Daemon HTTP access log (`logRequests` middleware; 4xx/5xx
  carry `code` / `error` / `op` envelope fields; timestamps
  enabled). Landed session 30, 2026-05-03.
- ✅ First-run `config.json` write. Daemon seeds a default file at
  the resolved candidate path on first launch (atomic temp+rename
  via `config.WriteJSON`); subsequent runs load it. Defaults
  flipped to a "fresh install runs without operator-side edits"
  shape: `protocol=tcp`, `socket_path=127.0.0.1:8080` (matches Vite
  dev proxy), `serve_spa=true`, file logging with timestamps,
  `db/` and `log/` subdirs match the `build/{db,log}/.gitkeep`
  convention. Landed session 31, 2026-05-03.
- ✅ Daemon `GET/PUT /v1/config` — operator-config API. Wire shape
  embeds `types.X` for every nested object (`types.LoggingStation`,
  `types.Logbook`, `types.RigConfig`); no parallel structs.
  Source-of-truth split: scalar IDs (`default_logbook_id`,
  `default_rig_id`) in config.json; display fields (`name`,
  `callsign`) joined from DB / `cfg.Rigs[]` at GET time. PUT is
  mutex-guarded (`cfgSvc.Update()`) and atomic on disk; setup
  transition (`setup_complete` false→true) seeds a default
  logbook row at id=1 using the operator's just-set callsign,
  folding what was previously a separate "seed default logbook"
  step. `setup_complete` is server-managed — clients can't set it
  directly. Landed session 31, 2026-05-03.
- ✅ SPA first-run setup form. `lib/api/config.ts` typed clients
  (`fetchConfig`, `putConfig`); `configState` extended with
  `loaded`, `setupComplete`, `loggingStation`, `defaultLogbook`,
  `defaultRig` views + `applyResponse(...)`; `app.svelte` boot
  gate renders the setup card while `setup_complete=false` and
  the QSO panel after. Failure paths (network/server) toast and
  flip `loaded=true` so the UI doesn't hang. Landed session 31,
  2026-05-03.
- ✅ ADIF identity fallback chain — `configState.loggingStation`
  carries `stationCallsign`, `operator`, `ownerCallsign` as plain
  fields (no `$state`; nothing reactively derives). Daemon
  materialises `operator` / `owner_callsign` from `station_callsign`
  on the first-setup transition only when the request leaves them
  empty (one-shot — post-setup edits are authoritative).
  `applyResponse` applies the same fallback at hydration as a
  no-op safety net. `MyStationPanel.svelte` renders all three via
  `ValidatedInput` + `isValidCallsign`. Tests cover seed, club-
  station preserve, and post-setup-blank cases. Landed session 32,
  2026-05-04.
- ✅ `qsoDraft` state-module lift — QSO draft fields moved out of
  `QsoPanel.svelte` into `lib/states/qsoDraft.svelte.ts`. Singleton
  `QsoDraft` class with form-bound `$state` fields, `$state`
  `qsoStarted` + `lookupCallsign` lifecycle flags (both feed
  reactive consumers — see `frontend-spa.md`'s QSO-draft section),
  `$derived` `defaultRst` / `canSubmit`, and methods `clear()` /
  `startQso()` / `stopQso()` / `tick()`. Reactivity audit locks
  the rule for future enrichment fields: form-bound (`bind:value`)
  or read by `$derived` / `$effect` / templates ⇒ `$state`;
  submit-only data without reactive consumers ⇒ plain class
  field. RST
  default-fill clarified: always-overwrite on CW ↔ voice mode
  transition (the earlier "operator-typed sticks" rule was
  reversed because `'59'` is meaningless on CW), plus
  manual-clear refill. Ticker rate dropped 60s → 1s so `timeOff`
  catches the minute boundary within ~1s instead of lagging up
  to 60s; writes still gated on HH:MM string change so cost is
  one no-op compare per tick. Mode stays panel-local (CAT-state
  concern, not a draft field). `submitQso` now sources
  `STATION_CALLSIGN` from `configState.loggingStation.stationCallsign`
  rather than the legacy `station` store — closes a daemon-side
  validator mismatch where the My Station card's writes were not
  reaching the wire. Landed session 33, 2026-05-05.
- ✅ `types.RigConfig.ID`: `string` → `int64`. Closes
  cat-serial-reuse.md §7.5; uniform with `Logbook.ID int64`;
  numeric defaults (1) work cleanly. Zero blast radius (no
  consumers in code yet). Landed session 31, 2026-05-03.
- ✅ Daemon `GET /v1/enrich/callsign` — enrichment endpoint
  per ADR 0017 (supersedes ADR 0005). Daemon-side fully shipped
  2026-05-07: schema migration, provider abstraction, hamnut + QRZ
  ports from v1, orchestrator with three-state read policy +
  always-merge filter→hamnut, bounded async-refresh worker,
  operator config schema, HTTP handler, contacted_station upsert
  in QSO submit. Unlocks F2 lookup-only path. SPA-side wiring
  shipped 2026-05-08, session 44: `lib/api/enrichment.ts` thin fetch
  wrapper, `QsoPanel.handleEnrich` populates `qsoDraft.name`/`qsoDraft.qth`
  on success, "not found" warn-toast when `station_source === 'none'`.
  Country-panel UI shipped 2026-05-08, session 45: daemon-side
  `country.is_new_entity` populated from a QSO-log Exists query;
  SPA `lib/states/enrichment.svelte.ts` holds the latest result +
  short/long path selection; `CountryPanel.svelte` displays country
  name (with `*` for new DXCC), bundled `flag-icons` SVG flag,
  short/long path distance/bearing pair (active path in
  `text-indigo-700`), short/long radio, and local time + offset.
  Path selection drives ADIF `ANT_AZ` on submit; sticky
  "Looking up..." info-toast for slow lookups.
- ✅ Migrate `lib/stores/station.ts` MY_* fields into
  `configState.loggingStation` and ship the full ADIF MY_* set
  end-to-end. `internal/utils/maidenhead.go` derives `MyLat`/`MyLon`
  from `MyGridsquare` (4/6/8-char locator, ADIF "XDDD MM.MMM"
  output) on every PUT `/v1/config`; daemon validates the grid
  format and clears coordinates when blanked. Frontend extended
  `configState.loggingStation` and `LoggingStationFields` with
  the operator-typed bucket (`MyAntenna`, `MyCity`, `MyCountry`,
  `MyGridsquare`, `MyName`, `MyPostalCode`, `MyRig`, `MyStreet`,
  `MyAltitude`, `MyMorseKeyType`, `MyMorseKeyInfo`, plus
  `MyCqZone`/`MyITUZone`/`MyDxcc` operator-typed for v1 — zone
  derivation needs a polygon dataset SM doesn't bundle); plus
  daemon-derived `MyLat`/`MyLon`. `MyStationPanel.svelte`
  surfaces the panel as **five sub-tabs** (identity / location /
  equipment / CW / qso) — same `tab-item`/`tab-button` pattern as
  the parent `InfoPanel` strip but no icons + smaller font for
  visual nesting; `activeSection` is panel-local `$state` mirrored
  to `sessionStorage` (`sm.myStation.activeSection`). The QSO
  sub-tab (added late session 36) carries QSO-emission preferences:
  a `QSO_RANDOM` tri-state (`'Y'`/`'N'`/`'off'`, default `'off'`
  omits the field; backed by `qsoDefaults.qsoRandom` in
  `lib/states/qsoDefaults.svelte.ts`, localStorage-persisted), and
  the linear-amp pair (`ampEnabled` checkbox + `ampMultiplier`
  numeric input; daemon-persisted via the new `types.StationConfig`
  round-tripped through a `station` block on `/v1/config`), and
  notification toggles (`notifyQsoStored`, `notifyConfigSaved`;
  localStorage-persisted; errors/duplicates always toast regardless).
  The Equipment sub-tab gained a "Default TX power (W)" numeric
  input wired to `types.StationConfig.DefaultPower` (validated
  0-2000W; 0 = TX_PWR omitted from ADIF) — replaces the old hard-
  coded `DEFAULT_POWER_WATTS = 100` constant on `manualState.power`
  which had no UI; `displayedState.rawPower` now reads
  `configState.station.defaultPower` in the CAT-off branch.
  `displayedState.effectivePower` is now
  `ampEnabled ? rawPower * ampMultiplier : rawPower` so unchecked-
  amp logs raw rig power. An "Update" button at the bottom-right
  (outside the tab bodies so it persists across section switches)
  PUTs both the full `logging_station` and `station` blocks and
  re-applies the daemon's response so canonical normalisations
  (callsign upper-case, gridsquare mixed case) and derived fields
  (`MyLat`/`MyLon`) flow back into the UI; info-toast on success,
  error-toast for validation/server/network outcomes. Free-text
  inputs use the shared `lib/validators/passthrough.ts`. `MY_SIG`
  / `MY_SIG_INFO` deferred (special-events use case, not in scope
  until requested). `formatAdifRecord` emits all MY_* tags +
  `OPERATOR`, `OWNER_CALLSIGN`, `ANT_AZ` with stable order;
  spec test pins the wire shape. `QsoPanel.submitQso` sources
  every identity field from `configState`; `lib/stores/station.ts`
  and the now-empty `lib/stores/` directory deleted. Bearing
  utility (`lib/utils/bearing.ts`) ported from v1's
  `internal/maidenhead` package — `gridToDecimal`,
  `calculateBearing`, `haversineKm`, `pathInfo` — for the
  country panel's short/long path display. Landed sessions
  34-35, 2026-05-05.
- ✅ `internal/bridge` package per ADR 0013 — shipped end-to-end
  via M3a (sessions 48–51). v1 is read-only (no rigctld-compat TCP,
  no PTT — those are out of M3a per ADR 0019). See the M3a
  sub-sections below for the full work record. Cross-listed here
  because the M2 plan originally bundled it; the package and the
  SSE consumer are the foundation the SPA's live-rig display sits on.
- ✅ Real `EventSource` consumer in `bridge.svelte.ts` — populates
  catState from SSE. Shipped session 50 (M3a.4, 2026-05-11).
- ✅ CAT-handover toast — `bridge.svelte.ts` disconnect/reconnect
  state machine SHIPPED 2026-05-16 (Session 66, same day as the
  bridge supervisor in ADR 0020). Three-state shape (idle →
  scheduled → visible) gated by an 800ms flash-suppression window
  so brief blips never reach the operator. `rig-disconnected` →
  `toasts.warn(t('bridge.disconnected.<code>'), ttl=0)` after the
  window elapses; a reconnect inside the window cancels quietly,
  a reconnect after the window dismisses the warn AND pushes the
  positive `toasts.info(t('bridge.reconnected'))`. Code-driven
  i18n via `lib/i18n/en.ts` so different disconnect causes
  (`rig_no_data`, `serial_port_error`, future entries) get distinct
  wording. Sticky-toast leak fix on `closeSource()` followed up
  same session. Verified live on the FTdx10 2026-05-29 — the
  power-off / power-on sequence produces the expected warn / info
  pair around the suppression window.
- 🟡 Keyboard shortcuts per ADR 0007 — **mostly shipped, two
  deferred.** Live in the SPA today: ESC clears the QSO form,
  Ctrl/Cmd+Enter submits, Tab in Callsign triggers enrichment +
  starts the QSO timer, F2 runs enrichment + contact-history fetch
  WITHOUT starting the QSO timer (lookup-only, shipped 2026-05-15),
  Enter/Space on VFO box / SessionPanel row / overlay backdrop,
  Enter / ESC inside VFO frequency input, ESC in the QSO Edit
  Overlay. Deferred: Ctrl+\ VFO swap, `?` help overlay. Full
  inventory lives in `docs/keyboard-shortcuts.md`.
- ⏳ Logbook and config SPAs — deferred. Single `frontend/logging/`
  bundle covers the operator's primary workflow; logbook /
  config become separate routes or separate SPAs when needed.
- ✅ InfoPanel + tabbed surface (Worked / Details / My Station /
  Session). Shipped sessions 42–46. Each tab feeds from existing
  daemon endpoints (`/v1/contact-history`, `/v1/qso/*`, enrichment).
  WAI-ARIA tablist keyboard nav (ArrowLeft/Right/Home/End +
  roving tabindex) added in the session-53 code-review pass.
- ✅ QSO Edit Overlay — modal edit for any session-list QSO via
  `GET /v1/qso/{uuid}` + `PATCH /v1/qso/{uuid}`. Focus trap and
  ESC contract per the session-53 code-review pass.
- ✅ Session email-out — `POST /v1/session/email` ships the session's
  ADIF to a recipient via the daemon's mailer (when `mailer.enabled`).
  Controls live in the InfoPanel tab strip next to the Session tab.
  Reworked 2026-05-31: the SPA posts `{to, uuids[]}` and the daemon
  rebuilds the ADIF from the live DB rows (current data, proper
  `<EOH>` header), durably stamps each QSO `sm_fwrd_by_email_*`
  (SessionPanel "Emailed" column), and archives a local copy under
  `<workingDir>/exports/sent-adif/` before sending.
- ✅ Frontend code review closed (sessions 53–60). All 5 critical,
  all 17 important, 8 of 11 nits closed (3 reviewer-accepted-as-is).
  See `docs/reviews/frontend-logging-2026-05-12.md`.
- 🗑 Text-file fallback — re-evaluated. The SPA is served BY the
  daemon (single-origin embed); SPA-side offline mode is
  incoherent (no daemon → no SPA load). Daemon-side QSO write
  resilience (atomic transaction, forwarder retry) covers the
  invariant. See `project_sm_spa_config_layering` memory.

### Acceptance test

Launch the daemon (`./smd` — no flag needed; first run seeds
`config.json` at the resolved working dir). Open
`http://localhost:<port>/` in a browser. Log a QSO through the SPA;
verify the resulting row in sqlite via `GET /v1/qso/{uuid}`. Verify
the upload-queue rows exist via `GET /v1/qso/{uuid}/uploads`. Verify
SSE events arrive on a separate `curl http://localhost:<port>/v1/events`
stream. Refresh the browser; verify the session timer state survives
(sessionStorage), the manual VFO state survives (localStorage), and
the daemon QSO list is unchanged. Stop the daemon; verify the SPA
shows a connection-status indicator when submit fails — today this
is a `toasts.error("Cannot reach the daemon — check it is running.")`
top-centre toast (network-arm of `lib/api/qso.ts`'s `SubmitOutcome`).

### Milestone 2 — Original Wails scope (preserved as record, pre-ADR 0001)

**Goal:** The three Wails thin clients (`apps/logging`, `apps/logbook`,
`apps/config`) are rebuilt as v2 applications that talk to the daemon
over the Unix socket API.

**Scope:**

- `go.work` reintroduced per `structure.md` decision #2
- `apps/logging` — real-time QSO entry, the primary operator
  interface. Svelte 5 frontend, `internal/smclient` Go HTTP client
  for daemon communication. Text-file fallback when daemon is
  unreachable (the "nothing blocks logging" invariant).
- `apps/logbook` — logbook management, historical browsing, QSO
  editing, ADIF export, forwarding status review.
- `apps/config` — configuration editor. Reads/writes config.json
  directly via `internal/config`, no daemon API.
- `internal/smclient` — shared Go HTTP client library for the daemon
  API, consumed by all Wails app backends.
- SSE integration in the Wails frontends for live updates.

---

## Milestone 3 — Bridge subsystem + external integration

**Goal:** The daemon's bridge subsystem (CAT/serial) ships, the SPA
displays live rig state, and external tools can feed QSOs into the
daemon without going through a SPA.

The original M3 framing (a separate `cmd/sm-serial-bridge` binary
with two frontends) is obsolete per ADR 0013 (2026-05-02) and ADR
0019 (2026-05-10): the bridge is a daemon subsystem (`internal/bridge`),
the v1 shape is read-only, and only one frontend ships in v1. The
sub-milestones below break the work down by concrete artifact.

---

### Milestone 3a — Bridge subsystem v1 (read-only, SPA-only)

**Goal:** The logging SPA displays live rig state — frequency, mode,
VFO, etc. — pushed by the rig via AUTO-mode CAT through the daemon's
new `internal/bridge` subsystem over SSE at `/v1/rig/events`. v1 is
read-only: SPA observes; no commands flow back from SPA to rig.
Settled by ADR 0010 (wire shape) + ADR 0013 (topology) + ADR 0019
(internal v1 design).

#### M3a.1 — Package skeleton + config + SSE plumbing

**Status: ✅ SHIPPED (session 48, 2026-05-10).**

- New `internal/bridge/` package with `Service` lifecycle (`Initialize`
  → `Start(ctx)` → `Stop()`), DI-wired via `internal/iocdi`.
- Config additions: `Bridge.Enabled` (default `true`), `Bridge.Serial.Port`,
  `Bridge.Serial.Baud`, `Bridge.Cat.Driver` (yaesu-ft710 / yaesu-ftdx10 /
  future).
- HTTP route `GET /v1/rig/events` registered on the daemon's mux when
  `Bridge.Enabled: true`. Initially emits hardcoded stub events for
  pipeline verification.
- Package-boundary tests: assert `internal/storage` and
  `internal/forwarder` don't import `internal/bridge` (per ADR 0013's
  package-graph discipline).
- DI wiring in `cmd/smd/main.go`.

**Acceptance:** `curl -N http://localhost:8080/v1/rig/events` shows the
stub events when `bridge.enabled: true`; route doesn't exist when
`false`. Daemon starts/stops cleanly in both configurations.

#### M3a.2 — Serial + CAT pipeline

**Status: ✅ SHIPPED (session 49, 2026-05-10).**

- `internal/bridge/Service.Start(ctx)` opens the serial port via
  `internal/serial` and starts an AUTO-mode read loop.
- Bytes from serial → `internal/cat` decoder → structured events on an
  internal Go channel.
- 30s data-flow timeout → emit `rig-disconnected` (per ADR 0010 passive
  liveness).
- Field filter: forward only SPA-relevant fields (vfoA, vfoB, mode,
  subMode, selectedVfo, splitOverride, power); drop waterfall /
  S-meter / etc.
- Stub-rig test harness: in-memory or PTY-pair Yaesu emulator that
  emits AUTO-mode pushes on cue, lets us integration-test without
  rig hardware.

**Acceptance:** stub rig pushes a frequency change → bridge's internal
channel receives a typed event. 30s of stub silence → `rig-disconnected`
event.

#### M3a.3 — Pipeline → SSE + bootstrap poll + error events

**Status: ✅ SHIPPED (session 49, 2026-05-10).**

- Wire M3a.2's internal channel to M3a.1's SSE handler.
- `http.Flusher.Flush()` after each event write.
- **Bootstrap poll on SSE-open** (ADR 0019): on each new SSE connection,
  send a CAT poll command via `internal/cat`, forward response as
  framed events. *Implementation note:* uses each rigdef's `READ`
  command (single-name dispatch via `cat.Encode(def, "READ")`); for
  Yaesu rigdefs that's `ID;FA;FB;ST;VS;MD0;MD1;PC;` — 8 framed
  responses, each decoded and published as a partial `rig-state`. The
  ADR 0019 example `IF;` was illustrative; the rigdef-driven shape
  works identically for the SPA's catState merge. INIT redefined to
  `AI1;` only (arms AUTO push state; identity now folded into READ).
- `bridge-error` event emission for operator-actionable conditions
  (port permission denied, unknown driver, missing INIT/READ in
  rigdef, INIT write failure, identity mismatch — once per
  pipeline-instance). NOT for transient retries.
- **Hub-cached startup bridge-errors** (review #2): `hub.lastBridgeError`
  caches the most recent `EventBridgeError`; new subscribers receive
  it as their first event so a SPA tab opening AFTER a startup
  failure (typo'd driver, permission denied) still gets the toast.
  Per-Service-instance lifetime; never cleared within a Service.
- Multi-subscriber fan-out tested with 5+ concurrent SSE clients.

**Acceptance:** stub rig + multiple concurrent `curl -N` clients all
see the bootstrap event then live updates. Permission-denied test
produces a clean `bridge-error` with operator-readable reason.

#### M3a.4 — SPA consumer + live rig test

**Status: ✅ SHIPPED (session 50, 2026-05-11).**

- `frontend/logging/src/lib/states/bridge.svelte.ts` is a real
  EventSource consumer wired to `/v1/rig/events`, replacing the
  M3a.1 stub.
- Three event listeners as designed: `rig-state` (field-by-field
  merge into `catState`; `*bool` `splitOverride` preserved via
  explicit existence check so the OFF case survives), `rig-disconnected`
  (warn toast + `rigResponding=false`), `bridge-error` (error toast).
- `bridgeState.connected` mirrors `EventSource.readyState` via
  `open`/`error` event listeners; `bridgeState.rigResponding` flips
  true on every `rig-state` (implicit reconnect per ADR 0009),
  false on `rig-disconnected` or transport `error`.
- `startBridge()` / `stopBridge()` lifecycle: app onMount calls
  `startBridge()` after `fetchConfig()` settles; an internal
  `$effect.root` tracks `configState.station.enabled` and
  opens/closes the EventSource accordingly. `configState.station.enabled`
  is now hydrated from `/v1/config` response's `bridge.enabled`
  (daemon-authoritative per ADR 0003) — the operator flips CAT
  on/off via `config.json` + daemon restart, no SPA toggle needed.
- 19 Vitest cases against a FakeEventSource (lifecycle, connected
  flag, full+partial merge, splitOverride OFF regression,
  disconnected toast, bridge-error toast, JSON-parse fault
  tolerance). Total SPA suite: 464 tests across 27 files green.
- **Daemon-side fixes surfaced during live testing:**
  - `responseRecorder.Unwrap()` added in `internal/api/middleware.go`
    so `http.ResponseController.SetWriteDeadline(time.Time{})` can
    reach the underlying conn and disable the deadline. Without
    this the server's `WriteTimeout=30s` killed every SSE stream
    every 30 seconds; band-change rig pushes during the reconnect
    gap were lost.
  - `BridgeInfo.RigName` exposed via `/v1/config`, resolved daemon-side
    from `cat.Lookup(Bridge.Cat.Driver).Name` (e.g. "Yaesu FTdx10").
    SPA mirrors it into `configState.station.rigName`;
    `displayedState.rigName` derives `isLive ? rigName : ''`.
    Used by the My Station Equipment panel's Rig field (read-only
    when CAT live) and as the ADIF MY_RIG fallback so logged QSOs
    carry the descriptive rig string when the operator hasn't typed
    their own.
  - My Station Equipment panel's Default TX Power field reads
    `catState.power` read-only when CAT live; operator's typed
    default editable when off.
- **Live test confirmed:** dial turn on the FTdx10 → SPA's VFO
  display updates live; mode change → SPA reflects; rig
  identity "Yaesu FTdx10" + power 50W populate Equipment panel
  read-only; QSO submission carries MY_RIG + TX_PWR correctly.
- **Rig-mode → ADIF translation (the M3a.4 follow-up):** SHIPPED
  session 51 (2026-05-11). Two-layer architecture (rigdef-shipped
  defaults in each `internal/cat/rigs/*.json` under `mode_mappings`
  + operator overrides in `config.json` under `bridge.mode_mappings`,
  merged at `/v1/config` GET time). Bridge stays pure pass-through
  (rig literal on the wire); SPA resolves via
  `displayedState.mode` / `.subMode` derivations. New My Station →
  Mode Mappings sub-tab edits the override layer.
- **Side-effect win:** daemon's `internal/enums/modes` enum became
  data-driven via embedded `adif-modes.json` + optional
  `$SM_WORKING_DIR/modes.json` operator override. Future ADIF
  spec growth no longer strictly requires a daemon binary release.

**Acceptance:** ✅ end-to-end live test passed, operator confirmed.
**M3a (bridge subsystem v1) closed.**

---

### What's NOT in M3a (per ADR 0019)

- **PTT and inbound command path** — bridge has no PTT awareness in
  v1; SPA doesn't send commands TO the rig in v1. Built whole when a
  driver appears (FT8 stack TX cycles, future voice keyer, etc.).
- **rigctld-compat TCP frontend** — deferred until a third-party app
  (WSJT-X / fldigi sharing the rig with logging app) needs bridge
  mediation.
- **NDJSON Unix-socket frontend** — deferred until a non-browser
  in-house client (FT8 stack, future CAT control SPA) needs CAT.
- **Bridge-side state cache** — bridge is stateless; SPA's `catState`
  provides value-persistence.
- **Periodic active polling for liveness** — passive 30s data-flow
  timeout only; active polling is a future improvement.
- **Multi-rig implementation** — internal API is rig-ID-aware from day
  one; HTTP route singular for v1, grows to `/v1/rig/{id}/events`
  later.
- **Persistent rig state across daemon restart** — rig itself
  remembers; bridge re-establishes on startup.

---

### Milestone 3b — External integration (deferred)

Original M3's other items, none currently scheduled:

- `cmd/udp-bridge` — generic UDP-to-daemon bridge. Listens on UDP for
  ADIF-formatted payloads and POSTs them to the daemon. Useful for
  WSJT-X/JTDX log integration.
- `cmd/importer` — ADIF bulk import CLI. Reads an ADIF file and
  submits each record via `POST /v1/qso`. Useful for migrating from
  v1 logs or other logging software.
- Multi-rig awareness in the daemon API: `types.Qso` carries a rig
  identifier, the daemon API accepts it, the bridge populates it.
  Only meaningful once multi-rig hardware is in scope (M3a.* design
  is rig-ID-aware but the daemon-side wiring is single-rig).

---

### M3 acceptance test (whole milestone)

For M3a alone: see M3a.4 acceptance.

For M3 as a whole (when M3b lands too): start the daemon. Tune the
rig — observe live updates in the SPA. Submit a QSO with the SPA;
rig state auto-populates from `displayedState`. Run `cmd/udp-bridge`
alongside, send an ADIF UDP packet, see it land in the logbook. Run
`cmd/importer` against a historical ADIF file, verify records appear.

---

## Milestone 4 — FT8 subsystem

> **PARKED 2026-05-30.** The `internal/ft8` subsystem and the `research/`
> clean-room decoder tree were removed from the SM tree and preserved at
> tag **`ft8-snapshot-2026-05-30`** (sandbox at strict 129/18 matched).
> FT8 resumes out-of-tree as a separate stream (planned separate repo →
> fresh clean-room MIT v2 → later import-as-library or re-inline). The
> milestone record below is retained as the work history and design
> reference for when FT8 returns; the licensing preamble remains
> authoritative for the out-of-tree effort. `internal/audio` (CGO-free
> WAV/FFT) was retained in the SM tree.

**Goal:** Station Manager decodes and transmits FT8 in-process via the
`internal/ft8` subsystem. Decode parity with WSJT-X (whatever current
distro `jt9` reports for a given WAV) first; layered improvements only
after parity is established and provable.

The architecture, package boundaries, and reversal of the prior
out-of-tree extraction are settled in ADR 0021. This milestone breaks
that decision down into independently-exercisable slices, mirroring how
M3a sub-divided the bridge work.

### Design preamble

**Licensing constraint (load-bearing — applies to every code decision below).**
WSJT-X is GPL v3; Station Manager is MIT. The two licences are
incompatible — any derivative work of WSJT-X must itself be GPL v3,
which would force-relicense SM. The implementation must therefore be
**GPL-clean**: built from the protocol specification rather than from
the GPL source.

What's safe and what isn't:

- ✅ **Running `jt9` / `ft8sim` as subprocess oracles for testing.**
  Tool use doesn't infect callers (same legal shape as compiling with
  GCC). The M4.1 parity gate operates entirely at this level.
- ✅ **Implementing from the published protocol specification.** Franke
  (K9AN), Somerville (G4WJS), and Taylor (K1JT), "The FT4 and FT8
  Communication Protocols," QEX July/August 2020 — free PDF at
  <https://wsjt.sourceforge.io/FT4_FT8_QEX.pdf> — gives the LDPC(174,91)
  generator matrix, Costas sequence, symbol mapping, and frame
  structure. Steve Franke's companion papers cover the demodulator.
  The WSJT-X user docs cover sequencing and message packing. Read
  these.
- ✅ **Mathematical constants (Costas array, LDPC parity matrix,
  CRC14 polynomial)** cited from the paper. Facts aren't copyrightable;
  reference the paper, don't copy a Fortran array literal.
- ✅ **Using the public-domain reference tarball** at reference [14]
  of the QEX paper: `ft4_ft8_protocols.tgz` from
  `http://physics.princeton.edu/pulsar/k1jt/`. The paper authors
  explicitly carve this out from the GPL: *"With the exception of
  code contained in reference [14], source code for our
  implementations of FT4, FT8, and MSK144 is not in the public
  domain."* The tarball contains `generator.dat` + `parity.dat` (the
  LDPC(174,91) matrices, directly usable), plus short reference
  programs `gen_crc14`, `std_call_to_c28`, `nonstd_to_c58`,
  `hashcodes`, `grid4_to_g15`, `grid6_to_g25`, `free_text_to_f71`
  (source-encoding algorithms, usable as algorithm references and/or
  vendored directly), plus lookup tables `states_provinces.txt`,
  `arrl_rac_sections.txt`. **This is a distinct bucket from the GPL
  WSJT-X tree** — reading and reusing it is explicitly invited.
- ❌ **Translating Fortran sources from the GPL WSJT-X tree to Go.**
  Even line-by-line rewrites in another language are derivative works
  under standard copyright doctrine. Don't open `*.f90` files from
  the WSJT-X main source tree looking for "how does WSJT-X do X" with
  intent to translate. (The reference [14] tarball is in a different
  bucket — see the ✅ bullet above.)
- ❌ **Bundling GPL WSJT-X assets (sample WAVs, dictionaries, message
  files) in this repo.** They're GPL distribution; shipping them inside
  an MIT repo is the contamination route. Operator-recorded WAVs (own
  copyright, MIT/CC0 release) are the long-term clean source.
- ⚠️ **Linking against FFTW3 — nuanced.** FFTW3 is GPL v2 (or commercial).
  CGO bindings against system `libfftw3f` (the operator's own
  `fftw.go` + `fftw_wrapper.c` in go-ft8/research/ are clean
  MIT-able binding code) DO NOT contaminate the SM source. **The GPL
  trigger fires at BINARY DISTRIBUTION** — a binary built with FFTW3
  linkage inherits GPL v2 when shipped to third parties. For
  personal-use builds where the operator is the sole user (per
  `user_profile` memory), the GPL trigger is dormant and FFTW3 use is
  technically permissible. SM's conservative policy preference is
  still **permissively-licensed CGO alternatives** (KissFFT BSD-3,
  PocketFFT BSD-3) for any CGO build path that might ever
  ship publicly — that keeps the binary-redistribution option open
  without forcing GPL on downstream consumers.

When in doubt, the rule is: **the only WSJT-X artifacts that touch this
codebase are the binaries we exec for testing, the academic papers we
cite, and the public-domain reference tarball at QEX paper reference
[14].** The GPL WSJT-X source tree is off-limits as an implementation
reference.

**Protocol-level constraint: no robotic operation.** Section 9 of the
QEX paper imposes a condition on implementations that use the names
"FT4" or "FT8": *"Robotic or unattended QSOs must be explicitly
disallowed... Any implementation of these or similar protocols that
allows robotic, unattended, or non-conforming multi-streaming
operation shall not use the names 'FT4' or 'FT8' and must be made
incompatible by some means, such as using different Costas arrays for
synchronization."* We want bit-compatible FT8, therefore we accept the
constraint. Concretely: every QSO must involve a human-initiated TX
action. Auto-sequencing within a QSO (step from CQ → reply → R+report
→ RR73 → 73 hands-free once the operator clicks Engage) is fine and
matches WSJT-X's behaviour; auto-CQ-call-watch loops with no human in
the loop are not. M4.5's scope reflects this.

**Implementation source: protocol specification.** The FT8 protocol is
fully documented in publicly-published academic papers (Taylor 2020 in
QEX; companion papers by Franke and Taylor in earlier QEX issues).
Implementation works against those papers — not against the Fortran
sources, per the licensing constraint above. Departures from the
specification are explicit decisions captured in commit messages or
follow-up ADRs, never silent drift. Goal: same or better decode results
than WSJT-X under the same conditions, validated by the M4.1 parity gate.

**CGO commitment.** FT8's signal-processing budget inside a 15-second
slot is tight enough that pure-Go alternatives don't carry their weight
— prior research showed measurable wins from native FFT and LDPC
implementations that we can't ignore at this scale. The subsystem
binds permissively-licensed C libraries only (the licensing constraint
forecloses GPL-licensed dependencies):

- **Permissively-licensed FFT preferred** — KissFFT or PocketFFT
  (both BSD-3) for any CGO FFT path. Both are well-trodden
  in scientific computing with stable C APIs. **FFTW3 is the WSJT-X
  reference but its GPL v2 linkage requirement is a binary-
  distribution constraint** (the GPL doesn't contaminate SM's source —
  see the licensing section above for the nuance); the conservative
  policy is to prefer BSD/Apache alternatives so SM's binary
  redistribution options stay open. For personal-use builds where the
  operator is the only user, FFTW3 is permissible. Specific choice
  between KissFFT and PocketFFT is empirical — but as Session 80's
  CGO experiment showed, **scalar CGO transition overhead dominates
  this workload regardless of which FFT library sits on the C side**;
  the library choice only matters once SIMD-batched BP lands.
- **LDPC(174,91) decoder** — clean-room implementation from Taylor's
  2020 paper, written in C and CGO-bound (or written in Go directly if
  benchmarks show no measurable difference; the original CGO motivation
  is FFT throughput, not LDPC).
- **Audio backend (portaudio leaning)** — PortAudio is MIT-equivalent.
  Decided in M4.2 when audio actually lands; ALSA-direct is a Linux-
  only fallback if portaudio proves painful.

Costs accepted (per ADR 0021): cross-compilation complexity, larger
binary, build-time dependency on the CGO toolchain. Caught early by
the CD pipeline (per `project_sm_cd_pipeline_planned` memory) — a CGO
break fails the gate immediately.

**Test architecture: five layers, ordered from "always runs" to "one-shot helper."** Spec-implementation needs falsifiable correctness claims at every level — algorithmic primitives, end-to-end consistency, real-signal robustness. A single "diff against `jt9 -8`" gate is both too strict (locks us into WSJT-X's implementation-specific metrics like exact SNR/DT/freq) and too coarse (a single end-to-end miss tells you nothing about which stage is wrong). The layered shape replaces it:

| Layer | What it proves | Runs in CI? | Needs jt9? | Needs ft8sim? |
|---|---|---|---|---|
| 1. **Spec vectors** — known input → known output for each algorithm (CRC14, LDPC encode/decode, callsign/locator packing, hash codes) | Each piece matches the FT8 spec | ✅ Always | ❌ | ❌ |
| 2. **Round-trip** — SM encoder → SM decoder, verify the message survives | Encoder + decoder are mutually consistent | ✅ Always | ❌ | ❌ |
| 3. **Synthetic signals** — `ft8sim`-generated WAV with known truth → SM decoder, verify it decodes the known message | Decoder pipeline works end-to-end against signals with construction-truth | 🟡 Locally / when fixtures present | ❌ | At corpus prep only |
| 4. **Real off-air signals** — operator-recorded WAV → SM decoder, compared to a `.expected` fixture file listing the known messages | Decoder works under real-world conditions | 🟡 Locally / when fixtures present | ❌ at test time | ❌ |
| 5. **Corpus prep helper** — `cmd/ft8-corpus-prep`: one-shot CLI that runs `jt9 -8` over a directory of WAVs and writes the `.expected` files used by Layer 4 | Generates Layer 4 fixtures | ❌ Never | ✅ One-time | ✅ Optional |

**Critical property: jt9 is not a test-time dependency.** It enters only at Layer 5 — a developer-only CLI tool (`cmd/ft8-corpus-prep`) that runs once when adding new real-signal fixtures. The output `.expected` files become regular test fixtures and Layer 4 tests then run with zero external dependencies. CI machines without WSJT-X installed see Layers 1+2 run normally and Layers 3+4 skip cleanly when fixtures aren't present.

**Why this is better than the original "exact parity against jt9" framing:**

- **Layers 1+2 always run** in CI, with zero external state. Every algorithmic regression is caught the moment it lands.
- **The parity claim is "we decode the same messages"**, not "we produce byte-identical output to jt9." Implementation-specific signal-processing details (SNR estimator choice, DT calculation method, frequency-bin resolution) are ours to pick without forcing WSJT-X parity on them. WSJT-X bugs are not bugs we must replicate.
- **Each layer pinpoints a different failure mode.** A Layer 1 failure means "this algorithm is wrong." A Layer 2 failure means "encoder and decoder disagree." A Layer 3 failure means "the demodulator can't handle clean signals." A Layer 4 failure means "real-world conditions break us." Cleaner debugging surface than a monolithic end-to-end gate.
- **Test fixtures are operator-supplied locally**, not bundled in the repo (per the licensing constraint). Both synthetic (Layer 3) and real (Layer 4) fixtures live on the operator's machine; tests iterate the configured directories and skip when empty.

**Fixture layout:**

```
internal/ft8/codec/testdata/
├── synthetic/             # Layer 3 — ft8sim-generated WAVs
│   ├── README.md          # how to regenerate
│   ├── cq_known_snr-10.wav
│   ├── cq_known_snr-10.wav.expected
│   └── ...
└── realsignals/           # Layer 4 — operator-recorded WAVs
    ├── README.md
    ├── 210703_133430.wav
    ├── 210703_133430.wav.expected
    └── ...
```

Each `.expected` file is plain text, one message per line — only the *messages* (callsigns, locators, reports) which are protocol-level facts. No SNR/DT/Freq metadata, since those are implementation-specific.

> **Naming note: `jt9` the binary vs JT9 the protocol.** The decoder
> CLI `jt9` is WSJT-X's general decoder, historically named after the
> JT9 mode but extended over time to handle FT8, FT4, JT65, MSK144,
> FST4, Q65, and others. The `-8` flag selects FT8 mode. Station
> Manager is FT8-only — JT9-the-protocol is out of scope (different
> timing, different modulation, different sensitivity). Wherever this
> doc says "`jt9`" we mean the binary in FT8 mode (`jt9 -8`), not the
> JT9 protocol.

**Decode-threshold targets (Table 5 of the QEX paper).** The M4.1 parity
gate has a measurable goal beyond "matches `jt9 -8` byte-for-byte":
WSJT-X's FT8 sensitivity on AWGN is −19.6 dB SNR (block detection +
belief-propagation only) and −20.8 dB SNR (BP + ordered-statistics
decoding fallback). These are measured in 2500 Hz bandwidth. SM's
decoder should land at or near the −19.6 dB threshold for M4.1; OSD
fallback (the extra ~1 dB) can be a later refinement if BP-only proves
insufficient. Decode-probability-vs-SNR curves can be measured against
operator-recorded WAVs at known SNRs, or against `ft8sim`-generated
WAVs at specified SNRs.

**Package boundary discipline.** Per ADR 0021 inheriting ADR 0013:
`internal/ft8` may import `internal/errors`, `internal/logging`,
`internal/types`, `internal/cat`, `internal/serial`, and
`internal/qsoservice` (FT8 is a consumer — decoded QSOs flow via
`qsoservice.Submit`). It MUST NOT import `internal/bridge`; bridge
MUST NOT import ft8. If a later milestone needs bridge state inside
FT8 (likely at M4.3 — see below), the route is a neutral package both
can depend on, not a direct sibling-to-sibling import.
`boundary_test.go` in both packages defends this; CI catches drift.

**Defaults for open questions** (so they don't block writing code):

- **Decode concurrency:** single-threaded implementation at M4.1. Fan-
  out only if M4.2 measurements show the 15s budget is tight under
  the operator's actual workload. Matches "build specific, not generic"
  — premature concurrency is harder to debug than slow code.
- **Decoded-message persistence:** in-memory ring buffer for v1.
  Historical "who did I hear when" is logbook-app territory per the
  logging-vs-logbook scope memory; v1 keeps just enough state to feed
  the M4.6 SPA panel.
- **Time sync:** assume NTP-correct host. The daemon measures and
  surfaces the slot-offset (how late the slot fired relative to UTC)
  as a status field; if it drifts >500ms the operator sees a warning
  but the decoder keeps trying. No active clock-correction in v1.
- **TX dependency:** M4.4 is gated on the inbound CAT command path
  landing (parked follow-up flagged in ADR 0021). Until that ships,
  PTT can't be keyed from the FT8 subsystem and TX work is a no-op
  to attempt. M4.1–M4.3 are unblocked.

---

### M4.1 — WAV-file decode (parity with `jt9 -8`)

**Status: 🚧 IN PROGRESS.** Layer 1 (algorithmic primitives) **shipped 2026-05-17**:
all nine encode-side message-codec primitives complete in
`internal/ft8/codec/`, ~85 tests pinned against the public-domain
QEX ref [14] reference programs, all benchmarks zero- or one-
allocation. Layer 2 (message-format packing per QEX Table 1 i3.n3
types) is mid-flight: **Phase 1 (BitBuilder utility) shipped
2026-05-18; Phase 2B (Type 1 encoder) + Phase 2C (Type 1 decoder
+ round-trip + encoder routing fix) + Phase 2D (token codec +
`FormatMessage` / `ParseMessage` text layer + bit-faithful decode
of Type 1 tokens) + Phase 3A (Type 0.0 Free Text + F71ToFreeText
Layer 1 inverse + i3.n3 sub-dispatch) shipped 2026-05-19; Phase 3B
(Type 2 EU VHF /P encoder / decoder / format / parse + Suffix1/2
rename + /P classifier branch) + Phase 3C (Type 4 NonStd Call
encoder / decoder / format / parse + `C58ToCallsign` Layer 1
inverse + `Message.Hash12` + `wire.go` constant centralisation)
shipped 2026-05-20; Phase 3D (Type 5 EU VHF hashes+g25 encoder /
decoder / format / parse + `G25ToGrid6` Layer 1 inverse + new
`Message.Hash22` / `Report3` / `Serial` / `Grid6` fields + Free
Text classifier tightening to exclude angle-bracket inputs +
Type 5 classifier dispatch ahead of Type 4) shipped 2026-05-20;
Phase 4 hash table (receiver-side running callsign-hash table —
`internal/ft8/codec/hashtable.go` with bounded FIFO + LRU-on-
reinsert eviction, `Insert` / `LookupH22` / `LookupH12` /
`LookupH10` / `Observe(Message)` / `Resolve(Message) Message`)
shipped 2026-05-20; **session 77 (2026-05-21) spec-correctness
sweep + Type 3 RTTY Roundup SHIPPED** — six-commit chain landing
(i) the **i3=5 mis-assignment fix** (Phase 3D had shipped Type 5
at wire i3=3 instead of the QEX-Table-1-mandated i3=5; constants
renumbered + test pins updated, i3=3 freed for Type 3 RTTY RU);
(ii) **finding #1** — `CallsignC28` rewritten with digit-
position-3 alignment so EVERY std-shape callsign packs into the
`[stdCallOffset, 2^28)` range per QEX Appendix A Table 7's
"Standard call signs" row ("28 bits are enough to encode any
standard call sign uniquely") — short-call `HashedCallC28`
routing dropped; spec-vectors regenerated (M1A 10,608,568, K1JT
10,222,009, VK7MO 237,090,319, G3X 9,483,721, AB1CD 86,389,684);
(iii) **finding #2** — Type 2 c28 now accepts tokens (CQ / DE /
QRZ / "CQ <suffix>") per QEX Table 2's universal c28 definition;
`validateType2Call` delegates to `validateType1Call`; new
`validateType2Suffix` gates /P on a token; `parseEUVHFP`
refactored into dispatcher + `parseCQEUVHFP` / `parseDirectedEUVHFP`
/ `parsePlainEUVHFP` mirroring `parseStd`; "CQ G4ABC/P JO22"
round-trips end-to-end; (iv) **finding #3** — Free Text parser
now falls back to Type 0.0 when structured parse fails AND
`validateFreeText` accepts (so QEX Table 1's canonical example
"TNX BOB 73 GL" with no `.`/`?` trigger parses correctly);
(v) **finding #4** — `G25ToGrid6` signature changed from
`(uint32) string` to `(uint32) (string, bool)` so out-of-range
g25 (~44% of 25-bit codepoints) returns `("", false)` instead of
panicking; `decodeEUVHFHash` wraps as new `ErrInvalidGrid6`;
(vi) **Type 3 RTTY Roundup end-to-end** — new `rttyroundup.go`
Layer 1 primitive for the multi-modal s13 exchange slot
(serial 0..7999 OR state/province from the 65-entry QEX ref [14]
`states_provinces.txt` lookup; `S13Kind` enum + `SerialToS13` /
`StateToS13` / `S13ToExchange` API), new `Message.TU` (t1 prefix)
+ `Message.StateProvince` fields, new `encodeRTTYRoundup` /
`decodeRTTYRoundup` / `formatRTTYRoundup` / `parseRTTYRoundup`,
new `ErrInvalidS13` sentinel for unassigned s13 codepoints,
classifier trigger `isType3Trigger` on `"TU;"` first-token OR
`"5N9"` 3-digit report token (N ∈ 2..9). Type 3's r3 displays
per QEX Table 2 as "5"+(r3+2)+"9" (r3=0 → 529, r3=7 → 599).
Type 1 ("Std Msg") now round-trips std callsigns of any length
3..6 (incl. short calls K1JT / G3X / M1A / VK7MO / AB1CD that
previously errored as `ErrCallsignNeedsHashLookup`) AND
CQ/DE/QRZ/`CQ <suffix>` tokens bit-for-bit AND text-for-text.
Type 2 ("EU VHF /P") round-trips std-callsign pairs AND tokens
with the /P portable suffix on either or both call slots.
Type 3 ("RTTY Roundup") round-trips with TU prefix + R ack +
token-aware Call1 + serial-or-state exchange forms. Type 4
("NonStd Call") round-trips one-hashed-side + one-nonstd-c58-side
pairs plus the CQ-from-nonstd form; the hashed side surfaces as
`<...>` sentinel + raw `Hash12`. Type 5 ("EU VHF hashes+g25")
round-trips contest-style exchanges where BOTH callsigns hash to
the wire (h12 + h22) plus `AckBit`, `Report3`, `Serial`, `Grid6`.
Type 0.0 ("Free Text") round-trips 1-13 character f71-alphabet
payloads via both eager `.`/`?` trigger and the no-trigger
fallback.
Parser classifier dispatch order: Free Text on the presence of
`.` or `?` (AND absence of angle brackets); Type 5 on both first
two tokens being angle-bracketed AND the last token being a
6-char Maidenhead grid; Type 4 on angle brackets / mid-slash /
non-/R-non-/P trailing slash / token longer than 6 chars without
slash; Type 2 on the presence of any `/P` token suffix; Type 3
on `"TU;"` first-token OR `"5N9"` 3-digit report token; Type 1
by default; Free Text no-trigger fallback if Type 1 fails AND
input fits f71 constraints.
The Phase 4 hash table itself shipped 2026-05-20:
`HashTable.Insert` / `Observe(Message)` for receiver-loop ingest,
`LookupH10` / `LookupH12` / `LookupH22` for direct hash queries,
and `Resolve(Message) Message` for type-aware sentinel
replacement (Type 4 via Hash12; Type 5 via Hash12+Hash22).
Phase 4 specialty codec types (0.1 DXpedition / 0.3+0.4 Field Day
/ 0.5 Telemetry) deferred 2026-05-21 — operator confirmed they
aren't needed for personal use; can return when needed.
**Session 78 (2026-05-21 afternoon) — DSP pipeline + real-WAV FT8
decoding SHIPPED end-to-end.** Nine commits built the audio →
messages signal-processing chain in clean-room spec-based Go:
LDPC belief-propagation decoder + CRC14 gate (per QEX §6),
new `internal/audio/` package (WAV reader + pure-Go mixed-radix
FFT covering all 5-smooth FT8 sizes), new `internal/ft8/dsp/`
sub-package (params + spectrogram + Costas-sync detector +
baseband downsampler + soft demodulator per Taylor §6's L_j),
top-level `Decode` entry point in `internal/ft8/decode.go`
wiring WAV → Spectrogram → Sync → per-candidate (Downsample
→ Demodulate → LDPCDecode → DecodeMessage → FormatMessage).
**Real WAV decoding works:** `internal/ft8/testdata/ft8_cap1.wav`
(operator-recorded capture, WSJT-X 2.7.0 finds 11 signals)
yields 2 decoded messages from SM's pipeline — `SV2SIH ES2AJ -16`
at 1862.5 Hz and `VE1WT K4GBI 73` at 1309.4 Hz. The 2/11
sensitivity gap is the next-session topic — OSD (LDPC fallback),
fine-frequency search around sync candidates, fine-timing
alignment within symbol windows, K-scale tuning in the
demodulator are each a session-sized chunk.
**Notable: Costas-sync detector clean-room redo.** First attempt
was a port of go-ft8/research/sync8.go which itself ports
WSJT-X's GPL `lib/ft8/sync8.f90`. Operator caught the
derivation immediately and rejected it; redone from QEX paper
§4 + textbook matched-filter signal processing. Result is a
deliberately simpler detector — 21-position Costas template,
mean-in-pattern / mean-out-of-pattern ratio score; skipped the
sync8.f90 tunings (triple-block scoring, BC-only fallback,
40th-percentile normalization, narrow/wide search split,
QSO-priority placement) — all available as future clean-room
reinventions if sensitivity demands.
**Side discovery — GrayUnmap latent bug.** Positions 5 and 7
were swapped in `internal/ft8/dsp/params.go` (carried verbatim
from go-ft8/research/constants.go where it was dormant). Caught
during demodulator testing; fixed against QEX paper Table 3 and
pinned by `internal/ft8/dsp/params_test.go`'s
`TestGrayMapInversesGrayUnmap` + `TestGrayMapMatchesQEXTable3`.
M4.1 is now structurally complete (WAV → messages works end-
to-end on real audio). Sensitivity tuning + M4.2 live audio
are the next-milestone topics.

**Session 80 (2026-05-21 late evening) — performance + sensitivity
overhaul.** ~28-commit day arc closed with 18.5× speedup on the
pipeline and decode rate 12 → 22 across the three vendored
fixtures (46% of WSJT-X parity, up from 25%). Six commits, in
order: (1) Phase 1 — cache forward FFT in Decode (profiling
showed 77.9% of CPU was a 192k FFT running 100× per slot with
identical input). New `dsp.ForwardSpectrum` + `dsp.Downsample
FromSpectrum` API; 6.38s→0.69s. (2) Multi-capture validation:
extended benchmark + smoke test across all 3 fixtures with per-
capture decode floors; surfaced that cap1's 1/11 number was
misleadingly hostile (cap2=4/14, cap3=7/23 are truer baseline).
(3) Phase 2 — `audio.Plan` with precomputed master twiddle
table + alternating workspace buffers (wsA/wsB); recursion
alternates buffer roles at each level for zero per-level
allocations. Net allocation per `Plan.FFT(x)` is exactly one
slice. Spectrogram constructs one Plan up-front and reuses
across all 372 sliding FFTs; 0.69s→0.36s, allocs 4.5M→35K
(126× fewer). `audio.FFT`/`audio.IFFT` retained as back-compat
ad-hoc-Plan wrappers. (4) Plan-threading: added `WithPlan`
variants for `ForwardSpectrum`, `DownsampleFromSpectrum`,
`Demodulate`; Decode constructs 3 plans per slot. Cumulative
Phase 1+2: 18.5× faster, 157× less memory, 1083× fewer allocs;
44× under 15-s slot budget. (5) Fine-timing retry — when a
candidate's coarse-DT LDPC attempt fails, retry at `c.DT+
dOffset` for `dOffset ∈ {0, ±5ms, ±10ms}` (= 0, ±1, ±2 baseband
samples at Fs2=200, within the 32-sample demod symbol window).
First success wins. **+6 decodes**: cap2 4→6, cap3 7→11.
Empirically both ±5ms AND ±10ms matter — cap3 picks up
`5Z4VJ YB1RUS OI33` and `CQ SP4MSY KO13` from ±10ms shifts
alone. Cost 344ms→870ms. (6) Fine-frequency retry — outer
freq-retry loop wraps inner fine-timing loop; each freq retry
triggers fresh DownsampleFromSpectrumWithPlan at `c.Freq +
fOffset` for `fOffset ∈ {0, ±1.5625, ±3.125}` Hz (= 0, ±0.5
bin, ±1 bin) then re-runs full fine-timing. Labelled `break
freqRetry` short-circuits. **+4 decodes**: cap1 1→3
(**`VE1WT K4GBI 73` recovered — was a sub-bin freq offset,
NOT the DT offset I'd predicted; lesson: sub-bin freq drift is
operationally more common than sub-step DT drift on real
captures**), cap3 11→13. Cost 870ms→3.5s (still 4.3× under
budget). Plus a magic-numbers→config refactor (operator-
caught mid-implementation): `DecodeOptions.FineTimingOffsets`
+ `FineFrequencyOffsets` (nil → defaults, non-nil-empty →
disable, non-nil populated → exact override), with
`DefaultFineTimingOffsets` + `DefaultFineFrequencyOffsets`
package vars carrying empirical doc comments per
`feedback_no_magic_numbers.md`. Smoke-test floors locked:
cap1=3, cap2=6, cap3=13.
After fine-freq landed (22/48 = 46% parity), Session 80
continued with three more stages that brought decode parity
to 85%:
(h) **OSD (Ordered Statistics Decoding) as LDPC fallback** at
`internal/ft8/codec/ldpc_osd.go` (~230 LOC, clean-room from
Fossorier & Lin 1995). Algorithm: sort codeword positions
by |LLR| descending, permute H matrix's columns by
reliability, Gauss-Jordan over GF(2) right-to-left so
pivots land on the 83 LEAST-reliable independent columns
(parity), leaving the 91 most-reliable as the MRB (info
bits). Hard-decide MRB → re-encode parity → check CRC14.
Order-1 search flips each MRB bit individually (~91
trials). H stored bit-packed as `[83][3]uint64` for fast
row XOR. `OSDDecode(llrs, order)` + `LDPCDecodeWithOSD`
wrapper + `DecodeOptions.OSDOrder` (default 1). **Decode
rate jumped 22 → 41 (+19 decodes, 46%→85% parity)**. Cost
870 ms → ~4.5 s per slot. Smoke-test floors bumped to
cap1=8/cap2=13/cap3=20.
(i) **OSD allocation pool** — post-OSD profile showed
`osdMRBSetup` allocating ~50 KB per call × ~2125 calls per
slot. Initially restructured to "OSD-only-at-coarse" but
that lost 16 decodes (41→25); reverted. Correct fix:
`osdScratch` type holding all fixed-size buffers, pooled
via `sync.Pool`. Flat `parityDepsBuf []uint8` replaces
slice-of-slices. **Per-slot allocs 968K → 316K (-67%) with
zero sensitivity change.**
(j) **CGO KissFFT + Sleef experiment — measured, reverted,
learned.** Vendored KissFFT (BSD-3) + linked Sleef
(Boost license via Fedora sleef-devel) behind `-tags
ft8cgo`. Hit intermittent SIGSEGV from
`runtime.SetFinalizer` racing with active cgo calls;
fixed by `runtime.KeepAlive(p)` + slice keepalives.
Benchmarked: **2.7× SLOWER than pure-Go.** Scalar cgo
overhead (~200 ns/call) dominates ~143K per-symbol N=32
FFTs + ~3M tanh/atanh calls per slot. Big-FFT wins
(N=192000, ~3ms compute) get swamped by the tiny-FFT
overhead. Reverted entirely. Three small artifacts kept
that survived cleanly: `internal/audio/itoa.go` (util
extracted from fft.go during the file split),
`internal/ft8/codec/transc.go` (`tanh`/`atanh` indirection
— costs nothing, leaves swap point for future
SIMD-batched experiments), `ldpc_decode.go` using the
indirection (`math` import dropped). Cautionary tale
saved at `feedback_cgo_scalar_interop_overhead.md`. To
actually win with CGO requires SIMD-batched BP (~200-300
LOC refactor to feed Sleef vector variants), not drop-in
scalar interop.
**Session 81 (2026-05-22) extended M4.1 past the original parity
goal.** Four-stage arc:
(k) **K-scale config exposure.** `DemodScale` const replaced with
`dsp.DefaultLLRScale` (package var) + explicit `scale` parameter
on `Demodulate` / `DemodulateWithPlan`; new
`DecodeOptions.LLRScale` field resolves zero-value → default.
Knob plumbed for future per-candidate noise-floor scaling.
(l) **Real-input FFT (RealPlan) vendored** from operator's
MIT-clean go-ft8/research/realfft.go (Brigham §10-5 1974
pack-and-unpack textbook technique, NOT a WSJT-X port).
Spectrogram switched from complex Plan to RealPlan; 3-5%
slot-time saved (4.57s → 4.36s on cap1). FFTW3 GPL nuance
clarified across docs — GPL fires at binary distribution
(libfftw3f linkage), not at source-level `#include <fftw3.h>`
+ `-lfftw3f`; for SM's personal-use case the trigger is
dormant and FFTW3 use is technically permissible (the
permissive-CGO-deps policy is about keeping binary
redistribution options open, not legal necessity).
(m) **GFSK FT8 waveform synthesizer SHIPPED** at
`internal/ft8/dsp/synthesis.go` (~210 LOC clean-room from
QEX §4 + Murota & Hirade 1981, NOT WSJT-X
`gen_ft8wave.f90`). BT=2 Gaussian shaping per spec.
Constants per no-magic-numbers policy:
`GFSKBandwidthTimeProduct` const (spec-mandated),
`GFSKGaussianTruncationSigma` var (overrideable). Round-trip
test (Synthesize → ForwardSpectrum → DownsampleFromSpectrum
→ Demodulate → LDPCDecode) recovers the input message,
confirming demod-compatibility.
(n) **Q-function shortcut — 87× speedup** (210ms → 2.4ms).
Replaced brute-force per-sample convolution with the
standard GMSK trick: Gaussian-filtered freq trajectory
equals superposition of step responses
`Δf · Φ((t - τ) / σ)` at each symbol boundary; transition
zones (±3σ ≈ ±32ms) don't overlap with T=160ms symbol
duration, so freqTraj fills in two simple sweeps
(steady-state segments at constant freq[n], transition
zones via precomputed Φ shape × Δf). Op count drops ~114M
→ ~150K. Round-trip still passes.
(o) **`SynthesizeBoth` + `SubtractSignal`.** Refactored synthesis
to share freqTraj + phase computation between sin-only and
sin+cos paths. `SynthesizeBoth` returns both quadrature
templates via `math.Sincos`. `SubtractSignal(audio,
msgBits, f0, dt)` does matched-filter amplitude+phase
estimation via real projection (a = Σ audio·sin / Σ sin²,
b = Σ audio·cos / Σ cos²), subtracts `a·sin + b·cos` from
audio in place, returns √(a²+b²). Sin/cos templates
nearly orthogonal at FT8 carriers (~18K+ cycles over the
12.64s TX window).
(p) **Iterative subtraction loop in Decode — HEADLINE WIN.**
New `DecodeOptions.SubtractionPasses int` +
`DefaultSubtractionPasses = 0` const (opt-in until further
verification). After pass 0 (unchanged), optional outer
loop: copy audio (caller's buffer preserved) → for each
pass-(N-1) decode call `dsp.SubtractSignal` → recompute
Spectrogram + Sync + ForwardSpectrum on residual → re-run
candidate decode loop with msgBits-keyed dedup → break
early if zero new decodes. **Real-WAV results with
SubtractionPasses=1: cap1 8→10, cap2 13→17, cap3 20→24;
total 41→51 (+10, +24% sensitivity, 106% of WSJT-X 2.7.0
parity).** Cost ~2× wall time (4.5s → ~8.7s per slot, 58%
of budget). We now find MORE decodes than WSJT-X on these
captures (51 vs 48); extras are likely exotic compound-call
or hash-collision matches that CRC14 + LDPC structural
validation keeps at negligible false-positive rate.

M4.1 cumulative result vs original baseline: time 6.38s →
~4.4s (SubtractionPasses=0, baseline-equivalent), ~8.7s
(SubtractionPasses=1, new opt-in); memory 10GB → ~150MB
(66× less); allocs 37.9M → ~316K (120× fewer); **decode
rate 12 → 51 (+325% sensitivity, 25% → 106% WSJT-X
parity)**. **M4.1 closes here at 106% WSJT-X parity** —
audio→messages pipeline structurally complete beyond the
original parity goal. Remaining sensitivity moves
(K-scale noise-floor experiments, AP decoding via BP
soft-priors, sync-step fine-timing in baseband, raising
DefaultSubtractionPasses to 1) are incremental refinements;
M4.2 live audio capture wiring is the bigger next-track
item.

> **⚠ CORRECTION (Session 83, 2026-05-22): the "106% WSJT-X parity"
> figure above was FALSE-POSITIVE-INFLATED.** It counted raw decode
> output, which includes low-confidence CRC14-lottery survivors (garbage
> messages). Measuring `match`-to-oracle (decodes that jt9 3.0.1 ALSO
> finds) instead of raw count, real parity is **~54%** (corpus: SM
> match=90 of jt9=180). The iterative-subtraction "+10 decodes" were
> entirely false positives — matched count is identical at
> SubtractionPasses 0 and 1. See the **M4.1 refinement** subsection
> below; subtraction stays default-off.
Deferred follow-up still open: Type 1's `decodeStd` continues to
return `ErrCallsignNeedsHashLookup` for c28 values in the
`[nTokens, stdCallOffset)` hash partition. After finding #1 the
partition only holds legitimate non-std-call hashes (Type 4 c58 →
later h22 reference); the symmetric "surface `<...>` + `Hash22`,
let `Resolve` handle it" path is still unshipped.

| Layer 1 primitive | File | ns/op | allocs |
|---|---|---|---|
| `Pack` / `Unpack` (bit-per-byte ↔ packed-byte) | `bits.go` | — | 0 |
| `CRC14` (polynomial 0x6757) | `crc14.go` | 973 | 0 |
| `CallsignC28` (standard call → 28 bits) + `C28ToCallsign` inverse | `callsign.go` | 34–46 | 0 |
| LDPC(174,91) matrices + `LDPCEncode` | `ldpc.go`, `qexref14/` | 4123 | 1 |
| `Grid4ToG15` (4-char grid / reserved / report) + `G15ToGrid4` inverse | `grid.go` | 4–9 | 0 |
| `Grid6ToG25` (6-char grid) + `G25ToGrid6 (uint32) (string, bool)` inverse (Phase 3D; signature widened session 77 finding #4 — out-of-range g25 returns `("", false)` rather than panicking, decoder wraps as `ErrInvalidGrid6`) | `grid.go` | 4.6 | 0 |
| `HashCodes` (h10/h12/h22) + `HashedCallC28` | `hashcodes.go` | 50 | 0 |
| `FreeTextToF71` (13-char free text → 71 bits) | `freetext.go` | 150 | 1 |
| `CallsignC58` (nonstandard / compound call) + `C58ToCallsign` inverse | `nonstdcall.go` | 48 | 0 |

Phase 2 additions:

| Layer 2 component | File | Notes |
|---|---|---|
| `BitBuilder` | `bitbuilder.go` | MSB-first composer; chainable `Append` + `AppendBits`; ~271 ns for a 77-bit Type 1 build |
| `Message` + `MessageType` | `message.go` | Concrete struct discriminated by `Type`; 10 enum values declared; **implemented (post-session-77):** `MessageTypeStd` + `MessageTypeFreeText` + `MessageTypeEUVHFP` + `MessageTypeRTTYRU` + `MessageTypeNonStdCall` + `MessageTypeEUVHFHash`. Per-callsign `Suffix1`/`Suffix2` boolean fields are type-neutral — Type 1 renders /R, Type 2 renders /P, Types 3/4/5 don't use them. `Hash12 uint16` carries the raw 12-bit hash for decoded Type 4 + Type 5 messages; `Hash22 uint32` for Type 5's second-callsign slot. `Report3 uint8` (0..7) is reused across Types 3 + 5 with different display mappings; `Serial uint16` is reused across Types 3 + 5 with per-Type max (2047 for Type 5 s11, 7999 for Type 3 s13 serial form). `Grid6 string` carries Type 5 Maidenhead. **Session 77 additions:** `TU bool` (Type 3 t1 prefix slot) + `StateProvince string` (Type 3 s13 state-form exchange) |
| `wire.go` | `wire.go` | Layer 2 wire-format constants centralised: `MessageBits`, `i3Width` / `i3Offset`, i3 tag values (`i3Std` / `i3EUVHFP` / `i3RTTYRoundup` / `i3NonStdCall` / `i3EUVHFHash` / `i3Zero` — per QEX Table 1 column "Type i3.n3" the dotted-number IS the wire i3 for top-level types; **session-77 fix** corrected `i3EUVHFHash` from 3 to 5), n3 family (`n3FreeText` / `n3FieldBits`), Type 4 widths (`h12Bits` / `h1Bits` / `r2Bits` / `c1Bits`), Type 4 r2 token codes, Type 5 widths (`r3Bits` / `s11Bits`) + bias (`r3Bias`=52, Type 5 displays "52".."59"). **Session 77 additions for Type 3:** `t1Bits` / `s13Bits`, `s13SerialMax`=7999, `s13StateBase`=8001, `r3DisplayBiasType3`=2 (Type 3 displays "529".."599"). Per-primitive Layer 1 widths stay with their primitive files (`HashBits22` in hashcodes.go, `G25Bits` in grid.go) |
| `EncodeMessage` + `encodeStd` + `type1CallToC28` | `encode.go` | Type 1 packer; routes Call1/Call2 through `TokenToC28` first, then `CallsignC28` directly (post-session-77 finding #1 — `CallsignC28` uses digit-position-3 alignment so every std-shape call packs into the `[stdCallOffset, 2^28)` range per QEX Appendix A Table 7); `validateType1Suffix` blocks token+suffix combinations; `slices.Clone` on return detaches BitBuilder storage |
| `encodeEUVHFP` + `validateType2Call` + `validateType2Suffix` | `encode.go` | Type 2 packer; `c28 + suffix + c28 + suffix + R1 + g15 + i3=2` = 77 bits; routes Call1/Call2 through `type1CallToC28` (post-session-77 finding #2 — Type 2 c28 accepts tokens per QEX Table 2's universal "Standard callsign, CQ, DE, QRZ, or 22-bit hash" definition); `validateType2Call` delegates to `validateType1Call`; new `validateType2Suffix` rejects /P portable bit on a token |
| `encodeRTTYRoundup` + `validateRTTYReport` | `encode.go` | Type 3 packer (session 77); `t1 + c28 + c28 + R1 + r3 + s13 + i3=3` = 77 bits. Token-aware c28 via `type1CallToC28`; no per-call suffix slots; exclusive choice between Serial (0..7999) and StateProvince (lookup in `rttyRoundupStates`); `validateRTTYReport` bounds Report3 ∈ [0,7] |
| `encodeNonStdCall` + `validateType4Calls` + `isType4ValidNonStdCall` + `gridToR2` / `r2ToGrid` | `encode.go` | Type 4 packer; `h12 + c58 + h1 + r2 + c1 + i3=4` = 77 bits; std-vs-nonstd shape detection picks h1; `Call1 == "CQ"` sets c1 (h12 wire bits ignored); validator enforces exactly-one-std + one-nonstd OR CQ + nonstd, rejects CQ-with-suffix and Type 1 tokens, restricts Grid to `{"", "RRR", "RR73", "73"}` |
| `encodeEUVHFHash` + `validateType5Call` + `validateType5Report` + `validateType5Serial` + `validateType5Grid` | `encode.go` | Type 5 packer; `h12 + h22 + R1 + r3 + s11 + g25 + i3=5` = 77 bits (i3 corrected session 77); both callsigns go through `HashCodes` (h12 from Call1, h22 from Call2); validator enforces std-callsign-only on both calls (tokens rejected), Report3 in [0,7], Serial in [0,2047], Grid6 as strict 6-char Maidenhead |
| `encodeFreeText` + `validateFreeText` + `isF71Char` | `encode.go` | Type 0.0 packer; `f71 + n3=0 + i3=0` = 77 bits via `BitBuilder.AppendBits`; validates 1-13 char range + f71 alphabet upstream of the primitive |
| `DecodeMessage` + `decodeStd` + `type1CallFromC28` + `readBitsUint64` | `decode.go` | Type 1 unpacker; bit-faithful (accepts token+suffix wire pattern, semantic gates run at format/encode); `ErrTokenInGap` for spec-violating wire values; `ErrCallsignNeedsHashLookup` for c28 in the hash partition (post-finding-#1 only holds legitimate non-std-call hashes — short std calls now decode losslessly) |
| `decodeEUVHFP` | `decode.go` | Type 2 unpacker (post-session-77 finding #2 — uses `type1CallFromC28` since Type 1 and Type 2 c28 partitions are identical per QEX Table 2; obsolete `type2CallFromC28` removed); hash-range c28 surfaces `ErrCallsignNeedsHashLookup` like Type 1 |
| `decodeRTTYRoundup` | `decode.go` | Type 3 unpacker (session 77); routes c28 via `type1CallFromC28`; `S13ToExchange` populates either Serial (0..7999) or StateProvince (from `rttyRoundupStates` lookup); new `ErrInvalidS13` for unassigned s13 codepoints (8000, > 8065) |
| `decodeNonStdCall` | `decode.go` | Type 4 unpacker; recovers c58 side fully via `C58ToCallsign`, surfaces raw `Hash12` value + `hashedCallSentinel` (`"<...>"`) on the hash side until Phase 4's table resolves it. CQ-from-nonstd path (c1=1) zeroes `Hash12` per spec |
| `decodeEUVHFHash` | `decode.go` | Type 5 unpacker; both call slots surface as `hashedCallSentinel` (`"<...>"`) with raw `Hash12` + `Hash22` in `Message` for Phase 4's table to resolve; Grid6 recovers via `G25ToGrid6` (post-session-77 finding #4 — now returns `(string, bool)`, decoder wraps as `ErrInvalidGrid6` instead of panicking on out-of-range); AckBit / Report3 / Serial decode straight from the wire |
| `decodeI3Zero` + `decodeFreeText` | `decode.go` | Type 0.0 unpacker; sub-dispatches the i3=0 family on n3 (Phase 3A wires n3=0 only; 1/3/4/5 return `ErrUnknownMessageType` until Phase 4) |
| `TokenToC28` + `C28ToToken` | `token.go` | CQ-token partition per QEX Table 7; unified formula `c28 = 1003 + base27(left-space-padded 4-char suffix)` with `(T, bool)` returns; intra-row + inter-row gaps surface as `(_, false)` |
| `F71ToFreeText` | `freetext.go` | Layer 1 inverse of `FreeTextToF71`; 128-bit divmod via `math/bits.Div64`; trims leading spaces per `adjustr` padding asymmetry |
| `S13ToExchange` + `SerialToS13` + `StateToS13` + `rttyRoundupStates` | `rttyroundup.go` | Type 3 Layer 1 multi-modal s13 exchange primitive (session 77). `rttyRoundupStates` table verbatim from QEX ref [14] `states_provinces.txt` (65 entries). `S13Kind` enum: `Serial / State / Unassigned`. Round-trip via `(SerialToS13 \| StateToS13)` forward + `S13ToExchange` inverse. The Type 3 r3 display ("5"+(r3+2)+"9") is rendered in `formatRTTYRoundup`, not in this primitive |
| `FormatMessage` + `formatStd` + `formatEUVHFP` + `formatRTTYRoundup` + `formatNonStdCall` + `formatEUVHFHash` + `formatFreeText` | `format.go` | Type 1 + 2 + 3 + 4 + 5 + 0.0 text rendering. Type 1 renders `/R`; Type 2 renders `/P`; both share `formatGridField` (ack-R fused with reports `R-09`, separated from grids `R IO91`). Type 3 (session 77) renders `[TU; ]<call1> <call2> [R ]5N9 <exchange>` with 4-digit zero-padded serial or state code. Type 4 angle-brackets the hashed side; Type 5 brackets BOTH calls and fuses report+serial into one token. Type 0.0 emits the FreeText payload as-is |
| `ParseMessage` + `parseStd` + `parseEUVHFP` + `parseRTTYRoundup` + `parseNonStdCall` + `parseEUVHFHash` + `parseFreeText` + classifier | `parse.go` | Text → Message; classifier dispatch order: Free Text (`.`/`?` AND no angle brackets) → Type 5 (dual `<>` + g25 grid trailing) → Type 4 (any bracket / mid-slash / non-/R-non-/P trailing slash / > 6 chars no slash) → Type 2 (`/P` suffix) → Type 3 (`"TU;"` first-token OR `"5N9"` 3-digit report token, N ∈ 2..9) → Type 1; **post-session-77 finding #3:** Free Text fallback after Type 1 fails if input ≤ 13 chars and all chars in f71 alphabet (handles "TNX BOB 73 GL"). `parseEUVHFP` is now a dispatcher mirroring `parseStd`'s shape (post-session-77 finding #2): `parseCQEUVHFP` / `parseDirectedEUVHFP` / `parsePlainEUVHFP` |
| `C28Kind` + `G15Kind` + `S13Kind` discriminators | `callsign.go`, `grid.go`, `rttyroundup.go` | Route-B kind enums for the multi-modal c28 / g15 / s13 partition spaces |
| `HashTable` (`Insert` / `LookupH22` / `LookupH12` / `LookupH10` / `Observe` / `Resolve` / `Len`) | `hashtable.go` | Phase 4 receiver-side running callsign-hash table per QEX §6. Bounded capacity (default 100, WSJT-X convention) with FIFO eviction of the oldest entry on overflow + LRU-on-reinsert (re-Inserting a present callsign moves it to MRU). Newest-wins on hash collision via newest-to-oldest lookup iteration; collision rates at cap=100 are h22≈0.002% / h12≈2.4% / h10≈10%. Insert silently filters non-callsigns (empty, `<...>` sentinel, tokens via `TokenToC28`, out-of-alphabet, > 11 chars) and trims whitespace. Observe walks Call1/Call2 of a decoded Message and Inserts each. Resolve takes a Message and returns a copy with sentinel call slots replaced — Type 4 uses Hash12 → whichever slot is sentinel; Type 5 uses Hash12 → Call1 and Hash22 → Call2; resolved hash fields are zeroed; other Types return unchanged. Thread-safe via `sync.RWMutex` |

Session 78 DSP-layer additions (the audio → messages signal-processing pipeline):

| DSP component | File | Notes |
|---|---|---|
| `LDPCDecodeBP` + `LDPCDecode` | `internal/ft8/codec/ldpc_decode.go` | Sum-product belief-propagation decoder per QEX §6 (tanh product-rule with prefix/suffix accumulation for O(1) excluded-variable updates; numerical clamping at 1−1e−15 keeps atanh finite). Default 50 iterations; per-iter syndrome-zero early exit. `LDPCDecode` adds the CRC14 gate (per QEX §6's "decoded codeword's 77-bit CRC matches" criterion) — returns the recovered 77-bit message body iff BP converged AND CRC matched. ~35 µs clean / 70 µs with 5-bit errors on operator's i3-10100F. OSD (Ordered Statistics Decoding) explicitly deferred — Taylor §6 says BP catches the vast majority; OSD adds ~1 dB |
| `audio.ReadWAV` | `internal/audio/wav.go` | WAV file reader at new neutral `internal/audio/` package (peer of internal/cat/serial). Supports PCM 8-bit unsigned, PCM 16-bit signed, IEEE float 32-bit. Adversarial-chunk-size defence via `io.LimitReader`. Single `Data{SampleRate, Channels, Samples []float32}` exported struct. The shape FT8 expects: 12 kHz mono PCM16 |
| `audio.FFT` + `audio.IFFT` | `internal/audio/fft.go` | Pure-Go mixed-radix Cooley-Tukey radix-2/3/5 covering every 5-smooth FT8 size (192000/180000/3840/3200/1920). Operator-authored (ported MIT-clean from go-ft8/research/fft.go). Pure stdlib (math + math/cmplx) — no CGO, no gonum dependency. Performance: ~400 µs at N=1920, ~870 µs at N=3840 — about 20-50× slower than FFTW3 but well within FT8's 15-second slot budget. **The operator's go-ft8/research/fftw.go + fftw_wrapper.c are clean MIT-able binding code** (no FFTW3 source vendored; `#include <fftw3.h>` + `-lfftw3f` linkage only) and could move into SM if desired — they're not vendored here by policy preference rather than legal necessity. The GPL v2 trigger on FFTW3 fires at binary distribution, not in source; for personal-use builds where the operator is the only user, FFTW3 use is permissible. Session 80's CGO experiment showed that scalar CGO transition overhead dominates this workload anyway, so the FFT library choice is largely moot until SIMD-batched BP lands (see the licensing section + Session 80 entry for the full nuance) |
| `dsp` params | `internal/ft8/dsp/params.go` | FT8 protocol constants from QEX paper + ft8_params.f90: Fs=12000, NSPS=1920, NN=79, NMAX=180000, NFFT1=3840, NH1=1920, NSTEP=480, NHSYM=372, NDOWN=60, NFFT2=3200, NFFT1DS=192000, Fs2=200, Baud=6.25, SpectrogramScale=1/300 (WSJT-X parity convention), Icos7={3,1,4,0,6,5,2}, GrayMap, GrayUnmap. **Session 78 fix:** GrayUnmap had positions 5 and 7 swapped (latent bug carried verbatim from go-ft8/research/constants.go) — fixed against QEX paper Table 3 and pinned by `TestGrayMapInversesGrayUnmap` + `TestGrayMapMatchesQEXTable3` regression tests |
| `dsp.Spectrogram` | `internal/ft8/dsp/spectrogram.go` | `Spectrogram(audio []float32) [][]float64` — slides a 1920-sample window at NSTEP=480 stride across 12 kHz audio, FFTs each window (zero-padded to 3840) via audio.FFT, output `power[t][f]` for t∈[0,372), f∈[0,1920). Single contiguous backing array (~5.4 MB at canonical sizes) sliced into row views for cache-friendly access by the Costas correlator. 310 ms per full 15-s slot |
| `dsp.Sync` + `dsp.Candidate` + `dsp.SyncOptions` | `internal/ft8/dsp/sync.go` | **Clean-room Costas-sync detector** per QEX §4 + textbook matched-filter signal processing. First attempt was a port of go-ft8/research/sync8.go which itself ports WSJT-X's GPL `lib/ft8/sync8.f90`; operator caught and rejected; redone clean-room from scratch. 21-position Costas template (3 blocks × 7 tones), score = mean in-pattern Costas-tone power / mean out-of-pattern tone power across the same time slots, descending-sync sort, proximity-based dedup (1 freq bin × 2 time-steps). **Deliberately skipped** (deferred to clean-room reinvention if sensitivity demands): triple-block scoring (sync8's t_a/t_b/t_c separation), BC-only fallback for late signals, 40th-percentile noise-floor normalization, narrow vs wide time-lag search split, operator-frequency priority placement. On real ft8_cap1.wav → 100 candidates with sensible peaks at FT8-band frequencies |
| `dsp.Downsample` | `internal/ft8/dsp/downsample.go` | `Downsample(audio []float32, f0 float64) []complex128` — FFT-based mix to baseband and decimate from 12 kHz to 200 Hz. Forward 192000-point FFT of zero-padded audio, extract 3200 bins centred on f0 (with Hann edge taper), inverse 3200-point FFT → 16 seconds of complex baseband at 200 Hz. The bin extraction IS the anti-alias filter (no explicit FIR design needed given the FFT in hand). 58 ms per call |
| `dsp.Demodulate` | `internal/ft8/dsp/demod.go` | `Demodulate(baseband []complex128, dt float64) []float64` — produces 174 LLRs per Taylor 2020 §6's L_j formula. For each of 58 data symbols (channel symbols 7..35 and 43..71): extract 32 baseband samples, 32-point FFT, take magnitudes at the 8 FSK tone bins, compute per-bit `L_j = max{\|C_i\| : bit=0} − max{\|C_i\| : bit=1}` over Gray-demapped tone partitions. LDPC-literature sign convention (positive ⟹ bit-is-0) so output feeds LDPCDecode directly. K scale defaults to 1.0 (per Taylor §6 "adjusted empirically" — tunable knob if sensitivity work surfaces it) |
| `ft8.Decode` + `ft8.DecodedMessage` + `ft8.DecodeOptions` | `internal/ft8/decode.go` | Top-level entry point wiring the full pipeline: WAV → Spectrogram → Sync → per-candidate (Downsample → Demodulate → LDPCDecode → DecodeMessage → FormatMessage). Returns `[]DecodedMessage{Freq, DT, SyncPower, Message, Text}` for every successful decode. Candidates that fail at any stage are silently dropped — the sync detector is permissive (typically 100/slot for a busy band) and the LDPC + CRC chain is the structural validator that separates real signals from noise |
| `testdata/ft8_cap{1,2,3}.wav` + README | `internal/ft8/testdata/` | Operator-recorded FT8 captures vendored from go-ft8/testdata (operator-owned recordings, not WSJT-X GPL samples; ~360 KB each, ~1 MB total). README documents provenance (7Q5MLV's station), file format (12 kHz mono PCM16), and WSJT-X 2.7.0 decode counts (cap1=11, cap2=14, cap3=23). Tests resolve via `$FT8_TEST_CORPUS` env override → `testdata/` fallback; skip-on-missing per the L4 (real-signal) test layer |

The codec package was renamed from `internal/ft8/decoder` →
`internal/ft8/codec` mid-sequence (when it became clear the same
primitives serve both encode and decode paths). The `jt9` parity-
oracle wrapper that briefly lived inside the package now lives in
`cmd/ft8-corpus-prep/` per the layered test architecture above —
no test-time dependency on WSJT-X.

**Goal:** Read an FT8 WAV file from disk, run the full decode pipeline
end-to-end, emit decoded `{message, snr, time_offset, freq_offset}`
records that match what WSJT-X's `jt9 -8` produces against the same
input. No audio capture, no rig, no TX, no storage, no SPA.

#### Scope

- CGO bindings: BSD-licensed FFT (KissFFT or PocketFFT — pick one in
  this milestone's first commit set after a small benchmark on FT8
  access patterns) + LDPC(174,91) decoder. Vendor C sources or link
  against system libraries — decided alongside the FFT pick after
  assessing cross-compile impact.
- Implement audio resample to baseband, per spec.
- Implement coarse sync — search for the Costas sync sequence across
  the 15s window (sync pattern from Taylor 2020).
- Implement fine sync + frequency estimation.
- Implement demodulator — soft-symbol generation from baseband.
- Implement LDPC(174,91) decode + CRC14 check, generator matrix from
  Taylor 2020.
- Implement message decoder — callsign1/callsign2/locator/report
  unpacking per the message-formats section of the WSJT-X user docs.
- CLI entrypoint: `smd ft8 decode <file.wav>` subcommand. Prints one
  decoded message per line to stdout. The output is for human
  inspection and ad-hoc diff against `jt9 -8`; tests don't depend on
  exact format compatibility.
- **Layer 1 (spec vectors).** A `*_test.go` per algorithmic primitive
  (CRC14, LDPC encode/decode, callsign packing, locator packing,
  hash codes, message packing/unpacking). Each test pins known
  inputs to known outputs derived from the QEX paper and/or computed
  once via the public-domain reference programs in QEX ref [14]
  (`gen_crc14`, `std_call_to_c28`, `nonstd_to_c58`, `hashcodes`,
  `grid4_to_g15`, `grid6_to_g25`, `free_text_to_f71`). These tests
  always run in CI; no external state, no fixtures, no skipping.
- **Layer 2 (round-trip).** A `*_test.go` that exercises SM's
  encoder + decoder end-to-end at the bit/symbol/baseband level: an
  arbitrary message goes in, comes out unchanged. Catches any
  encoder/decoder disagreement on the protocol. Always runs.
- **Layer 3 (synthetic).** Tests iterate `internal/ft8/codec/testdata/synthetic/`;
  for each `<name>.wav` they run SM's decoder and assert the decoded
  message matches `<name>.wav.expected`. Skip cleanly when the
  directory is empty. Operators generate fixtures locally by running
  `ft8sim` with chosen message + SNR; the resulting `.expected`
  file is the message they handed to `ft8sim`.
- **Layer 4 (real signals).** Same iteration shape as Layer 3 but
  over `internal/ft8/codec/testdata/realsignals/`. Fixtures here
  are operator-recorded WAVs paired with `.expected` files written
  by `cmd/ft8-corpus-prep`.
- **Corpus prep (Layer 5).** `cmd/ft8-corpus-prep` already exists
  (moved in Step-0 of M4.1 from an earlier `internal/ft8/parity/`
  experiment). Current shape: `ft8-corpus-prep <file.wav>` runs
  `jt9 -8` and prints decoded messages. Grows to
  `-in DIR -out DIR` (walk + write `.expected` files) as Layer 4
  fixtures land.
- **No WAVs in the repo.** Per the licensing constraint above, both
  the synthetic and real-signal fixture directories live on operator
  machines (or are git-ignored at the testdata path). Tests skip
  when fixtures aren't present; CI without WSJT-X still passes
  Layers 1+2.

#### Acceptance

```
# Layer 1+2 — always available, always run
go test -race ./internal/ft8/codec/...
# Expected: all algorithmic + round-trip tests pass

# Layer 3 — operator has generated synthetic fixtures
ls internal/ft8/codec/testdata/synthetic/*.wav | wc -l   # > 0
go test -race ./internal/ft8/codec/... -run TestSyntheticCorpus
# Expected: every WAV decoded to the message in its .expected file

# Layer 4 — operator has recorded + prepped real fixtures
ls internal/ft8/codec/testdata/realsignals/*.wav | wc -l   # > 0
go test -race ./internal/ft8/codec/... -run TestRealSignalCorpus
# Expected: every WAV decoded; messages match the .expected files

# Layer 5 — adding a new real-signal fixture (developer task)
go run ./cmd/ft8-corpus-prep ~/recordings/250517_133430.wav \
    > internal/ft8/codec/testdata/realsignals/250517_133430.wav.expected
cp ~/recordings/250517_133430.wav internal/ft8/codec/testdata/realsignals/
# Then re-run Layer 4 test — new fixture is in scope automatically
```

When Layers 1+2 are green, Layer 3 is green against a small seed
corpus of synthetic WAVs covering clean / noisy / multi-station
cases, and Layer 4 is green against at least the bundled WSJT-X
sample (`210703_133430.wav` decoded correctly), M4.1 is done.

---

### M4.1 refinement (Session 83, 2026-05-22) — honest metric, false-positive gates, stage-level diagnosis

After M4.2 Phase A/B (Session 82), the focus turned to decode QUALITY against
**real captures** (operator-recorded 10m + 20m slots) using **WSJT-X 3.0.1
`jt9 -8`** as a black-box oracle. The key correction: parity must be measured
as **`match`-to-oracle** (decodes jt9 also finds), not raw decode count —
SM's raw output is inflated by low-confidence CRC14-lottery false positives.

**Honest baseline:** corpus (3 vendored + 6 new live WAVs) SM `match`=90 of
jt9=180 = **~54% real parity**, with **52 false positives**. (The Session-81
"106% parity" was raw count incl. garbage.)

**Two new developer tools (the workbench):**
- `cmd/ft8-eval` — runs `ft8.Decode` on WAV files/dirs with every tuning knob
  as a flag, `-jobs N` parallel, `-oracle` (shells `jt9 -8`) + `-diff`, and the
  honest `match/miss/extra` table. `cmd/ft8-capture-probe -out` + new
  `internal/audio.WriteWAV` persist live captures for repeatable measurement.
- `cmd/ft8-stage-probe` — **clean-room** stage-level diagnostic: synthesise a
  KNOWN message (via `dsp.Synthesize`) + calibrated AWGN (WSJT-X 2500 Hz SNR
  convention), score each stage; demod scored by **bit-errors / 174** (LLR
  hard-decisions vs the known codeword). Built INSTEAD of instrumenting GPL jt9
  — see the clean-room note below.

**SHIPPED — false-positive gates (52 → 18 false positives, ZERO real lost):**
- **B1** `DecodeOptions.MinSyncPower` (default `DefaultMinSyncPower = 3.0`):
  drop final decodes below a sync-power floor. 52 → 44.
- **B2** `DecodeOptions.OSDMaxNormDist` (default `DefaultOSDMaxNormDist = 0.06`):
  OSD soft-distance acceptance gate (`codec.softDistanceNorm` / `osdAccept`) —
  reject an OSD codeword when its reliability-weighted normalised distance to
  the received LLRs exceeds the ceiling. Gates ONLY the OSD path (BP+CRC is
  trusted; OSD's bit-flip search is ~96% of the false positives — confirmed by
  OSD-off → match 70 + false+ 2). 44 → 18. The discriminating band is
  compressed near zero (OSD builds codewords from the most-reliable bits, so
  its output always agrees with high-confidence positions); knee at 0.06, below
  0.05 loses real decodes. **OSD order-2 is poison** (match −8, false+ ×10).
- Smoke floors updated 8/13/20 → 4/8/17 (removed = all false positives).
  `TestDecode_SubtractionDoesNotDecreaseCounts` rewritten to a relative
  invariant (`sub1 >= sub0`). New unit tests `TestSoftDistanceNorm` /
  `TestOSDAccept`.

**STAGE-LEVEL DIAGNOSIS (`ft8-stage-probe` SNR sweeps):**
- **Sync is not the bottleneck** — 100% candidate detection by −22 dB, below
  where SM can decode.
- **Clean on-grid demod is fine** — decode threshold ~−18 dB vs WSJT-X's
  published ~−21 dB single-shot, so ~2-3 dB behind on clean AWGN, entirely in
  demod-errors → decoder. OSD does the heavy lifting (−18 dB: BP 8% vs OSD 92%).
- **ADJACENT-SIGNAL INTERFERENCE is the dominant real-capture failure mode.**
  A +6 dB neighbour 6 Hz away pins demod at ~51/174 bit-errors at EVERY SNR;
  10 Hz → 80-90 errors, 0% decode even at −6 dB. `demod@true == demod@cand`, so
  it is NOT a centering problem — the neighbour's tones fall inside the
  target's 8 tone-bins (6.25 Hz spacing; a 32-pt symbol FFT cannot resolve a
  6-16 Hz neighbour). This explains the missed STRONG signals in dense real
  slots (e.g. the 2112/2118 Hz pair, both missed) and why clean synthetic
  signals decode but busy real slots don't.
- **Per-symbol LLR noise normalization is a DEAD END** (implemented + measured
  + reverted). Off-tone-bin noise estimator made it worse (rectangular-window
  sidelobes scale the "noise" with signal strength → down-weights the cleanest
  symbols); in-band estimator helped raw BP slightly but WRECKED OSD (its
  most-reliable-basis selection depends on the absolute LLR magnitude
  *ordering*, which per-symbol rescaling scrambles). `max0−max1` already
  self-normalises per symbol. **Do not re-attempt LLR-magnitude normalization.**

**At session-83 end the working hypothesis was that effective iterative
subtraction was the next sensitivity lever.** Session 84 (next subsection)
ruled that out with two-signal synthetic experiments: matched-filter
subtraction cannot separate in-channel pairs even when the synth template is
bit-perfect, and AP (a-priori) decoding plus coherent demod are the real
levers — see Session 84 update below.

**GPL/clean-room note.** Instrumenting jt9 to compare intermediate stages is
legal to RUN (GPL v3 triggers on *distribution*, not private modify/run;
numeric stage data is not copyrightable) but would **break SM's clean-room
firewall** — placing the instrumentation requires READING WSJT-X's GPL source,
and a single developer who reads the GPL reference then writes the MIT
implementation is the classic clean-room contamination, putting SM's
MIT-distributability (the whole point of the § Licensing constraint) at risk.
`ft8-stage-probe` (synthetic known-signal) + the ref [14] public-domain
programs are the sanctioned substitutes.

#### M4.1 refinement continued (Session 84, 2026-05-23) — subtraction structurally ruled out

Session 83's stage diagnosis fingered iterative subtraction as the most
promising remaining lever. Session 84 built the apparatus to test it
cleanly — and the verdict is **the matched-filter subtraction in
`dsp.SubtractSignal` cannot work in the regime that matters**.

**Tool extended.** `cmd/ft8-stage-probe` gained a two-signal mode
(`-msg2`, `-freq2`, `-gain2`, `-subtract`, `-out`): synthesises two
genuinely-distinct known messages + AWGN; runs `ft8.Decode` at
`SubtractionPasses=0` and `=1`; scores "A found / B found / extras" per
trial against the known target text. Audio is fully deterministic from
its parameters; verified byte-identical re-runs. 16 canonical fixtures
shipped at `captures/synthetic/` with `regen.sh` + `README.md` — the
permanent regression yardstick.

**Sweep matrix (4 cells × 10 trials each):**

| Configuration | Δf=10 | Δf=20 | Δf=30 | Δf=40 | Δf=50/60/200 |
|---|---|---|---|---|---|
| Equal gain (A=B=1), SNR=-6 | A 10% / B 70% | A 100% / B 70% | both 100% | both 100% | both 100% @50 |
| A 2× B, SNR=-6 | A 100% / B 0% | A 100% / B 0% | A 100% / B 0% | A 100% / B 0% | both 100% @60 |
| A 4× B, SNR=-6 | A 100% / B 0% | A 100% / B 0% | A 100% / B 0% | A 100% / B 0% | — |
| A 2× B, SNR=-2 (louder) | A 100% / B 0% | A 100% / B 0% | A 100% / B 0% | A 100% / B 0% | — |

**Three honest conclusions:**

1. **Masking scales with relative loudness, not just Δf.** Equal-gain
   pairs clear at 30 Hz; A=2× B masks B at 30 Hz; A=4× B masks B even at
   40 Hz. The "effective channel width" widens as one signal gets louder.
2. **SNR doesn't punch through.** -6 → -2 dB (4 dB louder) recovered B
   at zero spacings. The limit is structural (tone-bin energy overlap),
   not noise-floor-limited.
3. **Subtraction NEVER helps and consistently adds false positives.**
   12 cells × 10 trials with A's bits known EXACTLY (perfect synth
   template) — zero new B-decodes recovered; FPs added in 7 of 12 cells.
   Matched-filter projection cannot separate in-channel signals: when
   A and B share tone-bin space, the estimator's `a/b` coefficients
   absorb energy from both, so subtracting `a·sin_A + b·cos_A` also
   removes part of B's signal.

**Audio-quality angle (revisited).** Also a dead end for the current
bottleneck. Already proved Session 83 (`jt9` decodes 29 from
`live_slot1.wav` vs SM's 11 matched — same bytes; audio is fine). 16-bit
12 kHz PCM gives ~96 dB headroom on a 50 dB-range signal; the gap is
the decoder. The ONE audio-side bet that would matter is I/Q (complex
baseband) capture from an SDR for coherent demod — but the FTdx10 has
no I/Q output. Critically: **the complex baseband is already present
internally** post `dsp.Downsample`; SM just throws the phase away at
`dsp/demod.go:161` (`cmplx.Abs`). Coherent demod can happen from
existing audio, no rig change needed.

**Recommended next actions (queued, not started):**

- **Delete the subtraction code.** Per CLAUDE.md "no half-finished
  implementations": remove `DecodeOptions.SubtractionPasses`, the pass
  loop in `internal/ft8/decode.go`, `dsp.SubtractSignal` /
  `SynthesizeBoth` if unused elsewhere, and the three subtraction tests.
  Synthetic fixtures stay as the decoder-progress yardstick.
- **Coherent demodulation.** Highest-impact lever — single-file rewrite
  of `dsp/demod.go` to use complex amplitudes (track carrier phase
  across the 79 symbols, compute LLRs from complex tone-bin amplitudes
  instead of magnitudes). Expected gain ~2-3 dB matched to Session 83's
  clean-AWGN diagnosis. Validation: `captures/synthetic/` should
  improve at Δf 30/40 Hz with gain-asymmetric pairs; `ft8-eval` corpus
  match-to-oracle should climb from ~54%.
- **AP decoding.** Second lever — `internal/ft8/codec/ldpc_decode.go`
  needs an optional `priorLLR []float64` parameter so callsign-hash
  matches from the running `HashTable` can seed bit priors. jt9's
  `a1`-marked weak-CQ decodes are AP-assisted; SM has none.

---

### M4.2 — Continuous live audio + slot scheduling

**Status: 🚧 IN PROGRESS.** Session 82 (2026-05-22) shipped Phase A
(audio capture) + Phase B (UTC slot scheduler) end-to-end, validated
live on the FTdx10. Phases C (Service wiring), D (status endpoint),
and E (10-minute acceptance) are the remaining chunks.

**Goal:** The daemon captures live audio, schedules decode windows
aligned to UTC 15-second boundaries, and feeds each window through
M4.1's pipeline. Decodes scroll continuously while the daemon runs.
Still no rig coordination, no TX.

#### Phase breakdown

- **Phase A — audio capture skeleton.** ✅ SHIPPED 2026-05-22.
- **Phase B — UTC slot scheduler.** ✅ SHIPPED 2026-05-22.
- **Phase C — FT8 Service wiring.** 🚧 NEXT.
- **Phase D — `GET /v1/ft8/status` endpoint.** 🚧 Follows Phase C.
- **Phase E — 10-minute live acceptance run.** 🚧 Final gate.

#### Decisions landed during Phases A + B (Session 82)

- **Audio backend = miniaudio via `github.com/gen2brain/malgo`.**
  Both libraries are public-domain (Unlicense), safer than MIT for
  binary redistribution. PortAudio was the doc's earlier leaning;
  malgo was picked because station-manager-v1's audio package
  (which the operator already uses successfully on this hardware)
  is built on it, so v1's `Capture` shape ported cleanly. ALSA-direct
  remains the Linux-only fallback if malgo ever proves painful.
- **CGO is back, but isolated.** Session 80's anti-CGO finding
  applied to hot-path scalar math; audio capture is the opposite
  regime (low-frequency callbacks delivering big buffers — CGO
  overhead amortises well). Capture lives in `internal/audio/capture/`
  so the existing `internal/audio` package (FFT/WAV/etc.) stays
  CGO-free.
- **Audio device config = single string field** following the
  `BridgeSerialConfig.Port` precedent. Operator supplies the device
  name (e.g. `"PCM2903C Audio CODEC Analog Stereo"`) seen in the
  `ft8-capture-probe -list` output; empty = system default.
- **Sample rate handling = miniaudio's built-in resampler.** The
  FTdx10's USB CODEC is natively 48 kHz stereo; miniaudio
  downsamples to 12 kHz mono on the C side. Validated clean on
  live RF; Go-side decimation is a future tweak if quality ever
  bites.
- **Slot handoff = channel, not callback.** `Scheduler.Slots() <-chan
  Slot` decouples worker-pool and concurrency choices in Phase C.
- **Failure policy = fail-soft** (saved to `project_ft8_failsoft`
  memory). Audio device fails to open / capture stops mid-session /
  decode panics → log warn, noop. Daemon and other subsystems stay
  up. Mirrors the "enrichment never blocks logging" invariant.

#### Phase A — audio capture skeleton (SHIPPED 2026-05-22)

New `internal/audio/capture/` subpackage. `Capture` type with
`New / Init / ListDevices / Start(ctx) / Stop / Close`, `Samples()
<-chan []float32` for buffered consumption, `SetCallback` for
low-latency direct-from-callback processing. Lock-free callback
path via `atomic.Pointer`, CAS-protected Start, TOCTOU-safe
`safeSend`, `DroppedChunks() int64` metric, `closeOnce`-guarded
channel teardown.

`DefaultConfig()` returns FT8 canonical: 12 kHz, mono, float32,
`DeviceIndex=-1` (system default), `BufferSize=512` (~43 ms callback
period at 12 kHz).

Errors API uses v2's `internal/errors` (`.WithErr`/`.WithMsgf`).

New `cmd/ft8-capture-probe` developer binary:

```
# List capture devices
go run ./cmd/ft8-capture-probe -list

# Capture 15 s from device N at 12 kHz mono, decode FT8 messages
go run ./cmd/ft8-capture-probe -device=N
```

**Phase A live validation result** (FTdx10 on a busy 20 m FT8
frequency): 179 712 samples in 14.98 s, peak amplitude 0.0927
(-21 dBFS), dropped chunks=0, **14 decoded messages** including
`7Q6UJ DO7TTR JN58` (operator's own country prefix). End-to-end
audio→messages works on real RF.

#### Phase B — UTC slot scheduler (SHIPPED 2026-05-22)

New `internal/ft8/ring.go` — wrapping float32 ring at `dsp.NMAX`
(180 000) with `Append([]float32)`, `Snapshot() []float32`,
`Filled() int64`. Internal to `internal/ft8`. Snapshot linearises
oldest → newest in chronological order; the head is zero-padded
until Filled ≥ Cap.

New `internal/ft8/scheduler.go` — `Scheduler{source, log, out,
dropped}`:

- Reads `<-chan []float32` (typically `capture.Capture.Samples()`),
  drains into the ring continuously in `Run(ctx)`.
- `time.Timer` aimed at `nextSlotBoundary(time.Now().UTC())` — the
  next UTC :00/:15/:30/:45.
- On fire: snapshots the ring, emits `Slot{StartUTC, OffsetMs,
  Samples}` on `Slots() <-chan Slot` (cap-1 buffered).
- Cold-start skip: no emission until the ring is full
  (`Filled >= Cap`). Avoids surfacing half-zero slots that would
  poison the decode floor.
- Channel-full → increments `Dropped()` and logs warn. Healthy run
  expects zero drops.

```go
sch := ft8.NewScheduler(capture.Samples(), s.log)
go func() { _ = sch.Run(ctx) }()
for slot := range sch.Slots() {
    results := ft8.Decode(slot.Samples, ft8.DecodeOptions{...})
    // log / submit / publish
}
```

Test coverage: table-driven `nextSlotBoundary` (mid-slot, boundary-
exact, minute / hour / day rollover, non-UTC normalisation); ring
unit tests (write-below-cap, wrap, overflow-single, overflow-massive,
snapshot-independence); integration tests waiting for real UTC
boundaries (`-short` skip).

Extended `cmd/ft8-capture-probe` with `-scheduler`, `-slots N`,
`-subtraction-passes` flags. SIGINT-safe shutdown.

**Phase B live validation result** (FTdx10): 4 consecutive slots at
`offset=0ms` on every fire, 180 000 samples each, peak 0.116-0.123
throughout, 0 dropped slots, 0 capture drops, decode 3.6-4.1 s per
slot (single worker has ~3× headroom on this hardware). Slot 1 and
Slot 2 captured the same `7Q6UJ ↔ S52TW` QSO in opposite directions
with sync ≥ 10, confirming live timing alignment is real.

Low-sync (< 6) CRC14 false-positive noise still surfaces a few
garbage decodes per slot (`WG67F-6E?1K-?`, `9LR4PQD6VRRL3`, etc.) —
deferred as a future sync-threshold tuning pass.

#### Phase C — FT8 Service wiring (next)

Replace `internal/ft8/Service.Start`'s placeholder goroutine with
the real chain:

- New `types.Ft8AudioConfig{Device string}` (single field, mirrors
  `BridgeSerialConfig.Port`). Added under `types.Ft8Config.Audio`.
- `Service.Start(ctx)` enumerates devices (`capture.New(...).Init`
  then `ListDevices`), picks the first matching `cfg.Audio.Device`
  (exact match; empty → -1 default). On any capture-open failure:
  log warn + return nil (fail-soft per `project_ft8_failsoft`).
- Spawn `Scheduler.Run` via `safego.GoTracked`.
- Spawn decode worker via `safego.GoTracked`: `for slot := range
  sch.Slots()` → `ft8.Decode(slot.Samples, DecodeOptions{HashTable:
  s.hashTable, ...})` → `s.log.InfoWith().…Msg("ft8.decode")` per
  result.
- Long-lived `codec.HashTable` allocated at Service scope so hash
  resolution spans slots (per ADR 0021's M4.1 design intent).
- `Stop()` cancels the run context, drains the in-flight slot,
  calls `capture.Close()`.

Config additions on `Ft8Config`: `Audio.Device string`,
`SlotOffsetWarnMs int` (default 500), `SubtractionPasses int`
(default 0, exposed but not raised yet).

Decision deferred to Phase C kickoff: should we surface `Slot` /
`DecodedMessage` events on the SPA-facing SSE stream now, or wait
for M4.6? Current lean: journal-only for M4.2; SSE in M4.6.

#### Phase D — `GET /v1/ft8/status` endpoint

Minimal JSON:

```json
{
  "enabled": true,
  "audio_device": "PCM2903C Audio CODEC Analog Stereo",
  "slot_offset_ms": 0,
  "last_slot_at": "2026-05-22T16:45:30Z",
  "last_decode_count": 6,
  "dropped_slots": 0,
  "subtraction_passes": 0
}
```

Read-only; mirrors `/v1/bridge/status`-style endpoints.

#### Phase E — Acceptance (final M4.2 gate)

```
# Rig manually tuned to a busy FT8 frequency:
./smd                                              # daemon running, ft8.enabled=true
journalctl --user -u smd -f | grep ft8.decode      # decodes scroll live

# Slot timing health
curl http://localhost:8080/v1/ft8/status
# Expected: {"slot_offset_ms":42,"last_decode_count":7,...}
# slot_offset_ms stays under 500 for a clean run
```

Continuous decoding for a 10-minute window with no dropped slots, no
audio-buffer overruns, slot offset under 500 ms throughout. Then
M4.2 closes.

---

### M4.3 — Rig-aware decode (read-only)

**Status: 🚧 NOT STARTED.** Unblocks when M4.2 is green.

**Goal:** FT8 decodes are tagged with the current band and dial
frequency from the bridge. Operator can change band on the rig and
FT8 decodes immediately reflect the new context without restart.
Still RX-only.

#### Scope

- **Cross-subsystem signalling** without breaking the bridge↔ft8 no-
  import rule. Two options to weigh in the M4.3 design pass:
  1. Neutral pubsub package (`internal/rigstate` or similar) that
     bridge publishes to and ft8 subscribes to. Both subsystems
     depend on the neutral package; neither depends on the other.
  2. Channel injected at construction time via the DI container —
     `cmd/smd/main.go` wires the producer end (bridge) and the
     consumer end (ft8) without either knowing about the other.
  Default lean: option 1, because the neutral package can grow other
  consumers (future inbound-CAT path, future contest module) without
  retrofitting wiring everywhere.
- FT8 dial-frequency config (`Ft8.DialFrequencyHz` per band, or a
  derived "use bridge's reported VFO-A") with the standard +1500Hz
  audio-offset convention applied to decoded freq_offsets.
- Decoded message DTO grows `band` and `dial_freq_hz` fields,
  populated at decode time from the latest bridge-reported state.
- Status endpoint surfaces the current rig context FT8 is decoding
  against (band, dial freq, mode — sanity check that operator is in
  USB and on a known FT8 channel).

#### Acceptance

```
# Daemon running with rig tuned to 14.074 MHz (20m FT8)
curl http://localhost:8080/v1/ft8/status
# Expected: band=20m, dial_freq_hz=14074000

# Change band on the rig to 40m FT8 (7.074 MHz)
curl http://localhost:8080/v1/ft8/status
# Expected: band=40m, dial_freq_hz=7074000 — no daemon restart needed

# Decodes from that point on carry the new band tag
```

---

### M4.4 — TX path (manual send)

**Status: 🚧 NOT STARTED. BLOCKED on inbound CAT command path.**

The bridge subsystem is read-only in v1 (ADR 0019). FT8 TX requires
the daemon to key PTT — which means writing to the rig over CAT —
which means the inbound CAT command path must exist. That path is
parked per ADR 0021's references; it unblocks both FT8 TX and any
future voice-keyer / contest-helper work.

**Goal:** Operator types an FT8 message, clicks send, the daemon
encodes it, transmits via the audio output, keys PTT for the slot
duration, releases. Manual send only — no auto-sequencing yet.

#### Scope (when unblocked)

- Inbound CAT command path lands first (separate work item; not part
  of M4.4). FT8 consumes the resulting CAT write surface.
- Implement FT8 message encoder + LDPC encoder + tone synthesis from
  encoded symbols, all per Taylor 2020 spec. Same licensing constraint
  as the decoder — no source-translation from the `.f90` files.
- Audio output device — same backend choice as M4.2's input; opened
  for playback at TX time.
- PTT key/unkey via the inbound CAT path. Driver-specific PTT command
  comes from the rigdef.
- TX slot scheduling: alternating RX/TX 15s slots, operator-configurable
  odd-or-even.
- Manual-send API: `POST /v1/ft8/tx` with `{"message":"CQ G4ABC IO91"}`,
  queues for the next available TX slot.
- Safety interlock: refuse TX if audio level is unconfigured, if power
  is above the operator's stated ceiling, or if the rig isn't on a
  known FT8 channel. Better to no-op than to splatter.

#### Acceptance

```
# Queue a manual TX
curl -X POST http://localhost:8080/v1/ft8/tx \
  -H 'Content-Type: application/json' \
  -d '{"message":"CQ G4ABC IO91"}'
# Expected: 202 Accepted, body indicates next TX slot

# Watch a second receiver (another FT8 station nearby, or a WebSDR):
# the CQ is heard cleanly at the right slot boundary
```

---

### M4.5 — QSO state machine (auto-sequencing)

**Status: 🚧 NOT STARTED.** Unblocks when M4.4 is green.

**Goal:** With auto-sequence enabled, the operator clicks a decoded CQ
and the FT8 stack runs the full exchange hands-free. Completed QSOs
land in the logbook via `qsoservice.Submit`. First "complete FT8
station" milestone.

#### Scope

- Implement the standard FT8 QSO state machine: CQ → reply → R+report →
  RR73 → 73, with operator-tunable timeout-and-retry behaviour. The
  state-machine shape is documented in the WSJT-X user docs and on-air
  practice (no Fortran source consulted — see the licensing constraint
  in the M4 design preamble).
- **Human-initiated TX is a protocol-level requirement.** Per the M4
  design preamble's *Protocol-level constraint: no robotic operation*
  (which derives from QEX paper Section 9), every QSO must begin with
  a human action — operator clicks Engage on a decoded callsign, or
  manually enters a TX message. Auto-sequencing within the QSO is
  fine (step CQ → reply → R+report → RR73 → 73 hands-free once
  engaged); auto-call-watch loops that initiate new QSOs without
  operator action are not. The UI surfaces an Engage button per
  decode, not a "work everything you can" toggle. This is not a UX
  preference — it's a condition on calling our implementation FT8.
- Per-QSO state object: callsign, locator, our report, their report,
  current state, last-heard-at timestamp.
- Auto-reply policy (operator config): `always` / `call-once-then-stop`
  / `never` (M4.4 manual-send is the `never` path).
- On QSO completion: synthesise `types.Qso` from the conversation —
  callsign, mode=FT8, band/freq from M4.3 context, our + their
  reports, start/end times, locator → grid — and submit via
  `qsoservice.Submit`. One-fails-all-fail applies: a DB failure means
  the QSO didn't happen and the operator sees an error.
- SSE event stream: `GET /v1/ft8/events` emits `ft8.decode`,
  `ft8.qso.started`, `ft8.qso.state-changed`, `ft8.qso.completed`,
  `ft8.tx.started`, `ft8.tx.completed`.

#### Acceptance

```
# Operator opens the FT8 panel in the SPA (M4.6, but the SSE stream
# is testable now via curl):
curl -N http://localhost:8080/v1/ft8/events
# Expected: live event stream

# Operator clicks a decoded CQ in the SPA (or POSTs an "engage"
# command in the interim), state machine runs the exchange, QSO
# appears via:
curl http://localhost:8080/v1/qso/{uuid}
# Expected: a complete FT8 QSO row in the logbook
```

A real on-air QSO worked hands-free, end-to-end, logged correctly.

---

### M4.6 — SPA panel

**Status: 🚧 NOT STARTED.** Unblocks when M4.5 is green (or in
parallel with M4.5 once `ft8.decode` events flow).

**Goal:** Operator runs an FT8 session entirely from the browser. The
SPA shows live decodes, lets the operator click a CQ to engage, shows
QSO-in-progress state, and surfaces decoded-callsign context (worked
before / new entity) the same way the country panel does for SSB/CW.

#### Scope

- New top-level FT8 surface — likely a sibling card to `LoggingCard`,
  routed via a tab or top-level mode toggle (decided in the M4.6
  design pass when the visual hierarchy is clearer).
- Live decode list: consumes `/v1/ft8/events`, displays
  `{time, snr, freq_offset, message}` rows. Auto-scrolling with a
  pause-on-hover affordance.
- Decoded-callsign coloring: new entity / new band / worked before,
  matching the country panel's existing convention.
- Click-to-call: clicking a decoded CQ engages the QSO state machine
  for that station (POSTs to a `/v1/ft8/engage` endpoint that wraps
  the M4.5 state machine).
- QSO-in-progress indicator: which station, current state, time in
  state, next expected transmission.
- TX queue display: what's queued, what's transmitting now, slot
  countdown.
- Per-panel sub-tabs if the surface grows: decodes / QSO / settings.

#### Acceptance

The operator works a full FT8 session — multiple QSOs, band changes,
manual TX experiments — without ever opening a terminal or touching
`smd` directly. The QSOs appear in the daemon logbook with the right
ADIF fields populated.

---

### What's NOT in Milestone 4 (per ADR 0021)

- **Other digital modes.** JT9, JT65, FT4, MSK144 are siblings in the
  WSJT-X codebase but out of scope for v1. The decoder architecture
  should make adding them straightforward later; don't pre-build for
  them now.
- **WSPR.** Different protocol, different tooling. Separate decision
  if ever needed.
- **Fox-and-hounds DXpedition mode.** Not a hobbyist-DXing concern at
  the operator's current operating profile.
- **Multi-rig FT8 / SO2R.** Single-rig v1 matches the rest of the
  daemon's current capability ceiling.
- **Contest auto-CQ macros.** Contest tooling is a separate future
  initiative; FT8 in v1 is general-purpose operating.
- **Decode-history persistence.** Logbook-app territory per the
  logging-vs-logbook scope memory.

### M4 acceptance test (whole milestone)

When M4.6 is shipped: the operator runs `smd` on the daily-driver
machine, opens the SPA in a browser, switches to the FT8 surface,
sees live decodes, works a station hands-free, and the QSO appears
in the logbook with band + mode + reports populated. No terminal, no
`jt9`, no WSJT-X side-by-side.

---

## Pre-dogfooding — getting v2 onto the operator's daily-driver machine

**Status:** Not a milestone in the formal sense — this is the
operational glue that lets the operator stop running v1 and start
running v2 day-to-day. Three stages:

- **Stage 1 — ADIF importer (`smd import <file.adi>`). SHIPPED 2026-05-14.**
  One-shot subcommand that brings the operator's 4233 historical QRZ
  Logbook QSOs into the v2 daemon via the canonical
  `qsoservice.Submit` path (validation + atomic write + audit table
  all inherited). Stamps the QRZ `qso_upload` row pre-success using
  the source ADIF's `app_qrzlog_logid` so future PATCH/DELETE
  forwarder flows can target the right upstream record. No HTTP
  endpoint — that would be permanent code surface for an operation
  that runs maybe 5 times in its lifetime. Live test confirmed
  4230 stored / 2 in-file duplicates / 1 source-data error (a single
  bad `rst_rcvd=4657` violating the daemon's length check) on
  ~5 seconds wall-clock.

- **Stage 2 — RPM packaging via `nfpm`. SHIPPED 2026-05-14.**
  Single-binary RPM (`station-manager-<ver>.x86_64.rpm`) containing
  `/usr/bin/smd` + `/usr/lib/systemd/user/smd.service` + a
  print-only postinstall scriptlet. Same package name as v1 so
  `dnf install` replaces the existing v1 install cleanly. Built via
  `scripts/release-rpm.sh <version>` (SPA build → static Go build →
  nfpm pack). Dramatically simpler than v1's RPM — no GTK/WebKit
  depends, no Wails binaries, no desktop/icon/XDG-menu files (the
  browser SPA is embedded in the daemon binary and served at
  `GET /`).

- **Stage 3 — Install day.** Next session in the operator's normal
  workflow. Sequence: backup `~/.local/share/station-manager/` →
  `dnf remove station-manager` (clears v1) → `dnf install build/release/station-manager-<ver>.x86_64.rpm`
  → `systemctl --user daemon-reload && systemctl --user enable --now smd`
  → `loginctl enable-linger "$USER"` → first-run setup at
  `http://127.0.0.1:8080` → `smd import ~/Downloads/qrz-export.adi`.

The reason this sits between Milestone 3 and SM Cloud rather than
inside any milestone is simple: it's not new product capability,
it's the path from "v2 is correct in tests" to "v2 is what runs on
the rig." Once Stage 3 completes, ongoing work proceeds against a
real daily-use logbook.

---

## SM Cloud — explicitly deferred

**Status:** Deferred per ADR 0016 (2026-05-06). NOT a milestone.

A hosted multi-tenant SM Cloud service (multi-user, multi-logbook,
browser-accessible, off-site backup) has long been on the operator's
mind but has zero current drivers. ADR 0016 forecloses speculative
Postgres / auth / multi-tenancy / public-API work in v1, and commits
two cheap-now schema prep items (globally-unique time-ordered QSO
IDs + edit/delete audit table) so that a future cloud build is a
forwarder-driver-shaped change rather than a data-migration
nightmare. See ADR 0016 for the full reasoning, the foreclosure
list, and the triggers that would reopen the question.

The two prep items are scheduled to land before milestone 3; they
are independently valuable for v1 (stable external QSO IDs +
"what did I change?" auditing) regardless of whether SM Cloud
ever materialises.

**Prep status (audited 2026-05-07):**

- **Prep #1 — globally-unique time-ordered QSO IDs: SHIPPED (2026-05-06).**
  UUIDv7 generated server-side at submit time, persisted on the
  `qso.uuid` column with a strict format CHECK, exposed as the
  canonical external identifier on every QSO API response, and
  carried through ADIF emission as `APP_SM_QSO_ID`. Path routing for
  `GET/PATCH/DELETE /v1/qso/{uuid}` and `GET /v1/qso/{uuid}/uploads`
  resolves UUID → internal int row id at the handler boundary; the
  internal storage shape is unchanged. SSE event payloads still
  carry the int `qso_id` (no live consumer yet) and grow `qso_uuid`
  when the SPA wires up event consumption — see api.md §4.5 known-
  gap note.
- **Prep #2 — qso_history append-only audit table: SHIPPED (2026-05-07).**
  Separate `qso_history` table added in `0001_init.up.sql` (migration
  amended in place — pre-production). Audit scope is `update` and
  `delete` only; INSERT is not audited because origin already lives in
  `qso.additional_data` per ADR 0014 prep #4. FK is by `qso_uuid`
  (not int PK) so audit rows survive any renumbering. `before_image`
  is the full `json.Marshal(types.Qso)` of the pre-mutation row,
  appended in the same transaction as the QSO mutation under one-
  fails-all-fail. Append-only is enforced by `BEFORE UPDATE` /
  `BEFORE DELETE` triggers on top of the daemon's never-mutate code
  path. New `internal/enums/source/` enum (`source.API = "api"`
  declared today; further constants added one at a time as new
  subsystems start mutating QSOs). Helpers:
  `sqlite.Service.InsertQsoHistoryTx` / `FetchQsoHistoryByUUIDWithContext`.
  DTO: `types.QsoHistory`. Operator-facing "show edit history for
  this QSO" SPA endpoint is **not** in scope for prep #2 — that's a
  separate UI task once the storage is in place.

---

## What "done" does NOT mean

Each milestone being "done" means the acceptance test passes and the
code is committed, tested, and documented. It does not mean:

- Performance-optimised. Personal-operator scale; optimise when
  measurements say to.
- Feature-frozen. Later milestones may refine earlier endpoints.
- Production-hardened. This is a personal project, not a shipped
  product. "Done" means "works reliably for the operator."

---

## Related documents

- `docs/v2-design/structure.md` — target layouts for milestones 1
  and 2.
- `docs/v2-design/api.md` — HTTP API design brief. Endpoint sketch
  maps to milestones here.
- `docs/v1-analysis/invariants.md` — constraints that apply to every
  milestone.
- `docs/v1-analysis/lessons-for-v2.md` — patterns to apply and
  avoid.
- `docs/session-handoff.md` — deferred features and their milestone
  targets.
