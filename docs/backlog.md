# Backlog — deferred work

Bugs and enhancements that are **known but deliberately not-now**. This is the
"we'll get to it" list, not the active-cycle task list (that lives at the top of
`docs/session-handoff.md`). FT8-internal mechanics also get captured in
`docs/ft8.md`; this file is the cross-cutting backlog the operator drives.

Convention: one bullet per item, newest at the bottom of its section. Lead with
the surface (file/subsystem) so it's greppable. Strike through or delete an item
when it ships — don't let this rot into a graveyard.

## Bugs

- **`PUT /v1/config` contract: default ids ignored + omitted blocks zeroed.**
  Filed from the `internal/api` review (2026-06-14, M4 + M5). M4: the handler
  accepts `default_logbook.id`/`default_rig.id` (documented + in the SPA's
  writable type) but never copies them into the candidate — a silent no-op. M5:
  `logging_station`/`station` are copied unconditionally from the request, so a
  PUT omitting them zeroes the operator identity (the SPA works around it by
  always bundling the blocks). Fix together with a **presence-aware pointer-block
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

- **Fresh-install `config.json` is sparse + can carry a dangling `default_rig_id`.**
  Filed from the 2026-06-23 clean-DB dogfood deploy. A newly generated config
  (1) omits defaultable keys instead of materializing them —
  `bridge.timeouts.liveness_ms` absent, `ft8` carries only `enabled`/`enable_osd`,
  and `forwarders` is `null` rather than the supported set listed-but-disabled;
  and (2) `internal/config/migrations.go:67` sets `default_rig_id = 1` even with
  zero rigs, which `validate.go:201` lets pass because it only checks the pointer
  resolves when `len(rigs) > 0`. Downstream symptoms from the same clean install:
  no rig configured → the Phone/CW panel shows no Rig display at first startup,
  and the `qsl` defaults block reads empty (clarify whether the complaint is the
  null JSON field or a blank UI). Needs a first-run decision: full-scaffold a
  complete annotated config vs. keep it minimal but fix the dangling-id
  validation + add SPA empty-states. Closely tied to the install-friction item
  under Features.

## Features / enhancements

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
- **`ft8.device` name-matching.** Config takes an integer device index from
  `ft8-capture-probe -list`; matching by device *name* (stable across reorder)
  is a noted follow-up.
