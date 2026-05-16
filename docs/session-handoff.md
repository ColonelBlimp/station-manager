# Station Manager — Session Handoff

**Purpose:** rolling handoff document across Claude sessions. Captures what was
done in the previous session, where the repo currently is, and **what the next
session should pick up**. Read this first when starting a session — it exists
precisely so we don't re-derive state or redo finished work.

**How to use this document:**

- **At session start:** read top-to-bottom. The "Current state" section tells
  you where the repo is. The "Next steps" section tells you what to do. If the
  next session's goals have already been set, work from them.
- **At session end:** the assistant updates this document before stopping.
  Move anything in "Next steps" that was completed into "What happened this
  session" with a date. Leave anything unfinished in "Next steps" and add new
  items discovered during the session.
- **Rolling window:** keep roughly the last 2–3 sessions of history in "What
  happened." Older entries can be summarized or elided — the long-form record
  lives in the git history, the v1-analysis docs, and the memory files.
- **Durable facts go in memory files,** not here. This document is for
  transitory session-to-session state. If something is stable across all
  future sessions (a project invariant, a user preference, a design rule),
  capture it in a memory file under `~/.claude/projects/.../memory/`.

---

## Current state (as of 2026-05-10, session 49 — **M3a.3 (bootstrap-on-SSE-open + bridge-error events + identity verification + multi-subscriber fan-out) shipped on top of M3a.2's pipeline.** Both rigdefs (yaesu-ft710, yaesu-ftdx10) updated to a clean INIT/READ split: `INIT = "AI1;"` (arms AUTO push state, silent on wire), `READ = "ID;FA;FB;ST;VS;MD0;MD1;PC;"` (8-query identity + state snapshot). Wire shape verified live on the FTdx10 via catcli's new `-read` flag — operator confirmed all 8 expected decodes flow through cat plus AUTO pushes on dial movement, with S-meter `RM*` lines correctly producing `[no match]` (validates the field filter for free). Bridge subsystem changes: `Service.activeClient` + `bootstrapBytes` mu-guarded fields + `TriggerBootstrap(ctx)` method that the SSE handler calls immediately after Subscribe; `runPipeline` pre-encodes both INIT and READ before `openClient` (fast-fail symmetry); pipeline-exit defer clears `activeClient` under mu BEFORE `Close()` (race-tightening); all log-and-bail paths now `publishBridgeError` before returning; `readLoop` does once-per-pipeline-lifecycle identity verification on the first IDENTITY status (fires `bridge-error` if status is empty = wrong driver, or value mapped but `≠ def.Model` = mismatch). Code-review pass against `docs/reviews/internal-bridge-pipeline.md` addressed all 10 findings: substantive (`SplitOverride bool → *bool` so split-OFF survives JSON omitempty; close/clear race ordering; INIT/READ encode-before-open; startup bridge-error visibility), polish (test sleep → poll, boundary test → `filepath.WalkDir`, explicit delimiter handling, `Stop`-before-`Start` flag, identity-verify scope doc), future affordance (debug-level decode log left for later). The startup-error visibility fix took two attempts: lazy-start (pipeline spawns on first Subscribe) was implemented end-to-end and 10x race-detector clean, then **reverted** because (a) `pipelineSpawned` was a one-shot latch — tab refresh after a startup-failure saw blank UI, no toast; (b) operator expectation is "daemon holds the rig from start" not "rig only acquired when SPA happens to be open." Final shape: eager-spawn at `Service.Start` + a `hub.lastBridgeError` cache that replays the most recent `EventBridgeError` to every new subscriber as their first event (per-Service-instance lifetime; never cleared within a Service; non-blocking send to fresh cap-64 channel can't drop). Multi-tab and tab-reload both see the toast; pinned by `TestHub_CachesBridgeErrorForLateSubscriber`. 37 bridge tests pass; race-detector hammer (`-count=10`) clean; both ADR 0013 boundary tests still green; `go vet` clean. Prior in-session work — 2026-05-10, session 49 (start) — **M3a.2 (Serial + CAT pipeline) shipped end-to-end.** The bridge subsystem now reads AUTO-mode rig pushes from the configured serial port, decodes via `internal/cat`, filters to the SPA-relevant field set, and publishes typed rig-state events through the existing hub. New `internal/bridge/pipeline.go` owns the orchestration: `cat.Lookup(driver)` → `buildSerialConfig(rigdef.Serial)` → `s.openClient(serialCfg)` → `cat.Encode(def, "INIT")` → write → `readLoop`. The read loop runs `client.ReadResponseBytes(ctxWithTimeout(30s))` → `cat.Decode(def, line)` → `mapStatusToPayload(status)` → `hub.publish`. `mapStatusToPayload` is the rigdef-tag → ADR 0019 SPA-payload filter (8 tags forwarded: `IDENTITY`, `VFOAFREQ`, `VFOBFREQ`, `MAINMODE`, `SUBMODE`, `SELECT`, `SPLIT`, `TXPWR`; anything else dropped — that's how "no waterfall, no S-meter" is enforced). Termination contract pinned: parent ctx cancel exits silently; 30s without a line publishes `rig-disconnected` once and keeps waiting (next successful read clears the `announcedDisconnect` flag); `ErrClosed`/EIO publishes `rig-disconnected` with reason and exits; initial open / lookup / INIT failures log-and-bail (M3a.3 promotes these to `bridge-error` events). Test seam: `Service.openClient` is an injectable `func(serial.Config)(serial.Client, error)` field defaulting to a thin `serial.Open` wrapper; in-package tests substitute `fakeSerial` via the new `installFakeSerial(s)` helper. New `fake_serial_test.go` implements `serial.Client` with `feedLine` / `recordedWrites` / `Close` — mu-guarded so feed-after-close can't panic. New `pipeline_test.go` (10 tests) covers INIT-sent (`AI1;ID;` first write), identity decode (FT-710 ID 0800 → `RigIdentity = "FT-710"`), freq decode (partial-payload contract — VfoA set, others zero), mode/split/VFO/power dispatch, 30s liveness timeout fires, dedup of consecutive disconnects, recovery after disconnect, terminal close → disconnect, unknown driver clean exit, open failure clean exit, rigdef→serial.Config translation pinned for FT-710. Existing tests reframed: `TestStart_Enabled_PublishesStubEvents` → `TestStart_Enabled_PublishesPipelineEvents`; `TestHTTPHandler_StreamsStubEvents` → `TestHTTPHandler_StreamsPipelineEvents`. `runStubEmitter` deleted; `stubEventInterval` package var removed. Build clean; full `go test -race ./...` green; race-detector hammer `go test -race -count=5 ./internal/bridge/` clean; both ADR 0013 boundary tests still green; `go vet` clean. Prior session — 2026-05-10, session 48 — CAT/bridge implementation kicked off. ADR 0019 (bridge subsystem v1 design) landed: stateless filter, read-only v1 (no PTT, no inbound command path), one SSE frontend for the SPA only, multi-rig API-aware but build-deferred, performance not a design risk. Cross-references stitched: ADR 0010's "Bridge-side current-state cache" subsection revised, ADR 0013 noted as complementary, `docs/v2-design/bridge.md` banner updated, `invariants.md` "Two-frontend bridge shape" qualified with ordering note (eventual two frontends, v1 ships one). `docs/v2-design/milestones.md` M3 restructured into M3a (bridge subsystem v1, four sub-milestones) + M3b (deferred external integration). Memory `project_sm_serial_bridge.md` rewritten. **M3a.1 — package skeleton + config + stub SSE — shipped.** New `internal/bridge/` package: `Service` lifecycle (Initialize → Start → Stop, all idempotent + nil-safe Enabled), bridge-internal hub (typed for `bridge.Event`, mirrors `events.Hub` shape), SSE handler at `GET /v1/rig/events` (registered conditionally on `bridge.Enabled()`, wrapped with `limitEventSubscribers` matching `/v1/events`), stub event emitter (replaced by real serial+CAT pipeline in M3a.2), three event types (`rig-state` / `rig-disconnected` / `bridge-error` per ADR 0010), three payload types. Config additions: `BridgeConfig` in `internal/types/`, `Bridge` field on `Config`, `Serial.Baud` defaults 38400, `validateBridge` enforces "Enabled=true → Port + Driver required" loudly at startup. DI wiring in `cmd/smd/main.go` constructs bridge service with `workerCtx`, Stop in shutdown sequence before `server.Shutdown`. **Package-boundary discipline enforced in tests** — two AST-walk tests assert (a) bridge MUST NOT import `internal/database/sqlite` / `internal/forwarding` / `internal/qsoservice`, and (b) no internal package outside an `{bridge, api}` allowlist may import bridge. CI catches violations loudly. 14 bridge tests pass; full Go suite green; daemon builds. **Code-review cleanup landed same session** — 7 findings from `docs/reviews/internal-bridge.md` all addressed: subscriber cap, marshal-failure logging, redundant logger param dropped, `Stop()` idempotency race fixed via `sync.Once` + `stopDone` channel, reverse boundary test generalised, doc.go real paths, confusing dual-success test path collapsed. Race-detector hammer clean on the new `stopOnce` path. Prior session — 2026-05-09, session 47 — ESLint + Prettier wired up across the SPA. Type-checked TS rules turned on at the outset (operator's call: catch subtle bugs now rather than later); 29 lint errors surfaced across 8 files and were all fixed in-session, no carry-over warnings. Net wiring: deleted the legacy `.eslintrc.cjs` (v8 format with stale Wails-era `src/lib/wailsjs/**` ignore that the milestone-1 restructure made dead); rewrote flat `eslint.config.js` to use `...ts.configs.recommendedTypeChecked` (verified — `recommendedTypeChecked` is a 3-config array, spread required), `_var`/`_arg` ignore pattern for unused-vars, `eslint-config-prettier` last so prettier owns formatting and eslint owns logic, dropped the dead `includeIgnoreFile` ref to a non-existent local `.gitignore` (frontend/logging has no local .gitignore — repo-root one is canonical); `.prettierrc` now declares the `prettier-plugin-svelte` plugin with the expected `*.svelte` parser override. Installed: `prettier`, `prettier-plugin-svelte`, `eslint`, `@eslint/js`, `typescript-eslint`, `eslint-plugin-svelte`, `eslint-config-prettier`, `globals` (98 transitive deps; 0 vulnerabilities). New scripts: `lint`, `lint:fix`, `format`, `format:check`. One-time prettier `--write` ran across the entire `src/` tree as agreed (~209 files reformatted) — the noisy diff is the price of getting the codebase normalised once. Lint fixes worth flagging: `enrichment.ts` had `'hamnut' | 'cache' | 'none' | string` unions that collapse to `string` (lint catches it as `no-redundant-type-constituents`) — tightened `country_source` to a strict 3-literal union since the daemon controls the values exhaustively, kept `station_source: string` (provider names are open-ended; doc-comment lists known values). `qso.test.ts` had four `vi.fn(async () => x)` mock-fetch patterns flagged by `require-await` — converted to `vi.fn(() => Promise.resolve(x))` / `Promise.reject(new TypeError(...))`; the async keyword was load-bearing-looking but actually unused (no awaits inside). `Vfos.svelte` flagged `unsafe-assignment` on `manualState.vfoA = hz` even though `VfoInput.onCommit` is typed `(hz: number) => void` — turns out svelte-eslint-parser doesn't propagate prop types through inline snippet callbacks (known parser gap), worked around with explicit `(hz: number)` annotation on the inline arrow. `toasts.svelte.ts` flagged `prefer-svelte-reactivity` on the module-level `timers: Map<…>` — added a why-comment + line-disable: `timers` is internal `clearTimeout` plumbing, never read from a template/$derived/$effect, so SvelteMap would add unused reactivity machinery. `QsoPanel.svelte:46` flagged `prefer-writable-derived` on `let mode = $state(displayedState.mode)` + mirror-`$effect` — kept the pattern with extended why-comment + line-disable: a plain `$derived` is read-only and would break `<Mode bind:value={mode}>`; the two-effect shape (input mirror + conditional output mirror) is the standard Svelte 5 idiom for a two-way bind across two reactive stores under the ADR 0009 "writes only when editable" gate. 16 trivial issues (unnecessary type assertions on `.toBe(true)` etc.) auto-fixed by `lint:fix`. Final state: 0 lint errors, 0 prettier diffs, svelte-check 0 errors / 222 files, vitest 376 passed across 21 files. Convention going forward: when a lint-disable is warranted, the reason gets a comment above the disable; never a bare disable. Prior session — 2026-05-08, session 46 — Details panel UI + daemon-side zone validation polish + WorkedPanel UI all shipped. WorkedPanel is the third InfoPanel tab to come online (after Details and the country panel from session 45). It's the simplest of the three: the daemon endpoint `GET /v1/contact-history?call=X` already existed (returns `{ items: ContactHistory[] }` newest-first, capped at `Server.MaxContactHistoryResults`); SPA-side just needed wiring. New `lib/api/contact-history.ts` (discriminated-outcome fetch wrapper matching `lib/api/qso.ts`), `lib/states/contactHistory.svelte.ts` (singleton with `items: ContactHistory[] | null` $state and `count: number` $derived — null distinguishes "no Tab yet" from `[]` "fetched, none found"), `WorkedPanel.svelte` (table with Date/Time/Band/Mode/RST-Sent/RST-Rcvd, `tabular-nums` for digit alignment, three-state render: hidden when null, "No prior contacts" hint when [], table when populated). InfoPanel's `tabs` array is now `$derived` so the `Worked (N)` badge auto-updates from `contactHistoryState.count`. The fetch fires in parallel with enrichment in `QsoPanel.handleEnrich` (cheap indexed query, no upstream); failures silent because empty-history and network-error are the same operator-visible outcome. Clear hooks (submit-stored + Clear button) reset both `enrichmentState` and `contactHistoryState`. 4 new tests on the state holder. Daemon-side LoggingStation zone validation polish. Validation: `MyCqZone` (1–40), `MyITUZone` (1–90), `MyDXCC` (0–522, with 0 = "None"/maritime per ARRL) now rejected with `400 invalid_field_value` at PUT `/v1/config` when malformed. Empty stays empty. Whitespace trimmed before parsing. Catches non-numeric (`"37x"`), out-of-range (`"41"`, `"523"`), negative, fractional. Operator's existing config still loads; new validation only fires on PUT — backward-compatible. New `isValidZone(s, minVal, maxVal)` helper in `internal/api/validation.go`. ~20 new test cases. The 522 cap on DXCC is the current ARRL maximum at time of writing — bump when ARRL adds a new entity (rare; once every few years; comment in handler flags this). Details panel UI shipped (the second InfoPanel tab to feed from enrichment after the country panel). Two-column layout: left = operator-typed Power (`RX_PWR`, digit-strip on input), Rig (`RIG`, contacted's working conditions, free text), Notes (`NOTES`, distinct from `COMMENT` per ADIF spec — NOTES = operator's private record, COMMENT = things to share during the QSO), Request QSL (custom flag, emits `APP_SM_REQUEST_QSL='Y'` only when true to keep the additional_data blob lean for the common case). Right = read-only displays from `enrichmentState`: Email (`station.email`), Web Site (`station.web`) with an external-link launcher button, "Lookup on QRZ.com" link built from current callsign (`https://www.qrz.com/db/<call>`, opens new tab, gated on non-empty callsign), CQ Zone + ITU Zone label:value pairs (`country.cq_zone` / `country.itu_zone`). Read-only fields use `<input readonly tabindex={-1}>` + `bg-surface-disabled` so they look like fields per the operator's mockup but skip the focus chain. Four new fields on `qsoDraft` (`rxPwr` / `rig` / `notes` / `requestQsl`) cleared by `clear()`; four new ADIF emitters in `adif.ts` (omit-when-empty / omit-when-false). `EnrichmentStation` type extended with `web` / `lat` / `lon` (these were already on the daemon's `types.ContactedStation`; SPA type just hadn't surfaced them yet). 6 new tests in `adif.test.ts`. Country panel UI shipped end-to-end session 45. Daemon populates `country.is_new_entity` (orchestrator queries the QSO log for any prior contact with the same DXCC; `HasQsoForCountryWithContext` on sqlite service uses `Exists()` over the indexed `qso.country` column with sqlboiler's default soft-delete filter). SPA's new `lib/states/enrichment.svelte.ts` holds the latest result + path selection; `lib/ui/panels/CountryPanel.svelte` displays the country name (with `*` for new DXCC), bundled `flag-icons` SVG flag (160×120 via `w-40 aspect-[4/3]`), short/long-path distance + bearing pair (active path highlighted in `text-indigo-700`), short/long radio selector, and local time with offset suffix. Path selection drives ADIF `ANT_AZ` on submit; `enrichmentState.clear()` fires after stored AND when Clear is pressed so each QSO starts fresh. Sticky info-toast `Looking up <CALL>...` appears after a 500ms threshold (cache hits never see it; slow internet gets feedback) and dismisses on response. Outer card always renders for stable layout; inner content hidden via Tailwind `hidden` when `result === null`. flag-icons CSS is imported into the `components` cascade layer so Tailwind's `utilities`-layer width/aspect classes win the cascade — without the layer, unlayered flag-icons rules pinned the flag at ~21px regardless of overrides. Live testing surfaced four daemon/SPA polish items, all fixed: (1) orchestrator now recomputes `LocalTime` from the persisted `TimeOffset` on every Enrich return path so cache hits and cold misses produce the same shape — pre-fix cached country rows returned empty `local_time`, and cold-miss returned hamnut's wire-time string; (2) `ValidatedInput` gained a `transform` prop so RST sent/rcvd strip non-digits as the operator types ("5A9" lands as "59" not "5A9"-with-red-border); (3) RST manual-clear refill effects removed — they were resetting the field to default the moment the last digit was deleted, blocking the natural delete-right-to-left edit flow (mode-flip refill kept, since that's the only case operators actually want); (4) daemon now self-heals at startup when config marks `setup_complete=true` and points at a `default_logbook_id` that has no DB row (most commonly: operator nuked dev DB without clearing config) — `ensureDefaultLogbook` seeds from `LoggingStation.StationCallsign` and persists a corrected ID back to config if AUTOINCREMENT lands a different value; non-fatal on persist failure (in-memory correction still works for the session). All fixes have regression tests under `-race`. Code-review Major findings (M1–M5) closed in session 42, Minor findings (m1–m8) closed in session 43. Carry-over from session 41: ADR 0017 SHIPPED end-to-end. The enrichment pipeline (callsign/country lookup) is fully wired daemon-side: schema migration, provider abstraction, hamnut + QRZ providers ported from v1, orchestrator with three-state read policy + always-merge filter→hamnut, bounded async-refresh worker, operator config schema, HTTP handler `GET /v1/enrich/callsign`, contacted_station upsert in `qsoservice.Submit`, integration tests with real providers against httptest upstreams. ADR 0017 supersedes ADR 0005 (the model is fundamentally different — domain tables as cache, hamnut as country source-of-truth, sequential callsign-class chain, always-merge at return). SPA wiring (`lib/enrichment.svelte.ts`, `Callsign.onenrich`, country-panel UI) is deferred to a separate session per the option-2 MVP scope. Tasks #55–#65 all closed. Carry-over from session 40: ADR 0016 prep #2 SHIPPED (qso_history append-only audit table). New `qso_history` table appended in-place to `0001_init.up.sql` (pre-production); audit scope is UPDATE + DELETE only, INSERT not audited because origin already lives in `additional_data` per ADR 0014 prep #4. FK by `qso_uuid` (canonical external identifier per prep #1, survives any renumbering). `before_image` is the full `json.Marshal(types.Qso)` snapshot of the pre-mutation row, not a diff (negligible storage at personal-operator scale, trivial to replay). Append-only enforced both by code path (daemon never UPDATEs/DELETEs the table) and by `BEFORE UPDATE`/`BEFORE DELETE` triggers (`RAISE(ABORT, 'qso_history is append-only…')`) — belt-and-braces against manual sqlite3 sessions. The audit-row insert shares the QSO mutation's transaction under one-fails-all-fail. New `internal/enums/source/` enum mirroring `internal/enums/upload/action/` shape; `source.API = "api"` declared today (PATCH/DELETE on `/v1/qso/{uuid}`); future sources added one constant at a time. `qsoservice.Update` gained a `src source.Source` param; `qsoservice.Delete` reshaped from `Delete(ctx, id int64)` to `Delete(ctx, existing types.Qso, src source.Source)` so the handler-fetched snapshot is reused (no second DB round-trip; snapshot guaranteed to match what the operator was looking at). New helpers: `sqlite.Service.InsertQsoHistoryTx` (write), `FetchQsoHistoryByUUIDWithContext` (read, ordered `at ASC, id ASC`). New DTO: `types.QsoHistory` with raw `BeforeImage []byte` so consumers either deserialize into `types.Qso` or forward intact to SM Cloud sync. Tests cover happy path, append-only triggers, multi-edit accumulation, op/source/image guards. Full suite green. **No operator-facing endpoint shipped** — only the storage + write hooks; "show edit history for this QSO" SPA endpoint is a separate future task. Carry-over from session 38: prep #1 UUIDv7 shipped, all QSO-keyed paths route by UUID; SSE event payloads still carry only int `qso_id` (no live consumer yet — `bridge.svelte.ts` stubbed) and grow `qso_uuid` when the SPA wires up event consumption — known wire-shape gap documented in api.md §4.5. Carry-over from sessions 34-36: station-store migration + ADIF MY_* end-to-end SHIPPED, My Station sub-tabs + Update button + QSO sub-tab + CAT-off default power + notification toggles all live; `formatAdifRecord` emits all MY_* + `OPERATOR` + `OWNER_CALLSIGN` + `ANT_AZ` + `APP_SM_QSO_ID` with stable order. Bearing utility (`lib/utils/bearing.ts`) ready for country-panel hookup.

### Session 66 (2026-05-16) — Bridge auto-recovery: supervisor + probe + flash suppression (ADR 0020)

End-to-end bridge auto-recovery shipped and live-tested on the FTdx10 across both directions (cold boot with rig off, then on; mid-session rig power-cycle). Operator can now start the daemon before the rig and switch the rig on at any later point; brief power blips ≤1s produce zero SPA noise; 10s+ rig-off cycles surface a sticky disconnect toast that's automatically replaced by a positive "Rig reconnected." info toast on recovery.

**Daemon side (`internal/bridge/`):**

- `runSupervisor(ctx)` wraps `runPipeline(ctx)`. `runPipeline` now returns a classified `pipelineExitClass` (`exitPermanent` / `exitTransient` / `exitContextCancelled`); supervisor retries transient with exponential backoff (1s start, 30s cap), exits silently on cancel, exits with one bridge-error on permanent. Toast dedup across retries via `Service.lastPublishedExitKey` — same error code only publishes once per failure cycle; supervisor clears the key after a 10s steady-state pipeline run.
- `runPipeline` sends READ (`ID;FA;FB;ST;VS;MD0;MD1;PC;`) right after INIT (`AI1;`) on every cycle — so supervisor-driven restarts pull a fresh snapshot immediately rather than waiting for the operator to wiggle a control.
- `readLoop` no-data branch re-issues INIT+READ as a **probe** on each liveness timeout. Handles the case where the FTdx10 family's USB-serial layer keeps `/dev/ttyUSBn` open across rig power-cycle (port stays open, reads go silent, no terminal-error fires). Probe lands on the now-alive rig and triggers recovery without any separate kernel signal.
- **Liveness default lowered from 30s to 5s.** The probe makes false-positives during legitimate idle self-recovering (the READ pulls a snapshot, `rig-state` flows, dedup clears).
- **Timeouts promoted to operator config** under `bridge.timeouts.{liveness_ms, backoff_initial_ms, backoff_max_ms, steady_state_threshold_ms}` (all optional; zero = built-in default). Validation 50ms–1h, initial ≤ max. Service snapshots at `New` via `resolveTimeout(cfgMs, packageDefault)`; test pattern (dialing package vars before `New`) preserved.

**SPA side (`frontend/logging/src/lib/states/bridge.svelte.ts`):**

- `rig-disconnected` handler scheduling: warn-toast push goes through `setTimeout(..., FLASH_SUPPRESS_MS)` (currently 800ms) rather than calling `toasts.warn` immediately. Three-state machine (idle → scheduled → visible) with full transition table in ADR 0020's second revision.
- If `rig-state` arrives within the 800ms window, the timer is cancelled AND the reconnect-info push is skipped — no UI churn at all for short blips.
- If the window elapses, the warn pushes sticky (`ttl=0`) — operator sees the disconnect for as long as it lasts (default 6s TTL was too short for real outages). Recovery dismisses the sticky warn and pushes `bridge.reconnected` info toast.
- New i18n key `bridge.reconnected` in `lib/i18n/en.ts`.

**Documentation:**

- ADR 0020 (`docs/decisions/0020-bridge-pipeline-supervisor.md`) — full ADR for the supervisor decision; two revision sections appended in session 66 covering the timeouts-to-config promotion and the SPA-side flash suppression (with three-state machine table + alternatives weighed).
- ADR 0019 — revision note narrowing the "no persistent state across daemon restart" cost.
- `docs/v2-design/bridge.md` §8.5 — moved from open question to "Resolved (2026-05-16)" pointing at ADR 0020.
- CLAUDE.md M3a paragraph extended with supervisor + probe + sticky toast + flash suppression details.
- Memory `project_sm_serial_bridge.md` — three new bullets (supervisor, timeouts-to-config, flash suppression).
- Memory `MEMORY.md` — bridge hook line updated.

**Tests added/updated:**

Go side (`internal/bridge/`, `internal/config/`):
- `TestSupervisor_RetriesUntilOpenSucceeds` — flavour 2 (port absent at boot)
- `TestSupervisor_ReopensAfterTerminalReadError` — mid-session cable yank
- `TestSupervisor_PermanentErrorDoesNotRetry` — unknown driver
- `TestSupervisor_DedupesIdenticalOpenFailureAcrossRetries`
- `TestReadLoop_ProbeReissuesINITAndREADOnNoData` — drives the no-data branch
- `TestServiceNew_SnapshotsConfiguredTimeouts` — config-override wiring
- `TestValidateBridge_TimeoutRangeChecks` — validation bounds
- Existing `TestPipeline_TriggerBootstrap_WritesReadCommand` updated to expect 3 writes (INIT + startup-READ + bootstrap-READ) so it actually exercises the TriggerBootstrap path

SPA side (`bridge.test.ts`):
- Existing rig-disconnected tests updated for the `vi.useFakeTimers()` + `vi.advanceTimersByTime(800)` pattern
- `SUPPRESSES the flash when rig-state arrives within the suppression window` — proves the cancel path
- `latest disconnect wins when a new disconnect replaces a pending one`
- `handles disconnect → reconnect → disconnect → reconnect cycles`

All tests green under race detector (Go side) and full Vitest suite (SPA side). Lint + format clean.

**Live verification on FTdx10:** Cold boot (daemon up, rig off, then on) → supervisor catches first-boot, INIT+READ lands on alive rig, SPA populates. Mid-session rig off 10s+ → sticky warn appears at ~10.8s, persists, replaced by info on recovery. Rig off ~1s (simulated power blip) → zero SPA toasts, daemon recovers silently.

### Session 49 continuation (2026-05-10) — M3a.3 + code-review pass + eager-start with hub-cached bridge-error

After M3a.2 wrapped, M3a.3 closed out the bridge subsystem v1's wire side: bootstrap-on-SSE-open, bridge-error events for operator-actionable failures, identity verification, multi-subscriber fan-out test. Then a code-review pass against `docs/reviews/internal-bridge-pipeline.md` produced 10 findings; all addressed in the same session. The startup-bridge-error-visibility finding (review #2) took two design attempts before landing.

**Wire-shape changes (validated live on FTdx10 first via catcli):**

- Both rigdefs (`internal/cat/rigs/yaesu-ft710.json` + `yaesu-ftdx10.json`):
  - `INIT` → `AI1;` (arms AUTO push state, silent on wire — no response expected)
  - `READ` → `ID;FA;FB;ST;VS;MD0;MD1;PC;` (8-query identity + state snapshot)
- Rationale (B1 from the in-session design discussion): single command name (`READ`) for snapshot; clean intent split between `INIT` (arm AUTO mode) and `READ` (fetch identity + state); zero new cat-side parser work. `cat.Encode` fixtures + commentary updated; no in-code callers existed yet so the wire-shape change was free.
- `cmd/catcli/main.go` gained `-read` flag as a parallel companion to `-init`. Live-test command:
  ```
  /tmp/catcli -device /dev/ttyUSB0 -rig yaesu-ftdx10 -init -read -listen
  ```
  Operator verified on the real FTdx10 — all 8 expected decodes flowed (`ID0761 → FTdx10`, FA/FB freqs at 28.072/28.074 MHz on FT8, DATA-U/USB modes, split off, VFO-A, 50W) plus AUTO pushes on dial movement. `RM*` (S-meter) lines correctly produced `[no match]` from cat — validates the field filter (mapStatusToPayload) for free, since the bridge's `readLoop` skips `ErrNoMatch` silently.

**Bridge subsystem changes (M3a.3):**

- `Service.activeClient serial.Client` + `Service.bootstrapBytes []byte` mu-guarded fields. Pipeline stashes them after the open succeeds; defer clears them on exit (under mu, BEFORE `client.Close()` — close/clear race tightening from review #3).
- `Service.TriggerBootstrap(ctx context.Context) error` — writes the pre-encoded `READ` via `activeClient`. Safe-by-design no-op when the pipeline isn't running. Errors don't break the SSE connection.
- `runPipeline` pre-encodes both `INIT` and `READ` BEFORE `s.openClient` (review #4 fast-fail symmetry — a missing-INIT or missing-READ rigdef fails without acquiring the port).
- All log-and-bail paths in `runPipeline` (unknown driver, bad serial config, missing INIT, missing READ, openClient failure, INIT write failure) now `publishBridgeError(reason)` before returning. Pre-fix these were log-only.
- `readLoop` runs identity verification once per pipeline lifecycle on the first IDENTITY status arrival. Fires `bridge-error` if `status["IDENTITY"]` is empty (rig responded with an ID code the configured rigdef doesn't recognise — operator wired the wrong driver) or if the mapped value doesn't equal `def.Model` (semantic mismatch). Dedup via local `identityVerified` flag.
- `internal/bridge/handler.go` SSE handler calls `s.TriggerBootstrap(r.Context())` immediately after `Subscribe`; logs at warn on error.

**Code review (`docs/reviews/internal-bridge-pipeline.md`, 10 findings, all addressed):**

Substantive (4):

- **#1 SplitOverride bool → *bool.** `omitempty` collapsed `false` with "not pushed", so a rig push of `ST0` (split OFF) was silently dropped on the wire. Pointer makes "OFF pushed" vs "not pushed this frame" distinguishable. Other fields (`VfoA`, `VfoB`, `Power`) keep `omitempty` since 0 is never a legitimate rig value. Pinned by `TestPipeline_DecodesSplitOff`.
- **#2 Startup bridge-error visibility — TWO ATTEMPTS.** The original implementation had `runPipeline` spawn at `Service.Start`, which fired bridge-errors to zero subscribers (no SPA tab open yet). Operator with a typo'd `bridge.cat.driver` saw blank UI, no toast — only the daemon log mentioned the failure.
  - **Attempt 1 — lazy-start.** Implemented end-to-end: pipeline spawns on first Subscribe so the triggering subscriber catches startup errors. New fields `parentCtx`, `pipelineSpawned`, `pipelineReady chan struct{}`; new `ensurePipeline()` method called by Subscribe; `TriggerBootstrap` waits on `pipelineReady` with a 2s timeout. `Subscribe()` reordered to register on hub BEFORE calling `ensurePipeline` to close a startup-error race window. Existing tests updated to add Subscribe-after-Start; new `TestPipeline_LazyStart_StartupErrorReachesFirstSubscriber` regression test. 10x race-detector hammer clean. **Then reverted** — operator review surfaced two real downsides:
    - `pipelineSpawned` is a one-shot latch. After a pipeline-fail-and-exit, the next Subscribe sees the latch already set and never re-spawns. Tab refresh / second tab opening sees blank UI and no toast for an error the first tab DID see. Real UX gap.
    - Operator expectation: "smd daemon is running, rig is connected." Lazy port acquisition (port held only while a SPA tab is open) is backwards for the daemon-owns-the-rig mental model.
  - **Attempt 2 — eager-start + hub-cached bridge-error.** Reverted lazy-start machinery. Added `hub.lastBridgeError *Event` field; `hub.publish` caches every `EventBridgeError` (overwriting previous, never cleared within a Service's lifetime — fresh hub per daemon restart); `hub.subscribe` replays the cached event to every new subscriber as their first message via a non-blocking send to the fresh cap-64 channel (can't drop). Multi-tab and tab-reload both see the toast. Net code change vs M3a.3-as-shipped: ~30 lines in `hub.go`, no Service complexity. Replaced the lazy-start regression test with `TestHub_CachesBridgeErrorForLateSubscriber` (sleeps to let the eager-spawn goroutine fail before subscribing, asserts two consecutive late subscribers both see the cached event).
- **#3 TriggerBootstrap close/clear race.** `runPipeline`-exit defer reordered: clear `activeClient` under mu BEFORE `client.Close()`. "If a TriggerBootstrap captured non-nil cl, the underlying port is still open" is now enforceable by ordering rather than incidental.
- **#4 INIT/READ encode-before-open.** Both `cat.Encode` calls moved before `s.openClient`. A missing-INIT or missing-READ rigdef now fails fast without acquiring the port.

Polish (5):

- **#5** `TestPipeline_TerminalSerialErrorEmitsDisconnect`: `time.Sleep(20ms)` → poll on `len(fake.recordedWrites()) >= 1`. Survives loaded-CI scheduling.
- **#6** Boundary test reverse-direction: replaced 3-loop manual recursion with `filepath.WalkDir`. Unbounded depth, half the LOC.
- **#7** `delimiterFromString`: empty string now returns an error rather than relying on `serial.newPort`'s private `0 → '\r'` fallback (cross-package contract was leaky).
- **#8** `Stop`-before-`Start`: new `Service.stopped` flag set inside the `stopOnce` body. A subsequent `Start` returns silently rather than spinning up a pipeline whose hub is already closed. Pinned by `TestStart_AfterStop_NoOps`.
- **#9** Identity-verify scope: doc-comment now states verification is per-pipeline-instance, not per-physical-rig (cable yank → terminal serial error → pipeline exits → daemon restart is the recovery path).

Future affordance (1):

- **#10** Debug-level decode log left for later — silent-skip behaviour matches the codec doc.

**Test fan-out:**

- `pipeline_test.go`: 11 new tests (TriggerBootstrap-writes-READ, TriggerBootstrap-no-op-when-pipeline-not-running, BridgeError-UnknownDriver, BridgeError-OpenFailure, IdentityVerified-NoErrorOnMatch, IdentityVerified-ErrorOnUnrecognised, IdentityVerified-OnceOnly, DecodesSplitOff, plus `TestHub_CachesBridgeErrorForLateSubscriber`). Lazy-start regression test removed when lazy-start was reverted.
- `handler_test.go`: `BootstrapFiresOnSubscribe` (asserts INIT-then-READ writes around an HTTP GET) + `FanOutToMultipleSubscribers` (5 concurrent SSE clients all see a broadcast frequency push). Pre-existing race in `TestHTTPHandler_StreamsPipelineEvents` and `ShutdownChClosesStream` (handler subscribes AFTER `Do()` returns; `feedLine` could race ahead and publish to zero subscribers) fixed by waiting for INIT+bootstrap-READ writes before feeding test events.
- `service_test.go`: `TestStart_AfterStop_NoOps` (stop-before-start fix from polish #8).

**Verification:**

- `go build ./...` clean.
- `go test -race ./...` all packages green.
- `go test -race -count=10 ./internal/bridge/` clean (~9s; no flakes after the Subscribe-vs-Do race fixes).
- `go vet ./...` clean.
- 37 bridge tests pass; both ADR 0013 boundary tests (forward + reverse) still green.

**Rig-specific mode → ADIF translation SHIPPED (session 51, 2026-05-11) — the M3a.4 follow-up that was parked is now closed.** See the Session 51 subsection below for the full work record. The bridge stays a pure pass-through (rig literal on the wire); the SPA resolves to ADIF (MODE, SUBMODE) pairs via per-rig mappings stored in two layers (rigdef-shipped defaults + operator overrides in config.json), merged daemon-side at `/v1/config` GET time. New My Station → Mode Mappings sub-tab edits the override layer. **Bonus:** the daemon's `internal/enums/modes` enum became data-driven via an embedded `adif-modes.json` catalogue + optional `$SM_WORKING_DIR/modes.json` operator override, so future ADIF spec growth doesn't strictly require a daemon binary release. The natural next picks-up point is **continuing live operator testing on the FTdx10** — confirm the Mode Mappings panel renders correctly with the operator's config, FT8 QSOs log with the correct ADIF MODE=FT8, the default mappings cover the operator's common modes.

**`cmd/smd` code-review cleanup SHIPPED (session 52, 2026-05-12).** Focused re-review at `docs/reviews/cmd-smd-2026-05-12.md` produced 0 critical, 3 major, 4 medium, 5 minor findings — all closed in three commits over the session. Only behavioural change: bridge `Initialize`/`Start` failures now return wrapped errors instead of `os.Exit(1)`, so deferred cleanup (DB close, logger close, refresher stop, worker drain) actually runs on bridge-startup failure. Everything else is reader-correctness / hygiene — swapped doc-comments untangled, `doc.go` rewritten against ADR 0001 + ADR 0013 + ADR 0017, `defaultConfigPath()` helper extracted, lifecycle-shape variance documented in `run()`'s doc, named returns on `loadConfig`, process-lifetime globals annotated.

**`frontend/logging` code review — 5/5 critical + 14/17 important closed (session 53 → 54, 2026-05-12 → 2026-05-13; IN PROGRESS, only nits remain).** Multi-agent review at `docs/reviews/frontend-logging-2026-05-12.md` produced 5 critical, 17 important, 11 nit findings. Closed: every critical (real wire-protocol UTF-8 bug, overlay focus trap + ESC contract, Mode tabindex regression, mode-mapping clobber via untrack), the I7/I8/I12 API/UI bug cluster, and the a11y cluster (I13/I14/I15/I16/I18/I19/I20). Quick batch (I11/I3/I9 + validator-parity comments) also landed: Callsign max bumped 20→32 to match daemon, hydration-writeback double-fire suppressed via per-effect first-run latch in `qsoDefaults` + `manual`, `EnrichmentStation` index signature dropped (typos like `station.gridsqure` now compile-error). **Outstanding:** architectural sweep — I2 (`_disposeForTests`), I5 (AbortController helper), I6 (`isShape<T>` boundary guard); reviewer expects this as a single coherent commit covering the three systemic diagnoses. I1 pushed back (reviewer didn't see the session-47 prior decision on the QsoPanel mode-mirror pattern). I4 + I10 declined per reviewer's own "acceptable as-is" / "dead today" notes. I17 (color-only invalid signal) deferred as its own commit — requires refactoring validators from `boolean` → `string | null` across `callsign.ts`, `maidenhead.ts`, `zone.ts` and extending `ValidatedInput`. All 11 nits left for polish. Test surface grew from 478 to 498 cases (focus-trap, ESC stopImmediatePropagation, Mode-tabindex roving, mode-mapping clobber regression, tablist keyboard nav, Callsign length boundaries). Two operator-visible behavioural changes shipped after explicit confirmation: Toasts dismiss is X-button-only (was whole-toast click) and SessionPanel edit affordance is a trailing-cell Edit button (was whole-row click) — both flagged for review and approved keep-as-is.

### Session 53 work (2026-05-12) — `frontend/logging` code-review pass (16 findings closed, 6 commits)

Wide-scope review at `docs/reviews/frontend-logging-2026-05-12.md` — three parallel agents covered rune discipline, the TypeScript layer (`lib/{api,utils,validators,i18n}`), and the UI layer. 33 findings total (5 critical / 17 important / 11 nit). Counts after this session: **all 5 critical closed**, **11 of 17 important closed**, **3 important pushed back or declined per reviewer's own verdict**, **3 important + all 11 nits deferred**.

Commits (in order):

1. **C2 + I12 + I7 + I8.** ADIF length prefix now byte-counted (`new TextEncoder().encode(value).byteLength`); was UTF-16 code units, corrupting every accented-character QSO on the submit path (verified against `internal/adif/parse.go::parseFields`). `QsoEditOverlay.handleSave` builds `body` inside the try so a throw from `toPatchBody` no longer leaves `saving=true` permanent. `config.ts::parseOutcome`, `qso-update.ts::fetchQso`, `qso-update.ts::patchQso` all guard the 200-OK + null-body path with `body === null || typeof body !== 'object'`, returning `{ kind: 'server', code: 'malformed_response' }` instead of crashing the caller. `qso.ts::submitQso` guards the missing-uuid case the same way — phantom empty IDs no longer propagate to sessionQsos / edit overlay / email-out.

2. **C3 + C4.** `QsoEditOverlay` got a focus trap: `dialogEl` bind, `$effect` capturing the trigger element on open and restoring on close, first-input focus once the form has rendered (gated by `initialFocused` so a later loading flip doesn't yank focus). Tab/Shift+Tab wrap at the boundaries; focus pulled back if it has escaped. ESC switched from the reviewer's recommended `stopPropagation()` to `stopImmediatePropagation()` — both window keydowns target `window`, so `stopPropagation` would not stop a sibling handler (QsoPanel's draft-clearing ESC). Pinned by 8 new tests in `QsoEditOverlay.test.ts`.

3. **C5 (Mode only).** Decision required: should Date/Time/Mode inputs be keyboard-reachable? User: "Tab over them (current)" for Date/Time, drop the blanket fix and only fix Mode. `Mode.svelte:28` now `tabindex={disabled ? -1 : 0}` so the dropdown is keyboard-reachable when CAT is off (operator drives mode manually) and skips the tab order when CAT is live. Two new tests pin both states.

4. **C1.** `MyStationPanel`'s `$effect.pre` used to track `configState.bridge.rigModes` + `.modeMappings` implicitly via the `snapModeMappings()` call inside; any external mutation would wipe in-progress operator edits on the Mode Mappings sub-tab. Fixed with `untrack()` around the snap call and a `lastSection` transition gate so re-snaps only fire on INTO-modes. Doc-comment expanded to spell out the dependency-tracking contract. Two new component tests in `MyStationPanel.test.ts` pin both the clobber-resistance contract (verification gap the reviewer called out) and the legitimate first-snap path.

5. **A11y cluster — I13/I14/I15/I16/I18/I19/I20.** Toasts restructured: outer `aria-live`/`role="status"` dropped, each toast is `<div role={level === 'error' ? 'alert' : 'status'}>` (the message is the announced content — no aria-label override), dismiss is a sibling `<button aria-label="Dismiss notification">` with `×` glyph. InfoPanel + MyStationPanel tablists got the WAI-ARIA keyboard contract (ArrowLeft/Right cycle, Home/End jump, roving tabindex active=0/inactive=-1, auto-activation) + matching `aria-labelledby` on every tabpanel. SessionPanel dropped `<tr role="button">` and added a trailing Actions cell with per-row Edit button (table semantics restored, cells keep their gridcell relationship). Callsign blur no longer forcibly refocuses on invalid (operator can Shift+Tab to abandon; submit button is the load-bearing guard). app.svelte setup wraps callsign+Save in `<form onsubmit=...>` so Enter submits. DetailsPanel readonly inputs lost their `tabindex=-1` so they're keyboard-copyable. Six new tablist tests + revised Callsign blur-behaviour tests. **Two operator-visible behaviour changes flagged and confirmed keep-as-is**: Toast dismiss is X-only (was whole-toast click), SessionPanel edit is Edit-button-only (was whole-row click).

6. **Quick batch — I11 + I3 + I9 + validator-parity comments.** Callsign MAX bumped 20→32 to match daemon (`internal/qsoservice/validation.go::IsValidCallsign`); two boundary tests added. Hydration writebacks gated via per-effect first-run latch (`mirrorString`/`mirrorField` helper in `qsoDefaults.svelte.ts` and `manual.svelte.ts`) — the reviewer's "shared `initialized` flag" pattern doesn't work in Svelte 5 because sync code after `$effect.root()` runs before the inner effects fire, so the latch must live inside each effect body. `EnrichmentStation` index signature `[extra: string]: unknown` dropped + four missing daemon fields added (`iota_island_id`, `sig`, `sig_info`, `wwff_ref`); typos like `station.gridsqure` now compile-error. Validator-parity comments added to `callsign.ts` (daemon parity + intentional SPA-stricter character class noted), `maidenhead.ts` (character-identical regex with no shared source), `frequency.ts` (SPA-only UX guard, no daemon counterpart). `zone.ts` already had this — convention now uniform.

**Pushed back / declined:**

- **I1 — derived-via-effect in QsoPanel.** The reviewer's recommended callback-based fix was already considered and rejected in session 47 with a documented why-comment + line-disable. The two-effect mirror is the load-bearing idiom for two-way bind across two reactive stores under the ADR 0009 "writes only when editable" gate. The existing why-comment is the answer.
- **I4 — InfoPanel seed effect re-runs forever.** Reviewer's own verdict: "Functionally fine; acceptable as-is."
- **I10 — `contact-history.ts` `logbook_not_found` 404 collapse.** Reviewer's own note: "Today the SPA never sends `?logbook=` so this is dead." Maintenance-only; revisit when logbook filtering ships.

**Deferred (separate commits):**

- **I2 + I5 + I6 (architectural sweep)** — shared `signalAware()` AbortController helper for the six API helpers, shared `isShape<T>()` runtime-validation helper for response bodies, `_disposeForTests()` exports on `qsoDraft` / `qsoDefaults` / `manual` module-level `$effect.root` modules. Single coherent commit covering the review's three systemic diagnoses ("no `AbortController` anywhere", "no `untrack()` anywhere", "JSON wire boundary trusts the daemon"). Approved by user as next batch — interrupted by power outage before starting.
- **I17 — color-only invalid signal.** Requires refactoring validators from `boolean` → `string | null` across `callsign.ts`, `maidenhead.ts`, `zone.ts` + extending `ValidatedInput` to render an error message. Architectural change rather than a11y polish; deferred as its own commit.
- **All 11 nits (N1–N11).** Polish; batched whenever an adjacent piece of work surfaces one.

**Verification across all six commits:** 498/498 tests pass (was 474 pre-session; +24), `svelte-check` 0 errors / 0 warnings / 243 files, lint clean throughout. New test files: `QsoEditOverlay.test.ts` (8), `MyStationPanel.test.ts` (8). Updated test files: `adif.test.ts` (+4 UTF-8 cases), `Mode.test.ts` (+2 tabindex cases), `Callsign.test.ts` (rewritten focus-trap block + 2 length boundaries).

**Next pickup:** architectural sweep (I2 + I5 + I6). User explicitly approved this as the next batch. Document this session before continuing.

### Session 54 work (2026-05-13) — I2 + I5 + I6 architectural sweep (1 commit, three systemic diagnoses closed)

Picked up the explicitly-approved architectural sweep that session 53 documented but didn't start. Single coherent commit, no behaviour change for current callers.

**New `lib/api/_helpers.ts`** — three primitives the previous boilerplate was repeating across six wrappers:

- `safeFetch(input, init)` — wraps `fetch` and classifies the exception. Returns `FetchOutcome = { ok: true, response } | { ok: false, kind: 'aborted', message } | { ok: false, kind: 'network', message }`. Abort detection is belt-and-braces: error name (`AbortError` from manual abort, `TimeoutError` from `AbortSignal.timeout()`) plus `signal.aborted` fallback for polyfills that surface a generic TypeError after abort.
- `readJsonBody(response)` — wraps `response.json()`, returns `unknown | null` rather than throwing. Every caller wanted to downgrade an unparseable body to a synthesised error envelope, not propagate `SyntaxError`.
- `isPlainObject(value)` — narrows `unknown` to `Record<string, unknown>` for non-null, non-array objects. Arrays excluded by design — every daemon envelope is `{...}`, never `[...]`.
- `isShape<T>(value, requiredKeys)` — composite guard: object + every required key present. Presence-only; per-endpoint semantic checks (e.g. uuid non-empty string) stay at the call site because the right downgrade differs per endpoint.

**Six API helpers updated.** All accept an optional `signal?: AbortSignal` parameter and surface a `kind: 'aborted'` outcome arm. Calls without a signal behave exactly as before. Body envelope handling refactored through `safeFetch` + `readJsonBody` + `isPlainObject` — no blind `as` casts on response bodies.

- `config.ts` — fetchConfig + putConfig
- `qso.ts` — submitQso
- `qso-update.ts` — fetchQso + patchQso
- `enrichment.ts` — enrichCallsign
- `contact-history.ts` — fetchContactHistory
- `session-email.ts` — sendSessionEmail

**Three state modules got `_disposeForTests()`.** Each captures the `$effect.root` dispose function and exports a teardown matching `bridge.svelte.ts → stopBridge()`. Production behaviour unchanged (module lifetime = page lifetime); tests that exercise these singletons can now cleanly tear down between cases without the brittle workaround in `Vfos.test.ts:344-362`.

- `qsoDraft.svelte.ts`
- `qsoDefaults.svelte.ts`
- `manual.svelte.ts`

**No call-site changes.** Every existing caller in `app.svelte`, `QsoPanel.svelte`, `QsoEditOverlay.svelte`, `SessionPanel.svelte`, `InfoPanel.svelte`, `MyStationPanel.svelte` was already non-exhaustive on `outcome.kind` (no `satisfies never` enforcement), so the new `'aborted'` arm doesn't break compile and falls through the same way `'network'` did before. When a future call site wants cancellation, it passes `signal` and adds an arm.

**Tests:** new `_helpers.test.ts` (17 cases covering all four primitives — abort/timeout/network classification, generic-TypeError fallback via signal.aborted, init-passthrough, isPlainObject/isShape edge cases). `qso.test.ts` gains end-to-end abort wire-through (2 cases — aborted-kind classification and signal-passthrough to fetch). Total: **517/517 passing** (+19 from 498), svelte-check 0/0, lint clean.

**Doc footprint:** this entry, review document Status block bumped (16/17 important closed, three primitives + abort param documented). No ADR (additive helpers, no architectural decision moved).

**Verification gaps still open from the review** (called out as work-not-done, not deferred): `api/enrichment.ts` outcome tests, `api/config.ts` outcome tests, `api/contact-history.ts` outcome tests, `utils/frequency.formatFrequency` tests. Highest-priority among these is enrichment per the project's "test error path first for enrichment code" rule.

**Remaining from the review:** I17 (validators boolean → string|null + ValidatedInput error rendering — own commit), all 11 nits (N1–N11 — polish, batch with adjacent work).

### Session 64 work (2026-05-15) — F2 lookup-only + TimerControls start/stop + F3 toggle

Three related shortcut / affordance changes landed together. **F2** (lookup-only enrichment without starting the QSO timer) per ADR 0007 amendment. **TimerControls** Start / Stop buttons (reverses the earlier "considered and dropped" decision in `frontend-spa.md`) with a three-state gate. **F3** as the focus-independent equivalent that mirrors the button gates exactly.

**State machine the buttons / F3 implement:**

1. No callsign typed OR no lookup done for current callsign → both buttons disabled, F3 is a no-op.
2. Callsign typed + F2 lookup done (or post-Stop) → Start enabled, Stop disabled. F3 fires Start.
3. Tab pressed OR Start clicked OR F3-while-stopped+ready → Stop enabled, Start disabled. F3 fires Stop.

The gate `lookupDone` is a `$derived` comparison: `qsoDraft.lookupCallsign === normalize(qsoDraft.callsign)`. Editing the callsign field auto-invalidates the gate without any extra plumbing. Re-typing the original value re-enables it (cheap undo-typo).

**Implementation:**

- New `qsoDraft.lookupCallsign: string` ($state). Set synchronously inside `runLookup(call)` so the gate flips before any network response — enrichment is allowed to fail (the "Enrichment never blocks logging" invariant) and the operator must still be able to commit.
- New `qsoDraft.stopQso()`: symmetric to `startQso()` — resnaps the three time fields to current UTC and flips `qsoStarted=false`. Typed fields stay.
- `qsoDraft.clear()` extended to reset `lookupCallsign = ''`.
- Extracted the enrichment + contact-history fetch body of `handleEnrich` into a new `runLookup(call)` helper. `handleEnrich(call)` now reads as `qsoDraft.startQso(); runLookup(call)` — splitting the timer-start side effect from the lookup work is what makes F2's semantics possible.
- New `handleLookupShortcut()` reads `qsoDraft.callsign`, validates via `isValidCallsign` (same gate Tab uses inside `Callsign.svelte`), uppercases, and calls `runLookup`. Empty or invalid callsign is a silent no-op — the Callsign component's own inline error UI is enough.
- F2 case added to `handleKeydown`: gated on `!qsoEditState.open`, `e.preventDefault()` to defang any browser-level binding. Fires regardless of focus context per ADR 0007's in-field policy — function key, no typing-collision risk.
- F3 case added to `handleKeydown`: reads the same `startEnabled` / `stopEnabled` derived gates the buttons read, fires whichever is active. No-op when both are disabled.
- `TimerControls.svelte` (operator-added skeleton; this session wired the props): now accepts `startDisabled` / `stopDisabled` / `onStart` / `onStop`. Pre-derived disabled flags from the parent — the three-state machine lives in QsoPanel.

**Doc updates:**

- `docs/keyboard-shortcuts.md` — new F2 + F3 rows in the Global section; new TimerControls row in Component activation table.
- `docs/decisions/0007-keyboard-shortcuts.md` — F2 + F3 added to the shortcut map; "Key choices that aren't obvious" gains F2 and F3 entries; "Reserved key real estate" updated to F-keys (F1, F4–F12) reserved for future contest macros with F2 / F3 documented as the operating-flow exceptions.
- `docs/v2-design/milestones.md` — F2 removed from the M2 deferred-shortcuts list.
- `docs/v2-design/frontend-spa.md` — Start/Stop button section flipped from "considered and dropped" to "landed 2026-05-15" with the gate semantics explained. Timer transitions list grew Start-button / Stop-button rows.

**Verification:**

- `svelte-check` clean (250 files, 0 errors, 0 warnings).
- `npm run lint` clean.
- `npm test` — 584/584 pass across 34 files. No new unit tests for the TimerControls gate logic — characterisation tests against the derived gate would mostly re-state the derivation; the live test on the dev SPA covers the real workflow.

**Deferred shortcuts remaining** (per ADR 0007): `Ctrl+\` VFO swap, `?` help overlay. Both still parked.

**Follow-up bug fix (same session): `qsoDraft.qsoStarted` promoted to `$state`.** Live test surfaced "Tab from Callsign, timer ticks but Stop button stays disabled." Root cause: `qsoStarted` was a plain (non-reactive) class field by the original session-33 rule "read only by imperative method bodies, never by a template, `$derived`, or `$effect`." This session's TimerControls work introduced a `$derived(qsoDraft.qsoStarted)` consumer (`stopEnabled` gate) that silently never re-ran when `startQso()` flipped the field. Fix: promote to `$state(false)`. Module-level doc comment in `qsoDraft.svelte.ts` updated; `frontend-spa.md` QSO-draft section + `milestones.md` qsoDraft bullet updated to reflect the new reactivity audit (qsoStarted + lookupCallsign now `$state` because they feed reactive consumers). General rule restated: when a new reactive consumer appears on a previously-plain field, promote — the original "plain unless template-read" wording was too narrow because `$derived` / `$effect` are also reactive consumers.

**Bonus polish (same session): inline validator error messages are now screen-reader-only.** Operator reported "type M0, press ESC — value clears but red border + error message stay stuck." Two changes, applied to both `Callsign.svelte` and `ValidatedInput.svelte`: (1) `errorKey` converted from `$state` mutated by `oninput` / `onblur` handlers to `$derived(validator(value))` — programmatic value resets (ESC → `qsoDraft.clear()` → `bind:value = ''`) now re-run validation reactively rather than being missed because the input handlers didn't fire; (2) the inline error `<p>` switched from `class="input-error"` (visible text) to `class="sr-only"` (screen-reader announcement only) — the red border on the input is the only visible cue. Behavioural contracts preserved: focus-trap on invalid blur in ValidatedInput still works (it now reads the always-up-to-date derived); transform-pre-validation pipeline still mutates `value` before the derived re-runs. Tests: 4 new regression tests (sr-only class + programmatic-reset-clears-error for both components); 1 existing test reframed (ValidatedInput "calls validator on blur" → "does NOT re-call validator on blur" since the verdict is already current).

---

### Session 63 work (2026-05-14) — Install-day shakeout: config templates, UA refactor, working-dir fix, dev RPM workflow

Long session driven by real install-day friction on the operator's machine. Two install cycles, two real bugs surfaced and fixed, a handful of UX-polish config-template additions, one structural refactor (global UserAgent), and the dev RPM workflow itself. Net effect: a freshly-installed daemon now produces a complete, hand-editable `config.json` that exposes every operator-touchable knob, and the import + daemon binaries pick the right working dir without any shell-side env setup.

**Bug fixes (both surfaced live, both required RPM rebuild + reinstall to verify):**

1. **`utils.WorkingDir()` never called at startup → daemon crash-looped on first install.** The systemd unit set `SM_WORKING_DIR=%h/.local/share/station-manager`, but nothing in `cmd/smd/main.go` actually called `WorkingDir()` to MkdirAll-create that path before `loadConfig` tried to seed `config.json` into it. The existing first-run write tolerated the failure ("continuing with in-memory defaults"); my new `cfgSvc.Update(UserAgent)` line then hit the same missing-dir error and treated it as fatal, producing a permanent crash loop. Fix: `defaultConfigPath()` now delegates to `utils.WorkingDir()` (which does the MkdirAll), and the UA-persist call softened to log-and-continue matching the seed pattern.
2. **`defaultConfigPath()` cwd-first preference picked up stray `$HOME/config.json`.** First attempt at the working-dir refactor preferred a cwd-local `config.json` over `utils.WorkingDir()`. The systemd unit defaults cwd to `$HOME` (no `WorkingDirectory=` override), so a leftover `~/config.json` from an earlier misconfigured `smd import` run silently preempted the real install. CAT stopped working because the stray config had `bridge.enabled=false`. Operator caught it ("CAT does not seem to be working (it was)?"); cwd preference removed entirely — `utils.WorkingDir()` is now the canonical resolver, full stop. The dev-workflow benefit (run `./smd` from a repo dir with config beside the binary) is preserved via `utils.WorkingDir`'s exec-dir fallback for non-system-path binaries; system-path binaries (`/usr/bin/smd`) always resolve to XDG. `TestLoadConfig_CwdFallback` deleted (premise no longer applies); `TestLoadConfig_FirstRunWritesDefaultInCwd` renamed to `_InWorkingDir` and rewritten to use `SM_WORKING_DIR` explicitly, plus a new assertion that no stray `config.json` lands in cwd.

Both bugs are the canonical "deployment-shape regression that unit tests can't see" — they require `dnf install` → `systemctl --user start` → real filesystem + env from the unit. Logged as concrete motivation in `project_sm_cd_pipeline_planned` memory; future CD pipeline work has a 5-8-case post-install acceptance test surface staked out.

**Config templates — every operator-editable field now visible in the rendered `config.json`:**

The principle the operator drove home over the session: a fresh `config.json` should show every knob you might need to touch, with sensible empty defaults and an explicit `enabled` flag, so editing is "pattern-match and fill in" rather than "read the schema source to remember what fields exist." Specific changes:

- **SMTP block — explicit `enabled` field + omitempty stripped from every operator-fillable field.** `SmtpConfig.Enabled bool` is now the kill-switch (replaces the implicit "empty Host = disabled" convention). `Service.Enabled()` and `Send()` gate on `cfg.Enabled`. `ErrMailerDisabled` message updated. `StartTLS` defaults to `true` in DefaultConfig (matches the doc-comment intent — was previously omitempty-hidden when false). Validation: enabled→requires Host+From+Port+TimeoutSec; disabled→no further checks. Rendered shape: `{enabled, host, port=587, username, password, from, default_recipient, starttls=true, timeout_sec=30}`. ~10 test sites updated to add `Enabled: true` where they previously implied enable-via-Host.
- **QRZ forwarder template** in `DefaultConfig.Forwarders`: disabled QRZ entry with empty `api_key` so the operator only fills in credentials + flips `enabled`. Validation passes (statically decidable), disabled entries skipped at startup (`cmd/smd/main.go:617`).
- **Hamnut lookup template**: prepopulated with canonical URL `https://api.hamnut.com/v1/call-signs/prefixes` (v1's value, recovered from `git show v1.0.0:internal/config/defaults.go`). Operator just flips `enabled` for the common case.
- **QRZ lookup chain template**: prepopulated with canonical XML endpoint `https://xmldata.qrz.com/xml/current`, `view_url` set to `https://www.qrz.com/db/` (trailing slash; SPA concatenates the callsign; matches v1's `QrzViewUrl` default), empty `username` / `password`. Operator fills credentials, flips `enabled`.
- **Bridge block — `serial.port` and `cat.driver` rendered as empty strings** instead of being `omitempty`-hidden inside `serial: {}` / `cat: {}` placeholder objects. `BridgeConfig.{Serial,Cat}` and the inner `Port` / `Driver` fields lost their `omitempty` tags; rendered shape now `{enabled: false, serial: {port: ""}, cat: {driver: ""}}` so a fresh install advertises the two fields you need to fill in.
- **LookupConfig — `username` / `password` rendered as empty strings** (omitempty stripped). Hamnut shows them empty even though it doesn't use them; the operator can ignore them there.

**Global UserAgent refactor (operator-driven structural change):**

The operator noticed the QRZ forwarder hardcoded `station-manager/dev` while the lookup providers each took a per-provider `useragent` from `LookupConfig`. Two asymmetries: forwarder vs lookup (one hardcoded, the others operator-configurable), and within lookup (per-provider when in practice every provider speaks for the same daemon). Per their direction: collapse to ONE global `Config.UserAgent` at the top level of `config.json`.

Schema changes:
- `Config.UserAgent string \`json:"useragent"\`` added at the top of `Config`.
- `LookupConfig.UserAgent` removed entirely (no `omitempty` adjustment needed — gone).
- `validateLookupProvider` no longer checks UA (moved to Service.Initialize per provider).
- Provider Service structs (`hamnut.Service`, `lookup/qrz.Service`) gained a `UserAgent string` field; `s.Config.UserAgent` reads switched to `s.UserAgent`; Initialize fails loudly when empty for enabled providers.

Plumbing in `cmd/smd/main.go`:
- Post-Load: if `cfg.UserAgent` is empty, fill with `"station-manager/" + Version` (ldflags-injected build version). Persist via `cfgSvc.Update`. Fail loudly if the resolved UA ends up empty (shouldn't happen — defense in depth).
- After resolution: set `qrz.UserAgent` (forwarder package var) from `cfg.UserAgent`, closing the stage-8 TODO in the forwarder's doc comment. Pass `cfg.UserAgent` to each lookup Service via the new struct field at construction.

Build-time wiring:
- `scripts/release-rpm.sh` and `scripts/dev-rpm.sh` now inject `-X main.Version=$VERSION` via `-ldflags`. Production builds carry the real version in their UA; dev builds carry `station-manager/dev`. The previously-broken `var Version = "dev"` ldflags hookup at `cmd/smd/main.go:44` is now actually used.

Migration note: existing operator configs with `useragent` inside `lookup.hamnut` or `lookup.chain[i]` get silently ignored on load (unknown JSON field). For the single-user dev install that's the entire user base today, this is a non-issue.

**Dev RPM workflow (operator-driven):**

The operator was rebuilding/reinstalling 5+ times in the session and asked for a fixed-filename dev artifact so the install command stays the same across iterations. New deliverables:

- **`scripts/dev-rpm.sh`** — fixed-version (`dev`), fixed-output (`build/release/station-manager-dev.x86_64.rpm`). Same SPA→Go→nfpm pipeline as `release-rpm.sh` but no version argument to invent each time.
- **`Taskfile.yml` → `rpm:dev` task** — delegates to the script (existing Taskfile convention: tasks are thin wrappers over `scripts/*.sh` so anyone without `task` installed can run the bash directly). Listed via `task --list`.
- **`packaging/postinstall.sh` removed; `nfpm.yaml` `scripts.postinstall` dropped.** The scriptlet only echoed instructions during `dnf install`; an RPM scriptlet can't do anything actually useful (runs as root, can't touch the operator's systemd user instance). `docs/install.md` is the canonical setup guide; the scriptlet was noise.

**Misc UX fixes:**

- **Welcome-page callsign field autofocus.** First-run setup's input lacked focus on mount. Added a tiny Svelte 5 action `use:autofocus` (fires when the element mounts — exactly when the `{#if !configState.setupComplete}` branch becomes true). Three-line change in `app.svelte`. The setup snippet is gated behind a fetch, so a script-level `onMount` focus call wouldn't have worked.
- **`bridge.serial.baud` removed from config schema.** The operator spotted the redundancy: rigdef declares `baud_rate` (per-rig protocol setting), bridge config also had `Baud` (operator-supplied). `buildSerialConfig` was reading the config one and ignoring the rigdef one entirely — silently wrong for any future non-38400 rig because `applyDefaults` stamped 38400 on the config-side. Removed `BridgeSerialConfig.Baud`, `buildSerialConfig` now sources `BaudRate` from `rigSerial.BaudRate`, the 38400 default and `>0` validation deleted, 10 test sites updated. New rigdefs (Icom at 9600, etc.) just declare their baud in the JSON and it flows through.
- **WorkedPanel "no prior contacts" message moved to i18n + reworded.** Was hardcoded `"No prior contacts with this callsign."` — operator wanted "with this station" (correct framing: the panel is keyed by callsign but the operator's mental model is "the station I'm working"). New i18n key `worked.empty` in `lib/i18n/en.ts`, WorkedPanel imports `t` and renders `{t('worked.empty')}`. First non-bridge-error consumer of the i18n system.

**Doc footprint:**

- This entry.
- `docs/install.md` — §5 (ADIF import) loses the `SET SM_WORKING_DIR` env-setting requirement (no longer needed); §6 (locations) clarifies the XDG-fallback behaviour.
- `project_sm_cd_pipeline_planned.md` — updated with the two concrete deployment-shape incidents and a starter list of post-install acceptance test cases.
- No ADR — every change was operator-directed; no plausible-alternatives-weighed decision worth the formal log.

**Verification:** all Go tests pass (`go test ./...`); SPA `npm run check` 0/0/0, `npm test` 584/584. Two operator-confirmed live tests this session (`task rpm:dev` cycle; daemon serves SPA, opens serial port for CAT, logs to right path).

**Next-session pickups:**

- CD pipeline planning is now real (operator surfaced it this session as direct consequence of the deployment-shape bugs). When started, the test inventory in `project_sm_cd_pipeline_planned.md` plus the two named incidents are the starting brief.
- Stage 3 (install day) effectively happened across this session; operator's machine is now running v2 with CAT confirmed live earlier (though it was loading the wrong config for part of the session due to the bug). Real ADIF import still to happen on the right database (was blocked all session by the cwd-fallback bug; the cleanup command in the last fix should clear the path).

### Session 62 work (2026-05-14) — Stage 2 of pre-dogfooding: RPM packaging

Single binary + systemd `--user` unit, packaged as `station-manager-<ver>.x86_64.rpm`. Same package name as v1 so `dnf install` replaces the existing `station-manager-0.0.0~local-1` cleanly via file-list swap. v2's package is dramatically simpler than v1's — no GTK/WebKit depends, no three Wails binaries, no `.desktop` / icon / XDG-menu files. The browser SPA is embedded in the daemon binary via `//go:embed` and served at `GET /`, so the operator's browser is the UI.

**Files (all NEW):**

- **`nfpm.yaml`** (repo root). Two `contents:` entries — `build/bin/smd` → `/usr/bin/smd` (0755) and `packaging/smd.service` → `/usr/lib/systemd/user/smd.service` (0644). Postinstall scriptlet wired via `scripts.postinstall`. `${VERSION}` placeholder filled by the build script.
- **`packaging/smd.service`**. Type=simple, `ExecStart=/usr/bin/smd`, `Environment=SM_WORKING_DIR=%h/.local/share/station-manager`, `Restart=on-failure` + `RestartSec=5s`, `WantedBy=default.target`. The `SM_WORKING_DIR` env is essential — without it, `utils.WorkingDir()`'s executable-dir fallback would land in `/usr/bin/`, which is read-only and wrong for user data.
- **`packaging/postinstall.sh`**. RPM scriptlet runs as root, so it cannot do user-context systemd (`systemctl --user`) or `loginctl enable-linger` — both need a target user, and the right answer is "the operator," not "root." Prints next-step instructions instead: `systemctl --user daemon-reload && systemctl --user enable --now smd`, `loginctl enable-linger "$USER"`, default URL `http://127.0.0.1:8080`, and the data/log/config paths under `~/.local/share/station-manager/`.
- **`scripts/release-rpm.sh`**. Three-step build: (1) `npm run build` in `frontend/logging/` so `dist/` is current for `//go:embed`; (2) `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o build/bin/smd ./cmd/smd` (modernc-sqlite is pure Go, so the binary is fully static — `file` confirms `statically linked`, `ldd` says `not a dynamic executable`); (3) `VERSION="$1" nfpm pkg -f nfpm.yaml -p rpm -t build/release/`. Single positional arg is the version tag.

**Verification (built `station-manager-0.0.0~stage2-1.x86_64.rpm`, 7.9 MB):**

- `rpm -qlp` lists exactly the two expected files (`/usr/bin/smd`, `/usr/lib/systemd/user/smd.service`) — no stray content.
- `rpm -qp --scripts` shows the postinstall scriptlet embedded verbatim.
- Modes correct: binary `-rwxr-xr-x root:root`, unit `-rw-r--r-- root:root`.
- Binary inside is 21 MB statically linked, smd's `--help` reports the expected `--config` flag.
- Metadata correct: `Name: station-manager`, `License: MIT`, `Packager: ColonelBlimp`, summary matches `nfpm.yaml`.

**Key design choices:**

- **No `depends:`.** v2 has zero runtime deps (Go binary, modernc-sqlite pure-Go, browser SPA embedded). Empty depends keeps the package minimal and avoids stale GTK/WebKit pins from v1.
- **No Taskfile/Makefile.** v1's Taskfile existed mostly to wrangle the multi-module workspace. v2 has a single `go.mod`; a 30-line bash script is enough. No new tooling to install.
- **`CGO_ENABLED=0` + `-trimpath` + `-ldflags="-s -w"`** for a fully static, reproducible, strip-debug-info binary. 21 MB stripped is about as small as the daemon + embedded SPA gets.
- **`%h/.local/share/station-manager`, not `/var/lib/station-manager`.** This is a `--user` service running per-operator; everything lives under their home directory. Matches the path the operator named in session 61's stage-3 plan.
- **Postinstall is print-only.** An RPM scriptlet can't enable a user service or call `loginctl enable-linger` (root scriptlet, wrong user). Best the package can do is tell the operator the two commands to run; they're short and a one-time cost.

**Stage 3 (install day) is now ready to run** whenever the operator wants to cut over. Sequence: backup `~/.local/share/station-manager/` → `dnf remove station-manager` (clears v1) → `dnf install build/release/station-manager-<ver>.x86_64.rpm` → `systemctl --user daemon-reload && systemctl --user enable --now smd` → `loginctl enable-linger "$USER"` → first-run setup at `http://127.0.0.1:8080` → `smd import ~/Downloads/qrz-export.adi`.

**Doc footprint:** this entry; new "Pre-dogfooding" section in `docs/v2-design/milestones.md` covering stages 1–3.

### Session 61 work (2026-05-14) — ADIF importer subcommand (`smd import`)

Stage 1 of the pre-dogfooding work. New `smd import <file.adi>` subcommand brings 4233 historical QSOs from QRZ Logbook into the v2 daemon. **No new HTTP endpoint** — discussed and rejected in conversation as over-engineering for a one-shot operation. The subcommand drives the canonical `qsoservice.Submit` path directly (validation + atomic write + audit table all inherited) and stamps the QRZ `qso_upload` row pre-success with the LOGID from the source ADIF.

**Files touched:**

- **`cmd/smd/import.go`** (NEW). `runImport(args []string) error` — minimal DI container (config + logging + sqlite + qsoservice + events.Hub; no HTTP, no forwarder workers, no bridge, no enrichment), parses the ADIF file, iterates records, per record: `normalizeImportedMode(&rec)` → `qsoservice.Submit` → if `app_qrzlog_logid` + `qrzcom_qso_upload_status=Y` present AND QRZ forwarder is configured, `MarkUploadSuccessWithContext` to stamp the qso_upload row pre-success. Prints `{stored, duplicate, errors}` summary; exit 1 if errors > 0.
- **`cmd/smd/main.go`** — subcommand dispatch at the top of `main()`. `os.Args[1] == "import"` branches to `runImport(os.Args[2:])` before the daemon's `flag.Parse()`. Shape future-proof for additional subcommands.
- **`cmd/smd/import_test.go`** (NEW). 5 integration tests (real sqlite, no mocks per project convention): happy path, dry-run skips DB, idempotency (same file re-imported → all duplicate), MY_\* preservation (operator's current config does NOT overwrite historical record values), QRZ LOGID stamping (qso_upload row gets correct upstream_id + uploaded status).
- **`internal/adif/adif.go`** — added `AppQrzlogLogid string \`adif:"app_qrzlog_logid,omitempty"\`` to `Record`. The QRZ-app-specific LOGID was being silently dropped by the parser (no struct field). Now captured, used by the importer, omitted from non-import emit paths via `omitempty`. Same pattern as the existing `AppSmQsoID` / `AppSmRequestQsl`.

**Key design choices (locked in conversation, captured for posterity):**

- **No new HTTP endpoint.** Operator pushback was correct — `POST /v1/qso/import` would be permanent code surface (handler + tests + api.md entry + error paths) for an operation that runs maybe 5 times in its lifetime. Subcommand on the existing binary is dramatically smaller.
- **Caller-supplied MY_\* fields win.** `qsoservice.Submit` already does this (it overlays only `StationCallsign` from the record onto `qso.LoggingStation`; the other MY_\* fields flow via `adif.RecordToQso` as-is). No code change needed in qsoservice — verified by reading the function before building. The test pins the contract.
- **`normalizeImportedMode` handles `MODE=USB` (QRZ shorthand).** QRZ Logbook exports submodes as MODE (e.g. `MODE=USB` instead of `MODE=SSB SUBMODE=USB`). Strict ADIF validation rejects the shorthand. Pre-Submit normalisation: if MODE is recognised as a submode, swap → `MODE=<parent> SUBMODE=<original>`. Lives in the importer because the live SPA submit path never sees this corruption.
- **`MarkUploadSuccessWithContext`, not `MarkUploadSuccessWithAdifStampWithContext`.** The latter stamps today's date onto the QSO row; for imports we want the historical `qrzcom_qso_upload_date` from the source. `RecordToQso` already preserves those fields onto the QSO row, so the importer only needs to touch the qso_upload row (status + upstream_id).
- **Operator's existing v1 `station-manager` RPM `nfpm.yaml` shape.** Confirmed packaging via `nfpm` is the project's pattern. v2 RPM will be one binary (`smd`) + systemd `--user` unit + `loginctl enable-linger` instructions for boot-time auto-start. Same package name (`station-manager`) so `dnf install` replaces v1's `station-manager-0.0.0~local-1` via file-list swap.

**Live test against `build/20260514-7q5mlv.adi` (4233 records):**

- 4230 stored, 2 duplicate (in-file dupes), 1 error (single bad record with `rst_rcvd=4657` violating the daemon's `length(rst_rcvd) <= 3` CHECK constraint — likely a typo in the source, not a daemon bug).
- All 4230 stored QSOs have `qso_upload` rows pre-stamped (`status=uploaded`, `upstream_id` populated with the QRZ LOGID).
- MY_\* fields preserved per-record where present: `my_gridsquare` on every record (the historical Malawi `KH78an`, NOT current config), `my_antenna` on 2340/2342, `my_rig` on 1181/1181.
- Historical `qrzcom_qso_upload_date` preserved (`20240617`, not today's date).
- Throughput ~930 records/sec on the operator's machine. The whole import runs in ~5 seconds.

**Pre-dogfooding sequencing (recap):**

1. **Stage 1 — Importer.** ✅ This session. Test surface green; live test confirms the canonical path.
2. **Stage 2 — RPM with `nfpm.yaml`.** Next session. One binary, systemd `--user` unit, `loginctl enable-linger` documented in post-install.
3. **Stage 3 — Install day.** Same sitting as stage 2's first install: backup `~/.local/share/station-manager/` → `dnf remove station-manager` → `dnf install station-manager-<ver>.rpm` → `systemctl --user daemon-reload && systemctl --user enable --now smd` → first-run setup → `smd import ~/Downloads/qrz-export.adi`.

**Verification:** all Go tests pass (full `go test -race ./...`); SPA tests unaffected (no JS changes). 5 new tests in `cmd/smd/import_test.go`. Live import survives 4233 real records.

**Doc footprint:** this entry. `docs/v2-design/milestones.md` should grow a brief "pre-dogfooding" section pointing at this work — deferred to the stage-2 session so the section lands with the RPM work too.

### Session 60 work (2026-05-14) — 8 of 11 nits closed, review fully resolved on intent

Final cleanup pass on the frontend-logging code review (`docs/reviews/frontend-logging-2026-05-12.md`). N1 and N11 were reviewer-marked acceptable-as-is; N5 was flagged "take or leave" and skipped per the project's "build specific not generic" rule. The remaining eight all landed.

**N2 — i18n placeholder regex constraint documented.** `lib/i18n/index.ts:70` — added a 6-line comment noting that `{(\w+)}` only matches `[A-Za-z0-9_]`, so a future `{client-id}` would render literally instead of substituting. All daemon `details` keys are snake_case today; comment flags the constraint for when that changes.

**N3 — `submitQso` gains `force?: boolean`.** Doc-comment promised "caller decides whether to offer `?force=1` retry" but the wrapper had no parameter to pass it. Signature changed from `(adif, logbookID, signal?)` to `(adif, logbookID, options: SubmitOptions = {})` with `{ force?, signal? }`. Daemon support verified at `internal/api/handler_qso.go:236` (accepts truthy `1`/`true`). Only one production call site (`QsoPanel.svelte:303`); the empty-options default keeps it working unchanged. Two test changes: existing AbortSignal cases updated to the new options shape; two new cases pin the wire seam (`?force` omitted when unset, `?logbook=1&force=1` when `force:true`).

**N4 — `formatFrequency` guards negative / fractional Hz.** `utils/frequency.ts` previously produced `"-1.-01.-01"` for `hz < 0` and NaN-padded segments for fractional input. Now coerces via `Math.max(0, Math.floor(hz))` — keeps the three-group dot shape stable on nonsense input (which CAT never produces, but the function is now safe for ad-hoc consumers). Two new tests in `frequency.test.ts` pin the clamp.

**N6 — `DaemonQsoForEdit` type moved to API layer.** Previously defined in `lib/states/qsoEdit.svelte.ts` and imported into `lib/api/qso-update.ts` — the API wrapper had a reactive-state-module dependency for a wire shape that has nothing to do with reactivity. Type now lives in `qso-update.ts` (where the fetch/PATCH helpers consume it); `qsoEdit.svelte.ts` re-exports it via `export type { DaemonQsoForEdit } from '../api/qso-update'` plus an import-for-local-use line so internal references stay typed. The test file's `import { type DaemonQsoForEdit }` still works via the re-export. Net: API layer no longer drags state-module dependencies.

**N7 — zone `DIGITS_ONLY` strictness documented.** `validators/zone.ts:23` — added a 5-line comment noting the deliberate stricter behaviour vs daemon's `strconv.ParseInt` (which accepts a leading `+`). Operator workflow today is "type a small positive integer"; a leading `+14` is almost certainly a paste artefact. The daemon side stays compatible because ParseInt accepts both.

**N8 — long-path distance clamped at zero.** `utils/bearing.ts:144` — haversine + FP rounding can push antipodal-grid `shortPathDistanceKm` a hair above the great-circle limit, producing a small negative `longPathDistanceKm`. Wrapped both the km and miles values in `Math.max(0, …)` so the dot-format consumer never sees a sign-flipped value.

**N9 — `LoggingCard` spacer divs collapsed.** `LoggingCard.svelte:14-25` — three empty `<div class="flex w-...">` spacers replaced with `ml-auto` on the Session Time block. Net: 6 lines deleted, same visual layout, intent (push the timer right) now matches what the markup says.

**N10 — InfoPanel tab icon picker as `Record<TabId, Snippet>`.** `InfoPanel.svelte:345-349` — 4-arm `{#if/else if}` chain replaced with `{@const tabIcons: Record<TabId, Snippet> = {...}}` + `{@render tabIcons[tab.id]()}`. New `import type { Snippet } from 'svelte'`. The lookup re-builds per iteration (4 tabs, trivial cost) but the data-driven intent is now explicit and adding a fifth tab won't require touching the render block.

**Skipped:**

- **N1** — RST mode-flip refill. Reviewer's own verdict: "Acceptable as-is."
- **N5** — `adif.ts` operator-station if-cascade. Reviewer flagged "take or leave"; collapsing 25+ if-blocks into a table-driven loop is exactly the kind of premature abstraction the [feedback_design_patterns] memory warns against — leaving the specific repetition.
- **N11** — `SessionTimer` `setInterval` at module lifetime. Reviewer note: "consistent with QsoPanel's ticker; future component tests will need to mock timers." Future cost flagged, not a defect.

**Verification:** 584/584 SPA tests passing (+4 from 580: 2 new `submitQso` force cases, 2 new `formatFrequency` guard cases), svelte-check 0 errors / 249 files, eslint clean.

**Doc footprint:** this entry, review doc Status block bumped to **review FULLY RESOLVED ON INTENT** (everything actioned or explicitly reviewer-deferred), each closed nit marked CLOSED inline. No ADR (no architectural decision moved), no CLAUDE.md / memory updates (no new rule).

**Review state:** ALL critical findings, ALL 17 important findings, and 8 of 11 nits closed (the other 3 explicitly accepted-as-is per the reviewer's own verdicts). The review is done.

### Session 59 work (2026-05-14) — I17 closed: validators return string|null, inline error messages

I17 was the last open architectural item from the frontend code review (4 verification gaps closed sessions 55–58; I17 deferred as a real refactor). The validator contract changes from `(v: string) => boolean` to `(v: string) => string | null`: null when valid (including empty — presence stays form-level), an i18n key string when malformed. `ValidatedInput` and `Callsign` render the resolved key in a paired `<p id="{id}-err" role="alert">` and wire `aria-describedby` to it, so the operator-visible signal is no longer color-only.

**Files touched:**

- **`lib/i18n/en.ts`** — new `validators.*` namespace (callsign / maidenhead / cq_zone / itu_zone / dxcc / rst / frequency). Wording is short — the red border carries "something's wrong", the message answers "what shape does this expect."
- **All 5 validator modules** flipped to `string | null`. `passthrough.ts` returns `null` unconditionally. `zone.ts`'s `inRange` factory now takes an `i18nKey` arg so the three exports each carry their own key. The five validator test files had assertions flipped (`toBe(true)` → `toBeNull()`, `toBe(false)` → `toBe('validators.<name>')`); no behavioural test changes.
- **`ValidatedInput.svelte`** — prop type narrowed; internal `invalid` boolean replaced with `errorKey: string | null` $state; new `errorId = $derived(\`${id}-err\`)` (must be `$derived` because `id` is a reactive prop, not a const at module scope); renders `<p>` + `aria-describedby` conditionally on `errorKey !== null`. Five new test cases (`error message rendering (I17)`): `<p>` renders with rendered i18n text, `aria-describedby` wires up, both clear once valid, omitted for empty, drops on correction. Test fixture `acceptDigits` updated to return `string | null`; it borrows `'validators.rst'` as a real catalogue key so the rendered text isn't a `[missing: ...]` sentinel.
- **`Callsign.svelte`** — same treatment as ValidatedInput (uses `isValidCallsign` directly). Six new test cases including a blur-path variant. The session-53 I18 "blur does not refocus" contract is preserved.
- **`styles/app.css`** — new `.input-error` utility class (`mt-1 text-xs text-invalid`), paired with the existing `.invalid-input` outline rule. Compact spacing so single-line messages fit under most input rows without nudging the grid.

**Consumer fixes (drive-by — boolean → null/non-null semantics):**

- `lib/states/qsoDraft.svelte.ts::canSubmit` — three guards changed from `isValidX(...)` to `isValidX(...) === null`.
- `lib/utils/bearing.ts::gridToDecimal` — `!isValidMaidenhead(trimmed)` → `isValidMaidenhead(trimmed) !== null`.
- `lib/ui/components/VfoInput.svelte::handleInput` — `!isValidFrequency(editValue)` → `isValidFrequency(editValue) !== null`.
- `app.svelte::putCallsign` — same flip for the setup-card validation guard.

Surfaced by the test sweep — bearing / VfoInput / enrichment-paths-derivation all failed because the truthy-string return inverted the boolean intent at non-component call sites. `qsoDraft.canSubmit` was the svelte-check failure (`string | boolean | null` not assignable to `boolean`).

**Pushback / scope notes:**

- The 11+ `passthrough` call sites in MyStationPanel are untouched: `passthrough` returns `null` (valid), same observable behaviour as before.
- The reviewer's "i18n key + details" phrasing left details optional. No current validator needs details — failures are binary per validator (CQ Zone is out of range; the operator can see what they typed). If a future validator needs detail substitution, widen the validator return type to `string | { key: string; details: Record<string, string> }` per-call without disrupting existing callers.

**Verification:** 580/580 SPA tests passing (+11 from 569 — 5 new in ValidatedInput, 6 new in Callsign), svelte-check 0 errors / 249 files, eslint clean.

**Doc footprint:** this entry, review document I17 entry marked CLOSED, Status block bumped to 15 of 17 important. No ADR (no architectural decision moved — the validator-shape choice was prescribed by the review). No CLAUDE.md / memory updates: the validator-presence convention stays unchanged (validators don't enforce presence — empty is null/valid, form layer gates required-ness).

**Remaining from the review:** 11 nits (N1–N11) only. All criticals, all four verification gaps, and 15 of 17 important findings now closed.

### Session 58 work (2026-05-14) — `utils/frequency.formatFrequency` tests landed

Last of the four verification gaps from session 54. Existing `lib/utils/frequency.test.ts` already covered `frequencyToBand`; appended a `formatFrequency` describe block to the same file rather than splitting. No production change.

**Added to `lib/utils/frequency.test.ts` (10 cases, total file now 22):**

- The doc-comment example pinned (`14_250_000 → "14.250.000"`) so the comment can't silently drift from the code.
- Zero-padding correctness via the smallest-non-zero case: `5 → "0.000.005"`. Both pad sites exercised (`kHz=0 → "000"`, `Hz=5 → "005"`).
- `0 → "0.000.000"`.
- HF watering-hole spot-checks: 40m/20m/6m FT8 — `7.074.000`, `14.074.000`, `50.313.000`.
- 1 MHz boundary in both directions: `1_000_000 → "1.000.000"` and `999_999 → "0.999.999"`.
- Mid-value padding: `14_050_000 → "14.050.000"` (kHz tens) and `14_250_007 → "14.250.007"` (Hz tens).
- 23cm microwave: `1_296_000_000 → "1296.000.000"`. Pins that the MHz field is NOT padded or truncated — only kHz/Hz get `padStart(3)`. Guards against a future "always pad MHz to 4 digits" refactor that would break HF rendering.
- Single-Hz precision: `1_800_001 → "1.800.001"` (smallest representable rig-CAT step).
- Structural invariant: across magnitudes from 0 to 1 GHz, output always splits to exactly three dot-separated groups. Consumer parsers (e.g. SessionPanel display) rely on this regardless of magnitude.

**Verification:** 569/569 SPA tests passing (+10 from 559), svelte-check 0 errors / 249 files, eslint clean.

**Doc footprint:** this entry, review document Status block + verification-gap #6 (the frequency entry) marked CLOSED. No ADR (test-only change), no CLAUDE.md / memory updates (no rule moved).

**All four verification gaps from the review are now closed.** Remaining open from session 54: I17 (validators boolean → string|null + ValidatedInput error rendering — own commit, architectural) and the 11 nits (N1–N11 — polish, batch with adjacent work).

### Session 57 work (2026-05-14) — `api/config.ts` outcome tests landed

Third of the three remaining verification gaps from session 54. Single test file, no production change. Covers both `fetchConfig` (GET) and `putConfig` (PUT with payload) — the latter is the more operator-visible path (Save button click) and gets its own duplicated parseOutcome coverage rather than relying on the shared internal helper, since regression there is louder.

**New `lib/api/config.test.ts` (18 cases):**

- `fetchConfig` request shape — URL `/v1/config`, no explicit method (asserts `init.method === undefined` so a future "helpful" default-to-GET in the wrapper would surface as a contract change).
- `kind=ok` happy path with a minimal-but-valid `ConfigResponse` fixture (most fields elided via `omitempty`, matching how real daemon payloads look).
- `kind=server malformed_response` for two distinct unparseable-200 shapes: non-JSON body and a JSON array. The array case pins that `isPlainObject` excludes arrays even though `JSON.parse('[1,2,3]')` succeeds — daemon contract for `/v1/config` is always `{...}`. This is the I7 parse-failure guard the review called out: without it, a daemon regression crashes the caller at the first `config.logging_station` dereference.
- `kind=validation` on 400 (daemon `invalid_field_value` — session 46's zone/callsign validation work).
- `kind=server` on 500 (daemon `db_error`).
- Synthesised `unknown_error` + `HTTP 502` fallback for unparseable error body.
- `kind=network` / `kind=aborted` + signal passthrough.
- `putConfig` request shape — URL `/v1/config`, `method: 'PUT'`, `Content-Type: application/json`, body is bit-for-bit `JSON.stringify(payload)`. Bit-for-bit pin keeps a future "preprocess the payload" from silently drifting the wire format.
- `putConfig` parseOutcome coverage: `kind=ok` 200, `kind=validation` 400 (callsign validation), `kind=server` 500 (`config_write_failed`), `kind=server malformed_response` on `200 "null"` body, `kind=network`, `kind=aborted`, signal passthrough.

**Verification:** 559/559 SPA tests passing (+18 from 541), svelte-check 0 errors / 249 files, eslint clean.

**Doc footprint:** this entry, review document Status block + verification-gap #4 marked CLOSED. No ADR (test-only change), no CLAUDE.md / memory updates (no rule moved).

**Remaining verification gaps from the review:** `utils/frequency.formatFrequency` tests only. I17 (validators boolean → string|null) and the 11 nits (N1–N11) still outstanding per session 54.

### Session 56 work (2026-05-14) — `api/contact-history.ts` outcome tests landed

Second of the three remaining verification gaps from session 54. Single test file, no production change. Followed the session 55 enrichment-test pattern with two endpoint-specific divergences (called out below). State-holder coverage already existed at `lib/states/contactHistory.test.ts`; this fills in the wire-level outcome surface.

**New `lib/api/contact-history.test.ts` (14 cases):**

- GET URL construction — `M0XYZ/P` exercises `encodeURIComponent` (the `/` makes encoder presence observable).
- `kind=ok` happy path with two rows.
- `kind=ok` with `items: []` — pins the daemon's "no prior contacts is 200 + empty array, NOT 404" contract.
- Three structural-fallback cases all surface as `kind=ok, items=[]`: 200 with non-JSON body, 200 with no `items` field, 200 with non-array `items`. This is the **endpoint-specific divergence from enrichment**: contact-history's wrapper deliberately downgrades a "daemon regression" 200 body to the same empty-list outcome the panel already renders, matching the source comment ("every success path on this endpoint emits at least `{items: []}`"). Pinning all three keeps the contract from drifting silently.
- `kind=validation` for 400 `missing_required_param`, 400 `invalid_field_value`, and 404 `logbook_not_found`. The 404 case is the **other divergence**: the SPA wrapper currently never sends `?logbook=`, but the daemon emits this status, and 404 < 500 routes through the validation arm. Test covers the daemon contract even though the SPA call path is dead today (per session 53 I10 verdict).
- `kind=server` on 500 with daemon `code` + `message` envelope.
- Synthesised `unknown_error` + `HTTP 502` fallback when an error body isn't parseable.
- `kind=network` on fetch reject (TypeError).
- `kind=aborted` on AbortError + AbortSignal passthrough.

**Notable absence vs enrichment:** no `unparseable_response` synthesised-error case. Contact-history's wrapper doesn't synthesise a `server` error for a malformed 200 — it intentionally absorbs the case as `items: []`. Three of the fourteen tests pin this directly.

**Verification:** 541/541 SPA tests passing (+14 from 527), svelte-check 0 errors / 248 files, eslint clean.

**Doc footprint:** this entry. No ADR (test-only change), no CLAUDE.md / memory updates (no rule moved).

**Remaining verification gaps from the review:** `api/config.ts` outcome tests, `utils/frequency.formatFrequency` tests. I17 (validators boolean → string|null) and the 11 nits (N1–N11) still outstanding per session 54.

### Session 55 work (2026-05-14) — `api/enrichment.ts` outcome tests landed

Highest-priority of the four verification gaps from session 54 (per the project rule "test the error path first for enrichment code"). One new test file, no production code change, no architectural shift.

**New `lib/api/enrichment.test.ts` (10 cases):**

- GET URL construction — uses `M0XYZ/P` to exercise `encodeURIComponent` (the `/` proves the encoder is actually firing; bare `M0XYZ` wouldn't).
- `kind=ok` happy path with full `EnrichmentResult` (callsign, country, station, hamnut + qrzlookupservice sources).
- `kind=ok` always-200 contract per ADR 0017 #12 — `country_source: 'none'` + `station_source: 'none'` + no country/station object surfaces as a normal `ok` arm, not a separate failure branch. This was the load-bearing case worth pinning because the daemon's "always-200 on provider failure" decision is a contract the SPA can't see in the type system.
- `kind=server` with synthesised `unparseable_response` code when a 200 body isn't a JSON object (daemon/proxy fault, not a client mistake — surfaces as `server`).
- `kind=validation` on 400 with daemon `code` + `message` envelope.
- `kind=server` on 500 with daemon `code` + `message` envelope.
- Synthesised `unknown_error` + `HTTP 502` fallback when an error body isn't parseable.
- `kind=network` on fetch reject (TypeError).
- `kind=aborted` on AbortError (manual abort).
- AbortSignal passthrough — verifies the signal handed to `enrichCallsign` reaches `fetch`.

**Verification:** 527/527 SPA tests passing (+10 from 517), svelte-check 0 errors / 246 files, eslint clean.

**Doc footprint:** this entry, review document Status block + new "Session 55" block. No ADR (test-only change), no CLAUDE.md / memory updates (no rule moved).

**Remaining verification gaps from the review:** `api/config.ts` outcome tests, `api/contact-history.ts` outcome tests, `utils/frequency.formatFrequency` tests. I17 (validators boolean → string|null) and the 11 nits (N1–N11) still outstanding per session 54.

### Session 52 work (2026-05-12) — `cmd/smd` code-review cleanup (12 findings closed)

Narrow review pass — `cmd/smd/main.go`, `cmd/smd/doc.go`, `cmd/smd/main_test.go` only. Counts: 0 critical / 3 major / 4 medium / 5 minor. All addressed in one session, three commits. Review document: `docs/reviews/cmd-smd-2026-05-12.md`.

**Major (3):**

- **M1 — `os.Exit(1)` in bridge init/start bypasses deferred cleanup.** The bridge subsystem block at the M3a.1 wiring site was using `os.Exit(1)` on `Initialize` or `Start` failure, which violated `run()`'s own anti-pattern preamble. By that point the DB is open, lookup refresher is started, forwarder workers are running, and the logger is flushable — `os.Exit` skipped all of those. Fix: return wrapped errors via `errors.New(op).WithErr(err).WithMsg(...)`; add `defer bridgeSvc.Stop()` (idempotent via `sync.Once`) so the error-return path tears the bridge down too. Happy-path teardown still uses the explicit `bridgeSvc.Stop()` call in the shutdown sequence; the defer is a no-op then.
- **M2 — Doc-comment surgery.** A prior merge had swapped two doc blocks: the long block above `ensureDefaultLogbook` opened describing `spawnForwarderWorkers` and switched subjects mid-paragraph; the block above `spawnForwarderWorkers` was the severed tail of the original spawn doc (started mid-sentence with `// loader validates Name uniqueness…`). Untangled both — each function now has its own intact doc.
- **M3 — `doc.go` two ADRs out of date.** Original copy still claimed "Unix domain socket" only, listed "Wails desktop apps, wsjtx-bridge, importer-style CLIs" as the client surface, and said rig control lives in a separate process. Rewritten against ADR 0001 (browser SPA embedded in daemon, served at `GET /` when `Protocol=tcp && ServeSPA=true`), ADR 0013 (bridge runs as in-process daemon subsystem in the default deployment; package-import-graph boundary), and ADR 0017 (enrichment pipeline). Now also mentions the mailer and lookup pipeline that the old copy didn't acknowledge.

**Medium (4):**

- **Med1 — Deferred close callbacks assigning to outer `err`.** Logger and DB close defers used `if err = …` instead of `if err := …`. `run()` returns `runErr` not `err`, so output was unaffected today, but it left a footgun if the return signature ever changed. Aligned with the refresher-Stop defer (which already uses `:=`).
- **Med2 — Hand-wired enrichment pipeline.** Orchestrator + refresher + providers are constructed inside `buildEnrichment` via struct literal, not through `iocdi`. Added a doc paragraph explaining why: instantiation is gated by `cfg.Lookup.Hamnut.Enabled` and `cfg.Lookup.Chain[i].Enabled` with a Name → concrete-type dispatch in the chain loop, neither of which the container expresses cleanly. The "grep for container.Register won't surface these" line is the discoverability pointer.
- **Med3 — Asymmetric lifecycle shapes.** `run()`'s doc now lists each subsystem (DB / forwarders / bridge / refresher+providers / mailer / hub) and the rationale for its lifecycle pattern — DB splits Initialize from Open because the DSN depends on the loaded config; forwarders are per-config-entry N so don't fit DI; bridge takes the loaded `cfg.Bridge` snapshot which isn't available at `container.Build` time; mailer gates internally via `Enabled()` from `cfg.Smtp.Host`; hub is a fan-out primitive, not a service.
- **Med4 — Process-lifetime globals annotated.** `qrz.UserAgent`, `adif.ProgramVersion`, `iocdi.SetLiteralProvider` all get `intentionally process-lifetime` notes so a future reader doesn't suspect missing restore-on-exit cleanup.

**Minor (5):**

- **Min1 — Duplicated precedence ladder.** `loadConfig` and `resolveConfigPath` both implemented "explicit flag → SM_WORKING_DIR → cwd." Extracted `defaultConfigPath()` helper; both functions route through it. Adding a third tier (e.g. `XDG_CONFIG_HOME`) is now a one-place change. Side benefit: `loadConfig` shrank from 30 → 14 lines by collapsing two near-identical env/cwd branches into one. Tiny correctness improvement: env branch now uses `filepath.Join` instead of string concat (no double-slash if the env var ends with `/`).
- **Min2 — Double `workerCancel` reads as redundant.** Both the `defer workerCancel()` and the explicit call in shutdown are load-bearing — defer covers error-return path; explicit call participates in ordered teardown (must run before `server.Shutdown` and the WG drain). Added a one-line comment so future readers don't suspect cruft.
- **Min3 — Named returns on `loadConfig`.** Signature is now `(cfg config.Config, firstRunPath string, err error)`. Reader doesn't have to descend into the body to learn what the string means.
- **Min4 — `hub.Close()` has no timeout.** Reviewer suggested wrapping in the same `select { <-done / <-ctx.Done() }` pattern used for worker drain. Inspected the implementation: `hub.Close` holds `h.mu` only briefly for a synchronous "close every subscriber channel" loop, no wg.Wait, no drain — non-blocking by construction. Added a comment explaining the assumption so a future Close-that-drains rewrite gets reviewed against it.
- **Min5 — `ShutdownTimeoutSec=0` defence.** `applyDefaults` sets it to 10 today, so it won't trigger in practice — but a hand-edited config or a future schema where defaults are missed would make `ctx.Done()` fire immediately and spam-log "workers did not drain within shutdown timeout" on every clean shutdown. Defensive `if shutdownTimeout <= 0 { shutdownTimeout = 10 * time.Second }` floor.

**Verification:**

- `go build ./...` clean.
- `go vet ./cmd/smd/...` clean.
- `go test ./cmd/smd/...` green (the full spawn-worker + ensureDefaultLogbook + loadConfig matrices all still pass — `loadConfig`'s named-returns refactor preserves the public signature shape so test call sites needed no changes).

**Doc footprint:** review document, this session-handoff entry. No ADR, CLAUDE.md, or memory changes — the work is hygiene; no architectural rule moved.

**Next picks-up point unchanged from session 51:** continue live operator testing on the FTdx10 with the i18n + Mode Mappings panel; callsign-stacking design conversation (parked, FIFO confirmed, right-drawer placement now discussed but not implemented).

### Session 51 work (2026-05-11) — Rig-mode → ADIF translation shipped; daemon enum is now data-driven

Five-stage work that closes the mode-mapping gap surfaced during M3a.4 live testing. Architecture as settled with the operator: bridge stays pure (rig literal on the wire); SPA resolves; per-rig translation table in a layered shape (rigdef-shipped defaults + operator overrides in config.json, merged at the daemon).

**Stage 1 — modes catalogue refactor (`internal/enums/modes`):**

- New embedded `adif-modes.json` baseline shipping the full ADIF 3.x main-mode list (~50 entries: FT8 / FT4 / FST4 / Q65 / JS8 / JT65 / MSK144 / MT63 / OLIVIA etc. promoted from MFSK submodes to first-class main modes per current spec) + the submode→parent map (USB/LSB/PSK31/PSK63/C4FM/DMR/etc.).
- `modes.go` rewritten to load the catalogue from JSON via `go:embed` at package init; new `LoadOverride(workingDir)` merges an optional `$SM_WORKING_DIR/modes.json` operator override on top (additive for main_modes, override-wins for submodes). The hardcoded Go enum is gone — `IsValidMode` / `IsValidSubMode` / `GetModeBySubmode` query the loaded set; the `Mode` / `SubMode` type wrappers + the 10 common-mode `const` references stay for typed call sites. New `MainModes()` / `SubModes()` snapshot helpers.
- `cmd/smd/main.go` calls `modes.LoadOverride(cfg.DataDir)` at startup; malformed override is loud-fatal so syntax errors surface at boot rather than as silent validation rejections later.
- Existing tests updated (FT8/FT4/FST4 moved from "valid submode" to "valid main mode" expectations) + new `TestLoadOverride` covering missing/malformed/extend/override-wins paths. All Go tests green.

**Stage 2 — daemon types + config merge + `/v1/config` exposure:**

- `types.ModeMapping{Mode, SubMode}` (operator-friendly ADIF pair) added to `internal/types`. `cat.RigDefinition.ModeMappings` switched from a cat-local type to `types.ModeMapping` (cat now imports types — fine since types is dependency-free). `types.BridgeConfig.ModeMappings map[string]map[string]types.ModeMapping` adds the operator-override layer (outer key = driver id, inner key = rig literal mode string).
- Both Yaesu rigdefs (`yaesu-ftdx10.json`, `yaesu-ft710.json`) gained a `mode_mappings` block with shipped defaults: USB → SSB+USB, LSB → SSB+LSB, CW-U/CW-L → CW (no submode — ADIF doesn't refine sideband for CW), FM/FM-N/DATA-FM/DATA-FM-N → FM, AM/AM-N → AM, RTTY-L/RTTY-U → RTTY, DATA-L/DATA-U → FT8 (most common digital protocol; operator overrides via the Mode Mappings UI when running something else), PSK → PSK+PSK31.
- `config.validateBridge` validates mode_mappings unconditionally (operator may configure mappings ahead of enabling CAT): non-empty Mode in the daemon's catalogue, SubMode in the catalogue or empty.
- `internal/api/handler_config.go` `BridgeInfo` block grows three fields: `Driver` (rig id), `RigModes` (the rigdef's MAINMODE value_mappings.value list, the SPA uses it to render rows in Mode Mappings sub-tab), `ModeMappings` (merged view: rigdef defaults overlaid with operator overrides for the configured driver). New `bridgeInfoFor(cfg)` helper consolidates the construction; old `rigNameForDriver` deleted as its caller moved. New `cat.RigModes(def)` helper extracts the unique rig-mode strings from a rigdef's MAINMODE markers.
- PUT `/v1/config` accepts updates to `bridge.mode_mappings`: validates each pair (400 with rig-key context on invalid Mode / SubMode), diffs against the rigdef's shipped defaults so only operator-deviations get persisted to config.json. Diff approach means future rigdef updates pick up unchanged keys for the operator automatically; keys the operator has changed stay sticky.

**Stage 3 — SPA configState hydration + displayedState mode resolution:**

- `lib/api/config.ts` `BridgeFields` grows `driver?: string`, `rig_modes?: string[]`, `mode_mappings?: Record<string, AdifModePair>`. New `AdifModePair` interface mirrors the daemon's ModeMapping shape.
- `configState.bridge` is a new sub-state (`BridgeView` class with `driver`, `rigModes`, `modeMappings`) — additive next to the existing `configState.station.enabled` / `.rigName` which are still hydrated from the same wire-level `bridge` block but stay in their historical position to avoid a wider refactor. `configState.applyResponse` hydrates the new fields.
- `displayedState.mode` and `.subMode` now produce **ADIF-resolved** values: when CAT is live they look up `catState.mode` in `configState.bridge.modeMappings` and return the mapping's `(Mode, SubMode)` pair (missing-mapping falls through with `catState.mode` literal so the issue surfaces visibly); when CAT is off they run the operator-friendly `manualState.mode` through `resolveModeAndSubmode` so callers always get an ADIF pair regardless of which side is in charge.
- `lib/utils/mode.ts` `SUBMODE_TO_MODE` table tightened to match the daemon's catalogue: FT8/FT4/FST4/FST4W/Q65/OLIVIA/CONTESTIA/DOMINOEX/FSQ/JS8/MT63/THOR/THROB/HFSK/HHELL/PKT removed (they're ADIF main modes now); USB/LSB/PSK31/PSK63/etc. + DIGITALVOICE/HELL family submodes stay. `resolveModeAndSubmode('FT8')` now produces `{mode: 'FT8', subMode: ''}` instead of the old `{MFSK, FT8}`.
- `QsoPanel.svelte`: mode dropdown local var derives from `displayedState.subMode || displayedState.mode` (operator-friendly view — shows "USB" for SSB+USB, "FT8" for FT8); dynamic modes list includes the current value if it's outside the baseline 9 entries so a custom mapping (e.g. operator's My Station table mapping DATA-U → "JS8") still displays. QSO submit no longer calls `resolveModeAndSubmode` since `displayedState.mode` / `.subMode` are already ADIF.
- Existing `mode.test.ts` updated (FT8/FT4 pass-through expectations); new `displayed.test.ts` cases covering CAT-live mapping lookup, missing-mapping fallthrough, CAT-off resolveModeAndSubmode passthrough. 467/467 SPA tests green.

**Stage 4 — My Station → Mode Mappings sub-tab:**

- New `'modes'` section in `MyStationPanel.svelte`'s sub-tab list, slotted between Equipment and CW. Persists active-section to sessionStorage like the others.
- Local edit state `editingModes: Record<string, {mode, submode}>` keyed by rig mode string. Snapshots from `configState.bridge.modeMappings` when the operator navigates INTO the tab — that way an external config refresh doesn't stomp in-progress edits. Refreshes from the daemon's response on successful save.
- Table renders one row per `configState.bridge.rigModes` entry: rig literal in a monospace cell on the left, two free-text inputs for ADIF MODE and SUBMODE on the right. Inputs are free-text (not select) because the daemon's catalogue is ~50 main modes + 20 submodes — a dropdown would either be cluttered or arbitrarily curated. Validation happens daemon-side on save; toast surfaces field-named 400s.
- "Update" button: builds the PUT payload (drops rows where Mode is blank — daemon's diff layer treats those as "back to rigdef default"), calls `putConfig({bridge: {enabled, driver, mode_mappings}})`, hydrates from response on success, toasts on validation/server/network failure. `putConfig`'s payload type widened to include `'bridge'`.
- Friendly empty-state when bridge isn't configured (`rigModes.length === 0`) — short paragraph explaining the table populates once CAT is enabled with a recognised driver.

**Stage 5 — verification + doc sweep:**

- Full Go test suite green. `go vet` clean. `go build ./...` clean.
- Full SPA test suite green (467/467 across 27 files). ESLint clean. svelte-check clean (238 files). `npm run build` clean.
- `docs/v2-design/cat-serial-reuse.md §8` updated — the parked rig-mode-translation entry is now marked SHIPPED with the architecture-as-built captured.
- This session-handoff entry.

**Resume points for next session:** continue live operator testing on the real FTdx10 with the new Mode Mappings panel. Confirm: the panel populates with the rigdef's shipped defaults; editing DATA-U/L from FT8 → PSK31 (or whatever) round-trips through PUT /v1/config; QSO submit with CAT live carries the resolved ADIF MODE/SUBMODE correctly. If anything's off, fix in-session. Otherwise the bridge subsystem v1 and its operator-config surface are both done — natural next pieces are M3b external integration (`cmd/udp-bridge`, `cmd/importer`, multi-rig daemon-side wiring) but those are operator-priority dependent.

#### Parked design conversation — callsign stacking (pile-up workflow)

Operator (session 51) proposed a feature: a queue of partial / full callsigns the operator types while listening to a pile-up. `+` pushes to the queue; submit or ESC pulls the next one onto the Callsign input. Replaces a paper queue. SPA-only feature; no daemon work; per-session ephemeral (sessionStorage tier).

Design state (mid-conversation when paused for the daemon startup issue):

- **Ordering: FIFO confirmed** — first heard / first written, first to work. `+` pushes to the bottom; submit/ESC pulls from the top.
- **Visual placement: 3 options outlined, not yet chosen.** Layout change so explicit operator approval needed:
  1. Horizontal pill row directly beneath the Callsign input — compact, single line wrapping, leftmost chip is the next-to-load. Clicking a chip jumps it to the front. Doesn't take vertical space from the form.
  2. New InfoPanel tab "Queue" — vertical list with chip + remove button per row. Uses panel space that's currently visible for Country/Worked/Details. More room for queue management (manual reorder, in-place edit of partials).
  3. Sidebar column right of QsoPanel — always visible, biggest layout change. Best for heavy contest sessions but squeezes the card width.
- **Open: partial completion mechanic.** Operator hears "G4..." then later "G4ABC" — do they replace the queued partial, or work the partial first and let the contacted station complete it on the air? Pile-up reality is usually the latter (call "?G4" and let them complete it).
- **Open: persistence scope.** sessionStorage survives F5 but resets on tab close. Probably right (operator wants fresh stack each session), but worth confirming whether daemon restart / hard reload should preserve.
- **Open: keybindings.** `+` to push, ESC to advance/clear, submit to advance — but is there a way to skip without losing? Probably need a "delete current" key.
- **Aligns with:** keyboard-first feedback memory; enrichment pipeline already has `Callsign.onenrich` (Tab) so partials → fulls retrigger lookup cleanly.

Pick up the conversation by confirming the visual placement (option 1, 2, or 3) and the partial-completion mechanic, then sketch the implementation.

#### Daemon-side error codes + SPA i18n machinery — SHIPPED session 51 continuation (2026-05-12)

Originally parked as a "if string-tuning gets annoying" refactor; promoted to ship-now when the operator declared Tumbuka and Chichewa as planned localization targets. Single-user-single-language is no longer the steady-state assumption; the i18n machinery now has a real driver.

**Wire-shape change** (backwards-incompatible, coordinated SPA + daemon upgrade):

- `EventRigDisconnected` payload: `{reason: string}` → `{code: RigDisconnectedCode, details?: map[string]string}`. Codes: `rig_no_data`, `serial_port_error`.
- `EventBridgeError` payload: `{message: string}` → `{code: BridgeErrorCode, details?: map[string]string}`. Codes: `unknown_driver`, `serial_config_invalid`, `missing_init_command`, `missing_read_command`, `serial_open_failed`, `init_write_failed`, `identity_unrecognised`, `identity_mismatch`.
- Daemon `publishDisconnect` / `publishBridgeError` signatures now `(code, details)`; all 10 call sites in `internal/bridge/pipeline.go` updated to pass typed codes + named substitution details. Daemon log lines stay technical English (operator-debugging audience; not localized).

**New SPA i18n machinery**:

- `frontend/logging/src/lib/i18n/index.ts` — `t(key, details?)` render helper + `setLocale` / `getLocale`. ~50 LOC, no external dependency (i18next/svelte-i18n would be overkill for SM's string surface). Fallback chain: current locale → English baseline → `[missing: key]` sentinel.
- `frontend/logging/src/lib/i18n/en.ts` — master catalogue, 10 keys for the bridge codes (8 errors + 2 disconnects). Templates use `{name}` placeholders substituted from details. Operator retunes wording by editing this file; HMR picks it up; no daemon restart needed.
- `frontend/logging/src/lib/states/bridge.svelte.ts` — toast handlers now call `t(\`bridge.disconnected.${code}\`, details)` and `t(\`bridge.error.${code}\`, details)`. The hardcoded "Rig disconnected: " and "Bridge: " prefixes are gone — those moved into the catalogue templates.

**Locale selector**: hardcoded to 'en' for now. Wire it into `configState.station.locale` when Tumbuka (`tum.ts`) and Chichewa (`ny.ts`) catalogues are added — operator picks via a Settings UI; persists via `/v1/config`. Don't ship the selector before the second catalogue exists.

**Tests**: `i18n.test.ts` covers render, placeholder substitution, missing-key fallback, locale switching. `bridge.test.ts` fixtures updated to use new payload shape; substring assertions pin stable parts of templates rather than exact wording so the operator can retune `en.ts` without breaking tests. `pipeline_test.go` fixtures assert codes + details rather than message-string substrings. 474/474 SPA tests + full Go suite green.

**Adding a new locale** (when ready): drop `lib/i18n/tum.ts` (or `ny.ts`), register it in the `catalogues` map in `index.ts`, ship a Settings UI for the operator to pick it. Missing keys fall back to English silently. No daemon work needed.

**Future Phase B** (parked again, not now): the same code-and-i18n pattern can extend to non-bridge toasts (config errors, QSO submit feedback, enrichment, email send, etc.). Do it incrementally as each surface is touched, not in one big PR.

#### Adjacent fixes shipped same session

- **`lastRigDisconnected` hub cache** added paralleling the M3a.3 `lastBridgeError` cache. Fixes a real operator-observed gap: the pipeline's `announcedDisconnect` dedup flag fires `rig-disconnected` exactly once per silent-window, so a second / refreshed SPA tab that subscribed AFTER the first emission saw nothing. Now the cache replays to every new subscriber. Distinct from the bridge-error cache: `lastRigDisconnected` clears on the next `EventRigState` arrival (auto-recovery — a cached disconnect would be stale and would misleadingly toast new subscribers when the rig is actually fine, since rig-back-online is the implicit-reconnect signal per ADR 0009). Two new tests pin the contract (`TestHub_CachesRigDisconnectedForLateSubscriber`, `TestHub_ClearsRigDisconnectedCacheOnRigState`); race-detector hammer clean.
- **QRZ `Initialize` soft-disable on session-fetch failure.** Pre-fix, a session-fetch failure (DNS timeout / QRZ.com down / wrong credentials) returned a hard error that aborted `cmd/smd` at startup — directly violating the load-bearing invariant "Enrichment never blocks logging." The operator hit this with `dial tcp: lookup xmldata.qrz.com: i/o timeout`; daemon refused to start at all. Fix: `qrz.Service.Initialize` now logs a warning, flips `Config.Enabled = false`, and returns nil; the cmd/smd existing `if !qrzSvc.Config.Enabled { continue }` check skips the provider cleanly. Daemon starts; QSOs log; QRZ enrichment is silently skipped until the network comes back or credentials get fixed. Operator log shows the warning. Aligns with the documented cmd/smd comment that already said "QRZ Initialize disables itself rather than returning an error" — pre-fix the code contradicted its own comment. Existing test `TestInitialize_SessionKeyFailureDisablesService` updated to assert the new soft-disable contract.

### Session 50 work (2026-05-11) — M3a.4 (SPA bridge consumer + live rig test) shipped; M3a closed

The bridge's last sub-milestone. The SPA's `bridge.svelte.ts` stub became a real EventSource consumer; daemon-side fixes surfaced during live testing closed two gaps (30s SSE cutoff, rig-name visibility); My Station Equipment panel now mirrors CAT-live rig + power values. Operator confirmed live on the FTdx10 — VFO updates on dial, mode reflects, identity/power populate read-only, QSO ADIF carries MY_RIG and TX_PWR correctly.

**SPA bridge consumer (`frontend/logging/src/lib/states/bridge.svelte.ts`):**

- Replaced the stub `BridgeState` with a real EventSource consumer wired to `/v1/rig/events`. Lifecycle: `startBridge()` (called from `app.svelte` onMount after `fetchConfig()` settles) wires a `$effect.root` that tracks `configState.station.enabled` and opens/closes the EventSource accordingly; `stopBridge()` tears down both the source and the effect.root (test seam, not used in production).
- Three listeners as designed by ADR 0010: `rig-state` (field-by-field merge into `catState` via a private `mergeRigState(payload)` helper — partial payloads preserve prior values; `*bool` splitOverride preserved via explicit `payload.splitOverride !== undefined` existence check so the OFF case survives the wire's omitempty semantics), `rig-disconnected` (`toasts.warn(...)` + flips `rigResponding=false`, leaves `connected` alone since transport is still up), `bridge-error` (`toasts.error(...)`, doesn't touch bridgeState flags).
- `bridgeState.connected` flips true on `open` event, false on `error` event (along with `rigResponding=false` since transport down means no rig signal either). Browser handles SSE auto-reconnect; no retry loop in the SPA.
- `bridgeState.rigResponding` flips true on every `rig-state` event (implicit reconnect per ADR 0009), false on `rig-disconnected` or transport `error`.
- 19 Vitest cases in `bridge.test.ts` against a synchronous `FakeEventSource` stub: lifecycle (no construct when disabled, construct when enabled flips true, close on flip back to false, `stopBridge` tears down, `startBridge` idempotent), connected flag (open/error transitions), full-payload merge, partial-payload preserves prior values, omitted-splitOverride preserves prior value, `splitOverride=false` is preserved (the wire-protocol-critical regression — pins review #1's bool→*bool decision on the SPA side too), rigResponding flips on every rig-state, JSON-parse fault tolerance, rig-disconnected toast + rigResponding flip, rig-disconnected leaves connected=true, subsequent rig-state implicit reconnect, bridge-error toast, bridge-error doesn't touch flags.

**Source-of-truth for `station.enabled` — daemon-authoritative.** Initial live test exposed that `configState.station.enabled` was hard-coded `false` SPA-side with no toggle and no daemon mirror, so the EventSource never opened despite `bridge.enabled: true` in config.json. Fixed by exposing `bridge.enabled` in the `/v1/config` response (`BridgeInfo.Enabled` field on the daemon side, mirrored into `configState.station.enabled` via `applyResponse`). Operator flips CAT on/off via `config.json` + daemon restart, matching the SMTP-creds / hardware-config "operator owns config.json directly" pattern per ADR 0003. The stale "SPA-only, in-memory" comment in `config.svelte.ts` was updated to point at the new wire-driven source.

**Daemon-side fixes surfaced during live testing:**

- **30-second SSE cutoff (Issue 1).** Daemon log showed `rig SSE write-deadline clear failed; stream subject to WriteTimeout` followed by every SSE request lasting exactly `30007ms`. Root cause: `responseRecorder` in `internal/api/middleware.go` wraps `http.ResponseWriter` to track status/bytes for the access log, but didn't implement `Unwrap() http.ResponseWriter`. So `http.ResponseController.SetWriteDeadline(time.Time{})` in the bridge handler couldn't traverse the wrapper to reach the underlying connection — it returned `ErrNotSupported`, the server's `WriteTimeout=30s` from config stayed active, and every 30s the connection was force-closed. SPA reconnected silently, but any frequency push that arrived in the reconnect gap was lost. Fix: added `func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }`. Post-fix SSE durations are unbounded (~85s in one log; closes only when the operator closes the tab). Bridge handler's `SetWriteDeadline(time.Time{})` still logs "feature not supported" warning at start because there's still a layer somewhere returning that — the warning is now misleading but the *effective* behaviour is correct (writes are not being deadlined; durations confirm). Track-down of the spurious warning deferred — it's noise, not a functional issue.
- **Rig-name visibility (Issue 3).** `BridgeInfo` on `/v1/config` grew a `RigName` field, resolved daemon-side from `cat.Lookup(cfg.Bridge.Cat.Driver).Name` (e.g. "Yaesu FTdx10" for the `yaesu-ftdx10` driver). Empty when bridge is disabled or driver unknown. New helper `rigNameForDriver(driver string) string` in `internal/api/handler_config.go`. SPA mirrors into `configState.station.rigName`; new `displayedState.rigName = isLive ? configState.station.rigName : ''` derivation. The QSO ADIF MY_RIG fallback chain updated from `displayedState.rigIdentity` (the bare ID-mapped value, "FTdx10") to `displayedState.rigName` (the rigdef-name, "Yaesu FTdx10") — more descriptive in logged QSOs.
- **Equipment panel CAT mirroring (Issue 3 continued).** My Station → Equipment panel's Rig field now displays `displayedState.rigName` read-only when CAT live; reverts to editable `configState.loggingStation.myRig` when off. Default TX Power input displays `catState.power` read-only when CAT live; reverts to editable `configState.station.defaultPower` when off. Both follow the readonly-when-CAT-enabled rule the operator established at session start. The Rig field is a conditional `{#if displayedState.isLive}` swap between two `ValidatedInput` invocations (read-only branch passes `value=` without `bind:`, editable branch uses `bind:value=`) to keep the visual layout identical. The Default Power field is a single raw input with conditional `value`, `readonly`, and `oninput` (only writes back to config when CAT-off).

**Parked work (operator decision):**

- **Rig-specific mode → ADIF translation.** FTdx10 reports modes like `DATA-U`, `DATA-L`, `CW-U`, `CW-L`, `RTTY-U`, `RTTY-L`, `FM-N`, `AM-N`, `DATA-FM`, `DATA-FM-N`, `PSK` that don't map 1:1 to the daemon's strict ADIF main-mode enum (`AM`/`CW`/`FM`/`RTTY`/`SSB`/`DIGITALVOICE`/`MFSK`/`PSK`/`HELL`/`PACKET`). The SPA's mode `<select>` widget shows blank when CAT pushes a value outside its 9-entry dropdown list; QSO submit would 400 against the daemon's enum if the rig is on `DATA-U`. Operator's decision: per-rig translation table in each `internal/cat/rigs/*.json` mapping rig-raw modes to `(ADIF MODE, ADIF SUBMODE)` pairs; bridge looks the table up before emitting `rig-state` so the SPA only ever sees ADIF-valid pairs. Full context + open questions written to `docs/v2-design/cat-serial-reuse.md §8` — that's the spec for the follow-up session.

**Verification:**

- Vitest: 464 tests across 27 files green.
- ESLint: clean.
- svelte-check: 0 errors, 0 warnings, 238 files.
- `npm run build` clean; dist regenerated.
- `go build ./...` clean; `go test ./internal/api/...` clean; `go vet` clean.
- Operator-driven live test on the FTdx10: dial turn → SPA VFO updates live (band change confirmed working post-Unwrap fix); mode change → SPA reflects (with caveat that DATA-U etc. show blank in the dropdown until the mode-mapping work lands); My Station Equipment panel shows "Yaesu FTdx10" + 50W read-only when CAT live; logged QSO has MY_RIG="Yaesu FTdx10" and TX_PWR=250 (50W × 5 amp multiplier).

**Resume points for next session:** rig-specific mode → ADIF translation (the parked work above). Design surface is in `docs/v2-design/cat-serial-reuse.md §8`; expected shape is a `mode_to_adif` table in each rigdef JSON, bridge resolution before the rig-state event, possible new operator-override widget for the "rig says DATA-U, I'm running FT8" case. M3a (bridge subsystem v1) is **closed**; no remaining wire work.

---

### Session 49 work (2026-05-10) — M3a.2 (Serial + CAT pipeline) shipped

M3a.1's stub emitter replaced with the real serial+CAT pipeline. The bridge now reads AUTO-mode rig pushes from the configured serial port, decodes them via `internal/cat`, filters to the SPA-relevant field set per ADR 0019, and publishes typed `rig-state` / `rig-disconnected` events through the existing hub to SSE subscribers.

**Layering preserved (per `bridge.md` §3c).** `internal/serial` stays bytes-I/O only with no protocol knowledge; `internal/cat` stays pure codec with no I/O; `internal/bridge` is the glue that opens the port, encodes the INIT command via cat, writes via serial, reads via serial, decodes via cat, filters, publishes.

**New files in `internal/bridge/`:**

- `pipeline.go` — `(s *Service) runPipeline(ctx)` orchestration: `cat.Lookup(driver)` → `buildSerialConfig(rigdef.Serial)` → `s.openClient(serialCfg)` → `cat.Encode(def, "INIT")` → `client.WriteCommandBytes`. Then `(s *Service) readLoop(ctx, client, def)` runs the steady-state read+decode+publish chain. Helpers: `mapStatusToPayload(cat.Status)` (the rigdef-tag → ADR-0019-payload filter — 8 tags forwarded, anything else dropped); `parseFreqHz` (9-char zero-padded → int64 Hz); `vfoLabelToTag` ("VFO-A"/"VFO-B" → "A"/"B"); `buildSerialConfig` (rigdef-JSON-friendly forms → typed `serial.Config`); `parityFromString` / `stopBitsFromInt` / `delimiterFromString` (the JSON↔enum translation that lives in the glue layer because cat is pure codec and serial is pure I/O — putting it elsewhere would break one of those two rules).
- `fake_serial_test.go` — `fakeSerial` struct implementing `serial.Client`. `feedLine([]byte)` for tests to enqueue rig responses; `recordedWrites()` to assert INIT was sent; `Close()` to simulate terminal disconnect. Mu-guarded so feed-after-close can't panic. `installFakeSerial(s *Service)` helper wires it into a Service via the new injectable opener seam.
- `pipeline_test.go` — 10 new tests cover INIT-sent (`recordedWrites()` first entry == `"AI1;ID;"`), identity decode (`ID0800` → `RigIdentity = "FT-710"`), freq decode (`FA014250000` → `VfoA = 14250000` with all other fields zero — pins the partial-payload-merge contract), mode/split/VFO/power dispatch through value-mapping tables, 30s liveness timeout fires `rig-disconnected`, dedup of consecutive disconnects (only one event per silence window), recovery (data after disconnect → `rig-state` event without explicit "reconnected" signal — SPA's existing rigResponding flip per ADR 0009), terminal `Close` → `rig-disconnected` with reason, unknown driver clean exit, `openClient` failure clean exit, rigdef→serial.Config translation pinned for FT-710 (port/baud/databits/delimiter/timeout).

**Termination semantics (canonical):**

- Parent ctx cancelled (Service.Stop / daemon shutdown): exit silently. SSE subscribers see hub close cleanly.
- 30s without a line: publish `rig-disconnected` once, keep waiting; next successful read clears the `announcedDisconnect` flag.
- Terminal serial error (`ErrClosed`/EIO): publish `rig-disconnected` with the error reason in the payload, exit pipeline goroutine.
- Initial open / unknown driver / missing INIT / INIT write fail: log loudly and bail. M3a.3 promotes these to `bridge-error` events.

**Modified files:**

- `service.go` — added injectable `openClient func(serial.Config)(serial.Client, error)` field on Service, defaulting to a `serial.Open` wrapper; tests substitute `fakeSerial` via `installFakeSerial`. `Start` now spawns `runPipeline` instead of the deleted `runStubEmitter`. The `stubEventInterval` package var and the entire `runStubEmitter` function removed.
- `service_test.go` — `TestStart_Enabled_PublishesStubEvents` reframed as `TestStart_Enabled_PublishesPipelineEvents`, drives an FT-710 ID push through the fake. `TestStop_ClosesHub_AndDrainsSubscribers` and `TestStart_Idempotent` updated to install a fake serial instead of relying on `/dev/null` + 5s stub ticker.
- `handler_test.go` — `newHandlerTestService` returns the fake; `TestHTTPHandler_StreamsStubEvents` → `TestHTTPHandler_StreamsPipelineEvents` feeds an FT-710 ID push and asserts the resulting SSE frame carries `"rigIdentity":"FT-710"`.

**Verification:**

- `go build ./...` clean.
- `go test -race ./...` all packages green (~45s for the integration suite).
- `go test -race -count=5 ./internal/bridge/` clean — no flakes on the new `livenessTimeout` paths.
- 19 bridge tests pass (10 new pipeline + 9 reframed); both ADR 0013 boundary tests (forward + reverse) still green.
- `go vet ./internal/bridge/...` clean.

**Resume points for next session: M3a.3.** Per `milestones.md` M3a.3 scope:

- **Bootstrap poll on SSE-open** (ADR 0019 active-snapshot model). On each new SSE connection, send a CAT poll command via `internal/cat` so the SPA tab opens with current rig state rather than waiting for the operator to wiggle the dial. Open question: use the rigdef's existing `READ` command (the 7-query macro `FA;FB;ST;VS;MD0;MD1;PC;`) or add a `BOOTSTRAP`/`IF` command for a single-round-trip query — operator's call.
- **`bridge-error` event emission** for operator-actionable conditions (port permission denied, unknown driver, missing INIT in rigdef, INIT write failure, build-serial-config failure, identity mismatch). NOT for transient retries. The runPipeline log-and-bail paths from M3a.2 become `publishBridgeError + return`.
- **Multi-subscriber fan-out test** with 5+ concurrent SSE clients all seeing the bootstrap event then live updates. Validates the existing hub's fan-out under realistic concurrency.

Then M3a.4 (SPA `bridge.svelte.ts` consumer + live rig test) closes out M3a.

---

### Session 48 work (2026-05-10) — ADR 0019 + M3a.1 (bridge subsystem skeleton) shipped

CAT/bridge implementation kicked off. Two design docs landed (ADR 0019 + milestones.md restructure), then M3a.1 of the four-sub-milestone bridge breakdown shipped end-to-end with a code-review pass.

**Design — ADR 0019: bridge subsystem v1 internal design.** Walked through the open questions ADR 0013 / 0010 left unsettled and consolidated decisions:

- **Stateless filter** — no cache, no delta computation. Bridge decodes incoming rig pushes, filters to SPA-relevant fields, emits as SSE event, forgets. SPA's `catState` (Svelte 5 `$state`) provides the value-persistence the cache was solving for. Snapshot-on-connect via active CAT poll at SSE-open time, not a cached send.
- **Read-only v1** — no PTT awareness, no inbound command path. SPA observes; rig dial remains source of truth. PTT tracking, disconnect-safety-release, arbitration, inbound `POST /v1/rig/cmd` all deferred together with their actual driver (FT8 stack TX cycles, future voice keyer, click-to-tune). Operator decision after walking through the trade-offs — speculating PTT design without a real consumer would be the wrong answer.
- **One SSE frontend in v1** — rigctld-compat TCP and NDJSON Unix-socket frontends both deferred until a real driver appears. `invariants.md` "two-frontend bridge shape" qualified with ordering note: eventual canonical shape is two frontends, v1 ships one.
- **Multi-rig API-aware, single-rig implementation** — internal API takes a rig identifier from day one (every call site threads it). HTTP route stays singular `/v1/rig/events` for v1; grows to `/v1/rig/{id}/events` when multi-rig hardware lands.
- **Performance — not a design risk.** SSE over loopback TCP delivers <1ms per-event end-to-end; the v1 lag bridge.md §7b warned about was Wails IPC, not applicable to Svelte 5 + EventSource.

ADR 0010's "Bridge-side current-state cache" subsection revised by 0019; ADR 0013 unchanged (topology decision separate from internal v1 design). bridge.md banner updated. Memory `project_sm_serial_bridge.md` rewritten to reflect read-only-v1 + parked items.

**Milestone breakdown — `docs/v2-design/milestones.md` M3 restructured.** Old framing of `cmd/sm-serial-bridge` as a separate binary was obsolete per ADR 0013 + 0019. New shape:

- M3a (current) — bridge subsystem v1 (read-only, SPA-only). Four sub-milestones:
  - M3a.1 — package skeleton + config + stub SSE  *(shipped this session)*
  - M3a.2 — Serial + CAT pipeline (replaces stub emitter with real rig data)
  - M3a.3 — Pipeline → SSE + bootstrap poll + bridge-error events
  - M3a.4 — SPA consumer + live rig test
- M3b (deferred) — `cmd/udp-bridge`, `cmd/importer`, multi-rig daemon-side wiring.

**M3a.1 — package skeleton + config + stub SSE.** New `internal/bridge/` package:

- `internal/types/bridge.go` — `BridgeConfig`, `BridgeSerialConfig`, `BridgeCatConfig` (Enabled / Serial.{Port,Baud} / Cat.Driver, defaults pattern matches `SmtpConfig`).
- `internal/config/config.go` — `Bridge` field on `Config`; `Serial.Baud` defaults to 38400; `validateBridge` enforces "Enabled=true → Port + Driver required" (loud startup failure rather than runtime).
- `internal/bridge/doc.go` — package overview pointing at ADR 0013 + 0019 + boundary discipline.
- `internal/bridge/events.go` — `EventName` constants (`rig-state`, `rig-disconnected`, `bridge-error` per ADR 0010), `Event` struct, three payload types (`RigStatePayload` / `RigDisconnectedPayload` / `BridgeErrorPayload`).
- `internal/bridge/hub.go` — internal pub/sub fan-out typed for `bridge.Event`, mirrors `events.Hub` shape (slow-subscriber eviction, idempotent unsub, close-on-stop). Build-specific not generic — typed Event avoids `any`-assertion at the SSE handler.
- `internal/bridge/service.go` — `Service` lifecycle (`Initialize` → `Start` → `Stop`, all idempotent). `Enabled()` reports `cfg.Enabled` (nil-safe). `Subscribe()` for SSE handlers. M3a.1 stub-event ticker spawning a hardcoded `rig-state` every 5s when enabled — replaced by real serial+CAT pipeline in M3a.2.
- `internal/bridge/handler.go` — SSE handler. Standard SSE headers, write-deadline cleared, observes both `r.Context().Done()` and `shutdownCh` (the bug `/v1/events` also fixed — `http.Server.Shutdown` doesn't cancel idle SSE contexts), keepalive every 30s, `http.Flusher.Flush()` after each event.
- `internal/api/server.go` — accepts `*bridge.Service`, registers `GET /v1/rig/events` conditionally on `br.Enabled()`, wrapped with `limitEventSubscribers` (caps at `MaxEventSubscribers` default 16, same as `/v1/events`).
- `cmd/smd/main.go` — constructs bridge with `bridge.New(cfg.Bridge, loggerSvc)`, Initialize/Start with `workerCtx`, Stop in shutdown sequence before `server.Shutdown`.
- **3 test files**: `service_test.go` (9 lifecycle tests — Initialize idempotent + missing-logger, Enabled flag, Start enabled/disabled, Stop closes hub, post-Stop Subscribe returns closed channel), `handler_test.go` (3 SSE tests — headers, streams stub events, shutdownCh closes stream), `boundary_test.go` (**2 ADR-0013-enforcement tests** that AST-walk imports asserting the package boundary — bridge MUST NOT import `database/sqlite` / `forwarding` / `qsoservice`, and conversely no internal package outside an `{bridge, api}` allowlist may import bridge).

14 bridge tests pass. Full Go suite green. Daemon builds. `bridge.enabled: false` deployments stay inert (no route, no goroutine, no port acquisition); `bridge.enabled: true` (with valid port + driver) emits stub events that `curl -N http://localhost:8080/v1/rig/events` shows.

**Code-review pass — `docs/reviews/internal-bridge.md`.** Seven findings; all addressed in same session:

1. **Subscriber cap on `/v1/rig/events`** — wrapped with `limitEventSubscribers`, same `MaxEventSubscribers` (16) cap as `/v1/events`. Pattern divergence resolved.
2. **Marshal-failure logging** — `writeSSEEvent` promoted to method on `*Service`; failures now log at WARN with event name (matches `/v1/events`). Doc-comment now describes actual behaviour instead of contradicting the code.
3. **Redundant `logger` parameter** — `HTTPHandler` signature reduced to `(shutdownCh)`; uses `s.logger`.
4. **`Stop()` idempotency race** — `sync.Once` + `stopDone` channel. First caller runs teardown; concurrent callers wait on `stopDone` before returning. "Stop returned, therefore stopped" holds for every caller.
5. **Reverse boundary-test scope** — generalised from hard-coded {sqlite, forwarding, qsoservice} list to walking `internal/*` recursively with `{bridge, api}` allowlist. Future internal packages get enforcement automatically.
6. **doc.go conceptual paths** — replaced with real paths (`internal/database/sqlite`, `internal/forwarding`, `internal/qsoservice`).
7. **Confusing dual-success path** — `TestStart_Disabled_NoPublisher` collapsed to a single clear contract (silence within timeout window).

Race-detector hammer (`-race -count=5`) clean on the new `stopOnce` path. Full Go test suite green. Daemon link clean.



Operator asked for prettier and eslint coverage on TS/JS/Svelte files, with type-checked rules turned on at the outset ("If there are issues we can fix them all. It allows us to knock out subtle bugs now"). Pre-existing scaffolding was a half-step: `.prettierrc`, `.prettierignore`, `eslint.config.js` (flat), and `.eslintrc.cjs` (legacy v8) were all staged-but-never-committed leftovers, **none of the deps were actually installed**, and the flat config still pointed at a non-existent local `.gitignore` plus a `src/lib/wailsjs/**` ignore that the milestone-1 restructure made dead. This session finished the wiring.

**Tooling configured.**
- Deleted `.eslintrc.cjs` — superseded by the flat config; v8-style with Wails-era references.
- `eslint.config.js` rewritten:
  - `...ts.configs.recommendedTypeChecked` (verified: `recommendedTypeChecked` is a 3-config array on the typescript-eslint default export, spread is required and the casing is camelCase).
  - `eslint-config-prettier` placed last so prettier wins on formatting rules and eslint owns logic-only rules — the recommended pairing.
  - `parserOptions.projectService: true` for type-aware analysis, with `tsconfigRootDir: import.meta.dirname` to anchor the project.
  - Svelte-specific block uses `parser: ts.parser` + `extraFileExtensions: ['.svelte']` + `svelteConfig` for cross-file Svelte parsing.
  - Root config files (`eslint.config.js`, `svelte.config.js`, `vite.config.ts`) ignored — they sit outside the `src/` tsconfig include and the projectService can't resolve them.
  - `@typescript-eslint/no-unused-vars` extended with `argsIgnorePattern: '^_'` / `varsIgnorePattern: '^_'` / `caughtErrorsIgnorePattern: '^_'` so the `_input` / `_init` convention works as expected.
- `.prettierrc` extended with `plugins: ["prettier-plugin-svelte"]` and an `overrides` block routing `*.svelte` to the `svelte` parser. Style settings unchanged (singleQuote, semi, 100-col, 4-space, es5 trailing commas, always arrow parens).

**Deps installed.** `prettier`, `prettier-plugin-svelte`, `eslint`, `@eslint/js`, `typescript-eslint`, `eslint-plugin-svelte`, `eslint-config-prettier`, `globals`. 98 transitive packages; 0 vulnerabilities.

**Scripts added** to `package.json`:
- `lint` → `eslint .`
- `lint:fix` → `eslint . --fix`
- `format` → `prettier --write "src/**/*.{ts,js,svelte,svelte.ts,svelte.js,css,html,json}"`
- `format:check` → `prettier --check ...`

**One-time prettier `--write` across `src/`.** As agreed, normalised the entire tree in one pass — ~209 files reformatted, the rest already conformant. The diff is large but the cost is paid once; future PRs see only their own changes.

**Lint pass surfaced 29 errors across 8 files. All fixed in-session.**

| File | Issue | Fix |
|---|---|---|
| `lib/api/enrichment.ts:72-73` | `'hamnut' \| 'cache' \| 'none' \| string` — literals collapsed by `string` (`no-redundant-type-constituents`) | Tightened `country_source` to strict literal union (daemon controls values exhaustively); kept `station_source: string` with a doc-comment listing known values (provider names are open-ended). |
| `lib/api/qso.test.ts` × 4 | `vi.fn(async () => x)` with no awaits inside (`require-await`) | Converted to `vi.fn(() => Promise.resolve(x))` / `Promise.reject(new TypeError(...))`. The `async` was non-load-bearing. |
| `lib/states/toasts.svelte.ts:46` | `Map<…>` flagged by `prefer-svelte-reactivity` | Kept plain `Map`, added why-comment + line-disable: `timers` is `clearTimeout` plumbing, never read from a template/$derived/$effect, so SvelteMap would add unused reactivity. |
| `lib/ui/components/Vfos.svelte:65` | Inline `onCommit={(hz) => …}` flagged `unsafe-assignment` | Added explicit `(hz: number)` annotation. svelte-eslint-parser doesn't propagate `VfoInput`'s `onCommit?: (hz: number) => void` prop type through inline snippet callbacks (known parser gap). |
| `lib/ui/panels/QsoPanel.svelte:46` | `let mode = $state(displayedState.mode)` + mirror-`$effect` flagged by `prefer-writable-derived` | Kept the pattern with extended why-comment + line-disable: a plain `$derived` is read-only and would break `<Mode bind:value={mode}>`; the two-effect shape (input mirror + conditional output mirror) is the standard Svelte 5 idiom for a two-way bind across two reactive stores under the ADR 0009 "writes only when editable" gate. |
| `lib/ui/components/Mode.test.ts` × 2, `Vfos.test.ts` × 10 | `as boolean` / `as string` redundant assertions | Auto-fixed by `lint:fix`. |

**Convention going forward.** When a lint-disable is warranted, write a why-comment immediately above the `eslint-disable-next-line` line. Never a bare disable. The two disables landed in this session are both for legitimate cases where the rule's heuristic is wrong for our pattern (Svelte's two-way bind across stores; non-reactive plumbing inside a reactive module).

**Final verifications:**
- `npm run lint` → 0 errors
- `npm run format:check` → all clean
- `npm run check` (svelte-check) → 0 errors, 0 warnings, 222 files
- `npm test` → 376 passed across 21 files

**Resume points (for next session):**

**SessionPanel Stages A–D + keyboard shortcuts + focus restoration all shipped 2026-05-09.** Operator live-tested the full flow (log → row click → edit overlay populates → save → row updates in place; ESC clears, Ctrl+Enter submits, cursor lands on Callsign at every state transition) and confirmed it works.

**Operator's roadmap (settled end of 2026-05-09):**

1. Get `smd` daemon working robustly (current focus — recent profiling expedition was step one)
2. Logging SPA polish + completion (mostly there; ongoing visual / UX iteration)
3. Logbook SPA (separate client; not yet started — see `feedback_logging_vs_logbook_scope.md` for the scope split)
4. FT8 stack — depends on CAT being in place
5. FT8 SPA — separate client for the FT8 workflow

CAT/bridge is the next major implementation block in service of the FT8 path. Contest-mode networking is parked indefinitely as a future consideration.

~~**Next session: M3a.2 — Serial + CAT pipeline.**~~ **Closed 2026-05-10 (session 49) — see Session 49 entry above.** Replaced `runStubEmitter` with the full `runPipeline` + `readLoop` chain: `cat.Lookup` → `buildSerialConfig` → `s.openClient` → `cat.Encode("INIT")` → write → read+decode+publish. 30s data-flow timeout fires `rig-disconnected`; SPA-relevant field filter via `mapStatusToPayload`; hardware-free `fakeSerial` test harness. 10 new tests; full Go suite + race hammer green.

**Bridge subsystem progress to date:**

| Layer | Already in tree | M3a status |
|---|---|---|
| `internal/cat/` | ✅ codec + per-rig DB (yaesu-ft710, yaesu-ftdx10) | — |
| `internal/serial/` | ✅ transport layer + buffer pool | — |
| `internal/bridge/` | ✅ M3a.3 shipped (M3a.2 pipeline + `TriggerBootstrap` writes rigdef READ on each SSE-open; `bridge-error` events for operator-actionable failures; once-per-pipeline identity verification; hub `lastBridgeError` cache replays startup errors to late subscribers; `fakeSerial` test seam) | M3a.4 wires SPA `bridge.svelte.ts` consumer + live rig test |
| `internal/api/` | ✅ `/v1/rig/events` route registered (capped via `limitEventSubscribers`) | — |
| `cmd/smd/main.go` | ✅ Bridge DI wired (Initialize → Start → Stop on workerCtx) | — |
| `frontend/logging/src/lib/states/bridge.svelte.ts` | ✅ stubbed | M3a.4 fleshes out (real EventSource consumer) |

**Topology design context (recorded in `memory/project_sm_field_master_topology.md`):**

The eventual shape is **multi-station contest** (N slaves each with rig + writer; 1 master aggregator), not just single-station portable. Current implementation target is the N=1 case (single station). Settled architectural decision: **CAT stays in-process — `cmd/bridge` is NOT shipped ahead of need.** Rig + daemon co-located on every writer in both shapes. Open future work (not blocking M3a): cross-host `/v1/qso` auth (when field→master forwarding lands), real-time dupe-query API for contest mode, offline dupe-check resilience.

**Other open items (not blocked on CAT or contest):**

- **HamQTH / QRZCQ provider chain expansion** — TBD per operator (still useful as a small adjacent task, ~30-60 min for HamQTH alone).
- **Cross-host `/v1/qso` auth** — open design work for whenever the field→master forwarder lands. Today the endpoint is unauthenticated (loopback-only). Cross-host needs at minimum a shared secret per forwarder pair.

**Other open items (carry-over) — logging-app scope:**

- HamQTH / QRZCQ providers — chain expansion under the existing `CallsignProvider` interface. Operator's tag: TBD (2026-05-09).
- ~~M3a.1 (bridge skeleton + config + stub SSE)~~ **Closed 2026-05-10. Code-review cleanup also shipped (7 findings addressed). Ready for M3a.2.**
- ~~`TestSchedule_ReleasesSlotAfterFn` flake~~ **Closed 2026-05-09.**
- ~~Daemon profiling for stress-test~~ **Closed 2026-05-09 — 5 bugs + 1 config tuning shipped, ~75× win on operator's mixed workload. Substrate-side knobs parked under Future scope.**
- ~~`?refresh=true` on `/v1/enrich/callsign`~~ **Closed 2026-05-09.**
- ~~SPA-side mirror of zone validation~~ **Closed 2026-05-09.**
- ~~SessionPanel Stage C (email-out)~~ **Closed 2026-05-09, live-tested against a real SMTP server.**
- ~~SessionPanel Stage D (edit overlay)~~ **Closed 2026-05-09.**
- ~~APP_SM_REQUEST_QSL parser gap (operator flag silently dropped on submit)~~ **Closed 2026-05-09.**
- ~~Keyboard shortcuts (ESC=Clear, Ctrl+Enter=Log) + focus restoration~~ **Closed 2026-05-09.**

**Parked — logbook-app scope (do NOT add to `frontend/logging/`):**

- "QSOs awaiting QSL request" view — `APP_SM_REQUEST_QSL` persists end-to-end, but listing / filtering historical contacts by it is a logbook concern, not a logging-app feature.
- Edit-history viewer over `qso_history` — consuming the audit table from session 40 is a logbook concern. Daemon endpoint `GET /v1/qso/{uuid}/history` doesn't exist yet either; build daemon-side once, surface in the logbook app when it lands.

See `memory/feedback_logging_vs_logbook_scope.md` for the scope rule (operator-confirmed 2026-05-09).

**Future scope (no immediate plan):**

- **Per-field encryption-at-rest for SMTP + provider passwords.** Operator flagged it 2026-05-09 alongside SMTP config landing — wants a security assessment first. Plaintext in `config.json` matches the existing pattern (QRZ password etc.) for now. See `memory/project_sm_security_assessment.md`.
- **Swap to `encoding/json/v2` once it un-flags.** Parked 2026-05-09 during the profiling session. v2 is in Go 1.26.2's source tree but gated behind `GOEXPERIMENT=jsonv2` and explicitly "experimental, not subject to Go 1 compatibility promise." It's a separate package (`encoding/json/v2`), not a drop-in substitute for `encoding/json` — would need to audit every import site + set the build flag in Taskfile + any CI. Cost-benefit isn't there today: post-parser-fix profile shows JSON at ~8% of allocations, throughput is fsync-bound, ~50% JSON-cost reduction would save ~4% of total allocs and zero throughput. Revisit when v2 un-flags (likely Go 1.27 or 1.28); at that point it becomes a genuine drop-in win. The cleaner JSON-cost reduction in the meantime is **shrink `additional_data`** by stripping promoted-column fields from the marshaled blob (the model→type adapter overlays columns over the unmarshaled blob, so the blob's copy is dead weight) — also parked, lower priority since current sizes are fine for personal-scale.
- **Daemon profiling rig.** Wired 2026-05-09 (`cfg.Server.EnableProfiling` flag, `cmd/loadgen` harness). Findings shipped as code + config fixes (see "Profiling expedition" continuation entry below for full chain). Net: 75× improvement on operator's mixed Tab→submit workload. Remaining future-revisit knobs (substrate-side, operator's call): `synchronous=OFF` (drops WAL fsync, risk last-tx loss on power cut), batched commits (changes API contract), faster substrate (NVMe).

### Session 47 continuation (2026-05-09) — InfoPanel tab-click bug fix

Operator reported during live-testing: the tab icons showed `cursor-pointer` on hover but didn't respond to clicks — only the text label switched tabs. Root cause: tab structure was `<div class="tab-item">` wrapping an icon SVG and a sibling `<button class="tab-button">`; `cursor-pointer` lived on the wrapper but the click handler was on the button — so the icon area lied with the cursor and clicks there hit dead air.

Fix: collapsed wrapper into the button itself. `<button role="tab" class="tab-item">` now contains both icon + label `<span>`, the entire row is one click target. Dropped the now-redundant `.tab-button { cursor-[inherit] }` rule from `app.css` (was a workaround for the old split structure). No test changes needed (no DOM tests covered this); 376 tests still pass; svelte-check + lint + format all green.

### Session 47 continuation (2026-05-09) — SPA-side zone validation mirror

Closes the carry-over from session 46's daemon-side validation. The daemon is the backstop; this gives operators a red border the instant they type out-of-range, without waiting for Update.

- New `lib/validators/zone.ts` — `isValidCqZone` (1–40), `isValidItuZone` (1–90), `isValidDxcc` (0–522). Single `inRange(min, max)` factory; same trimmed-then-digits-only-then-range logic as the daemon's `isValidZone(s, min, max)`. Empty allowed (operator clearing the field). No silent transform — `41` shows red rather than auto-becoming `4`.
- `lib/validators/zone.test.ts` — 26 cases mirroring the daemon's table-driven test (bounds, interior, empty/whitespace, non-numeric, fractional, negative).
- `MyStationPanel.svelte` — three `validator={passthrough}` swapped for the new validators on the CQ Zone / ITU Zone / DXCC `<ValidatedInput>` blocks.

### Session 47 continuation (2026-05-09) — `?refresh=true` on `/v1/enrich/callsign`

The operator's "the cache is wrong" escape valve. A regular Tab returns cached data per ADR 0017's three-state read; `?refresh=true` bypasses both layers' caches and forces upstream calls.

Daemon-side:
- `internal/lookup/orchestrator.go` — added `EnrichRefresh(ctx, callsign)` alongside `Enrich(ctx, callsign)`. Both delegate to a private `enrich(ctx, callsign, force bool)` core. `readCountry` / `readStation` accept `force` and skip the cache fetch when true; the upstream result is treated as a cold miss so the existing writeback path overwrites the cache row. Async stale-refresh scheduling is naturally suppressed (force-mode never sets `staleHit`). On upstream failure: returns `source=none` rather than silently falling back to the cached row — the operator asked for fresh data; returning stale data would defeat the purpose.
- `internal/api/handler_enrich.go` — `?refresh=true` parsed via strict equality (`== "true"`); anything else (`"1"`, `"TRUE"`, `"yes"`, missing) routes through cache-aware `Enrich`. Strict semantics keep the contract obvious.
- 6 new tests across orchestrator + handler covering bypass-fresh-cache-and-overwrite, upstream-down-no-fallback, no-cache-row-cold-miss-shape, query-param-routes, default-path-unchanged, non-true-values-treated-as-default.

SPA-side: not yet wired (no "Refresh" button on the country panel). Daemon is ready; one-button SPA hookup is a future micro-task.

### Session 47 continuation (2026-05-09) — npm dep bumps in three stages

Operator asked for a way to keep dependencies current. Settled on `npm outdated` + `npm update` plus convenience scripts:
- New npm scripts: `deps:check` (`npm outdated || true`), `deps:update` (`npm update`).
- New Taskfile targets: `frontend:deps:check`, `frontend:deps:update` for consistency with the existing `frontend:install` / `frontend:dev` / `frontend:build` shape.

Major bumps were staged across three commits for bisectability:
- **Stage 1 — in-range only** (`npm update`): tailwind 4.2.4→4.3.0, @tailwindcss/vite 4.2.4→4.3.0, @types/node patch, svelte-check patch. No code changes.
- **Stage 2 — Vite 6→8 + svelte-vite-plugin 5→7 paired.** Required a clean `node_modules` + `package-lock.json` reinstall because the stale `vite-plugin-svelte-inspector@4` peer-dep from v5 created a phantom conflict during the in-place upgrade. `vite.config.ts` needed no changes; build is faster (~960ms vs ~2.1s previously).
- **Stage 3 — TypeScript 5→6.** Surfaced one issue: TS 6 stopped tolerating the `import './styles/app.css'` side-effect import without an ambient declaration. Added `src/vite-env.d.ts` with the standard `/// <reference types="vite/client" />` (the canonical Vite scaffold pattern; brings in CSS / asset module declarations). `typescript-eslint` 8.59 is compatible with TS 6 — no lint config changes.

Final state: every dep is on its latest published version; svelte-check 0 errors / 228 files; vitest 402 tests; lint + format clean.

### Session 47 continuation (2026-05-09) — SessionPanel Stage A: daemon SMTP foundation

Operator clarified during scope-pinning that SessionPanel is "list of session QSOs (table similar to WorkedPanel) + edit overlay + email-out as ADIF" — same UX as v1's SessionPanel (icon click → "Sending…" toast → "Sent"). Required a daemon-side SMTP service. Designed the mailer as general-purpose so future alert paths (forwarder backlog, refresher repeated failures, daemon-health probes) plug in via the same `Send` primitive — not session-email-specific. Operator flagged this explicitly (alerts use case) so the boundary is right from day one.

- **`internal/types/email.go`** — new `SmtpConfig` (host/port/username/password/from/default_recipient/starttls/timeout_sec). Empty `Host` = mailer disabled; default port 587 (RFC 6409 submission); `From` required when host set.
- **`internal/email/`** — new package. `Service` is a singleton mailer with `Send(ctx, Message)`. Connect-and-send semantics (one SMTP session per call, no pooling — at this daemon's volume the per-message handshake cost is negligible). Stdlib-only — `net/smtp` + `crypto/tls` + a hand-rolled MIME multipart envelope (text/plain body + base64-wrapped attachments per RFC 2045). `ErrMailerDisabled` (host empty), `ErrInvalidMessage` (no recipient/subject), wrapped transport errors otherwise — all mapped distinctly by the handler so operators see specific status codes.
- **`internal/email/email_test.go`** — 9 tests with an in-process SMTP fake (just enough protocol to capture MAIL FROM / RCPT TO / DATA). Covers: nil service / empty host → `ErrMailerDisabled`; missing recipient/subject → `ErrInvalidMessage`; happy path with attachment → multipart envelope verified; no-attachment branch → plain text envelope; default recipient snapshot; dial-failure path returns wrapped error.
- **`internal/config/config.go`** — `Smtp types.SmtpConfig` field on `Config`; defaults (port 587, timeout 30s); `validateSmtp` (host empty = ok, otherwise from + port + timeout required).
- **`internal/api/server.go`** — `New` gained `mailer *email.Service` parameter; new route `POST /v1/session/email`.
- **`internal/api/handler_session_email.go`** — handler. Body shape `{to, adif, subject?, filename?}`; daemon stamps subject + filename defaults from UTC now (so SPA doesn't need timezone-aware formatting). Status codes: 200 ok, 400 validation (missing required field, invalid email, malformed JSON), 503 mailer_disabled, 502 smtp_failure (transport).
- **`internal/api/handler_session_email_test.go`** — 7 handler tests covering all status paths.
- **`cmd/smd/main.go`** — constructs the mailer from `cfg.Smtp` after config load; passes to `api.New`.
- Per-comment doc on `buildMimeEnvelope` pinning the assumption that unchecked `fmt.Fprintf` writes are safe because `bytes.Buffer.Write` never errors — load-bearing if the function is ever refactored to take `io.Writer` directly (streaming straight to SMTP DATA).

**Memory captured:** `memory/project_sm_security_assessment.md` (and indexed in `MEMORY.md`) — secrets-at-rest encryption deferred until a proper security assessment lands. New secret fields (SMTP password, etc.) land plaintext in `config.json` matching the existing pattern (QRZ password etc.). Don't propose crypto / vault / keyring without checking memory first.

### Session 47 continuation (2026-05-09) — SessionPanel Stage B: SPA session-QSO state + table render

Stage A covers the daemon transport; Stage B is the panel UI without the email-trigger UI (Stage C ships that).

- **`lib/states/sessionQsos.svelte.ts`** — singleton with `items: SessionQso[]` (`$state`), `count: number` (`$derived`), `add` / `update` / `clear` methods. Persists to `sessionStorage` under `sm.session.qsos` (matches `SessionTimer`'s lifecycle: F5-survival, cleared on tab close — that's what defines "session end" in the v1 carry-forward UX). Hydrates on construction; `try/catch` on every storage call so private-browsing / quota edge cases are graceful.
- **`SessionQso` shape** — uuid (UUIDv7 from daemon's submit response), callsign, name, freqHz (raw Hz; SessionPanel formats), band, rstSent, rstRcvd, mode, timeOn, qsoDate, country, distanceKm (string for display tolerance — empty when grids unavailable), adif (full record formatted at submit time, captured for future email-out so we don't re-fetch every QSO from the daemon).
- **`lib/utils/frequency.ts`** — promoted `formatFrequency(hz)` out of `Vfos.svelte` (second consumer landed); `Vfos.svelte` now imports it. Per the project's "build specific until a second use case shows up" rule.
- **`lib/states/enrichment.svelte.ts`** — added `activeDistanceKm` `$derived` alongside `activeBearing`; same path-aware logic, returns km as a string. Snapshotted onto `SessionQso.distanceKm` at submit time — once the next Tab fires, this value would otherwise reset.
- **`lib/ui/panels/SessionPanel.svelte`** — replaces the stub. 10-column table (Callsign / Name / Freq / Band / Send / Rcvd / Mode / Time On / Country / Distance), `tabular-nums` for digit alignment, newest-first via `slice().reverse()`. Empty-state message ("No QSOs logged this session.") when no rows. Read-only — Stage C will add the recipient input + send button on the InfoPanel header bar.
- **`lib/ui/panels/QsoPanel.svelte`** — `case 'stored'` branch snapshots all 13 fields (uuid from outcome, freqHz from txFreqHz, band from `frequencyToBand`, country from `enrichmentState.result?.country?.name`, distanceKm from `enrichmentState.activeDistanceKm`, full adif string captured) into `sessionQsosState.add(...)` BEFORE the existing `qsoDraft.clear()` / `enrichmentState.clear()` wipe their sources.
- **`lib/ui/panels/InfoPanel.svelte`** — Session badge `count` wired to `sessionQsosState.count` (was hardcoded `0`).
- **`lib/states/sessionQsos.test.ts`** — 9 tests covering add / update / clear, count derivation, oldest-first ordering, and the sessionStorage round-trip (including a full-fidelity field round-trip).

Operator live-tested Stage B (logging QSOs, watching them populate the table + badge) and confirmed it works as expected.

### Session 47 continuation (2026-05-09) — SessionPanel Stage C: email-out

Stage A landed the daemon SMTP transport; Stage C wires the SPA's send button on the InfoPanel header so operators can email the session ADIF to their QSL manager.

- **`internal/api/handler_config.go`** — `ConfigResponse` gained a `Mailer MailerInfo` block. `MailerInfo` is the SPA-visible subset only (`enabled` bool + `default_recipient` string); host/port/username/password/from are deliberately NOT on the wire — exposing them would leak the SMTP password or invite SPA-side edits to credentials it doesn't own. Server-managed (PUT body's `mailer` block is ignored). Sourced from the live `email.Service` rather than the cfg snapshot so a future "reload SMTP without restart" flow stays correct without a parallel branch.
- **`internal/api/handler_config_test.go`** — 3 new tests: nil mailer surface, configured mailer surface (with grep-asserts that password/host/from never leak into the JSON body), PUT body's `mailer` block ignored.
- **`lib/api/config.ts`** — `ConfigResponse` extended with `MailerFields { enabled, default_recipient? }`.
- **`lib/states/config.svelte.ts`** — new `MailerView` class (`enabled` + `defaultRecipient`, both `$state` so the SessionPanel can reactively show/hide the email controls). `applyResponse` hydrates it with defensive fallback for older daemon builds without the block.
- **`lib/api/session-email.ts`** — discriminated-outcome wrapper for `POST /v1/session/email`. Outcomes: `sent | mailer_disabled | invalid | smtp_failure | server | network`. Mirrors `lib/api/qso.ts` and `lib/api/config.ts` conventions.
- **`lib/api/session-email.test.ts`** — 10 tests covering all status paths + body-shape passthrough for optional `subject` / `filename`.
- **`lib/ui/panels/InfoPanel.svelte`** — recipient `<input type="email">` + paper-plane button on the right of the tab strip (rendered only when Session is active AND `configState.mailer.enabled`). `recipient` seeds once via `$effect` from `configState.mailer.defaultRecipient` then becomes operator-owned (subsequent config changes don't clobber the operator's typing). Send flow: sticky "Sending…" info toast → on response, dismiss the sticky and push `Sent to <recipient>` (info) / error variants per outcome (`Email not configured` / `Email send failed; check daemon logs` / `Cannot reach daemon`). ADIF body built from `sessionQsosState.items.map(q => q.adif).join('')` — the per-row `adif` string captured at submit time avoids a re-fetch from the daemon.
- **`canSend`** gating: `mailer enabled && session count > 0 && recipient non-empty + contains @ && !sending`. Belt-and-braces — the button is hidden entirely when mailer disabled, the input is empty-by-default before the seed lands, the `@` check is the obvious-typo guard so a 400 round-trip isn't paid for whitespace-only input.

### Session 47 continuation (2026-05-09) — SessionPanel Stage D: edit overlay

Stage D is the SessionPanel's row-click edit flow. Independent state singleton per the operator's "completely independent of the draft" rule — a half-finished edit must never bleed into the in-progress draft.

- **`lib/states/qsoEdit.svelte.ts`** — singleton class with `$state` working-copy fields (uuid, callsign, name, qth, country, comment, mode, freq, freqRx, band, rstSent, rstRcvd, qsoDate, qsoDateOff, timeOn, timeOff, rig, rxPwr, notes, requestQsl) + lifecycle flags (open, loading, saving). `populate()` hydrates from a daemon GET response and converts ADIF canonical formats (YYYYMMDD → YYYY-MM-DD, HHMM → HH:MM) for the form components. `beginOpen(uuid)` flips open=true + loading=true so the modal renders instantly with a spinner while the GET resolves. `close()` resets every field — defence against a stale value leaking into the next open(). `toPatchBody()` emits the editable subset.
- **`lib/states/qsoEdit.test.ts`** — 11 tests covering populate/beginOpen/close/toPatchBody, the date+time format conversions, and the missing-fields default path.
- **`lib/api/qso-update.ts`** — `fetchQso(uuid)` + `patchQso(uuid, body)` discriminated-outcome wrappers. Outcomes include `not_found` (404), `duplicate` (409), `validation` (400), `server` (5xx), `network`.
- **`lib/api/qso-update.test.ts`** — 13 tests covering all status paths for both fetch + patch.
- **`lib/ui/components/QsoEditOverlay.svelte`** — modal-style component. Dim backdrop (`fixed inset-0 z-50 bg-black/40`), ESC + click-outside + Cancel button dismiss, `role="dialog"` + `aria-modal="true"`. Form layout matches QsoPanel + DetailsPanel content: row 1 Callsign / RST Sent / RST Rcvd / Mode / VFO-A+VFO-B; row 2 Name / QTH / Comment; row 3 Date / Time On / Time Off; row 4 Rig (textarea) / Power (digit-strip) / Request QSL checkbox; row 5 Notes (textarea, full width). VFO pair uses `VfoBox` + `VfoInput` with MHz↔Hz conversion shims local to the file (qsoEditState holds MHz strings; VfoInput speaks Hz). On Save: PATCH → on `ok` update the matching session-list row in place via `sessionQsosState.update` and close; on failure stay open so the operator can retry without retyping. Modal width: 56rem to fit the wider top row.
- **`lib/ui/panels/SessionPanel.svelte`** — rows now `role="button"` clickable (Enter/Space keyboard parity), open the overlay via `fetchQso → populate`. Renders `<QsoEditOverlay />` as a sibling so the fixed-position backdrop sits above the panel's own clipping context. Network/not_found errors toast; double-click guarded.

### Session 47 continuation (2026-05-09) — APP_SM_REQUEST_QSL plumbed end-to-end + types.Qso JSON-tagged

Closes the pre-existing parser gap that the SPA's request-QSL flag was silently dropped on the original POST /v1/qso path. Two plumbing changes:

- **`internal/types/qso.go`** — new `AppSmRequestQsl bool` top-level field with `json:"app_sm_request_qsl,omitempty"`. Round-trips via `additional_data` through the existing `json.Marshal(qso)` adapter; no other adapter changes needed. JSON-only path for the edit overlay even before the ADIF parser fix landed.
- **`internal/adif/adif.go`** — `Record` gained `AppSmRequestQsl string \`adif:"app_sm_request_qsl,omitempty"\``. `QsoToRecord` encodes `bool true → "Y"` (matches the project's existing `Y`/`N` convention used by `SmQsoUploadStatus` etc. — string is the only kind the reflection-based parser/emitter handles); `RecordToQso` decodes `"Y" → true`, anything else → false (strict equality, defensive default).
- **5 unit tests in `adif_test.go`** — emit-when-true, omit-when-false, parse-`Y`-as-true, absent-as-false, full bool→ADIF→bool round-trip.
- **2 integration tests in `handler_test.go`** — `TestSubmitQso_AppSmRequestQslSurvivesGet` (POST with newline-separated SPA-format ADIF carrying `<APP_SM_REQUEST_QSL:1>Y` → GET returns `app_sm_request_qsl: true`) and `TestUpdateQso_AppSmRequestQslPatchPersists` (PATCH with `{"app_sm_request_qsl": true}` → GET returns `true`). Both round-trip paths green.

End-to-end flow now works: SPA emits flag in ADIF → daemon parses → persists via additional_data → GET surfaces it → SPA edit overlay populates the checkbox correctly. Operator can also correct the flag after-the-fact via the edit overlay (PATCH path was already JSON-shaped).

### Session 47 continuation (2026-05-09) — Wire-shape bug: types.Qso JSON is FLAT, not nested

Bug surfaced during live-test: clicking a session row opened the overlay but every field rendered blank. Root cause: `types.Qso` embeds `QsoDetails` / `ContactedStation` / `LoggingStation` / `Qsl` as **anonymous structs**, and Go's `encoding/json` promotes anonymous-struct fields to the top level on marshal. The daemon's GET response is therefore flat — `call`, `band`, `freq`, etc. all appear as siblings of `uuid`, NOT nested under `contacted_station` / `qso_details`. The SPA's `DaemonQsoForEdit` interface assumed nested. Every field read as `undefined`, populate set every working-copy field to empty.

- **`lib/states/qsoEdit.svelte.ts`** — flattened `DaemonQsoForEdit` interface; rewrote `populate()` and `toPatchBody()` to use top-level keys. Doc-comment now pins the wire shape with the cautionary tale so a future contributor doesn't repeat it.
- **`lib/states/qsoEdit.test.ts`** — fixture rewritten with flat shape; doc note about the trap.
- **`lib/api/qso-update.test.ts`** — fixture + assertions updated to flat shape.

**Why unit tests didn't catch it before:** my test fixtures used the same incorrect nested shape my code expected, so populate-of-nested-data into nested-expecting code looked correct. The Go-side integration test (`TestSubmitQso_AppSmRequestQslSurvivesGet`) tested the daemon's flat response correctly, but didn't run through the SPA's parsing — that gap is what the wire-shape fixture mismatch exploited. Tightened: after this fix, all SPA tests use the actual daemon wire shape.

### Session 47 continuation (2026-05-09) — Stale daemon binary diagnostic

Operator reported during live-test: "request QSL not being reflected" after the wire-shape fix landed. Diagnosis path: re-ran `TestSubmitQso_AppSmRequestQslSurvivesGet` with newline-separated ADIF (the exact SPA wire format) — green. Then `pgrep -af smd` showed the running daemon (PID 6446) was a `go run`-cached binary built at 07:44 today, hours before the ADIF parser fix landed. The running daemon didn't have the `AppSmRequestQsl` field on `types.Qso`, the parser plumbing, or the latest embedded SPA assets — so the flag dropped at submit and the GET returned nothing. Operator restarted via `task run:smd`; flag now reflects correctly.

Lesson worth keeping in mind for future debugging: when a fix's tests pass but the running system seems unaffected, check the binary timestamp before chasing further code. `task run:smd` does `go run ./cmd/smd` which compiles a fresh binary on each task invocation, but once spawned it's pinned to that binary — code changes between invocations don't reach the running process.

### Session 47 continuation (2026-05-09) — Profiling expedition: 75× win on the mixed workload

What started as a "Saturday afternoon profiling jaunt" turned into a 5-bug chain. Each fix surfaced the next; each was small, mechanical, and stays out of the operator's flow. New `cmd/loadgen` harness lives at `cmd/loadgen/main.go` (submit-only or mixed mode, configurable concurrency / count / prefix; `cfg.Server.EnableProfiling` flag added behind the same gate as before).

**Five real bugs fixed, in the order they surfaced:**

1. **modernc DSN syntax** (`internal/database/sqlite/internal.go::getDsn`). DSN options used mattn-style flat names (`_busy_timeout=5000`); modernc.org/sqlite ignores those silently and accepts `_pragma=name(value)` syntax instead. Pool=1 had masked the bug — only the connection that ran the runtime PRAGMA had `busy_timeout` set; bumping to pool=16 for stress meant 15 of 16 conns returned SQLITE_BUSY immediately on write contention. Captured in `memory/feedback_sqlite_modernc_patterns.md` as a project-idiom rule for future PRAGMA additions.
2. **ADIF parser allocations** (`internal/adif/parse.go`). `parseRecords` was `make([]Record, 0, 64)` — pre-allocating 64-record capacity (~190 KB per call) when the daemon's POST handler only ever uses 1. Reduced to cap 1 (multi-record bulk imports pay doubling-grow cost, negligible). Plus `buildTagSetter` was building a fresh map of closures per parse via reflection over `Record`'s fields — replaced with `recordSetters` slice computed once at package `init()` from `reflect.TypeOf(Record{})`. Net allocation reduction: **83%** on the submit hot path (8 GB → 1.4 GB over 50k QSOs); p99-max latency dropped 32%.
3. **SQLite planner statistics** (`internal/database/sqlite/service.go::Migrate` + `Close`). `sqlite_stat1` didn't exist on the operator's DB — never analysed. Without it the planner falls back to default heuristics that pick the wrong index (chose `idx_qso_logbook_id` for queries where `idx_qso_active_call` was the right choice, because logbook_id "looks more selective" by index column count). Wired `ANALYZE` after Migrate and `PRAGMA optimize` on Close (SQLite-recommended periodic maintenance). Both non-fatal on failure.
4. **OR-with-LIKE planner confusion** (`internal/database/sqlite/api_context.go::FetchQsoSliceByCallsignWithContext`). `(call = X OR call LIKE 'X/%')` made the planner pick wrong index without stats, then fall back to full table scan once stats were populated. Split into two single-predicate queries (exact match + portable variants) merged in Go. Each branch has a clean predicate that maps to `idx_qso_active_call`.
5. **LIKE → GLOB for portable variants** (same file). SQLite's default LIKE is case-insensitive — even after the split, the LIKE branch still scanned because case-folding prevents index use. Switched to `GLOB 'X/*'` (case-sensitive, recognised as a sargable prefix range). Callsigns are uppercase by validator contract so the case-sensitive match is semantically correct. sqlboiler doesn't expose GLOB so used `qm.Where("call GLOB ?", ...)` raw clause.

**One config tuning:**

- `applyDefaults` MaxOpenConns/MaxIdleConns: 1 → 8 (`internal/config/config.go`). The "sqlite is single-writer" comment was load-bearing under the broken DSN but stale post-fix; 8 captures most of the pool=16 win without wasting fds. Doc-comment captures the cautionary tale.

**Net win on the operator's actual workload (mixed mode = enrich + history + submit per iteration):**

| Stage | Submit p50 | Submit thr/s | History p50 |
|---|---:|---:|---:|
| Original (pool=1) | 650 ms | 143 | — |
| Pool=16 + DSN fix | 65 ms | 1400 | — |
| Mixed-mode | 30 ms | 488 | 335 ms |
| + ANALYZE + GLOB-split | **7.7 ms** | (dup) 11500 | **8.5 ms** |

50k×100 mixed (150,000 total requests) finished in **14 seconds** post-fixes. Submit at scale: 1400 req/s on fresh inserts (fsync-bound, hardware ceiling), 11500 req/s on duplicates (dedupe fast-path skips fsync — useful indicator of read-side throughput). History at scale: holds at 8.5ms p50 with 175k+ rows in qso table, confirming the index fix scales logarithmically as expected.

The runtime PRAGMA at `service.go:131` (`PRAGMA busy_timeout=5000` after Open) is now redundant with the DSN's `_pragma=busy_timeout(5000)` but kept as belt-and-braces. Doesn't hurt; runs once on the first connection at startup.

### Session 47 continuation (2026-05-09) — Keyboard shortcuts + focus restoration on QsoPanel

Operator's ask after Stage D landed: keyboard-driven workflow so hands stay on the keyboard between QSOs (the single-handed pile-up flow). Two paired changes — keyboard shortcuts that match the FormControls buttons, plus focus restoration to Callsign at every form-resetting transition.

- **`lib/ui/panels/QsoPanel.svelte`** — extracted `clearForm()` so the FormControls Clear button and the keyboard shortcut share one path (was previously inlined in the `onClear` prop). Added `<svelte:window onkeydown={handleKeydown} />`:
  - **ESC** → `clearForm()` (no-op while `qsoEditState.open` so the overlay's own ESC handler wins; without that guard, ESC-to-dismiss-overlay would also wipe the live draft beneath it).
  - **Ctrl+Enter / Cmd+Enter** → `submitQso()` (gated on `qsoDraft.canSubmit` to mirror the button's disabled state — the shortcut doesn't bypass validation; macOS gets Cmd via `metaKey` for native feel).
  - `preventDefault` on Ctrl+Enter as belt-and-braces against any future surrounding `<form>`.
- **Focus helper** — new `focusCallsign()` reaches for the input by its stable `id="call"` (passed unchanged as the prop). Doc-comment notes this bypasses component encapsulation pragmatically (it's a personal logging app, the operator's flow matters more than strict encapsulation here) and the conditions under which it would need to be promoted to a component-exported `focus()` (a second Callsign instance ever lands on the page). Called from three places:
  - `onMount` — cursor lands in Callsign on first render of the panel.
  - `clearForm` — ESC and the Clear button both restore focus after the wipes.
  - `submitQso` `case 'stored'` — Ctrl+Enter and the Submit button both restore focus after the post-stored clears, ready for the next QSO.
- **Failure paths intentionally don't move focus.** Duplicate / validation / server / network outcomes leave the form populated and let the operator decide what to fix; auto-snapping back to Callsign would be wrong when the operator may want to inspect what they typed.

`svelte-check`, lint, all 445 tests, SPA + daemon builds — green. Operator live-tested and confirmed.

### Session 46 work (2026-05-08) — Details panel UI

Continued from session 45. The Details tab in InfoPanel was a 7-line stub; this session filled it in as the second enrichment-fed surface after the country panel. Operator supplied a mockup (Power / Rig / Notes / Email / Web Site / Lookup on QRZ.com / CQ Zone / Request QSL) and confirmed the field-source split: Power/Rig/Notes/Request-QSL are operator-typed; Email/Web Site/CQ Zone (+ added ITU Zone) are read-only displays from `enrichmentState`.

**`qsoDraft` additions:**
- `rxPwr: string` ($state) — contacted station's TX power (operator's estimate, watts). Maps to ADIF `RX_PWR`. Digit-strip filter on input.
- `rig: string` ($state) — contacted station's rig / working conditions, free text. Maps to ADIF `RIG`. Operator-typed: QRZ's per-call rig data is sparse + inconsistent, auto-fill would risk stale values.
- `notes: string` ($state) — operator's personal record. Maps to ADIF `NOTES`, distinct from `COMMENT`. The existing `comment` field on qsoDraft already maps to COMMENT (things shared during the QSO); NOTES is the operator's private remarks ("had bad QSB", "first SSB QSO with this op").
- `requestQsl: boolean` ($state) — operator's reminder flag. Custom field; emits `APP_SM_REQUEST_QSL='Y'` when true, omitted when false (no `'N'` noise in additional_data for the common case). A future "QSOs awaiting QSL request" view consumes this.

`qsoDraft.clear()` resets all four. Cleared on submit-stored, on Clear-button, and on QSO start? — answer: only on clear() and on submit-stored, since `startQso()` doesn't reset draft fields (it pins time/date but the operator's typed inputs persist into the QSO).

**ADIF emitter additions (`adif.ts`):**
- `AdifQsoFields` gained `rxPwr?` / `rig?` / `notes?` / `appSmRequestQsl?`.
- Emit order: existing fields → ANT_AZ → contacted-station per-QSO block (RX_PWR, RIG, NOTES) → APP_SM_REQUEST_QSL → EOR. Each gated on non-empty / true so omit-when-blank holds.
- 6 new tests covering each emitter individually, the COMMENT-vs-NOTES distinction, the omit-when-false rule for APP_SM_REQUEST_QSL, and the omit-all-when-unset baseline.

**`DetailsPanel.svelte` (full implementation, ~180 lines).** Two-column flex layout under the existing InfoPanel tab frame:
- **Left column (operator-typed):** Power (small numeric input, digit-strip on `oninput` matching the RST pattern), Rig (3-row textarea, "Working conditions" placeholder), Notes (3-row textarea, "My personal notes" placeholder), Request QSL checkbox.
- **Right column (read-only + link):** Email (`<input readonly>` styled with `bg-surface-disabled cursor-default`, `tabindex={-1}` so the operator's tab-rhythm skips it), Web Site (same shape) + external-link button that calls `window.open(url, '_blank', 'noopener,noreferrer')`, "Lookup on QRZ.com" button (built via `$derived` from `qsoDraft.callsign`, disabled when empty), CQ Zone label:value, ITU Zone label:value.

**External-link icon** — Heroicon outline `arrow-top-right-on-square`, inlined as a Svelte snippet matching the InfoPanel tab-icon pattern. Reused for both Web Site launch and QRZ.com lookup.

**`EnrichmentStation` SPA type extended** with `web?: string` / `lat?: string` / `lon?: string`. The daemon's `types.ContactedStation` already carried these (QRZ provider populates them); the SPA's TypeScript interface just hadn't surfaced them yet. Strict TypeScript over the index signature `[extra: string]: unknown` was rejecting `enrichmentState.result?.station?.web` until the explicit field landed.

**`QsoPanel.submitQso` wire-up.** Passes the four new qsoDraft fields to `formatAdifRecord` with `.trim() || undefined` for the strings (so blank-after-trim → omitted) and `qsoDraft.requestQsl` directly for the boolean. Existing `enrichmentState.activeBearing → antAz` plumbing unchanged.

**Continuation (post-reboot, same day) — daemon-side LoggingStation zone validation.** Closed the long-standing carry-over: malformed `MyCqZone` / `MyITUZone` / `MyDXCC` values previously slipped through `PUT /v1/config` and got emitted on every QSO's MY_* tags, which downstream services (ClubLog, LoTW) reject or quietly mangle.

- `internal/api/handler_config.go` — three new validation blocks before the existing amp/power checks. Empty stays empty (pre-setup state). Whitespace-trimmed via `strings.TrimSpace` before parsing so cut-paste leaks don't reject as "non-numeric". Each rejection returns `400 invalid_field_value` with a structured message identifying the field; the SPA can route this to inline error markers when those land.
- Ranges: CQ Zone 1–40 (CQWW), ITU Zone 1–90 (ITU R-REC-V.6), DXCC 0–522 (operator's correction — ARRL's current list runs 0=None/maritime through 522). The 522 cap will need bumping when ARRL adds a new entity (rare; once every few years; comment in handler flags this).
- `internal/api/validation.go` — single new helper `isValidZone(s, minVal, maxVal)` using `strconv.Atoi`. Atoi naturally rejects non-digit ("37x"), fractional ("291.5"), and empty inputs — caller still gates on empty separately so operators can clear a field. Reused for all three zone types.
- ~20 new test cases in `handler_config_test.go`: a table-driven `TestHandlePutConfig_LoggingStationZoneValidation` covering valid bounds (1, 40, 90, 0, 522), interior values, out-of-range, non-numeric, fractional, negative inputs; plus a `TestHandlePutConfig_ZoneTrimming` that confirms whitespace gets trimmed.

**Continuation (post-validation, same session) — WorkedPanel UI.** Closed the third InfoPanel tab. The daemon endpoint `GET /v1/contact-history?call=X` already existed (handler at `internal/api/handler_contact_history.go`, returning newest-first `ContactHistory[]` capped at `Server.MaxContactHistoryResults` default 100, with optional `?logbook=N` filter that the SPA wrapper deliberately omits — operators almost always want "have I ever worked them"). SPA-side wiring was the entire scope.

- `frontend/logging/src/lib/api/contact-history.ts` — discriminated-outcome fetch wrapper (`'ok' | 'validation' | 'server' | 'network'`) matching the `lib/api/qso.ts` shape.
- `frontend/logging/src/lib/states/contactHistory.svelte.ts` — module-level singleton with `items: ContactHistory[] | null` (`$state`) and `count: number` (`$derived` from `items?.length ?? 0`). The `null` vs `[]` distinction is load-bearing for the panel render: `null` means "no Tab yet, show nothing"; `[]` means "fetched, no prior contacts — show the friendly hint." Both render the badge as `(0)` because that's the operator-correct count, but the panel body differs.
- `frontend/logging/src/lib/ui/panels/WorkedPanel.svelte` — full implementation. Three render states: hidden (items=null), "No prior contacts with this callsign" hint (items=[]), table (items.length>0). Columns Date / Time / Band / Mode / RST S / RST R, `tabular-nums` for digit alignment, ADIF YYYYMMDD → YYYY-MM-DD and HHMM → HH:MM substring formatters (no Date round-trip — avoids browser-TZ surprises on a UTC-by-design value).
- `QsoPanel.handleEnrich` — fires `fetchContactHistory(call)` in parallel with `enrichCallsign(call)`. Cheap (single indexed query, no upstream calls) so no slow-lookup toast needed. Failures silent because empty-history and network-error are the same operator-visible outcome (the panel just stays empty).
- Clear hooks — both submit-stored AND the Clear button now reset `enrichmentState` AND `contactHistoryState`.
- `InfoPanel.svelte` — `tabs` array converted from a `const` to a `$derived` so the `Worked (N)` badge auto-updates from `contactHistoryState.count` without any imperative re-render. Session count stays `(0)` until SessionPanel ships.
- 4 new tests in `contactHistory.test.ts` covering set/clear/count derivation and the items=null vs items=[] semantic distinction.

**Resume points (for next session):**

`SessionPanel` is now the only InfoPanel tab still stubbed. Tracks the operator's current operating session — date/time started, QSO count this session, maybe a stop button. Underlying state needs to land first; the panel's a thin reader.

Other carry-over (still on the list from session 45):
- `?refresh=true` query param on `/v1/enrich/callsign` — operator's escape valve for "the cache is wrong" cases.
- HamQTH / QRZCQ providers — chain expansion under the existing `CallsignProvider` interface.
- "Show edit history for this QSO" SPA panel — consumes `qso_history` storage from session 40. New `/v1/qso/{uuid}/history` endpoint + a panel in InfoPanel (probably under Worked tab as a per-QSO detail view, or as a separate History tab).
- "QSOs awaiting QSL request" view — now that `APP_SM_REQUEST_QSL` is being stamped on submit, the data is there. Needs a list endpoint + a UI surface.
- ~~Daemon-side validation polish for `MyCqZone` / `MyITUZone` / `MyDXCC`.~~ **Closed in this session.** SPA-side mirror of the same validation (snappy inline feedback in My Station panel) is the natural follow-up — the daemon is the backstop, but operators get a red border before they hit Update.
- ~~`WorkedPanel` UI.~~ **Closed in this session.** Future polish: WorkedPanel currently shows the full result-set as a flat table; a "Notes" tooltip or expandable row would make per-QSO notes accessible without crowding the columns.
- Known intermittent flake — `TestSchedule_ReleasesSlotAfterFn` in `internal/lookup/refresher/`, observed once during session 45's `-race` sweep.

### Session 45 work (2026-05-08) — country panel UI end-to-end

Continued from session 44. The natural next chunk after wiring `qsoDraft.name`/`qsoDraft.qth` populate was the country panel UI. Operator supplied a mockup (Malawi flag, distance pair, bearing pair, short/long radio, local time + offset, asterisk for new DXCC entity); session ran the daemon-side new-entity tag, the SPA state holder, the panel implementation, ANT_AZ wiring, and the live-test polish that followed.

**Daemon — `country.is_new_entity` population.** New `HasQsoForCountryWithContext(ctx, country) (bool, error)` on the sqlite service: uses sqlboiler's `models.Qsos(...).Exists(ctx, h)` over the indexed `qso.country` column. The default soft-delete filter is preserved so a deleted-then-re-Tabbed callsign still flips the entity to "new" — the deleted QSO is functionally a row the operator decided didn't happen. Empty country string is treated as a no-match early-return without a query. The orchestrator's `Enrich` calls this after country resolves (whether from cache or hamnut) and sets `c.data.IsNewEntity = !exists`. Failure is non-fatal: `o.warn` logs it and the flag stays at zero (false) — new-entity is presentation, never load-bearing. Three regression tests pin: no-prior-QSO → true; with-prior-QSO → false; empty-country → false.

**SPA state — `lib/states/enrichment.svelte.ts`.** New module-level singleton holds:
- `result: EnrichmentResult | null` — `$state`; null means "no Tab yet, or post-clear"
- `path: 'short' | 'long'` — `$state`; resets to `'short'` on every `setResult` (a fresh Tab is a fresh QSO setup)
- `paths: PathInfo | null` — `$derived.by` from `configState.loggingStation.myGridsquare` + `result.station.gridsquare`; null when either grid is missing
- `activeBearing: string` — `$derived.by` from `path` + `paths`; `.toFixed(1)`-formatted; empty string when paths is null
- `setResult(r)` / `clear()` methods for QsoPanel to push and reset

10 new tests in `enrichment.test.ts` covering set/clear, path-reset, paths derivation (null when either grid missing, populated when both present), and activeBearing reflection.

**`CountryPanel.svelte`.** Layout follows the operator's mockup. Outer card always renders (`w-60 h-62 border bg-surface-muted`) so the layout doesn't shift between empty and populated states; the inner content is wrapped in a `<div class={result === null ? 'hidden' : ''}>` so the content vanishes via Tailwind's `hidden` (display: none) when no enrichment result exists — operator sees a stable bordered placeholder, not an empty box with em-dashes everywhere (the original em-dash design read as "untidy"). When populated:
- Country name (bold) + ` *` if `is_new_entity`
- Flag (`fi fi-<ccode-lowercase> w-40 aspect-[4/3]`)
- Distance pair (`{short} km / {long} km`) — only when paths is populated
- Bearing pair (`{short}° / {long}°`) — same gate
- Short/long radio — same gate
- Local time `HH:MM (offset)` — only when `country.local_time` is populated

Active path (short or long) is highlighted in `text-indigo-700 font-semibold`; inactive in default ink. Indigo matches the existing focus-blue accent (`--color-focus`) and the InfoPanel's active-tab colour, so it reads as "deliberate emphasis" without introducing a new colour.

**`flag-icons` bundled (carry-forward from v1).** `npm install flag-icons` (~600KB after gzip; ~250 SVG flags). Imported into the `components` cascade layer in `app.css` — load-bearing detail because Tailwind v4's `@import "tailwindcss"` puts utilities in the `utilities` cascade layer, and unlayered CSS beats layered CSS regardless of order. Without the explicit `layer(components)`, flag-icons' `.fi { width: 1.333em; line-height: 1em }` defaults override Tailwind's `w-40 aspect-[4/3]` and the flag stays stuck at ~21px. With the layer, the cascade order becomes: theme → base → components (flag-icons) → utilities (Tailwind sizing wins). Documented in the import comment.

**ANT_AZ wiring.** `QsoPanel.submitQso` reads `enrichmentState.activeBearing` (the bearing for the currently-selected path) and passes it as `antAz` to `formatAdifRecord`. The ADIF emitter already supported `antAz` (omit-when-empty); nothing else changed in the emitter.

**Clear-after-submit.** `QsoPanel.submitQso`'s `'stored'` arm calls `enrichmentState.clear()` after `qsoDraft.clear()`, so the country panel returns to its empty state for the next QSO.

**Live-test polish (3 fixes).**
1. **Slow-lookup info-toast.** Operator's flaky internet meant first-Tab on a cold-miss callsign could take many seconds. The country panel just looked broken in that window. Fix: 500ms-delayed sticky info-toast `Looking up <CALL>...` in `handleEnrich`. Cache hits return in <100ms so they never see it; cold misses see "still working" feedback. Toast is dismissed on response regardless of outcome (the warn-not-found / populate logic runs immediately after).
2. **Clear button now clears country panel.** `FormControls.onClear` was wired to `qsoDraft.clear()` only; pressing Clear emptied the form but left the country panel populated with the previous callsign's data. Added `enrichmentState.clear()` to the same handler.
3. **Flag size — bundled approach + `aspect-[4/3]`.** Initial attempt with `w-16` rendered the flag at the em-derived ~21px because of the cascade-layer issue above; with the layer fix in place, `w-40 aspect-[4/3]` now produces a 160×120 flag. Operator can tune via the Tailwind classes on the host span without touching the library CSS.

**Test count this session:** ~13 new tests (3 daemon-side IsNewEntity, 10 SPA enrichmentState). Full Go suite green; SPA suite green at 366 tests.

**Doc updates this session:**
- This session-handoff entry
- Code comments touched: `orchestrator.go` (IsNewEntity branch), `api_context.go` (`HasQsoForCountryWithContext` docstring), `CountryPanel.svelte` (layout decisions + cascade-layer note), `enrichment.svelte.ts` (state-class docstrings), `app.css` (flag-icons cascade-layer comment), `QsoPanel.handleEnrich` (slow-lookup-toast rationale)

**Resume points (for next session):**

The natural next steps are smaller polish items + the deferred enrichment refinements:
- `?refresh=true` query param on `/v1/enrich/callsign` (force-refresh, bypass cache) — operator's escape valve for "the cache is wrong" cases.
- HamQTH / QRZCQ providers — chain expansion under the existing `CallsignProvider` interface; per ADR 0017 each new provider is a small wiring change in `cmd/smd/main.go` plus a `lookupX.NewService(...)` constructor.
- "Show edit history for this QSO" SPA panel — consumes the `qso_history` storage shipped session 40. New `/v1/qso/{uuid}/history` endpoint + a panel under InfoPanel.
- Daemon-side validation polish for `MyCqZone` 1–40, `MyITUZone` 1–90, `MyDxcc` digit-only — currently free-text on the My Station panel.
- Country panel: optional polish — does the operator want auto-tick of the local time? (Daemon recomputes only on lookup; the value stays static until the next Tab.)
- Country panel: `flag-icons` size: operator settled on `w-22` in their tweak; revisit if a different size is wanted.
- Known intermittent flake — `TestSchedule_ReleasesSlotAfterFn` in `internal/lookup/refresher/` hung 600s once during the daemon-side `-race` sweep this session; 20 subsequent re-runs all passed in <2s. Suspected timing artefact under load; apply `cache=shared` or skip-on-CI fix if it recurs.

### Session 44 work (2026-05-08) — SPA enrichment wired + live-test polish

Continued from session 43. The natural next chunk after closing the code-review backlog was wiring the daemon enrichment pipeline through to the SPA. The session ran the full thread: thin fetch wrapper → form populate → live test → fix every quirk surfaced.

**SPA enrichment fetch wrapper (`frontend/logging/src/lib/api/enrichment.ts`).** New file matching the discriminated-outcome shape of `lib/api/qso.ts` (`'ok' | 'validation' | 'server' | 'network'`). Encodes the always-200 contract from ADR 0017 #12 on the type side: provider failures collapse to source=none with empty payloads; the SPA never sees a non-2xx unless the callsign is missing or invalid. Type definitions for `EnrichmentCountry`, `EnrichmentStation`, and `EnrichmentResult` mirror the daemon's JSON tags.

**`QsoPanel.handleEnrich` populates qsoDraft on ok.** Replaced the placeholder `console.log` with a populate body. On `outcome.kind === 'ok'`: write `result.station.name` to `qsoDraft.name` and `result.station.qth` to `qsoDraft.qth` (overwrite-on-new-callsign per the existing UX rationale). On `station_source === 'none'` (no callsign-class provider has a record AND no cached row): warn-toast `Lookup: <CALLSIGN> not found`. The toast gate is intentionally on `station_source` alone, NOT both layers — country lookup is longest-prefix-match so almost any callsign hits the country layer, but the form auto-fill is what the operator cares about. Without the toast, an operator on a flaky internet link would assume a network issue when the providers actually responded with no record. Failure outcomes (network/server/validation) leave the form untouched per the "enrichment never blocks logging" invariant; `qsoDraft.startQso()` still fires unconditionally because Tab is the QSO-start signal regardless of network result.

**LocalTime recompute (orchestrator).** Live test surfaced that `local_time` was empty on cache hits and returned hamnut's wire-time string verbatim on cold miss — neither was right. Root cause: `LocalTime` is presentation, not persistence; `TimeOffset` is the persisted source of truth, but the response shape didn't recompute. Fix: `applyLocalTime(c, now)` + `parseOffsetDuration(s)` in `internal/lookup/orchestrator.go`, called once before the `Result` is constructed in `Enrich`. Parser handles both formats hamnut emits: Go-duration shape (`"2h 0m"`, `"-5h 30m"`) and RFC 3339 zone shape (`"+02:00"`, `"-08:00"`). Empty offset → leave `LocalTime` empty (no UTC default — unparseable input is a data-quality signal). New behaviour tests: fresh-hit recompute, cold-miss-replaces-upstream-string, no-offset-stays-empty. New unit table `localtime_test.go` with 16 cases covering both formats, edge cases, and malformed input.

**RST input-strip via `ValidatedInput.transform` prop.** Live test surfaced that the RST validator (`/^[0-9]{2,3}$/`) only flipped a CSS class — non-digit characters could still be typed. Generalised fix: added an optional `transform?: (raw: string) => string` prop to `ValidatedInput.svelte` (the generic primitive). When set, `oninput` runs the transform before the validator and overwrites both the input element's text AND the bound prop if the cleaned form differs. Cursor parks at end of cleaned text via `setSelectionRange`. `Rst.svelte` wires `stripNonDigits = (raw) => raw.replace(/[^0-9]/g, '')` — a paste of "5A9" lands as "59". 5 new regression tests on `ValidatedInput`: overwrite-on-strip, validator-runs-on-cleaned-value, cursor-at-end, idempotent-on-clean-input, no-transform-still-validates. Generalised so future numeric-only inputs (zone numbers, etc.) reuse the same prop.

**RST manual-clear refill effects removed.** Live test surfaced a frustrating quirk: deleting RST digits right-to-left would snap the field back to default the moment the last digit went. Cause: two `$effect`s in `qsoDraft.svelte.ts` watched `rstSent === ''` / `rstRcvd === ''` and refilled to `defaultRst` immediately. Removed both. The mode-flip refill effect (the third one, tracking `defaultRst`) is preserved because that's the case operators actually want — switching CW ↔ voice should refill the appropriate default. Form-level submit gate (`canSubmit`) already requires non-empty RST so an empty field can't slip through unnoticed. Edit flow restored.

**Default-logbook self-heal at startup.** Live test surfaced that on a fresh DB with a config marked `setup_complete=true`, QSO submit FK-violated because no logbook row existed at `default_logbook_id`. Most common trigger: operator nukes the dev DB to fix some other issue (this session it was the schema-drift on `last_refreshed_at` columns) without clearing `setup_complete`. The first-run seeding handler in `handler_config.go` only fires on the `setup_complete=false → true` transition, which never re-runs once flipped. Fix: new `ensureDefaultLogbook(ctx, db, cfgSvc, logger)` helper called from `cmd/smd/main.go` right after migrations. Three branches: `DefaultLogbookID < 1` warn-and-return; `setup_complete=false` no-op (PUT /v1/config owns first-run seeding); row exists no-op; row missing seed from `LoggingStation.StationCallsign`. If AUTOINCREMENT assigns a different ID (operator hand-edited config to non-1), persist the corrected value back to config — non-fatal on persist failure (in-memory correction works for the session, next startup re-runs the same self-heal). 6 regression tests cover all branches.

**Live test data.** Operator's first-run callsign `7Q7EB` (Elayi Banda, Lilongwe). End-to-end pipeline confirmed working:
- Cold miss on first Tab: country from hamnut, station from QRZ; `last_refreshed_at` zero (orchestrator returns upstream-direct values before write-back stamps the row).
- Cache hit on second Tab: sub-100ms response; `last_refreshed_at` real timestamps; `country_source: "country_table"`, `station_source: "contacted_station"`.
- LocalTime recompute returning `2026-05-08T14:30:37+02:00` on cache hit.
- Form auto-fills `ELAYI BANDA` / `LILONGWE`.

**Test count this session:** ~14 new tests across the daemon (orchestrator local-time recompute behaviour + parser units, ensureDefaultLogbook six branches) and SPA (`ValidatedInput.transform` 5 cases). Full suite green at every commit boundary.

**Doc updates this session:**
- This session-handoff entry
- Code comments touched: `orchestrator.go` (Enrich return path + helpers), `ValidatedInput.svelte` (transform prop semantics), `Rst.svelte` (strip rationale), `qsoDraft.svelte.ts` (`$effect.root` block — what's no-longer-there and why), `cmd/smd/main.go` (ensureDefaultLogbook docstring), `QsoPanel.svelte` (handleEnrich populate-and-toast rationale)

**Resume points (for next session):**

The natural next step is **country-panel UI**. The daemon now returns a complete `Country` block with name / continent / CQ zone / ITU zone / DXCC / time offset / current local time / lat/lon-derivable fields. The SPA already has a `lib/utils/bearing.ts` ready for short/long-path bearing derivation. Component would consume the cached `EnrichmentResult` (probably a small store wrapping the most recent enrichment).

Other carry-over:
- `?refresh=true` query param on `/v1/enrich/callsign` (force-refresh, bypass cache)
- HamQTH / QRZCQ providers for chain expansion
- "Show edit history for this QSO" SPA panel (consumes `qso_history` storage from session 40)
- Daemon-side validation polish for `MyCqZone` 1–40, `MyITUZone` 1–90, `MyDxcc` digit-only
- One known intermittent flake — `TestWorker_DoesNotClaimFutureRows` in `internal/forwarding/worker/`. Probably `:memory:` connection-pool eviction; could be addressed by switching test DBs to `file::memory:?cache=shared` if the flake recurs.

### Session 43 work (2026-05-08) — code-review Minor findings closed (m1–m8)

Continued from session 42. Walked the eight Minor items in `docs/reviews/cmd-smd-and-imports.md`. Each fix is small, local, and lands with a regression test where one is meaningful (m3 / m8 are doc/comment-only).

**m1 — `safego.runWithRespawn` doesn't recover panics in `onPanic` itself.** A misbehaving panic handler (closed logger, nil method receiver) could escape the outer `defer recover()` and crash the daemon — defeating the very purpose of safego. Fix: extracted the handler invocation to `invokePanicHandler(name, panicValue, onPanic)` which runs its own `defer func() { _ = recover() }()` before calling onPanic. A handler crash now degrades to a silent skip. Regression test `TestGo_PanickingHandler_DoesNotEscape` provides an onPanic that itself panics; pre-fix it would have killed the test runner.

**m2 — `time.After` channel leak on respawn-cooldown ctx-cancel.** The `select { case <-ctx.Done(): case <-time.After(cooldown): }` left a live timer behind on the ctx-cancel branch. Bounded leak (one per respawn, expires within cooldown) but inconsistent with the rest of the daemon's long-lived selects. Fix: swap to `time.NewTimer` + explicit `t.Stop()` on the ctx-cancel branch. Existing `TestGo_CtxCancelled_SkipsRespawn` covers the observable behaviour; no new test added because the timer-stop call's effect isn't directly assertable.

**m3 — SSE keepalive write doesn't reset (cleared) write deadline.** Doc clarification: `net.Conn.SetWriteDeadline(time.Time{})` removal is sticky for the connection lifetime — net.Conn does not auto-rearm. Strengthened the comment in `handleEvents` and added an info-level log when `SetWriteDeadline` returns an error (previously discarded with `_ = ...`). A future regression that breaks all long-lived streams now leaves a log trace.

**m4 — Default config doesn't warn when SocketPath is non-loopback under TCP.** Operator binding the auth-less daemon API to `0.0.0.0`, `:8080`, or a LAN IP previously got no advisory. Fix: added `Warnings(cfg Config) []string` to `internal/config/config.go` returning advisory messages (via a new `isLoopbackBind` helper that handles loopback IPv4, IPv6 `[::1]`, the literal "localhost", explicit wildcards `:8080`/`0.0.0.0`/`[::]`, and unknown hostnames conservatively). `cmd/smd/main.go` calls Warnings after the logger boots and emits each at warn level. Unit test `TestWarnings_NonLoopbackTCPBind` covers 9 cases (loopback IPv4 / explicit localhost / loopback IPv6 / wildcard via empty host / wildcard 0.0.0.0 / LAN IP / wildcard IPv6 / unix socket / unrecognised hostname).

**m5 — `FetchCountryByCallsignWithContext` LIKE pattern unescaped.** The country-prefix is interpolated directly into a LIKE pattern (`<callsign> LIKE <prefix> || '%'`), so a row whose prefix contained `%`, `_`, or `\` would silently over-match every callsign. Today hamnut writes plain alphanumerics, but a future provider/admin import could insert problematic data. Fix: added a `strings.ContainsAny(prefix, "%_\\")` guard at `UpsertCountryWithContext` — the single chokepoint for country writes — that rejects malformed prefixes with a clear error. Annotated the read-side query with a comment cross-referencing the upsert guard so future readers don't re-discover the assumption. Regression test `TestUpsertCountry_RejectsLikeWildcardPrefix` covers `M%`, `M_`, `M\\A`, `%`, `_` (all rejected) and a valid alphanumeric (accepted).

**m6 — `qsoservice.IsValidCallsign` doesn't restrict character set.** Validator was "length 3-32 + at-least-one-digit" — a callsign containing `%`, `_`, whitespace, or non-ASCII would pass, and `FetchQsoSliceByCallsignWithContext` interpolates it into a `<callsign>/%` LIKE pattern. Fix: tightened to ASCII letters + digits + `/` + `-` only. The whitelist accepts every legitimate ham-radio callsign shape (portable suffixes like `G4ABC/P`, special-event calls with dashes) and rejects everything else. Removed the `unicode` import since the new validator is ASCII-only and uses a byte-loop. Regression tests `TestIsValidCallsign` extended with 14 negative cases covering SQL meta (`K1%`, `K1_`, `K1\\A`), shell meta (`K1*`), whitespace (`K1 A`, `K1\nA`, `K1\tA`), punctuation (`K1.A`, `K1+A`, `K1@ABC`), non-ASCII (`Käse1`, `K1ÆABC`), and SQL-injection-shaped strings.

**m7 — SSE handler swallows `writeSSEEvent` Marshal errors silently.** `writeSSEEvent` returned `nil` on `json.Marshal` failure with no log line; a future bug that makes one payload type unmarshallable would disappear from operator-visible logs. Fix: converted `writeSSEEvent` from a free function to a method on `*Server` (handler_events.go is the only caller) so the logger is in scope; on marshal failure emit a `WarnWith` line tagged with `event` name + `event_id` and return `nil` (skip behaviour preserved per the existing comment).

**m8 — `EventsHub.Publish` drop-on-Close behaviour worth documenting.** No defect — the silent-drop on a closed hub is the deliberate design and the current shutdown sequence relies on it. Strengthened the Publish doc comment to make "no replay/backlog semantics" explicit, naming the constraint a future "event replay on reconnect" feature would need to revisit. Doc-only.

**Test count this session:** 5 new regression tests (m1, m4, m5, m6 with extended cases, plus the m4 helper-function table-test). Full suite green at every commit boundary, both regular and `-race`.

**Doc updates this session:**
- Code comments touched: `safego.go` (m1 + m2), `handler_events.go` (m3 + m7), `hub.go` (m8), `api_context.go` country-LIKE comment (m5), `validation.go` callsign validator (m6)
- Helper added: `config.Warnings` + `isLoopbackBind` (m4)
- This session-handoff entry

**Resume points (for next session):**

The 8 Minors and 5 Majors are now closed. The 6 Observations (O1–O6) are positive findings — worth a re-read for context, but no work. The review is fully addressed.

Carry-over from prior sessions:
- **SPA-side enrichment wiring** — `lib/enrichment.svelte.ts`, `Callsign.onenrich`, country-panel UI consuming `Result.Country` for short/long-path display. The natural "next big block."
- Country panel UI — `pathInfo()` for short/long path + `antAz` derivation
- `?refresh=true` query param (force refresh on the enrichment endpoint)
- HamQTH / QRZCQ providers (additional callsign-class providers for the chain)
- "Show edit history for this QSO" SPA panel (consumes `qso_history` storage from session 40)
- Daemon-side validation polish for `MyCqZone` 1–40, `MyITUZone` 1–90, `MyDxcc` digit-only
- One known intermittent flake — `TestWorker_DoesNotClaimFutureRows` in `internal/forwarding/worker/`. Probably `:memory:` connection-pool eviction; could be addressed by switching test DBs to `file::memory:?cache=shared` if the flake recurs.

### Session 42 work (2026-05-07) — code-review Major findings closed (M1–M5)

Operator scheduled a thorough multi-agent code review of the daemon + imported internal packages after the heavy ADR 0017 work. Review landed as `docs/reviews/cmd-smd-and-imports.md` with **0 Critical, 5 Major, 8 Minor, 6 Observations**. This session worked through the five Major findings — each fix lands with at least one regression test, all running under `-race`.

**M1 — `refresher.Service.Schedule` ↔ `Stop` `sync.WaitGroup` race.** The pre-fix code checked `started.Load() && !stopped.Load()` (atomics) and then `safego.GoTracked` did a synchronous `wg.Add(1)` — a concurrent `Stop` could pass through `wg.Wait()` between the check and the Add, triggering the runtime's "WaitGroup misuse: Add called concurrently with Wait" detection. Fix: changed `started`/`stopped` from `atomic.Bool` to plain `bool` guarded by a new `sync.Mutex`; Schedule holds mu over the whole "check + sem-acquire + wg.Add" decision; Stop's matching `mu.Lock`/`Unlock` acts as a drain barrier so `wg.Wait` only runs after every in-flight Schedule has either completed or dropped. Regression test `TestSchedule_StopRace_NoWaitGroupMisuse` runs 100 iterations of 16 concurrent Schedules + Stop under `-race`.

**M2 — Orchestrator `Enrich` spawns unprotected goroutines.** The two `go func()` blocks spawning `readCountry` / `readStation` had no panic recovery — a panic in a row mapper, upstream parser, or future logger field would crash the daemon (api `recoverPanic` middleware doesn't reach child goroutines). Fix: replaced both with `safego.Go(ctx, "lookup.enrich.country|station", o.onPanic, fn, false)` and added an `onPanic` method on Orchestrator. The fn pre-declares its outcome variable and uses a `defer func() { ch <- out }()` so a panicking `readCountry`/`readStation` still produces a zero-value outcome on the channel — `Enrich`'s `<-cCh`/`<-sCh` never deadlocks. Source-field normalisation pass after the channel reads rewrites `""` (zero value) to `SourceNone` so the SPA never sees an undocumented enum value. Regression tests `TestEnrich_PanicInCountryProvider_RecoveredAndNoDeadlock` + `TestEnrich_PanicInChainProvider_RecoveredAndNoDeadlock` wrap `Enrich` in a 2-second timeout; pre-fix they would have crashed the test runner.

**M3 — `http.Server.ReadHeaderTimeout` unset.** Slow-headers DoS surface (slowloris-style) — a TCP connection in headers-phase didn't count against `MaxConcurrentRequests` and was bounded only by the operator-tunable `ReadTimeout` (which they may legitimately set high for slow uploads). Fix: added `ReadHeaderTimeoutSec int` to `ServerConfig` (default 5s, applied via `applyDefaults`), wired into the `http.Server` struct literal, documented in api.md §7a server-config table. Regression test `TestServer_ReadHeaderTimeout_Wired` asserts both the config default flows through to the constructed `http.Server.ReadHeaderTimeout` AND the value isn't dangerously large (>30s).

**M4 — QRZ session-key fetch ignored context.** `qrz.requestAndSetSessionKey` used `http.NewRequest` with no context, so daemon shutdown couldn't interrupt a stuck TLS handshake or hung response read — the only timeout was the absolute `HttpTimeoutSec`. Fix: widened the `lookup.Provider.Initialize` interface signature to take `ctx context.Context` (hamnut accepts ctx but ignores it; QRZ propagates into the session-key call via `http.NewRequestWithContext`). `cmd/smd/main.go`'s `buildEnrichment` passes `workerCtx` so a daemon-shutdown cancel reaches the QRZ HTTP transport. Updated all stub providers in tests to match the new interface. Regression test `TestInitialize_RespectsContextCancellation` spins up a hanging httptest server, cancels ctx before `Initialize`, asserts return within 2 seconds (pre-fix would have blocked the full `HttpTimeoutSec` regardless of ctx state).

**M5 — Dead `mattn/go-sqlite3` typed-error path.** The most architecturally interesting finding. Both `internal/database/sqlite/internal.go` and `internal/qsoservice/submit.go` imported `github.com/mattn/go-sqlite3` for the `sqlite3.Error` type and used it as the primary path in `isUniqueConstraintError` — but the daemon's actually-registered driver is `modernc.org/sqlite` (per the `_ "modernc.org/sqlite"` blank import in `service.go` and the `SqliteDriver = "sqlite"` constant). The typed branch never matched at runtime; correctness was silently riding on the substring-match fallback `strings.Contains(err.Error(), "UNIQUE constraint failed")`. Fix: replaced the typed-error detection with `*moderncsqlite.Error` + codes from `modernc.org/sqlite/lib` (`SQLITE_CONSTRAINT_UNIQUE = 2067`, `SQLITE_CONSTRAINT = 19`); exported as `sqlite.IsUniqueConstraintError` so qsoservice could stop duplicating the helper; removed direct mattn imports (it stays in `go.mod` as `// indirect` because golang-migrate's sqlite3 adapter still pulls it in transitively, but no daemon code imports it directly). Regression tests use raw SQL through `BeginTxContext` to trigger a real UNIQUE violation untranslated by the high-level Insert helpers, then assert (a) `IsUniqueConstraintError` returns true, (b) the error unwraps to `*modernc.org/sqlite.Error`, (c) the substring "UNIQUE constraint failed" is still present (fallback still works).

**ADR 0018 created** — `docs/decisions/0018-sqlite-driver-modernc.md` retroactively documents the modernc choice. The `git log` archaeology showed the switch had arrived in this repo via a `git subtree` pull from a standalone `internal/database` sub-repo (commit `16d348e`, 2026-03-23) — there was no ADR-level discussion in this repo's history and the operator (correctly) didn't remember agreeing to the change. ADR 0018 captures the decision retroactively with the full alternatives section (mattn / crawshaw / zombiezen / dual-driver build tags), the test surface ("86+ tests in `internal/database/sqlite/` plus 10+ test files in dependent packages, all green"), and the honest gap: no real-world load testing yet because v2 isn't in daily operator use. Triggers-to-revisit list cites production-load issues, perf bottlenecks, and modernc going unmaintained.

**Test count this session:** ~10 new regression tests across the five fixes. Full suite green at every commit boundary, both regular and `-race`.

**Doc updates this session:**
- ADR 0018 (created)
- `docs/v2-design/api.md` — added `ReadHeaderTimeoutSec` row to the server-config knobs table
- This session-handoff entry

**Resume points (for next session):**

The 8 Minor findings + 6 Observations from `docs/reviews/cmd-smd-and-imports.md` are still open:

- **m1** — `safego.runWithRespawn` doesn't recover panics in `onPanic` itself
- **m2** — `time.After` channel leak on respawn-cooldown ctx-cancel
- **m3** — SSE keepalive write doesn't reset the (cleared) write deadline
- **m4** — Default config doesn't warn when `SocketPath` is non-loopback under TCP
- **m5** — `FetchCountryByCallsignWithContext` LIKE pattern unescaped (operator-prefix-charset invariant undocumented)
- **m6** — `qsoservice.IsValidCallsign` doesn't restrict character set (LIKE-injection-shaped concern in `FetchQsoSliceByCallsignWithContext`)
- **m7** — SSE handler swallows `writeSSEEvent` Marshal errors silently (no log line)
- **m8** — `EventsHub.Publish` drop-on-Close behaviour worth documenting (no defect, just operator-visible "no replay" semantics)

The Observations (O1–O6) are positives — careful shutdown order, clean token-bucket implementation, orchestrator filter+merge correctly pinned, etc. Worth a re-read before the next round of work.

**Other deferred from prior sessions:**
- SPA-side enrichment wiring (`lib/enrichment.svelte.ts`, `Callsign.onenrich`, country-panel UI consuming `Result.Country` for short/long-path display)
- Country panel UI — `pathInfo()` for short/long path + `antAz` derivation
- `?refresh=true` query param
- HamQTH / QRZCQ providers
- "Show edit history for this QSO" SPA panel (consumes `qso_history` storage from session 40)
- Daemon-side validation polish for `MyCqZone` 1–40, `MyITUZone` 1–90, `MyDxcc` digit-only
- One known intermittent flake — `TestWorker_DoesNotClaimFutureRows` in `internal/forwarding/worker/`. Probably `:memory:` connection-pool eviction; could be addressed by switching test DBs to `file::memory:?cache=shared` if the flake recurs.

### Session 41 work (2026-05-07) — ADR 0017 SHIPPED end-to-end: enrichment pipeline daemon-side

Operator opened with "Let's discuss enrichment as it is a big block of work." The session ran the full design-→-ADR-→-implementation arc in eleven discrete tasks (#55–#65), each committed by the operator after the green-test mark.

**Design refinements during discussion** (vs ADR 0005's earlier framing):

- Hamnut is the country source-of-truth; QRZ-class providers are name/QTH/grid/license-class only. Concrete cautionary example: QRZ records `M0CMC` in CQ Zone 37 / ITU Zone 53 (Malawi) but the call is English. Country / CQ / ITU / continent / DXCC are **hamnut-exclusive on write** — pinned in tests as a load-bearing assertion.
- The "cache" is the existing domain tables (`country` keyed on prefix, `contacted_station` keyed on callsign). Inverts ADR 0005's "internet is truth, cache is auxiliary" framing: the local DB IS truth once populated; internet is the fallback for misses + occasional refresh. Aligns with the operator's Malawi-internet-is-unreliable constraint.
- Multiple callsign-class providers (QRZ.com, HamQTH, QRZCQ, …) form a sequential priority chain — first-non-empty wins, fallback on empty/error per ADR 0017 #8. Empty chain valid (no callsign-class enrichment runs).
- Read policy is three-state per layer: cold miss → block-and-write; stale hit → serve-stale + async-refresh; fresh hit → return cached.
- Always-merge at return time using `MergeStationFromCountry` with only-when-different semantics. The merge runs on every read state, not just cold miss — pinned by `TestEnrich_FreshStation_FreshCountry_MergeIsNoOp` and the cold-station-merges-from-cached-country test.
- Two-path `contacted_station` (per ADR 0017 #10): enrichment lookup writes on chain hit; QSO submit upserts whatever operator-collected fields are populated. Refresh data wins on conflict (ADR 0017 #11) — `qso` row preserves historical truth.

**Tasks shipped (in dependency order):**

1. **#55 Schema migration** — added `last_refreshed_at DATETIME` (nullable) to `country` + `contacted_station` in-place to `0001_init.up.sql` (pre-production); sqlboiler regen.
2. **#56 Provider interface + DTOs** — `internal/lookup/lookup.go` defines `CountryProvider` / `CallsignProvider` (asymmetric return types, share a small `Provider` for `Name()` + `Initialize()`), `FilterToCallsignFields` helper for stripping QRZ-bug country fields at the orchestrator boundary. Type-system enforcement (parallel struct without country fields) was rejected — runtime filter is the single chokepoint, consistent with `feedback_types_canonical_dto`.
3. **#57 Cache helpers** — added `LastRefreshedAt time.Time` to `types.Country` + `types.ContactedStation` (canonical DTOs, not parallel cache structs); adapter round-trip; `FetchCountryByPrefix` (exact match for hamnut writes), `UpsertCountry` (full-row replace), `UpsertContactedStation` (Go-side non-empty-wins-per-field merge — necessary because most fields live in `additional_data` JSON). The existing `FetchCountryByCallsign` already does longest-prefix-match, so no separate prefix-extraction helper was needed.
4. **#58 Hamnut provider port** — `internal/lookup/hamnut/` ported from v1; v2 idioms (`errors.New(op).WithMsg`, stdlib `encoding/json`, `lookup.CountryProvider` compile-time check); httptest-backed unit tests.
5. **#59 QRZ-lookup port** — `internal/lookup/qrz/` ported from v1, distinct from `internal/forwarding/qrz/`. The QRZ provider does NOT pre-strip country fields; the orchestrator's `FilterToCallsignFields` is the single enforcement point. Session-fetch failure flips `Config.Enabled` to false so the chain runner skips the service.
6. **#60 Orchestrator** — `internal/lookup/orchestrator.go` implements `Enrich(ctx, callsign) Result`. Concurrent reads of both layers, then synchronous filter + merge + writes (cold miss only) + async refresh schedule (stale hit only). Refactored mid-session after operator clarified merge semantics: filter zeros QRZ-bug values, merge fills with hamnut truth using only-when-different. Async station refresh re-merges from country at refresh time so denormalized fields stay aligned.
7. **#61 Async-refresh worker** — `internal/lookup/refresher/` provides bounded goroutine pool (default 4 in-flight) + `safego.GoTracked` panic recovery + lifecycle (Initialize / Start(ctx) / Stop). Drop-at-capacity rather than queue. Stop has no hard deadline (matches the project's operator-triggered-shutdown posture).
8. **#62 Operator config** — `types.EnrichmentConfig` (Hamnut + Chain + TTLs + RefreshMaxInFlight). Defaults: country TTL 365 days, station TTL 90 days, refresh max-in-flight 4. Validation rules: empty chain valid; enabled providers must have URL/UserAgent/timeout; duplicate names rejected; chain entry can't shadow Hamnut. New accessors on `config.Service`: `Enrichment()`, `CountryTTL()`, `StationTTL()`, `RefreshMaxInFlight()`, `LookupServiceConfig(name)`. Hamnut/QRZ Initialize paths now pull config via `LookupServiceConfig` (closes the "not yet wired" loop from #58/#59).
9. **#63 HTTP handler + DI wiring** — `internal/api/handler_enrich.go` for `GET /v1/enrich/callsign?call=X`. Always-200 contract; 4xx only for malformed input. `cmd/smd/main.go` gained a `buildEnrichment` helper that constructs hamnut + chain providers from operator config (only enabled entries), starts the refresher with `workerCtx`, and constructs the orchestrator. Nil orchestrator (no providers enabled) is a valid daemon shape — the handler returns the uniform empty-result.
10. **#64 QSO-submit upsert** — `qsoservice.Submit` now does a best-effort `UpsertContactedStation` after `tx.Commit` (outside the QSO transaction per the cache-writes-are-best-effort half of one-fails-all-fail). Failures log a warning but don't fail the submit response. Duplicate-path returns early before the upsert, so the cache row's `CSID` stays stable across retries.
11. **#65 Integration tests + docs pass** — three e2e tests in `handler_enrich_e2e_test.go` wire real `hamnut.Service` + real `lookupqrz.Service` against httptest upstreams: happy path with merge, second-Tab cache hit, hamnut-down fall-through. Docs updated: ADR 0017 + `docs/v2-design/enrichment.md` (created) + `docs/v2-design/api.md` § 4 + § 7a + new memory file `project_sm_enrichment.md` + this session-handoff entry.

**Test count summary:** ~70 new tests across the eleven tasks. Full suite green at every commit boundary. The QRZ-bug-for-M0CMC scenario is pinned end-to-end (provider unit test + orchestrator unit test + handler e2e test) so any regression in filter+merge would surface loud.

**Resume points (NOT in this session's scope):**

- **SPA-side enrichment wiring** — `lib/enrichment.svelte.ts` (~30 lines per ADR 0005's still-valid SPA framing), `Callsign.onenrich` callback wired through, `QsoPanel` applies returned fields, country-panel UI consumes `Result.Country` + `pathInfo()` for short/long-path display.
- **`?refresh=true` query param** — operator-triggered force-refresh (deferred per ADR 0017 triggers-to-revisit; add when the operator wants explicit cache invalidation).
- **HamQTH / QRZCQ providers** — fresh code (no v1 carry-forward); add by mirroring the QRZ provider shape and registering the constructor in `cmd/smd/main.go`'s `buildEnrichment` switch.
- **"Show edit history for this QSO"** SPA panel — consumes the `qso_history` storage from session 40's prep #2.

### Session 40 work (2026-05-07) — ADR 0016 prep #2 SHIPPED: qso_history append-only audit table

Picked up exactly where session 39 left off — the design call (option (a): audit UPDATE+DELETE only, NOT INSERT) and the schema were locked in the session 39 entry, so the work was straight implementation.

**Migration (`0001_init.up.sql` + `.down.sql`).** `qso_history` table appended in place per the agreed shape: `id` AUTOINCREMENT PK, `qso_uuid` TEXT NOT NULL FK to `qso(uuid)` (UUID — not int — so audit rows survive any future renumbering or cross-daemon sync), `op` TEXT CHECK `op IN ('update','delete')`, `at` DATETIME DEFAULT `datetime('now','localtime')`, `source` TEXT NOT NULL (freetext — daemon writes typed values from the new source enum), `before_image` JSON NOT NULL CHECK `json_valid(before_image)`. `idx_qso_history_qso_uuid (qso_uuid, at)` for the lookup-by-QSO-time-ordered shape. Two `BEFORE UPDATE` / `BEFORE DELETE` triggers (`trg_qso_history_no_update` / `trg_qso_history_no_delete`) `RAISE(ABORT, 'qso_history is append-only — UPDATE/DELETE not permitted')`. Down migration drops triggers + index + table in reverse-FK order. FK uses `ON DELETE NO ACTION` because the QSO is soft-deleted, not removed — the FK row stays intact.

**Source enum (`internal/enums/source/source.go`).** Mirrors `internal/enums/upload/action/`. `Source string` typed alias, `API Source = "api"` constant (`source.API`, not `source.SourceAPI` — package-qualified). `String()` + `Parse(s)` methods. Only `API` declared today; future sources (worker, importer, cli) added one constant at a time as they appear. Doc comment notes the column is freetext on the DB side, so values read back may be outside the typed set.

**sqlboiler regen.** Blew away `build/db/data.db`, applied the migration to a fresh DB via `sqlite3 < 0001_init.up.sql`, then `cd internal/database/sqlite && sqlboiler sqlite3` per `sqlboiler.toml`. New `models/qso_history.go` generated. `BeforeImage` lands as `types.JSON` (from `aarondl/sqlboiler/v4/types`, imported as `boiltypes` in `api_context.go` to avoid colliding with the project's `internal/types` package).

**`InsertQsoHistoryTx` helper (`api_context.go`).** Slots in next to the other `*Tx` helpers. Signature `InsertQsoHistoryTx(ctx, tx, qsoUUID, mutationOp action.Action, src source.Source, beforeImage []byte)`. Reuses `action.Action` for the op param rather than building a parallel `historyOp` enum (the string set overlaps; "build specific not generic" rule, plus `types is canonical DTO`). Validates: tx non-nil, qsoUUID non-empty, mutationOp ∈ {Update, Delete} (rejects Insert before SQL CHECK fires for clearer error), src non-empty, beforeImage non-empty. Insert via sqlboiler `model.Insert(ctx, tx, boil.Infer())` (project rule: sqlboiler default, raw SQL only with stated reason). `at` left to SQL DEFAULT so the timestamp comes from the database, not Go wall-clock.

**`FetchQsoHistoryByUUIDWithContext` (`api_context.go`).** Read helper returning `[]types.QsoHistory` ordered `at ASC, id ASC`. Used by tests today; future operator-facing "show edit history" endpoint will use the same helper.

**DTO (`types/qso_history.go`).** Five fields plus `BeforeImage []byte` (raw JSON — the consumer either deserializes into `types.Qso` for display or forwards intact to SM Cloud sync). Adapter `QsoHistoryModelToType` in `database/sqlite/adapters/model_to_type.go` does the model→DTO mapping (`[]byte(model.BeforeImage)`).

**`qsoservice.Update` hook.** Added `src source.Source` param. Just before `tx.Commit()` (after the upload-queue insert loop), `json.Marshal(existing)` — the pre-edit snapshot, NOT `merged` — and `s.DB.InsertQsoHistoryTx(ctx, tx, existing.UUID, action.Update, src, beforeImage)`. Both potential failure points roll the tx back so the QSO update doesn't commit without its audit row.

**`qsoservice.Delete` hook + signature reshape.** Resolved the open question from session 39: pass the snapshot from the handler instead of fetching inside the tx. The handler already fetches the QSO at handler_qso.go:50 to resolve UUID→ID, so reusing that result avoids a second DB round-trip and guarantees the audit row's `before_image` matches what the operator was looking at when they triggered the delete. New signature: `Delete(ctx context.Context, existing types.Qso, src source.Source) error`. Internal logic uses `existing.ID` and `existing.UUID`. Same atomicity story as Update.

**Handler wiring (`handler_qso.go`).** `handleUpdateQso` passes `source.API` to `s.qso.Update`. `handleDeleteQso` passes the already-fetched `qso` (full snapshot) plus `source.API` to `s.qso.Delete`.

**Tests.** Two new files:
- `internal/database/sqlite/qso_history_test.go` — DB-level. `TestInsertQsoHistoryTx_HappyPath`, `TestInsertQsoHistoryTx_RejectsInsertOp`, `TestInsertQsoHistoryTx_RejectsEmptyArgs` (table-driven uuid/source/image guards), `TestQsoHistory_AppendOnly_TriggersFire` (UPDATE and DELETE both rejected by trigger, row survives), `TestFetchQsoHistoryByUUID_OrderedAscending` (3 rows, insertion order preserved), `TestFetchQsoHistoryByUUID_EmptyForUnknownUUID`. Includes a test-only `insertQsoHistory` helper mirroring the existing `enqueueUpload` shape.
- `internal/api/handler_qso_history_test.go` — e2e via PATCH/DELETE. `TestE2E_PatchWritesAuditRow` (one row, op=update, source=api, snapshot is pre-edit not merged — verified by checking `comment` is empty in snapshot but PATCH set it), `TestE2E_DeleteWritesAuditRow` (one row, op=delete, source=api, snapshot has the QSO's call), `TestE2E_TwoEditsAccumulateHistory` (two PATCHes → two rows in temporal order, second snapshot's comment matches what the first PATCH set).

Full suite green (cmd/smd, all internal/* packages). One micro-fix during test authoring: `types.Qso` doesn't have a `DeletedAt` field, so dropped a "snapshot DeletedAt is zero" assertion that turned out to be checking a non-existent struct field (the soft-delete column lives on the DB row, not the type).

**Documentation pass.**
- `docs/decisions/0016-sm-cloud-deferred-with-prep.md` — appended "Implementation outcome (session 40, 2026-05-07): SHIPPED" paragraph to prep #2's section. Updated the Consequences "Signed up for" bullet on `qso_history` to reflect the shipped shape (UPDATE/DELETE only scope, FK by UUID, full JSON snapshot, trigger-enforced append-only).
- `docs/v2-design/api.md` — PATCH and DELETE bullets in §7a now note the audit-trail behaviour (one row appended in the same tx, op/source/before_image shape).
- `docs/v2-design/milestones.md` — SM Cloud section, prep #2 flipped from NOT STARTED to SHIPPED with full implementation breakdown. Audit date bumped to 2026-05-07.
- `memory/project_sm_cloud_deferral.md` — prep #2 entry rewritten from "NOT STARTED" to "SHIPPED 2026-05-07 (session 40)" with the full schema + helper + DTO + enum breakdown.
- This session-handoff entry replaces the session 39 placeholder.

**What's next:**

- Both ADR 0016 prep items are now shipped. SM Cloud itself stays deferred per ADR 0016 — no Postgres / multi-tenancy / auth / cloud-aware UI work, just the schema readiness that the prep items provided.
- Open threads from session 36-37, still unstarted: country panel UI (wire `pathInfo()` for short/long-path display + populate `antAz` on submit when remote grid is known); daemon `GET /v1/enrich/callsign` per ADR 0005 (supplies remote gridsquare automatically); optional polish on daemon-side validation for `MyCqZone` / `MyITUZone` / `MyDxcc` digit-only / range checks.
- Future SM Cloud-flavoured task (NOT urgent, NOT a blocker): an operator-facing "show edit history for this QSO" endpoint + SPA panel. The storage and read helper are now in place; this would be the UI on top.

### Session 39 work (2026-05-07) — ADR 0016 prep #2 (qso_history audit table) — DESIGN AGREED, no code (power outage)

Operator opened the session asking what was next; offered the two carry-forward options from session 38. Operator picked prep #2 (qso_history audit table) and pinned the same in-place migration approach as session 38: amend `0001_init.up.sql` rather than introducing a `0002_*.sql`, because the project hasn't gone to production. Power went out before any edits landed.

**Design call locked this session — option (a):** audit table records UPDATE + DELETE only, NOT INSERT. Reason: the ADR 0016 headline says "edit/delete provenance trail" and origin/source for the *initial* insert is already covered by `additional_data` provenance fields per ADR 0014 prep #4 (`received_from` / `originated_by`), so auditing inserts would duplicate what `additional_data` already carries. The ADR 0016 consequences-list mention of `op IN ('insert','update','delete')` is the broader phrasing; the headline shape is what we're building. CHECK constraint accordingly tightens to `op IN ('update','delete')`.

**Agreed schema (to be added in-place to `0001_init.up.sql`):**

```sql
CREATE TABLE IF NOT EXISTS qso_history (
    id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    qso_uuid     TEXT NOT NULL,
    op           TEXT NOT NULL CHECK (op IN ('update','delete')),
    at           DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    source       TEXT NOT NULL,
    before_image JSON NOT NULL CHECK (json_valid(before_image)),
    CONSTRAINT fk_qso_history_qso FOREIGN KEY (qso_uuid)
        REFERENCES qso(uuid) ON DELETE NO ACTION
);
CREATE INDEX IF NOT EXISTS idx_qso_history_qso_uuid ON qso_history (qso_uuid, at);

-- Append-only enforcement (belt + braces — daemon code never UPDATEs/DELETEs
-- these rows; the triggers ensure that bug-introduced or schema-cleanup
-- attempts hit a hard error rather than silently rewriting history).
CREATE TRIGGER IF NOT EXISTS trg_qso_history_no_update
    BEFORE UPDATE ON qso_history
BEGIN
    SELECT RAISE(ABORT, 'qso_history is append-only — UPDATE not permitted');
END;
CREATE TRIGGER IF NOT EXISTS trg_qso_history_no_delete
    BEFORE DELETE ON qso_history
BEGIN
    SELECT RAISE(ABORT, 'qso_history is append-only — DELETE not permitted');
END;
```

**Why FK by qso_uuid (not qso.id):** the canonical external identifier per ADR 0016 is the UUID, and the audit history outlives any future schema reshuffle that touches the int PK; references qso(uuid) which is already UNIQUE NOT NULL.

**Why before_image is full JSON snapshot (not diff):** trivial to compute (`json.Marshal(existing types.Qso)`) and trivial to reconstruct. At personal-operator scale (thousands of QSOs, dozens of edits per year) the storage cost is negligible. Diff would be a premature optimisation.

**Source field is freetext (no CHECK).** First implementation passes `"api"` from the HTTP handler. Future sources (`"adif_import"` for `cmd/importer`, etc.) add labels without migration churn. Constants will live in a new `internal/enums/source/` package mirroring `internal/enums/upload/action/`.

**Resume point — session 40 picks up exactly here.** The TaskList for the work was created in session 39 (#49–#54) and is still pending; reuse those tasks rather than re-creating. Steps in order:

1. **Amend `0001_init.up.sql`** with the `qso_history` block above. Update `0001_init.down.sql` to drop the triggers + index + table in correct order (triggers before table; table before `qso` since `qso_history` references it).
2. **Add `internal/enums/source/source.go`** mirroring `internal/enums/upload/action/`. v1 constants: `SourceAPI = "api"`. Type `Source string` + `String()` + `Parse(s)` for symmetry.
3. **Regenerate sqlboiler models.** Process: blow away `build/db/data.db` if present, run the migration to a fresh DB (or open the daemon once against a fresh working dir), then `cd internal/database/sqlite && sqlboiler sqlite3` per `sqlboiler.toml`. Generated `models/qso_history.go` will appear alongside the existing models.
4. **Add `InsertQsoHistoryTx(ctx, tx, qsoUUID, op, source, beforeImage)` on `sqlite.Service`** in `api_context.go` next to the other `*Tx` helpers (line ~1689 onwards). Default to sqlboiler's `model.Insert(ctx, tx, boil.Infer())` per the project's "sqlboiler default" rule. Note: `before_image` accepts `[]byte` (raw JSON) — sqlboiler typically maps JSON columns to `null.JSON` or `types.JSON`; the regen will tell us which. If it lands as `null.JSON`, wrap with `null.JSONFrom([]byte)`.
5. **Hook `qsoservice.Update`:** add `source string` param; before the `tx.Commit()`, marshal `existing` (the pre-edit snapshot, NOT `merged`) with `json.Marshal` and call `s.DB.InsertQsoHistoryTx(ctx, tx, existing.UUID, "update", source, beforeImage)`. Same one-fails-all-fail tx envelope.
6. **Hook `qsoservice.Delete`:** signature changes to `Delete(ctx, id, source string)` — but the handler currently passes `int64` only. Refactor so the handler fetches the QSO snapshot before calling Delete (it already does — `qso, err := s.db.FetchQsoByUUIDWithContext(...)` at handler_qso.go:50), and either pass that snapshot down to Delete OR have Delete itself fetch by ID inside the tx for snapshot consistency. Cleaner shape: Delete signature becomes `Delete(ctx, id, source string)` and Delete itself does `existing, err := s.DB.FetchQsoByIDTx(ctx, tx, id)` (need to add this if it doesn't exist) so the snapshot is read inside the tx — guarantees consistency under concurrent updates.
7. **Update `handler_qso.go`** call sites to pass `source.SourceAPI` (or just `"api"` if the enum import is overkill for v1).
8. **Tests** in `internal/qsoservice/audit_test.go` (or extend `delete_test.go` / `update_test.go`):
   - Update writes a row: `op="update"`, `qso_uuid` matches the QSO, `before_image` round-trips back to the pre-edit `types.Qso`, `source` flows through.
   - Delete writes a row with `op="delete"`, snapshot is the pre-delete state.
   - Append-only triggers: direct `UPDATE qso_history SET ...` and `DELETE FROM qso_history` both error.
   - Atomicity: if `InsertQsoHistoryTx` fails (e.g. force a `before_image` violating json_valid by passing non-JSON bytes), the QSO mutation rolls back. Symmetric with the existing `qso_upload` rollback test pattern.
9. **Documentation pass:** ADR 0016 — append "Implementation outcome (session 40, 2026-05-XX): SHIPPED — qso_history table..." paragraph after prep #2's cloud-readiness payoff. `docs/v2-design/api.md` — note that PATCH/DELETE now write audit rows (informational, no API surface change). `docs/v2-design/milestones.md` SM Cloud section — flip prep #2 from NOT STARTED to SHIPPED. `memory/project_sm_cloud_deferral.md` — same. session-handoff: move this entry to "Session 40 work" with full implementation breakdown, replace this placeholder.

**Open question deferred to implementation:** does `Delete` snapshot inside the tx (FetchQsoByIDTx — needs new helper) or accept a snapshot param from the handler (handler already has one in scope)? Lean toward inside-tx for consistency under concurrent edits, but it's a one-line preference and either works.

### Session 38 work (2026-05-06) — ADR 0016 prep #1 SHIPPED: UUIDv7 as canonical QSO identifier

Operator opened the session with "Let's implement the globally-unique time-ordered QSO IDs" → chose UUIDv7 (vs ULID — RFC 9562 standardisation + ecosystem familiarity won). Phased the work as Phase 1 (model + storage + submit) then Phase 2 (full external surface switch + ADIF). Both phases shipped in the same session.

**Phase 1 — model + storage.** `internal/utils/uuid.go` hand-builds UUIDv7 from `crypto/rand` (no external dependency, per minimize-deps rule): 48-bit ms timestamp + version `7` nibble + variant `10` + 62 random bits. `NewUUIDv7()`, `NewUUIDv7At(t)`, `IsValidUUIDv7(s)`. Seven test functions cover format, version/variant bits, timestamp prefix, k-sortable ordering, uniqueness across 1024 generations, pre-epoch coercion, edge-case validation. Operator caught my plan to chain a 2-stage migration with Go-side backfill — corrected to "Edit the current migration as we have not gone to production." `0001_init.up.sql` edited in place: `uuid TEXT NOT NULL UNIQUE` with a strict format CHECK pinning length=36, dash positions, and version-7 nibble. sqlboiler regenerated. `types.Qso.UUID` added (one-line, minimal blast radius — types follows ADIF and absorbs spec-tracking changes the same way). `qsoservice.Submit` stamps `qso.UUID = utils.NewUUIDv7()` before insert; both duplicate paths return `SubmitResult{Status, UUID, ID}` with `ID` flagged in code as transitional. Adapter pair carries UUID both directions.

**Phase 2 — external surface switch.** All QSO-keyed paths (`/v1/qso/{uuid}`, `/v1/qso/{uuid}/uploads`) route by UUID; `parsePathUUID` validates via `IsValidUUIDv7` and returns `400 invalid_uuid` on miss. Handlers fetch by UUID then pass the resolved int PK into the existing service methods — qsoservice signatures unchanged, surface separation kept clean. `internal/adif` Record gained `AppSmQsoID string \`adif:"app_sm_qso_id,omitempty"\``; `QsoToRecord` populates it from `q.UUID`. SPA `submitQso` outcome union switched: `{kind: 'stored'|'duplicate', uuid: string}`; the `id` field on the daemon response is read but ignored (transitional shim). `QsoPanel`'s duplicate toast logs the UUID to the console (`[QSO submit] duplicate uuid=...`) and surfaces the operator-readable callsign in the visible toast — UUIDs are noise for human display. Test scaffolding swept: `submitAndGetID` returns `(int64, string)`, `patchQso`/`deleteQso`/`fetchUploads`/`waitForUploads` take UUID strings, `not-found` tests use `utils.NewUUIDv7()` as a valid-format-but-nonexistent UUID.

**Documentation pass (this session).** `docs/decisions/0016-sm-cloud-deferred-with-prep.md` (status already Accepted), `docs/v2-design/api.md` (UUID across §4.2 / §4.3 / §4.5 / §5 endpoint sketch / §7a landed shapes / error-code table / canonical-forms section incl. APP_SM_QSO_ID), `docs/v2-design/milestones.md` SM Cloud section (prep #1 marked SHIPPED, prep #2 NOT STARTED), `memory/project_sm_cloud_deferral.md` (prep #1 status updated; ULID rejection rationale recorded), this current-state line + this entry. The `internal/events` SSE payloads gap is documented in api.md §4.5 — not fixed in this session because no live consumer exists (bridge.svelte.ts is stubbed and the e2e test correlates by int), and pre-emptive change would be design-by-anticipation.

**Resume point:** session 39 picks up either ADR 0016 prep #2 (qso_history audit table — operator's framing was "we need to have this working before we implement any other planned features," which arguably extends to the audit table since it's the other half of ADR 0016), or the country panel UI / `GET /v1/enrich/callsign` thread that was deferred from session 36-37. Operator picks.

### Session 37 work (2026-05-06) — SM Cloud deferral captured (ADR 0016)

Operator surfaced the long-held intent to eventually offer **SM Cloud** — a multi-tenant hosted service for multi-user / multi-logbook upload-edit-delete-from-anywhere. Distinct from ADR 0014 (upstream forwarding among the operator's own daemons): SM Cloud is a public-facing hosted product. Operator asked: plan now, or defer?

**Decision (ADR 0016, Accepted 2026-05-06): defer the build, commit two cheap-now schema decisions.**

Same shape as ADR 0014 (deferred-with-prep). The build is foreclosed in v1: no Postgres migration, no user/account model, no auth flows, no multi-tenant code paths, no public-facing API, no cloud-aware SPA. The local daemon stays single-operator, SQLite-backed. When "what about the cloud?" comes up in design, the answer is "ADR 0016 — out of scope until a real driver appears."

Two prep items accepted as standing requirements because each is independently justified by v1 scope **and** retroactively unsalvageable once a populated log exists:

1. **Globally-unique, time-ordered QSO identifier** — UUIDv7 or ULID (impl session picks), daemon-generated at create time, never reused, becomes the canonical external identifier across API responses, ADIF exports, future edit endpoints. Local int PK can stay as sqlboiler/storage detail. UUIDv4 explicitly rejected — index locality + debuggability win for time-ordered. Once a populated log exists, retro-assigning UUIDs is doable but external references and ADIF exports still carry the old shape.
2. **Edit/delete audit table** — separate `qso_history`-shaped table (NOT a column on `qso`, NOT in `additional_data`), append-only, capturing `qso_id` (FK via UUID), `op`, `at`, `source`, `before_image`. Operator wants "what did I change last Tuesday?" auditing the moment a manual edit happens; ADIF re-imports are notorious for silent overwrites. Audit history is the *one* thing that cannot be backfilled — once an edit happens unrecorded, the before-image is gone.

**Why this isn't ADR 0014's problem.** ADR 0014's prep covered the wire shape (forwarder driver layer + threaded auth + `additional_data` provenance + namespaced `enabled` flags). It did not cover schema. SM Cloud's "two operators uploading their logs cannot collide on QSO identity" need is schema-shaped, hence a new ADR.

**Documentation pass this session.** ADR 0016 created. New memory file `project_sm_cloud_deferral.md` with `MEMORY.md` index entry. `docs/v2-design/milestones.md` gets a brief deferral note after Milestone 3 so the milestone doc cross-references the ADR rather than implying SM Cloud is on the roadmap. This handoff entry. No code changes this session — pure design / documentation.

**What's next (carried into session 38+):**

- **Implement the two ADR 0016 prep items.** Operator said "do the globally-unique ID/provenance trail next session and it is not too much work now." Concretely: add UUID column + index to QSO table, generate at create time, plumb through API responses; create `qso_history` audit table with append-only writes from the qsoservice update / delete paths; backfill existing rows' UUIDs sortable by `created_at` so time-ordering is preserved; pin in tests.
- Country panel UI — wire `pathInfo()` for short/long-path display + populate `antAz` on submit when remote grid is known.
- Daemon `GET /v1/enrich/callsign` per ADR 0005 — supplies remote gridsquare automatically.
- Optional polish: daemon-side string-format validation for zones (`MyCqZone` 1-40, `MyITUZone` 1-90, `MyDxcc` digit-only).

**Resume point:** session 38 starts with the ADR 0016 implementation. Read ADR 0016 + `project_sm_cloud_deferral.md` memory before designing the schema migration; the ADR pins the foreclosed alternatives so re-litigation isn't needed.

### Session 36 work (2026-05-05) — My Station panel UX iteration: sub-tabs + Update button

Operator drove a UX iteration on `MyStationPanel.svelte` after seeing the post-session-35 panel was visually too busy (17 inputs stacked vertically, scrolling required).

**Layout exploration.** Discussed three options: (a) move some fields to a separate config SPA, (b) collapsibles via native `<details>`/`<summary>`, (c) sub-tabs inside the panel. Recommended collapsibles first as the lighter-weight option (no JS state, edit-once-ish fields stay collapsed by default). Operator tried collapsibles, then opted for tabs because tabs eliminate the scroll entirely. Locked: **sub-tabs** as the canonical My Station structure.

**Sub-tab implementation.** Four tabs: Identity / Location / Equipment / CW. Reused the `tab-item` / `tab-button` Tailwind classes from the parent `InfoPanel` so styling stays consistent; differences from the parent (deliberate, marks the nesting): no icons, `text-sm` font (vs default base), tighter `space-x-8` spacing (vs InfoPanel's `space-x-24`), thinner `border-gray-300 pb-1.5` separator. ARIA: `role="tablist"` / `role="tab"` / `role="tabpanel"` mirrors the InfoPanel convention so screen readers see the nested tab strip. `activeSection: SectionId = $state('identity')` is panel-local — first-load is Identity (most-edited at first run); the rest are edit-once-ish. Operator further iterated the layout inside each tab (rows of `flex space-x-4` with `widthClass="w-fit"`).

**Update button.** Single button at the bottom of the panel, outside the `{#if}` tab-body chain so it persists across section switches and saves the panel as a whole. `onUpdate()` builds a `logging_station` payload from `configState.loggingStation` (camelCase → snake_case explicit field-by-field map; `my_lat`/`my_lon` deliberately omitted because daemon derives them on every PUT), calls `putConfig(...)` from `lib/api/config.ts`, and dispatches the discriminated outcome:

- `'ok'` → `configState.applyResponse(outcome.config)` re-hydrates so canonical normalisations (callsign upper-case, gridsquare mixed case) and derived `MyLat`/`MyLon` flow back into the input bindings; info toast "Station updated."
- `'validation'` / `'server'` / `'network'` → error toasts (same shape as the setup-dialog pattern in `app.svelte`).

`saving = $state(false)` flag swaps the button label to "Saving…" and disables it during the round-trip; styling matches the existing primary CTA (`bg-focus`, white, hover `bg-focus-ring`).

**Documentation pass.** This file (current state line + this entry); `docs/v2-design/frontend-spa.md` migration bullet rewritten to mention sub-tabs + Update button; `docs/v2-design/milestones.md` shipped-block updated similarly. No new memory files — both InfoPanel + MyStationPanel demonstrate the parent-tabs-with-icons / sub-tabs-without-icons pattern in code, easy to rediscover by reading the source.

**What's next (carried into session 37+):**

- Country panel UI — wire `pathInfo()` into the panel's short/long-path display + populate `antAz` on submit when remote grid is known.
- Daemon `GET /v1/enrich/callsign` per ADR 0005 — supplies remote gridsquare automatically; the country panel becomes useful end-to-end after this.
- Optional polish: daemon-side string-format validation for zones (`MyCqZone` 1-40, `MyITUZone` 1-90, `MyDxcc` digit-only). Currently round-trip as any string.

**Late session 36 — debug-output diagnostic, toast placement, notification toggles.**

- **Daemon log-level diagnostic.** Operator reported "no debug output despite `level: debug` in config.json". Diagnosis: level wiring is correct; there are simply no `DebugWith` calls in the active hot paths (forwarder worker is the only existing one, `LogStats` is dead-wired). Added two changes: `cmd/smd/main.go` now emits a "logging configured" line at startup with `Str("level", cfg.Logging.Level)` so the operator can verify which level loaded; `internal/api/middleware.go`'s `logRequests` adds a `DebugWith` "http request received" breadcrumb at handler entry, paired with the existing info-level completion line so a debug-configured daemon shows request-in / request-out, while info-only stays completion-only.
- **Toast placement.** Operator confirmed the My Station Update toast was firing but easy to miss in the top-right corner. Moved to top-centre (`fixed top-1.25 left-1/2 -translate-x-1/2` with `items-center` for stacked-toast alignment) in `Toasts.svelte`. Comment block updated to record the rationale.
- **CAT-off default TX power moved to daemon-persisted config.** Operator surfaced the underlying gap: with CAT off, `displayedState.rawPower` was reading `manualState.power` which had a hardcoded `DEFAULT_POWER_WATTS = 100` constant and **no UI to change it**. Every CAT-off QSO silently logged `TX_PWR=100` (or `100 × ampMultiplier`). Replaced with `configState.station.defaultPower` ("set once" model): added `DefaultPower float64` to `types.StationConfig` (validated 0-2000W; 0 means "not set" so ADIF TX_PWR is omitted, matching the existing zero-as-omit rule); extended `StationFields` and `applyResponse`; `displayedState.rawPower` now reads `configState.station.defaultPower` in the CAT-off branch (CAT-reported power still wins when live). Removed `manualState.power`, the `power` localStorage save, the related `manual.test.ts` case, and the now-unused `DEFAULT_POWER_WATTS` constant from `cat.svelte.ts`. Six `displayed.test.ts` cases updated to use `configState.station.defaultPower`; new test pinning "rawPower=0 when defaultPower is unset". `Vfos.test.ts` reset block updated similarly. New numeric input added to the My Station Equipment sub-tab (label "Default TX power (W)"; step=1; min=0; max=2000) with a clarifying note: *"Used only when CAT is unavailable. When CAT is connected, the rig's reported power overrides this. Set to 0 to omit ADIF TX_PWR from QSO records."* Update button payload extended with `default_power`. Three new daemon handler tests pin round-trip + negative + above-cap rejection. The change deliberately simplifies ADR 0009's manualState ownership — power was the only field there with no UI driving it.

- **Notification toggles + "QSO stored" toast.** Per-event-type info-toast preferences added on `qsoDefaults`: `notifyQsoStored` and `notifyConfigSaved`, both default true, both localStorage-backed under `sm.qsoDefaults.notifyQsoStored` / `notifyConfigSaved`. `QsoPanel.submitQso` now emits an info toast `QSO with <CALL> stored.` after a successful submit (gated on `notifyQsoStored`); the contact callsign is captured before the await so the message survives `qsoDraft.clear()`. `MyStationPanel.onUpdate` wraps its existing `Station updated.` toast in a `notifyConfigSaved` check. Errors, validation failures, and duplicates ALWAYS toast regardless of these flags — they're never noise. Two checkboxes added to the QSO sub-tab under a "Notifications" sub-heading. `svelte-check` clean; 351 SPA tests pass.

**Resume point:** operator-driven from the "What's next" bucket above.

**Mid-session polish (later in session 36):**

- **`lib/validators/passthrough.ts`** added — single-line `passthrough = () => true` export with a comment explaining when to reach for it vs writing a per-field validator. `MyStationPanel.svelte`'s 13 free-text `ValidatedInput`s switched from the inline `anyValue` const to the shared import; the inline const removed. Future panels with free-text fields should reuse `passthrough` rather than reintroducing the literal.
- **`activeSection` sessionStorage persistence** added to `MyStationPanel.svelte`. Key `sm.myStation.activeSection`; reads on init via `loadActiveSection()` (validates against the `VALID_SECTIONS` set so a stale/foreign value falls back to `'identity'`); writes via a `$effect` on every change. Same try/catch shape as `SessionTimer.svelte` for private-browsing edge cases. Operator returns to whichever sub-tab they last opened after a page refresh; new tab gets a fresh default.

**QSO sub-tab (later in session 36, 2026-05-05).**

Added a fifth sub-tab to `MyStationPanel.svelte` for QSO emission preferences. Operator originally asked for `qso_random` + `sig` + linear-amp pair; SIG was deferred (special-events use case, not in scope until requested). Final scope: QSO_RANDOM tri-state + amp checkbox + amp multiplier.

- **Daemon — new `types.StationConfig` referenced as `Config.Station`.** `internal/types/station.go` defines `StationConfig { AmpEnabled bool; AmpMultiplier float64 }` — placed in `internal/types` alongside the other config-block DTOs (`DatastoreConfig`, `LoggingConfig`, `ForwarderConfig`, `RigConfig`) per the canonical-DTO project idiom; the api handler imports `types.StationConfig` directly without going through `internal/config`. `internal/config/config.go` references `types.StationConfig` as `cfg.Station` and applies a 1.0 default for `AmpMultiplier` so an unset block reads as "no amp". `internal/api/handler_config.go` adds the block to `ConfigResponse`, accepts it on PUT, validates `AmpMultiplier` in `[0, 1000]` (negative is nonsense, >1000 is a typo guard — real linear amps top out around ×50). Three new handler tests pin round-trip + both rejection paths.
- **Wire shape — `lib/api/config.ts`.** New `StationFields` interface with `amp_enabled` / `amp_multiplier`; `ConfigResponse.station` is required. `putConfig`'s payload signature widened to include `'station'`.
- **`configState.station`.** Existing `enabled` (CAT-enabled, SPA-only) and `ampMultiplier` join a new `ampEnabled` boolean. `applyResponse` now hydrates `ampEnabled` and `ampMultiplier` from `resp.station`; `enabled` stays SPA-only and is not touched.
- **`displayedState.effectivePower`.** Was `rawPower * ampMultiplier`; now `ampEnabled ? rawPower * ampMultiplier : rawPower`. Six tests in `displayed.test.ts` updated to reflect the new gating; `Vfos.test.ts`'s `beforeEach` reset adds `ampEnabled = false`.
- **`qsoDefaults` state module — new file.** `lib/states/qsoDefaults.svelte.ts` exports a singleton with one field: `qsoRandom: 'Y' | 'N' | 'off'`. Default `'off'`. localStorage-persisted under `sm.qsoDefaults.qsoRandom` (per-device, survives reloads, survives tab close — same tier as `manualState`). NOT daemon-persisted because the operator only asked for the amp pair to round-trip. Module-level `$effect.root` for the localStorage write (matches `manual.svelte.ts` pattern).
- **ADIF emission — `lib/utils/adif.ts`.** `AdifQsoFields.qsoRandom?: 'Y' | 'N'`. `formatAdifRecord` emits `<QSO_RANDOM:1>Y` or `N` after `COMMENT`, omits when undefined. Three new tests in `adif.test.ts`.
- **`QsoPanel.submitQso`.** Reads `qsoDefaults.qsoRandom` and passes through to `formatAdifRecord` — `'off'` becomes `undefined` (omitted from ADIF entirely).
- **`MyStationPanel.svelte` — new QSO sub-tab.** Added `'qso'` to `SectionId` union, `sections[]` array, `VALID_SECTIONS`. New tab body has a `<select>` for QSO_RANDOM tri-state (Don't emit / Y / N), a checkbox for amp enabled, a numeric input for amp multiplier (disabled when checkbox is off, `step="0.1"`, `min="0"`, `max="1000"`). Update button payload extended with `station: { amp_enabled, amp_multiplier }`.

`go test ./...` clean; 351 SPA tests pass; `svelte-check` clean.

### Session 35 work (2026-05-05) — completed station-store migration + ADIF MY_* end-to-end

Resumed after the session-34 power outage. Operator confirmed the #42 scope reduction (daemon derives `MyLat`/`MyLon` from `MyGridsquare` only; zones+DXCC stay operator-typed). Worked the task list #42 → #48 in order.

**#42 (Daemon Maidenhead → lat/lon).** New `internal/utils/maidenhead.go` with `IsValidMaidenhead`, `NormalizeMaidenhead`, `MaidenheadToDecimal` (4/6/8-char, cell-centre coords), `MaidenheadToADIFLatLon` (ADIF "XDDD MM.MMM" via existing `ConvertToXDDDMMM`). `internal/utils/maidenhead_test.go` with round-trip + edge-case coverage. `internal/api/handler_config.go`'s `handlePutConfig` now normalises `MyGridsquare`, rejects invalid format with 400, derives `MyLat`/`MyLon` on PUT, clears them when grid is blanked. Four new handler tests in `handler_config_test.go`.

**#43 (configState extension).** `LoggingStationFields` interface in `lib/api/config.ts` tightened — replaced the loose `[key: string]: string | undefined` index with explicit MY_* fields (operator-typed bucket + daemon-derived `my_lat`/`my_lon` + per-QSO `ant_az` + deferred IOTA/SIG fields for wire-shape parity with `types.LoggingStation`). `LoggingStationView` in `states/config.svelte.ts` extended with the operator-typed MY_* set as plain class fields (matching the existing `stationCallsign` / `ownerCallsign` pattern); `applyResponse` hydrates each field with `?? ''` fallback.

**#44 (My Station panel).** `MyStationPanel.svelte` reorganised into four sections (identity → location → equipment → CW). Free-text fields use a permissive `() => true` validator since daemon does the format-correct check on PUT; `MyGridsquare` uses `isValidMaidenhead`. `MyLat` and `MyLon` rendered as read-only display rows (daemon-derived, never edited in the SPA). Section headings use inline Tailwind rather than introducing a new utility class.

**#45 (ADIF emission).** `lib/utils/adif.ts` `AdifQsoFields` extended with the full MY_* set + `operator`/`ownerCallsign` + `antAz`. `formatAdifRecord` emits in stable order: identity (`STATION_CALLSIGN`/`OPERATOR`/`OWNER_CALLSIGN`) → location (`MY_GRIDSQUARE`/`MY_LAT`/`MY_LON`/`MY_STREET`/`MY_CITY`/`MY_POSTAL_CODE`/`MY_COUNTRY`/`MY_ALTITUDE`/`MY_CQ_ZONE`/`MY_ITU_ZONE`/`MY_DXCC`) → personal (`MY_NAME`) → equipment (`MY_RIG`/`MY_ANTENNA`) → CW (`MY_MORSE_KEY_TYPE`/`MY_MORSE_KEY_INFO`) → per-QSO (`ANT_AZ`). Existing canonical-spec tests still pass byte-identical (new fields default to absent → omitted). Added new spec test pinning the byte-identical full-MY_* output.

**#46 (submit-path switch + store deletion).** `QsoPanel.svelte` now reads every identity field from `configState.loggingStation` (`ls = configState.loggingStation`); the legacy `import { station } from '../../stores/station'` and the `get(station)` snapshot block both removed. `lib/stores/station.ts` deleted; the now-empty `lib/stores/` directory removed too. All 326 SPA tests still green.

**#47 (bearing utility).** Operator pointed at the v1 `internal/maidenhead` package as the battle-tested reference. Saved `reference_v1_maidenhead_package.md` memory pointer rather than re-deriving from scratch. New `lib/utils/bearing.ts` ports v1's math: `gridToDecimal` (4/6/8-char extension over v1's 6-only), `calculateBearing` (haversine, rounds to 0.1°), `haversineKm` (`Math.ceil`-rounded), `pathInfo` (bundles short+long path bearings + km/mi distances). Test vectors mirror v1's `bearing_test.go` — JN58td reference coords, London→NY ~288.3°, Munich→New England short+long bearings 180° apart, short+long distance = Earth circumference ±3 km. 21 tests passing.

**#48 (this doc + memory pass).** Updated `docs/v2-design/frontend-spa.md` (persistence-tier table, reactivity-rule examples, the open-item bullet flipped to ✅ with the full migration recap), `docs/v2-design/milestones.md` (the migration ⏳ flipped to ✅ with the implementation summary), this file (current-state line + this work block). Memory updates: `project_sm_station_store_migration.md` content+description rewritten as "done"; `project_sm_adif_my_star_buckets.md` removed the "pending operator confirmation" hedge; `MEMORY.md` index entry hooks updated.

**What's next (carried into session 36+):**

- Country panel UI — wire `pathInfo()` into the panel's short/long-path display + populate `antAz` on submit when remote grid is known (typically from enrichment).
- Daemon `GET /v1/enrich/callsign` endpoint per ADR 0005 (still pending). Would supply remote gridsquare automatically; the bearing/info-panel display becomes useful end-to-end at that point.
- Optional polish: a tiny shared `passthrough.ts` validator export to replace the inline `() => true` literals in `MyStationPanel.svelte` if the duplication starts grating.
- Optional polish on daemon-side validation: for `MyCqZone`/`MyITUZone`/`MyDxcc`, daemon could enforce digit-only / range-checked format. Current state is "any string accepted" — operator can type nonsense and it'll round-trip. Not urgent because the operator is the only consumer of their own typing.

**Resume point:** the bucket of "next hookups" above is operator-driven — pick one. No autonomous follow-up assumed.

### Session 34 in-progress (power-outage save, 2026-05-05; SUPERSEDED by session 35 — preserved for context)

**Operator brief at session start:** continue from session 33's directive — migrate `lib/stores/station.ts` MY_* fields into `configState.loggingStation` and implement the ADIF MY_* field set with My Station panel surface.

**Scoping done:**

- **ADIF MY_* bucketing — confirmed by operator after a follow-up clarification.** The `types.LoggingStation` Go struct already enumerates the full set (`internal/types/logging_station.go`); no new fields needed there. Buckets:
  - **Configurable in My Station (operator-typed):** `MyAntenna`, `MyCity`, `MyCountry`, `MyGridsquare`, `MyName`, `MyPostalCode`, `MyRig`, `MyStreet`, `MyAltitude`, `MyMorseKeyType`, `MyMorseKeyInfo`. Already shipped: `StationCallsign`, `Operator`, `OwnerCallsign`.
  - **Daemon-derived from `MyGridsquare` — surfaced read-only in `configState.loggingStation`, consumed by UI:** `MyLat`, `MyLon` (deterministic Maidenhead → decimal coords). `MyCqZone`, `MyITUZone`, `MyDXCC` were *originally* in this bucket but reclassified as operator-typed (see scope reduction below) — daemon only validates them as strings.
  - **Per-QSO calculated client-side from `(MyLat,MyLon)` + contacted-station coords:** `AntennaAzimuth` (short-path bearing); long-path = (short + 180) mod 360; distance via haversine. Lives in a UI-side `lib/utils/bearing.ts` helper. Writes the short-path value into the ADIF record on submit; country/info panel displays both paths. Daemon does NOT store derived bearing on `LoggingStation` (it's per-QSO, not station identity). **Operator clarification at session 34:** AntennaAzimuth IS needed by the UI (country panel, long-path/short-path indication, physical-map lookup) — the bearing math is the load-bearing reason `MyLat`/`MyLon` need to be surfaced.
  - **Per-activation, deferred (not in this session):** `MyIota`, `MyIotaIslandID`, `MyWwffRef`, `MySig`, `MySigInfo`.

- **Scope reduction applied to task #42 (daemon-side derivation), pending operator confirmation when power returns:**
  - Original proposal: derive `lat`, `lon`, `cq_zone`, `itu_zone`, `dxcc` from `MyGridsquare`.
  - Revised: derive **`MyLat` and `MyLon` only** from `MyGridsquare` (pure Maidenhead math, deterministic).
  - Reason: zone (CQ / ITU) derivation from coordinates requires a polygon dataset; SM doesn't bundle one. The existing `internal/utils/dxcc_iso2.go` is ISO2-country → DXCC, not coord → zone.
  - For v1, CQ/ITU/DXCC stay **operator-typed** (free-text strings, daemon validates format only). A future enrichment hook (or per-QSO hamnut callsign lookup) can fill the contacted station's zones; the operator's home-station zones are a one-time entry that doesn't change.
  - **Operator did not yet confirm this reduction at outage** — message was "Save all of this as we have a power outage..." mid-question. Resume with the question.

**Shipped (committed only via filesystem write — NOT git-committed yet, power outage interrupted):**

- `frontend/logging/src/lib/validators/maidenhead.ts` — Maidenhead grid validator. `^[A-R]{2}[0-9]{2}([A-X]{2}([0-9]{2})?)?$` after trim+uppercase; empty passes (matches the other validators' "presence is a separate concern" pattern).
- `frontend/logging/src/lib/validators/maidenhead.test.ts` — 15 cases: accept 4/6/8-char; case-fold; trim; empty/whitespace pass; reject 5/7-char; reject field>R; reject subsquare>X; reject digit-in-field; reject letter-in-square; reject non-alphanumeric. **All 15 passing** (`npx vitest run src/lib/validators/maidenhead.test.ts`).

**Task list state:**

- `#37` Migrate station store fields into configState.loggingStation — **in_progress** (parent task).
- `#41` Add Maidenhead grid validator (frontend) — **completed**.
- `#42` Daemon: derive lat/lon (revised scope) from MyGridsquare — **pending**, blocked on operator confirming scope reduction.
- `#43` Frontend: extend configState.loggingStation with new MY_* fields — **pending**.
- `#44` Frontend: extend MyStationPanel with operator-editable MY_* fields — **pending**.
- `#45` Frontend: extend formatAdifRecord with new MY_* tag emissions — **pending**.
- `#46` Frontend: switch QsoPanel.submitQso to source ALL identity fields from configState; delete station store — **pending**.
- `#47` Frontend: bearing/distance utility for country panel (short/long path) — **pending**.
- `#48` Doc + memory pass for station-store migration + ADIF MY_* — **pending**.

**Reading recap (state of relevant files at outage):**

- `internal/types/logging_station.go` — already has the full ADIF MY_* set; no Go-type extension needed.
- `internal/api/handler_config.go` — already passes `cfg.LoggingStation = req.LoggingStation` wholesale; new fields auto-flow once tags are added on the wire shape. Validation currently only normalises `StationCallsign`.
- `frontend/logging/src/lib/states/config.svelte.ts` — `LoggingStationView` has `stationCallsign` (plain), `operator` ($state per session-32 contest-mode placeholder), `ownerCallsign` (plain). Needs the new fields added as plain class fields.
- `frontend/logging/src/lib/api/config.ts` — `LoggingStationFields` already has an index signature `[key: string]: string | undefined` that accepts new fields; the named `station_callsign` field stays for documentation. Will gain typed names for the new fields too (TypeScript benefit).
- `frontend/logging/src/lib/stores/station.ts` — legacy Svelte writable; hardcoded defaults `'G4ABC'` / `'KH78an'` / `'My name'` / `'FTdx10'` / `'Hex Beam'`. Delete after consumers are switched.
- `frontend/logging/src/lib/ui/panels/MyStationPanel.svelte` — currently three `ValidatedInput` rows (stationCallsign / ownerCallsign / operator). Will gain rows for the configurable bucket.
- `frontend/logging/src/lib/utils/adif.ts` — `formatAdifRecord` emits MY_GRIDSQUARE, MY_NAME, MY_RIG, MY_ANTENNA today. Will gain MY_CITY / MY_COUNTRY / MY_POSTAL_CODE / MY_STREET / MY_ALTITUDE / MY_MORSE_KEY_TYPE / MY_MORSE_KEY_INFO / MY_LAT / MY_LON / MY_CQ_ZONE / MY_ITU_ZONE / MY_DXCC.
- `frontend/logging/src/lib/ui/panels/QsoPanel.svelte` — `submitQso` still reads `gridSquare`/`name`/`rig`/`antenna` from `get(station)`; only `stationCallsign` switched to `configState`. Switch the rest in #46 once the configState fields exist.

**Resume point on power return:** repeat the open question to operator —

> Confirm scope reduction for #42: daemon derives `MyLat`/`MyLon` from `MyGridsquare` only; `MyCqZone`/`MyITUZone`/`MyDXCC` stay operator-typed (validated as strings)? Or do you want zone derivation in scope, which requires bundling a polygon dataset?

Once confirmed, work down the task list in ID order: #42 → #43 → #44 → #45 → #46 → #47 → #48.

### Session 33 prior current-state summary (preserved for context — superseded by the session-34-in-progress current-state line above)

Session 33 — `qsoDraft` state-module lift shipped: `lib/states/qsoDraft.svelte.ts` is now the in-memory singleton owning the QSO draft fields previously local to `QsoPanel.svelte`. Reactivity audit locked the rule for future enrichment fields (form-bound ⇒ `$state`; submit-only ⇒ plain class field). RST default-fill rule reversed from "operator-typed sticks" to "always overwrite on CW ↔ voice mode flip" — `'59'` is meaningless on CW. QSO ticker rate dropped 60s → 1s so `timeOff` catches the minute boundary within ~1s rather than lagging up to 60s; writes still gated on HH:MM string change. STATION_CALLSIGN source switched from the legacy `lib/stores/station.ts` Svelte store to `configState.loggingStation.stationCallsign` after a daemon-side validator caught the mismatch — patched the one field; the rest of `station` store (gridSquare/name/rig/antenna) still holds hardcoded defaults and must migrate before any new daemon validator touches them. svelte-check 0/0/0 + 302 vitest tests + all daemon tests green. Carried over: ADIF three-callsign fallback chain (daemon one-shot seed + SPA hydration mirror); `/v1/config` GET/PUT end-to-end; SPA boot setup-gate; first-run `config.json` write; SPA-friendly defaults; `types.RigConfig.ID` int64; InfoPanel ARIA tablist; cards/panels convention A; CLAUDE.md "Reuse types.X" idiom.)

### Session 33 work (qsoDraft state-module lift; RST mode-flip rule + 1s ticker; STATION_CALLSIGN source fix)

**Operator brief:** carryover item #6 from session 32's queue (qsoDraft lift) — operator decided to bring it forward because draft + enrichment fields will keep growing as the UI expands, and the second-consumer trigger from frontend-spa.md was no longer the right gate.

**What landed:**

- **`lib/states/qsoDraft.svelte.ts`** — new singleton `QsoDraft` class. 9 form-bound `$state` fields (`callsign`, `name`, `qth`, `comment`, `rstSent`, `rstRcvd`, `qsoDate`, `timeOn`, `timeOff`); plain `qsoStarted` flag (no reactive consumer — read only by imperative method bodies); `$derived` `defaultRst` and `canSubmit`; methods `clear()`, `startQso()`, `tick()`. Module-level `$effect.root` for the RST default-fill effects.
- **Reactivity audit before writing.** Operator pushed back on a default-everything-`$state` shape: "we should keep the overhead down from the beginning rather than refactor later." Per-field audit confirmed all 9 form-bound fields need `$state` (forced by `bind:value`), `qsoStarted` stays plain, `canSubmit` is `$derived`. The audit's payoff is forward — future enrichment fields (`gridSquare`, `country`, `dxcc`, `cqZone`, `ituZone`, `prefix`, `continent`) default to **plain class fields** unless a `bind:value` lands.
- **`QsoPanel.svelte` refactored.** All draft reads/writes routed through `qsoDraft.*`. Mode local + its two `$effect`s stay in the panel — mode is a CAT-state concern (mirrors displayedState/manualState per ADR 0009), not a draft field. Ticker calls `qsoDraft.tick()` from a panel-scoped `setInterval` so module load is side-effect-free.
- **RST mode-flip bug + fix.** Live-test caught: RST fields didn't update when mode flipped USB → CW. Original code's "refill if empty" effect skipped populated values. Reversed the rule — a new `$effect` tracks `defaultRst` (which is `$derived` from `displayedState.mode`) and always writes both `rstSent` and `rstRcvd` to the new value when it changes. Only fires on CW ↔ voice transitions (USB ↔ LSB doesn't touch `defaultRst`). Operator-typed RST values are clobbered on a mode flip — accepted because RST is typically set after the contact (mode is settled by then) and `'59'` on CW / `'599'` on voice are nonsense defaults. Manual-clear refill effect kept for the operator-deletes-field case.
- **Time-off ticker rate bug + fix.** Live-test caught: `timeOff` field appeared not to update. Cause was the original 60s ticker — after Tab at e.g. HH:42:30, the next tick at HH:43:30 was the first chance for `timeOff` to flip from "HH:42" to "HH:43", giving up to 60s of lag. Dropped to 1s; writes still gated on HH:MM string change so cost is one Date construction + format + compare per second (negligible) but the field catches the minute boundary within ~1s.
- **STATION_CALLSIGN source fix.** Live-test caught: daemon's QSO-submit validator rejected the record with "STATION_CALLSIGN does not match the logbook's callsign" even though config.json and DB both held the correct callsign. Cause: `submitQso` was reading `stn.stationCallsign` from `get(station)` (the legacy `lib/stores/station.ts` writable, which has hardcoded defaults like `'G4ABC'`). My Station card writes to `configState.loggingStation`, not the store, so the wire value was the hardcoded default. Switched `stationCallsign` source in `formatAdifRecord` call to `configState.loggingStation.stationCallsign`; the other identity fields (`gridSquare`, `name`, `rig`, `antenna`) still source from `get(station)` because `/v1/config` doesn't surface them yet. Logged the migration as carried item.
- **New memory file** — `project_sm_station_store_migration.md` captures the trap so future sessions don't add daemon validators against MY_* fields without first migrating the source out of the legacy store.

**Doc audit (this section):**

- `docs/v2-design/frontend-spa.md` — state-layer table replaces "QsoPanel's draft fields" with `qsoDraft`. The QSO-submit "Wire-shape decisions" footnote rewritten to explain the split-source identity (and the migration ordering constraint). The "QSO draft store" open-question entry resolved with full detail (reactivity audit, RST mode-flip rule, 1s ticker, mode stays panel-local). New open-question entry added for the `station` store migration.
- `docs/v2-design/milestones.md` — added "qsoDraft state-module lift" under Milestone 1c-shipped block; added "Migrate `lib/stores/station.ts` MY_* fields" to the in-progress list.
- `docs/session-handoff.md` (this file) — current-state line and Session 33 block.
- Memory `project_sm_station_store_migration.md` (new) — captures the daemon-validator-vs-store-defaults trap.

**Verified:** `npx svelte-check` 0 errors / 0 warnings / 211 files; `npx vitest run` 17/17 files, 302/302 tests.

### Session 32 work (ADIF three-callsign fallback chain — daemon + SPA + MyStationPanel + tests)

**Operator brief:** scaffold-and-step-in pattern continued from session 31. Operator was wiring up MyStationPanel content; spotted that the data model only carried `station_callsign` while ADIF defines three load-bearing callsign fields (`STATION_CALLSIGN`, `OPERATOR`, `OWNER_CALLSIGN`) with explicit fallback rules between them.

**What landed:**

- **`configState.loggingStation` extended.** Added `operator` and `ownerCallsign` alongside `stationCallsign` in `frontend/logging/src/lib/states/config.svelte.ts`. All three are **plain class fields** (no `$state`) — operator initially proposed `$state` for `ownerCallsign`, then corrected: these are static-ish ADIF MY_* identity fields hydrated from `/v1/config`, with no reactive consumers. Operator note triggered a memory correction: the rule is "`$state` for any reactivity (template, `$derived`, `$effect`)" — `$derived` is one trigger, not the only one. `InfoPanel.svelte`'s `activeTab: TabId = $state('worked')` is the canonical counter-example.
- **SPA fallback chain** in `applyResponse(...)`: empty `station_callsign` falls back to `operator`; empty `owner_callsign` falls back to the (post-resolution) `stationCallsign`. Implemented with `||` (truthy chain — `?? ''` was redundant noise that operator flagged on review).
- **Daemon materialisation** in `internal/api/handler_config.go`'s setup-transition block. On the false→true `SetupComplete` flip, when the request body leaves `Operator` / `OwnerCallsign` empty, the daemon copies `incomingCall` into both. One-shot — post-setup PUTs don't re-seed; My Station panel edits are authoritative including blanking either field. Club-station case (request supplies all three distinct callsigns) flows through unchanged.
- **Three new daemon tests** in `handler_config_test.go`:
  - `TestHandlePutConfig_FirstSetup_MaterialisesOperatorAndOwner` — confirms first-setup seed.
  - `TestHandlePutConfig_FirstSetup_RespectsOperatorAndOwnerWhenProvided` — confirms club-station case (request-supplied values preserved).
  - `TestHandlePutConfig_PostSetupNoMaterialisation` — confirms one-shot semantics: post-setup PUT that blanks both fields stays blank.
  All 10 PUT-config tests green.
- **`MyStationPanel.svelte` refactor.** Replaced the read-only `<div class="input-base">` placeholders with three `ValidatedInput` instances, each wired to `isValidCallsign` and `bind:value` against `configState.loggingStation.{stationCallsign, ownerCallsign, operator}`. Operator added the third (operator) field while reviewing. svelte-check 0/0/0.

**Doc audit (this section):**

- `docs/v2-design/api.md` — `PUT /v1/config` section gains the "ADIF identity materialisation (one-shot, setup transition only)" bullet describing the daemon-side fallback rule.
- `docs/v2-design/frontend-spa.md` — Reactivity rule corrected (was "no `$state` without `$derived`"; now "`$state` for any reactive consumer"). Three-callsign distinction expanded with the fallback chain and pointers to both materialisation sites.
- `docs/v2-design/milestones.md` — added "ADIF identity fallback chain" line under Milestone 1c-shipped block.
- Memory `project_sm_spa_config_layering.md` — reactivity rule corrected (already done mid-session).

**Verified:** `go test ./internal/api/ -count=1` 0 fails; `npx svelte-check` 0 errors / 0 warnings.

### Session 31 work (first-run config write; /v1/config GET/PUT with setup-transition logbook seed; SPA setup dialog; defaults overhaul; RigConfig.ID → int64; InfoPanel refactor)

### Session 31 work (first-run config write; /v1/config GET/PUT with setup-transition logbook seed; SPA setup dialog; defaults overhaul; RigConfig.ID → int64; InfoPanel refactor)

**Operator brief:** continue from session 30's carried queue, top-down. Step #1 (seed default logbook) reframed mid-session into a broader "operator can install + run with one prompt for callsign" flow that pulled in #2 (`/v1/config`) ahead of schedule. Two side discussions captured before code: (a) the parallel-struct anti-pattern caught and made explicit in CLAUDE.md after I drifted into it twice, (b) the consumer-driven shape of GET `/v1/config` settled by enumerating who reads it.

**What landed (daemon + types):**

- **First-run config write** — `internal/config/config.go` gains `WriteJSON(path, cfg)` (atomic temp+rename, indented JSON). `cmd/smd/main.go`'s `loadConfig` now seeds a default `config.json` at the resolved candidate path when no file exists yet, then loads it back. Returns the written path so `run()` can emit a structured `first run: wrote default config to disk` line in `smd.log` once the logger is up. `firstRunWrite` falls back to in-memory defaults if the disk write fails so the daemon still starts. `cfgSvc.SetPath(...)` records the path so `/v1/config` PUT knows where to atomically rewrite.

- **Defaults overhaul** — flipped to a "fresh install just runs" shape:
  - `Server.Protocol`: `unix` → **`tcp`**.
  - `SocketPath`: gated on protocol — `127.0.0.1:8080` on TCP (matches Vite proxy), `/tmp/smd.sock` on Unix.
  - `Server.ServeSPA`: derived `true` automatically (was already protocol-gated; flipping protocol flipped this).
  - `Logging.WithTimestamp`, `FileLogging`, `LogFileCompress`: **set in `DefaultConfig` only**, NOT in `applyDefaults`, so an operator's explicit `false` in their edited config isn't silently flipped on at every Load. Long-term `*bool` migration noted as follow-up.
  - `Logging.RelLogFileDir`: `"logs"` → **`"log"`** (matches `build/log/.gitkeep` convention).
  - `Datastore.Path`: `${DataDir}/station-manager.db` → **`${DataDir}/db/station-manager.db`** (matches `build/db/.gitkeep`).
  - `Logging.LogFileMaxSizeMB / LogFileMaxBackups / LogFileMaxAgeDays`: 100 / 5 / 30 (was zero — using lib defaults).
  - Tests added: `TestDefaultConfig_TCPAndServeSPADefaultsOn`, `TestLoad_UnixProtocolKeepsUnixSocketDefault`, `TestLoad_OperatorFalsePreserved` (regression guard for the *bool trap).

- **`Config` struct gained four fields:**
  - `LoggingStation types.LoggingStation` — full embed (canonical-DTO rule, see CLAUDE.md change below).
  - `SetupComplete bool` — server-managed; flipped false→true on the first PUT that supplies a non-empty `station_callsign`.
  - `DefaultLogbookID int64` (default 1).
  - `DefaultRigID int64` (default 1).

- **`config.Service` gained mutex-guarded mutation:**
  - `Snapshot() Config` — RLock; returns copy.
  - `Update(fn func(*Config) error) error` — Lock; copies cfg, applies fn, atomic-writes file via `WriteJSON`, then commits in-memory state only on disk-write success.
  - `SetPath(p string)` — recorded once at startup.

- **`/v1/config` GET/PUT** (`internal/api/handler_config.go`, new file):
  - **`ConfigResponse`** local projection struct — embeds `types.LoggingStation`, `types.Logbook`, `types.RigConfig` directly. No parallel field definitions.
  - **GET** reads `cfgSvc.Snapshot()`, joins the logbook row from DB by `DefaultLogbookID` (returns `{id: N}` stub on `ErrNotFound`); rig join deferred until CAT lands.
  - **PUT** validates callsign with `isValidCallsign`, gates the setup-transition (`!current.SetupComplete && incomingCall != ""`) on inserting a logbook row at id=1 with the operator's callsign, then `cfgSvc.Update(...)` persists `LoggingStation` + (on transition) `SetupComplete=true` and `DefaultLogbookID=<seeded id>`. Server-managed `setup_complete` from the body is ignored; non-writable joined fields (`default_logbook.name` etc.) are ignored.
  - 7 handler tests covering pre-setup GET, first-setup PUT (with logbook fetch verifying the seed), normalisation, validation, empty-callsign-accepted, server-managed flag immune to client overrides, idempotent re-PUT, file persistence.
  - New error code `config_write_error` added to api.md table.

- **`types.RigConfig.ID`: `string` → `int64`** — settled the open from `cat-serial-reuse.md` §7.5. Originally a free-form operator label; no consumer ever materialised that needed strings (CAT lib looks up by `Model`, not ID). Numeric matches `Logbook.ID int64` and lets `default_rig_id` default to 1. Blast radius: zero (no code consumers yet). Doc comment updated; ADR §7.5 closed with the decision recorded.

**What landed (SPA):**

- **`frontend/logging/src/lib/api/config.ts`** (new) — typed `fetchConfig()` and `putConfig(payload)` clients matching `ConfigResponse`. Discriminated union `ConfigOutcome` (`ok` / `validation` / `server` / `network`) — same shape as `qso.ts` for consistency.

- **`frontend/logging/src/lib/states/config.svelte.ts`** — extended with `setupComplete`, `loaded`, `loggingStation`, `defaultLogbook`, `defaultRig` views + `applyResponse(resp)` (field-by-field hydration to preserve $state reactivity boundaries) + `markLoaded()` (failure-path sentinel). Existing `station: {enabled, ampMultiplier}` block (CAT-side concern, ADR 0009) untouched.

- **`frontend/logging/src/app.svelte`** — `onMount` calls `fetchConfig()` and dispatches by outcome. Top-level render gate: `{#if configState.loaded}` then `{#if !setupComplete}{@render setup()}{:else}{@render main_app()}{/if}{/if}`. Loading window renders nothing (fast on localhost; toasts mount point always live for failure surfacing). Operator scaffolded the setup card markup with form input + Save button; `putCallsign()` PUTs to `/v1/config`, on `ok` calls `configState.applyResponse` (flips `setupComplete=true` reactively → main_app renders).

- **`InfoPanel.svelte` refactor** — replaced four hand-duplicated tab blocks with:
  - Typed `TabId = 'worked' | 'details' | 'station' | 'session'` union.
  - `tabs[]` data array driving the markup loop.
  - Single `tabItemClass(isActive)` helper (was two functions).
  - ARIA: `role="tablist"` on a `<div>` (not `<nav>` — landmark/widget conflict), per-tab `role="tab"` + `aria-selected` + `aria-controls`, matching `id="panel-X"` + `role="tabpanel"`.
  - Icon snippets per-tab dispatched by id inside the loop.
  - Imports `WorkedPanel`, `DetailsPanel`, `MyStationPanel`, `SessionPanel` (operator scaffolded these four files; content TBD).
  - New `.tab-item` / `.tab-button` component classes in `app.css`. The cursor-inheritance trick (`cursor-[inherit]`) is now baked into `.tab-button` so future tab navs reach for the class.

- **`LoggingCard.svelte`** — fixed `w-17Wiat` typo → `w-17` (silently dropped by Tailwind, so it had been a no-op).

- **Bug fixes during live testing:**
  - "Unable to load config" briefly flashed on every page refresh because the `!loaded` branch was used as a *loading* state with failure-state wording. Replaced with render-nothing-while-loading.
  - `cursor-pointer` on a parent `div` didn't propagate to a child `<button>` — browser default cursor on `<button>` overrides the parent. Fixed by `cursor-[inherit]` on the button (now extracted to `.tab-button`).
  - Initial 500 from `GET /v1/config` was diagnosed as the daemon listening on Unix socket while the Vite dev proxy targets `localhost:8080` — feedback that drove the TCP-by-default decision.

**What landed (docs + memory):**

- **CLAUDE.md** — new project idiom: *"Reuse `types.X` rather than building parallel structs."* Promoted from the memory entry to CLAUDE.md so it's loaded into every session, not just the ones where the relevant memory triggers.

- **`docs/v2-design/api.md`** — new "Config" subsection under §7a Landed endpoints. Wire shape, source-of-truth split, writable vs server-managed vs read-only-join field rules, setup-transition semantics. New `config_write_error` row in the error code table.

- **`docs/v2-design/cat-serial-reuse.md`** — §7.5 RigConfig ID decision recorded (string → int64, settled session 31, blast radius zero, why).

- **`docs/v2-design/milestones.md`** — acceptance-test daemon-launch snippet updated to remove the `--config ./config.json` flag (the first-run write makes that unnecessary).

- **`internal/config/doc.go`** — first-run-write paragraph added.

- **Cards/panels naming convention "A" locked:** `cards/` = page-level shell (LoggingCard); `panels/` = content blocks within a card (QsoPanel, CountryPanel, InfoPanel, plus the four upcoming tab-content panels). "X card" in conversation is acceptable shorthand; file names use Panel.

**Verification:**

- Full Go test suite green under `-race` — 24 packages, all green.
- Frontend `npm run check`: 0/0 errors/warnings, 206 files.
- Frontend `npm test`: 302/302 pass across 17 files.
- Live end-to-end: fresh install → daemon writes default config → SPA loads → setup dialog renders → operator types `M0XYZ` → daemon seeds logbook + persists config → setup_complete flips → main_app renders. Toasts cover all failure paths.



### Session 30 work (SPA → daemon submit wiring; mode-resolver; ADR 0015 omitempty pass)

**Operator brief:** "Yes" to the carried session-29 next-step #1 — wire the SPA's `QsoPanel.submitQso()` to the daemon's `POST /v1/qso?logbook=<id>` endpoint.

**What landed:**

- **`frontend/logging/src/lib/api/qso.ts`** (new file) — thin daemon wrapper. Exports `submitQso(adif: string, logbookID: number): Promise<SubmitOutcome>` where `SubmitOutcome` is a discriminated union with five `kind`s:
  - `'stored'` (201) — fresh QSO persisted, draft can be cleared.
  - `'duplicate'` (200) — dedupe match on existing QSO; daemon did not write.
  - `'validation'` (4xx) — daemon-emitted code+message (e.g. `invalid_adif`, `callsign_mismatch`).
  - `'server'` (5xx) — daemon logged full chain; SPA shows generic retry.
  - `'network'` — fetch threw before getting a Response.
  
  Wraps fetch in try/catch for the network arm; treats unparseable JSON body as a synthesised `unknown_error` envelope. Logbook id is `encodeURIComponent`'d defensively.

- **`frontend/logging/src/lib/api/qso.test.ts`** (new file, 8 cases) — covers each outcome arm, the request shape (URL/method/Content-Type/body), and the unparseable-body fallback.

- **`frontend/logging/src/lib/ui/panels/QsoPanel.svelte`** — `submitQso()` is now `async`. The previous `console.log('[QSO submit] ADIF payload:\n' + adif)` block is replaced with a call to `submitQsoToDaemon(adif, DEFAULT_LOGBOOK_ID)` and a `switch` on the outcome:
  - `stored` → `clearDraft()` (operator continues to next QSO).
  - everything else → draft preserved; `console.warn` (duplicate / validation) or `console.error` (server / network). The "preserve the operator's typing on failure" pattern is baked in until ADR 0008's toast system lands.
  
  `DEFAULT_LOGBOOK_ID = 1` constant added at the top of the script with a comment naming the logbook switcher as the future replacement. Imported as `submitQso as submitQsoToDaemon` to avoid colliding with the local `submitQso` handler name.

**Verification:**

- `npm run check` (svelte-check): 0 errors / 0 warnings, 199 files.
- `npm test` (vitest, full suite): 276/276 pass across 15 files (was 268; the 8 new cases account for the delta).
- Browser/daemon end-to-end test deferred — daemon spin-up wasn't run this session; the unit tests + the wire-contract docs in api.md §7a are the proxy. First real-traffic verification will land alongside the next browser session.

**Documentation updated:**

- **`docs/v2-design/frontend-spa.md`** — "ADIF wire shape" section now references `lib/api/qso.ts` (added 2026-05-03) and the `SubmitOutcome` discriminated union; the "QSO draft store" open-question bullet's "produces ADIF console output" line is rewritten to "wired to `POST /v1/qso?logbook=1` via `lib/api/qso.ts`; on `stored` outcome the draft clears, on every other outcome the draft is preserved and a console warn/error logged until ADR 0008's toast system lands."

**Mode-vs-submode resolver (mid-session, after the first 7Q5MLV QSO got rejected as `MODE "USB" is not a recognised mode`):**

ADIF distinguishes MODE (parent: SSB, MFSK, PSK, CW, FM, AM, RTTY, DIGITALVOICE, HELL, PACKET) from SUBMODE (the variants: USB/LSB under SSB, FT8/FT4 under MFSK, PSK31 under PSK, CW-N under CW, etc). The operator-facing dropdown — and rigs over CAT — speak in the names operators say at the mic, which are mostly submodes. The SPA was passing those values through as `MODE` and the daemon's strict ADIF-MODE enum rejected them.

- **`frontend/logging/src/lib/utils/mode.ts`** (new, 110 lines) — `resolveModeAndSubmode(opMode, opSubMode)` returns a `(MODE, SUBMODE)` pair. Mirrors the daemon's `submodeToMode` table. Three rules: submode-named-mode → resolve to (parent, submode); main-mode → pass through with whatever subMode was passed; empty / unknown → return as-is for the daemon to reject. Trim + uppercase.
- **`frontend/logging/src/lib/utils/mode.test.ts`** (new, 12 cases) — every translation arm + edge cases (whitespace, case, unknown values, override-on-submode-conflict).
- **`QsoPanel.submitQso()`** — calls the resolver before `formatAdifRecord` so the ADIF record carries `MODE=SSB, SUBMODE=USB` (or the appropriate pair) on the wire.
- **`internal/enums/modes/modes.go`** — added `"FT8": MFSK` to `submodeToMode`. The map had FT4 but FT8 was missing; would have failed any FT8 QSO.

**End-to-end verification (mid-session):** logged 7Q5MLV (Marc, Mzuzu, 14.250 MHz USB, RST 59/59, 100 W, Hex Beam, KH78an grid, FTdx10) via the SPA against the running daemon. Confirmation in the daemon log: `INF QSO stored band=20m call=7Q5MLV freq_mhz=14.250 logbook_id=1 mode=SSB qso_id=2`. The first real QSO logged through the v2 stack.

**ADR 0015 — `additional_data` blob omits empty fields:**

Inspection of the stored QSO's row showed ~80 keys, only ~18 carrying values. The other ~60 were `"field":""` noise — the asymmetric-round-trip lesson surfaced in real data: `types.QsoDetails` had eight `,omitempty` fields documented as "for the importer," but every other field in the blob had the same write-only-when-set rationale and was missing the tag. Operator decided to finish the rule.

- **ADR 0015 (`docs/decisions/0015-additional-data-omits-empty-fields.md`)** — new, status Accepted. Documents the rule, the asymmetric-round-trip motivation, three rejected alternatives (status quo, `*Country` pointer, custom `MarshalJSON`), the read-path impact (none — `json.Unmarshal` already handles absent fields), and triggers to revisit.
- **`internal/types/qso.go`, `qso_details.go`, `contacted_station.go`, `logging_station.go`, `qsl.go`, `country.go`** — every string and numeric field gets `,omitempty`. `contact_history` slice + `country_details` embed also tagged. Per-struct package comments rewritten to describe the ADR 0015 rule (replacing the old "some fields use omitempty for the importer" notes).
- **No migration.** Operator's call: dev DB, blow it away.
- **Tests green unchanged.** Pre-flight audit confirmed all `additional_data`-asserting tests check key *presence* (after `json_set` stamps), not key count or full blob equality. Full `go test ./... -race` passes (24 packages, ~26s).
- **Wire-format change worth noting:** `Country.ID` previously marshalled as `"ID"` (no json tag, default Go field name); now `"id"` (lowercase + omitempty). Consistent with every other field.

**Documentation updated for ADR 0015:**

- `docs/v1-analysis/invariants.md` § "additional_data absorbs ADIF spec evolution" — added the ADR 0015 callout paragraph.
- `docs/v1-analysis/lessons-for-v2.md` § "What v1 got right" item 1 — added "(refinement, ADR 0015)" note.
- `docs/v1-analysis/design-decisions-log.md` § "additional_data JSON blob column" — header changed to "**KEEP (refined by ADR 0015, 2026-05-03)**", rationale paragraph extended to cover the uniform empty-omission rule, related-links section updated.

**Documentation updated for the mode-resolver:** none beyond this entry — the resolver is implementation detail; api.md §7a's MODE description is already correct ("ADIF MODE = parent family, e.g. SSB"); the SPA-side translation is captured here as the canonical record.

**ADR 0008 toast system built (after a placeholder banner exposed the wrong-place problem):**

A first-cut feedback banner inside QsoPanel proved the daemon-error round-trip end-to-end (a fresh-DB submit hit `404 logbook_not_found` and surfaced correctly), but the operator immediately flagged that submit feedback isn't QsoPanel-scoped state — v1 used top/bottom-right toasts for exactly this. ADR 0008 already specified the right answer; we built it.

- **`frontend/logging/src/lib/states/toasts.svelte.ts`** (new, ~140 lines) — `$state`-array singleton with `pushToast(level, message, ttl?)`, `dismissToast(id)`, and `toasts.{info,warn,error,dismiss}` shortcuts. Per-level default TTLs (info=4s/warn=6s/error=8s); `ttl=0` opts out for sticky toasts; max-stack=5 with oldest-evicted semantics; pending timers cancelled on manual dismiss and on eviction.
- **`frontend/logging/src/lib/states/toasts.test.ts`** (new, 14 cases) — push assignment + monotonic ids + level TTL defaults + explicit-ttl override + sticky `ttl=0` + auto-dismiss timing + per-level TTL + manual dismiss cancels timer + idempotent unknown-id dismiss + max-stack eviction with timer cleanup + convenience-wrapper shape.
- **`frontend/logging/src/lib/ui/Toasts.svelte`** (new, ~50 lines) — fixed `top-right` flex column with `flex-col` (oldest-at-top, newest appended below — calmer than `flex-col-reverse`'s shift-on-each-push); `svelte/transition`'s `fade` (150 ms) for in/out; `pointer-events-none` on the container, `pointer-events-auto` on each toast (clicks pass through gaps); `aria-live="polite"` and `role="status"` for accessibility; each toast is a `<button>` so the entire surface is click-to-dismiss without nested-control issues. Position revised from ADR 0008's original `bottom-right` after the operator surfaced that v1 used top-right; ADR text amended to record both the change and the corrected rationale (the entry form sits inside a finite-size shell, not at the top of the viewport, so a top-right toast doesn't obscure it).
- **Tailwind cluster in `styles/app.css` `@layer components`** — `.toast-base` (shape + shadow + max-w-sm) and `.toast-info`/`.toast-warn`/`.toast-error` (colour palette, indigo / amber / rose per ADR 0008's reference). Same convention as `.input-base` / `.invalid-input`.
- **Mount in `app.svelte`** — `<Toasts/>` rendered as a sibling of `<main>` so the fixed-position overlay isn't constrained by the shell's z-index stack.
- **`QsoPanel.svelte` rewired** — banner `$state`, helper, and template block removed; the four non-stored `SubmitOutcome` arms now call `toasts.warn(...)` (duplicate) or `toasts.error(...)` (validation / server / network). Net: −40 lines.

**Verification:** `npm run check` 0/0; `npm test` full suite 302/302 (was 288 pre-toast; +14 toast tests). Browser path will be re-verified next session, but the unit-test coverage of TTL/eviction/dismiss + the daemon-side first-real-QSO from earlier in the session means the wiring is on solid ground.

**Documentation updated for ADR 0008:**

- `docs/decisions/0008-notifications-toast-system.md` — References section flipped from "(Planned)" to "(built 2026-05-03)" with file paths; first-consumer line names QsoPanel's submit-outcome arms.
- `docs/v2-design/frontend-spa.md` — "Open questions" toast entry rewritten from "Resolved" to "Resolved + built 2026-05-03" with module API summary; the QSO-draft-store entry's "until ADR 0008 lands" wording replaced with the live behaviour.

**Daemon access log added (`logRequests` middleware):**

The operator surfaced that 4xx failures (the just-tested `404 logbook_not_found`, plus all `400 invalid_field_value`, `409 duplicate_key`, `400 callsign_mismatch`) leave no trace in `smd.log`. `writeError` builds a JSON response but emits no log; only `writeServerError` (5xx) and panic-recovery were observable. For dev work and bug-tracing this gap is real — invisible 4xx failures mean no log evidence to share when something behaves wrong.

Fix: outermost middleware that emits one structured `INF http request` line per completion.

- **`internal/api/middleware.go`** — added `logRequests`, `responseRecorder`, `clientIP`. The recorder wraps `http.ResponseWriter` to capture status (defaults to 200, mirroring net/http's implicit-WriteHeader) and bytes written; it forwards `Flush()` so SSE handlers keep working. `clientIP` strips RemoteAddr's port and honours `X-Forwarded-For`'s first hop for LAN reverse-proxy scenarios. The middleware sits as the outermost wrap so it captures every shape of completion uniformly: 2xx/3xx normal returns, 4xx from `writeError`, 5xx from `writeServerError`, 503 from the concurrent / subscriber caps, 500 from `recoverPanic`. Level stays Info regardless of status — operators grep `status:5` / `status:4` for sweep filtering.
- **`internal/api/server.go`** — handler chain reordered to `logRequests(limitConcurrent(recoverPanic(mux)))`. Access log outside the concurrent cap so 503-rejected requests stay observable; recovery inside the cap so a panicked handler still releases its slot.
- **`internal/api/middleware_test.go`** — six new cases: status defaults to 200, explicit status pinned (first WriteHeader sticks), Flush passthrough for SSE, response pass-through unchanged, `clientIP` strips port, `clientIP` honours X-Forwarded-For first hop.
- **`docs/v2-design/api.md` §"Load limits and middleware"** — updated to list `logRequests`, document the line shape, and re-state the chain order.

`go test ./... -race` green across all 24 packages. Next daemon restart will start emitting access-log lines into `build/log/smd.log` — the missing-logbook submit will now produce `INF http request method=POST path=/v1/qso status=404 duration_ms=2 …` per attempt.

**Two follow-up refinements after the first access-log lines landed without enough context:**

1. **Timestamps were missing from every JSON log line.** Root cause: `with_timestamp` is a `bool` config field that defaults to false on JSON unmarshal, and the operator's `build/config.json` didn't set it. Fixed by adding `"with_timestamp": true` to the operator's config block. Did NOT promote to a forced default in `applyDefaults` because the `bool` shape can't distinguish "absent" from "explicitly false," and existing `internal/logging` tests rely on `WithTimestamp: false` to produce stable test fixtures. A future tri-state migration (`WithTimestamp *bool` like `ServeSPA`) would let operators see the default-true behaviour without breaking those tests.

2. **The 4xx access-log line carried no error context.** `status=404` alone wasn't useful for bug tracing — the line said *something* failed without saying *what*. Threaded the envelope's `code` / `error` / `op` fields up to the access log via a new `responseRecorder.noteError(...)` hook called from `writeError`. The access-log line now includes those three fields when status >= 400, so a tracing operator sees `INF http request method=POST path=/v1/qso status=404 code=logbook_not_found error="logbook does not exist" op=api.handleSubmitQso`. 5xx detail (the wrapped error chain) still lives on `writeServerError`'s separate ERR line; the access-log line is the request-level summary either way. Test coverage: a new case verifies the recorder captures `errCode` / `errMessage` / `errOp` from a `writeError` call, and that the first call wins (defence-in-depth against a buggy handler calling `writeError` twice).

**Frontend logging deliberately deferred.** `build/log/logging.log` is stale (from the parked Gio `cmd/logging` app — last touched 2026-04-29, 17 KB). The browser SPA writes nowhere on disk; only `console.warn`/`console.error` to DevTools. For a single-operator desktop app, DevTools is the right tool — operator IS the developer. A `POST /v1/log/client` endpoint can land later if there's a need to persist SPA logs server-side.

**Toast UX iterations after the initial build (in chronological order, all live):**

1. **Position flipped bottom-right → top-right.** ADR 0008's original spec was `bottom-4 right-4` with the rationale "keep the top of the viewport clear for the QSO entry area." Operator pointed out v1 used top-right and the framing was wrong — the entry form sits in a finite-size shell, not at the top of the viewport, so a top-right toast doesn't obscure it. ADR 0008 amended in-place to record both the change and the corrected rationale.
2. **Stacking direction set to `flex-col` (oldest-at-top, new toasts append below).** Considered `flex-col-reverse` to put the newest at the top, but that shifts existing toasts on every push — calmer to leave older entries in place and have the queue grow downward.
3. **Severity prefix rendered by `Toasts.svelte`, not by callers.** Initial pass had each call site bake `"Error: …"` / `"Warning: …"` into the message string — fragile (caller has to remember; conflicts with prefixes already in messages like "Daemon error: …"). Hoisted to `Toasts.svelte` as a `<strong>{levelLabel(level)}</strong>` rendered before the message body. Single source of truth + accessibility (`aria-live="polite"` reads "Error" / "Warning" aloud, conveying severity to screen-reader / colour-blind operators without depending on the colour palette).
4. **Toast message text simplified.** Validation arm now passes the daemon's `message` directly (e.g. `"logbook does not exist"` instead of `"logbook_not_found: logbook does not exist"`) — the wire-protocol `code` is operator-noise but useful in dev console for grepping daemon logs, so it goes to `console.warn` / `console.error` instead. Server arm dropped its redundant `"Daemon error: …"` prefix (the level label does that work). Network arm rewrote to a friendly "Cannot reach the daemon — check it is running." with the underlying fetch error going to console only.

**Recap of v1/v2 phrasing (memory hardening):**

I regressed mid-session and called pre-rewrite packages "v1 carry-forward" — the operator caught it. Same fix as session 29 plus a memory file (`feedback_no_v1_carry_forward_phrasing.md`) explicitly forbidding that phrasing. Correct framing: codebase on `main` is v2 in full; the `v1` branch + `v1.0.0` tag preserve the Wails app; `/v1/` URL is API versioning, unrelated. Pre-rewrite packages are "preserved from the prior tree," never "v1 anything."

**What did NOT change:**

- No SQL schema changes. No migrations.
- The wire contract in api.md §7a is unchanged. Daemon endpoint behaviour is identical; only the in-memory struct's marshal output (per ADR 0015) and access-log surface (new INF lines) changed.
- `qsoDraft` state-module lift is still deferred; QsoPanel still owns the draft as local `$state`.
- Inline validation message slot (Fix 13) is still unbuilt — operator-facing field-level validation today comes via a single toast carrying the daemon's message, not per-field error markers.
- First-launch DB has no logbooks; `?logbook=1` returns 404. Deliberately deferred (was the fixture for the toast-system verification).
- `cmd/logging` Gio app is still parked; no work this session.

### Session 29 archive (was: 2026-05-02, session 29 — daemon audit: existing v2 milestone-1 daemon (`cmd/smd`) is fully shipped and serving the wire contract the SPA targets; api.md §7a / milestones.md / handoff updated to reflect what's actually landed; no code changes this session)

### Session 29 work (audit & capture: what landed in the daemon)

**Operator brief at session start:** "Let's attack the daemon and get some QSOs logged." Initial audit revealed the daemon's `POST /v1/qso` endpoint (and most of milestones 1, 1b, 1c) had already shipped — back when the daemon itself was the first slice of the v2 rewrite — but the v2-design docs had drifted. The session pivoted to closing that capture gap before any new daemon code lands.

**Clarification mid-session:** v1 was a Wails app with no daemon. The daemon was the very first work of the v2 rewrite (session-8 cluster). The `/v1/` URL prefix is the **API** version, not the **project** version. **And the v2 milestone-1 restructure already happened.** The current `main` IS structure.md's milestone-1 target layout: Wails `apps/` gone, `go.work` gone, daemon + new internal packages exist, `internal/forwarding/` reshaped from v1's hardcoded-QRZ to multi-destination `Forwarder` interface + worker + registry. Carry-forward packages from v1 (`types`, `adif`, `errors`, `iocdi`, `enums`, `config`, `logging`, `utils`) kept their package paths but were code-reviewed and corrected; new packages (`api`, `qsoservice`, `events`, `safego`) are fresh v2. The only remaining structural addition planned is `internal/bridge` per ADR 0013 (a new package, not a reshape).

**CLAUDE.md updated.** It previously said "main is still at the v1.0.0 layout; the restructure commit … has not yet landed" — that was wrong and was repeated in early drafts of this session before the audit ran. The "Repository structure" section is now rewritten to say main IS the milestone-1 layout, with the UI-toolkit progression (Wails → Gio → SPA) laid out explicitly and ADR 0013 named as the only outstanding *structural* addition.

**Documentation updates:**

- **`docs/v2-design/api.md` §7a** — section retitled from "(as of session 9, 2026-04-17)" to "(current daemon state, audited 2026-05-02)" and given an **Origin note** explaining v1 had no daemon (the daemon is v2 milestone-1 work, the restructure already ran) and that `/v1/` is API versioning. Added landed-endpoint entries that the previous §7a was missing: `GET /v1/qso/{id}/uploads`, `GET /v1/contact-history`, `GET /v1/version`. Added three new subsections: **Transport, listener and SPA hosting**; **Load limits and middleware**; **Server-config knobs** (full table of every `Server.*` field).
- **`docs/v2-design/api.md` §6** — added status note: the "minimal floor" for accidental-self-DoS has shipped.
- **`docs/v2-design/api.md` §1 (Consumer enumeration)** — replaced the Wails-era `apps/logging` / `apps/logbook` / `apps/config` consumer table with the current SPA shape per ADR 0001 + ADR 0013. Original table preserved as historical record.
- **`docs/v2-design/api.md` §3 (Config reload)** — replaced `apps/config` reference with the daemon `PUT /v1/config` shape from ADR 0001's pivot.
- **`docs/v2-design/milestones.md`** — Milestone 1 → ✅ SHIPPED, Milestone 1b → ✅ SHIPPED, Milestone 1c → ✅ SHIPPED. Milestone 2 rewritten per ADR 0001 (browser SPA, not Wails clients); original Wails-era scope preserved as historical record.
- **`docs/v2-design/structure.md`** — decision #2, decision #3, decision #7 banner-noted as superseded by ADR 0001. "Migration from main's current state to milestone 1" marked as COMPLETED (the restructure ran in session-8 cluster). "Target layout for milestone 2 (client apps)" replaced with current shape (frontend/logging SPA + cmd/smd + parked Gio + future internal/bridge); original Gio-era layout preserved as record. `internal/smclient` documented as never-created (not needed once SPA replaced Wails plan).
- **`docs/v2-design/ui-toolkit.md`** — top status line updated: "Resolved 2026-04-30 in ADR 0001"; toolkit progression history laid out. ADR cross-reference added.
- **`docs/v2-design/bridge.md` §6 "YAGNI question"** — banner-noted as Resolved 2026-05-02 by ADR 0013 (build the bridge as a daemon subsystem; default single-binary, split-host opt-in). "Decision pending" line replaced with the actual decision.
- **`docs/v2-design/cat-serial-reuse.md`** — banner-noted that ADR 0001 (SPA, not Gio) and ADR 0013 (bridge as daemon subsystem) shifted the *consumer* of `internal/serial` and `internal/cat` from a Gio app to the daemon's bridge subsystem; carve-out questions unchanged.
- **`docs/v2-design/forwarding.md` §5** — `next_attempt_at` column noted as shipped (with line ref into `0001_init.up.sql`); "pre-milestone-1c, schema is not yet frozen" replaced with the live-schema reference.
- **`docs/v1-analysis/lessons-for-v2.md` "New in v2" / "Changed substantially"** — flipped status markers (✅ shipped / 🚧 in progress / ⏳ deferred). `internal/smclient` documented as never-created.
- **`CLAUDE.md`** — opening "Repository structure" rewritten: main IS the milestone-1 layout (no pending restructure), UI-toolkit progression Wails → Gio → SPA spelled out, ADR 0013 bridge subsystem flagged as the only outstanding *structural* addition, `/v1/` URL prefix clarified as API versioning unrelated to v1/v2 project distinction. "Three Wails apps" example updated to "three client apps (now Svelte SPAs per ADR 0001)".
- **Memory `MEMORY.md` index lines** — `project_sm_restructure` description updated to reflect that the restructure has run; `project_sm_spa_config_layering` description updated to the five-tier model.
- **Memory `feedback_keep_docs_current.md`** — new memory file added: when the operator says "document," they mean every doc/ADR/memory/CLAUDE.md that touches the changed area; full audit pass, not just the obvious one. Triggered by the CLAUDE.md staleness incident this session.

**Daemon code review (mid-session, before SPA wiring):**

A `general-purpose` agent did a thorough pass over `cmd/smd`, `internal/api`, `internal/qsoservice`, `internal/forwarding`, `internal/database/sqlite`, `internal/events`, `internal/safego`, `internal/iocdi`, `internal/config`. Verdict: yellow — core invariants honored (atomic QSO+upload-queue, dedupe race-resolution, narrow daemon scope), but four issues needed fixing before the SPA wires against it:

- **C1 — `qso_upload` UNIQUE collision on second PATCH/DELETE.** The `UNIQUE (qso_id, forwarder_name, action)` constraint had no partial-status predicate, so a second PATCH triggered a constraint violation (raw 500). Fixed by switching `InsertQsoUploadTx` (`internal/database/sqlite/api_context.go:1684`) from `Insert` to a raw-SQL UPSERT with `ON CONFLICT DO UPDATE` that re-arms the row to status='pending' with cleared retry state. `upstream_id` is preserved across re-arm — `FetchInsertUpstreamIDWithContext` reads it back for the QRZ delete-after-insert flow. The previously-stale `TestFetchInsertUpstreamID_UniqueConstraintPreventsDuplicates` test was rewritten as `TestInsertQsoUploadTx_ReArmOnConflict` to pin the new semantics. New API tests `TestUpdate_TwicePatchesRearm` and `TestDelete_TwiceIsRejectedAt404` cover the second-PATCH and second-DELETE paths.

- **C2 — WaitGroup underflow during forwarder panic-respawn.** `cmd/smd/main.go`'s `wg.Add(1)` outside the closure + `wg.Add(1)` inside the closure on the second invocation pattern had a 5-second underflow window during which `wg.Wait()` could unblock prematurely. Fixed by adding `safego.GoTracked(ctx, name, onPanic, fn, respawn, wg)` which owns the WG lifecycle: `Add(1)` at the call site, `Done()` once when the goroutine permanently exits. Respawns now stay inside the same goroutine (loop-based, not recursive self-call), so the WG count never drops to zero between attempts. `cmd/smd/main.go:347-372` simplified accordingly. New tests in `internal/safego/safego_test.go`: `TestGoTracked_WaitGroup_NoUnderflowDuringRespawn`, `TestGoTracked_WaitGroup_DonePromptlyOnNormalReturn`, `TestGoTracked_WaitGroup_DoneOnCtxCancelDuringCooldown`. The `Go` (untracked) form is preserved with the same loop-based semantics for short-lived callers.

- **H3 — SSE shutdown sequence wedged for the full graceful timeout.** `r.Context().Done()` doesn't fire on `http.Server.Shutdown` — only on connection close — so an idle SSE subscriber kept Shutdown blocked until ctx expired and the listener force-closed. Fixed by adding `Server.shutdownCh chan struct{}`, closed by `Shutdown()` before `httpServer.Shutdown(ctx)`. The SSE handler's select now also watches `<-s.shutdownCh` for prompt exit. Test: `TestHandleEvents_ShutdownChClosureEndsStream`.

- **H1 — 5xx paths leaked `err.Error()` to clients without logging.** Every `s.writeError(w, 500, "db_error", err.Error(), op)` site (15 across `handler_qso.go`, `handler_logbook.go`, `handler_uploads.go`, `handler_contact_history.go`, `handler_qso_list.go`, `handler_contest_dupe.go`) returned raw internal text and never logged. Added `s.writeServerError(w, op, err, code, clientMsg)` in `internal/api/response.go` which logs the wrapped error via `s.logger.ErrorWith().Err(err).Str("op", ...)` and emits a deliberately generic envelope. All 15 sites converted. Same defence-in-depth as `recoverPanic`'s generic-message rule.

- **H2 — `qsoservice.Update` didn't translate UNIQUE-constraint races into `duplicate_key`** the way Submit did (`submit.go:202`). A concurrent submit+edit producing the same dedupe key after the pre-check returned "no collision" would bubble as a generic 500. Fixed by mirroring the constraint-violation race-resolution branch from Submit: if `UpdateQsoTx` fails and `isUniqueConstraintError(err)` returns true, return `&SubmitError{Code: "duplicate_key"}` so the handler maps to 409 the same way the pre-check path does (`update.go:194-216`). Pre-check test `TestUpdateQso_DuplicateKeyConflict` covers the handler→409 mapping; the constraint-violation path is genuinely race-only and not deterministically testable without fault injection.

- **H4 — `force=true` strict-equality parsing.** `force := r.URL.Query().Get("force") == "true"` silently accepted `?force=1`/`True`/`TRUE` as **false**, exactly the typo failure mode that loses contest QSOs. Fixed by switching to `strconv.ParseBool` with explicit rejection of unrecognised values (400 `invalid_query_param`). Empty/missing param keeps the dedupe-checked default. New tests `TestSubmitQso_ForceParamAcceptsBoolVariants` (true/True/TRUE/1 all bypass dedupe) and `TestSubmitQso_ForceParamRejectsGarbage` (yes/y/force/tru/2 all 400).

**Findings deferred (medium / low priority):**

The review surfaced ~10 additional findings that are deliberately left for follow-up: `cmd/smd/main.go` should use `errors.Op` instead of `fmt.Errorf` (M1); Submit's race-resolution refetch has no retry on ctx-deadline (M2); `handler_logbook.go` uses `strings.Contains(err.Error(), "UNIQUE constraint")` for 409-detection — should be promoted to a typed sentinel (M3); QRZ `io.ReadAll(resp.Body)` is unbounded (M6). None of these block SPA wiring. They're listed in the review report and will land as a cleanup pass.

**Medium-priority cleanup pass landed (mid-session):**

- **M1 — `errors.Op` in `cmd/smd/main.go`.** All 15 `fmt.Errorf` sites converted to `errors.New(op).WithErr(err).WithMsg(...)` / `WithMsgf(...)`. New op constants: `smd.run`, `smd.spawnForwarderWorkers`. `fmt.Fprintf` to stderr in pre-logger / post-logger panic paths preserved (those genuinely need stderr).
- **M2 — Submit race-resolution refetch detached from request ctx.** `submit.go:213` now uses `context.WithTimeout(context.Background(), 2*time.Second)` for the post-constraint-violation `FetchQsoByDedupeKey` lookup. The duplicate row is committed in sqlite by this point, so the lookup is bounded and pure-read; inheriting the request ctx would let a deadline expiry turn a known-duplicate into a generic 500.
- **M3 — typed sentinels replace string-match in `handler_logbook.go`.** New sentinels `errors.ErrDuplicateName` and `errors.ErrLogbookHasQsos` in `internal/errors/sentinels.go`. `internal/database/sqlite/api_context.go` translates UNIQUE-constraint violations on logbook insert/update into `ErrDuplicateName`, and the "has QSOs" guard returns `ErrLogbookHasQsos` directly. `handler_logbook.go` now uses `stderr.Is(err, sentinel)` for the 409 mapping. New `isUniqueConstraintError` helper in `internal/database/sqlite/internal.go` (mirrors the one in `internal/qsoservice/submit.go`; promoting one canonical copy is deferred until a third caller appears).
- **M4 — pre-tx dedupe collision check race window documented as backstopped by H2.** `update.go:169-175` keeps the pre-check as a fast-path optimization; the comment now explicitly names H2's UNIQUE-constraint translation as the safety net for the cross-handler race that opens between the pre-check and the actual UPDATE. No behavioural change; the comment closes the review's concern.
- **M5 — multi-record ADIF body rejected.** `handleSubmitQso` now returns `400 too_many_records` if the body parses to more than one QSO record. POST `/v1/qso` is single-record by contract; bulk imports use `cmd/importer`. Bounds the parser-allocation cost flagged in M5.
- **M6 — QRZ response body capped.** `internal/forwarding/qrz/qrz.go:216` now reads via `io.LimitReader(resp.Body, maxResponseBytes)` with `maxResponseBytes = 1 << 20` (1 MiB — generous; real QRZ responses are well under 1 KiB). Bounds the worker's memory if a misbehaving upstream returns a giant body.
- **M7 — structured warnings for unreachable forwarder paths.** Three "should be unreachable" sites in `internal/forwarding/worker/worker.go` (`unknown action`, `unreachable action`, `unknown outcome`) now emit `w.logger.WarnWith()...Msg(...)` alongside the existing `markFailed` `last_error` text. Operators see the unreachable-becoming-reachable signal in logs without grepping `last_error` strings.
- **M8 — dead commented-out code removed** from `internal/database/sqlite/service.go:154,162` (the obsolete `// return errors.New(op).WithMsg(errMsgNotOpen)` lines).
- **Low-priority — empty Content-Type acceptance documented** in `handleSubmitQso` (`api.md §3` already says it's accepted; comment now names the curl-without-headers operator scenario).

**Findings genuinely left as-is for now:**

- M9 — additional test coverage for race-only paths (race-only by construction; would need fault injection to test deterministically).
- Test gap on PATCH/DELETE-twice (covered as part of C1 fix).
- `safego` recursive self-call obscuring stack traces — already addressed as part of C2 (`runWithRespawn` is now a loop).
- `internal/iocdi` uses `fmt.Errorf` throughout — accepted carry-forward per the original review.
- `internal/forwarding/stub/stub.go` `fmt.Errorf` — stub-only test code, acceptable.
- `internal/database/sqlite/api.go` context-less wrappers — v1 carry-forward, dead daemon-side; cleanup deferred until v1 logging app fully retires.

**Documentation pass after the medium-priority fixes:**

A second drift audit ran after the medium fixes landed; eight findings, all addressed:

- **`docs/v2-design/api.md` §4.2** — `?force` query-param parsing now spells out the `strconv.ParseBool` accepted set and `400 invalid_query_param` rejection of unknown values.
- **`docs/v2-design/api.md` §4.2** — added a "Race-resolution refetch" paragraph documenting the M2 detached-context (2-second `context.WithTimeout(context.Background(), …)`) for the post-constraint-violation duplicate lookup.
- **`docs/v2-design/api.md` §4.5** — "Daemon shutdown" bullet rewritten: `r.Context()` doesn't fire on `Shutdown`; `Server.shutdownCh` is the explicit signal the SSE handler watches; final `hub.Close()` after publishers stop.
- **`docs/v2-design/api.md` §7a** — added a "Shipped error codes" table listing every wire `code` value the daemon emits today (4xx, 5xx, middleware), with HTTP status and trigger condition for each. Replaces the previous "see §4.6 envelope" hand-wave.
- **`docs/v2-design/forwarding.md` §"Re-queue semantics"** — added implementation note pointing to the `InsertQsoUploadTx` UPSERT and explaining `upstream_id` preservation across re-arm (load-bearing for QRZ delete-after-insert).
- **`docs/v2-design/forwarding-implementation.md` §4.8** — `safego` shape block now shows both `Go` and `GoTracked` with their respective lifecycle contracts; `cmd/smd/main.go` example uses `GoTracked`. Loop-based respawn called out.
- **`docs/v2-design/forwarding-implementation.md` §9.10** — `uq_qso_forwarder_action` constraint section rewritten: bare-INSERT was the C1 bug; UPSERT is today's behaviour; `ON CONFLICT DO NOTHING` is rejected with rationale.
- **`docs/v2-design/frontend-spa.md` (ADIF wire shape section)** — daemon endpoint contract spelled out (single-record, `?force` parsing, response codes); error-codes-the-SPA-must-surface list added with mapping suggestions for each, plus the H1 generic-message rule.

**Status: daemon is now ready for SPA wiring.** All tests pass under `-race`, the four recommended fixes are landed, and the wire contract documented in `api.md §7a` is unchanged. Next session: replace `console.log(adif)` in `frontend/logging/src/lib/ui/panels/QsoPanel.svelte`'s `submitQso()` with `fetch('/v1/qso?logbook=<id>', ...)` per the carried Step 1.

**What did NOT change:**

- No daemon code touched; no SPA code touched; no config schema changes; no migrations.
- The "Next steps" carried in from session 28 are still valid; what changed is that step 1 is "wire the SPA to the existing endpoint" rather than "build the endpoint."
- Memory files unchanged (the project memory's overview already says daemon work has shipped).

**Wire contract the SPA can target today (summary, full detail in api.md §7a):**

- `POST /v1/qso?logbook=<id>[&force=true]` with ADIF body, `Content-Type: application/x-adif` or `text/plain`. Returns `{status:"stored",id}` (201) or `{status:"duplicate",id}` (200). Errors via the standard envelope.
- Default deployment: TCP listener on the same `host:port` as the SPA (single-origin), so the SPA fetch is just `'/v1/qso?logbook=...'` — no `daemonUrl` prefix needed when running embedded.
- Submit rate cap is 20 QPS / 40 burst; way above any single-operator logging cadence.

### Next steps (carried into session 33+)

**OPERATOR field — contest-mode impact (deferred from session 32, 2026-05-04).** The `operator` field on `configState.loggingStation` was flipped to `$state('')` (reactive) at session-end specifically because it has implications for contest-mode logging that need to be designed before further work lands. Likely shape: in contest mode the *operator* of record may differ from the station owner / station_callsign across QSOs (multi-op contest stations), and the QSO submit path needs to read the current `operator` reactively so downstream logging picks up changes within a session without a page reload. **Don't extend the operator field's behaviour without first nailing down the contest-mode design** — the reactive declaration is a placeholder until that design is made.

In rough order of dependency:

**Landed in session 31 (struck off; kept as record):**

- ~~**Seed a default logbook on first-run DB init.**~~ Folded into `/v1/config` PUT — setup-transition (`SetupComplete` false→true) inserts a logbook row at id=1 using the operator's just-set callsign. Idempotent.
- ~~**Daemon `GET/PUT /v1/config`.**~~ Shipped with embedded-`types.X` projection (no parallel structs), source-of-truth split between config.json (scalar IDs) and DB (joined details), server-managed `setup_complete`, atomic file rewrite via `cfgSvc.Update()`.
- ~~**SPA setup form / first-run dialog.**~~ `lib/api/config.ts` + extended `configState` + `app.svelte` boot gate + operator-scaffolded setup card render `setup_complete=false` until callsign saved.
- ~~**First-run `config.json` write.**~~ Daemon seeds a default file on first launch; emits structured `first run: wrote default config to disk` line in `smd.log`.
- ~~**Defaults overhaul.**~~ TCP+ServeSPA on; `with_timestamp`/`file_logging`/`log_file_compress` true via `DefaultConfig` (preserves operator-explicit false on Load); db/log subdirs match `build/{db,log}/.gitkeep` convention.
- ~~**`types.RigConfig.ID` → int64.**~~ §7.5 of cat-serial-reuse.md closed.
- ~~**InfoPanel data-driven refactor + ARIA tablist.**~~ Cards/panels convention A locked.
- ~~**CLAUDE.md "Reuse types.X" idiom.**~~ Promoted from memory to project rules.

**Landed in session 33 (struck off; kept as record):**

- ~~**`qsoDraft` state-module lift.**~~ Shipped — `lib/states/qsoDraft.svelte.ts` is the singleton; `QsoPanel.svelte` consumes it. Trigger condition relaxed from "second consumer appears" to "fields are growing and refactor cost is rising" — operator's call.

**Session 34 directive (operator-set at end of session 33):** migrate the `station` store fields AND implement the ADIF MY_* fields end-to-end — some configurable / viewable from the My Station panel. Items 1+2 below are the session 34 scope; the rest are downstream.

1. **Migrate `lib/stores/station.ts` MY_* fields into `configState.loggingStation`** + **implement the ADIF MY_* field set.** Surfaced session 33 when `STATION_CALLSIGN` validation caught the mismatch. The remaining identity fields (`gridSquare`, `name`, `rig`, `antenna`) still ship via the legacy store with hardcoded defaults; submit reads them via `get(station)`. **Must precede any new daemon-side validator on these fields** — same trap recurs otherwise.

   **Scope for session 34 — work the ADIF MY_* set as a unit, not just the four current fields:**
   - **Audit ADIF MY_* fields and decide configurable vs. viewable vs. omit** for SM's personal-station shape. ADIF defines (non-exhaustive): `MY_GRIDSQUARE`, `MY_NAME`, `MY_RIG`, `MY_ANTENNA`, `MY_CITY`, `MY_COUNTRY`, `MY_CQ_ZONE`, `MY_ITU_ZONE`, `MY_DXCC`, `MY_STATE`, `MY_COUNTY`, `MY_LAT`, `MY_LON`, `MY_POSTAL_CODE`, `MY_STREET`, `MY_USACA_COUNTIES`, `MY_VUCC_GRIDS`, `MY_IOTA`, `MY_IOTA_ISLAND_ID`, `MY_SOTA_REF`, `MY_POTA_REF`, `MY_WWFF_REF`, `MY_FISTS`, `MY_ALTITUDE`, `MY_ARRL_SECT`. Some are configurable (operator-set in My Station: rig, antenna, name, gridsquare, country, etc.), some are *derived* from gridsquare (cq_zone, itu_zone, dxcc — likely candidates for daemon-side computation rather than operator entry), some are awards/activations (POTA/SOTA/IOTA — viewable + per-QSO override candidates), some are probably not worth surfacing for a single-operator personal station (street/postal_code/usaca_counties).
   - **Decide derivation strategy** — gridsquare is the load-bearing input; cq_zone / itu_zone / dxcc / continent should derive from it. Decide whether the SPA does the derivation (lib/utils) or the daemon does it on PUT (probably daemon — keeps the SPA dumb).
   - **Extend `/v1/config` schema** to surface the agreed configurable fields; extend `types.LoggingStation` to match.
   - **Add the new fields to `configState.loggingStation`** as plain class fields per the reactivity audit — no `$derived` consumers, daemon-hydrated, edited via form bindings.
   - **Wire MyStationPanel** to write configurable fields through `ValidatedInput`. `gridSquare` needs a Maidenhead validator (new pure module under `lib/validators/`); free-text fields use the existing patterns.
   - **Switch `submitQso`** to source ALL identity fields from `configState.loggingStation`; the `formatAdifRecord` MY_* mapping pulls from one place.
   - **Delete `lib/stores/station.ts`** and update its three remaining consumers (search via grep; should be QsoPanel + the previous MyStationPanel doc reference).
   - **Update `formatAdifRecord` + ADIF tests** for any newly-emitted MY_* fields.
   - **Doc + memory pass:** update `frontend-spa.md` (state-layer table loses the `station` store; QSO-submit footnote loses the split-source caveat); `milestones.md` (strikethrough on the migration line); `project_sm_station_store_migration.md` (mark as completed and explain what shipped); resolve the open-question entry in `frontend-spa.md`.
   - **Daemon-side:** if any MY_* field gains a validator (e.g. gridsquare format check, country-name normalization), add tests covering the round-trip first.

   See `project_sm_station_store_migration.md` memory for the full trap analysis that motivated the migration.
2. **Operator's scaffolded `WorkedPanel` / `DetailsPanel` / `SessionPanel` content.** (MyStationPanel landed session 32.) InfoPanel chassis is in place; the remaining three files exist as scaffolds. SessionPanel can read `SessionTimer` state. Worked / Details depend on QSO data and contact-history.
3. **Daemon `GET /v1/enrich/callsign`** (Go) — per ADR 0005. Unlocks the F2 lookup-only path. SPA-side `qsoDraft.populateFromEnrichment(...)` method lands alongside the wrapper.
4. **`internal/bridge` package** (Go) — per ADR 0013. `/v1/rig/events` SSE, rigctld-compat TCP, AUTO-mode CAT, current-state cache, PTT arbitration.
5. **Real EventSource consumer in `bridge.svelte.ts`** — populate catState from SSE; snapshot-on-CAT-off effect.
6. **CAT-handover toast** — toast plumbing exists; awaits the bridge so there's a transition to fire on.
7. **Keyboard shortcuts** (ADR 0007 + `@svelte-put/shortcut`) — F2 lookup-only, Ctrl+\ VFO swap, Ctrl+Enter submit, ? help overlay.
8. **Inline validation message slot (Fix 13).**

**Lower-priority follow-ups noted but not blocking:**

- **`*bool` migration for tri-state booleans.** Currently `WithTimestamp`, `FileLogging`, `LogFileCompress` are set in `DefaultConfig` only (not in `applyDefaults`) so operator-explicit `false` survives Load. Future migration to `*bool` (mirrors `Server.ServeSPA`) would let `applyDefaults` distinguish "absent from config" from "explicitly false" and apply defaults uniformly without the split between DefaultConfig and applyDefaults. Regression-guarded by `TestLoad_OperatorFalsePreserved`.
- **Logbook callsign coupling.** When the operator changes their `station_callsign` in My Station, the default logbook's `callsign` (seeded from it during setup) doesn't auto-update. Two stores diverge unless we propagate. Decide when MyStationPanel edit ships: (i) PUT `/v1/config` propagates to logbook id=1; (ii) operator edits logbook callsign separately via `/v1/logbook/:id`; (iii) make logbook callsign a derived projection of station_callsign for the default logbook only.
- **`POST /v1/log/client` endpoint.** SPA-side persistent logging deferred. Land this only if SPA-side log persistence becomes wanted.

---

## Session 28 archive (was: SPA-side QSO logging path landed end-to-end via console.log; ADIF wire shape settled with MY_* operator-station fields; QSO timer refactored to paired-ticking model; SessionTimer with sessionStorage persistence; Station store with three-callsigns-aware naming; design tokens (spacing + colour) complete; tab-order indexing wired; 268 tests pass)

### Session 28 work (SPA QSO submit pipeline end-to-end; new field components; QSO + session timer model; design-token palette; station store; tab order)

**Operator brief at session start:** continue from session 27. The next set of asks shaped the session arc — code review fixes, then add field components, wire defaults, design tokens, QSO submit + clear, session timer, station refactor, tab order. No daemon work this session; everything lands on the SPA side and stops at console.log.

**Conversation arc (in order):**

1. Code review of `frontend/logging`. Found: Mode-dropdown reactivity bug (binding never reached `manualState.mode` so the mode-dependent RST default never recomputed); Callsign Tab handler firing on Shift+Tab; VfoBox top-box affordance lying about its behaviour; missing test coverage for Callsign / ValidatedInput / displayedState. Fixed all and added 57 tests across the three new test files. (Done in session 27.)
2. New field components landed: `TextInput`, `Comment`, `DateInput`, `TimeInput`, `FormControls`, `SessionTimer`. Operator added to QsoPanel layout; review pass tightened the styling consistency, added `disabled` props where missing, dropped doubled date/time icons in favour of native browser pickers tinted with `accent-focus`.
3. Default values wired in QsoPanel: UTC date / time-on / time-off snapshotted at panel mount; mode-dependent RST defaults (`'59'` voice / `'599'` CW) with re-fill on empty + mode change.
4. ADIF formatter settled. New `lib/utils/adif.ts` produces ADIF wire format: `<CALL:5>...` / `<EOR>` records with required fields always, optional fields omit-if-empty, fixed emission order (canonical complete-record byte-identity test pins this).
5. `clearDraft()` and `submitQso()` wired in QsoPanel; `canSubmit` `$derived` gates Log Contact; FormControls receives `onClear` / `onSubmit` / `submitDisabled` callbacks. Console.log of the ADIF record after submit; clearDraft runs after submit so the next QSO starts fresh.
6. QSO timer model refactor — settled the paired-ticking design (replaced earlier "ticker only after Tab"). One always-running `setInterval(60_000)`; `qsoStarted` boolean picks the branch. Pre-QSO: tick paired-updates qsoDate/timeOn/timeOff. Active QSO: tick only updates timeOff (qsoDate and timeOn pin at Tab moment). Decision to drop Start/Stop button affordances given lookup-only F2 path covers the DX-pile-up case.
7. SessionTimer landed. New `lib/ui/components/SessionTimer.svelte` — 1Hz tick, `formatDurationHms(ms)` (added to `lib/utils/time.ts`) renders HH:MM:SS. **sessionStorage persistence** under key `sm.session.startedAt` — survives page reload (especially F5 collisions with planned F-key shortcuts), resets on tab close. New persistence tier alongside daemon (config) and localStorage (manualState).
8. Operator → Station refactor. Renamed `lib/states/operator.ts` → `lib/stores/station.ts`. Justification: ADIF spec uses STATION_CALLSIGN for "logging station's callsign (the callsign used over the air)", distinct from OPERATOR (the human at the controls) and OWNER_CALLSIGN (the license holder of the station). v1 only models STATION_CALLSIGN; the other two are deferred. Field rename: `callsign` → `stationCallsign`. Class name: `Operator` → `Station`. Directory move: `lib/states/` (runes) vs `lib/stores/` (Svelte stores) — paradigm split.
9. Reactivity-boundary clarification: `$state` is for fields that drive `$derived` computations; Svelte stores for static-ish profile data (no `$derived` dependents, populated once at app start). `configState.station.enabled` and `.ampMultiplier` keep `$state` (they drive `displayedState.isLive` and `.effectivePower`); the new station store fields are plain Svelte writable.
10. ADIF MY_* fields wired. `formatAdifRecord` extended with `stationCallsign`, `myGridSquare`, `myName`, `myRig`, `myAntenna` — all optional, omit-if-empty, emitted in fixed order after the contact-info block. `submitQso` reads `get(station)` once and threads the values through. Operator populated their actual defaults in `stores/station.ts` (the v1 "edit-the-file" pattern until the daemon `/v1/config` endpoint lands).
11. Tab-order indexing. `Mode` always `tabindex={-1}`; `VfoBox` always `tabindex={-1}` (Tab skips the box, mouse-click + future Ctrl+\ shortcut handle the swap); `VfoInput` gains `tabindex` prop with default 0 — Vfos passes 0 for the top input, conditional on split for the bottom; `DateInput`/`TimeInput` always `tabindex={-1}`; `FormControls` Clear button `tabindex={-1}`. Resulting tab order: Callsign → RST Sent → RST Rcvd → top VFO → [bottom VFO if split] → Name → QTH → Comment → Log Contact (with VFO inputs naturally absent when CAT is live since they're disabled).

**Cross-cutting work:**

- Design tokens added to `app.css` `@theme`. **Spacing tokens:** `--spacing-vfo-w`, `--spacing-vfo-half`, `--spacing-vfo-full` (with the `full = 2 × half` invariant), `--spacing-input-slot`. **Colour tokens (semantic palette):** `--color-surface`, `--color-surface-muted`, `--color-surface-disabled`, `--color-line`, `--color-line-soft`, `--color-ink`, `--color-focus`, `--color-focus-ring`, `--color-invalid`, `--color-vfo-rx`, `--color-vfo-tx`, `--color-vfo-label`, `--color-vfo-inactive`. All components migrated. Sets up future light/dark-mode swap as a one-block change.
- Layout positioning rule (Rule 5 in component-patterns memory): parent owns external vertical rhythm via its own `py-*` / `gap-*`; children own internal layout (e.g. Vfos's `pt-2` for label-compensation stays). `.input-row` no longer carries `my-4`; QsoPanel's column owns `pt-4` / `mt-2` between rows. Same visual outcome, cleaner architecture.
- Utils growing: `lib/utils/time.ts` (`formatUtcDate`, `formatUtcTime`, `formatDurationHms`), `lib/utils/adif.ts` (`formatAdifRecord` + `AdifQsoFields`), `lib/utils/frequency.ts` (`frequencyToBand`, lifted from Vfos when QsoPanel became a second consumer). Each has a sibling `*.test.ts` pinning the spec.
- Composite `tsconfig.node.json` — added `composite: true` to fix TS6306 from the project-references setup.
- Favicon 404 — added `<link rel="icon" href="data:," />` in `index.html` to suppress the browser's default `/favicon.ico` request. 0-byte placeholder kept in `public/` for future real icon.
- "Drop $state when not needed" preference settled. Operator pushed back on reflexive use of `$state` for static config — reactive subscriptions cost something, and config fields without `$derived` consumers don't earn it. Established the `$state`-vs-store split. Memory updated.

**Test coverage growth this session:** 168 → 268 tests across 14 files. New: Mode (7), Callsign (19), ValidatedInput (16), displayed (22), FormControls (12), time utils +10 (`formatDurationHms`), frequency utils (12), adif (33). Plus updates to existing Vfos.test.ts for affordance + tab-order changes.

**Documentation updated this session:** `docs/v2-design/frontend-spa.md` (CSS conventions section, layout positioning section); memory `project_sm_spa_component_patterns` extended from 4 → 7 rules; memory `project_sm_spa_config_layering` reflects sessionStorage tier; this handoff doc gets the session-28 entry.

**No ADR-level decisions.** All session 28 changes fit under existing ADRs (0001 toolkit, 0009 four-object decomposition, 0011 manualState persistence). Component-pattern memory is the canonical place for these conventions.

**Status: SPA-side logging is end-to-end usable.** Type a callsign → Tab triggers QSO start (timer + enrichment hook) → fill remaining fields with the correct tab order → Log Contact emits a complete ADIF record to the dev-tools console → form clears for the next contact. The remaining work is daemon-side: `POST /v1/qso` (turn console.log into persistence), `GET/PUT /v1/config` (replace edit-the-file station defaults), `GET /v1/enrich/callsign` (unlock F2 lookup-only path), and the bridge subsystem (`internal/bridge` per ADR 0013).

### Next steps (carried into session 29+)

In rough order of dependency:

1. **Daemon `POST /v1/qso`** (Go) — accept ADIF body, write QSO + upload-queue rows atomically per `invariants.md` "One-fails-all-fail." Exercises the daemon HTTP scaffold.
2. **Daemon `GET/PUT /v1/config`** (Go) — replaces the v1 edit-the-file workflow for station/operator config.
3. **Daemon `GET /v1/enrich/callsign`** (Go) — per ADR 0005. Unlocks the F2 lookup-only path.
4. **`internal/bridge` package** (Go) — per ADR 0013. `/v1/rig/events` SSE, rigctld-compat TCP, AUTO-mode CAT, current-state cache, PTT arbitration.
5. **Real EventSource consumer in `bridge.svelte.ts`** — populate catState from SSE; snapshot-on-CAT-off effect.
6. **CAT-handover toast** — depends on toast system per ADR 0008.
7. **"My Station" header card** — UI display of the station store (callsign, grid, rig). Subscribes to `station` store via `$station` template syntax.
8. **`qsoDraft` state-module lift** — promote QsoPanel local `$state` into `lib/states/qsoDraft.svelte.ts` singleton when a second consumer (recent-QSOs panel, CountryPanel reading current callsign) appears.
9. **Toast system** (ADR 0008) — submit feedback, CAT handover, bridge errors.
10. **Keyboard shortcuts** (ADR 0007 + `@svelte-put/shortcut`) — F2 lookup-only, Ctrl+\ VFO swap, Ctrl+Enter submit, ? help overlay.
11. **Inline validation message slot (Fix 13)** — the `.input-row` `h-input-slot` reserves the height; the slot itself isn't built. Lands cleanly when QSO draft store + form composition exist.

---

## Session 27 archive (was: SPA code review: Mode-dropdown reactivity bug fixed; Callsign Shift+Tab guard; VfoBox top-box affordance suppressed; layout positioning refactored to parent-owns rule; design tokens introduced; test coverage extended Callsign/ValidatedInput/displayedState — 168 tests pass)

### Session 27 work (logging app code review + fixes)

**Operator brief at session start:** "Let's start with a code review of the logging app and pick up any issues before we move on to add more features."

**Review found three real bugs, several test gaps, one positioning-discipline issue, and one design-token opportunity. All are fixed.**

**Bugs fixed:**

- **Mode dropdown wasn't wired to manualState.** `QsoPanel.svelte` passed a hardcoded `value="USB"` to `<Mode>`; `Mode` declared `value = $bindable('')` but the parent didn't bind. Net effect: changing the mode in the UI didn't update `manualState.mode`, didn't reach `displayedState.mode`, didn't trigger the mode-dependent RST default (`'59'` voice / `'599'` CW) that landed end-of-session-25. Fix: introduce a local `mode` `$state` in `QsoPanel` that mirrors `displayedState.mode` (live source via `$effect`) and writes back to `manualState.mode` on operator edits when `displayedState.editable`. `<Mode>` is now `bind:value={mode}` with `disabled={!displayedState.editable}`. Same static-ownership pattern as the Vfos→manualState writes.
- **Mode `<option>` had a redundant `selected` attribute** that fights `bind:value` on the `<select>` in some browsers. Removed.
- **Callsign Tab handler fired `onenrich` on Shift+Tab** as well as Tab. Added `e.shiftKey` guard.
- **`Mode` component shape was ceremony.** `list: {key, value}[]` with both fields always identical. Collapsed to `list: string[]`.

**VfoBox affordance bug discovered during user testing:**

- The top (selected/RX) VfoBox had identical hover/cursor/focus-ring affordances as the bottom one, but its click closure wrote `manualState.selectedVfo = vfo` — same letter, no swap. Visual affordance lied about behaviour.
- Fix (Option A from the discussion): `VfoBox` gained an `isSelected` prop. When `isSelected || disabled`, `tabindex={-1}`, no title, click/keydown handlers bail. Cursor classes split three ways: `cursor-pointer` (interactive), `cursor-not-allowed` (CAT operating), `cursor-default` (already selected). Bottom (interactive) box gains `title="Select"` tooltip on hover. `manualState` now only writes on meaningful swaps — no more redundant localStorage mirror writes for top-box clicks.

**Layout positioning refactored — parent owns vertical rhythm (new Rule 5 in component-patterns memory):**

- Children no longer carry external vertical margins. The panel that places them owns the row's vertical rhythm via its own `py-*`.
- `.input-row` is `h-input-slot` only — no more `my-4`. The class says "I am this tall"; it does not say where it sits.
- `Vfos.svelte` dropped its `my-4`. Kept `pt-2` with a comment naming it as **internal** label-compensation (Vfos has no `<label>` of its own, so first content row sits 0.5rem above sibling inputs that do; `pt-2` brings it back onto the same baseline). Internal compensations stay inside the child; external positioning lives in the parent.
- `QsoPanel.svelte` gained `py-4` on its flex container.
- Visual outcome unchanged; architecture cleaner. A future panel just picks a different `py-*` without overriding every child's margin.

**Design tokens introduced — `@theme` block in `app.css` (new Rule 6):**

- Tokens for values that **anchor a relationship**: `--spacing-vfo-w`, `--spacing-vfo-half`, `--spacing-vfo-full` (with the `full = 2 × half` invariant documented), and `--spacing-input-slot`. Tailwind v4 auto-generates `w-vfo-w`, `h-vfo-half`, `h-vfo-full`, `h-input-slot` utility classes.
- `VfoBox.svelte` switched from `w-13 h-4.25 h-8.5` to `w-vfo-w h-vfo-half h-vfo-full`. `.input-row` switched from `h-17.5` to `h-input-slot`.
- One-off shell numbers (`h-13.5`/`w-72.5` in `LoggingCard`, `h-120`/`w-200` in `app.svelte`) stay as Tailwind arbitrary values **with comments** naming them as one-off and the threshold for promoting them to tokens.
- Top-of-file convention comment in `app.css` captures the rule so future additions follow the same shape.

**Test coverage extended:**

- `Mode.test.ts` (7 tests) — option count + label parity, value/label equality, initial value reflected, change-event binding, default-enabled, disabled prop, label rendering.
- `Callsign.test.ts` (19 tests) — rendering (label, uppercase class, initial value), validation (invalid styling on/off, empty-not-invalid), focus-trap on blur (refocus + select on invalid non-empty, no trap on empty/valid), and the full Tab→onenrich path: fires only on valid+non-empty, uppercase-normalizes, trims whitespace, ignores empty/whitespace/invalid input, ignores Shift+Tab, ignores Enter/Space/letter keys, tolerates absent callback.
- `ValidatedInput.test.ts` (16 tests) — rendering, validator wiring (invalid styling on/off, empty-not-invalid), focus-trap parity with Callsign, `inputClass` prop pass-through, base class preservation, HTML attribute spread (`maxlength`), validator-call observability on input + blur.
- `displayed.test.ts` (22 tests) — three-flag truth table for `isLive`/`editable` (8 cases covering every false/true combination), field source switching catState↔manualState, `rigIdentity` special case, `split` derivation (frequency-divergence in CAT-off, `splitOverride` in CAT-on, frequency-divergence ignored in CAT-on), `rawPower` source switching, `effectivePower` with no/2x/0.5x multiplier in both CAT-on and CAT-off modes.
- `Vfos.test.ts` updated for the new affordance suppression: top-box has `tabindex=-1`, no `title`, `cursor-default` class; bottom-box has `title="Select"`; no manualState write on no-op clicks; CAT-operating disables both boxes.

**Test totals:** 99 → 168 (+69 across five test files). All passing. `svelte-check` clean (182 files, 0 errors). Production build verified — Tailwind tokens compile correctly into utility classes.

**Documentation updated:**

- `docs/v2-design/frontend-spa.md` §"Global CSS conventions" — `@theme` bucket added, threshold convention refined for 2026-05-02. New §"Layout positioning — parent owns vertical rhythm".
- Memory `project_sm_spa_component_patterns` — extended from 4 rules to 6 (added Rule 5 parent-owns-positioning, Rule 6 design-tokens). Cross-references updated to point at the current file inventory.

**No ADR-level decisions this session — all changes are SPA implementation/style choices that fit under the existing SPA-related ADRs (0001 toolkit, 0009 four-object decomposition, 0011 manualState persistence).** Component-pattern memory is the canonical place for these conventions; ADR is reserved for choices with plausible alternatives that were genuinely weighed.

**Code review items deferred per "wait for feature work" policy:**

- `configState.daemonUrl` / `bridgeUrl` fields — wait for `/v1/config` daemon endpoint per ADR 0003.
- `bridgeState` SSE wiring — step 5 of the original execution plan.
- `catState.power` default-0 visual concern — wait for live SSE behaviour.
- QSO draft store — wait for form composition / submit pipeline.
- Inline validation message slot (Fix 13) — `h-input-slot` already reserves the space; lands when form composition arrives.
- Frequency helpers extraction (`formatFrequency`, `frequencyToBand`) — wait for second consumer (history view, QSO row).

### Next steps (carried into session 28+)

Same as session 26's carried list, plus the deferred review items above. The execution plan from session 25 (now with the topology revision):

- **Step 4 (revised per ADR 0013):** wire `/v1/rig/events` into the daemon binary as part of the `internal/bridge` package. Same wire shape (ADR 0010); host is the daemon by default.
- **Step 5:** real EventSource wiring in `bridge.svelte.ts`. URL composition reads `configState.bridgeUrl` (defaults equal to `daemonUrl` in single-binary deployment).
- **Step 6:** CAT-handover toast (depends on toast-system implementation per ADR 0008).

Other carried items: daemon `/v1/config` endpoint (ADR 0003), daemon `/v1/enrich/callsign` handler (ADR 0005), toast system (ADR 0008), keyboard shortcuts (ADR 0007), QSO draft store / form composition, first real route, CI workflow update with Node ≥22 + frontend test step, `internal/bridge` package as a real Go package, `bridge.enabled` config flag in daemon's config schema, driver-shaped forwarder layer, `Authorization: Bearer <token>` header threading.

---

## Session 26 archive (was: Current state as of 2026-05-02 — topology pivot: bridge collapsed into daemon as internal subsystem; ADRs 0012/0013/0014 + invariants/topology/memory propagation; no code changes — pure design)

### Session 26 work (topology redesign: daemon owns the bridge as an internal subsystem; upstream forwarding deferred; full ADR + doc + memory propagation)

**Operator brief at session start:** "We need to make a design change/decision." After I summarized the current design back, the operator identified the issue: the daemon was supposed to be network-deployable (Pi/NAS/VPS), but the SPA's path to `/v1/rig/events` had drifted to "daemon hosts it" — which couples the daemon to the rig host. Two-process / two-origin / CORS-as-default-config was the consequence.

**Conversation arc:**

1. ADR 0012 drafted earlier in the conversation to codify "daemon and bridge as separate origins; daemon never proxies bridge data." Captured the causal chain through ADR 0001 (browser SPA can't own serial → CAT must run somewhere else → and that somewhere has to be operator-local-to-the-rig, while the daemon must be network-deployable).
2. The operator stepped back and asked for my honest view of the design overall. I flagged: it's coherent and defensible, but heavy for a personal project — three processes, four state objects, two origins, twelve ADRs at the time. A lot is riding on ADR 0001. The bridge has the most novel work and the least proven value. The all-on-one case (the dominant deployment) pays for split-deployment flexibility.
3. The operator proposed: default deployment = daemon owns bridge/CAT/serial/SPA/DB on local PC. Cluster-mode (network-deployed daemon for forwarding, locals forward to master) deferred but kept easy to add. Asked for pushback.
4. I pushed back on the cluster-readiness instinct: it's at risk of design-by-anticipation. Reframed as "four prep-work items justified by v1 scope today" + "explicit foreclosure list for speculative work."
5. Operator agreed; also flagged a real win for browser SPA missed earlier: native zoom for accessibility (Gio/Wails ship fixed sizes; browser ships `Ctrl-+` reflowable layout from day one).
6. Drafted everything.

**ADRs landed:**

- **ADR 0001** — added "free, native operator-controlled zoom and accessibility" to "Gained" section. Reworded the SPA→bridge consequence chain from "must be a separate process" to "must run somewhere other than the browser" — the daemon can be that somewhere.
- **ADR 0010** — second-revision note documenting two host changes the same day (ambiguous → bridge process → daemon-with-bridge-subsystem). Endpoint section distinguishes default (daemon serves it) vs split-host (standalone bridge serves it). CORS clause clarifies "no CORS in default deployment."
- **ADR 0012** — superseded same day. Forward-pointing note explains why "two processes, two origins" was rejected and what 0013 changed. Body preserved as the reasoning trail.
- **ADR 0013 (new)** — daemon owns the bridge as an internal subsystem; single binary by default; `bridge.enabled` config flag for the network-deployed-daemon case; split-host preserved as opt-in via separately-buildable `cmd/bridge`. Static-ownership lowered from process boundary to package-import graph (`internal/storage` and `internal/forwarder` must not import `internal/bridge`).
- **ADR 0014 (new)** — upstream forwarding (federation) deferred. Four prep-work items justified by v1 scope (driver-shaped forwarders, `Authorization` header threaded through every fetch from day one, namespaced subsystem `enabled` flag pattern, `additional_data` provenance metadata). Explicit foreclosure list: master discovery, cluster config schema, federation routing, multi-daemon UI, master-daemon-specific code paths.

**Doc / invariant propagation:**

- `docs/v1-analysis/invariants.md` "Daemon scope is explicitly narrow" — restated to package-boundary phrasing with a 2026-05-02 revision note pointing at ADR 0013. Protection unchanged; enforcement mechanism lowered.
- `docs/v2-design/topology.md` — substantially rewritten. Default topology is single-binary single-origin; ASCII diagram updated; "Service responsibilities" rewritten; "Alternative deployments" replaces the prior "deployment topologies this enables." Explicit foreclosure section for speculative cluster work.
- `docs/v2-design/frontend-spa.md` — top-of-file topology revision note. Open-questions resolved: bridge URL discovery → `bridgeUrl == daemonUrl` in default deployment per ADR 0013; CORS → none in default deployment; auth → `Authorization` header threaded from day one per ADR 0014. File structure note updated (`config.ts` comment).
- `docs/v2-design/bridge.md` — top-of-file revision note flagging which sections of the 2026-04-20 doc are superseded by ADR 0013 (deployment shape) and which were reversed by invariants.md (rigctld-compat TCP frontend stays canonical).
- `CLAUDE.md` — "Narrow daemon scope" headline rewritten to package-boundary phrasing, with ADR 0013 reference.

**Memory updates:**

- `project_sm_serial_bridge` — rewritten end-to-end. Bridge as daemon subsystem in default; split-host as opt-in; two-frontend (rigctld-compat TCP + SM-native SSE) reaffirmed; package-boundary discipline noted. Session-15 (2026-04-20) "drop rigctld TCP" decision marked as historical/reversed.
- `project_sm_spa_config_layering` — added "URL fields in configState" section: `daemonUrl` + `bridgeUrl`, default-equality in single-binary, multi-rig future generalization (`bridges[]`).
- `project_sm_daemon_vs_spa_split` — added bridge subsystem to the daemon-side responsibility list; ADR 0013 reference; package-boundary phrasing for narrow-daemon-scope.
- `project_sm_restructure` — topology refinement section added; daemon scope updated with bridge subsystem; repo-split stance updated (`internal/bridge` package + opt-in `cmd/bridge` binary).

**No code changes this session.** Pure design / documentation / memory work. Tests still pass from the end of session 25 (svelte-check clean; 99 tests across 6 files).

**Current ADR ledger:** 0001–0011 from session 25; 0012 added then superseded same day; 0013 + 0014 added and accepted. Ten accepted, one superseded. Numbering scheme working as intended (supersession via Status field + forward pointer + body preservation).

### Next steps (carried into session 27+)

The original SPA-side execution plan (steps 4/5/6 from session 25) needs adapting to the new topology:

- ~~**Step 4:** Standalone-bridge-process SSE endpoint per ADR 0010.~~ **Replaced by:** wire `/v1/rig/events` into the daemon binary as part of the `internal/bridge` package implementation (ADR 0013). Same wire shape (ADR 0010); different host.
- **Step 5:** Real EventSource wiring in `bridge.svelte.ts`. URL composition reads `configState.bridgeUrl`; in default deployment that equals `configState.daemonUrl`. Snapshot-on-CAT-off effect lands here.
- **Step 6:** CAT-handover toast (depends on toast-system implementation per ADR 0008).

Other pending threads carried from session 25:

- Daemon `/v1/config` endpoint per ADR 0003 (Go work).
- Daemon `/v1/enrich/callsign` handler per ADR 0005 (Go work).
- Toast system implementation per ADR 0008.
- Keyboard shortcuts implementation per ADR 0007.
- QSO draft store / form composition.
- Inline validation message slot (Fix 13).
- First real route (svelte-spa-router vs hand-rolled).
- CI workflow update with Node ≥22 + frontend test step.

New work surfaced this session, deferred per ADR 0014:

- `internal/bridge` package as a real Go package (when bridge work resumes).
- `bridge.enabled` config flag in the daemon's config schema.
- Driver-shaped forwarder layer (justified-today, not just for cluster-readiness).
- `Authorization: Bearer <token>` header threaded through SPA fetch wrapper from day one (justified-today for the eventual network-deployed daemon case).
- A future "package-import-graph lint" that flags `internal/storage` or `internal/forwarder` importing `internal/bridge`. Cheap to add when CI gets revisited.

---



### Session 25 work (CAT state + ADRs 0002–0011 + Vfos component + component-testing infra + CAT precedence + 4-object decomposition + keyboard shortcuts + toast system + rig SSE wire shape + manualState persistence + step 2 implementation + step 3 select-VFO UI)

**Operator brief at session start:** keep the SPA's architectural shape coherent — surface the load-bearing decisions before code cements them, then build the Vfos display.

**What landed:**

- **`frontend/logging/src/lib/states/cat.svelte.ts`** — first SPA state singleton. Module-level `class CatState` with eight fields: `enabled`, `rigIdentity`, `vfoA`/`vfoB` (Hz), `mode`, `subMode`, `selectedVfo`, `split`. All defaults defined as named module-level `const`s (`DEFAULT_VFO_HZ` = 14.250 MHz, `DEFAULT_MODE` = 'USB', etc.) per `no magic numbers` rule. JSDoc captures load-bearing distinctions: `enabled` (operator config) is separate from "rig is currently responding"; frequency in Hz not MHz (JS f64 has 53-bit integer precision, ADIF MHz string is a submit-boundary conversion); `mode`/`subMode` map to ADIF MODE/SUBMODE.

- **`lib/states/` directory convention** — settled 2026-05-01. Each `*.svelte.ts` file owns one slice of cross-component state. `cat.svelte.ts` first; `bridge.svelte.ts` (planned EventSource transport) and `qsoDraft.svelte.ts` (planned, when form composition lands) sit alongside. `frontend-spa.md` layout sketch + "Notes on the layout" updated to match.

- **`docs/decisions/0002-spa-config-shape.md`** — Status: **Superseded by 0003** (same day). Originally specified a three-layer daemon/localStorage/hardcoded config resolution with offline-write queue and last-write-wins sync. Body preserved as a record of how the more complex shape was considered and rejected.

- **`docs/decisions/0003-spa-config-daemon-only.md`** — Status: Accepted. Operator's correction to 0002: the SPA is hosted by the daemon (ADR 0001), so loading the SPA *requires* a successful daemon round-trip — there is no SPA-running-offline scenario to cache for. From the SPA's standpoint, the daemon is always reachable when the SPA is running. Two layers: daemon `/v1/config` + hardcoded module-level constants as bootstrap/first-install fallback. No localStorage. No offline write queue. The forthcoming "offline QSO storage" ADR flagged in 0002 is also no longer needed — same reasoning rules out SPA-side offline QSO log; daemon-side write resilience (atomic transaction, forwarder retry) stays daemon internals.

- **`docs/decisions/0004-daemon-vs-spa-responsibilities.md`** — Status: Accepted. The general rule that 0003 surfaced. **Daemon owns persistence, external-service orchestration, and shared cross-session state. SPA owns UI reactivity, presentation, and per-session UX.** Two-topology framing keeps it honest: the *persistence/authority* topology collapsed to daemon-only by ADR 0003, but the *runtime/events* topology still lives in the browser (DOM, keystrokes, SSE event dispatch — can't relocate). Includes a 13-row table assigning concrete features to sides. Three anti-patterns flagged in the memory entry: "daemon is local, so put orchestration in SPA" (wrong — credentials and shared cache belong daemon-side regardless of locality); "SPA is hosted by daemon, so render server-side" (wrong — that's SSR, explicitly rejected by 0001); "SPA orchestrates parallel fetches to hamnut/QRZ" (wrong per 0004 + 0005).

- **`docs/decisions/0005-enrichment-pipeline-shape.md`** — Status: Accepted. First concrete application of 0004 beyond config. **One daemon endpoint** (`GET /v1/enrich/callsign?call=X`), **aggregated JSON response** (no SSE streaming for v1), **cache-first orchestration** with concurrent hamnut+QRZ on miss. **Always returns 200** per the `enrichment never blocks logging` invariant — partial/null fields when only some sources succeed. **AbortController on the SPA** propagates to daemon request context for cancellation. **7-day cache TTL** default, operator-configurable via daemon config (per 0003). Reverses the prior `frontend-spa.md` framing of `lib/enrichment.svelte.ts` orchestrating concurrent fetches in the browser; the SPA module now becomes a thin (~30 line) fetch + abort-handle wrapper.

- **`Vfos.svelte` built and shipped** — sits alongside Callsign/Rst/Mode in `QsoPanel`. Reads `catState` directly. Visual stacking: the **selected VFO is rendered in the top "RX" position**, the other in the bottom "TX" position. `VfoBox` (small label badge) + `VfoInput` (formatted freq + band) per VFO. When `split === true`, both VfoBoxes show explicit RX/TX action labels above the VFO label. Frequency formatting in MHz.kHz.Hz convention (`14_250_000` → `"14.250.000"`).

- **`frequencyToBand(hz)` helper in `Vfos.svelte`** — module-local lookup table over the amateur-radio band allocations (160m through 23cm, ADIF BAND-field naming). Returns `''` for out-of-band frequencies (UI decides how to render absence). Inline per "build specific not generic"; extract to `lib/bands.ts` when a second consumer (QSO list, history view) appears. 60m widened to 5.25–5.45 MHz to cover regional variations; rest follow IARU allocations.

- **`Vfos.test.ts` — 10 component tests** — exercises the four state combinations (`selectedVfo: A|B` × `split: true|false`) plus a band-display sanity pair. Uses `@testing-library/svelte`'s `render` + DOM assertions on `container.textContent` for ordering (`indexOf(...) < indexOf(...)`) and `querySelectorAll('input')` for input values. `beforeEach` resets `catState` to known defaults so the module-level singleton doesn't bleed across cases; `afterEach(cleanup)` unmounts.

- **`@testing-library/svelte/vite` plugin wired into `vite.config.ts`** — without `svelteTesting()`, vitest loaded Svelte's SSR build and `mount()` threw `lifecycle_function_unavailable`. The plugin sets `resolve.conditions = ['browser', ...]` only when `process.env.VITEST` is set, so production builds are unaffected. Established the canonical Svelte component-testing setup for this project.

- **`docs/decisions/README.md` and `template.md`** — already in place from session 24; the four ADRs in this session validated the format works.

- **Memory updates:**
  - `project_sm_spa_config_layering` — rewritten to match ADR 0003 (was originally written for the now-superseded 0002).
  - `project_sm_daemon_vs_spa_split` — new entry capturing the 0004 rule + the three anti-patterns. Most likely to be load-bearing in future sessions when deciding feature placement.
  - `feedback_svelte_empty_script_block` — captured the operator's convention "always include `<script lang="ts"></script>` even if empty" after I incorrectly recommended deleting empty script blocks earlier.
  - `MEMORY.md` index updated for all of the above.

**Mid-session additions (continued 2026-05-01):**

- **Frequency input validation.** `src/lib/validators/frequency.ts` (`parseFrequency` + `isValidFrequency`) — accepts display format `"14.250.000"` and decimal MHz `"14.250"`; range 100 kHz to 30 GHz; empty/whitespace returns true per the validator-presence convention. 28 unit tests in `frequency.test.ts`.
- **VfoInput edit/commit wiring** — local `editValue` buffer with `editing` flag; commit on Enter or blur (only when valid); revert on Escape; `aria-invalid` while typing invalid; new `onCommit?: (hz: number) => void` prop. Vfos.svelte threads per-VFO commit closures (`(hz) => catState.vfoA = hz`, etc.) into the `box` snippet.
- **Component test coverage** — `VfoInput.test.ts` (13 tests: display, commit on blur, commit on Enter, Escape revert, invalid styling) and 5 new commit-routing tests appended to `Vfos.test.ts` covering closure-routing under both `selectedVfo='A'` and `'B'`. Total: 77 tests across 5 files.
- **ADR 0006 — CAT-state precedence rule (Accepted).** Conversation thread covered the CAT-off vs CAT-on transition, who-writes-when, the bridge-connect handover, split derivation, and bridge liveness. Five sub-decisions settled:
  1. **Three state singletons.** `catState` (rig state) + `qsoDraftState` (in-progress QSO, planned) + `bridgeState` (transport, planned). No field duplicated across them. QSO submit reads from all three.
  2. **Precedence rule.** SPA edit affordances disabled when `catState.enabled && bridgeState.connected`; otherwise SPA writes accepted. Implement once as `const editable = $derived(!(catState.enabled && bridgeState.connected))` — every editable component reads it.
  3. **Bridge-connect transition.** Unconditional read of rig state from first SSE event. Operator's manual edits superseded — *this is the act of CAT handover, not silent loss*. A toast notification ("CAT connected — reading rig state") makes it visible; until the toast system exists this is silent (known technical debt, flag in `bridge.md` when written).
  4. **Liveness: connection-only.** `EventSource.readyState === OPEN`. No heartbeat in v1 — rig-not-changing is indistinguishable from rig-not-responding without one. Deferred. Operator agreed: if it bites, two heartbeat shapes available (bridge keepalive vs CAT-poll-derived `cat-stalled` event); prefer the latter.
  5. **Split derivation.** CAT-off: `split = (vfoA !== vfoB)`. CAT-on: bridge writes `splitOverride: boolean` overriding the derivation. Same-frequency-split limitation accepted (rare; additive fix later via explicit toggle button).
- **ADR 0007 — Keyboard shortcuts (Accepted).** Library: `@svelte-put/shortcut` (~1 KB, action-based, idiomatic Svelte 5). Hand-rolled rejected: modifier normalisation and platform quirks aren't worth re-solving. Initial shortcut map: `Ctrl+\\` (swap VFO, CAT-gated via `editable`), `Ctrl+Enter` (submit QSO, not gated), `Escape` (revert in-field / cancel draft outside field), `?` (help overlay, suppressed in fields). In-field policy: modifier-keyed shortcuts work in-field by default; bare keys check `event.target` against `INPUT`/`TEXTAREA`/`contenteditable` and bail. F1–F12 reserved for future contest macros. Initial map will iterate after ~30 days of operating use; revisions amend the ADR rather than supersede.

- **ADR 0008 — Notifications/toast system (Accepted).** Hand-rolled rather than `@zerodevx/svelte-toast` / `svelte-french-toast` / `svelte-sonner` — the `$state`-queue subscribability fits the architecture and the platform-quirks-layer that justifies a library elsewhere doesn't exist for toasts. State singleton at `lib/states/toasts.svelte.ts` (`Toast[]` reactive array + `pushToast` / `dismissToast` / `info` / `warn` / `error` helpers). Three levels: info (4s TTL), warn (6s), error (8s). Max stack 5; oldest dropped on overflow. Click-to-dismiss always available. `<Toasts/>` mounted once in `app.svelte` shell, `fixed bottom-4 right-4`. Tailwind styling via `@layer components` cluster (`.toast-base`, `.toast-info`, `.toast-warn`, `.toast-error`) — same convention as `.input-base`/`.input-row`. Discipline: **toasts express events, inline messages express state**. First concrete consumer: ADR 0006's CAT-handover toast. Triggers to revisit named: missed-toasts → pause-on-hover; promise-toasts wanted; toast-history view; per-toast custom components.

- **ADR 0009 — CAT-state decomposition (Accepted; refines ADR 0006).** Operator's instinct that the mode-flipped `catState` had a smell turned out to be correct. Forced into the open by the power-with-linear-amp example: `effectivePower = rigPower × ampMultiplier` doesn't fit a single mode-flipped object — there's nowhere for the multiplier to live. Decomposition into four state objects with static ownership: `catState` (rig mirror, bridge-only writes), `manualState` (operator edits in CAT-off mode, SPA-only writes — only the operator-writeable subset of fields), `configState` (operator-declared station properties from `/v1/config`), `displayedState` (derived `$derived.by(...)` view, read by every component, no own storage). `split` and `selectedVfo` become structurally derived projections — no writeable `split` field anywhere; setting "split" in CAT-off mode means changing a VFO frequency. Snapshot-on-CAT-off rule recommended (manualState adopts catState on bridge disconnect for value continuity). All ADR 0006 behavioural rules stand — precedence, edit-affordance lockout, transition handover, connection-only liveness, split-from-divergence, same-frequency-split limitation. ADR 0006 status remains Accepted; a forward-pointer to 0009 was added at the top. Memory `project_sm_cat_precedence_rule.md` rewritten to reflect the four-object structure. The `editable` helper from ADR 0006 stays — gates UI write affordances; less load-bearing now that structural ownership prevents accidental wrong-object writes.

- **ADR 0010 — Rig SSE wire shape (Accepted).** Wire contract for the bridge → SPA event stream that drives `catState` per ADR 0009. **One endpoint** (`GET /v1/rig/events`), **three named events**: `rig-state` (partial JSON — full snapshot on initial connect, deltas thereafter; SPA merges into catState), `rig-disconnected` (carries `{reason}`; SPA flips `bridgeState.rigResponding` false but **does not clear catState** — last-known values stay marked stale), `bridge-error` (operator-relevant errors, toasted via ADR 0008). **Implicit reconnection** — first `rig-state` after disconnect implies reconnect; no separate `rig-reconnected` event. **Passive liveness** — bridge leverages the rig's continuous AUTO-mode data flow (waterfall, telemetry) as the heartbeat; 30s data-flow timeout to detect dead-rig (suggested starting value, tunable). No synthetic heartbeat, no Last-Event-ID, no auth in v1 — all deferred. **Bridge maintains a current-state cache** for delta computation and snapshot-on-connect. **Three SPA flags** drive the `editable` helper: `configState.station.enabled` ∧ `bridgeState.connected` ∧ `bridgeState.rigResponding` — operator can edit when any is false. **Open SSE while rig already disconnected:** bridge sends cached last-known `rig-state` first, then `rig-disconnected` (operator sees what the rig was last on, marked stale). Several v1 limitations explicitly accepted: wedged-but-streaming rig undetectable; brief stale-window between rig disappearance and timeout firing; per-rig data-rate variability not yet handled. Triggers to revisit each named in the ADR.

- **ADR 0011 — manualState persistence (Accepted).** Operator's typed VFO frequencies, mode picks, selectedVfo, etc. now persist across browser refresh via per-field `localStorage` under the `sm.manual.<field>` namespace. Distinct from ADR 0003's no-localStorage stance — that applies to *config* (daemon-authoritative); this ADR adds persistence for *transient session activity per device*. Failure modes (quota, private browsing, disabled storage) silently fall back to in-memory state. Per-field keys (not one big JSON blob) so corrupt-one-field doesn't poison others. Hydration parsers validate per-field type (Number for frequencies/power, string for mode/subMode, `'A' | 'B'` literal check for selectedVfo). Cross-tab divergence accepted for v1; `storage` event sync deferred. Sets up a foundation for `qsoDraftState` to follow the same pattern when it lands.

- **Step 2 implementation — SPA-side state decomposition (ADR 0009).** Four new state singletons under `lib/states/`: `bridge.svelte.ts` (stub `connected`/`rigResponding` flags), `manual.svelte.ts` (operator-edit subset with localStorage persistence per ADR 0011), `config.svelte.ts` (stub with `station.enabled` and `station.ampMultiplier`), `displayed.svelte.ts` (`$derived.by(...)` view that exposes `editable`, `isLive`, `vfoA`, `vfoB`, `mode`, `subMode`, `selectedVfo`, `rigIdentity`, `split`, `rawPower`, `effectivePower`). `cat.svelte.ts` refactored: `splitOverride` and `power` fields added; `enabled` and `split` fields removed (moved to configState/displayedState respectively); DEFAULT_* constants exported for manualState to import; doc comments updated to reflect ADR 0009/0010 framing. `Vfos.svelte` rewired to read from `displayedState` and write to `manualState`. `Vfos.test.ts` rewritten: beforeEach resets all four state singletons + clears localStorage; setup writes to `manualState`; assertions check `manualState`. New 4-test `edit affordance` describe block verifies the three-flag editable rule per ADR 0010.

- **Step 3 implementation — select-VFO UI.** `VfoBox` gained `disabled` and `onSelect` props plus `role="button"`, `tabindex` (0 / -1), `aria-label`, `aria-disabled`, `data-vfo` attributes; click and Enter/Space keydown handlers fire `onSelect`. Visual affordance: `cursor-pointer hover:brightness-110` when editable; `cursor-not-allowed` when disabled; `focus-visible:ring-2` for keyboard focus. `Vfos.svelte` snippet refactored to `box(vfo, action)` — frequency, label, commit closure, and onSelect closure all derived inside from the `vfo` letter via `{@const}`. Both `VfoBox` and `VfoInput` share `disabled={!displayedState.editable}` so the entire CAT-state surface locks in unison. New 10-test `select VFO` describe block in `Vfos.test.ts`: click swap, currently-selected no-op, disabled no-op, Enter/Space keyboard equivalents, ARIA attribute correctness, post-swap RX/TX position update.

- **RST mode-dependent defaults in QsoPanel.** Voice modes (USB/LSB/FM/AM/RTTY/FT8/FT4/PSK31) default RST to `'59'`; CW defaults to `'599'`. Implemented as a `$derived` value reading `displayedState.mode`, passed as the `value` prop to both `Rst` components. **NOT persisted to localStorage** — RST is per-QSO operator activity (computed default + per-QSO edit), not station configuration; this is the explicit boundary between manualState (persisted, per ADR 0011) and the future `qsoDraftState` (not persisted, per ADR 0009). Constants `DEFAULT_RST_VOICE = '59'` and `DEFAULT_RST_CW = '599'` make the convention explicit. Caveat documented inline: with the current `Rst` `value` prop wiring, mid-QSO mode change reactively overwrites operator typing — acceptable for v1 (operators rarely change mode mid-QSO); refinement waits for the QSO submit / draft-reset machinery, at which point these defaults move into `qsoDraftState` as initial-on-draft-creation values.

**Test/check status at session end:** `svelte-check` clean (177 files, 0 errors); 99 tests pass across 6 files (validators × 3 + manualState persistence + VfoInput + Vfos).
- **Documentation updates:**
  - `docs/decisions/0006-cat-state-precedence-rule.md` — new ADR (Accepted).
  - `docs/v2-design/frontend-spa.md` — added 0006 to architectural decisions list; new entries in open questions for QSO draft state, bridge state, keyboard shortcuts (forthcoming 0007); CAT-state precedence struck through with 0006 link.
  - Memory `project_sm_cat_precedence_rule.md` — new entry capturing the rule for future sessions; `MEMORY.md` index updated.

### Next session

**Highest leverage — pick one:**

- **Daemon-side `/v1/config` API.** ADR 0003 promised this; the SPA's hardcoded fallbacks are placeholders waiting for it. First operator-config field is the CAT defaults. Schema decision: single blob or per-section endpoints; PUT-vs-PATCH semantics. Touches `internal/config/` (new operator-config layer adjacent to the existing system-config) and the SPA's `lib/config.svelte.ts` (TBD).

- **Bridge HTTP/SSE surface for `bridge.md`.** Endpoints (`GET /v1/rig`, `GET /v1/rig/events`, `POST /v1/rig/freq`, `POST /v1/rig/mode`), CORS, reconnection semantics. Then `lib/states/bridge.svelte.ts` consumes the SSE stream and updates `catState`. Blocks the live-VFO rendering — currently `Vfos.svelte` only ever shows hardcoded defaults.

- **Daemon-side `/v1/enrich/callsign` handler.** ADR 0005 specifies the contract; implementation lives daemon-side. Reuses v1's hamnut/QRZ services and the cache shape from `internal/database/sqlite/cache.go`. Then `lib/enrichment.svelte.ts` in the SPA is the thin wrapper that Callsign's `onenrich` callback calls into.

**Tactical (small, batch in one commit when convenient):**

- **Fix 13 — inline error message slot under inputs.** Still deferred; lands cleanly once form composition + draft store exist.
- **Toast / notifications system (`docs/v2-design/notifications.md`).** Will be needed before the first async outcome surfaces (QSO save, enrichment completion, bridge connect/disconnect, forwarder progress).
- **Carried magic dimensions** — `h-120`, `h-13.5`, `w-72.5`, `mt-10`, `w-19`, `w-36`, `w-48`, `w-56` — stay inline until 2nd use case appears per the documented threshold.

**Carried over (multi-session):**

- **CI workflow update** — Node ≥22 setup + `task frontend:install && task frontend:build && task frontend:test` before Go build/test. Tests now exist (vitest), so include them.
- **Client-side QSO payload shape** — agree what the client sends to `POST /v1/qso`. Client owns freq/mode (subscribed to bridge) and includes them in the submission.
- **`cmd/logging/` (Gio app) parked** until SPA reaches feature parity, then abandoned cleanly. Don't pre-emptively delete.
- **Chrome DevTools MCP env note** if the plugin upgrades and Chromium-not-Chrome breaks again — see memory `reference_chrome_devtools_mcp_setup`.

### Session 24 work (vitest + testing infra landed, code-review round 2, queued cleanups closed, validator-presence convention settled)

**Operator brief at session start:** add vitest, do a fresh code review of `frontend/logging/src/` to make sure the early shape is right, and work down the carried queue.

**What landed:**

- **Vitest wired up.** Added `vitest`, `@vitest/ui`, `jsdom`, `@testing-library/svelte`, `@testing-library/jest-dom`, `@types/node` as devDeps. New scripts `test`, `test:watch`, `test:ui`. Test config lives in `vite.config.ts` (`test:` block, `jsdom` environment, globals on, `src/test/setup.ts` imports `jest-dom/vitest`). Switched `defineConfig` import from `vite` to `vitest/config` so the `test:` block type-checks. Split tsconfig: new `tsconfig.node.json` covers `vite.config.ts` so the IDE stops complaining about unresolved `@tailwindcss/vite` imports — the root tsconfig references it. Added validator suites at `src/lib/validators/{callsign,rst}.test.ts`. **21 tests pass; svelte-check clean.**

- **Code review round 2** — surfaced one major project-rule violation (focus-context infrastructure) plus the carried queue. Operator deleted `lib/states/focus-context.svelte.ts` + its test (≈500 LOC of speculative/mock-based infra that violated the "build specific, not generic" rule and "no mock interfaces for internal services" rule from CLAUDE.md). Two more concerns surfaced — zero-value wrappers, and validator-presence semantics — both addressed this session.

- **Concern 2 — zero-value wrappers flattened.** `LoggingCardHeader.svelte` and `LoggingCardContent.svelte` deleted; `LoggingCard.svelte` absorbed the header markup directly. Composition chain went from `app → LoggingCard → LoggingCardContent → QsoPanel` (four boundaries, one piece of content) to `app → LoggingCard → QsoPanel` (two boundaries). `LoggingCard` is now load-bearing — owns both the header and the panel slot, ready for sibling panels (StationPanel, InfoPanel) when they land.

- **Concern 3 — validator presence convention settled (2026-05-01).** Validators are pure predicates over the field's data domain; an empty/whitespace-only string returns `true` (= "validator has no objection"). Required-ness is enforced at the form layer, not inside the validator. Removed the `if (v === '') { invalid = false; return; }` shortcuts from both `ValidatedInput.svelte` and `Callsign.svelte`. `Callsign.handleKeydown` now calls `value.trim()` once instead of testing empty + non-trimmed-validity separately. Convention captured in `frontend-spa.md` §"Validators don't enforce presence". Same validator now serves both required (QSO submit) and optional (search filter) contexts without consumers writing the empty-string-shortcut around it.

- **Carried queue closures:** Fix 1 (`@layer container` → `@layer base` typo, fixed by operator), Fix 2 (debug `border border-blue-500` removed from `QsoPanel.svelte:6`), Fix 8 (`min-w-200 max-w-200` redundancy → `w-200` on `app.svelte:5`), Fix 9 (empty `<script>` blocks resolved by Concern-2's structural cleanup; deleted files + `LoggingCard` script now imports `QsoPanel`), Fix 10 *partially* (`.input-row { @apply my-4 h-17.5 }` extracted to `app.css` `@layer components`; `ValidatedInput`/`Callsign` use it now; threshold for further extraction documented — single-use magic numbers stay inline until 2nd use shows up). Trivial cleanups: `.editorconfig` added (4-space indent for code, 2-space for md/yml/yaml/json), `app.svelte` re-indented to match, `public/favicon.ico` placeholder file (0 bytes) silences the dev-server 404.

- **Fix 13 still open (deferred by design).** Validation feedback is currently colour-only (red outline) — discussed in detail. Deferred because the right design depends on QSO draft store + form composition (currently unbuilt). The eventual fix is largely additive: `.input-row`'s `h-17.5` already reserves space for an inline error message slot. Captured the toast-vs-inline distinction during the conversation: toasts are for non-blocking system **events** (QSO saved, enrichment outcome, bridge dis/connect, forwarder progress); inline messages are for **state** of a specific field. Both will exist; they don't substitute. The toast-system design itself is a separate piece of work — capture in `docs/v2-design/notifications.md` (or similar) when the first event-style outcome needs to surface.

- **Items dropped from the queue as no-longer-applicable:** Fix 4 (subsumed by Concern 3's resolution — function name no longer lies because empty is now valid), Fix 5 (`$bindable()` defaults verified during cleanup), Fix 11 (`;` vs `,` separator — current Props interfaces all use `;` consistently).

- **`docs/decisions/` ADR scaffold added.** New directory at `docs/decisions/` with `README.md` (convention, lifecycle, when-to-write), `template.md` (five-section format: Context / Decision / Alternatives considered / Consequences / Triggers to revisit), and `0001-ui-toolkit-browser-spa.md` as a seed example reconstructing the Gio → Wails → SPA decision. Pattern: append-only log, one file per decision, numbered, `status` field walks Proposed → Accepted → Superseded by NNNN. CLAUDE.md "Where the durable project context lives" updated to point at it. Use this when a choice has alternatives that were genuinely weighed and might be revisited; skip for routine code-level choices.

**Queue still open after this session:**

| # | Item | Status |
|---|---|---|
| 13 | Validation feedback design — inline message slot under inputs (Fix 13) | deferred until form composition lands |
| — | Carried magic dimensions (`h-120`, `h-13.5`, `w-72.5`, `mt-10`, `w-19`, `w-36`) | inline until 2nd use case appears (per documented threshold) |
| — | Toast / notifications system (`docs/v2-design/notifications.md`) | needed before first async outcome surfaces (QSO save, enrichment, bridge connect) |

**Files added:**

- `frontend/logging/.editorconfig` — 4-space code, 2-space data
- `frontend/logging/tsconfig.node.json` — covers `vite.config.ts` for IDE/typecheck
- `frontend/logging/src/test/setup.ts` — imports `@testing-library/jest-dom/vitest`
- `frontend/logging/src/lib/validators/callsign.test.ts` — pure validator suite
- `frontend/logging/src/lib/validators/rst.test.ts` — pure validator suite
- `frontend/logging/public/favicon.ico` — 0-byte placeholder

**Files modified:**

- `frontend/logging/package.json` — vitest + testing-library devDeps; `test`/`test:watch`/`test:ui` scripts
- `frontend/logging/vite.config.ts` — `defineConfig` from `vitest/config`; `test:` block (jsdom, globals, setup file, glob)
- `frontend/logging/tsconfig.json` — `types: ["vitest/globals", "@testing-library/jest-dom"]`; references `tsconfig.node.json`
- `frontend/logging/src/styles/app.css` — `.input-row` extracted to `@layer components`
- `frontend/logging/src/lib/ui/components/ValidatedInput.svelte` — empty-string shortcut removed; `class` uses `.input-row`
- `frontend/logging/src/lib/ui/components/Callsign.svelte` — empty-string shortcut removed; `handleKeydown` uses `value.trim()` once; `class` uses `.input-row`
- `frontend/logging/src/lib/validators/callsign.ts` — empty/whitespace returns `true`
- `frontend/logging/src/lib/validators/rst.ts` — empty/whitespace returns `true`; trims before pattern-test
- `frontend/logging/src/app.svelte` — `min-w-200 max-w-200` → `w-200`; re-indented to 4-space
- `frontend/logging/src/lib/ui/cards/LoggingCard.svelte` — absorbed header; imports `QsoPanel` directly
- `frontend/logging/src/lib/ui/panels/QsoPanel.svelte` — debug border removed
- `docs/v2-design/frontend-spa.md` — added "Validators don't enforce presence" subsection; documented `.input-row` + magic-number threshold; removed stale `@layer container` parenthetical

**Files deleted:**

- `frontend/logging/src/lib/states/focus-context.svelte.ts` (+ test) — speculative infra, never wired up, mocked Svelte runtime in its tests
- `frontend/logging/src/lib/ui/cards/LoggingCardHeader.svelte`
- `frontend/logging/src/lib/ui/cards/LoggingCardContent.svelte`

### Next session

**Highest leverage — pick one to start:**

- **Enrichment pipeline architecture (`docs/v2-design/enrichment.md`).** Still the next architecturally-significant decision. Callsign emits "this call is ready" on Tab; the orchestration that consumes it doesn't exist. Decisions: where the orchestrator lives (`enrichment.svelte.ts` module vs a `qsoStore`), source ordering and concurrency (cache → hamnut → QRZ in parallel? cancellation when a newer Tab arrives?), how partial results flow back into the QsoPanel's draft, how loading/error state surfaces, and how the [enrichment-never-blocks-logging](v1-analysis/invariants.md) invariant is enforced at this boundary. **This is now the head-of-queue architecture decision.** Capture in a design doc before code lands.

- **QSO draft store.** `QsoPanel`'s `<Callsign value=""/>` and two `<Rst value=""/>` are inert — `bind:value` has nothing to bind back to. Needs a `qsoDraft.svelte.ts` `$state` module that holds the in-progress QSO and absorbs the `onenrich` callback's results. Likely lands in the same session as enrichment pipeline since the two are coupled.

- **First real route.** `svelte-spa-router` (~3 KB dep) vs hand-rolled hash router (~50 lines). Add `/log` as the first route stub.

- **Bridge HTTP/SSE surface for `bridge.md`.** Endpoints (`GET /v1/rig`, `GET /v1/rig/events`, `POST /v1/rig/freq`, `POST /v1/rig/mode`), CORS, reconnection semantics. Blocks `bridge.svelte.ts`.

**Carried over (multi-session):**

- **CI workflow update** — Node ≥22 setup + `task frontend:install && task frontend:build` before Go build/test. Lockfile committed → CI uses `npm ci` for deterministic resolution. Tests now exist (vitest), so a `task frontend:test` step (or equivalent) should also land.
- **Client-side QSO payload shape** — agree what the client sends to `POST /v1/qso`, given the client (subscribed to bridge) is the only thing that knows current freq/mode.
- **`cmd/logging/` (Gio app) left alone until SPA reaches feature parity**, then abandoned cleanly. Don't pre-emptively delete.
- **Chrome DevTools MCP env note** if the plugin upgrades and Chromium-not-Chrome breaks again — see memory `reference_chrome_devtools_mcp_setup`.

**Tactical (small, batch in one commit when convenient):**

- **Toast / notifications system (`docs/v2-design/notifications.md`).** Inline-message-under-input is for **state**; toasts are for **events**. The two are complementary. Capture: levels (info/warn/error), default TTL, max stack depth, dismissable behaviour, mount point. Will be needed before the first async outcome surfaces (QSO save, enrichment, bridge connect/disconnect, forwarder progress).
- **Fix 13 — inline error message slot under inputs.** `.input-row`'s `h-17.5` already reserves space. Open question: where the error string comes from (validators stay `boolean`, components hold their own message string is the leading candidate; alternative is `{ valid, message }` return tuple but that breaks the pure-predicate convention). Lands cleanly once form composition + draft store exist.
- **Carried magic dimensions** — `h-120`, `h-13.5`, `w-72.5`, `mt-10`, `w-19`, `w-36` stay inline until 2nd use case appears. Per documented threshold in `frontend-spa.md`.

### Session 23 work (dev workflow established; first round of input components shipped; component-pattern split between generic primitive and domain widgets settled)

**Operator brief at session start:** open the SPA dev loop, do real frontend work, code-review the first components landed off-session, and resolve a design tension that surfaced — Callsign was shaped to fit a generic `ValidatedInput`, but Tab on callsign is the QSO-enrichment trigger and that pulls the component back into domain-shaped territory.

**What landed:**

- **Dev workflow nailed.** Two-terminal HMR loop established — `task run:smd` (new — `go run ./cmd/smd`) in one terminal for the daemon's `/v1/*`, `task frontend:dev` in another for Vite on `:5173` with `/v1/*` proxied. `dist/` is *not* rebuilt during dev; that path (`task build:smd`) is only for verifying the daemon-embedded bundle. Operator confused dev/embed paths once during the session — captured in `frontend-spa.md` §"Dev workflow" so it's the first-stop reference next time.

- **Code review of `frontend/logging/src/`** — fifteen items surfaced ranging from probable bugs to nits. Three landed this session; the rest are queued for the next.

- **Fix 3 — `Callsign.validateAndFocus` always-on-blur.** The pre-session implementation only ran validation when `lastKey === "Tab"`, so click-away or any non-Tab focus shift left the field silently in an invalid state. Removed the `lastKey` tracking, the `onkeydown` handler that fed it, and the unused `async`/`Promise<void>`. Function now matches `Rst.svelte`'s shape exactly.

- **Fix 6 — generic primitive + thin wrappers extraction (then partially reverted).** Initial pass:
  - `src/lib/ui/components/ValidatedInput.svelte` — primitive that takes a `validator: (v: string) => boolean` prop, plus `widthClass`, `inputClass`, and `...rest` for passthrough HTML input attrs.
  - `src/lib/validators/callsign.ts` and `src/lib/validators/rst.ts` — pure validator modules with constants module-level (also closes fix 7's "validator constants belong outside the component").
  - `Callsign.svelte` and `Rst.svelte` reduced to ~15-line semantic wrappers.

  Then the operator pointed out: **Tab on callsign is the signal for QSO enrichment** (online lookups, hamnut/QRZ, cache hits, populating downstream fields). That's a domain behaviour, not a generic-input concern. Bolting it onto `ValidatedInput` via callbacks would push the abstraction toward exactly the v1-adapter shape the project's "build specific, not generic" rule warns against. **Callsign was reverted to a standalone component** (no longer a thin wrapper) with an optional `onenrich?: (callsign: string) => void` prop fired on Tab when the value is non-empty and valid. Default Tab behaviour preserved (no `preventDefault`); enrichment kicks off in parallel with focus shifting to RST Sent. The callback receives the normalised callsign.

  **What stayed from the initial extraction:**
  - `validators/callsign.ts` and `validators/rst.ts` — pure modules, valid independent of component shape.
  - `ValidatedInput.svelte` — kept as the primitive for the well-formed-string-only family. Currently has one consumer (RST). The design bet: Frequency, Grid Square, possibly Operator land as wrappers in the next round, justifying the abstraction. If they don't, inlining ValidatedInput back into Rst is trivial.

- **Component patterns documented.** Added a new "Component patterns" section to [`docs/v2-design/frontend-spa.md`](v2-design/frontend-spa.md) covering: generic-primitive-vs-domain-component rule, Tab-on-callsign as the enrichment trigger boundary, validators as pure modules under `lib/validators/`, and the global-CSS conventions (`@layer base` for element defaults, `@layer components` for `@apply` clusters, `@theme` for design tokens; no `tailwind.config.js`).

- **Globals in `src/styles/app.css`.** During the session the operator added `.input-base`, `.input-label`, `.invalid-input` `@apply` clusters in `@layer components`, plus typographic defaults. Note: the file currently uses `@layer container` (typo) which is non-load-bearing — flagged for fix in the next session.

**Code review fixes still queued (from the same review, not addressed this session):**

| # | Item | Status |
|---|---|---|
| 1 | `@layer container` → `@layer base` typo in `app.css` | queued |
| 2 | Debug `border border-blue-500` on `QsoPanel.svelte:6` | queued (red borders on inputs gone via Callsign/Rst rework) |
| 4 | `isValid("")` returns `false` in Callsign — function name lies even if behaviour is OK due to caller-side empty-input shortcuts | queued |
| 5 | `$bindable()` defensive defaults | partially closed (defaults added to wrappers; verify Callsign) |
| 8 | `min-w-200 max-w-200` redundancy in `app.svelte:5` | queued |
| 9 | Empty `<script lang="ts">` blocks in `LoggingCardHeader.svelte` etc. | queued |
| 10 | Magic dimensions (`h-13.5`, `h-17.5`, `h-120`, `w-19`, `w-200`) — promote to `@theme` tokens | queued |
| 11 | `;` vs `,` separator inconsistency in `Props` interfaces | queued |
| 13 | Validation feedback colour-only (no icon/border-style cue for colour-blind operators) | queued (low priority) |

**Files added:**

- `frontend/logging/src/lib/ui/components/ValidatedInput.svelte` — generic primitive
- `frontend/logging/src/lib/validators/callsign.ts` — pure validator + regex constants
- `frontend/logging/src/lib/validators/rst.ts` — pure validator

**Files modified:**

- `frontend/logging/src/lib/ui/components/Callsign.svelte` — standalone domain component with `onenrich` callback fired on valid-Tab
- `frontend/logging/src/lib/ui/components/Rst.svelte` — thin wrapper around `ValidatedInput`
- `Taskfile.yml` — added `run:smd` (direct `go run ./cmd/smd`, no rebuild loop, paired with `frontend:dev`)
- `docs/v2-design/frontend-spa.md` — new "Component patterns" and "Dev workflow" sections

### Next session

**Highest leverage — pick one to start:**

- **Enrichment pipeline architecture (`docs/v2-design/enrichment.md`).** The Callsign component now emits "this call is ready" on Tab; the orchestration that consumes it doesn't exist. Decisions to make: where the orchestrator lives (`enrichment.svelte.ts` module vs a `qsoStore`), source ordering and concurrency (cache → hamnut → QRZ in parallel? cancellation when a newer Tab arrives?), how partial results flow back into the QsoPanel's draft, how loading/error state surfaces, and how the [enrichment-never-blocks-logging](v1-analysis/invariants.md) invariant is enforced at this boundary. **This is the next architecturally-significant decision** — capture it in a design doc before code lands.

- **First real route.** Pick `svelte-spa-router` (~3 KB) vs hand-rolled hash router (~50 lines) and add `/log` as the first route stub. Tailwind v4 toolchain is verified.

- **Bridge HTTP/SSE surface for `bridge.md`.** Endpoints (`GET /v1/rig`, `GET /v1/rig/events`, `POST /v1/rig/freq`, `POST /v1/rig/mode`), CORS, reconnection semantics. Blocks `bridge.svelte.ts`.

**Cleanups (small, batch in one commit):**

- Walk through the queued code-review items table above. Most are 1–5 minute fixes (`@layer base` typo, `border border-blue-500` debug colour, `min-w-200 max-w-200` redundancy, empty `<script>` blocks, magic dimensions to `@theme` tokens).
- Drop `frontend/logging/public/favicon.ico` to silence the cosmetic 404.

**Carried over from session 22:**

- **CI workflow update** — Node ≥22 setup + `task frontend:install && task frontend:build` before Go build/test. Lockfile committed → CI uses `npm ci` for deterministic resolution.
- **Client-side QSO payload shape** — agree what the client sends to `POST /v1/qso`, given the client (subscribed to bridge) is the only thing that knows current freq/mode.
- **`cmd/logging/` (Gio app) left alone until SPA reaches feature parity, then abandoned cleanly.** Don't pre-emptively delete.
- **Chrome DevTools MCP env note** if the plugin upgrades and Chromium-not-Chrome breaks again — see memory `reference_chrome_devtools_mcp_setup`.

### Session 22 work (architecture conversation captured into v2-design docs; SPA scaffold landed with both daemon-embed and Vite-dev paths verified live via Chrome DevTools MCP)

**Three open questions surfaced; UI toolkit, CAT-perf, and CSS-approach decisions all resolved by end of session. Frontend scaffold sketched, implemented, and verified end-to-end.**

**What landed:**

- **`cmd/logging` UI step:** Mode picker added. New file `cmd/logging/mode_cycle.go` — a click-to-advance cycle button over `[]string{"USB", "LSB", "CW"}`. Wired into `qsoSectionTop` as a 4th cell next to Callsign/RST Sent/RST Rcvd, using a `labeledMode(theme)` helper that mirrors `labeledInput`'s vertical label-over-content shape. Cycle button rebuilt to match `borderedInput` exactly (1 dp `gray500` border, 4 dp corner radius, 6 dp uniform inset, body1 text) so heights align across the row. Pinned to fixed 60 dp width via `gtx.Constraints.Min.X = gtx.Constraints.Max.X = gtx.Dp(modeFieldWidth)` so frame width doesn't reflow as the label changes.

- **Tailwind v4 palettes added to `cmd/logging/colours.go`:** `red50–red950`, `gray50–gray950`, `green50–green950` alongside the existing indigo palette. Each block has a header comment listing canonical `oklch(L C H)` tuples for traceability. `borderedInput` (in `main.go`) now uses `gray500` for its border (`inputBorderColor` const removed).

- **UI toolkit decision resolved 2026-04-30: browser SPA, Svelte 5 + Vite + plain TS, embedded into the daemon via `//go:embed`.** Operator concern that prompted the reconsideration: Gio learning curve fights the "small steps / no cognitive debt" working-mode preference. Three options analysed (stick with Gio / fall back to Wails v2 / browser SPA hosted by daemon). Operator confirmed Svelte 5 over Vue/React on three grounds: compiled-reactivity DOM updates suit a long-running tab consuming a 10–20 Hz rig SSE stream; bundle size matters when the SPA is `go:embed`-ed; existing fluency removes cognitive overhead. Performance is not the deciding factor (see topology and cat-performance docs). [`docs/v2-design/ui-toolkit.md`](v2-design/ui-toolkit.md) was updated with the resolution; the SPA scaffold itself is captured in [`docs/v2-design/frontend-spa.md`](v2-design/frontend-spa.md).

- **Frontend SPA scaffold designed AND landed AND verified.** `frontend/logging/` (Svelte 5 + Vite 6 + plain TS + Tailwind CSS v4, *not* SvelteKit). Daemon-side: new `frontend/embed.go` package owns `//go:embed all:logging/dist`, `internal/api/spa.go` is the `spaHandler` (with index.html-fallback for client-side routing), `internal/api/server.go` registers `GET /` conditionally on `cfg.Server.Protocol == "tcp" && *cfg.Server.ServeSPA` so Unix-socket headless deployments stay supported. **Implementation simplification vs. the original sketch:** `LoggingFS()` returns just `fs.FS` (no error) — `fs.Sub` on a hard-coded valid embed path is infallible at runtime; the panic-on-error stays as a programmer-error guard but is unreachable. This kept `api.New()`'s signature stable, so no test changes were needed. **Tailwind CSS v4 added during scaffolding** — the original "plain scoped component CSS to start" stance was reversed in favour of v4's CSS-first config (`@tailwindcss/vite` plugin, single `@import "tailwindcss";`). Verified via Chrome DevTools MCP that all utilities apply correctly. **CI builds the SPA before `go build`; `dist/` is NOT committed** (except a placeholder `dist/index.html` so first-time builds compile before anyone runs `npm install`). Full file inventory + verification tables in [`docs/v2-design/frontend-spa.md`](v2-design/frontend-spa.md) §"Scaffold landed and verified".

- **End-to-end smoke test landed via Chrome DevTools MCP.** Two paths verified:
  - **Path A (daemon-embedded placeholder):** `task build && ./build/bin/smd` → `GET /v1/healthz` 200 → `GET /` returns embedded `dist/index.html` → page title + h1 + body text confirmed via accessibility-tree snapshot in Chromium. Plus `internal/api/spa_test.go` covers the same handler at unit level (root + four fallback paths).
  - **Path B (Vite dev server with the real Svelte 5 + Tailwind app):** `task frontend:install` + `task frontend:dev` → 17 network requests all 200 (one cosmetic 404 for `favicon.ico`) → Svelte 5 `$state` rune rendered → seven Tailwind v4 utility classes verified via computed-style readback (oklch colours, viewport min-height, font stack, font weights). The whole frontend toolchain works end-to-end: Vite + `@sveltejs/vite-plugin-svelte` + `@tailwindcss/vite` + Svelte 5 + the `$state` rune + HMR.

- **Topology refined:** the bridge is a **peer** of the daemon, not a subordinate. Load-bearing distinction = host-bound (rig wires) vs network-shaped (storage + HTTP). Bridge owns CAT/serial/PTT/audio (host-bound by physics); daemon owns log + forwarding + SPA hosting (network-shaped, can live anywhere). **The two never talk to each other — they share clients.** Client subscribes to bridge for live rig state, submits QSOs to daemon with freq/mode in the payload. Earlier wording about "daemon brokers events from the bridge" was wrong if "brokers" implied bridge → daemon → client; the correct model is bridge → client and daemon → client as parallel channels. Enables four deployment topologies (all-on-one, server+shack, remote operating, multi-rig) without code changes. Captured in [`docs/v2-design/topology.md`](v2-design/topology.md).

- **CAT codec perf analysed and baselined.** Code-read of `internal/cat/codec.go` and `rigdb.go`. Real hot-spots identified at file:line — `Status{}` fresh-allocated per `Decode` call (`codec.go:55`) is the biggest cost; `lookupState` linear scan with `bytes.EqualFold` (`codec.go:107`) is second. The "map and rehashing" the operator was recalling is **not** `rigDB` (that's cold-path init-only) — it was the per-frame `Status` map allocation. Ranked optimisations Tier 1/2/3 documented. **Baseline benchmarks added at `internal/cat/codec_bench_test.go`**, captured 2026-04-30 on Intel i3-10100F: `BenchmarkDecode` 197.5 ns/op / 352 B/op / 3 allocs/op, `BenchmarkLookupState` 60.05 ns/op / 16 B/op / 1 alloc/op. The codec is an order of magnitude faster than the doc's pre-bench estimate — at 100 ms poll cadence it's 0.0002% of the budget. **Decision (operator-confirmed): don't refactor.** Code stays as-is; benchmarks remain as a regression guard. Real latency dominators are bridge-side (poll interval, USB-serial latency timer, TCP_NODELAY, async/auto-tx mode). The Tier-1 refactor (slice-of-tags or caller-owned map) is only worth doing if multi-client SSE fan-out later shows GC pressure correlating with codec allocations in a `runtime/trace`. Captured in [`docs/v2-design/cat-performance.md`](v2-design/cat-performance.md).

**Files added:**

- `docs/v2-design/ui-toolkit.md` — Gio vs Wails vs browser-SPA analysis (decision resolved end of session)
- `docs/v2-design/topology.md` — bridge/daemon/client peer model, deployment scenarios, CORS/auth/discovery practicalities
- `docs/v2-design/cat-performance.md` — codec hot-spot analysis cited to file:line, ranked optimisations, baseline benchmark numbers
- `docs/v2-design/frontend-spa.md` — SPA scaffold, embed wiring, build pipeline, CI stance, "Scaffold landed and verified" verification tables
- `cmd/logging/mode_cycle.go` — Mode cycle-button widget
- `internal/cat/codec_bench_test.go` — baseline `BenchmarkDecode` + `BenchmarkLookupState`
- `frontend/embed.go` — Go embed package owning the SPA `dist/` filesystem
- `frontend/logging/` — full scaffold: `package.json`, `vite.config.ts`, `tsconfig.json`, `svelte.config.js`, `index.html`, `src/main.ts`, `src/app.svelte`, `src/styles/app.css` (Tailwind import), `dist/index.html` (placeholder), `package-lock.json`
- `internal/api/spa.go` — `spaHandler` with index.html-fallback
- `internal/api/spa_test.go` — covers root + four fallback paths

**Files modified:**

- `internal/config/config.go` — `cfg.Server.ServeSPA *bool` with TCP-default-true logic
- `internal/api/server.go` — imports `frontend`; conditionally registers `GET /` SPA route
- `Taskfile.yml` — `frontend:install` (auto-detects lockfile), `frontend:dev`, `frontend:build`, `build:smd`
- `.gitignore` — ignores `frontend/logging/{node_modules,dist/*}` except `dist/index.html`
- `build/config.json` — switched to TCP `127.0.0.1:8080` for the smoke test (daemon configurable per deployment)

### Next session

- **Land the first real route.** Pick between `svelte-spa-router` (~3 KB dep) and a hand-rolled hash router (~50 lines). Add `/log` as the first route stub. The Tailwind v4 toolchain is verified working; use it.
- **Bridge HTTP/SSE surface** — concrete API design to drop into `bridge.md`. Endpoints: `GET /v1/rig` (snapshot), `GET /v1/rig/events` (SSE stream), `POST /v1/rig/freq`, `POST /v1/rig/mode`, plus the existing rigctld TCP frontend unchanged. CORS (`Access-Control-Allow-Origin: *` for single-user, scoped to daemon origin for stricter setups). Reconnection semantics on the SSE side.
- **`bridge.svelte.ts` rig-state module** — once the bridge SSE shape is settled. Module-level `$state` holds `{freq, mode, vfo}`; any component that reads it re-renders on EventSource updates. ~30 lines.
- **Client-side QSO payload shape** — agree what the client sends to `POST /v1/qso`. Since the client is the only thing that knows current freq/mode (subscribed to bridge), it must include those fields in the QSO submission. Daemon takes them as authoritative.
- **CI workflow update** — add Node ≥22 setup + `task frontend:install && task frontend:build` before the Go build/test steps. The `//go:embed` enforces ordering — missing `dist/` is a compile error, so a botched CI step surfaces immediately. (Lockfile is now committed, so CI can use `npm ci` for deterministic resolution.)
- **Trivial cleanup:** drop `frontend/logging/public/favicon.ico` (any file) to silence the cosmetic 404 the Vite smoke test surfaced. Or accept it.
- **Chrome DevTools MCP env note** — see memory `reference_chrome_devtools_mcp_setup`. The plugin's launch config is read from `~/.claude/plugins/cache/chrome-devtools-plugins/chrome-devtools-mcp/<version>/.claude-plugin/plugin.json`; if you upgrade the plugin and Chromium-not-Chrome breaks browser automation again, re-add `--executablePath=/usr/bin/chromium-browser --isolated` to that file's `mcpServers.args` array.
- **`cmd/logging/` (Gio app) is left alone for now.** Per `ui-toolkit.md` and `frontend-spa.md`, it stays in place until the SPA reaches feature parity, then gets abandoned cleanly. Don't pre-emptively delete.

### Session 21 work (UI frame built up in single-concept steps; entry-row helpers parked for later)

**Working mode (operator-set):** *"work through building the logging app UI with you in very small steps — I don't want cognitive debt."* Each step landed one new concept with a short explanation: Constraints (Min/Max), Flex axes, `Rigid` vs `Flexed`, single-edge rules via `clip+paint` (since `widget.Border` is all-four-or-nothing), border composition (panels compose like HTML boxes — adjacent borders sit side-by-side, no `border-collapse`).

### Session 21 work (UI frame built up in single-concept steps; entry-row helpers parked for later)

**Working mode (operator-set):** *"work through building the logging app UI with you in very small steps — I don't want cognitive debt."* Each step landed one new concept with a short explanation: Constraints (Min/Max), Flex axes, `Rigid` vs `Flexed`, single-edge rules via `clip+paint` (since `widget.Border` is all-four-or-nothing), border composition (panels compose like HTML boxes — adjacent borders sit side-by-side, no `border-collapse`).

**What landed:**

- **`run()` reset to a bare event loop, helpers kept in place.** The session-20 three-column QSO entry row was *not deleted* — `layoutUI`, `labeledInput`, `borderedInput`, the callsign/RST editors, focus logic — they're parked as package-level helpers and constants in `main.go`, just no longer called from `run()`. Intent: reintroduce them inside the green left panel once the outer frame is finalised. Go is fine with unused package-level funcs; the imports they pull in (`material`, `widget`) are still used by the helpers themselves.

- **`task run:logging` added to `Taskfile.yml`** — builds *only* `cmd/logging` and runs the binary (skips daemon + full-module build). Picks up `SM_WORKING_DIR` from `.env` like the existing `run` task. Faster cycle when iterating on UI.

- **Top frame assembled, three nested layers, debug-coloured borders so each layer is identifiable on screen:**
  - Outer: `layout.Flex{Axis: Vertical}` in `run()`'s `FrameEvent`, with `statusRow()` and `mainRow()` as `Rigid` children.
  - **`statusRow()`** — full-width, fixed height (currently `unit.Dp(40)`), single 1dp red rule along the *bottom edge only*. `widget.Border` paints all four sides, so for a single-edge rule we drop `widget.Border` and use `clip.Rect(...).Push(ops)` + `paint.ColorOp` + `paint.PaintOp` to fill a 1px-tall image rectangle along the bottom. Pattern: clip-as-shape — the clip's geometry *is* the painted shape; `PaintOp{}` paints the entire active clip.
  - **`mainRow()`** — full-width, fixed height (currently `unit.Dp(400)`), framed by a 1dp blue `widget.Border` (all four sides). Inner content is a `layout.Flex{Axis: Horizontal}` split 2/3 + 1/3 via `Flexed(2, …)` / `Flexed(1, …)`. Weights are relative numbers, not percentages.
  - **Inner panels** — left panel has a 1dp green border, right panel a 1dp yellow border, both via `borderedPanel(c color.NRGBA) layout.Widget`. Wraps `fillPanel` (a primitive that pins `Min = Max` and returns those dims) in a `widget.Border`. Same higher-order shape as `labeledInput` / `borderedInput` — function takes config, returns `layout.Widget`. The blue outer border + adjacent green/yellow borders produce visible 2px seams; per session-21 confirmation, that's the expected "borders compose, they don't merge" behaviour (CSS analogue: no `border-collapse`).

- **Debug colours split into `cmd/logging/colours.go`.** Centralised so `main.go` doesn't carry `image/color` purely for var declarations. Currently holds: `inputBorderColor` (`#101828` — production input outline), `statusRowBorderColor` (red), `mainRowBorderColor` (blue), `leftPanelBorderColor` (green), `rightPanelBorderColor` (yellow). The four debug colours are flagged as "temporary debug" in their doc comments — they come out when each layer gets its real fill / contents.

- **New imports in `main.go`** to support the frame work: `image`, `image/color`, `gioui.org/op/clip`, `gioui.org/op/paint`.

**Helper inventory in `cmd/logging/main.go` after session 21:**

| Helper | Status | Returns | Purpose |
|---|---|---|---|
| `statusRow()` | live | `layout.Widget` | top status strip with bottom-edge rule |
| `mainRow()` | live | `layout.Widget` | 2/3 + 1/3 row beneath status |
| `fillPanel` | live | `layout.Dimensions` | bare "claim my assigned space" primitive |
| `borderedPanel(c)` | live | `layout.Widget` | parameterised colour-bordered fill panel |
| `layoutUI(...)` | parked | `layout.Dimensions` | three-column QSO entry row from session 20 |
| `labeledInput(...)` | parked | `layout.Widget` | label-above-input vertical pair |
| `borderedInput(...)` | parked | `layout.Widget` | fixed-width outlined editor |
| `loadConfig(path)` | live | `(config.Config, error)` | flag → env → cwd → defaults config resolution |

### Next session (session 21 — *superseded by session 22's Next session block above; preserved for the sub-items still relevant if the Gio path is kept*)

- **Replace debug border colours with real fills/contents.** The four debug colours (red status-row rule, blue main-row frame, green/yellow inner panels) come out as each layer gets its real treatment. Status row is the natural first one — see below.
- **Status row contents** (red debug rule comes out): `Logging Mode: [Normal ▾] | Logbook: <name> | Rig: <model> | Session Time: hh:mm:ss`. Session time = monotonic counter started at window-open. Inset the row contents 8dp horizontally so text doesn't sit flush against the window edge.
- **Reintroduce the three-column QSO entry row inside the green left panel** of `mainRow`. The helpers (`layoutUI`, `labeledInput`, `borderedInput`) and editor variables are already parked in `main.go` — the work is wiring them through the `mainRow()` left panel instead of the current `borderedPanel(leftPanelBorderColor)`. Restore the post-layout `key.FocusCmd{Tag: &callsign}` first-frame focus call.
- **Right panel content** — 1/3-width pane: session list (per `project_sm_session_scope` memory: client-side, no daemon endpoints). For now a placeholder header + empty list view is enough.
- **Register `smclient` as the first real iocdi service with a dependency.** Precondition for a working Log Contact button (POST `/v1/qso` to the daemon). Stub `hamnut`, `qrz`, `enrichment`, `email`, `rigloop` as iocdi-shaped placeholders with green `Initialize()` and TODO bodies so the service graph is fully visible from `cmd/logging/container.go`.
- **Remaining v1-layout rows** (after the entry row is back inside the left panel): Row 2 — Name / Qth / Comment (textarea via `widget.Editor` with `SingleLine = false`); Row 3 — Date picker, Time On UTC, Time Off UTC, Log Contact (filled button → smclient.Submit), Clear (outline button → reset editors).
- **Mode dropdown** (Row 1 completion): Gio has no stock dropdown — choose between a cycle-button (click cycles SSB/CW/FT8/…) and a small custom menu. Worth a small spike.
- **Frequency readout + VFO-A/B** (Row 1 completion): blocked on `rigloop`. Will land once that service exists.
- **Drop `//go:build gio` from `cmd/giospike/main.go`** (or delete `cmd/giospike/` entirely — spike's job is done; see memory `project_sm_ui_toolkit`). CI has the deps.
- **`internal/rigconfig` composition function** — still unblocked. Expected shape: `rigconfig.Compose(types.RigConfig, cat.RigDefinition) (serial.Config, error)`. Absorbs the ~15 LOC inline helper duplicated in `cmd/catcli/` and `cmd/giospike/`.
- **Open item from session 16 still outstanding:** mystery `FD` prefix on FTdx10 in AI mode. Investigate opportunistically.

### Session 20 work (toolchain modernisation + cmd/logging wired into iocdi + three-column QSO entry row)

**What landed:**

- **Go 1.26.2 bump.** `go.mod` updated from `go 1.25.0` → `go 1.26.2` (operator installed the toolchain locally; CI picks it up automatically via `actions/setup-go` with `go-version-file: go.mod`). Clean `go mod tidy`; full build + `go test -race ./...` green post-bump.

- **`go vet ./...`** — zero findings after the toolchain bump. Already part of CI.

- **Modernize pass via `gopls/modernize`.** Dry-run surfaced ~130 findings; applied the safe bucket across 29 files in one pass, reverted two judgment items for review:
  - **Safe (applied):** `interface{}` → `any`, `for i := 0; i < n; i++` → `for i := range n`, `if x > y { x = y }` → `min`/`max`, `[]byte(fmt.Sprintf(...))` → `fmt.Appendf`, `m[k]=v` loop → `maps.Copy`, `b.N` → `b.Loop()`, `go func() + wg.Wait()` → `wg.Go(fn)` (Go 1.25 `WaitGroup.Go`), `context.WithCancel` in tests → `t.Context()`, `slices.Contains`, `reflect.TypeOf((*T)(nil)).Elem()` → `reflect.TypeFor[T]()` (hand-written code only).
  - **Skipped (generated code):** all hits under `internal/database/sqlite/models/*.go` — sqlboiler output, per CLAUDE.md rule "sqlboiler-generated models are not hand-edited."
  - **Judgment item #1 — applied:** `internal/types/rig.go:39` `json:"overrides,omitempty"` → `json:"overrides,omitzero"`. The original tag was a no-op (omitempty has no effect on nested struct fields); `omitzero` (Go 1.24+) actually omits when the struct is zero-valued, matching the doc-comment's "missing = inherit" promise.
  - **Judgment item #2 — skipped:** `internal/iocdi/internal.go:29` offered `reflect.Type.Fields()` iteration (new in Go 1.26). Low value for the cost — the rewrite saves two lines but introduces a `field := field` shadow in DI-container hot-path reflection code; kept the explicit index loop.

- **zerolog deprecation fixed.** `internal/logging/event.go:405` — `zerolog.Dict()` is deprecated because it doesn't preserve the parent event's stack, hooks, or context. Swapped to `e.event.CreateDict()`. `zerolog` import remains for level constants and type references elsewhere in the file.

- **`Taskfile.yml` build target now emits the logging-app binary.** Added `go build -o build/bin/logging ./cmd/logging` alongside the existing `smd` line, so `task build` produces both `build/bin/smd` and `build/bin/logging`.

- **CI Gio Linux deps finalised.** Two-pass fix: initial apt list missed `libx11-xcb-dev` and `libxfixes-dev` (Gio's pkg-config requires `x11-xcb` and `xfixes`, both shipped as separate Debian/Ubuntu packages from `libx11-dev`). Final list: `libwayland-dev libxkbcommon-dev libxkbcommon-x11-dev libvulkan-dev libgles2-mesa-dev libegl1-mesa-dev libffi-dev libx11-dev libx11-xcb-dev libxcb1-dev libxcursor-dev libxfixes-dev libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev`.

- **`cmd/logging` wired into iocdi.** `cmd/logging/container.go` (new) exposes `buildContainer(cfg config.Config) (*iocdi.Container, error)`, mirroring `cmd/smd`'s pattern: registers the `config` service (instance) and the `logging` service (type via `reflect.TypeFor[*logging.Service]()`), sets the `LiteralProvider` so `logging.Service.WorkingDir` (`di.inject:"workingdir"`) resolves from `cfgSvc.WorkingDir()`, then calls `container.Build()` to fire `Initialize()` in dependency order. Uses the `errors.Op = "logging.app.main.buildContainer"` convention. A leading comment lists the six services still-to-register so the service graph is visible in source even before implementations exist: `smclient`, `hamnut`, `qrz`, `enrichment`, `email`, `rigloop`.

  `cmd/logging/main.go` now:
  - Parses `-config` flag, loads config via `loadConfig` (same resolution order as `cmd/smd`: explicit path → `$SM_WORKING_DIR/config.json` → `./config.json` → `config.DefaultConfig(cwd)`).
  - Calls `buildContainer(cfg)`, resolves `*logging.Service` out of the container.
  - Spawns the Gio goroutine, passing the logger into `run()`. Logger emits `"logging app started"` / `"logging app stopped"` bookends; `loggerSvc.Close()` runs before `os.Exit` in the shutdown path.
  - `DestroyEvent` returns `nil` cleanly when `e.Err == nil` (previously wrapped nil as an error).

- **First QSO entry row rendered** (three fixed-width labeled inputs, left-aligned, horizontal flex, top of the 16dp-inset window):
  - **Callsign** — `unit.Dp(130)` (~10 proportional chars), hint `"G0ABC"`, receives initial focus on first `FrameEvent` via `gtx.Execute(key.FocusCmd{Tag: &callsign})` *after* `layoutUI` (Gio's focus command resolves against registered event.Ops, so it must run post-layout).
  - **RST Sent** — `unit.Dp(60)` (~3 digits), default value `"59"` set via `SetText("59")`.
  - **RST Rcvd** — same width, default `"59"`.
  - All three are `widget.Editor` with `SingleLine = true, Submit = true`. All share a `widget.Border` frame (1dp outline in `#101828`, 4dp corner radius, 6dp inner inset). Input border colour is a package-level `color.NRGBA` constant — named, not parameterised, per session-20 decision below.
  - Labels use `material.Body2` (≈12sp) rather than `Body1` (≈14sp) — slight reduction kept tight vertical rhythm as the row grew to three columns.
  - Helpers: `labeledInput(th, label, ed, hint, width)` returns a vertical flex (label stacked above input), `borderedInput(th, ed, hint, width)` returns the fixed-width outlined editor. Both return `layout.Widget`, matching Gio's `material.*` idiom. Kept in `main.go` — splitting to `widgets.go` is teed up for when a third widget type lands (not a third instance of the same type).

- **v1 layout reference logged** (operator shared a v1 logging-window screenshot). Alignment notes for v2:
  - **Kept:** status row (`Logging Mode: [Normal ▾] | Logbook: <name> | Rig: <model> | Session Time: hh:mm:ss`), three-row entry block (Row 1: Callsign / RST Sent / RST Rcvd / Mode / VFO-A/B + freq + band; Row 2: Name / Qth / Comment; Row 3: Date / Time On UTC / Time Off UTC / Log Contact / Clear), bottom sub-tab strip (`Worked / Details / My Station / Session`) over a QSO table.
  - **Dropped:** the v1 top-level tab strip (`Logging` / `Control` as siblings). In v2, `cmd/logging` and a future `cmd/control` (CAT client) are separate binaries — no in-app nav between them.

- **Widget-abstraction discussion closed** (operator asked whether to split widgets into reusable blocks). Outcome: the current function-returning-`layout.Widget` shape is the Gio idiom; don't promote to a framework prematurely. Named constants over parameters for colour/inset/size. Extract to a `cmd/logging/widgets.go` file once a third *kind* of widget lands (dropdown, date picker, etc.), not on the third instance of the same kind. Promote to an `internal/ui` package only when a second app (`cmd/logbook`, `cmd/config`) needs the same widget — that's when the API has two uses to fit, rather than speculation.

*(Session 20's "Next session" goals are superseded by session 21's "Next session" block above. Service-registration, mode dropdown, and rig-loop-blocked items were rolled forward; layout-frame items have been taken.)*

### Session 19 work (cmd/logging main window + CI Linux deps for Gio)

**What landed:**

- **`cmd/logging/main.go` — empty 1024×751 Gio main window.** Fixed-size window constants (`windowWidth = unit.Dp(1024)`, `windowHeight = unit.Dp(751)`), title "Station Manager — Logging". Standard Gio event loop: `DestroyEvent` exits cleanly (wrapped through `errors.Op = "logging.app.main.run"` so the shutdown path uses the project error convention), `FrameEvent` renders an empty frame. No widgets yet — this is the shell on which the iocdi-wired service graph and the first QSO-entry row will be built.
  - Import note: Gio's `op` package is aliased as `giop` so it doesn't collide with the `errors.Op` constant used inside `run()`.

- **CI Linux deps for Gio** (`.github/workflows/ci.yml`) — `apt-get install` step added before `go vet`. Installs: `libwayland-dev`, `libxkbcommon-dev`, `libxkbcommon-x11-dev`, `libvulkan-dev`, `libgles2-mesa-dev`, `libegl1-mesa-dev`, `libffi-dev`, `libx11-dev`, `libx11-xcb-dev`, `libxcb1-dev`, `libxcursor-dev`, `libxfixes-dev`, `libxrandr-dev`, `libxinerama-dev`, `libxi-dev`, `libxxf86vm-dev`. Two iterations — the first pass missed `libx11-xcb-dev` and `libxfixes-dev`, which Gio's pkg-config requires via `x11-xcb` and `xfixes` (both ship as separate Debian/Ubuntu packages from `libx11-dev`).

**Follow-up teed up but not taken this session:**

- `//go:build gio` tag on `cmd/giospike/main.go` can now be removed — CI has the deps. Per the session-18 plan this change is meant to land alongside the CI update; keeping them separate this session because the user asked only for the scaffold + CI fix. Remove when picking up next session.
- `cmd/giospike/` can also be deleted entirely per memory `project_sm_ui_toolkit` (spike's job is done). The operator's call — preserved for now as a working reference.

### Outcome (superseded by session 20)

Session 20 picked up from here — see the "Next session" block under the session-20 heading above. Container wiring (config + logging + LiteralProvider) and the first QSO entry row (Callsign / RST Sent / RST Rcvd) landed; Go 1.26 bump and modernize pass came along for the ride.

### Session 18 work (daemon accidental-self-DoS floor + structure.md amendment + logging-app DI decision)

**What landed:**

- **Daemon hardening floor** (driven by the scenario "a user's cron job floods the submit endpoint and knocks the daemon over with 500s"):
  - `internal/config/config.go` — four new `ServerConfig` fields with defaults: `MaxConcurrentRequests=128`, `MaxEventSubscribers=16`, `SubmitRatePerSec=20`, `SubmitRateBurst=40`. Documented in-line with the threat model (accidental self-DoS, not malicious).
  - `internal/api/limits.go` (new, ~110 LOC) — `loadLimiter` with a buffered-channel semaphore (concurrent cap), a mutex-guarded subscriber counter (SSE cap), and a lazy-refill token bucket (submit cap). No background goroutines.
  - `internal/api/middleware.go` — three new methods: `limitConcurrent` (wraps full mux, exempts `/v1/events`, returns 503 `server_busy`), `limitEventSubscribers` (wraps `/v1/events`, 503), `limitSubmitRate` (wraps `POST /v1/qso`, 429 `rate_limited`).
  - `internal/api/server.go` — routes for `POST /v1/qso` and `GET /v1/events` now use `mux.Handle(...)` with per-route wrappers; outer chain is `limitConcurrent(recoverPanic(mux))`.
  - `internal/api/limits_test.go` (new) — 5 tests; edge case caught during implementation: `allowSubmit` must advance `submitLastFill` unconditionally (even on negative elapsed) or subsequent calls see negative elapsed forever. Full suite green.
  - `docs/v2-design/api.md` §6 rewritten to recognize accidental self-DoS as a milestone-1 concern (not just a TCP-exposure concern) and to record the minimal-floor as "implemented" with trigger conditions for the fuller hardening items (TCP binding, non-owner clients, multi-client workload).

- **CI fix:** `cmd/giospike/main.go` now has `//go:build gio`. CI's `go build ./...` skips it (no Gio system deps installed on the runner); local build uses `go build -tags gio ./cmd/giospike/`. When `cmd/logging` (the real Gio app) lands, CI gets a one-line `apt-get install` for Vulkan/Wayland/xkbcommon dev packages and the build-tag gate is removed.

- **`docs/v2-design/structure.md` amended** to reflect the Gio pivot:
  - Decisions #2 and #3 each carry a `> Superseded 2026-04-21 by decision #7` banner (historical rationale preserved — the rule "module boundaries earn their keep via independent build tooling or dependency isolation" still stands; it just no longer applies because we have no Wails apps).
  - New decision #7: "Gio UI toolkit replaces Wails; all apps stay in the root module" — records spike validation, structural consequence (no `go.work`, no `apps/`), CI wrinkle (Linux C build deps).
  - "Deliberately absent from milestone 1" — `apps/logging/…` bullet rewritten as `cmd/logging/…`.
  - "Target layout for milestone 2" — replaced `apps/*/go.mod`-and-`go.work` diagram with single-`go.mod`-extra-`cmd/` diagram.

- **Logging app scaffold started.** Operator has begun work on `cmd/logging`. Not yet reviewed in this session — the user started it in parallel.

**Design decision taken (iocdi in cmd/logging):**

Initial lean was *don't use iocdi* — argued that a Gio app with "config + logging + reader loop" doesn't need a framework. The user corrected the premise by sharing the real service inventory for the logging app: callsign-online lookup (QRZ), prefix/country lookup (Hamnut), enrichment orchestrator, email-out-to-QSL-manager, plus config/logging/smclient/rigloop. That's ~8–10 services with real interdependencies (enrichment depends on hamnut + qrz + config; email depends on config; rigloop depends on config for rig selection) AND the enrichment-never-blocks-logging invariant, which demands each external service declare its graceful-degradation path at startup — exactly iocdi's `Initialize()` phase.

**Decision (2026-04-22):** `cmd/logging` uses iocdi from day one. Reasons:
1. Several services (QRZ, Hamnut, email) are lift-and-shift from v1 and already iocdi-shaped.
2. The enrichment-never-blocks-logging invariant needs a principled "validate / warn / continue" hook per service, which iocdi provides via `Initialize()`.
3. Ordering matters across the graph; iocdi's container enforces what would otherwise be a hand-maintained init order in `main.go` that drifts over time.
4. Consistency with `cmd/smd` — same pattern, same failure modes, reduced cognitive load.

The earlier "CLAUDE.md says build specific" pull still stands but its target was v1's reflection-based adapter framework, not DI. iocdi survived the v2 cull specifically because it solves a real problem — and `cmd/logging` at this service count has the same problem.

### Outcome (superseded by session 19)

Session 19 picked up from here — see the "Next session" block under the session-19 heading above. Empty `cmd/logging` window landed; CI got Gio deps; iocdi service registration still open.

### Session 17 work (Gio UI spike — toolkit decision)

### Session 17 work (Gio UI spike — toolkit decision)

**Goal:** evening-scale throwaway spike to decide whether Gio can carry the v2 logging app, or whether we fall back to Wails.

**What landed:**

- `cmd/giospike/main.go` — ~250-LOC Gio app wired to a live FTdx10. Hard-codes `rigID = "yaesu-ftdx10"` and `portPath = "/dev/ttyUSB0"`. On startup: opens port via `serial.Open` (with inline `serial.Config`-from-`RigSerial` helper, same shape as catcli), starts a reader goroutine, sends `INIT` to enable AI push-state, sends `READ` to seed current VFO/mode/etc. into the UI without waiting for a knob twirl.
- Reader goroutine: `port.ReadResponseBytes` → `cat.Decode` → folds `VFOAFREQ` / `VFOBFREQ` / `MAINMODE` into a `rigState` snapshot → publishes to a buffered channel and calls `w.Invalidate()`.
- Main loop: blocks on `w.Event()`, drains the channel inside `FrameEvent`, renders three readout rows + callsign editor + Log button. Log prints a draft QSO to stdout (no DB, no validation beyond non-empty).
- `gioui.org v0.9.0` added to `go.mod` (plus transitive: `golang.org/x/image`, `github.com/go-text/typesetting`, `golang.org/x/exp/shiny`, `eliasnaur.com/font`, `gioui.org/shader`, `github.com/go-text/typesetting-utils`).

**Linux build deps installed** (system-level, not in go.mod):

- `vulkan-headers`, `vulkan-loader-devel`, `libxkbcommon-x11-devel` (the first build on a fresh machine will need these).
- Wayland / X / xkbcommon / Xcursor / Xfixes devel packages were already present.

**Bugs hit + fixed during the spike:**

1. **First run: no updates in the UI.** Cause: main loop's non-blocking `select` checked the channel once, then `w.Event()` blocked forever because the reader wasn't calling `w.Invalidate()`. Fix: reader now calls `w.Invalidate()` after each channel push, and the main loop drains the channel inside the `FrameEvent` handler.
2. **Second run: channel pushes happening but UI still stale.** Cause: I guessed the tag names (`VFO-A`, `VFO-B`, `MODE`) rather than checking `yaesu-ftdx10.json`. Real tags are `VFOAFREQ`, `VFOBFREQ`, `MAINMODE`. Fix: corrected the keys + added `log.Printf` of every successful decode so the stream is observable.
3. **Third run: updates live but fields empty until a knob is touched.** Cause: only `INIT` (= `AI1;ID;`) was sent; the rig only broadcasts state when something changes. Fix: follow `INIT` with `READ` (= `FA;FB;ST;VS;MD0;MD1;PC;`) to seed the current state.

**Decision:** commit to Gio for the v2 logging app. Operator's verdict after live-rig validation: "we can build a clean UI from this and keep the whole thing with Go." Recorded in memory (`project_sm_ui_toolkit.md`). `cmd/giospike/` stays in the tree as a working reference; it gets deleted when the real logging app lands.

### Session 16 work (CAT/serial data layer + characterization tests)

### Session 16 work (CAT/serial data layer + characterization tests)

**What landed:**

- `internal/serial` brought across from v1, audited, drift-fixed (drift = v1 errors API `.Msg`/`.Err` → v2 `.WithMsg`/`.WithErr`, `types.SerialConfig` moved into the serial package as `serial.Config`, `go.bug.st/serial v1.6.4` added to go.mod, tiny Open() port-leak fix, cmd/catcli SIGINT handler, README+DEV merged into doc.go).
- `internal/cat/rigs/yaesu-ftdx10.json` and `yaesu-ft710.json` authored, lifted from v1's battle-tested `internal/config/defaults.go` on the v1 branch (3 commands INIT/READ/PLAYBACK, 8 states ID/FA/FB/ST/VS/MD0/MD1/PC, v1 tag names VFOAFREQ etc.).
- `internal/cat/rig.go` — types: `RigDefinition`, `RigSerial` (now including RTS/DTR/WriteTimeoutMS per v1), `RigTiming`, `Command`, `State`, `Marker`, `ValueMapping`.
- `internal/cat/rigdb.go` — `//go:embed rigs/*.json` + `Lookup(id)`, `List()`, stubbed `RegisterExternalDir(dir)`.
- `internal/cat/rigdb_test.go`, `reference_test.go`, `decode_fixtures_test.go`, `encode_fixtures_test.go` — 38 subtests green. `reference_test.go` holds frozen v1-faithful mirrors of lookup/decode/encode as the §4 Step 0 acceptance criteria.
- `internal/types/rig.go` — `RigConfig` + `RigOverrides` DTO (stdlib-only, shape per cat-serial-reuse.md §3c).
- `docs/v2-design/cat-serial-reuse.md` continuously updated — §1a blockers resolved, §3 rig-database story, §3c three-type split, §4 Step 0 marked done, §6 decision log extended (incl. v1-provenance entry and FTdx10 manual-verification entry), §7.5 updated, §9 session pickup updated.
- FTdx10 verified against `FTDX10_CAT_OM_ENG_2308-F.pdf` — every command/state/mode-code confirmed. Recorded in §6 decision log.

**FT-710 manual verification (completed at session resume):**

- `ID = 0800 (Fixed)` — FT-710's identity code. Added `{"key": "0800", "value": "FT-710"}` to the `IDENTITY` state in `yaesu-ft710.json`.
- `ST` on FT-710 only supports `0=OFF` / `1=ON` — no `2=ON+` like the FTdx10. Removed the `{"key": "2", "value": "ON+"}` mapping.
- `VS` on FT-710: `P1=0: MAIN Band: VFO-A / SUB Band: VFO-B`, `P1=1: MAIN Band: VFO-B / SUB Band: VFO-A`. Operationally equivalent to FTdx10's "VFO-A/B operation" — kept v1's `VFO-A` / `VFO-B` labels.
- `MD`, `FA`/`FB`, `PC`, `PB`, `AI` — identical to FTdx10 (all 16 mode codes incl. `E=PSK`/`F=DATA-FM-N`, 9-digit Hz range `000030000-075000000`, 3-digit power 005-100, `PB0%s;` template, AI USB-only with power-off reset).
- Fixture tests updated: replaced the old "raw passthrough" FT-710 ID case with `ID0800 → IDENTITY: "FT-710"` and `ID9999 → IDENTITY: ""`; added `ST=2 on FT-710 → SPLIT: ""` to pin the rig-specific difference. 41 subtests green.
- `cat-serial-reuse.md` §6 decision log has the matching FT-710 verification entry.

### §4 Step 1 landed (real codec)

- `internal/cat/codec.go` — `Decode(def, line) (Status, error)` with `ErrNoMatch` sentinel, `Encode(def, name, args...) ([]byte, error)` with `ErrUnknownCommand` sentinel, and unexported `lookupState` helper. Logic byte-for-byte equivalent to `referenceLookup`/`referenceDecode`/`referenceEncode` in `reference_test.go`.
- `decode_fixtures_test.go` / `encode_fixtures_test.go` — swapped `reference*` calls for `cat.Decode` / `cat.Encode`; tests renamed `TestDecode` / `TestEncode`.
- `codec_equivalence_test.go` (new) — runs every fixture through BOTH the real codec and the frozen reference, asserts identical output. Drift detection: catches any divergence between the two even if the fixture table is also updated.
- 76 subtests green total in `internal/cat`.

### `cmd/catcli` relocated + extended for live rig verification

- Moved from `internal/serial/cmd/catcli/` to top-level `cmd/catcli/` (§7.4 closed).
- New `-rig <id>` flag — looks the rig up in `cat.Lookup`, uses its serial defaults, pipes every framed response through `cat.Decode`, prints raw bytes plus the extracted tag map.
- New `-init` flag — sends the rig's `INIT` command via `cat.Encode` at startup (enables AI push-state mode on Yaesu rigs).
- Without `-rig`, behaviour is unchanged from before (pure serial diagnostic, raw bytes).
- End-to-end validation path: `catcli -device /dev/ttyUSB0 -rig yaesu-ftdx10 -init -listen` → live decoded state stream.
- First real wiring of `serial.Port` + `cat.Lookup` + `cat.Decode`/`Encode`. Inline `serial.Config`-from-`RigSerial` conversion (~15 LOC) foreshadows `internal/rigconfig`.

### Live-rig validation landed

Operator plugged in the FTdx10, ran `catcli -device /dev/ttyUSB0 -rig yaesu-ftdx10 -init -listen`, and confirmed end-to-end:

- `INIT` burst sent cleanly.
- `ID0761` received, decoded as `IDENTITY: FTdx10`.
- Live `FA` VFO-A broadcasts tracked as the operator turned the knob, each decoded to `VFOAFREQ: <9 digits>`.
- Mode change decoded: `MD02 → MAINMODE: USB`.
- SIGINT on the listen loop produced a clean `serial.ReadResponseBytes: serial: port closed` error and graceful exit.

The FTdx10 in AI mode broadcasts ~15 prefixes v1 never configured (`IF`, `SS`, `NB`, `RF`, `AC`, `RM`, `RG`, `MG`, `ML`, `GT`, `SH`, `BI`, `KR`, plus a mystery `FD` not in the manual). These surface as `[no match]` in catcli, which is the correct behaviour — v1 ignored them silently; we flag them louder. Decision recorded in cat-serial-reuse.md §6: do NOT pre-broaden the state table; expand only when a specific downstream feature needs a specific prefix. `FD` logged as an open item in §8 for future investigation.

### Outcome (superseded by session 18)

Session 18 picked up from here — see the "Next session" block under the session-18 heading above.

### Session 15 work: bridge design simplified, YAGNI question on the table

Started with the three "next options" from session 14 (alpha checkpoint, second
real forwarder, bridge/CAT design). User reasoned through dependencies:
alpha checkpoint needs a logging client, which needs the bridge → bridge is the
real blocker → picked bridge for this session.

**What landed:**

- `docs/v2-design/bridge.md` created and then substantially rewritten in the
  same session as the design was re-examined.
- Pointer updates: `docs/v2-design/structure.md`, this handoff, and
  `project_sm_serial_bridge` memory now point to the new doc instead of the
  memory-as-canonical + hypothetical `multi-rig.md`.
- NDJSON-over-Unix-socket transport **confirmed decided** (was recorded
  pre-v2 in `design-decisions-log.md` and `invariants.md`; was wrongly hedged
  as "probably SSE" in memory and in the first draft of bridge.md — corrected).

**What the re-examination produced:**

1. **The bridge is much smaller than the 2026-04-14 two-frontend design.**
   Daemon absorbing the QSO-logging concern means port ownership decouples
   from logging, so most of the multiplexing rationale disappears.
2. **No rigctld TCP frontend.** WSJT-X/JTDX own their own rigs' ports
   directly in the v2 architecture — no shared-rig scenario with them.
3. **No PTY virtual serial ports.** Same reason.
4. **The bridge, if built, is SM-internal only** — mediates between
   logging app + future CAT control app on the same rig. Third-party apps
   never touch it.
5. **Correct layering pinned:** `internal/serial` for port I/O (no protocol
   knowledge), `internal/cat` for CAT protocol encoding/decoding (no I/O),
   bridge as glue.
6. **SM apps cooperate on write boundaries**, so the bridge needs no
   per-rig-protocol client-side framing logic (I over-engineered this in
   the first draft; user called it out).
7. **Kenwood is NOT an outlier** — same family as Yaesu (ASCII + `;`);
   only Icom CI-V is binary.
8. **v1 UI lag was almost certainly Wails IPC, not a bridge concern.**
   A Unix socket hop adds <1ms; Wails backend↔frontend JSON adds 10-100x that.

**Open at session end (in `docs/v2-design/bridge.md §6`):**

- **YAGNI: build the bridge now, or defer?** The logging app currently can own
  its rig's port directly via `internal/cat` + `internal/serial` — no bridge
  needed today. A CAT control app is a "strong possibility," not a commitment.
  Deferring costs nothing **if** `internal/cat` is given a pluggable transport
  abstraction from the start (`SerialTransport` today → `SocketTransport` the
  day a second app exists). User leaning toward defer at session end.

### SSE event stream: complete (stages 1–4 landed, docs updated)

`GET /v1/events` serves the firehose of the five settled events
(`qso.stored`, `qso.updated`, `qso.deleted`, `forward.succeeded`,
`forward.failed`). End-to-end proof in
`internal/api/handler_events_e2e_test.go`: a real HTTP client opens
the stream, the logging path commits a QSO via `POST /v1/qso`, the
worker runs and submits to the stub forwarder, and the client
receives both `qso.stored` and `forward.succeeded` frames in
monotonic-ID order.

Shape settled and pinned in `docs/v2-design/api.md §4.5` — wire
format, payload shapes, reconnect semantics, slow-reader policy,
keepalive. The "deferred to implementation" items for SSE in §6
are now closed.

**Stage 1 — `internal/events` hub**:

- Plain `*Hub` (not a DI service type; registered as an instance).
- `NewHub()`, `Publish(name, payload)`, `Subscribe() (<-chan Event, unsub)`,
  `Close()`, `SubscriberCount()` (for test rendezvous).
- 64-event per-subscriber buffer. Publish is non-blocking and
  under a mutex so publish order is preserved; full buffer → the
  hub closes that subscriber's channel and drops it from the map.
- Monotonic event IDs assigned inside the Publish mutex so IDs
  match on-the-wire order.
- 12 unit tests including a race-detector soak for concurrent
  publish + subscribe/unsubscribe.

**Stage 2 — emit wiring + DI injection**:

- `events.ServiceName = "eventhub"`; `cmd/smd/main.go` calls
  `events.NewHub()` and registers via `container.RegisterInstance`
  before `container.Build` so anything with a
  `di.inject:"eventhub"` field gets the same `*Hub`.
- `qsoservice.Service` gained `Hub *events.Hub`; Submit/Update/Delete
  publish `qso.stored` / `qso.updated` / `qso.deleted` AFTER tx
  commit (so a rolled-back write never emits).
- `DeleteQsoByIDTx` now returns `(logbookID, error)` so the delete
  path can emit an accurately-scoped `qso.deleted` without a second
  DB round-trip (sqlboiler `FindQso` was already running).
- `worker.Worker` gained a required `*events.Hub` constructor
  parameter; `markSuccess`/`markFailed` publish
  `forward.succeeded` / `forward.failed` AFTER the DB mark call
  succeeds. `Attempts` is read as `int(row.Attempts) + 1` because
  the DB `Mark*` methods increment internally before the write.
- `api.Server.New` takes the hub (held for stage 3's handler).

**Stage 3 — `GET /v1/events` handler**:

- `internal/api/handler_events.go`. Sets
  `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
  `Connection: keep-alive`, `X-Accel-Buffering: no` (harmless on
  unix sockets, cleans TCP mode behind nginx).
- Disables the per-request write deadline via
  `http.ResponseController.SetWriteDeadline(time.Time{})` — without
  this, Go's `WriteTimeout` would cut idle-but-healthy SSE
  connections every `WriteTimeoutSec`.
- Subscribe → select on `r.Context().Done()` + hub channel +
  30 s keep-alive ticker. Frames are `id: %d\nevent: %s\ndata: %s\n\n`.
  On channel close (hub close or slow-reader eviction) or write
  error: return → defer unsubscribe.
- 7 handler tests + 2 e2e-with-worker tests covering delivery,
  shape, multi-event ordering, client disconnect unsub, hub close,
  slow-reader eviction, keep-alive, insert happy path, terminal
  failure path.

**Stage 4 — e2e tests**: see above; live `httptest.NewServer`
wrapping `srv.httpServer.Handler` so a real HTTP client can stream
frames while the worker goroutine ticks.

**Bonus fix** — the M2 race the review had accepted as theoretical
was surfaced reliably by `-race`: `spawnForwarderWorkers` now does
`wg.Add(1)` synchronously BEFORE `safego.Go`, with an `isRespawn`
closure flag so respawn paths still re-increment after a panic.
Stable under 10 consecutive race-detector runs of
`TestSpawnForwarderWorkers_HappyPath_Single` where it previously
flaked ~40% of the time.

### QRZ port: complete (stages 1–8 all landed)

The 8-stage QRZ port is done. Insert / update / delete are
live-validated against real QRZ; the ADIF upload-status stamp
rides on success; each forwarder owns its own retry defaults;
and the daemon binary's ldflags-injected Version now threads
into `qrz.UserAgent` and `adif.ProgramVersion` at startup.

Station Manager v2 can now push QSOs to QRZ.com end-to-end
through the daemon's forwarding pipeline. The stub forwarder
remains available for plumbing tests.

**Stage 1 — Forwarder interface extension** (session 12, committed):

- `Forwarder.Submit` gained a `priorUpstreamID string` parameter so
  the worker can pass QRZ's LOGID (captured on the earlier successful
  insert's `Result.UpstreamID`) through to the delete call.
- New `AdifPrefix() string` method: declarative metadata telling the
  worker which ADIF upload-status field pair to stamp on the QSO row
  on success (`QRZCOM_QSO_UPLOAD_STATUS` / `QRZCOM_QSO_UPLOAD_DATE`
  for QRZ, `CLUBLOG_*` for ClubLog, `""` for stub / custom webhooks).
  v1 did this stamp from inside the QRZ service; v2 moves it to the
  worker so forwarders stay pure plugins.

**Stage 2 — QRZ package skeleton** (session 13):

- `internal/forwarding/qrz/qrz.go`: `Type = "qrz"`,
  `AdifFieldPrefix = "QRZCOM"`, registry `init()`, `New` with
  credentials validation, stubbed `Submit` that returns Terminal
  until stage 4 lands the real HTTP call.
- `internal/forwarding/qrz/qrz_test.go`: 9 tests covering registry
  round-trip, happy path, malformed/missing credentials, ctx
  cancellation.
- **Credentials shape decided**: `{"api_key": "..."}` — only.
  QRZ enforces the callsign/logbook match server-side (every QSO's
  `STATION_CALLSIGN` must match the logbook's callsign, or QRZ
  rejects the record); keeping a local copy of the callsign would
  only introduce config-drift risk without a correctness guarantee.
  `forwarding.md` §2 updated.

**Stage 3 — Response parser + classifier** (session 13):

- `internal/forwarding/qrz/response.go`: `parseResponse(body)` (pure
  function, `net/url.ParseQuery`-based) and `classifyResponse(act,
  resp)` split into per-action helpers (`classifyInsert`,
  `classifyUpdate`, `classifyDelete`). `AUTH` short-circuits across
  all actions. No substring matching on `REASON` text — QRZ's
  documented per-action RESULT sets are unambiguous.
- `internal/forwarding/qrz/response_test.go`: 26 tests covering the
  full per-action matrix (see `forwarding-implementation.md` §8.1).
- **Key classification refinement**: for `action=delete`, QRZ's
  single-LOGID delete makes `RESULT=FAIL` unambiguously mean "LOGID
  not found". We reclassify it as `OutcomeSuccess` — the record's
  absence upstream matches intent. `RESULT=PARTIAL` on a
  single-LOGID delete is treated as Terminal (shouldn't occur in
  practice).

**Stage 4 — HTTP Submit for insert + update** (session 13):

- `internal/forwarding/qrz/qrz.go`: real `Submit` implementation
  with `buildForm` (insert = `ACTION=INSERT + ADIF`; update =
  `ACTION=INSERT + OPTION=REPLACE + ADIF`) and `classifyHTTPStatus`
  (408/429/5xx → Transient; other non-2xx → Terminal; 2xx falls
  through to body parse). Delete still returns Terminal "deferred
  to stage 5".
- Package-level knobs: `DefaultEndpoint = "https://logbook.qrz.com/api"`,
  `DefaultHTTPTimeout = 30 * time.Second`, `var UserAgent =
  "station-manager/dev"` (to be overridden from `cmd/smd/main.go`
  alongside the blank import in stage 8).
- Package-internal `newWithEndpoint(apiKey, endpoint, client)` —
  tests use it to point at `httptest.NewServer.URL`; production
  code goes through public `New` with the real endpoint.
- `submit_test.go`: 18 httptest-based tests covering transport
  class (network error, ctx cancel, 408/429/500/400/401), body
  class (OK/FAIL/AUTH/REPLACE on insert+update), malformed bodies,
  request-shape assertions (method, KEY, ACTION, OPTION=REPLACE
  on update, ADIF payload, User-Agent), delete-deferred guard,
  unknown-action fallthrough.
- **Live harness** at `internal/forwarding/qrz/live_test.go`
  (`//go:build manual`, gated by `QRZ_TEST_API_KEY` +
  `QRZ_TEST_CALLSIGN` env vars loaded from `.env`):
  - `TestLive_InsertThenUpdate` — quick round-trip with t.Cleanup
    delete; `task test:qrz-live`.
  - `TestLive_InteractiveFlow` — insert → pause → update → pause →
    delete, with `[Enter]` prompts between steps so the operator
    can inspect the record on QRZ.com. `task test:qrz-live-interactive`.
  - **Gotcha recorded**: `go test` feeds the test binary a closed
    stdin, so `bufio.Scanner(os.Stdin)` returns EOF immediately.
    Interactive test opens `/dev/tty` directly to read the
    controlling terminal — Unix-only (Linux/macOS is fine for the
    operator's setup).
- **Live-validated end-to-end**: insert → LOGID returned, update
  with `OPTION=REPLACE` returns the same LOGID (confirming in-place
  update rather than new record), raw delete cleans up. Real QRZ
  response shapes match our parser's assumptions exactly.
- **DB-level verification in the live harness is deferred to
  stage 6** — when `MarkUploadSuccessWithAdifStampWithContext`
  lands, that's the fresh multi-table tx code that earns a
  real-stack check. Today's layered tests (worker + SQLite in-memory
  with stub forwarder; QRZ unit + live with no DB) cover the seam
  transitively.

**Stage 5 — Delete via `Submit` + worker-side LOGID lookup** (session 13):

- `internal/database/sqlite/api_context.go`: new
  `FetchInsertUpstreamIDWithContext(ctx, qsoID, forwarderName)
  (string, error)`. Returns the `upstream_id` from the one
  successful insert row for the (qso, forwarder) pair, `""` when
  no match, non-nil error only for infrastructure failures.
  `ORDER BY modified_at DESC LIMIT 1` is defensive — the schema
  already enforces `UNIQUE(qso_id, forwarder_name, action)` so
  only one row can match in practice. Tests: `service_test.go`
  gains 6 cases covering happy path, scope (forwarder, action,
  status) filtering, the UNIQUE-constraint guard, and input
  validation.
- `internal/forwarding/worker/worker.go`: new
  `resolvePriorUpstreamID` helper called from `processRow`. For
  `action=delete`, consults the DB lookup before `Submit`:
  infra error → transient, empty result → terminal
  ("no upstream id for delete — no successful insert found"),
  non-empty → passed through as `priorUpstreamID`. Worker tests:
  `TestWorker_Delete_NoPriorInsert_IsTerminalImmediately`,
  `TestWorker_Delete_WithPriorInsert_PassesUpstreamID`,
  `TestWorker_InsertAndUpdate_DoNotTriggerLookup`. The existing
  `TestWorker_SoftDeletedQso_DeleteStillForwards` was updated to
  seed a prior successful insert (the pre-stage-5 test would now
  fail on "no upstream id"). Added a `recordingForwarder`
  helper that captures Submit arguments for delete-path tests.
- `internal/forwarding/qrz/qrz.go`: `buildForm` delete branch
  assembles `ACTION=DELETE + LOGIDS=priorUpstreamID`. Empty
  `priorUpstreamID` here is a caller bug (worker should have
  short-circuited) — classified Terminal with clear error, no
  HTTP fired. Tests: replaced `TestSubmit_Delete_DeferredToStage5`
  with 4 new cases (OK/Success, FAIL/idempotent-success,
  EmptyPriorID/Terminal, RequestShape verifying
  `ACTION=DELETE`/`LOGIDS=...`/no ADIF/no OPTION).
- Live harness: `TestLive_InsertThenUpdate` cleanup and
  `TestLive_InteractiveFlow` delete phase both converted from the
  raw HTTP helper to `Submit(..., Delete, logID)`. Dropped
  `liveDelete` helper and its dead imports (`io`, `net/url`,
  `strings`).
- **CI fix (bundled)**: `internal/database/sqlite/internal.go`
  adds `cache=shared` to the DSN options when path is `:memory:`.
  Under `-race` timing in CI, a single pooled connection could
  transiently drop and be replaced with a fresh private
  `:memory:` DB, producing "no such table: qso_upload". Shared
  cache makes all pool connections to the same DSN see the same
  in-memory DB; file-backed paths unchanged.

**Stage 6 — ADIF-stamp wiring** (session 13):

- `internal/database/sqlite/api_context.go`: new
  `MarkUploadSuccessWithAdifStampWithContext(ctx, id, upstreamID, qsoID, adifPrefix)`.
  Single transaction, two writes:
  1. `qso_upload` row transitions (same shape as
     `MarkUploadSuccessWithContext`).
  2. `qso.additional_data` gets `json_set` for
     `$.<prefix>_qso_upload_status = "Y"` (adif.YesString) and
     `$.<prefix>_qso_upload_date = today UTC YYYYMMDD`.
  One-fails-all-fail holds: either both writes land or the tx
  rolls back. Regex validator on adifPrefix (`^[A-Z][A-Z0-9]*$`)
  as defense-in-depth.
- **Schema discovery**: the `qso` table has no per-destination
  ADIF columns — `types.Qso.QrzComUploadDate` /
  `QrzComUploadStatus` ride inside `additional_data`, not as
  columns. json_set on additional_data is the right landing
  place and matches the "additional_data absorbs ADIF spec
  evolution" invariant. No schema migration needed for the
  stamp, and none needed for future forwarders either.
- `internal/forwarding/worker/worker.go`: `markSuccess` now
  dispatches — `fwd.AdifPrefix() != "" && row.Action != Delete`
  → ADIF-stamp variant; else plain variant. Delete never
  stamps (soft-deleted local QSO); prefix-less forwarders
  (stub, custom webhooks) never stamp.
- Tests: sqlite gets 6 new cases (happy path with round-trip
  via `FetchQsoByID`; raw-SQL verification of JSON blob keys;
  prefix-agnostic test using a notional CLUBLOG prefix; invalid
  prefix rejection including injection-style strings; missing
  upload row / missing qso row with rollback verification).
  Worker gets 3 new cases via a local `stampingForwarder` type
  (insert+prefix stamps, delete+prefix doesn't stamp, empty
  prefix doesn't stamp).
- **Generalisability**: adding a new forwarder (ClubLog, LoTW,
  ...) requires zero sqlite/worker/migration changes. The
  forwarder's package returns its `AdifPrefix()`, and future
  `types.Qso` fields (for ADIF export round-trip) get the
  matching JSON tag.

**Stage 7 — Retry-defaults ownership refactor** (session 13):

- `internal/forwarding/registry.go`: new
  `RegisterDefaultRetry(typeName, retry)` + `DefaultRetryFor(typeName)`.
  Companion map alongside the existing constructor registry.
  `Register` panics on empty type / duplicate / nil ctor; the new
  `RegisterDefaultRetry` adds validation parity with
  `worker.New` (panics on MaxAttempts < 1, InitialBackoffSec < 1,
  or MaxBackoffSec < InitialBackoffSec) so an invalid default never
  survives to spawn.
- `internal/forwarding/qrz/qrz.go`: exports
  `DefaultRetry = {5 attempts, 60s initial, 1800s max}`, tuned for
  the QRZ web API + the operator's slow/unreliable link. Registered
  in `init()`.
- `internal/forwarding/stub/stub.go`: exports
  `DefaultRetry = {3 attempts, 1s initial, 5s max}`. Tight values —
  stub is for plumbing verification; tests that want to exercise
  backoff set `Config.Retry` directly.
- `cmd/smd/main.go`: `spawnForwarderWorkers` now resolves retry
  via `forwarding.DefaultRetryFor(fc.Type)` when `fc.Retry` is
  absent. A type with neither a config override nor a registered
  default is a setup error and fails startup loudly with a clear
  message naming both the forwarder instance and the type. The
  package-level `defaultForwarderRetry` fallback is deleted.
- Tests: registry gets 6 new cases (register + lookup, missing
  type, empty-type panic, duplicate panic, invalid-config panic
  for each of the three RetryConfig fields). qrz and stub each
  get an `TestInit_RegistersDefaultRetry` asserting the var is
  exported and registered consistently.
- **Consequence for future forwarders**: ClubLog / LoTW / eQSL
  each ship their own `DefaultRetry` with values appropriate to
  that upstream's quirks (LoTW's batch acknowledgements,
  ClubLog's daily windows, ...). No main.go changes to land a
  new forwarder.

**Stage 8 — Wire-up + docs** (session 13, final):

- `cmd/smd/main.go`: added regular import of
  `internal/forwarding/qrz` (so the init() registers the
  constructor and default retry, AND so main can set
  `qrz.UserAgent`) and blank import of
  `internal/forwarding/stub` (registration only). The
  blank-import style is preserved for forwarders main.go
  doesn't otherwise reference.
- `cmd/smd/main.go`: at the top of `run()`, two package vars
  are now overridden from the ldflags-bound `Version`:
  ```go
  qrz.UserAgent      = "station-manager/" + Version
  adif.ProgramVersion = Version
  ```
  This thread ensures ADIF exports' PROGRAMVERSION header
  and QRZ's User-Agent both reflect the actual binary
  version.
- `internal/adif/consts.go`: `ProgramVersion` flipped from
  `const` to `var` (default `"dev"`) with a doc comment
  explaining the override mechanism; `ProgramID` stays
  `const` (identity marker, not version-dependent).
- Ldflags smoke check:
  `go build -ldflags "-X main.Version=1.2.3-test" ./cmd/smd`
  builds cleanly.

Full suite green under `-race`. v2's forwarding subsystem is
complete end-to-end: ingest → queue → worker → forwarder →
upstream → outcome → qso_upload + ADIF stamp, with live QRZ
validated.

### Forwarding subsystem code review (session 13, end-of-session)

In-depth subagent review of the 8-stage port landed at
`docs/reviews/forwarding-subsystem.md`. Headline: 0 high · 6
medium · 7 low · 5 positives. No correctness bugs, no invariant
violations, no credential leakage.

Triaged and **actioned** in the same commit series:

- **M2** — `spawnForwarderWorkers` now takes a `*sync.WaitGroup`;
  `run()` waits (bounded) for workers to drain after
  `server.Shutdown` before the deferred `dbSvc.Close()` fires.
  Matches the E2E test harness shape; stops the "database is
  closed" log spam on every clean shutdown.
- **M3** — `FetchInsertUpstreamIDWithContext` changed to
  `ORDER BY created_at DESC` so the defensive fallback (if the
  UNIQUE constraint ever relaxes) picks the most-recently-inserted
  row regardless of what retry bookkeeping did to `modified_at`.
- **M4** — Document-only invariant comments at
  `ClaimPendingUploadsWithContext` and `spawnForwarderWorkers`
  pinning "one worker per forwarder_name" and its single
  enforcement point.
- **M5** — Three sections of `forwarding-implementation.md` that
  referenced the deleted `defaultForwarderRetry` rewritten for the
  per-package `DefaultRetry` + `RegisterDefaultRetry` shape.
- **M6** — Added HTML-proxy-body and multi-line `REASON` tests to
  freeze QRZ's real-world failure modes. `cmd/smd/main.go` spawn-path
  coverage landed in session 14 (task #29): `cmd/smd/main_test.go`
  covers 7 `spawnForwarderWorkers` paths + `loadConfig`'s 4 resolution
  modes.
- **L1** — Deleted unused `response.Fields` map.
- **L2** — Parse `action.Parse` once at the top of `processRow`;
  `fetchQsoForAction` now switches on the typed value.
- **L3** — Deleted hand-rolled `itoa`/`containsSubstring`/`indexOf`
  helpers in worker tests; import `strconv` / `strings`.
- **L5** — Multi-byte UTF-8 case (`QRZCOMé`) added to the
  invalid-prefix test slice.
- **L7** — Hardcoded contact callsign in the live harness changed
  from `2E0TEST` to `W1AW/T` (ARRL HQ portable-temporary).

Triaged and **accepted as-is** with rationale pinned in the review
doc's Resolution status section:

- **M1** — Worker wedging a row `in_progress` when a mark-call DB
  write fails. The daemon-restart `ResetOrphanedUploadsWithContext`
  sweep is the documented safety net; the failure requires a
  tx-commit error or sqlboiler Update failure on SQLite — vanishingly
  rare.
- **L4** — `qrz.Forwarder` concurrent-safety docstring is slightly
  imprecise but not misleading.
- **L6** — `bodySnippet` byte-boundary truncation is theoretical
  (QRZ responds in ASCII).

**Task #29** — `cmd/smd/main.go` test coverage (spawn-path +
lifecycle) completed in session 14; see M6 above.

Full suite green under `-race` after every fix; ldflags build
smoke-check passes. Forwarding subsystem is **review-complete**
and ready for the next phase.

### Forwarder subsystem thin-slice complete (session 11)

Design: `docs/v2-design/forwarding.md` is the authoritative shape;
the 11-stage thin slice below implements it end-to-end. All 11
stages landed in session 11. The spine — POST → queue → worker →
forwarder submit → persist outcome → pull endpoint — is covered by
a regression test at `internal/api/handler_e2e_test.go` for the
insert / update / delete actions.

**What's still deferred from milestone 1c** (tracked below as
follow-ups, not thin-slice scope):

- **Real QRZ forwarder implementation.** The stub exercises the
  plumbing; porting v1's `internal/upload/qrz/` into
  `internal/forwarding/qrz/` is milestone-1c work but was not part
  of the 11-stage slice.
- **SSE event stream (`GET /v1/events`).** Terminal transitions
  (`in_progress → uploaded` / `failed`) are the emit sites per
  forwarding.md §7, but the stream itself hasn't been built yet.
  The worker code has comments marking the emit points.
- **Manual re-queue / dead-letter cleanup endpoints** (forwarding.md
  §11). Deferred by design; no design pressure yet.

### Session 11 progress (2026-04-18)

**Design doc landed.** `docs/v2-design/forwarding.md` settles the
internal shape of the forwarder subsystem: constraints, terminology,
fan-out config, `Forwarder` interface, per-destination worker
topology, retry policy, queue-row data shape (§6), lifecycle and
status transitions, `SafeGo` recovery, v1 migration, acceptance.
Walked through the flow step-by-step with the user, which surfaced
several refinements:

- **Ham services are effectively singleton per operator.** One QRZ,
  one ClubLog, one LoTW per operator; `forwarder_name` defaults to
  the type string. The `name`/`type` split exists for rename safety
  (historical rows stay interpretable when an operator relabels a
  destination), not because we expect multi-instance deployments.
  Memory: `project_sm_ham_services_singleton.md`.
- **Retry defaults live in the forwarder package, not config.** Each
  upstream's tolerances are different; `qrz.New` knows what QRZ can
  take. Operators only write a `retry` block in config when they
  need to override.
- **Config reload is off the table.** Restart required for config
  changes. Live reload introduces real complexity (in-flight
  attempts, credential rotation) without matching operator benefit.
- **Slow-link operator-environment defaults** went into the doc:
  `tick_interval_sec=120`, `batch_size=5`, matching v1 operational
  values. Memory: `project_sm_operator_network.md`.

**Implementation plan: 11-stage thin slice**, each stage a
committable unit:

| # | Stage | Status |
|---|-------|--------|
| 1 | Schema update — split `service` into `forwarder_name`+`forwarder_type`, add `next_attempt_at`, `upstream_id` | **done** |
| 2 | Config surface — `ForwarderConfig`/`RetryConfig` in types, `Forwarders[]` on `Config`, defaults + validation, `Forwarders()` accessor | **done** |
| 3 | `internal/forwarding/` — `Forwarder` interface, `Outcome`/`Result`, `Action` alias, init()-time `Register`/`Build` registry | **done** |
| 4 | Stub forwarder — `internal/forwarding/stub/`, modes: `always_success`, `always_transient`, `always_terminal`, `flap_n` | **done** |
| 5 | `safego` helper — landed as `internal/safego/` (not `internal/utils`; cycle avoided), callback-based, ctx-aware cooldown | **done** |
| 6 | DB methods — `ClaimPendingUploadsWithContext` (atomic `UPDATE ... RETURNING`), `MarkUpload{Success,TransientRetry,Failed}WithContext`, `ResetOrphanedUploadsWithContext`, `FetchUploadsByQsoIDWithContext` | **done** |
| 7 | Wire ingest — `submit.go`/`update.go` loops read `config.Forwarders` filtered by enabled + action_filter; new `qsoservice.Delete` atomically soft-deletes + enqueues delete rows | **done** |
| 8 | Worker loop — `internal/forwarding/worker/` per-forwarder tick + claim + submit + persist | **done** |
| 9 | Startup wiring — `main.go` orphan sweep + spawn workers via SafeGo | **done** |
| 10 | Pull endpoint — `GET /v1/qso/:id/uploads` | **done** |
| 11 | E2E integration test — POST → observe row transition to `uploaded` | **done** |

**Stage 1 cleanup (incidentally resolved):**
- `uploadRetryCooldown` + `defaultUploadBatchLimit` constants
  deleted from `sqlite/consts.go` — the M8 `TODO(forwarder)` is
  closed. Retry cadence now lives in per-forwarder config / the
  forwarder package's own defaults.
- `types.RequiredConfigs` + `config.Service.RequiredConfigs()`
  deleted (its one field `QsoForwardingRowLimit` was consumed only
  by the now-deleted legacy worker code; the replacement lives in
  `ForwarderConfig.BatchSize`).
- Legacy v1 worker methods (`InsertQsoUploadWithContext`,
  `FetchPendingUploadsWithContext`, `UpdateQsoUploadStatusWithContext`
  and their non-ctx wrappers) deleted from the sqlite package;
  their three tests likewise removed. Stage 6 added the new
  purpose-built replacements.

**Stage 4 — stub forwarder.** `internal/forwarding/stub/` implements
`Forwarder` with four modes (`always_success`, `always_transient`,
`always_terminal`, `flap_n`) selected via the credentials blob. Ctx
cancellation short-circuits before the call counter bumps so tests
can assert on "how many real submits happened" cleanly. Registers
under type `"stub"` via `init()`; 11 tests covering validation,
each mode, flap transition, ctx-cancel, and round-trip via
`forwarding.Build`.

**Stage 5 — `internal/safego/`.** Deviation from the draft doc:
lives in its own package, not `internal/utils`. Cause: `logging`
already imports `utils`, so putting `*logging.Service` in utils
would create a cycle. The landed shape takes a `PanicHandler`
callback instead of a concrete logger — zero dependency on logging,
callers wire the log format. Signature also gained a `ctx` parameter
so the cooldown sleep is interrupted by shutdown rather than
spawning a final respawn that immediately exits. Cooldown is an
`atomic.Int64` (nanoseconds) after the race detector caught a real
race between `t.Cleanup` and still-running goroutines. `docs/v2-
design/forwarding.md §9` rewritten to match as-implemented shape.

**Stage 6 — upload-queue DB surface.** Six methods, all worker-
facing. `ClaimPendingUploadsWithContext` is the atomic
`UPDATE ... RETURNING *` from the design doc, scoped to a single
forwarder so two workers never compete. `modified_at` is driven by
`trg_qso_upload_set_updated_at` so the mark/claim statements don't
touch it manually; SQLite's default `recursive_triggers=off` prevents
the trigger's own UPDATE from re-firing. Empty `upstream_id` is
stored as NULL rather than the empty string. New
`QsoUploadModelToType` adapter flattens nullable columns for
callers that don't care about null-vs-value. 13 integration tests
cover claim ordering, forwarder scoping, future-`next_attempt_at`
gating, each mark method, orphan sweep, and pull-endpoint fetch.

**Stage 6b — sqlboiler refactor (post-review).** User flagged that
four of the Stage-6 methods were using raw SQL where sqlboiler's
type-safe builders would do. Refactored `MarkUploadSuccess`,
`MarkUploadTransientRetry`, `MarkUploadFailed` to the load-then-save
pattern (`FindQsoUpload` → mutate fields → `Update(ctx, h, boil.Infer())`);
refactored `ResetOrphanedUploads` to `models.QsoUploads(...).UpdateAll(...)`.
`ClaimPendingUploadsWithContext` kept as raw with an expanded doc
comment naming the two sqlboiler limitations that justify the
exception (`UPDATE ... RETURNING *`, `WHERE id IN (SELECT ... LIMIT N)`
subquery-same-table). Bonus: Mark* now correctly surface
`errors.ErrNotFound` for nonexistent row IDs — the raw version was
silently no-oping, a latent bug. Preference saved as
`feedback_sqlboiler_default.md` memory.

**Stage 7 — ingest → forwarders wired.** `qsoservice.Service` gains
a `Config *config.Service` DI field. New
`internal/qsoservice/forwarders.go` helper
(`shouldEnqueue(fc, action) bool`) centralises the enabled-and-
action-filter check for all three ingest sites. `submit.go` loop
swaps the stub for `s.Config.Forwarders()`; `update.go` activates
its commented hook with the same pattern. New
`internal/qsoservice/delete.go` introduces the first domain-level
`Delete(ctx, id)` that atomically soft-deletes the QSO and enqueues
`delete`-action queue rows under one tx (one-fails-all-fail). DB
layer gains `DeleteQsoByIDTx(ctx, tx, id)` for the tx-scoped
soft-delete; the old `DeleteQsoByIDWithContext` is deleted (its one
caller, `handleDeleteQso`, now goes through `qsoservice.Delete`).
`testServer` split into `testServerWithCfg(t, mutate)` so tests can
populate `cfg.Forwarders` before construction. 6 new HTTP-level
tests verify enabled→row-inserted, disabled→skipped, action-filter
exclusion, update-path enqueue, delete-path enqueue + soft-delete,
and delete-with-zero-forwarders.

**Stage 8 — worker loop.** `internal/forwarding/worker/` lands the
per-destination goroutine the design calls for. `Worker` holds a
resolved `Config` (name, tick, batch, retry) plus references to
`*sqlite.Service`, `*logging.Service`, and a `forwarding.Forwarder`.
`Run(ctx)` runs an initial tick then selects on a `time.Ticker`
until ctx cancels; each tick calls `ClaimPendingUploadsWithContext`
for its forwarder_name and iterates rows, calling the forwarder's
`Submit` and persisting the outcome via `MarkUpload{Success,
TransientRetry,Failed}`. Soft-delete handling per forwarding.md §4
is implemented: `insert`/`update` with a soft-deleted QSO marks the
row failed; `delete` falls back to
`FetchQsoByIDIncludingDeletedWithContext` so the upstream still gets
told. Backoff (`backoff.go`) implements §5's exponential +20% jitter
with an overflow cap at `maxBackoffShift=30`. 16 tests across
`worker_test.go` (positive outcomes via real sqlite + stub
forwarder) and `backoff_test.go` (pure-function bounds). Test
helpers `runUntil(t, w, h, qsoID, match)` and `runFor(t, w, d)`
replace an earlier fixed-sleep `runOnce` shape that flaked under
`-race` load; the polling approach is deterministic regardless of
scheduler latency. New sqlite method
`FetchQsoByIDIncludingDeletedWithContext` uses `models.NewQuery` +
`qm` mods — sqlboiler's re-exported query builder — to sidestep the
auto-filter on `deleted_at IS NULL` that `FindQso` and
`models.Qsos(...)` bake in; column/table references still come from
generated constants. Memory `feedback_sqlboiler_default.md`
expanded with the `models.NewQuery` idiom so future sessions reach
for it before `queries.Raw`.

**Stage 9 — startup wiring in `cmd/smd/main.go`.** Blank import
`_ "internal/forwarding/stub"` registers the stub type. After
migrations run, `ResetOrphanedUploadsWithContext` sweeps any
`in_progress` rows back to `pending` with a 10s context; log line
fires only when the count is non-zero. A `workerCtx/workerCancel`
pair is constructed before the HTTP server starts, so workers live
exactly for the daemon's lifetime. A new
`spawnForwarderWorkers(ctx, fwds, db, logger) error` helper loops
`cfg.Forwarders`, skips disabled entries, builds each forwarder via
`forwarding.Build`, resolves retry (per-entry override or the
package-level `defaultForwarderRetry = {5, 60, 3600}` fallback, a
temporary stand-in until real forwarders supply their own per
forwarding.md §2), constructs a `worker.Worker`, and launches it
under `safego.Go` with `respawn=true`. Panic handler logs
structured fields (`goroutine`, `panic`, `stack`) through the
daemon logger. Shutdown ordering: `workerCancel()` fires **before**
`server.Shutdown(ctx)` so in-flight forwarder HTTP calls abort
promptly and workers stop starting new DB ops against the
about-to-close handle.

**Stage 10 — pull endpoint `GET /v1/qso/:id/uploads`.**
`internal/api/handler_uploads.go` implements a thin handler: parse
id (400 on bad), existence-probe via
`FetchQsoByIDIncludingDeletedWithContext` (404 only for
genuinely-unknown ids; soft-deleted QSOs still return their rows
because the delete-action forwarding work remains observable),
fetch via `FetchUploadsByQsoIDWithContext`, normalise nil→empty
slice, return `{"items": [...]}`. Route wired in `server.go`. Five
handler tests cover: two-forwarder happy path with stable
`(forwarder_name, action)` ordering, no-forwarders → literal
`"items":[]` (not `null`), soft-deleted QSO → still returns rows,
unknown id → 404 `not_found`, invalid id → 400 `invalid_id`.

**Stage 11 — end-to-end acceptance test.**
`internal/api/handler_e2e_test.go`, three scenarios, all using the
existing `testServerWithCfg` harness plus real `worker.Worker`
goroutines (plain `go` + `sync.WaitGroup` for deterministic
shutdown — `safego`'s respawn path is tested in its own package):
`TestE2E_InsertReachesUploaded` (POST, both upload rows reach
`uploaded`, `attempts=1`, `upstream_id=stub-ok`),
`TestE2E_UpdateReachesUploaded` (POST → settle → PATCH → wait for
update row to upload), `TestE2E_DeleteReachesUploaded` (POST →
settle → DELETE → wait for delete row to upload, asserts canonical
`GET /v1/qso/{id}` now 404s while the uploads endpoint still shows
the rows). Helpers: `startE2E(t, fwds...)` spawns workers with a
50ms tick and returns a shutdown closure registered as
`t.Cleanup` backstop; `waitForUploads(t, srv, qsoID, match)` polls
at 25 ms with a 3 s deadline, logs the last observed rows on
timeout.

### Current state (as of 2026-04-17 end-of-session 10)

### Milestones 1 and 1b both complete, full code review landed

Milestone 1 (submit a QSO) and milestone 1b (QSO CRUD, logbook
management, list, contest-dupe, contact history, version) are both
complete and CI-green under `-race`. The daemon now exposes the
full set of endpoints the logging-app and logbook-app need for
milestone 2+.

**Session 10 focus was hardening, not new features.** A full
independent code review (`docs/reviews/milestone-1b-review.md`)
surfaced 23 findings across high/medium/low severity; every one
has been addressed. The codebase is now in a "clean slate for
forwarder" state — no known bugs, no convention drift, no dead
code outstanding.

### Session 10 headline changes

- **H1 — concurrent-submit race plugged.** Pre-transaction dedupe
  check + UNIQUE-constraint catch-and-reclassify; deterministic
  test (`TestSubmitQso_ConcurrentDuplicate`) locks it in.
- **ADIF export moves entirely client-side.** `POST /v1/logbook/{id}/export`
  is dropped from the roadmap; clients that need ADIF use
  `internal/adif` as a library. Backup story is forwarding to
  online services, not file dumps. See
  `memory/project_sm_session_scope.md`.
- **SQL call-site audit items 1–2 landed.** New
  `LogbookCallsignByIDWithContext` on the submit hot path; new
  composite partial index `idx_qso_logbook_date_time` for cursor
  pagination.
- **M6 proactive fix for one-fails-all-fail.** `qsoservice.Update`
  is now transactional with a commented forwarder hook inside the
  tx envelope, so the forwarder PR just drops the
  `InsertQsoUploadTx(action.Update)` loop into the existing slot.
- **Daemon lifecycle is defer-based.** `cmd/smd/main.go` delegates
  to `run() error` with defers for logger + db cleanup; `fatal()`
  is gone. Failures at any startup step unwind cleanly through
  registered defers.
- **Dead code swept.** `qsoservice.FreqMHzToKHzString`,
  `sqlite.Service.ExecContext`, `sqlite.Service.QueryContext`,
  `fatal()`, and the unused-error-return in `adif.parseRecords` —
  all deleted. No functional change; less noise.
- **Convention sweep.** All 9 residual `fmt.Errorf` call sites in
  the sqlite package converted to `errors.New(op).WithErr(err).WithMsg(...)`.
  Four handler tests moved off English-message substring matching
  onto structured decode. Eight `fmt.Sscanf` sites converted to
  `unmarshalJSON`. Contact-history LIKE pattern anchored on slash
  (`X/%`) so coincidental prefixes no longer match.
- **Panic handling added (post-review).** `main()` has a
  `defer recover()` with `ExitError`/`ExitPanic` exit-code
  constants so a supervisor can tell a panic from a graceful
  error exit. `api.recoverPanic` middleware wraps the mux — any
  handler panic is structurally logged and returns a generic 500
  envelope (no panic-value leak). Worker-goroutine recovery is a
  noted follow-up for when the forwarder lands.
- **`goccy/go-json` dep dropped (post-review).** Adapters now use
  stdlib `encoding/json`; go.mod / go.sum cleaned. Consistency
  restored — one JSON library, fewer external deps.

Commits covering session 10 are in the `main` branch; the review
doc has a resolution note pointing at them.

### Milestone 1b progress

| Step | Scope | Status |
|------|-------|--------|
| 1. Logbook CRUD | `GET/POST/PATCH/DELETE /v1/logbook` | **done** (session 8) |
| 2. QSO fetch/edit/delete | `GET/PATCH/DELETE /v1/qso/:id` | **done** (session 9) |
| 3. QSO list with pagination | `GET /v1/logbook/:id/qso` | **done** (session 9) |
| 4. Contest dupe check | `GET /v1/contest-dupe` | **done** (session 9) |
| 5. Contact history | `GET /v1/contact-history` | **done** (session 9) |
| 6. Version | `GET /v1/version` | **done** (session 9) |

### FREQ added to dedupe-key inputs (session 9)

The dedupe-key hash was expanded from
`CALL|BAND|MODE|QSO_DATE|TIME_ON` to
`CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON`. Aligns with ADIF-spec
guidance on QSO identity and distinguishes same-call/same-time
contacts on different frequencies (net ops, split, frequency
hopping). FREQ is the normalized integer-kHz string so "14.074" /
"14074" / "14.0740" all hash to the same key.

No schema change — `dedupe_key` is just a hash column. No migration
needed pre-1.0.

### PATCH design decisions (session 9)

- **Immutable fields:** `id`, `logbook_id`, `station_callsign`,
  `dedupe_key`, forwarding state (`sm_*`, `qrzcom_*`), enrichment
  (`country_details`, `contact_history`). Always restored from the
  existing row after `json.Unmarshal` overlay. Clients cannot rewrite
  them via PATCH even if they include those keys in the body.
- **Dedupe-key recompute:** if any of CALL/BAND/MODE/FREQ/QSO_DATE/
  TIME_ON change, the key is recomputed. A new key that collides with
  another QSO in the same logbook returns 409 `duplicate_key`. No
  `force=true` bypass on edit — edit is never allowed to create a
  duplicate.
- **No parallel patch struct.** PATCH accepts a JSON body matching
  the canonical `types.Qso` shape. `json.Unmarshal` overlays present
  keys onto a copy of the existing QSO; missing keys leave fields
  alone. Adding an ADIF field to `types.Qso` automatically becomes
  editable via PATCH with no second change.

### DELETE is soft-delete only (session 9)

`DELETE /v1/qso/:id` flips `deleted_at`. The daemon's job is "log +
forward"; any hard-delete / purge tooling is a logbook-app concern.
Soft-deleted rows are hidden from `FindQso` (sqlboiler's generated
WHERE clause filters `deleted_at IS NULL`), so subsequent GETs
return 404. The partial unique index on `dedupe_key` is scoped
`WHERE deleted_at IS NULL`, so soft-deleting a QSO frees its dedupe
key — the same (call, band, mode, freq, date, time) can be re-logged
after deletion.

### FREQ is MHz on the external surface (session 9)

`types.Qso.Freq` was storing the integer-kHz string, leaking a
storage unit out through the HTTP API and the "QSO stored" log line.
Fixed: `types.Qso.Freq` is the ADIF-native MHz decimal string
(e.g. `"14.074"`) everywhere above the adapter; the sqlite adapter
is the only place that knows about integer-kHz storage.

- `utils.ParseFreqMHz(string) (int64, error)` and
  `utils.FormatFreqMHz(int64) string` are the kHz↔MHz bridge,
  co-located with the other freq helpers.
- The old `qsoservice.FreqMHzToKHzString` helper was removed.
- The sqlite `freq` column is still INTEGER kHz (per v2-design
  decision: SQLite likes integers for sortable/indexable storage;
  translation happens in the daemon).
- Dedupe-key hash still uses the int-kHz string internally for
  deterministic numeric normalization ("7.050" / "7.0500" / "7050"
  all collapse to the same integer).

### Cursor-based QSO list pagination (session 9)

`GET /v1/logbook/{id}/qso?after=<cursor>&limit=<N>` per api.md §4.4.
Forward-only, DESC sort on `(qso_date, time_on, id)` — newest first.
Cursor is opaque base64url-encoded JSON `{"d","t","i"}`. Response
shape: `{"items": [...], "next_cursor": null | "<token>"}`.

- `ServerConfig.DefaultPageLimit` (50) and `ServerConfig.MaxPageLimit`
  (500) added. Clients that omit `?limit` get the default; values
  above max are silently clamped; non-positive values are 400.
- Soft-deleted rows are hidden (sqlboiler default).
- Opt-in "show deleted" is deferred — logbook-app concern per the
  narrow-daemon invariant. When the logbook-app is built we'll add
  `?include_deleted=true` symmetrically across GET/LIST.

### Contest-dupe endpoint (session 9)

`GET /v1/contest-dupe?logbook=<id>&call=<callsign>&band=<band>&mode=<mode>`
returns `{"duplicate": bool}`. Mode is optional: omit for band-only
contests (ARRL DX), include for band+mode contests (CQ WW).

- Narrow purpose-built endpoint rather than filters on the list
  endpoint — contest operators hit this path hard and the answer
  is a single boolean.
- **Contest isolation via logbook-per-contest, not separate DB
  file.** The logbook abstraction is designed for exactly this
  partitioning; contest-dupe queries are `WHERE logbook_id = ?` so
  they're inherently scoped to the contest's logbook with no
  cross-contamination. See the `project_sm_session_scope.md` memory
  for the related "logging session stays client-side" decision.
- `IsContestDuplicateByLogbookIDWithContext` widened to take an
  optional `mode` argument.

### QSO submit path tightened (session 8)

The submit endpoint now requires `?logbook=<id>` and validates:
- The logbook exists (404 if not)
- The logbook's callsign matches STATION_CALLSIGN (400
  `callsign_mismatch` if not)
- Auto-create logic removed — logbooks must be created explicitly
  via `POST /v1/logbook` before QSOs can be submitted

### Code style decisions (session 8)

- **`errors.Op` convention standardised:** all ops use
  `const op errors.Op = "package.FuncName"` pattern. Handler ops
  changed from URL-path-style (`api.v1.qso.submit`) to function-name
  style (`api.handleSubmitQso`).
- **`writeError` uses `errors.Op`** not plain string for the op
  parameter and the `ErrorResponse.Op` field.
- **No `fmt.Errorf` in packages that use `internal/errors`** — the
  `errors.New(op).WithErr(err).WithMsg(...)` pattern is the standard
  for all error paths.

### Listener protocol is config-driven

`ServerConfig.Protocol` (default `"unix"`, alternative `"tcp"`)
controls `net.Listen`. The stale-socket cleanup only runs for Unix
protocol. This keeps the door open for network deployment (daemon on
a Pi) without any code changes — just a config change.

### Dev environment

- `build/config.json` — dev config with debug logging
- `build/db/` — sqlite database
- `build/log/` — daemon log files
- `task run` — builds and runs the daemon using `SM_WORKING_DIR`
  from `.env`
- `task build` — compiles all packages + daemon binary
- `.github/workflows/ci.yml` — CI passes cleanly on GitHub

### Repo state

**Branches:**
- `main` — milestone 1b complete, session-10 hardening landed. CI green.
- `v1` at `0e158ec` — unchanged since session 2.

---

## What happened in session 10 (2026-04-17)

### Goals set for the session

- Finish the SQL call-site audit items 1–2 (started late session 9).
- Do a full pre-forwarder code review to catch drift/bugs before
  the much larger forwarder subsystem lands.
- Address everything the review surfaces.

### What got done

1. **SQL audit wins (items 1–2 of the session-9 list).**
   - Added `LogbookCallsignByIDWithContext` — `SELECT callsign …`,
     skips full-row scan + adapter; submit hot path now uses it.
   - Added composite partial index
     `idx_qso_logbook_date_time ON qso (logbook_id, qso_date DESC,
     time_on DESC, id DESC) WHERE deleted_at IS NULL` for cursor
     pagination. `EXPLAIN QUERY PLAN` confirms the planner seeks
     directly on the index with no temp B-tree for ORDER BY.
   - Added directly to `0001_init.up.sql` rather than a new
     migration file — pre-1.0, no data to migrate.

2. **Export-endpoint reversal.** Previously-nominated
   `POST /v1/logbook/{id}/export` dropped from the roadmap.
   Rationale in the session-scope memory and in `api.md` §5.
   ADIF is a client/admin concern; daemon's backup story is
   forwarding to online services.

3. **Full milestone-1b review** (`docs/reviews/milestone-1b-review.md`).
   Independent agent pass with CLAUDE.md + memory files as context.
   23 findings: 2 high, 9 medium, 12 low. All addressed in the
   same session:

   **High:**
   - **H1**: concurrent-submit race (two workers passing the pre-tx
     dedupe check, second losing on UNIQUE index, surfacing as 500
     instead of 200-duplicate). Fixed with constraint-error catch
     and re-query. Deterministic regression test added.
   - **H2**: dead `qsoservice.FreqMHzToKHzString` still in tree
     despite session-9 handoff claiming removal. Deleted. `math`
     import dropped with it.

   **Medium:**
   - **M1 + M2**: shared `readBody` / `readJSONBody` helpers on
     `*Server`; logbook POST/PATCH now honour `MaxBodyBytes`;
     `*http.MaxBytesError` detected via `errors.As` instead of
     stdlib string match.
   - **M3**: SQL schema comment and `types.Qso.DedupeKey` docstring
     now name FREQ in the dedupe-key list.
   - **M4**: `sqlite.Service.Close` resets `initOnce` +
     `isInitialized` so re-init cycles work. Cycle test added.
   - **M5**: dead `ExecContext` + `QueryContext` deleted (also
     eliminates the context-cancel leak).
   - **M6**: `qsoservice.Update` is now transactional, mirroring
     Submit's shape. Commented forwarder hook in place for the
     future `InsertQsoUploadTx(action.Update)` loop.
   - **M7**: all 9 residual `fmt.Errorf` sites in the sqlite
     package converted to the `errors.New(op).WithErr(err).WithMsg(...)`
     pattern. Two `fmt` imports dropped.
   - **M8**: `uploadRetryCooldown` annotated with a `TODO(forwarder)`
     pointer naming the expected config shape
     (`ForwarderConfig.RetryCooldownSec`). Deferred to the
     forwarder PR by design.
   - **M9**: four handler tests moved off English-message substring
     matching onto structured decode (`ErrorResponse`/`types.Logbook`).

   **Low (all 12 addressed):**
   - **L1**: `writeJSON`/`writeError` are now methods on `*Server`
     with encode-error logging; 81 call sites converted.
   - **L2**: `Server.Shutdown` removes the Unix socket file
     (best-effort, gated on `s.protocol == "unix"`).
   - **L3**: "smd stopped" log moved above `loggerSvc.Close()`.
   - **L4**: `main` now delegates to `run() error` with
     defer-based cleanup; `fatal()` deleted.
   - **L5**: `LIMIT 1` in `SchemaVersionWithContext` annotated as
     defensive.
   - **L6 (broadened)**: contact-history LIKE pattern changed
     from `X%` to `X/%` — anchors on slash, matches portable
     variants (M0CMC/P) but excludes coincidental prefixes
     (M0CMCE). Two new regression tests.
   - **L7**: `missingCoreTables` checks `rows.Err()` after the
     `for rows.Next()` loop.
   - **L8**: `validTestQso` uses canonical MHz `"7.050"` instead
     of legacy kHz `"7050"`.
   - **L9**: sqlite `doc.go` lifecycle description corrected —
     Migrate is a distinct call, Close resets init guard.
   - **L10**: `config_test.go` now asserts `DefaultPageLimit=50`,
     `MaxPageLimit=500`, `MaxContactHistoryResults=100`.
   - **L11**: 8 `fmt.Sscanf` JSON-substring-matching sites
     converted to `unmarshalJSON` + structured decode.
   - **L12**: `adif.parseRecords` error return dropped (dead
     path; caller check collapsed).

4. **Panic handling added** (post-review, user-initiated).
   - `cmd/smd/main.go`: `ExitError` / `ExitPanic` constants (ExitOK
     is implicit — Go's default on clean return). `main()` wraps
     `run()` with a `defer recover()` that prints a `PANIC:`-prefixed
     stderr line + `debug.Stack()` and exits `ExitPanic`. `run()`'s
     own defers (logger close, dbSvc close) still fire first as the
     panic unwinds through its frame.
   - `internal/api/middleware.go`: new `recoverPanic` middleware on
     `*Server`. Wraps the mux so any panic in a handler logs through
     `logging.Service` with panic value + stack + method + path, then
     writes a generic 500 `internal_error` envelope. The panic value
     is deliberately NOT surfaced to the client (could leak
     internals; full detail stays in the log).
   - Two regression tests (`TestRecoverPanic_CatchesAndReturns500`,
     `TestRecoverPanic_NoPanicPassesThrough`) — including a canary
     assertion that the panic message doesn't bleed into the
     response body.
   - Worker-goroutine recovery (`safeGo` helper) intentionally
     deferred until the forwarder PR spawns its first worker — the
     pattern template is noted here so the forwarder author can
     copy it from `recoverPanic`.

5. **`goccy/go-json` dropped from the dependency tree** (user pref).
   - Two adapter files (`internal/database/sqlite/adapters/model_to_type.go`
     and `type_to_model.go`) switched from `github.com/goccy/go-json`
     to stdlib `encoding/json`. Drop-in — `Marshal` / `Unmarshal`
     signatures are identical. `go mod tidy` removed the dependency
     from both `go.mod` and `go.sum`.
   - Rationale: at this daemon's scale (~146 QSO/s per stress test)
     the performance delta is below the noise floor; stdlib preference
     per CLAUDE.md; one fewer external dep to carry. The adapter's
     prior use of goccy was inherited from sqlboiler-generated
     idioms, not a deliberate choice.

### Coverage summary end-of-session

All tests green under `-race` after every finding. One new test
family:
- `TestSubmitQso_ConcurrentDuplicate` — H1 regression.
- `TestService_InitOpenCloseInitOpen` — M4 cycle regression.
- `TestCreateLogbook_BodyTooLarge`, `TestUpdateLogbook_BodyTooLarge`,
  `TestCreateLogbook_InvalidJSON` — M1 regressions.
- `TestLogbookCallsignByID` — new sqlite helper.
- `TestContactHistory_PortableSuffixMatches`,
  `TestContactHistory_CoincidentalPrefixExcluded` — L6 regressions.
- `TestRecoverPanic_CatchesAndReturns500`,
  `TestRecoverPanic_NoPanicPassesThrough` — panic-handling
  middleware (post-review).

### Design decisions made / reaffirmed

- **No daemon-side ADIF export endpoint.** Captured in
  `memory/project_sm_session_scope.md` as explicit "do not propose."
- **`qsoservice.Update` shape matches Submit** (tx envelope).
  Forwarder-hook placeholder inside the tx makes the later
  extension mechanical.
- **MaxBodyBytes is enforced on every handler that reads a body.**
  `readBody` / `readJSONBody` are the single enforcement point.
- **Contact-history prefix match is portable-only** (`X` OR
  `X/suffix`). The looser `LIKE X%` shape is gone.
- **`cmd/smd/main.go` follows the `run() error` pattern.**
  Cleanups are defers; startup failures unwind them in LIFO order.
- **Panic handling is two-layered.** Top-level `main` defer catches
  anything that escapes `run()` and exits with `ExitPanic` (2) so
  process supervisors can distinguish it from startup errors
  (`ExitError`, 1). A `recoverPanic` middleware on the HTTP mux
  catches handler panics, logs them structurally, and returns a
  generic 500 envelope (panic value stays server-side).
- **`encoding/json` is the only JSON library.** Dropped
  `goccy/go-json`. At this scale stdlib is fine and the "minimise
  external deps" rule wins over marginal throughput gains.

### Parked follow-ups

- SQL audit item 3 — dead-method sweep of the six sqlite methods
  with only test callers (`FetchContactedStationByCallsign`,
  `FetchCountryByCallsign`, `FetchCountryByName`,
  `FetchPendingUploads`, `UpdateQsoUploadStatus`,
  `FetchQsoSliceByLogbookId`, `FetchQsoCountByLogbookId`). The
  last two forwarder-queue methods will get real callers when the
  forwarder lands. The enrichment ones will get real callers in
  milestone 2.
- SQL audit item 4 — optional `(call, logbook_id) WHERE
  deleted_at IS NULL` composite for contact-history queries under
  `?logbook=` filter. Flagged under "wait for a concrete problem."

---

## What happened in session 9 (2026-04-17)

### Goals set for the session

- Implement milestone 1b step 2 (QSO fetch/edit/delete)
- Extend the stress test to exercise new read/edit/delete paths

### What got done

1. **`GET /v1/qso/{id}`** — `handleGetQso` in
   `internal/api/handler_qso.go`. Parses `{id}`, calls
   `FetchQsoByIdWithContext`, maps `ErrNotFound` → 404. Soft-deleted
   rows return 404 because `FindQso` already filters
   `deleted_at IS NULL`.

2. **`PATCH /v1/qso/{id}`** — `handleUpdateQso` + `qsoservice.Update`
   in `internal/qsoservice/update.go`. First iteration built a
   `QsoPatch` struct with pointer fields per editable attribute; was
   torn out and rebuilt to use `types.Qso` directly via
   `json.Unmarshal` overlay + stash-restore of immutables. The
   rewrite prevents drift with `types.Qso` and honors the "adding an
   ADIF field is a one-line change" invariant. Validation errors
   come back as `*SubmitError`; collision → `duplicate_key` → 409.
   No `force=true` bypass.

3. **`DELETE /v1/qso/{id}`** — `handleDeleteQso` + sqlite
   `DeleteQsoByIDWithContext`. Soft-delete via sqlboiler's
   `qso.Delete(ctx, h, false)`. Returns 404 if the QSO doesn't exist
   or is already soft-deleted. No FK check — QSO is the child. Test
   `TestDeleteQso_FreesDedupeKey` locks in the behavior that
   soft-deletion frees the dedupe-key slot (thanks to the partial
   unique index `WHERE deleted_at IS NULL`).

4. **FREQ added to dedupe-key inputs** —
   `ComputeDedupeKey(call, band, mode, freq, qsoDate, timeOn)`.
   Tests, call sites (submit + update), and the `dedupeChanged`
   check in `Update` all updated in lockstep. See "FREQ added to
   dedupe-key inputs" above for the rationale.

5. **`IsSubmitError` uses `errors.As`** instead of a direct type
   assertion. Future-proofs against anyone wrapping a `*SubmitError`
   with `%w` or the `internal/errors` builder.

6. **Stress test expanded** — `TestStress_20Clients_50QSOs` now runs
   submit → fetch (verify call) → PATCH(freq) (verify dedupe-key
   recomputed) → DELETE (verify 204, verify subsequent GET 404) per
   QSO. 1000 QSOs, zero errors across all four operations, race
   detector clean. End-to-end round trip ~17–18 ms.

7. **Types-package audit** — searched for exported types outside
   `internal/types` that cross package boundaries. Concluded (with
   user agreement) that types move into `internal/types` only when
   there is actual cyclic-dependency risk, not prophylactically. No
   migrations made; `adif.Record`, `qsoservice.SubmitResult`,
   `qsoservice.SubmitError` stay in their own packages.

8. **FREQ leak fix — MHz is the canonical external form.**
   `types.Qso.Freq` was holding the integer-kHz string, so HTTP
   responses and log lines returned `"14074"` instead of `"14.074"` —
   violating the ADIF-follows-spec invariant. Fix: added
   `utils.ParseFreqMHz` / `utils.FormatFreqMHz`, moved the
   MHz↔kHz boundary into the sqlite adapter, made `types.Qso.Freq`
   canonical MHz everywhere above the adapter. DB column stays
   INTEGER kHz (user decision: SQLite prefers integers for sortable
   storage). `qsoservice.FreqMHzToKHzString` removed; dedupe-key
   hash still uses the int-kHz string internally for determinism.
   Adapter tests had nonsense `Freq: 14250000` values (14.25 GHz
   in kHz) — fixed to realistic `14250` / `"14.250"` in the process.

9. **`IsContestDuplicateByLogbookIDWithContext` widened** to take
   an optional `mode` argument for band+mode contests, in
   preparation for the contest-dupe endpoint.

10. **`GET /v1/logbook/{id}/qso`** — forward-cursor pagination.
    New sqlite method `FetchQsoPageByLogbookWithContext` uses a
    tuple predicate on `(qso_date, time_on, id)` DESC and fetches
    `limit+1` to detect "has more" cheaply. Handler encodes/decodes
    an opaque base64url JSON cursor. `ServerConfig.DefaultPageLimit`
    (50) and `ServerConfig.MaxPageLimit` (500) added. Nine tests
    including a three-page walk with full ordering reconstruction
    and soft-delete-hidden assertion.

11. **`GET /v1/contest-dupe`** — narrow purpose-built endpoint.
    Validates `logbook`, `call`, `band` (required) and `mode`
    (optional). Returns `{"duplicate": bool}`. 15 tests covering
    band-only / band+mode hits and misses, soft-delete exclusion,
    logbook-scoping (hit in logbook A must NOT match in logbook B —
    the whole point of the logbook-per-contest pattern), and all
    validation error paths.

12. **`GET /v1/contact-history`** — "have I ever worked this call"
    lookup for the logging-app's draft panel. Required: `?call=`.
    Optional: `?logbook=` to restrict to a single logbook (default
    scope is all logbooks). Returns `{"items": [...]}` capped at
    `Server.MaxContactHistoryResults` (default 100). 10 tests
    including a **latent-bug fix** in the underlying sqlite query:
    the existing `Call = ? OR Call LIKE ?%` group was not
    parenthesised, so AND-ing additional predicates (logbook_id,
    the implicit `deleted_at IS NULL`) bound tighter than the OR
    and silently leaked rows. Wrapping the OR in `qm.Expr(...)`
    fixed it. The old code had the same issue but no test
    exercised it, so nothing caught the leak.

13. **`GET /v1/version`** — diagnostic. Returns
    `{"daemon":"<build>","go":"<runtime>","schema":{"version":N,"dirty":bool}}`.
    The daemon build version comes from `cmd/smd/main.go`'s
    package-level `Version` var, overridable via
    `go build -ldflags "-X main.Version=..."`. Schema version is
    pulled from `schema_migrations` (golang-migrate's table).
    `api.New` signature extended to accept `daemonVersion string`.

### Coverage summary end-of-session

| Package | Coverage |
|---------|----------|
| `internal/api` | full CRUD + list + contest-dupe handler tests; 1000-QSO stress round trip |
| `internal/qsoservice` | `Update` and `Submit` exercised via api tests; dedupe unit tests cover freq |
| `internal/database/sqlite` | new `DeleteQsoByIDWithContext`, `FetchQsoPageByLogbookWithContext`; widened contest-dupe method |
| `internal/utils` | new freq-conversion helpers with round-trip tests |

Full suite race-detector clean.

### Design decisions made

- **`types.Qso` is the canonical DTO** for HTTP/service boundaries.
  Do not build parallel `XPatch` / `XRequest` structs that duplicate
  field lists from `types.X`. Use `json.Unmarshal` overlay + stash-
  restore of immutables instead. Captured as a memory.
- **types package rule is pragmatic, not prophylactic.** Exported
  types move to `internal/types` only when an actual cycle could
  form, not as a preventive measure. Captured as a memory.
- **FREQ is part of QSO identity.** Dedupe key now includes FREQ per
  ADIF-spec guidance. Schema unchanged.
- **FREQ on the external surface is MHz** (ADIF-native). kHz is the
  sqlite storage unit; translation lives in the adapter, not
  anywhere above it.
- **PATCH design:** immutable fields always restored server-side,
  dedupe inputs recomputed on change, collision rejected with 409,
  no force bypass on edit.
- **DELETE is always soft-delete at the daemon.** Hard-delete stays
  a logbook-app concern.
- **Pagination is forward-cursor only**, newest-first, opaque token.
  Soft-deleted rows are always hidden. Opt-in "show deleted" is
  deferred until the logbook-app needs it.
- **Contest isolation via logbook-per-contest, not separate DB
  file.** `logbook_id` partition gives the contest-dupe endpoint
  false-positive-free scoping by construction.
- **Logging session is entirely client-side.** No `session_id`
  column, no `/v1/session` endpoints. The logging app keeps an
  in-memory list of QSOs submitted since Start, uses existing
  PATCH/DELETE for edits, and formats the end-of-session email
  payload client-side from data it already has (or re-fetches via
  `GET /v1/qso/{id}`). Captured as a memory.
- **No daemon-side ADIF export endpoint.** Export is a
  client/admin concern, not a daemon concern. Clients that need
  ADIF page through the QSO list and serialize client-side using
  `internal/adif` (which is a regular Go library, not HTTP-wrapped).
  The "real" backup story is forwarding to online services
  (QRZ, LoTW, SM-online) — that's the daemon's redundancy
  mechanism. Filesystem backup of the sqlite file is a user/OS
  concern.

---

## Session 8 (2026-04-17, compressed)

Implemented milestone 1b step 1 (full logbook CRUD: list, get,
create, update, delete with FK-safe soft-delete). Tightened QSO
submit to require `?logbook=<id>` with existence + callsign-match
validation. Standardised `errors.Op` pattern across handlers
(`api.FuncName`). Added `IsValidCallsign` at three layers (schema
CHECK, handler, domain). Fixed `UpsertLogbookWithContext` latent
no-op-on-existing bug. Listener protocol made config-driven
(`unix` / `tcp`). 20-client × 50-QSO stress test green with
~146 QSO/s baseline. sqlite package coverage 0% → 66.9%.

Design decisions fixed in this session and carried forward:
logbooks are created explicitly (no auto-create); logbook
callsign is immutable after creation; workflow-driven milestone-1b
order (fetch/edit/delete before enrichment).

---

## Session 7 (2026-04-16, compressed)

Reviewed all 8 carry-forward packages and wrote
`docs/v2-design/milestones.md`. Implemented milestone 1: daemon,
config, qsoservice (validation/dedupe/atomic write), API handlers,
error envelope, healthz. Dev environment + GitHub Actions CI.
5 bugs fixed during testing. 25 tests across 4 packages. Schema
cleanup (removed session table/apikey, added dedupe_key column).

---

## Sessions 1–6 (compressed summaries)

### Session 1 (2026-04-14)

v2 rewrite decision. Completed v1 analysis (5 docs). Tagged
`pre-ft8-removal` and `v1.0.0`. Created `v1` branch. Three v1 bug
fixes before tagging.

### Session 2 (2026-04-15)

Wrote `docs/v2-design/structure.md` (6 structural decisions). Rewrote
`CLAUDE.md`. Deleted v1 CI workflows. Commit `5ef55c1`.

### Session 3 (2026-04-16)

Big restructure: reshaped main into v2 milestone-1 layout. 730 files
changed, 68,934 deletions. Scaffolded `cmd/smd`, `internal/api`,
`internal/qsoservice`, `internal/config`. Commit `0010b6e`.

### Session 4 (2026-04-16)

Short session. Added `Taskfile.yml`. Deleted remaining v1 leftovers.
Commit `1ee2ced`.

### Session 5 (2026-04-17)

Wrote `docs/v2-design/api.md`. Strengthened invariants. Full
`internal/errors` review (11 findings). Wrote `internal/logging`
review doc (14 findings). Two feedback memories.

### Session 6 (2026-04-18)

Applied all 14 `internal/logging` findings. Fixed embedding bug.
Both `internal/errors` and `internal/logging` reached v2 final state.

---

## Next steps (priority order)

### Parked follow-ups (named, deliberate defer)

- **Contest logging not built.** Flagged session 66 (2026-05-16). The SPA today is steady-state casual-QSO logging — no contest mode, no macro keys (though F1, F4–F12 are already reserved by ADR 0007 for this), no exchange-field handling (serial numbers, RST+state, etc.), no real-time dupe checking, no multiplier tracking, no Cabrillo export, no contest-specific ADIF fields (`STX`, `STX_STRING`, `SRX`, `SRX_STRING`, `CONTEST_ID`). Scope question to settle when it's picked up: separate client (e.g. `frontend/contest/`) versus a mode switch inside `frontend/logging/`. Contest logging has different UX rhythm (high rate, keyboard-first, minimal panels) and different field shape (per-contest exchange template) — likely warrants its own SPA in line with the logging-vs-logbook split per `feedback_logging_vs_logbook_scope`, but pin that decision when an operator-driven need surfaces (likely the next CQ WW or similar contest the operator wants to enter). Daemon side is largely already there — `types.Qso` follows ADIF (so contest fields slot in via existing `additional_data` pattern), multi-rig API-aware for SO2R contests, UUIDv7 for sync.

- **Inbound CAT command path (ADR 0021 territory).** Flagged session 66 (2026-05-16) when "Ctrl+\\ VFO swap" surfaced as a deferred polish item. Operator's mental model: keyboard shortcuts work consistently across manual AND CAT modes (no other shortcut is gated by CAT state). Implementing Ctrl+\\ as manual-only would be surprising UX. Implementing it for CAT mode opens the v1 inbound-command path that ADR 0019 explicitly deferred. Natural scope at that point isn't just VFO swap — it's the full v1 SPA-drives-rig surface: set selected VFO, set split on/off, set frequency, set mode. (PTT stays deferred per ADR 0019 — separate concerns: per-connection asserted state, disconnect-safety-release, future arbitration.) Requires: bridge command-write methods, daemon HTTP endpoint shape (`POST /v1/rig/cmd` or per-field), rigdef SET-command encoders (currently only INIT + READ are encoded), error handling for rig-rejected commands, multi-rig awareness from day one. **Deliberately parked** so dogfooding the existing read-only surface surfaces what actually needs SET-side support and in what order. ADR 0019's "Triggers to revisit — The SPA needs to drive the rig" already captures this. When this gets picked up, expect a planning pass + new ADR before code.

### The immediate next action (post-review, pick a phase)

QRZ port complete, review triage complete, Task #29 (cmd/smd/main.go
tests) complete in session 14, SSE event stream complete in session
14. The forwarding subsystem + its live notification surface is
**done** — the next session picks one of three directions below.

My standing recommendation is a **daemon-only alpha checkpoint**:
cut a tagged build, dogfood via curl + SSE + the existing HTTP
endpoints, and use the results to inform the next subsystem
choice (a second real forwarder vs. bridge/CAT vs. client work).
The forwarding + events surface is the minimum viable
daemon-side feature set; running it against real QSOs for a
week will surface gaps cheaper than guessing at the next
subsystem. If alpha feels premature, the second-best option is
a second real forwarder (ClubLog or LoTW) — it validates the
"prefix-agnostic plumbing" claim and gives the SSE stream more
to say. Bridge/CAT is a larger effort with its own design doc
still to write.

The 8-stage QRZ plan is retained below for historical context;
do **not** re-derive the design decisions captured in it.

**QRZ API reference** (from the operator's paste of QRZ's developer
guide — use this, not an inferred version):

- Endpoint: `https://logbook.qrz.com/api`, HTTP POST with
  `application/x-www-form-urlencoded`.
- User-Agent header required (≤128 chars, should include callsign
  + app name for identifiability).
- **INSERT**: `ACTION=INSERT`, `KEY=<apikey>`, `ADIF=<single-record>`.
  Response: `RESULT=OK|FAIL|REPLACE` + `LOGID` + `COUNT`.
- **UPDATE**: no native update — use `ACTION=INSERT` +
  `OPTION=REPLACE`. Response `RESULT=REPLACE` when it overwrote a
  duplicate. This is what v1 did.
- **DELETE**: `ACTION=DELETE`, `LOGIDS=<id>` (comma list for many).
  Response: `RESULT=OK|PARTIAL|FAIL` + `COUNT`.

**Resolved design decisions** (don't re-open):

- **`Forwarder.Submit` signature**: `(ctx, qso, action, priorUpstreamID string)`
  (stage 1). Worker populates `priorUpstreamID` from the prior
  insert row's `upstream_id` for delete actions only.
- **`Forwarder.AdifPrefix()`** (stage 1). QRZ returns `"QRZCOM"`.
  Worker stamps `QRZCOM_QSO_UPLOAD_STATUS="Y"` +
  `QRZCOM_QSO_UPLOAD_DATE=today` on success (insert/update, not
  delete — soft-deleted QSOs don't export). Failures/transients
  stamp nothing.
- **Delete LOGID wiring**: option A from the session-12 discussion.
  Worker does a DB lookup before `Submit`; forwarder receives LOGID
  via `priorUpstreamID`; empty lookup → terminal "no upstream id
  for delete".
- **QRZ credentials shape**: `{"api_key": "..."}` only — QRZ
  enforces the callsign/logbook match server-side, so a local
  `callsign` field would only introduce drift risk without a
  guarantee. (stage 2, landed)
- **QRZ response classification** (stage 3, landed): per-action
  matrix in `response.go` and `forwarding-implementation.md` §8.1.
  Short form: `RESULT=AUTH` → Terminal (global); `RESULT=OK` /
  `RESULT=REPLACE` → Success with `UpstreamID = LOGID`;
  `RESULT=FAIL` on delete → **Success** (idempotent);
  `RESULT=FAIL` elsewhere → Terminal; `RESULT=PARTIAL` / unknown
  on any action → Terminal; missing `LOGID` on claimed-OK insert →
  Terminal. Transport-level errors (HTTP 4xx/5xx, network, timeout)
  are classified at the `Submit` call site in stage 4 — network
  and 5xx/429 → Transient, 4xx → Terminal.
- **Retry-defaults ownership** (stage 7): each forwarder package
  exports `var DefaultRetry types.RetryConfig`.
  `spawnForwarderWorkers` in `cmd/smd/main.go` looks it up by type.
  Delete the `defaultForwarderRetry` temporary fallback.
- **Test creds**: operator has a QRZ test logbook with `USER` and
  API key in env vars. Used for manual integration verification
  after code lands — **not** for automated tests.
- **Automated tests**: `httptest.NewServer` everywhere, hermetic
  and CI-safe.

**Remaining stages** (each is a committable unit):

| # | Stage | Status |
|---|-------|--------|
| 1 | Extend `Forwarder` interface (`AdifPrefix`, `priorUpstreamID`) | **done** (session 12) |
| 2 | `internal/forwarding/qrz/` skeleton — credentials struct (`api_key` only), `New`, `Type()="qrz"`, `AdifPrefix()="QRZCOM"`, registry init, stubbed Submit, validation tests | **done** (session 13) |
| 3 | Response parser + classification function — `parseResponse` + `classifyResponse` with per-action helpers (`classifyInsert`/`Update`/`Delete`); `AUTH` global, single-LOGID-delete `FAIL` → Success; 26 unit tests | **done** (session 13) |
| 4 | Insert + update `Submit` — real HTTP, `buildForm` + `classifyHTTPStatus`, `DefaultEndpoint`/`DefaultHTTPTimeout`/`UserAgent`, package-internal `newWithEndpoint`; 18 httptest tests + live harness (`TestLive_InsertThenUpdate` quick, `TestLive_InteractiveFlow` with `/dev/tty` pauses); live-validated against real QRZ | **done** (session 13) |
| 5 | Delete `Submit` + worker LOGID lookup — `FetchInsertUpstreamIDWithContext` (defensive ORDER BY, UNIQUE-constraint-aware), worker `resolvePriorUpstreamID` short-circuit, QRZ `buildForm` delete branch; CI fix for `:memory:` + `-race` flake (DSN `cache=shared`); live harness delete via `Submit` | **done** (session 13) |
| 6 | ADIF-stamp wiring — `MarkUploadSuccessWithAdifStampWithContext` writes both the qso_upload transition and a `json_set` stamp on `qso.additional_data` in one tx (no new columns; matches the "additional_data absorbs ADIF spec evolution" invariant); worker `markSuccess` dispatch gates on AdifPrefix + action; prefix-agnostic so new forwarders land without sqlite/migration changes | **done** (session 13) |
| 7 | Retry-defaults ownership refactor — per-forwarder `DefaultRetry` vars, `forwarding.RegisterDefaultRetry` / `DefaultRetryFor` registry companions, `spawnForwarderWorkers` lookup-by-type + loud error for missing defaults, hardcoded `defaultForwarderRetry` deleted | **done** (session 13) |
| 8 | Import `internal/forwarding/qrz` in `cmd/smd/main.go` (regular import — main sets qrz.UserAgent); wired `qrz.UserAgent = "station-manager/" + Version` and `adif.ProgramVersion = Version` at the top of run(); flipped `adif.ProgramVersion` from const to var; ldflags smoke-check passes | **done** (session 13) |

### Follow-ups after the QRZ port

1. **Alpha checkpoint.** Tag a build, dogfood the daemon against
   real QSOs for a week: ingest via `POST /v1/qso` (curl or a
   disposable script), QRZ forwarding on, SSE stream tailed with
   `curl -N` or a browser `EventSource`. The forwarding +
   events surface is the smallest self-contained daemon-side
   feature set; real use will surface gaps cheaper than guessing.
   **My standing recommendation for the next phase.**

2. **A second real forwarder (ClubLog / LoTW / eQSL)**. Exercises
   the "prefix-agnostic generic plumbing" claim. Would validate
   the registry + `DefaultRetry` ownership pattern in anger. Also
   a good smoke test for whether the stage-6 ADIF-stamp json_set
   generalises as cleanly as we think it does.

3. **Bridge / CAT design — substantial progress session 15, now at a
   decision point.** Design is in `docs/v2-design/bridge.md`, rewritten
   in-session from a two-frontend shape to a much smaller Unix-socket-only
   SM-internal multiplexer. The live question is **§6 YAGNI: build now or
   defer?** User lean at session end is *defer*, with `internal/cat` given
   a pluggable transport abstraction (§8.3) so the deferred path costs
   nothing. Recommended next-session work order:

   **a. Answer §6.** Everything else depends on this.
   **b. If deferred:** settle §8.3 (`internal/cat` transport abstraction
      shape) as a design-only exercise. This unblocks the logging app for
      milestone 2 without foreclosing the bridge.
   **c. If built now:** sequence is (i) `internal/cat` transport abstraction,
      (ii) NDJSON schema (§8.1), (iii) bridge implementation, (iv) logging
      app wired through `SocketTransport`, (v) defer CAT control app to its
      own design session.

   My recommendation: **defer the bridge, but do §8.3 now.** Keeps the
   logging app on the fastest path (direct `SerialTransport`) and makes the
   eventual switch to a bridge mechanical.

### Parked follow-ups (low priority, not blockers)

- **Dead-method sweep (SQL audit item 3).** Several sqlite methods
  have only test callers today. The former forwarder-queue
  candidates (`FetchPendingUploads`, `UpdateQsoUploadStatus`) have
  already been deleted in session 11 — they were v1 worker code,
  replaced by the stage-6 purpose-built methods. The remaining
  low-signal methods
  (`FetchQsoSliceByLogbookId`, `FetchQsoCountByLogbookId`,
  `FetchQsoByDedupeKey`'s no-context wrapper,
  `FetchContactedStationByCallsign`, `FetchCountryByCallsign`,
  `FetchCountryByName`) still need a specific "delete or keep"
  decision. Enrichment methods likely return in milestone 2; the
  QSO list helpers may be dead. Park until we know.
- **SQL audit item 4** — optional `(call, logbook_id) WHERE
  deleted_at IS NULL` composite for contact-history with
  `?logbook=` filter. Defer until a concrete performance
  complaint surfaces.

### v2 design work

- **Pick the ORM/generator approach** → `docs/v2-design/db-layer.md`.
  sqlboiler stays until there's a reason to change.
- **Multi-rig as first-class assumption** — bridge-side shape now
  captured in `docs/v2-design/bridge.md` (first-class from day one
  in the bridge). Data-model side (rig id on `types.Qso`, logbook
  schema impact) still open; address when rig control construction
  starts.

### Deferred features

- **Logging-app text-file fallback reconciliation** — milestone 2+.
- **Enrichment / contacted_station population** — milestone 2.
  Client-side concern; daemon submit path stays fast and network-free.
- **Daemon dashboard / monitoring UI** — post-milestone 2.

### v1 branch follow-ups

- Data race candidate fix (session 6) not yet verified on v1 branch.
- Hardcoded QRZ forwarder — v2 concern, unlikely to be fixed on v1.

### Maintenance

- Compress session 9 after session 11 lands (session 8 is already
  compressed at end of session 10).
- Update this file at end of every session.
