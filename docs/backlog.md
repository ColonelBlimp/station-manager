# Backlog — deferred work

Bugs and enhancements that are **known but deliberately not-now**. This is the
"we'll get to it" list, not the active-cycle task list (that lives at the top of
`docs/session-handoff.md`). FT8-internal mechanics also get captured in
`docs/ft8.md`; this file is the cross-cutting backlog the operator drives.

Convention: one bullet per item, newest at the bottom of its section. Lead with
the surface (file/subsystem) so it's greppable. Strike through or delete an item
when it ships — don't let this rot into a graveyard.

## Bugs

- **`PUT /v1/config` contract: omitted blocks zeroed (+ `default_logbook.id` unwired).**
  Filed from the `internal/api` review (2026-06-14, M4 + M5). **Partially addressed:**
  M4's `default_rig_id` half shipped with the config-SPA Rigs tab (`req.DefaultRigID`
  presence-aware at `handler_config.go:375`, validated). **Still open:** `default_logbook.id`
  is still never copied (no logbook-switching consumer yet — low priority), and M5 stands —
  `logging_station`/`station` are still copied unconditionally (`handler_config.go:353-354`),
  so a PUT omitting them zeroes the operator identity (the SPAs work around it by always
  bundling the blocks). Fix together with a **presence-aware pointer-block
  PUT request type** (`*types.LoggingStation`, `*types.StationConfig`,
  `*types.Logbook`, `*types.RigConfig`, `*types.Ft8DisplayConfig`, `*BridgeInfo` —
  pointers to the canonical `types.X`, not re-defined fields): apply only present
  blocks; wire + validate the default ids (logbook active-row exists; rig id via
  config validation). Backward-compatible with the SPA. Deferred because it
  changes `/v1/config` semantics and wants its own test/readthrough pass against
  both SPAs. See `docs/reviews/internal-api-2026-06-14.md`.

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

- **Rx Frequency table shows duplicate rows when feed mode ≠ "single".** With the
  FT8 display feed mode set to anything other than `single` (i.e. `accumulate`),
  duplicate entries appear in the Rx Frequency pane. Likely the `rxDecodes` filter
  in `Ft8Panel.svelte` surfaces the same station decoded across multiple
  accumulated slots (filter is by callsign in-QSO / offset±tol idle, with no
  per-call/per-offset dedup), which reads as duplicates. Investigate: confirm
  whether it's genuine repeat decodes across slots vs. a keying/accumulation bug,
  and decide whether the Rx pane should collapse to the latest decode per station.

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