- **FT8 Tx even/odd sequence option (caller-side).** WSJT-X's "Tx even/1st": let
  the operator choose which slot parity to transmit in when **calling CQ** (even =
  `:00/:30`, odd = `:15/:45`). Today the single-shot Call CQ (`TransmitNext` →
  `TransmitSlot`) fires on the *very next* boundary regardless of parity; the option
  would wait for the next slot of the chosen parity. **Caller-side only** — when
  *answering* a CQ the parity is forced opposite the worked station (ADR 0031/0032
  already correct), so this never applies there. Belongs with the deferred call-CQ
  caller-side scope, but the toggle is small enough to add to the single-shot
  button independently. Open design point: **persistent setting** (`config.json`)
  vs **operating state** (session toggle, like the selected offset) — WSJT-X treats
  it as a live checkbox; lean operating-state with maybe a config default.
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
- **FT8 caller-side sequencing — `auto_first` SHIPPED 2026-06-12 (ADR 0033); the
  `operator_pick` stack remains.** When *we* call CQ and stations answer, work them one
  at a time, looping the pile-up until Abandon. This was the gap on 2026-06-12 when a
  real 7Q pile-up (DK8IF / DL9UW / …) was unworkable.
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
- **FT8 Rx Frequency pane — cap the decode list + add a worked-station enrichment
  card.** The Rx Frequency column (`Ft8Panel.svelte`, `rxDecodes`) renders a tall
  scrolling decode list that earns little mid-QSO — the worked station transmits once
  per cycle, so the last ~3 of *their* messages is all that matters. Two changes:
  - *Cap the list to ~3–4 visible rows + scroll.* Also quietly de-fangs the B3
    "duplicate rows in accumulate" bug (far less surface to look wrong).
  - *Use the freed space for a compact worked-station enrichment card,* keyed to the
    current contact. **Caller or answerer:** key on *the current worked station*
    (`qso.theirCall` while `qso.active`), so it serves answer-a-CQ today and the
    caller-side pile-up stack later — one component, both roles, no rework when the
    stack lands.
  - *Reuse the DATA layer, not the stateful component.* We already hold the worked
    station's **grid** (from the CQ / their reply), so bearing + distance are free via
    `bearing.ts` / `pathInfo`. `/v1/enrich/callsign` gives flag / country / DXCC /
    CQ+ITU zone / continent / name / QTH; `ft8EnrichState` gives worked-before. Feed a
    small FT8 card from those endpoints — do **NOT** reuse the Phone/CW `CountryPanel` /
    `enrichmentState`, which is coupled to the manual draft + ANT_AZ path-selection and
    would tangle the two flows.
  - *Field set (lean, DX-focused):* flag · country · DXCC prefix; grid → **bearing +
    distance** (short/long — the FT8 DXing headline); **worked-before** on band+mode
    (the dupe tint already computed); CQ/ITU zone · continent; name/QTH secondary.
    (NB **per-CQ-row beam heading already shipped 2026-06-12** — Band Activity shows a
    short-path bearing on each CQ row via `pathInfo`, for pre-aiming before you answer.
    This card is the *persistent worked-station* view during an active QSO — distance +
    long-path + country, keyed on `qso.theirCall` — not a duplicate of the per-row one.)
  - *Idle state (DECIDED 2026-06-12):* when no QSO is active the pane is **blank —
    mirror the Phone/CW `CountryPanel` empty state** (same placeholder presentation, so
    the two modes feel consistent). The idle offset-decode list is **dropped** (the
    operator found it low-value); the pane exists to show the *current contact*, so no
    contact → blank. During an active QSO it shows the capped 3–4 row list of the
    worked station's messages + the enrichment card. (Presentation mirrors
    `CountryPanel`; data still comes from the FT8 data layer, not its stateful
    `enrichmentState` — see the reuse note above.)
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

- **FT8 `cmd/smd` / decode log: no WSJT-X-style `ALL.TXT` append-only decode log.**
  Filed from a 2026-06-22 dogfood question. SM does not write an `ALL.TXT`
  equivalent today; the FT8 wrapper logs each decode to the **daemon log** only.
  Decide: treat the daemon log as sufficient (non-issue) or add a real
  append-only per-decode log file (and document where it lives).

- **FT8 Operate pile-up: up-arrow to reorder callsigns in the answer queue.**
  Filed 2026-06-20. Add a left-side up-arrow on each pile-up entry to move a
  callsign up the list. Belongs with the `operator_pick` pile-up stack workstream
  (ADR 0033, currently pending — see the FT8 caller-side notes).

- **FT8 Band Activity enrichment: flag the worked decode as a new entity.**
  Filed 2026-06-20. When a decode is selected/being worked, indicate in the FT8
  enrichment display whether it's a new DXCC/entity. Extends the existing CQ
  enrichment (flag + worked-before tint) to the actively-worked station.

- **FT8 QSO edit overlay: remove the stray "Tune" button.** Filed 2026-06-20.
  The Tune control doesn't belong in the FT8 QSO edit overlay. Small UI removal.

- **Download-site install page (derive from `docs/install.md`).** Filed
  2026-06-23. The operator manual deliberately omits install/uninstall (ADR
  0036 arc starts at First Run; the embedded manual is unreachable pre-install
  anyway), so the public download site needs its own install page. Make it a
  lightly-edited operator-friendly rendering of `docs/install.md` (§1–3 install
  + enable, §10 uninstall) so the two don't drift — install.md stays the single
  canonical source. External/website work, out of this repo.

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
