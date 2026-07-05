# Backlog archive — shipped / resolved items

**Not read at session start.** This is the graveyard for `docs/backlog.md` items
that have **shipped, been resolved, or been ruled out** — moved here (not struck
in place) so the live backlog stays lean and cheap to load. Open it only when you
need the history of a specific item ("when/how did X ship?").

- **Authoritative record is git history + the ADRs + the memory files** — this is
  the grep-able convenience copy, same role `session-handoff-archive.md` plays for
  session entries.
- Items keep their original section grouping and their SHIPPED/FIXED/RESOLVED
  stamp. Newest work tends toward the bottom of each section (append on archive).
- A struck item in the *live* backlog is a bug — it should be **moved here**
  instead. See `backlog.md`'s conventions.

<!-- Archived detail blocks follow, grouped by their original backlog section. -->

## Bugs (detail)

- ~~**`PUT /v1/config` lost-update race between the two SPAs.**~~ **SHIPPED 2026-07-05**
  (`internal/api` review finding 1, LOW-MEDIUM, data-loss). The handler built a `candidate`
  from a pre-lock `Snapshot()`, overlaid the request, then committed with
  `Update(func(cfg){ *cfg = candidate })` — wholesale-replacing the fresh locked clone with a
  stale-snapshot-derived value. Two SPAs saving different surfaces concurrently (logging SPA My
  Station / FT8 settings + config SPA Rigs / Forwarding / SMTP) could interleave snapshot A,
  snapshot B, commit A, commit B → commit B silently reverts everything A changed. Real for the
  7Q8AC deployment (a second operator with the config SPA open while the operator logs). **Fix:**
  the presence-aware overlay + `Normalize` + `Validate` now run **inside** the `Update` callback,
  against the fresh lock-held clone (extracted to `overlayConfig`); the blank-keep credential
  merges base off the current stored value; request-only field validations (caller_answer_mode,
  max_repeats, mode-mapping driver) hoisted ahead of the lock as loud 400s; in-lock `Validate`
  failure → sentinel `errPutValidation` → 400 with the live config untouched. The first-run setup
  seed stays outside the lock (DB I/O; single-operator, not the race). Regression test
  `TestHandlePutConfig_ConcurrentCrossSurfaceNoClobber` (30×2 concurrent cross-surface PUTs under
  `-race`, both survive). `internal/api` full suite green under `-race`. Findings #2 + nits triaged
  to backlog.

- ~~**SPA `fetch` calls have no timeout (P1 — flaky-link ship risk).**~~ **SHIPPED
  2026-07-05.** No `safeFetch` call passed a timeout, so a wedged / half-open daemon
  (accepts the connection, never responds) hung boot on a blank page and `submitQso`'s
  in-flight latch for minutes, with no operator signal. Fix: `safeFetch` now applies a
  default `AbortSignal.timeout()` (`DEFAULT_TIMEOUT_MS` 15 s) to every call, composing
  it with any caller signal via `AbortSignal.any` so operator-cancel still works;
  `WRITE_TIMEOUT_MS` (30 s) on the QSO-log POST (a timed-out write is ambiguous, so
  give it room before the latch releases). A fired timeout now surfaces as retriable
  `'network'` (was misfiled as `'aborted'`, which callers drop silently — it would have
  swallowed the hang). `internal/cloud` unaffected; SPA-only. Tests: `_helpers.test.ts`
  (timeout→network, default-signal injection, caller-signal composition, opt-out) +
  five caller tests updated from signal-identity to signal-propagation assertions. Full
  suite 876 green, type-check/lint/prettier clean. NB the earlier commit `e0b860f0`
  titled "add fetch timeouts" only committed the backlog triage note — the actual
  wiring landed here.

- ~~**FT8 caller-side sequencing (Call CQ) — on-air validation.**~~ **PASSED 2026-07-04.**
  ADR 0033 Call-CQ auto-worked pile-up validated live on 17 m as 7Q5MLV: called CQ →
  large real pile-up (Middle East + Asia) → worked answerers through the full ladder
  (report → R-report → RR73) → logged with name+country enrichment (this session's fix,
  e.g. "Mr Xiao / China") → auto-resumed CQ. 33 QSOs in ~74 min, clean PTT cycling.
  **Guaranteed-stop confirmed:** rig powered off mid-TX → warning + TX stops
  (release-on-disconnect). Only issue found was the self-decode (fixed same session, below).

- ~~**FT8: SM self-decodes its own TX slot.**~~ **FIXED 2026-07-04.** During a Call-CQ
  pile-up the operator's OWN transmission decoded off the rig's TX-audio bleed at the TX
  offset (e.g. `JA2ICB 7Q5MLV RR73`, DE = own call) and appeared as a phantom station in
  Band Activity + Rx Frequency. Fixed with `dropOwnTransmissions` (`internal/ft8/decode.go`):
  the decode loop drops any decode whose sender/DE equals the operator's own call, wired via
  `Service.SetStationCall` (provider from live config, no restart to change call). Filters at
  source, so Band Activity, occupancy, AND the sequencer never see our own signal — a
  legitimate decode is never FROM our own call, so it's unconditionally safe. Test
  `TestDropOwnTransmissions`. Found on-air during the caller-side validation above.

- ~~**Bridge review F3/F4 (Low, deferred from the session-196 guaranteed-stop review).**~~
  **DONE 2026-07-04.** **F3** — the tune-off mode/power restore ran on the caller's ctx, so
  a request cancelled during the ~150 ms settle skipped the restore and stranded the rig in
  RTTY / tune-power (`releaseTune`, `tune.go`). Fixed by detaching step 2 (settle + restore)
  onto `context.Background()` — like the auto-off backstop — while the unkey (step 1) stays
  on the caller's ctx (a cancel there just re-arms the backstop). Regression test
  `TestStopTune_CtxCancelDuringSettleStillRestores` (inverted from the old
  `…SkipsRestore`; proven to fail pre-fix). **F4** — protocol-inherent: an uncorrelated CI-V
  FB/FA ack arriving late (after its command timed out) can be delivered to the next
  command's waiter. Cannot be closed in code (CI-V has no per-command sequencing); resolved
  as an accepted-limitation comment on `deliverAck` (`command.go`). No behaviour change; the
  FTdx10/Yaesu station uses the fire-and-forget path and never reaches it.

