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

**What's left in M2 (as of 2026-05-14):**

- CAT-handover toast (one `toasts.info(...)` call; awaits the live
  bridge transition).
- Two deferred keyboard shortcuts: Ctrl+\ VFO swap, `?` help
  overlay. F2 lookup-only shipped 2026-05-15. Other ADR 0007
  shortcuts (ESC, Ctrl/Cmd+Enter, Tab→enrichment, Enter/Space on
  activatable elements) are shipped — see
  `docs/keyboard-shortcuts.md` for the live inventory.
- Logbook + config SPAs (deferred — not blocking on M2 closeout).

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
- ⏳ CAT-handover toast — toast plumbing exists; awaits a chosen
  trigger event from the bridge (likely first `rig-state` after a
  fresh subscribe, gated on a "haven't toasted yet this session"
  flag). One `toasts.info(...)` call when settled.
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
- ✅ Session email-out — `POST /v1/session/email-out` ships the
  in-memory session's ADIF to a recipient via the daemon's mailer
  (when `mailer.enabled`). Surfaced in SessionPanel.
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

**Goal:** Station Manager decodes and transmits FT8 in-process via the
`internal/ft8` subsystem. Decode parity with WSJT-X v.3.0.0.1 first;
layered improvements only after parity is established and provable.

The architecture, package boundaries, and reversal of the prior
out-of-tree extraction are settled in ADR 0021. This milestone breaks
that decision down into independently-exercisable slices, mirroring how
M3a sub-divided the bridge work.

### Design preamble

**Porting philosophy: exact port first.** The Fortran reference at
`/home/mveary/Development/wsjtx` is the specification. Each sub-
milestone ports a slice of that reference into Go (with CGO where
appropriate — see below) and proves parity against the WSJT-X
binaries before moving on. Departures from the reference are explicit
decisions with rationale captured in commit messages or follow-up
ADRs, never silent drift. Goal: same or better decode results than
WSJT-X under the same conditions.

**CGO commitment.** FT8's signal-processing budget inside a 15-second
slot is tight enough that pure-Go alternatives don't carry their weight
— prior research showed measurable wins from native FFT and LDPC
implementations that we can't ignore at this scale. The subsystem
binds:

- **FFTW3** for FFT (the WSJT-X reference uses it; pure-Go FFT
  libraries like `gonum/fft` benchmark slower under the access
  patterns the decoder hits)
- **LDPC(174,91) decoder** — either CGO-bound from a C port of
  WSJT-X's `bpdecode174_91.f90` or hand-ported to C. Decided in
  M4.1 once the porter has read both options end-to-end.
- **Audio backend (portaudio likely)** — decided in M4.2 when audio
  actually lands. ALSA-direct is a Linux-only fallback if portaudio
  proves painful.

Costs accepted (per ADR 0021): cross-compilation complexity, larger
binary, build-time dependency on the CGO toolchain. Caught early by
the CD pipeline (per `project_sm_cd_pipeline_planned` memory) — a CGO
break fails the gate immediately.

**Test oracle: `jt9 -8`.** Exact-port-first needs a falsifiable parity
claim. The strategy: a corpus of FT8 WAV files (bundled WSJT-X test
samples plus operator-recorded captures) lives at
`internal/ft8/testdata/`; CI runs both the SM decoder and `jt9 -8`
against the corpus and diffs the decoded message lines. M4.1 establishes
this gate; every subsequent milestone keeps it green.

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

- **Decode concurrency:** single-threaded faithful port at M4.1. Fan-
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

**Status: 🚧 NOT STARTED.** Subsystem scaffold landed 2026-05-16
(lifecycle, boundary tests, DI wiring); decoder work begins here.

**Goal:** Read an FT8 WAV file from disk, run the full decode pipeline
end-to-end, emit decoded `{message, snr, time_offset, freq_offset}`
records that match what WSJT-X's `jt9 -8` produces against the same
input. No audio capture, no rig, no TX, no storage, no SPA.

#### Scope