- **Multiple browser tabs share one rig with no arbitration — operating-lock needed.**
  Filed 2026-06-27 (operator flagged as a real risk to mitigate). The SPA is multi-tab:
  every tab subscribes to `/v1/rig/events` and any tab can `POST /v1/rig/command` —
  there is **no "which tab owns the rig" concept**. Command writes are serialised only
  at the CAT-line level (`bridge` `cmdMu`), and TX is single-flight (`keyMu`/`ErrTxActive`,
  shared by tune + FT8-TX) so two tabs physically can't double-key or steal the mic
  (FT8 capture is refcounted on `/v1/ft8/events` subscribers, which a Phone/CW tab
  doesn't hold). The unmitigated hazard is the **shared VFO/mode**: a frequency/band/mode
  change in one tab physically moves the one radio the other tab is operating on — e.g. a
  Phone/CW tab retuning mid-FT8-QSO. Two control surfaces, one rig, no coordination.
  Direction to design (UX-first per the rule): a daemon-tracked **active operating client
  / rig lock** — one tab "holds" the rig for operating; others go read-only (state display
  still live) or must explicitly take over; surfaced in the SPA so the operator knows which
  tab is in control. Related dogfood notes (same root — uncoordinated control during an
  active exchange): "Next during TX moves on mid-transmit", "currently-worked station still
  clickable in Band Activity". Touches `internal/bridge` (ownership/lease), the rig command
  handler (reject/queue non-owner writes), and all three SPAs (lock indicator + take-over).

- **FT8 pile-up stack mixes odd/even parities — unworkable + confusing.** Filed 2026-06-27
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

- **FT8 Call-CQ + pile-up queue = two controllers fighting over the rig.** Filed 2026-06-27
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

- **`internal/iocdi` contract hardening (concurrency + build-time validation).**
  Filed from the `internal/iocdi` review (2026-06-19, M1 + M3 + M4); deferred
  because the daemon registers single-threaded then builds, so none is exercised
  today — the fail-fast wins (M2 reject duplicate/invalid registration) + L1/L2
  shipped 2026-06-19. **M1:** registration isn't transactional — dependency
  discovery + the `built` check happen before `regMu` is taken, so a concurrent
  `Register`/`Build` can race the dependency map or accept a post-build bean. Fix:
  do the whole register transaction (check `built` → discover deps → validate →
  mutate both maps) under one lock; add a race test. **M3:** `requiredDependency`
  stores one type per bean ID, so two fields sharing a tag ID with different types
  lose one; and unexported/incompatible tagged fields are silently skipped, so
  `Build` can succeed with a nil `di.inject` dependency that fails at first use.
  Fix: store dependency edges per-field and validate EVERY tagged field at build
  (settable, bean exists, assignable) — treat tagged-but-unset as a build error.
  **NB before doing M3: confirm the current cmd/smd wiring has no latent
  unsatisfied tag, or it will turn into a startup failure.** **M4:** `Build` runs
  user `Initialize()` while holding `regMu` (an initializer that resolves/registers
  would deadlock), and a failed initializer leaves `built=false` so a retry re-runs
  the already-succeeded ones. Fix: release the registry lock before invoking
  initializers, add a `building` state, and document/enforce initializer
  idempotence. See `docs/reviews/internal-iocdi-2026-06-19.md`.

- **`/v1/hardware` per-direction audio availability + enumeration caching.**
  Filed from the `internal/hardware` review (2026-06-19, M2 + M3), deferred to the
  **config-SPA workstream** (the unbuilt consumer). **M2:** `AudioDevices` returns
  `(capture, playback, err)` but errors on EITHER direction's failure, and the
  handler collapses that to `audio.available=false` with both lists empty — so a
  playback-enumeration failure hides a working capture list (and vice versa).
  Fix: surface per-direction availability (e.g. `capture_available` /
  `playback_available` on the `/v1/hardware` response, or populate the successful
  direction + log which failed). It's a Tier-1 `api-endpoints.md` wire change, so
  shape it WITH the config SPA that consumes it. **M3:** `/v1/hardware` does
  uncached live OS/audio enumeration on every request (a fresh malgo
  context per call), with only the default 128-wide request limiter — a buggy SPA
  tab / retry loop can spin up many audio contexts. Fix: a short-TTL cache or
  singleflight around enumeration (+ maybe a lower per-route cap), with a refresh
  path if the picker needs immediate hot-plug detection. Both are hardening for
  the config SPA; the M1 wrong-codec safety guard + L2 labels shipped 2026-06-19.
  See `docs/reviews/internal-hardware-2026-06-19.md`.

- **CI-V `sets_state` value-compatibility validation.** Filed from the
  `internal/cat` review (2026-06-19, L1). `ValidateRigDefinition` rejects a CI-V
  `sets_state` that names no State marker, but it does NOT verify the command's
  *encoding* can populate that marker — so a future CI-V rigdef could pass
  validation with a mismatched pair (a `bcd_freq` command setting a mode tag, a
  `bcd_power` command setting `MAINMODE`, or a valueless command declaring
  `sets_state`), and the wait-for-ACK path (ADR 0034) would then synthesize the
  wrong/empty state push after a successful ACK. NOT an active bug — the shipped
  IC-7300 rigdef's `sets_state` pairs are all correct (pinned by
  `TestCommandSetsState`). Deferred until external/operator rigdef loading is real
  (`RegisterExternalDir` is still a stub), since that's when an unvetted rigdef
  could actually reach this gap. Fix: extend CI-V validation so `sets_state` is
  encoding-compatible (`bcd_freq`→BCD-freq marker, `bcd_power`→BCD-level marker,
  `mode_seq`→marker whose mapped values include the mode literals; valueless
  commands can't declare `sets_state`), with negative tests per incompatible pair
  + a positive test per shipped IC-7300 `sets_state` command. See
  `docs/reviews/internal-cat-2026-06-19.md`.

- **Configurable session-email subject + body (formatting tags).** Flagged
  2026-06-17 as multi-operator interest grows — a QSL manager receiving logs from
  several operators benefits from operator-tailored, distinguishable mail. Today the
  subject (logbook-callsign-prefixed default) and body ("ADIF for this session
  attached." / "Contains N QSOs." / "Generated by …") are **hardcoded** in
  `sessionEmailSubject` / `sessionEmailBody` (`internal/api/handler_session_email.go`).
  Let the operator configure both via **templates with substitution tags** — e.g.
  `{callsign}`, `{count}`, `{date}`, `{logbook}`, `{to}` — stored in `config.json`
  (durable per-operator setting, per the settings-in-config rule; natural home is
  alongside the SMTP block) and edited from the SPA email/Settings surface. Keep the
  current hardcoded strings as the **defaults**. Design points when picked up: the tag
  set + a *small, safe* substitution (no general templating engine — just `{tag}`
  replacement); and whether a **per-send free-text note** (a transient SPA input,
  distinct from the durable template) is also wanted. The callsign-prefix + QSO-count
  shipped 2026-06-17 as the hardcoded first step toward this.
- **CAT poll mode (rigdef-configurable) — deferred (ADR 0034).** The bridge read
  model (ADR 0019) is push-only: the rig broadcasts on change (Yaesu/Kenwood AI,
  Icom CI-V Transceive). Designed but deferred: an optional rigdef `poll` block
  (interval + read command) that sends `READ` on a timer and flips liveness so a
  *missed poll* — not silence — is the disconnect signal. For Icom we instead
  document **CI-V Transceive ON** as an operator prerequisite. Add the `poll`
  block (additive; push-only rigs unaffected) if an operator can't keep
  Transceive on, the bus contends with other software, or state Transceive
  doesn't broadcast surfaces. Decide one-missed-poll vs N-strikes then.
- **FT8 offset picker — daemon-side no-overlap snap + click-anywhere.**
  `Ft8OccupancyStrip` offers daemon-vetted clear offsets as discrete markers
  today; clicking arbitrary spectrum (with a daemon-side snap to the nearest
  no-overlap slot) is future work.
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
- **FT8 semi-auto response to a session watch-list — SET ASIDE 2026-06-12 in favour
  of the caller-side work-stack; hunter/auto-fire variant stays UNDER CONSIDERATION
  (grayline, NOT decided).** The 2026-06-12 stack discussion concluded the
  **caller-side pile-up work-stack** (feature above) is the path: it delivers the
  "curate calls + work a queue" benefit while staying attended — the operator pops
  each contact, no auto-fire. The watch-list's *only* unique value was the **hunter**
  case (auto-respond to a wanted CQ inside the one-slot reply window, faster than a
  human can click), and that auto-initiation is exactly what crosses into
  daemon-initiated operation. So it is parked, not dropped; the original idea is
  preserved below. Idea: the operator manually selects a set of callsigns into a
  **session-bound** list and clicks **'Go'** to arm it; when one of those calls
  then appears as a CQ in a decode, the daemon responds in the **immediate next
  slot** — using the ADR 0032 synchronised/truncated send to hit the tight
  end-of-decode → next-sequence window a human can't reliably click within.
  Technically a small delta on ADR 0032: the only new piece is swapping the
  per-QSO click for a watch-list match as the initiation trigger; sequencer,
  timing, and off-ramps already exist.
  **Attended framing + guardrails (the operator's design):** the list is
  session-bound (cleared on session end — never persistent, never unmanned), the
  operator manually picks the targets and gives an explicit 'Go', is present and
  supervising, can abort instantly (Abandon / Disarm), and there is no auto-CQ
  cycle — the human supplies the operating *intent* (the selection + 'Go'); the
  software only covers human reaction time in the reply window.
  **Open regulatory question (acknowledged grayline, still being thought through):**
  does pre-authorising a batch + auto-responding count as *attended* (the operator
  initiated the intent, analogous to WSJT-X "Call 1st") or does it cross into
  *daemon-initiated* operation (QEX §9 forbids robotic/unattended; attended-only
  stance)? Not resolved — recorded to keep thinking. **If ever built, it must be
  framed as attended-assisted; public docs must never present it as automatic
  operation.** See memory `project_sm_ft8_attended_only`.
- **FT8 callsign ignore list.** An operator-maintained list of callsigns to
  suppress in the FT8 view — already worked, not being sought, known nuisance, etc.
  Listed calls should be hidden (or clearly de-emphasised) in Band Activity and not
  offered as answerable CQ rows. Distinct from the existing *automatic*
  worked-before tint (`ft8Enrich`): this is a **manual** list with mixed reasons,
  so keep it separate from worked-detection. Open design points: (1) storage — a
  non-session setting → daemon `config.json` (per the settings-in-config rule), with
  an add/remove UX in the FT8 view; (2) behaviour — hide entirely vs grey-out vs
  just non-clickable (lean: hide, with a toggle to reveal); (3) match semantics —
  exact callsign vs prefix/wildcard. Whether it also feeds AP-hint *de*-prioritising
  (ADR 0025) is a later question, not v1.
- **FT8 — work type-4 compound calls + free-text messages.** The answer-a-CQ,
  work-a-caller, and Call-CQ paths work any station whose exchange encodes as a
  **standard structured FT8 message**; the sequencer defensively **skips** anything
  else (the dynamic "reply does not encode" guard — every site tries
  `goft8.EncodeStandardMessage` and treats an error as `ErrTxBadMessage`, in
  `caller_sequencer.go` / `sequencer.go` / `work_sequencer.go` / `EncodeWaveform`).
  - ~~**Standard `/P` variant.**~~ **SHIPPED 2026-06-18** with the **go-ft8 v0.3.4→v0.3.5**
    bump — `EncodeStandardMessage` now accepts the standard `/P` variant, so the dynamic
    guards pass it through with **no SM code change** (the encode-check seam was designed
    for exactly this). Proven offline: `internal/ft8/modulate_test.go`
    (`TestEncodeStandardMessage_Portable` + `TestModulate_RoundTrip_Portable`).
  - **Still unsupported (deliberately skipped):** **type-4 compound / nonstandard calls**
    (`PJ4/K1ABC`, `K1ABC/4`, `/MM`, …) which need the WSJT-X hashed-callsign type-1/type-2
    scheme (the 22-bit hash + shared hash table — real protocol work, gated on go-ft8
    exposing an encode path for it); and **free text** (71-bit) encode + a UX to enter it.
    Until go-ft8 supports those encodes, the skip behaviour stays correct. Capture point:
    `docs/ft8.md`; see ADR 0029 (the `EncodeStandardMessage` seam).
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
- **FT8 caller-side sequencing — BOTH flows SHIPPED; only on-air validation remains.**
  `auto_first` (Call CQ, ADR 0033) shipped 2026-06-12 and the operator-pick experience
  shipped 2026-06-17 as the SPA-owned pile-up stack (+ up-arrow reorder, session 192) —
  which **supersedes** the never-built daemon `operator_pick` Call-CQ mode (501-rejected,
  not needed). The one true remainder is **on-air validation** of the caller side + the
  session-tab logging (unit-tested + offline-encode-verified only). Detail below; this was
  the gap on 2026-06-12 when a real 7Q pile-up (DK8IF / DL9UW / …) was unworkable.
  - **SHIPPED — `auto_first` (the WSJT-X "Auto Seq" mode):** the Operate-tab **Call CQ**
    button starts a sequenced session (`POST /v1/ft8/cq/start` → `Service.StartCallCq` →
    the `Sequencer` caller mode) that calls CQ, **auto-works the first answerer** through
    report → RR73, **logs it via the e4 sink**, then resumes CQ — looping until Abandon.
    Caller ladder: `internal/ft8/caller.go` (`CallerExchange`); driver:
    `caller_sequencer.go` (`onSlotCalling`); live role-aware ladder + "Calling CQ…":
    `Ft8MsgPanel`. Config: `ft8.tx.caller_answer_mode` (default `auto_first`). **Needs
    on-air validation** — unit-tested + offline-encode-verified only so far.
  - **SHIPPED 2026-06-17 — pile-up callsign stacking (the operator-pick experience, as
    an SPA-owned FIFO).** Realised the "pick which caller to work" need via a different
    (operator-chosen) shape than the original daemon `operator_pick` Call-CQ mode:
    **Ctrl/Cmd+click** a calling-you decode in Band Activity to push it onto an in-memory
    **FIFO** (`ft8PileupStack.svelte.ts`), worked **oldest-first**; the Operate view
    **drains** it via the existing work-a-caller path (`StartWorkCaller`) whenever the rig
    is armed+idle, advancing as each contact completes, while the operator keeps adding.
    Capture (Ctrl+click) is available in **any state** (mid-QSO, disarmed — pure capture,
    no TX), which is the whole point: callers are only visible in your RX parity and the
    work-now click is gated on armed+idle, so you grab them when you see them and the SPA
    works them when it can. Drawer (`Ft8PileupDrawer.svelte`) in the Operate tab + a depth
    badge on the tab; **Abandon pauses** the drain (queue kept, Resume on the drawer);
    Clear-all + per-entry remove. **SPA-only** — daemon untouched (reuses work-a-caller +
    the `ft8-qso` idle signal). In-memory (erased on tab/browser close), mirroring
    `callsignStack`. This **supersedes the daemon `caller_answer_mode: operator_pick`
    Call-CQ mode** (still `501`-rejected at `StartCallCq`; not needed — the stack gives
    operator-chosen working for *anyone* calling you, whether or not you called CQ).
    `auto_first` Call-CQ stays as the hands-off "call CQ + auto-work answerers" loop.
  - **Attended either way:** operator initiates by calling CQ, is present, Abandon stops
    it instantly; **no auto-CQ cycle, no auto-fire-on-watch-match** — which is why this
    **supersedes the auto-responder framing** of the watch-list item above.
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
- **Spot-submitter registry — generalize when a 2nd destination lands (e.g. DX cluster).**
  PSK Reporter is the first "submit what I heard" destination. A **DX cluster spot submit**
  (telnet/TCP to a cluster node, e.g. announce a worked/heard DX) would be a second — at
  which point it's a natural fit to extract a **spot-submitter interface + registry**,
  mirroring `internal/forwarding` (Forwarder interface + `init()`-registered destinations)
  and the lookup-provider chain: one decode-sink fans out to N registered submitters, each
  with its own config/enable/transport. **Deliberately NOT done now** ("build specific, not
  generic" — the v1 `internal/adapters` cautionary tale): one destination doesn't justify a
  framework, and the cluster transport/semantics (TCP, often selective/manual announce) differ
  enough that the abstraction should be designed against **two** real implementations, not
  one. The current `pskreporter.Service` (AddSpot/Flush/Start/Stop) is shaped so the extraction
  is clean when the DX-cluster submit actually arrives. Trigger: that second destination.
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
- **FT8 main-panel footer → an info strip (rehome the offset readout there).** The
  bottom-of-main-panel footer now holds "Next slot in Ns · even/odd" (added with the
  countdown move). Grow it into a small **info / status strip** and relocate the
  **"Offset N Hz ±tol"** readout into it — today that's the `rxCaption` under the Rx
  Frequency column (`Ft8Panel.svelte` ~line 282). **Pairs with the Rx-pane redesign
  above:** that pane becomes the worked-station enrichment card / blank empty-state, so
  its caption ("Offset N Hz ±tol" / "No offset selected") needs a new home — this strip
  is it. (The caption's "Following <call>" variant is subsumed by the enrichment card,
  so really only the idle offset readout moves.) Net: one tidy status strip along the
  panel bottom — next-slot countdown · parity · selected TX offset — leaving the three
  top panes (Main Freq / Band Activity / Rx-now-enrichment) uncluttered.

- **Install + first-run onboarding is too high-friction for non-Linux operators.**
  Filed from the 2026-06-23 clean-DB dogfood deploy. `docs/install.md` walks the
  operator through detailed manual rig configuration; for a non-experienced Linux
  user that's too much. North-star (KISS): the daemon discovers serial/audio
  devices and offers friendly labels, the SPA picks — the operator never
  hand-edits `config.json` or types hardware ids (extends the rig-profiles
  direction, ADR 0028). Scope is the whole onboarding arc — install, first-run
  rig setup, identity — not just the doc. Design initiative; pairs with the
  fresh-install config-defaults bug above.

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

- **Download-site install page (derive from `docs/install.md`).** Filed
  2026-06-23. The operator manual deliberately omits install/uninstall (ADR
  0036 arc starts at First Run; the embedded manual is unreachable pre-install
  anyway), so the public download site needs its own install page. Make it a
  lightly-edited operator-friendly rendering of `docs/install.md` (§1–3 install
  + enable, §10 uninstall) so the two don't drift — install.md stays the single
  canonical source. External/website work, out of this repo.

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

- **Clear the queued-upload backlog for a forwarder (esp. a disabled one).**
  Filed 2026-06-23. The shadow side of ADR 0022's enqueue-by-presence: a
  configured-but-disabled forwarder silently accumulates `pending` `qso_upload`
  rows, so *enabling it later flushes the whole backlog*. The operator needs a
  deliberate way to **discard that queue** ("don't upload the backlog, start
  sending from now"). Design points:
  - Daemon op: purge `pending`/`failed` `qso_upload` rows for a named forwarder;
    **never touch `uploaded`/`success` rows** (those are real upload history).
    Per-forwarder scope (not global). Safe for a disabled forwarder (worker idle,
    no race); for an enabled one, coordinate with the in-flight batch.
  - UX home: forwarder management in the **config SPA** — show "N queued for
    upload · [Clear queue]" next to each forwarder, with confirmation. Pairs with
    the enable/disable toggle (consider offering "clear queue?" when disabling).
  - Consider recording the purge in the audit trail (`qso_history`) so a cleared
    backlog is explainable later.
  - **Document the underlying behaviour for operators** (manual forwarding
    chapter, currently a stub): adding a forwarder queues *future* QSOs; disabling
    doesn't stop the queue growing, only the sending; this is the lever to empty
    it. The inverse (bulk-forward an existing log to a newly-enabled service) is
    the separate backfill feature.

- **Operator / user profiles — selectable op-identity bundles.**
  Filed 2026-06-24. Surfaced during the config-SPA Station-tab split: the
  **operational op-identity pair** (`Operator` callsign + `Operator Name`, and
  possibly `Owner's Callsign`) stays in the logging SPA precisely because it
  swaps per session — in a contest or multi-op you change operator mid-event.
  The future enhancement: turn that pair into **named, selectable profiles** the
  operator picks at session start instead of retyping (e.g. a dropdown of saved
  operators on the logging My Station / QSO surface). Daemon owns the profile
  list (config.json), SPA picks. Keep the current single-pair fields working;
  profiles layer on top without foreclosing today's split. Not scoped/started —
  a direction, not a commitment. Depends on the config-SPA workstream existing
  first (that's where profile CRUD would live).

- **UI consistency across SPAs — shared theme layer.**
  Filed 2026-06-24. The config SPA's tab shell was built with plain Tailwind
  utilities, because the logging SPA's theme tokens (`bg-focus`, `text-surface`,
  `outline-line`, `text-ink`) and shared classes (`.btn`, `.btn-primary`,
  `.tab-item`) live only in `frontend/logging/src/styles/app.css`. That's a small
  but real visual divergence between the two clients. Operator wants the SPAs to
  look like siblings: lift the theme tokens + shared component classes into a
  **layer both SPAs import** (a shared CSS / Tailwind theme package), then
  re-skin the config SPA onto it. Natural to do **with the UI-themes / dark-mode
  work** (see dogfood-inbox 2026-06-24) — a shared theme is the prerequisite for
  theming all SPAs at once. Likely also revisits the logging SPA's own layout for
  consistency. Not now — flagged for when the theming workstream starts.

- **Operator email address — config Station tab field (needs a daemon home).**
  Filed 2026-06-24. Surfaced while building the config-SPA Station tab: there's
  no operator/station email field anywhere today. It **can't ride
  `logging_station`** — that block strictly follows ADIF, and ADIF has no
  `MY_EMAIL`. So a working field needs a small daemon-side home; the leading
  option is a new SM config string **`operator_email`** (served on `/v1/config`
  GET, set via PUT, echoed like the rest), with the input dropped into the
  Station tab's Postal-address section. Rejected reusing `mailer.default_recipient`
  (that's "where session-log emails go," not the operator's contact address —
  don't overload). Related context (operator): **QRZ.com exposes an email address
  and uses it to populate the ADIF `EMAIL` field** — note that ADIF `EMAIL` is the
  *contacted station's* address (already modelled at `types.ContactedStation.Email`
  + filled by QRZ enrichment), distinct from the operator's *own* email this item
  is about. Worth checking, when this lands, whether the operator email should
  also flow anywhere outbound (e.g. forwarder profiles) or stays purely local
  contact info. Deferred — no concrete consumer yet.

- **Outbound UDP telemetry stream (WSJT-X-protocol-compatible).**
  Filed 2026-06-24, prompted by an external query about feeding WSJT-X data into
  Prometheus/Loki/Grafana. Idea: SM **emits** a UDP datagram stream of its FT8
  decodes, QSO-logged events, and rig status, the way WSJT-X's UDP Message
  Protocol does — so the existing ham tooling ecosystem (GridTracker, JTAlert,
  Grafana/Prometheus exporters, the operator-observability stacks people build)
  works against SM as the FT8 engine for free. Turns SM from a walled garden into
  a first-class citizen of the UDP-consuming tooling world; a real interop
  differentiator.
  - **Building blocks already exist:** `internal/events` (the hub) + the SSE
    surface (`/v1/rig/events`, `/v1/ft8/events`, `ft8-logged`, `rig-state`)
    already produce + fan out exactly this data — a UDP emitter is just another
    hub sink. `internal/pskreporter` already proves the outbound-UDP,
    never-block-the-decode-loop pattern (fire-and-forget, I/O off the decode
    goroutine). Architecturally a clean opt-in egress subsystem beside
    bridge/ft8/pskreporter; config-gated, default off.
  - **The decision (wants an ADR when it lands):** emit the **WSJT-X UDP protocol**
    (Qt QDataStream, big-endian, the Status/Decode/QSOLogged schema) for instant
    ecosystem compatibility — vs an SM-native JSON-over-UDP that's trivial to
    build but nothing consumes. Lean WSJT-X-compatible; the whole value is riding
    existing tooling. Cost: implement + MAINTAIN QDataStream encoding against a
    schema that's WSJT-X's to change (a maintenance tail — hence the ADR).
  - **Hard constraints to bake in:** (1) **emit-only** — expose only the OUTBOUND
    subset; the WSJT-X protocol's inbound control side (Reply / HaltTx / Replay)
    is a remote-rig-control surface that collides with the attended-only FT8
    invariant and the existing daemon-owned command path. Telemetry out, never
    control in. (2) **never block** decode/TX — same discipline as PSK Reporter
    (send off the decode path, drop on a full buffer, fail-soft).
  - Meaty subsystem; **later**, after SM is releasable. Closes the loop with the
    external WSJT-X→Grafana request (they could point existing WSJT-X-consuming
    tools straight at SM).

- **Cross-SPA navigation links (all SPAs).** From dogfood-inbox 2026-06-24. The
  three SPAs (logging at `/`, config at `/config/`, logbook at `/logbook/`) have
  no links between them — the operator hops by editing the URL. Add a small nav
  affordance (the header right-side slot is already reserved in each app shell)
  linking to the sibling SPAs + the future DB manager. Keep it dumb: static
  `<a href>`s to the known mount paths, no router. Decide the set once (logging ↔
  config ↔ logbook ↔ db-manager) so all three share one component. Small, but
  cross-cutting (touches every SPA shell), so batched here rather than done
  piecemeal.

- **UI themes + dark mode (all SPAs).** From dogfood-inbox 2026-06-24. Today the
  SPAs are light-only with hardcoded Tailwind colour classes (`text-gray-700`,
  `bg-white`, …). Wants a theme system: at minimum a dark mode, ideally
  operator-selectable themes. The real cost is that colours are inline across
  every component — a theme system needs them routed through CSS variables /
  Tailwind theme tokens / a `dark:` pass first, which is a broad refactor touching
  all three SPAs. Largest of the dogfood UI items. The theme **choice** is a
  durable setting → daemon `config.json`, not localStorage (per the settings-in-
  config rule); the FT8 highlight colours already live there, so a `display`/
  `theme` config block is the natural home. Do the colour-token refactor as its
  own pass before wiring the toggle. The theme **picker** belongs on the config
  SPA's new **`General` tab** (see "Config SPA — a `General` tab" below), alongside
  the other cross-cutting preferences.

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

- **FT8 occupancy — multiple switchable spectral views (channelised strip +
  waterfall).** Filed from dogfood-inbox 2026-06-25; rationale + direction settled
  2026-06-26. **Decision: provide BOTH the current channelised strip AND a scrolling
  waterfall, switchable** — not one or the other. Today TX-offset selection is a
  per-slot **data** view: `Ft8OccupancyStrip` lays the passband out horizontally with
  busy bands shaded + daemon-vetted clear offsets as clickable markers (deliberately
  *not* a render — CLAUDE.md).

  **Why the waterfall matters (the load-bearing rationale — operator's insight):**
  SM is, as far as we know, the **only FT8 app that channelises the offset**. On the
  air FT8 is *continuous and overlap-tolerant* — stations sit at arbitrary audio
  offsets (6.25 Hz tone spacing, ~50 Hz wide), the decoder processes the whole
  passband, and two close/partially-overlapping signals routinely BOTH decode (strong
  LDPC FEC + Costas sync; a real collision needs them within ~a signal-width *and*
  comparable strength). Channelising a continuous space has two costs: (a) the band
  **looks fuller than it is** (a neighbour that merely touches your nominal window
  shades the channel), and (b) the binary **red "occupied"** manufactures operator
  *guilt* — "why did they come onto my channel" / "am I transmitting on top of
  someone" — when in FT8 sharing offsets is normal, low-stakes, and usually fine. A
  waterfall shows the **continuous truth** and lets the operator exercise *judgment* —
  see real gaps, **straddle** between signals, pick a spot that's "clear enough." That
  is a genuine capability the channel markers can't give (channelising stays the right
  frictionless default for the common "click a green one" case; the waterfall is the
  complementary expert view).

  **Two distinct strands (weigh separately):**
  1. ~~**Soften the occupancy semantics.**~~ **SHIPPED 2026-06-26 as the switchable
     "Spectrum" view.** Rather than alter the channelised strip in place, this landed
     as a **second, switchable presentation** (Channels | Spectrum toggle in
     `Ft8OccupancyPanel`, operating state `ft8State.occupancyView`): the Spectrum view
     (`Ft8OccupancySpectrum.svelte`) shows signals as soft shading at their **true
     continuous positions** (no cells), the daemon clear offsets as ▾/★ ticks at real
     positions (aligned with the Clear Offsets list), **click-anywhere** continuous
     offset pick, and a **graded clear / near / sharing** status (neutral status words,
     no advice — the operator judges) instead of binary red — directly killing the
     false-full + TX-guilt. Pure logic in
     `lib/utils/ft8Spectrum.ts` (`signalProximity`/`offsetFromFraction`, tested). Both
     views write the one `selectedOffset`. The grading is **position-only** (`Ft8Band`
     has no strength — loud-vs-weak needs the waterfall's FFT magnitudes). Docs: `ft8.md`,
     CLAUDE.md FT8 bullet.
  2. **The waterfall itself (rich — the continuous view).** Feasibility assessed
     2026-06-26: **the browser render is NOT the bottleneck** and the "JS redraw is
     slow vs C/Go" worry is misplaced *if* done right — Canvas 2D self-blit scroll
     (`drawImage(canvas,0,1)` + one `putImageData` row), sub-ms/frame, proven by every
     web SDR (WebSDR/KiwiSDR/OpenWebRX) at far higher data rates than FT8's ~3 kHz /
     ~10 fps. **DOM-per-cell would be catastrophic — never do that.** The FFT stays in
     Go (the browser does zero numeric work — it rasterises pre-computed magnitude
     rows). **The real work/cost is daemon-side:** a sub-slot FFT cadence (~10 fps vs
     today's once-per-15s — ~150× more FFTs, still cheap absolute, but the exact trigger
     to revisit PocketFFT for the occupancy/waterfall FFT — memory
     `project_sm_realfft_stays_pure_go`), a streaming push channel (~8 KB/s of quantised
     rows — binary WebSocket cleaner than text SSE), demand-driven (only while the view
     is open), plus scaling/contrast (AGC) + slot-time gridlines for readability.
     **De-risk by spiking the Canvas render with synthetic rows first** (an afternoon,
     no daemon work) before building the FFT-streaming pipeline.

- **FT8 Spectrum view — colour revision.** Filed 2026-06-26. The Spectrum occupancy
  view (`Ft8OccupancySpectrum.svelte`) shipped with first-pass colours: soft slate
  shading for signals, green/amber/orange-red footprint by proximity (clear/near/
  sharing), indigo/amber ▾/★ offset ticks. Operator wants these revised (palette TBD)
  — likely tighten the proximity ramp + the signal-vs-pick contrast, and reconcile with
  the eventual shared theme layer / dark-mode work (the FT8 highlight colours are
  already operator-configurable daemon config `ft8.display`; consider whether the
  Spectrum palette should join that or stay component-level). Cosmetic; no logic change.

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

- **LSPA → My Station → Location: future POTA fields.** Filed from dogfood-inbox
  2026-06-25, alongside the Location-tab field trim (the trim itself is the
  active phase-2 LSPA cleanup, not backlog). Once the Location tab is reduced to
  Grid Square / Altitude / Lat / Lon, the future addition is **POTA fields**
  (park references — `MY_SIG`/`MY_SIG_INFO` ADIF, or POTA park id). Sibling to
  the already-deferred IOTA/POTA/SOTA bucket (memory
  `project_sm_adif_my_star_buckets`). Not scoped — a placeholder so the "future
  add POTA" intent isn't lost when the trim lands.

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

- **Config hot-reload — apply changes without a daemon restart (whole-thing review).**
  Filed 2026-06-26. Today most config is **restart-only**: a subsystem binds its config
  at boot, so changing it needs a full `smd` restart (the config SPA shows a
  "restart required" banner). The friction the operator hit: **add a rig, then switch the
  active rig to it** — `rigs` + `default_rig_id` are restart-bound (ADR 0028 Phase 1: rig
  switch = edit `default_rig_id` + restart), so a perfectly routine "I just added my new
  rig, make it active" is a restart. Same for the bridge, forwarders, FT8/PSK/decode-log,
  SMTP. Meanwhile *some* config already applies **live** (My Station identity, `ft8_display`
  prefs — re-read by the SPA after a `/v1/config` PUT). So the ask is a **whole-thing
  review of what can/should hot-reload vs stay restart-only**, then make the high-friction
  ones live — **rig add + active-rig switch first** (re-open the bridge against the new
  active rig without dropping the process; the demand-driven FT8 capture + the bridge
  supervisor already tear down / reopen, so a "reconfigure + re-acquire" path is plausible).
  Context: this is `config.md §11` (hot-reload, previously deferred *to* the config-SPA
  workstream) made concrete by real dogfooding. Open design points: which blocks are
  safe to swap live (rig/audio device re-acquire, serial reopen) vs genuinely need a
  restart (e.g. server listen address); how the SPA signals "applied live" vs "needs
  restart" per block; and the daemon mechanism (a reload/reconfigure entry point per
  subsystem vs a coarse re-init). **Not now — recorded as a whole-area initiative.**

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

- **Version display in a more permanent place (e.g. tab title) across all SPAs.**
  Filed 2026-06-26 (the second half of the About→config inbox note). The build version
  now lives on the config SPA's General-tab About section, but the operator wants it
  more *ambient* — surfaced consistently across all three SPAs without opening a tab,
  e.g. the browser **tab title** (`<title>Station Manager — Config (2.0.0-…)`), a footer,
  or a header chip. Small per-SPA; do it with the cross-SPA shared-shell / nav work so
  the three stay consistent. Source is `GET /v1/version` (`daemon` field).

- **Logbook SPA — the management surface (beyond browse).** Filed 2026-06-26. The
  logbook SPA's QSO **browse** (selector + cursor-paged read-only table) shipped
  2026-06-26; the richer logbook-management UX — present in the operator's v1 reference
  app (`7Q-Station-Manager.20250823/logbook-app`) and flagged by the logging-vs-logbook
  scope memory — is deferred. Each is its own pass (design the UX first, per the
  design-SPA-UX-before-building rule):
  - ~~**Per-row QSO edit**~~ **SHIPPED 2026-06-27** — an Edit button per row opens a
    modal seeded from the row; Save PATCHes `/v1/qso/{uuid}` and replaces the row in
    place. Form covers the editable fields (date/times, call, freq+band, mode/submode,
    RST sent/rcvd, country, name, grid, comment); the daemon restores immutables,
    re-derives band from freq, and re-validates (a bad edit returns a message, modal
    stays open). ESC cancels, Ctrl/Cmd+Enter saves. `EditQsoModal.svelte` +
    `api/qso.ts` (`patchQso`) + edit orchestration in `logbook.svelte.ts`.
  - ~~**Multi-select (selection mechanism)**~~ **SHIPPED 2026-06-26/27** — first-column
    row checkboxes + a header select-all (indeterminate when partial), selection
    persists across pages (keyed on QSO id), cleared on logbook switch; a "N selected ·
    Clear" indicator. The **bulk ACTIONS** it feeds are still deferred:
    - **Export (selected / all) as ADIF** — reuses the session email-out's server-side
      ADIF rebuild from `{uuids[]}`, or a dedicated export endpoint / download.
    - **Send selected by email** — the session email-out endpoint already takes
      `{to, uuids[]}`; generalise it to an arbitrary selection.
    - **Upload selected to online services** — bulk-enqueue chosen QSOs to forwarders.
      This is the operator-driven **backfill** lever (ADR 0022 enqueues *future* QSOs by
      presence; retrospective upload of an existing log is explicitly this app's job —
      see the forwarder-enqueue memory + the "clear queued-upload backlog" item, its
      inverse).
  - **Search / filter** — by callsign / date range / band / mode / country. Needs new
    daemon query params on the QSO-list endpoint (today it's cursor paging only, no
    filters) — a wire + validation + test change, so design it WITH the SPA.
  - **QSL-awaiting view** — filter to QSOs flagged for QSL (e.g. `app_sm_request_qsl` /
    QSL status) for card/label workflows.
  - **Edit-history viewer** — surface the `qso_history` append-only audit table for a
    QSO (who/what/when changed) — read-only forensics.
  - **Logbook management** — create / rename / delete logbooks from the UI (daemon
    endpoints exist: POST / PATCH / DELETE `/v1/logbook`; DELETE refuses a non-empty one).
  Build order for what remains: search/filter → bulk actions (export/email/upload) →
  QSL-awaiting → edit-history → logbook CRUD (edit + the selection mechanism are done).
  The reference app is a UX guide, not a port (it's Wails + page-number paging; SM is
  HTTP + cursor paging + its own utils/tokens).

- **FT8 slot parity (odd/even) is never labelled in the UI.** Filed 2026-06-27. The
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

- **FT8 Call-CQ auto-pick strategy should be config-selectable (decode-order vs strongest).**
  Filed 2026-06-27. In `auto_first` the daemon picks the next answerer by **decode order** —
  the first grid-reply to our call that we can encode a reply to (`caller_sequencer.go:109-139`;
  `m.SNR` is read only for the report we *send*, never for selection). From the operator's
  seat that looks arbitrary ("random") and ignores normal pile-up etiquette (work the
  strongest / cleanest copy). Add a config knob to choose the auto-pick strategy: **first
  in the decode list** (current behaviour) or **strongest signal** (rank the slot's valid
  answerers by SNR, take the highest). New field on `Ft8TXConfig` (`internal/types/ft8.go`,
  e.g. `ft8.tx.auto_pick = "decode_order" | "strongest"`, resolved with a default +
  validation like the other tx fields); the pick loop in `onSlotCalling` selects per the
  resolved strategy instead of always taking the first match. Independent of `operator_pick`
  (which is whom-to-work-next *by hand* — parked); this only governs the *automatic* pick.

## Scope notes (NOT backlog — recorded so they aren't mistaken for it)

- **FT8 automatic / unattended sequencing is OUT OF SCOPE and unsupported** — the
  QEX FT8 specification forbids automatic operation and it is licence-restricted
  in many jurisdictions. SM is attended-only. This is not a roadmap item.
- **"Design our own sequencing/timing" — future thinking (flagged 2026-06-12).**
  Operator wants to revisit, later, whether SM grows its own sequencing/timing
  design rather than mirroring WSJT-X's. Hard constraint to carry into that
  conversation: anything on the air as *FT8* must stay protocol-interoperable —
  the Costas sync, 15 s cadence, and 0.5 s nominal start are protocol, not SM
  choices; a genuinely new mode would need its own Costas arrays (per the QEX
  licence restriction on non-conforming streaming) and would not be "FT8". So the
  open design space is SM's own sequencer *architecture / policy / UX*, not the
  on-air timing of standard FT8. No action now — recorded so it isn't lost.
