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

- `GET /v1/qso/:id` — fetch a single QSO
- `PATCH /v1/qso/:id` — edit a QSO
- `DELETE /v1/qso/:id` — soft-delete a QSO
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
- ✅ `GET /v1/qso/:id/uploads` — per-destination forwarding status.
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
`GET /v1/qso/:id/uploads`, and verify SSE events arrive on a
connected `curl` event stream.

---

## Milestone 2 — Browser SPA clients

**Status: 🚧 IN PROGRESS (as of 2026-05-03).** Original scope was
"Wails thin clients"; per ADR 0001 (2026-04-30) the client apps are
now browser SPAs (Svelte 5 + Vite + Tailwind v4) embedded in the
daemon binary via `//go:embed` and served by the daemon at `GET /`
when `Protocol=tcp && ServeSPA=true`. The original Wails-era scope
is preserved at the bottom of this section as the historical record.

The logging SPA (`frontend/logging/`) scaffold landed 2026-04-30 and
QSO-entry UX shipped through session 28 (2026-05-02): callsign /
mode / RST / VFOs / date-time / name / qth / comment / submit
controls all wired, ADIF formatter complete, tab-order indexed,
session timer + QSO timer running, station-profile store populated.
Session 30 (2026-05-03) wired submit to `POST /v1/qso` end-to-end
via `lib/api/qso.ts`, built the toast system per ADR 0008, added
the daemon HTTP access log, and logged the first real QSO through
the v2 stack (7Q5MLV @ 14.250 MHz USB).

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
  `<Toasts/>` mounted at `app.svelte`; top-right fixed; severity
  prefix; per-level TTL; click-to-dismiss; max-stack=5. Landed
  session 30, 2026-05-03.
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
  `QsoDraft` class with 9 form-bound `$state` fields, plain
  `qsoStarted` flag, `$derived` `defaultRst` / `canSubmit`, and
  methods `clear()` / `startQso()` / `tick()`. Reactivity audit
  locks the rule for future enrichment fields: form-bound (`bind:value`)
  ⇒ `$state`; submit-only (populated by `populateFromEnrichment`
  when ADR 0005's endpoint lands) ⇒ plain class field. RST
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
- ⏳ Daemon `GET /v1/enrich/callsign` — enrichment endpoint
  per ADR 0005. Unlocks F2 lookup-only path. Plus the SPA-side
  `qsoDraft.populateFromEnrichment(...)` method that the panel's
  `handleEnrich` will call on Tab when the wrapper lands.
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
  round-tripped through a `station` block on `/v1/config`).
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
- ⏳ `internal/bridge` package per ADR 0013 — daemon subsystem
  for `/v1/rig/events` SSE, rigctld-compat TCP, AUTO-mode CAT,
  PTT arbitration. Replaces the parked `cmd/logging/` Gio CAT
  loop.
- ⏳ Real `EventSource` consumer in `bridge.svelte.ts` — populates
  catState from SSE.
- ⏳ CAT-handover toast — toast plumbing exists; awaits the bridge
  so there's a transition to fire on (one `toasts.info(...)` call).
- ⏳ Keyboard shortcuts per ADR 0007 — F2 lookup-only, Ctrl+\\ VFO
  swap, Ctrl+Enter submit, ? help overlay.
- ⏳ Logbook and config SPAs — deferred. Single `frontend/logging/`
  bundle covers the operator's primary workflow; logbook /
  config become separate routes or separate SPAs when needed.
- 🗑 Text-file fallback — re-evaluated. The SPA is served BY the
  daemon (single-origin embed); SPA-side offline mode is
  incoherent (no daemon → no SPA load). Daemon-side QSO write
  resilience (atomic transaction, forwarder retry) covers the
  invariant. See `project_sm_spa_config_layering` memory.

### Acceptance test

Launch the daemon (`./smd` — no flag needed; first run seeds
`config.json` at the resolved working dir). Open
`http://localhost:<port>/` in a browser. Log a QSO through the SPA;
verify the resulting row in sqlite via `GET /v1/qso/{id}`. Verify
the upload-queue rows exist via `GET /v1/qso/{id}/uploads`. Verify
SSE events arrive on a separate `curl http://localhost:<port>/v1/events`
stream. Refresh the browser; verify the session timer state survives
(sessionStorage), the manual VFO state survives (localStorage), and
the daemon QSO list is unchanged. Stop the daemon; verify the SPA
shows a connection-status indicator when submit fails — today this
is a `toasts.error("Cannot reach the daemon — check it is running.")`
top-right toast (network-arm of `lib/api/qso.ts`'s `SubmitOutcome`).

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

## Milestone 3 — Bridges and external integration

**Goal:** External tools can feed QSOs into the daemon without going
through a SPA.

### Scope

- `cmd/udp-bridge` — generic UDP-to-daemon bridge. Listens on UDP
  for ADIF-formatted payloads and POSTs them to the daemon.
- `cmd/sm-serial-bridge` — the serial/CAT bridge with two frontends
  (rigctld-compat TCP + SM-native event stream). Rig control is a
  client concern, not a daemon concern.
- `cmd/importer` — ADIF bulk import CLI. Reads an ADIF file and
  submits each record via `POST /v1/qso`.
- Multi-rig awareness: `types.Qso` carries a rig identifier, the
  daemon API accepts it, the bridge populates it.

### Acceptance test

Start the daemon and the serial bridge. Tune the rig. Observe
frequency/mode updates on the bridge's event stream. Submit a QSO
via the logging app with rig state auto-populated. Import a
historical ADIF file via `cmd/importer` and verify all records appear
in the logbook.

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