- ~~**Multi-tab rig awareness banner.**~~ **SHIPPED 2026-07-04.** The advisory half of
  the multi-tab operating-lock (the full lock stays live in `backlog.md` P2). Daemon:
  `internal/bridge` emits a `rig-clients {count}` SSE event on multi-tab transitions only
  (`EventRigClients` + `RigClientsPayload`; `Service.Subscribe` announces when the
  subscriber count reaches ≥2 on join, and the decrement back to ≥1 on leave so the banner
  clears — a lone tab gets nothing, so the single-subscriber event stream is untouched).
  SPA (logging only — the only rig-controlling SPA): `bridgeState.tabCount` + a `rig-clients`
  listener in `bridge.svelte.ts`, and a passive amber banner in `app.svelte` when tabCount
  > 1. Advisory only — no write is gated (that's the deferred lock). Tests:
  `TestSubscribe_BroadcastsClientCount` (bridge). Design/dig notes in the P2 backlog entry.

- ~~**`PUT /v1/config` contract: omitted blocks zeroed (M5).**~~ **FIXED 2026-07-04.**
  Filed from the `internal/api` review (2026-06-14, M4 + M5). M4's `default_rig_id`
  half shipped with the config-SPA Rigs tab (`req.DefaultRigID` presence-aware,
  validated). **M5:** `ConfigResponse.LoggingStation` / `.Station` were value-typed
  and copied unconditionally in `handlePutConfig`, so a PUT that omitted a block
  zeroed the operator identity (SPAs masked it by always bundling both). Fixed by
  making both `*types.LoggingStation` / `*types.StationConfig`, applied presence-aware
  (only overlaid when the body carried them) and always set non-nil in
  `buildConfigResponse` for GET — matching the pointer-block pattern the rest of the
  struct already uses (Qsl / Ft8Display / Rigs / Forwarders / Smtp / Lookup).
  Regression test `TestHandlePutConfig_OmittedBlocksPreserved` (proven to fail against
  the old unconditional copy). Backward-compatible. The `default_logbook.id`-on-PUT
  residual stays live in `backlog.md` (P3, no consumer). See
  `docs/reviews/internal-api-2026-06-14.md`.

- ~~**FT8/tune guaranteed-stop F1(b): defensive unkey on reconnect after a dead-port strand.**~~
  **SHIPPED 2026-07-02** (`markStrandedKeyed` + `defensiveUnkeyIfStranded` in
  `internal/bridge/pipeline.go`, ADR 0042; test `TestDefensiveUnkeyIfStranded`).
  Filed 2026-07-02 (bridge safety review); design recorded in ADR 0042. F1(a)
  (teardown unkey on the healthy-port daemon-shutdown case) SHIPPED + **bench-
  validated 2026-07-02** (`systemctl restart` drops PTT on the FTdx10); F1(b) now
  built too (flag-based reconnect unkey). When F1(a)'s teardown `tx_off` write *fails* — a
  dead-port exit mid-tune/FT8-TX (write-watchdog close or EIO) — the rig may still
  be CAT-keyed and the supervisor reopens the port with it transmitting. Fix
  (flag-based, per ADR 0042): set an in-memory `strandedKeyed` flag when the
  teardown unkey write fails; the next pipeline instance sends one defensive
  `tx_off` right after identity-confirm, then clears it. In-memory only (the
  dead-port-*at-shutdown*→restart residual is accepted — attended operator + rig
  TOT). Needs a test (mirror `TestUnkeyOnTeardown`) + on-air validation. Surfaces:
  `internal/bridge/pipeline.go` (teardown flag-set + post-identity hook),
  `tune.go`/`ft8tx.go`. See ADR 0042.

- ~~**FT8 capture grabs the mic with no CAT / rig off.**~~ **FIXED 2026-06-21.**
  Capture acquisition is now gated on CAT being live. `bridge.Service.RigConnected()`
  (the `activeClient != nil && identityConfirmed` core of `TxReady`, minus the
  tune/FT8-TX flags) is injected into the FT8 service via `ft8.Service.SetCatGate`
  in `cmd/smd` — **only when the bridge is enabled**, so a no-CAT audio-only setup
  stays purely demand-driven. `startCaptureLocked` defers (logs, no mic) when the
  gate is false; a reconcile loop (`catReconcileInterval`, 2s) acquires once CAT
  comes up with a subscriber present and releases the mic if CAT drops mid-session.
  **Passive no-data disconnect (review M1):** a rig that goes silent but leaves the
  serial port open keeps `activeClient`/`identityConfirmed` set (they clear only on
  pipeline exit), so `RigConnected` also requires fewer than `noDataStrikeLimit` (2)
  consecutive readLoop no-data timeouts — a quiet-but-alive rig recovers on the
  INIT+READ probe within one liveness cycle (one strike, then reset) and stays
  "connected"; a genuinely dead rig accrues strikes and reads not-connected, so the
  mic is released. Fail-soft throughout. Tests: `TestCapture_CatGate_*` (ft8),
  `TestRigConnected` + `TestRigConnected_NoDataDisconnect` + the strike assertion in
  `TestPipeline_DisconnectAnnouncedOnce` (bridge).

- ~~**Fresh-install `config.json` shape (design decision pending).**~~ **RESOLVED
  2026-06-23** — all three sub-issues below are closed (dangling `default_rig_id`
  fixed; `forwarders: null` confirmed correct; materialization decided →
  sparse-but-served). Nothing pending; kept for the trail. Filed from the
  2026-06-23 clean-DB dogfood deploy; narrowed as sub-issues resolved:
  - ~~**Dangling `default_rig_id`.**~~ **FIXED 2026-06-23.** `applyDefaults` now
    only sets `default_rig_id` when a rig catalogue exists (a rig-less install
    leaves it `0` = "no active rig"); `validate.go` rejects any non-resolving id
    except `0`-with-no-rigs; the logging header shows **"Rig: not set"** when unset
    (`LoggingCard.svelte`). Tests: `TestValidate_DefaultRigID`, the `default_rig_id`
    assertions in `TestLoad_DefaultsApplied`, and `TestHandleGetConfig_PreSetup`.
  - **`forwarders: null` is correct — WON'T change.** Do NOT pre-list types
    disabled: ADR 0022 enqueues by presence, so a listed-but-disabled forwarder
    accrues upload rows. The operator adds the ones they use via the config SPA
    (see the data-driven forwarder-setup item below).
  - ~~**Materialization is inconsistent.**~~ **DECIDED + DONE 2026-06-23 (sparse-but-served).**
    The investigation found the codebase uses three models, not two: filled-on-disk
    (`server`/`datastore`/`logging`/`smtp`/`lookup`), **sparse-but-served-resolved**
    (`ft8.display`/`ft8.frequencies`, `psk_reporter` — file empty, `/v1/config` GET
    serves resolved defaults), and sparse-AND-not-served (`bridge.timeouts`/`tune` —
    the only genuinely *invisible* block). Chosen fix: bring bridge onto the
    **sparse-but-served** pattern (config.md §15.5), NOT materialize-on-disk —
    `bridge.ResolveTimeouts`/`ResolveTune` (the same resolution `Service.New` uses,
    so served == runtime) are now served on GET as `bridge_timeouts`/`bridge_tune`.
    `config.json` stays sparse; the SPA reads effective values; no default-freeze,
    no migration, no constant duplication. `qsl: {}` stays empty (pure operator data,
    no defaults to serve). Tests: `TestResolveTimeouts`/`TestResolveTune` (bridge),
    bridge-timeout assertions in `TestHandleGetConfig_PreSetup` (api). Docs: config.md
    §15.5, api-endpoints.md GET/PUT `/v1/config`.

- ~~**FT8 pile-up stack mixes odd/even parities — unworkable + confusing.**~~ **SHIPPED
  2026-06-27.** The daemon now exposes the session's RX parity (`QsoStatus.their_period`
  on the `ft8-qso` SSE); the SPA derives each decode's parity via `utils/ft8Parity.ts`
  `slotParity` (`(unix/15)%2`, tested), and `Ft8Panel` computes the run's `workableParity`
  (RX parity when a contact is live, else the queue head's parity). ctrl+click enqueue is
  blocked for a wrong-parity decode (info toast with the reason); wrong-parity calling-you
  rows are muted (opacity-50) with an explanatory title; the pile-up drawer header shows
  the run parity. Original report below for reference. Filed 2026-06-27
  (operator confirmed as a real bug during a live pile-up). FT8 is half-duplex on a 15s
  two-parity grid: every station you can work in one run must sit on the parity OPPOSITE
  your TX (you're deaf to your own parity mid-transmit), and a single TX parity cannot
  serve a mixed-parity queue — chasing a wrong-parity station means flipping your whole
  run's parity, which mis-aligns everyone else. The caller sequencer already gets this
  right for answers (`caller_sequencer.go`: `theirPeriod = oppositePeriod(ourPeriod)`,
  scans only `theirPeriod`). The **SPA pile-up stack does not.** `ft8PileupStack.svelte.ts`
  captures `slotUtc` per entry (the comment says it "fixes the TX parity" and you should
  "only SEE/select them in your RX parity") but **never enforces or displays parity** —
  while idle/between QSOs both parities decode, so ctrl+click can queue stations from
  either, the FIFO drain interleaves them, and after a few QSOs the operator loses track
  of which entry is actually workable when. Fix direction: derive each entry's parity from
  `slotUtc`; make the stack single-parity (= the run's `theirPeriod`) — reject or grey a
  conflicting-parity push (don't silently queue it), show parity per entry in
  `Ft8PileupDrawer.svelte`, and have the drain only work same-parity entries (a wrong-parity
  station needs an explicit "flip the run's parity" action, not an interleave). Surfaces:
  `ft8PileupStack.svelte.ts`, `Ft8PileupDrawer.svelte`, the work-a-caller drain in the
  Operate view. Related dogfood notes (same pile-up cluster): "add the currently-worked
  station to the pile-up list", "currently-worked station should be coloured/disabled in
  Band Activity", "adjustable number of calls before Next". This also feeds the rationale
  for the pending `operator_pick` stack (ADR 0033) — bake the single-parity constraint in
  there too.
  **Agreed approach (2026-06-27, operator):** the workable parity in answer-a-CQ mode is
  precisely **your RX parity = the parity of the CQ you answered** (you take the opposite
  of station X, so X's parity is what you receive on, and every queueable caller must match
  it). Enforcement = **ctrl+click only enqueues a decode whose slot parity == your RX
  parity; wrong-parity decodes are non-clickable (greyed) with a reason**, never silently
  queued. Requires the parity-label work (next item) so the operator can SEE why a station
  is/ isn't clickable — the two ship together.

- ~~**FT8 Call-CQ + pile-up queue = two controllers fighting over the rig.**~~
  **SHIPPED 2026-06-27** (commit `865819b5`): during an active Call-CQ session the
  pile-up queue is disabled — ctrl+click enqueue is gated off (`callerActive` in
  `Ft8Panel.svelte`) with visible "Calling CQ — pile-up queue disabled. Abandon to
  work stations by hand." feedback; Abandon is the single stop, exactly the agreed
  interim posture. Original diagnosis kept for the trail. Filed 2026-06-27
  (operator observed live). During an auto Call-CQ session the daemon's `auto_first` is the
  "who's next" engine (picks each answerer by decode order, loops until Abandon). But the
  SPA pile-up queue is still live: ctrl+clicking answering stations into it **flips the run
  into pile-up-drain mode** — it stops auto-answering the next decode and instead drains the
  operator's queue (work-a-caller). So two independent "who's next" engines drive one rig
  with no coordination — the single-tab cousin of the multi-tab rig-lock risk above. (NB: a
  static read of the drain `$effect` guard `if (ft8State.qso.active) return` at
  `Ft8Panel.svelte:290` predicts the queue should be INERT during Call-CQ, since `seqCalling`
  publishes `Active:true` throughout — `caller_sequencer.go:251-260` / `sequencer.go:581`.)
  **Root cause CONFIRMED from the daemon log (2026-06-27, smd.log @ 10:03:31):** the queue
  IS inert during Call-CQ — the trigger is the **Next button**. ctrl+click populates the
  stack → a non-empty stack makes `canNext` true (`Ft8MsgPanel.svelte:67` =
  `canAbandon && pileup.count > 0`) → the **Next button appears inside the Call-CQ controls**
  → clicking it runs `onNext` = `abandon` **without pausing the stack** → the Call-CQ session
  goes idle → `qso.active` flips false → the drain un-parks and works the queue via
  work-a-caller, whose completions go IDLE (not resume CQ), so it **never returns to calling
  CQ**. Log proof: one `cq/start` at 09:12, then `session abandoned` at 10:03:31 immediately
  followed by `/work` (503 settle, then 202) and 100 `working a caller` starts / 0 further CQ.
  **Agreed approach (2026-06-27, operator):** ONE control model per mode. While a Call-CQ
  session is active,
  **ctrl+click-to-enqueue is DISABLED** (visibly — "Calling CQ — queue disabled", not a
  dead no-op), and **Abandon is the single way to stop the auto-answer loop**. This is the
  correct *interim* posture while `operator_pick` is parked (the desire to hand-pick during
  CQ IS `operator_pick`; when it lands, CQ-mode clicking becomes the picker's feed — so this
  is a coherent waypoint, not a forever-no). Surfaces: the Band Activity ctrl+click handler
  (gate on `qso.active && role==='caller'`), `ft8PileupStack.svelte.ts` /
  `Ft8PileupDrawer.svelte`, the drain `$effect` in `Ft8Panel.svelte`.

- ~~**`ft8.device` name-matching.**~~ **DONE (stale — superseded by ADR 0028).**
  Audio-device selection is now name-based: the active rig's `audio.rx`/`audio.tx`
  (by device *name*, stable across reorder) feed `Ft8Config.Device`, and
  `resolveAudioDevice` (`internal/ft8/servicetx.go`) accepts a device **name** (a
  non-numeric string resolves by name; a numeric string still works as a legacy
  index; empty → system default). No remaining follow-up.

- ~~**FT8 Tx even/odd sequence option (caller-side).**~~ **SHIPPED 2026-06-26.**
  WSJT-X's "Tx even/1st" for **Call CQ**. Delivered as a **3-state** selector
  (operator's choice over WSJT-X's binary): **Next** (fire on the next slot
  regardless of parity — SM's fast default, ≤15s first CQ), **Even** (:00/:30),
  **Odd** (:15/:45). Daemon: `tx_parity` ("even"|"odd"|"") threaded
  `handler_ft8_qso.go` → `Service.StartCallCq` → `Sequencer.StartCallCq`, where it
  picks the CQ parity (`s.theirPeriod = oppositePeriod(ourPeriod)`); "" / unknown
  keeps the next-slot default. SPA: a **CQ slot** `<select>` in `Ft8MsgPanel` bound
  to `ft8State.txParity` (operating state in localStorage `sm.ft8.tx.parity`, like
  the offset — settled the open config-vs-operating-state question as operating
  state, no config field), locked while a session is active, sent on `cq/start`.
  **Caller-side only** — answering a CQ forces the opposite parity (ADR 0031/0032),
  so it never applies there. Tests: `TestCallerSequencer_TxParityChoice`. Docs:
  `api-endpoints.md` (`cq/start` body), `ft8.md` (Operate tab).

- ~~**FT8 Band Activity — typed display filter (token-prefix).**~~ **SHIPPED 2026-06-18.**
  Token-prefix match (any whitespace token starts with the typed text, case-insensitive),
  toMe-bypass, session-scoped (`ft8State.bandFilter`). Placement landed as a **funnel
  popover** beside the "Band Activity" header (`Ft8FilterPopover.svelte`) — also holds the
  **hide hashed-call** toggle (moved out of the Settings tab; durable, auto-saves). The
  funnel shows an active tint when either filter is narrowing the feed. `cq_to_top` stayed
  in Settings (it's ordering, not a filter). See `docs/ft8.md`.

- ~~**FT8 e4 — `TIME_ON` should be the QSO start, not the completion instant.**~~
  **SHIPPED 2026-06-20.** `CompletedQso` now carries `StartedAt`, stamped at each
  session start (`StartQso` answer-a-CQ, `StartWorkCaller`, and the Call-CQ
  answerer-selection in `onSlotCalling`). `ft8.BuildQso` (`internal/ft8/qsolog.go`)
  uses it for `TIME_ON` (and `QSO_DATE`), keeping the completion instant for
  `TIME_OFF`; a zero start falls back to the completion instant. Both stay HHMM
  (schema CHECK). Regression tests in `qsolog_test.go` (`TestBuildQso_TimeOn`).

- ~~**FT8 → PSK Reporter reception-report upload.**~~ **SHIPPED 2026-06-18.** New
  `internal/pskreporter` subsystem: IPFIX/UDP encoder (byte-exact vs the spec's worked
  example — `ipfix_test.go`) + a buffer/dedup/flush/transport service. Fed by FT8 decodes
  via `ft8.Service.SetDecodeSink` (one-way DI, like `SetQsoLogger`, so `internal/ft8`
  stays decode-only — narrow-scope holds). Per spot: sender call + grid (`ft8.SpotFrom`,
  reusing the sequencer parse; hashed/free-text skipped), **freq = dial + audio offset**,
  SNR, `FT8`, slot time, `informationSource=1`; receiver = `logging_station` call/grid +
  `StationManager <ver>` + antenna (from `MY_ANTENNA`). Dedup per call (best SNR) per window, flush
  ~5 min (program-relative + jitter), descriptors in the first 3 datagrams + hourly,
  one long-lived UDP socket (constant source port). **Opt-in: `psk_reporter.enabled`
  default OFF**, also gated on a configured receiver callsign; **best-effort, never blocks
  decode**. Host/Port default to production `report.pskreporter.info:4739`; port `14739`
  on the same host is the test server (NOT `pskreporter.info` — that's the website and
  drops UDP). Validated end-to-end against the live collector via `cmd/ft8-psk-probe`
  (received + fully parsed per `/cgi-bin/psk-analysis.pl`, 2026-06-18). **Report/upload
  side only** — the retrieve/query feed (who heard *you*) remains a separate, later item.

- ~~**FT8 Rx Frequency pane — cap the decode list + add a worked-station enrichment
  card.**~~ **DONE 2026-06-26.** The headline shipped: `Ft8EnrichmentBox.svelte`
  renders below the Rx Frequency list, keyed to `qso.theirCall` for both roles
  (answerer + caller), showing **flag · country · op name · distance · beam heading ·
  short/long-path radio** — fed from the FT8 data layer (`ft8EnrichState` +
  `bearing.ts`/`pathInfo`), NOT the Phone/CW `CountryPanel`/`enrichmentState`. The Rx
  decode list is height-capped via a fixed `h-34` scroll box (the "cap to ~3–4 rows +
  scroll" intent). The **new-DXCC `*`** was added to the card 2026-06-26 (green `*`
  after the country, matching the Band Activity marker; previewable via the `?ft8demo`
  toggle) — `info.isNewEntity` rides the same `enrichCallsign` lookup, no extra fetch.
  **Idle state — resolved WAI 2026-06-26:** the 2026-06-12 "go blank, drop the idle
  offset-decode list" decision is **superseded**. Dogfooding the live behaviour, the
  operator prefers the current idle pane — with no QSO it shows decodes on/near the
  **selected TX offset** (`rxDecodes` offset branch, ±tolerance), i.e. "what's on the
  channel I'm parked on" (useful for spotting a clear TX freq), and falls to a
  placeholder only when no offset is picked. That's the desired behaviour; no change.
  **Deferred (revisit only if there's pressure for more):** the richer field set on the
  card — DXCC prefix, worked-before tint, CQ/ITU zone, continent, QTH.

- ~~**FT8 `cmd/smd` / decode log: no WSJT-X-style `ALL.TXT` append-only decode log.**~~
  **SHIPPED 2026-06-23.** Filed from a 2026-06-22 dogfood question. Resolved by
  the `ft8.decode_log` feature (`internal/ft8/decodelog.go`): a fail-soft,
  append-only JTDX `ALL.TXT`-style writer logging both RX decodes and our own TX
  rungs, on its own queued goroutine (never blocks decode/TX). Off by default
  (grows unbounded like WSJT-X's `ALL.TXT`); default path
  `$SM_WORKING_DIR/log/ft8-all.txt`. Documented in `docs/ft8.md` §"Decode log".

- ~~**FT8 Operate pile-up: up-arrow to reorder callsigns in the answer queue.**~~
  **SHIPPED 2026-06-25.** Filed 2026-06-20. Added `ft8PileupStack.moveUp(index)`
  (swap one place toward the head; head/out-of-bounds no-op) + a left-side `↑`
  button per drawer row (`Ft8PileupDrawer.svelte`), with a spacer on the head row
  to keep callsigns aligned. SPA-only, in-memory — the operator can prioritise a
  caller without clearing the FIFO. Test: `ft8PileupStack.test.ts` ("moveUp swaps
  an entry toward the head…"). Built against the SPA-owned pile-up stack (the
  shipped `operator_pick` shape), independent of the still-pending daemon
  `caller_answer_mode: operator_pick` Call-CQ mode.

- ~~**FT8 Band Activity enrichment: flag the worked decode as a new entity.**~~
  **SHIPPED 2026-06-25.** Filed 2026-06-20; reshaped 2026-06-25 from "flag the
  actively-worked station in the enrichment display" to **mark any new-DXCC decode
  with a far-right `*` directly in the Band Activity list** (operator's call — a
  per-row marker reads better than a separate display, and it covers every decode,
  not just the one being worked). Nearly free: the `enrichCallsign` lookup the rows
  already do for the flag carries `country.is_new_entity` (the same field the
  Phone/CW `CountryPanel` uses for its "new one" `*`) — no daemon change, no extra
  fetch. Added `isNewEntity` to `Ft8CallInfo` + the flag-lookup merge
  (`ft8Enrich.svelte.ts`); rendered a far-right green `*` in the `decodeRow` snippet
  (`Ft8Panel.svelte`), coexisting with the pile-up `✓` via a second `ml-auto`. Flag
  far-left, "new one" `*` far-right, message between. Test:
  `ft8Enrich.test.ts` ("carries is_new_entity…").

- ~~**New-entity false positives — match by DXCC number, not country name.**~~
  **FIXED 2026-06-25.** Surfaced immediately after the `*` marker shipped:
  established entities (European Russia, Germany) were flagged new. Root cause —
  the `IsNewEntity` check (`lookup/orchestrator.go`) did an **exact country-NAME
  string match** against the QSO log, but hamnut resolves names
  ("Fed. Rep. of Germany", "European Russia") that differ from the QRZ-imported
  QSOs' stored names ("Germany", "Russia"); the name never matched → always "new."
  Name-matching is also lossy — it can't tell European (DXCC 54) from Asiatic (15)
  Russia. Fix matches on the **numeric ADIF DXCC code** instead: hamnut emits no
  number, only `primaryDXCCPrefix` (`UA` vs `UA9`), so a new embedded table
  (`internal/enums/dxcc`, `dxcc-entities.json`, 154 entities) maps
  `primaryDXCCPrefix → DXCC number`; the orchestrator derives the number and calls
  the new `HasQsoForDxccWithContext` (`json_extract(additional_data,'$.dxcc')` —
  the QSO's numeric DXCC lives in the blob, distinguishing the split entities).
  Falls back to the old name-match when a prefix isn't in the table, so partial
  coverage is safe. Table generated by `scripts/gen-dxcc-entities.py` (seed = log's
  distinct DXCC numbers + a sample call → hamnut prefix; collisions resolved by
  preferring the candidate whose stored name matches hamnut's, rejecting log
  misfiles). Operator override at `$SM_WORKING_DIR/dxcc-entities.json`
  (`dxcc.LoadOverride`, wired in `cmd/smd`). Also improves the Phone/CW Country
  panel `*`. Tests: `internal/enums/dxcc`, `HasQsoForDxcc`, and
  `IsNewEntity_MatchedByDxccDespiteNameMismatch` / `_DxccPath_NoPriorQso`.
  **Known limit:** the embedded table covers the ~154 entities in the dogfood log;
  a worked-but-unmapped entity with a name mismatch can still false-positive until
  its prefix is added (override file, or regenerate from a richer log).

- ~~**FT8 QSO edit overlay: remove the stray "Tune" button.**~~ **DONE (stale).**
  Confirmed gone 2026-06-26 — `QsoEditOverlay.svelte` carries no Tune control (no
  `TuneButton` import, no "Tune" reference). Operator confirmed visually.

- ~~**Config SPA: data-driven forwarder setup (`RegisterForwarderType` + `/v1/forwarder-types`).**~~
  **SHIPPED 2026-06-24.** Filed 2026-06-23. Delivered as designed: the registry
  gained `RegisterForwarderType(type, displayName, actions, credentialFields)` +
  `ForwarderTypes()`; QRZ/ClubLog declare their credential descriptors;
  `GET /v1/forwarder-types` serves them; the config SPA's **Forwarding tab**
  renders the credential form data-drivenly and writes via masked-GET/merge-PUT
  `forwarders` on `/v1/config` (secrets never echoed back). The `kind:"secret"`
  idea collapsed to `text|password` (password covers masking). **Still deferred:**
  the queue-management purge (separate item below). Original plan retained for the
  record:
  - Extend the registry (today `RegisterSupportedActions(type, actions)`) into
    `RegisterForwarderType(...)` carrying display name + supported actions +
    **credential field descriptors** (`[]{key, label, kind: text|password|secret,
    help?}`). Upload logic stays specific per type (each `Forwarder` impl is its
    own protocol — QRZ api-key POST, ClubLog two-endpoint, HamQTH session XML…);
    ONLY the setup form is data-driven.
  - Daemon exposes `GET /v1/forwarder-types`; the SPA "Add forwarder" lists them
    and renders the right credential fields on pick. Adding a 9th type in Go then
    needs **zero** SPA changes.
  - Why data-driven (not per-type hardcoded forms): the type list is long and
    growing — QRZ, ClubLog, LoTW, HamQTH, HamCall, StationMaster, qrzcq, WRL… —
    so hardcoded forms would be real duplication + a SPA edit per new type.
  - Current credential shapes to seed the descriptors: **QRZ** = `{api_key}` (one
    field); **ClubLog** = `{email, password, callsign, api}` (four — and the `api`
    is an *application* key the operator must obtain from ClubLog, NOT embedded in
    source, so its descriptor needs help text / a link). Per-field help is part of
    the descriptor for exactly this reason.
  - Reinforces fresh-install `forwarders: null` (do NOT pre-list types disabled —
    ADR 0022 enqueues by presence, so a present-but-disabled forwarder still
    accrues upload rows; the operator adds only the ones they use).

- ~~**Cross-SPA navigation links (all SPAs).**~~ **SHIPPED 2026-07-02.** From
  dogfood-inbox 2026-06-24. The top-right `ManualLink` corner (identical per SPA —
  `*/src/lib/ui/ManualLink.svelte`) now carries the full sibling switcher —
  logging ↔ config ↔ logbook — built as a `navLink` snippet with icons. App links
  navigate **same-tab** (only the Manual opens a new tab); that same-tab choice also
  fixed a real hang — new-tab nav accumulated a tab per click, each holding
  long-lived SSE, starving the browser's ~6-connection-per-host limit (see the SSE
  consolidation item). A build-env `DEV` pill also lives in the cluster. Still
  **duplicated** across the three Vite projects by design. Remaining (minor,
  deferred): a db-manager link when that SPA exists; optionally folding the three
  copies into a shared component.

- ~~**First-run setup → config SPA hand-off link.**~~ **SHIPPED 2026-06-25.** From
  dogfood-inbox 2026-06-24. Delivered as a **post-save interstitial** (operator's
  choice over an always-visible inline link): after the first-run callsign save
  flips `setupComplete`, the logging SPA holds on a "✓ Setup complete" screen
  (`app.svelte` `setup_done` snippet) offering **Open the Config app →**
  (a dumb full-page `<a href="/config/">`, no router) and a secondary **Start
  logging →** (clears the local `justCompleted` to fall through to `main_app`).
  Shown once per install — `justCompleted` is only ever set right after this
  session's save, so a returning operator never sees it. Done standalone (didn't
  wait on the cross-SPA-link component); when that shared nav lands, the link can
  be revisited for consistency.

- ~~**FT8 Spectrum view — drag-to-set the offset indicator.**~~ **SHIPPED 2026-06-26.**
  Built as designed: Pointer Events on the Spectrum bar (`pointerdown`/`move`/`up`/`cancel`
  + `setPointerCapture` so the drag tracks off-bar; mouse/touch/pen), unifying click+drag —
  pointerdown picks, pointermove refines, release commits. **Persist-on-release** via a new
  `ft8State.previewOffset` (sets `selectedOffset` without writing localStorage) for the live
  drag, with `selectOffset` (persists) fired once on release — so the whole UI (footprint,
  footer readout, Rx pane) follows the pointer live + the proximity colour updates
  clear→near→sharing as you drag past signals, but storage is written once. `touch-none` on
  the bar so a touch-drag doesn't scroll; keyboard nudge (arrows/Home/End) retained. Tests:
  `previewOffset` non-persist vs `selectOffset` persist (`ft8.test.ts`); the pure
  `offsetFromFraction`/`clampOffset` already covered. Docs: `ft8.md`. **Deferred (optional):**
  a hold-Shift "magnetic" snap to the nearest clear offset / signal edge (off by default — it
  fights the continuous ethos).

- ~~**Config SPA — FT8 decode-log toggle (`ft8.decode_log`).**~~ **SHIPPED
  2026-06-25.** Filed from dogfood-inbox 2026-06-25; built the same day. Delivered
  exactly as scoped, mirroring the session-191 PSK Reporter pattern:
  - **Daemon:** added `Ft8DecodeLog *types.Ft8DecodeLogConfig`
    (`json:"ft8_decode_log"`) to the config handler's request/response struct
    (`handler_config.go`); GET serves `cfg.Ft8.DecodeLog` (nil → disabled zero block
    so the form binds), PUT applies presence-aware (omit → untouched). Reuses the
    canonical `types.Ft8DecodeLogConfig{Enabled, Path}`. Tests in
    `handler_config_decodelog_test.go` (GET round-trip, nil-served-as-zero, PUT
    round-trip, presence-aware).
  - **SPA:** `Ft8DecodeLogFields` type (`config.ts`); `DecodeLogForm` +
    `decodeLogFormFrom`/`decodeLogFormKey` folded into `ft8Dirty` /
    `ft8RestartRequired` / `saveFt8` / `cancelFt8` (`config.svelte.ts`); a "Decode
    log" section (enable toggle + path field, default-path placeholder) in
    `Ft8Tab.svelte`, under the same FT8-tab footer + restart banner as PSK Reporter.
  - **Restart-required:** the log file opens at FT8 service start, so the FT8 tab's
    restart banner now covers it. `api-endpoints.md` updated (GET + PUT).

- ~~**Config SPA — a `General` tab for cross-cutting operator preferences.**~~
  **SHIPPED 2026-06-26.** The config SPA gained a **General** tab (last in the strip)
  as the home for prefs that don't belong to a domain tab — chosen over "System"
  (which reads as daemon/infra, deliberately config.json-only and kept OUT). Shipped
  occupants: the **`restore_rig_on_mode_switch`** toggle (checkbox → presence-aware
  `/v1/config` PUT via `saveGeneral`) and the **About/version** diagnostics (moved from
  the logging SPA's My Station → About; new config-SPA `api/version.ts`). Pending
  occupants for when their workstreams land: the **UI theme / dark-mode** picker (see
  "UI themes + dark mode") and future behaviour/notification toggles (e.g. the FT8
  notify/qso-defaults that are localStorage today but should migrate to config). The
  line holds: daemon/system internals stay out; an "advanced/system" surface, if ever
  wanted, is separate + clearly-gated.

- ~~**FT8 slot parity (odd/even) is never labelled in the UI.**~~ **SHIPPED 2026-06-27**
  (with the mixed-parity fix above — they shipped together). Band Activity rows now carry an
  even/odd badge (E sky / O purple, derived from `slotUtc` via `utils/ft8Parity.ts`), and the
  pile-up drawer header shows the run's parity. The "current TX parity readout" idea is
  effectively covered by the drawer's run-parity tag + the per-row badges; a dedicated
  session readout can be revisited if it's still wanted after on-air evaluation. Original
  note below. Filed 2026-06-27. The
  operator has to read parity off the decode's UTC seconds (SM convention, grounded in
  `scheduler.go`/`caller_sequencer.go`: `(unix/15) % 2 == 0 → even`; on the clock,
  **:00/:30 = even, :15/:45 = odd**, = WSJT-X "Tx even/1st"). Nothing surfaces it —
  not on Band Activity decode rows, not on pile-up entries, not on the Operate/ladder
  view — so the operator counts seconds in their head to tell which stations are workable
  on the current run's parity. Add a clear per-row/per-entry parity indicator (even/odd
  badge or colour) and ideally a "current TX parity" readout for the session, so the
  workable set is obvious at a glance. Each decode/`PileupEntry` already carries the slot
  time (`slotUtc`), so this is presentation-only — no daemon change. Surfaces: the Band
  Activity feed, `Ft8PileupDrawer.svelte`, the Operate view; parity derivation belongs in
  a shared SPA util (mirror the daemon's `(unix/15)%2` convention). Prerequisite-adjacent
  to the **"FT8 pile-up stack mixes odd/even parities"** bug (Bugs section) — that fix
  needs parity visible anyway; this is the display half, useful on its own.

- ~~**FT8 Call-CQ auto-pick strategy should be config-selectable (decode-order vs strongest).**~~
  **SHIPPED 2026-06-28.** Filed 2026-06-27. Realised by **folding into the existing
  `ft8.tx.caller_answer_mode` knob** rather than adding a separate `auto_pick` field —
  the new literal `auto_strongest` ranks the slot's valid answerers by SNR and works the
  highest; `auto_first` keeps decode-order. `onSlotCalling` (`caller_sequencer.go`) selects
  per `s.answerMode`. Surfaced over `/v1/config` as `ft8_caller_answer_mode` (presence-aware,
  only the two attended auto modes accepted) and editable from the logging SPA's **FT8
  Settings tab → Call CQ → Answer** (First answerer / Strongest signal). The pile-up stack
  is unaffected — it always drains FIFO; the knob governs only the automatic answerer.
  Tests: `caller_sequencer_test.go`, `handler_config_ft8_test.go`, `types/ft8_test.go`.
  Still wants **on-air validation** like the rest of the caller side.

- ~~**FT8: suppress the ctrl-click affordance on an already-queued Band Activity row.**~~
  **SHIPPED 2026-07-03.** A queued row now drops its `hover:underline` "add" cue, its
  tooltip reads "already queued in the pile-up", and a ctrl/cmd-click on it is a no-op
  with an "X is already in the pile-up" toast (was a silent in-place refresh) —
  `Ft8Panel.svelte` (`onCallerClick` guard, mirroring the sibling in-flight/session-dupe
  guards, + conditional row class/title). Original note below. Filed 2026-06-27 (dogfood),
  triaged 2026-07-03. Functional double-add is already prevented (`ft8PileupStack.push`
  refreshes an existing call in place, never appends a duplicate) and a queued row carries
  a `✓` marker — but the row still showed the clickable/hover affordance, so a ctrl/cmd-click
  on an already-queued station silently refreshed it with no visible effect.

- ~~**FT8: pile-up drawer header wraps to two lines.**~~ **SHIPPED 2026-07-03 (operator).**
  Solved by making the header `<h2>` a `flex flex-col` with two spans — "Pile-up (N) ·
  parity" on line 1 and "Paused" on its own line 2 — so the count+parity never wraps
  mid-phrase (`Ft8PileupDrawer.svelte`). An earlier `whitespace-nowrap`+`bg-gray-500`
  attempt was reverted; this is the kept fix. Original note below. Filed 2026-06-27
  (dogfood), triaged 2026-07-03. The header ("Pile-up (N) · even · paused") could wrap in
  the narrow drawer.

- ~~**QRZ enrichment resilience on flaky/contended links (7Q8AC-relevant).**~~ **CLOSED
  2026-07-05 (operator).** Fix (1) — the big one — SHIPPED 2026-07-04 and is the whole
  shipped scope; the residual ideas ((2) per-lookup retry/backoff + HTTP-timeout revisit,
  (3) don't cache a nameless result as final) are **dropped, not pending** — re-open only
  via a fresh dogfood-inbox note if flaky-link name-loss recurs in practice. Original
  entry: found on-air 2026-07-04 (7Q5MLV FT8 pile-up in Malawi, during a `dnf upgrade`
  that saturated the link — an accidental but perfect stress-test of 7Q8AC's *normal*
  bandwidth-contended state). Evidence (smd.log): `dial tcp <qrz>:443: i/o timeout` on the
  **session-key login** at daemon start → **QRZ disabled itself for the ENTIRE run**
  (`QRZ session key fetch failed; service disabled` + `chain provider disabled itself
  during init`), so **no QSO got a name** all session (hamnut country only — e.g. PY2DN
  logged `country:Brazil`, no name); plus intermittent per-lookup `i/o timeout`s in an
  earlier run. The "enrichment never blocks logging" invariant held (QSOs logged +
  forwarded fine), so this was **completeness, not correctness** — but the failure mode IS
  7Q8AC's environment. **Fix (1) as shipped:** don't permanently disable QRZ on a
  boot-time session-key timeout — `Initialize` no longer flips `Enabled=false`; the
  service stays enabled but keyless, and `ensureSessionKey` lazily re-fetches the key on
  lookups (cooldown-bounded `sessionRetryCooldown` 30s, single-flighted via `authMu`), so
  QRZ revives on its own once the link returns — no daemon restart. Tests
  `TestInitialize_SessionKeyFailureStaysEnabled` / `TestLazySessionKey_RecoversAfterBootFailure`
  / `…CooldownSuppressesRetry` / `…RetriesAfterCooldown`. **Review-hardened same day** (a
  multi-agent review found real gaps in the first cut, all fixed): the expired-key re-auth
  path routes through the SAME cooled/single-flighted `ensureSessionKey` (was bypassing it
  + leaving a stale key that hammered QRZ); `authMu` uses `TryLock` so followers fail-soft
  instead of blocking the interactive path; the login runs on a DETACHED context (a client
  disconnect no longer aborts it or caches `context.Canceled`); the cooldown is stamped at
  completion (a login ≥ the cooldown no longer lets waiters through); credential-in-log
  scrubbed (next item). Commits `431b7eca`/`e04d643a`/`25e10f84`.

- ~~**QRZ credentials logged in cleartext.**~~ **FIXED 2026-07-04** (surfaced by the review of the
  resilience fix, which noted keeping the provider in-chain amplified the leak from once-per-boot to
  once-per-enriched-callsign). `scrubURLError` (`internal/lookup/qrz/internal.go`) strips the query
  string from the transport `*url.Error` at both `client.Do` sites before it enters the logged/cached
  error path — Go's `url.Error` masks userinfo but NOT query params, and QRZ carried
  `username`+`password` (session request) and the session key (lookup) there.

## Website / public presence

- ~~**Landing page for `station-manager.org`**~~ **MVP LIVE 2026-07-02.** Domain
  purchased (10-yr reg); the static page is deployed and live — HTTPS (Let's
  Encrypt), Hugo, GitHub Pages via Actions, Namecheap DNS — in a **separate public
  repo** (`station-manager-www`, as decided 2026-06-30 to keep this repo's heavy CI
  gate off a copy tweak). Carries the positioning line ("Station Manager — free,
  open-source Linux ham-radio station management," differentiating from the paid
  **Station Master** without naming it), logo/favicon, and the GPL-3.0 + GitHub link,
  reusing the manual's Hugo identity. **Remaining (content polish, as it comes):**
  screenshots (logging / rig control / FT8), a get-it/RPM-install section (pairs with
  the download-site install-page item), a manual link, and an honest "alpha" status
  banner.

## Platform / online store (future — revisits ADR 0016)

- ~~**SM online database — durable backup now, community platform later**~~
  **DESIGNED 2026-07-02 (ADR 0040 + `docs/v2-design/sm-cloud-p1.md`); NOT built.**
  Captured 2026-07-01, designed in a full session 2026-07-02. **SM Cloud P1** =
  full-fidelity off-site backup + restore, launched single-tenant but
  multi-tenant-ready (onboard 7Q8AC next, gated on the security assessment).
  Transport = a new `smcloud` **forwarder** (upsert-by-UUID, full `types.Qso` JSON,
  ADR-0038 retry); store = **Postgres**; same repo `cmd/smcloud`; split-ownership
  directionality (content up / confirmation down at P3); soft-delete tombstone;
  reconcile on `(UUID, modified_at)`; restore = full-JSON round-trip. Phase arc: P1
  backup → P2 7Q8AC → P3 auto-confirm → P4 community. Driver: a QRZ round-trip
  destroyed local HH:MM:SS. **Revisits ADR 0016.** Next step on go-ahead = implement
  per the plan (S1–S6); NOT building yet. See ADR 0040, `sm-cloud-p1.md`, memory
  `project_sm_online_db_community`.