- CGO bindings: FFTW3 + LDPC(174,91) decoder. Vendor C sources or
  link against system libraries — decided in this milestone's first
  commit set after the porter has assessed cross-compile impact.
- Port `ft8_downsample.f90` — audio resample to baseband.
- Port coarse sync — search for FT8 sync tones across the 15s window.
- Port fine sync + frequency estimation.
- Port demod — soft-symbol generation.
- Port LDPC(174,91) decode + CRC14 check.
- Port message decoder — callsign1/callsign2/locator/report unpacking.
- CLI entrypoint: `smd ft8 decode <file.wav>` subcommand. Prints one
  decoded line per detected message, format matches `jt9 -8` closely
  enough for the diff gate.
- Test corpus at `internal/ft8/testdata/` — bundled WSJT-X test WAVs
  (license check first) plus a handful of operator-recorded slots
  covering clean / noisy / multi-station-collision cases.
- CI gate: `task ft8:parity` (or similar) runs SM decoder + `jt9 -8`
  over the corpus, diffs message lines, fails on mismatch beyond a
  documented floating-point tolerance.

#### Acceptance

```
# Decode a known WAV
./smd ft8 decode internal/ft8/testdata/clean-cq-iv3-band.wav
# Expected: same decoded messages jt9 -8 produces against the same file

# CI parity gate
task ft8:parity
# Expected: 0 mismatches across the bundled corpus
```

When the parity gate is green against the seed corpus, M4.1 is done.

---

### M4.2 — Continuous live audio + slot scheduling

**Status: 🚧 NOT STARTED.** Unblocks when M4.1 is green.

**Goal:** The daemon captures live audio, schedules decode windows
aligned to UTC 15-second boundaries, and feeds each window through
M4.1's pipeline. Decodes scroll continuously while the daemon runs.
Still no rig coordination, no TX.

#### Decisions to land in this milestone's design pass

- **Audio backend** — portaudio (CGO, matches WSJT-X) vs ALSA-direct
  (Linux-only, lighter). Default recommendation is portaudio; revisit
  only if cross-compile to non-Linux ever becomes a requirement.
- **Audio device enumeration + selection** — config schema and the
  startup-time check that the configured device exists and is
  capturing at the expected sample rate.

#### Scope

- Audio capture service: device enumeration, open device at decode
  sample rate, ring-buffer the incoming PCM.
- 15s slot scheduler: fires at UTC second boundaries (0, 15, 30, 45);
  hands the preceding slot's PCM to a decode worker.
- Bounded decode-worker pool via `internal/safego` (extend if needed
  per the ADR 0021 follow-up note). Single worker is fine if M4.1's
  decode latency comfortably fits under 15s on the operator's
  hardware; pool size becomes a config field once measurements demand
  it.
- Slot-offset metric: how late did the slot worker actually fire
  relative to UTC. Exposed via `GET /v1/ft8/status` (new endpoint —
  light, no SSE yet).
- Lifecycle: `Service.Start(ctx)` spawns capture + scheduler; `Stop()`
  drains pending decodes and closes the audio device.
- Config: `Ft8.AudioDevice`, `Ft8.SampleRate`, `Ft8.SlotOffsetWarnMs`
  (default 500).

#### Acceptance

```
# With the rig manually tuned to a busy FT8 frequency:
./smd                                              # daemon running, ft8.enabled=true
journalctl --user -u smd -f | grep ft8.decode      # decodes scroll live

# Slot timing health
curl http://localhost:8080/v1/ft8/status
# Expected: {"slot_offset_ms":42,"last_decode_count":7,...}
# slot_offset_ms stays under 500 for a clean run
```

Continuous decoding for a 10-minute window with no dropped slots, no
audio-buffer overruns, slot offset under 500ms throughout. Then done.

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
- Port FT8 message encoder: `genft8.f90`, `encode174_91.f90`,
  `gen_ft8wave.f90` (tone synthesis from the encoded symbols).
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

- Port the WSJT-X FT8 QSO state machine: CQ → reply → R+report →
  RR73 → 73, with the standard timeout-and-retry behaviour for missing
  responses.
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
