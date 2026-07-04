# Backlog — the definitive ranked worklist

**Single purpose:** the one authoritative, priority-ordered list of every
triaged, not-yet-done piece of cross-cutting work. If it's real work and it
isn't being done *right now*, it lives here — ranked. This is where "what's
next, and in what order" is answered.

**How it relates to the other docs** (full map: `docs/README.md`):
- Raw, un-triaged notes jotted mid-operation live in `docs/dogfood-inbox.md`
  and **graduate here** once triaged (or are struck there as non-issues).
- The 1–3 items in flight **right now** live in `docs/session-handoff.md`'s
  active-cycle list, pulled from the top of this file — that doc does NOT
  re-rank the queue; **this file owns the ranking.**
- FT8-internal implementation mechanics live in `docs/ft8.md`; only
  *cross-cutting* FT8 work lands here.
- Shipped / resolved / ruled-out items are **moved** to `docs/backlog-archive.md`
  (NOT read at session start) so this file stays lean. Open the archive only when
  you need an item's history.

**Conventions:**
- The **Worklist index** below is the ranked table of contents — one terse line
  per item. The **detail** for each lives in its full entry further down; lead
  each entry with the surface (file/subsystem) so its title is greppable.
- When an item ships, **move its detail block to `backlog-archive.md`** and drop
  its index line — do NOT strike it in place. A `~~struck~~` top-level item left
  in this file is a bug: relocate it. (Struck *sub-bullets* inside a still-open
  item are fine — they show which sub-steps of live work are done.)
- Tiers: **P0** correctness/safety · **P1** finish in-flight · **P2** next
  features (by workstream) · **P3** deferred/large/needs-trigger · **Designed
  workstreams** (built on go-ahead) · **Parked** (not work).

## Worklist index (ranked) — the definitive "what's next"

> **▶ Active cycle (set 2026-07-04): _Get to the next shippable state for 7Q8AC._**
> The goal is a release the external operator (7Q8AC, Malawi, offline-first) can run;
> "stabilise & finish in-flight" is the means. Clear P0 + the P1 finish/validate items
> — they ARE the ship gate — before opening any new P2 workstream. Theming (P2 ·
> _UI cohesion_) is deliberately **not now**. The live active-cycle checklist is in
> `docs/session-handoff.md`; this index is the full ranked queue.

**P0 — correctness / safety (do first)**
- _None currently open._ (Last cleared: `PUT /v1/config` omitted-blocks-zeroed, FIXED 2026-07-04 → archive.)

**P1 — finish in-flight / validate (small; closes open arcs)**
- FT8 caller-side sequencing (Call CQ pile-up) — on-air validation (shipped)
- Behavioural retest of shipped daemon changes on the dogfood daemon (session 192/193 batch)

**P2 — next features (open one workstream per active focus)**
- _UI cohesion:_ shared theme layer (token convergence) → UI themes + dark mode → FT8 Spectrum colour revision · version-in-tab-title
- _FT8:_ type-4 compound + free-text · attempt-limit SPA control · callsign ignore list · Call-CQ waiting feedback · offset-picker no-overlap snap · same-session dupe → auto-workers · accumulate-mode duplicate rows → slot-grouped display · footer info-strip rehome · shift+ctrl freq-step key parity in FT8 (match phone/CW)
- _Forwarding / data:_ clear queued-upload backlog for a forwarder · configurable session-email subject/body · operator-email-address config field
- _Infra:_ SPA SSE consolidation (one multiplexed stream) · `/v1/hardware` audio availability + enum caching · CI-V `sets_state` value-compat validation · `internal/iocdi` contract hardening · multi-tab operating-lock (ownership + take-over; awareness banner already shipped)
- _Onboarding:_ install / first-run friction for non-Linux operators
- _Diagnostics:_ operator log viewer (DB-manager tab)

**P3 — deferred / large / needs a trigger**
- CAT poll mode (ADR 0034) · FT8 semi-auto watch-list (SET ASIDE) · spot-submitter registry (on 2nd destination) · operator / user profiles · outbound UDP telemetry (WSJT-X-compatible) · FT8 occupancy waterfall render · POTA fields · config hot-reload · settings help tooltips + beginner/expert mode · FT8 Monitor/Listen toggle (DISCUSSION) · download-site install page · `PUT /v1/config` `default_logbook.id` wiring (no consumer yet)

**Designed workstreams — built on go-ahead (not queued)**
- SM Cloud P1 (ADR 0040 + `docs/v2-design/sm-cloud-p1.md`) · DB-manager SPA (4th SPA)

**Parked — blocked or out of scope (do not pick up now)**
- _Blocked on external event:_ **FT8 Field Day UI** (FD-aware Operate ladder render · pile-up Ctrl-click · config-SPA section dropdown) + any further FD on-air validation — the FD path can only be exercised **during a Field Day contest**, so it waits for the next one. Flows already shipped + on-air-validated 2026-06-28. NOT a 7Q8AC ship concern (ARRL/RAC-only; a Malawi op doesn't run FD).
- _Out of scope (never):_ FT8 automatic / unattended sequencing — the FT8 spec forbids it; SM is attended-only.
- _Future thinking:_ "design our own sequencing / timing".

## Bugs (detail)

- **`PUT /v1/config` — wire `default_logbook.id` on PUT (P3 residual).** The
  omitted-blocks-zeroed data-loss half (M5) shipped 2026-07-04 (see backlog-archive);
  this is the leftover: `default_logbook.id` is still never copied on a PUT. Deliberately
  left — there is no logbook-switching consumer yet and it is NOT a data-loss path (an
  omitted id just isn't applied). Wire it (with active-row validation) if/when a
  logbook-switch UI lands. See `docs/reviews/internal-api-2026-06-14.md`.

- **FT8 accumulate-mode "duplicate rows" → slot-grouped display (NOT dedup).**
  Reframed 2026-07-02 (was "Rx Frequency shows duplicate rows when feed ≠ single").
  The apparent duplicates in `accumulate` feed mode are *legitimate* — the same
  station decoded across multiple 15 s slots — so the fix is **presentation (group
  by slot)**, not dedup (dedup would hide the useful "still calling / SNR-trend"
  signal). **Discovery 2026-07-02:** Band Activity **already has a slot divider** —
  `slotSeparator` (`Ft8Panel.svelte` ~758) draws one on each slot change showing the
  slot's UTC **time + band** (e.g. `14:30:15 · 20m`), gated on
  `feedMode === 'accumulate' && !cqToTop`. So it's largely done for the
  accumulate + non-cqToTop case (the time is already there). **Decided (operator,
  remote): keep the Band Activity divider; Rx pane left OPEN pending dogfood.**
  **Todo next:**
  1. ~~Add **parity** (even/odd) to the `slotSeparator` label.~~ **DONE 2026-07-03.**
     The divider now reads `14:30:15 · 20m · even` (`slotParity(utc)`), and took a
     styling pass at the operator's request the same day: filled `bg-gray-400` bar,
     `text-gray-700`, left pad `pl-2`, top border removed. `Ft8Panel.svelte`
     `slotSeparator`.
  2. Confirm whether the "duplicate rows" were seen under **`cqToTop`** — that
     ordering **suppresses the divider** (decodes get reordered, so slot-grouping
     can't apply). Decide: WAI, or should grouping still apply under cqToTop?
  3. **Dogfood** the divider (with parity added) to get a better story, THEN decide
     whether the **Rx Frequency pane** needs its own grouping/time or is fine as-is
     (no dedup today; `rxDecodes` ~520, filters by worked-call / offset±tol). Rx
     stays open until then.

- **Multi-tab operating-lock — ownership + take-over (P2; awareness banner already shipped).**
  Filed 2026-06-27 (operator flagged as a real risk). The SPA is multi-tab: every tab
  subscribes to `/v1/rig/events` and any tab can `POST /v1/rig/command` — no "which tab
  owns the rig" concept. The *dangerous* cases are already prevented daemon-side: writes
  serialise at `cmdMu`; TX is single-flight (`keyMu`/`ErrTxActive`, shared by tune + FT8-TX)
  so two tabs can't double-key or steal the mic. The only residual is the **soft** hazard —
  a freq/band/mode change in one tab moves the one radio another tab is operating on.
  **Advisory awareness shipped 2026-07-04** (see backlog-archive): the daemon emits a
  `rig-clients {count}` SSE event on multi-tab transitions and the logging SPA shows a
  passive banner when >1 tab is open — enough for the single-op 7Q8AC ship. **Remaining
  (the real lock, P2):** a daemon-tracked owner so a non-owner's write is *rejected*
  (read-only), with explicit take-over. Design facts from the 2026-07-04 dig: there is NO
  client/tab identity on any rig endpoint today (all anonymous; `EventSource` can't send
  headers), so it needs a per-tab id (body/query on commands + a correlating handshake on
  the SSE), a new `ErrNotRigOwner` gate in `SendCommands` (mirroring the `ErrTxActive`
  check + 409 mapping), a `POST /v1/rig/control` acquire/release/take-over endpoint, an
  owner-broadcast SSE event, and lock UI in the logging SPA (only SPA that controls the
  rig). Only worth it when multi-tab / multi-op is real (e.g. alongside a 2nd operator or
  smcloud). Related dogfood notes (same root): "Next during TX moves on mid-transmit",
  "currently-worked station still clickable in Band Activity".

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
  - **SHIPPED 2026-06-28 — `auto_strongest` answerer selection + Settings-tab knob.**
    `onSlotCalling` now picks the **highest-SNR** encodable answerer in the slot (clear
    the loud ones first) when `caller_answer_mode == auto_strongest`, else the first by
    decode order (`auto_first`). Surfaced over `/v1/config` as `ft8_caller_answer_mode`
    (presence-aware; only the two attended auto modes accepted, `operator_pick`/junk →
    400) and editable from the logging SPA's **FT8 Settings tab → Call CQ → Answer**
    (First answerer / Strongest signal). Pile-up drain stays **FIFO** — the knob governs
    only the hands-off auto-answerer. Tests: `caller_sequencer_test.go`
    (auto_strongest-picks-highest / auto_first-picks-first), `handler_config_ft8_test.go`,
    `types/ft8_test.go`. **Needs on-air validation** like the rest of the caller side.
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

- **Download-site install page (derive from `docs/install.md`).** Filed
  2026-06-23. The operator manual deliberately omits install/uninstall (ADR
  0036 arc starts at First Run; the embedded manual is unreachable pre-install
  anyway), so the public download site needs its own install page. Make it a
  lightly-edited operator-friendly rendering of `docs/install.md` (§1–3 install
  + enable, §10 uninstall) so the two don't drift — install.md stays the single
  canonical source. External/website work, out of this repo.

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

- **UI consistency across SPAs — shared theme layer.** THIS IS THE LOAD-BEARING
  WORK — theming (dark mode / selectable palettes, see "UI themes + dark mode")
  is the *carrot*, not the task; converging on one token layer is what actually
  tidies the CSS, and it pays off even if a theme picker never ships.
  Filed 2026-06-24; sized + sequenced 2026-07-04. The logging SPA already has a
  genuinely good token layer in `frontend/logging/src/styles/app.css` — a Tailwind
  v4 `@theme` block of semantic colours (`surface`, `ink`, `line`, `focus`,
  `invalid`, `vfo-*`) plus a `@layer components` of shared classes (`.btn`,
  `.input-base`, `.toast-*`, `.tab-item`). The mess is that it's **half-adopted**:
  measured 2026-07-04, config = 275 raw palette colour classes / **0** tokens,
  logbook = 98 / **0**, and even logging still bypasses its own tokens ~217 times
  (79 token uses). So a dev moving between SPAs juggles two mental models — that
  inconsistency is most of the "Tailwind feels cumbersome" pain.
  **Safety rail that makes this mechanical:** logging's tokens were deliberately
  defined EQUAL to their raw Tailwind values (`--color-surface: #fff`, `--color-focus`
  = indigo-600, each mapping noted in a comment), so converting `text-indigo-600`
  → `text-focus` is a **visual no-op** — the whole sweep is diff-reviewable, not a
  redesign. **Order (de-risked):** (1) lift logging's `@theme` + `@layer components`
  into a CSS file all three SPAs import (one source of truth); (2) convert config
  onto it (biggest win — 275 raw / 0 tokens, visual no-op); (3) finish logging's
  own conversion (retire its ~217 raw classes) + do logbook. Do NOT big-bang all
  three mid-other-work, and don't lead with a picker UI. Steps 1–3 ARE the tidy-up;
  the theme toggle (see "UI themes + dark mode") is a thin follow-on once colours
  route through variables. Not now — flagged for a dedicated pass.

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

- **UI themes + dark mode (all SPAs).** From dogfood-inbox 2026-06-24; operator
  reaffirmed the selectable-theme angle 2026-07-04. Wants a theme system: a dark
  mode **and** an operator-selectable theme (the operator picks a named palette for
  the UIs). **This entry is the thin follow-on, NOT the work** — the cost lives in
  the token-convergence pass under "UI consistency across SPAs — shared theme
  layer" (colours are inline in every component today: measured 275 raw classes in
  config, 98 in logbook, ~217 still-raw in logging). Once that pass routes colours
  through the shared `@theme` variables, a theme is cheap: a `data-theme` attribute
  plus a second set of variable values (dark mode = one such theme), and the
  operator's selectable theme is just N such sets. Do NOT start here — start with
  the convergence; wiring a toggle before the tokens exist just paints over the
  problem. The theme **choice** is a durable setting → daemon `config.json`, not
  localStorage (per the settings-in-config rule); the FT8 highlight colours already
  live there, so a `display`/`theme` config block is the natural home. The theme
  **picker** belongs on the config SPA's new **`General` tab** (see "Config SPA — a
  `General` tab" below), alongside the other cross-cutting preferences.

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

- **LSPA → My Station → Location: future POTA fields.** Filed from dogfood-inbox
  2026-06-25, alongside the Location-tab field trim (the trim itself is the
  active phase-2 LSPA cleanup, not backlog). Once the Location tab is reduced to
  Grid Square / Altitude / Lat / Lon, the future addition is **POTA fields**
  (park references — `MY_SIG`/`MY_SIG_INFO` ADIF, or POTA park id). Sibling to
  the already-deferred IOTA/POTA/SOTA bucket (memory
  `project_sm_adif_my_star_buckets`). Not scoped — a placeholder so the "future
  add POTA" intent isn't lost when the trim lands.

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
  - **Per-row edit — more fields (from dogfood-inbox 2026-06-29).** Extend the edit
    form beyond today's set with **notes** (distinct from `comment`) and
    **long-path/short-path (LP/SP)** propagation info.
  - **"Emailed" column (from dogfood-inbox 2026-06-29).** Surface per-QSO whether it's
    been sent via session email-out — mirror the SessionPanel "Emailed" column
    (`SmFwrdByEmail*` in `additional_data`) in the logbook table. (Sent-flag *edits*
    are already noted as future logbook work — memory `project_sm_session_email_sent_status`.)
  - **Bulk email / export as a dialog overlay (from dogfood-inbox 2026-06-29).** When
    the export-selected / email-selected bulk actions (above) land, present them in a
    dialog overlay rather than inline — operator UX preference.
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

- **FT8 Call CQ — no operator feedback while waiting for a chosen slot parity.** Filed
  2026-06-28. When **CQ slot** is set to **Even** or **Odd** (not the default **Next**),
  `StartCallCq` forces our CQ parity and deliberately does **not** immediate-fire — the
  first CQ is held until the next slot of the chosen parity (`caller_sequencer.go:63-88`),
  which can be up to ~one extra slot (~15–30 s) after the click. Correct behaviour (it's
  the point of choosing a parity), but the UI gives no sign it's waiting: the button flips
  to *Calling CQ…* immediately while the rig stays silent, so a non-default parity can read
  as "it didn't fire." Enhancement: a transient **"waiting for even/odd slot…"** indicator
  (or a countdown on the Call CQ control) until the first CQ actually keys, then drop to the
  normal calling state. SPA-only — the daemon already publishes the chosen `cq_period`
  (`QsoStatus`) and the first `tx-state {transmitting:true}` marks the real start, so the
  SPA can show "waiting" between StartCallCq and that first TX edge. Surfaces: `Ft8MsgPanel`
  (the Call CQ control), `ft8.svelte.ts`. Low effort; pure clarity. Default **Next** is
  unaffected (it fires on the very next boundary, so there's nothing to wait through).

- **FT8 Monitor/Listen on-off toggle — DISCUSSION POINT (not a committed build).** Filed
  2026-06-27. Today FT8 audio capture is tied to the **FT8 view being open**: `Ft8Panel`'s
  `onMount` calls `startFt8` → subscribes `/v1/ft8/events` → the daemon acquires the mic
  (demand-driven, refcounted, ~5 s linger, CAT-gated). So as long as a tab sits on the FT8
  operating mode — even backgrounded — the daemon holds the microphone and decodes every
  slot. That's correct and WSJT-X-like (its receiver runs while the window is up), and a
  2026-06-27 "why is the mic held?" turned out to be exactly this (an FT8 browser tab open
  alongside a Phone/CW one — benign). The discussion: should capture instead be gated on an
  explicit **Monitor/Listen** control the operator toggles, so the mic engages only when
  they deliberately start listening, not merely because the view is on screen? Points to
  weigh: (a) does Monitor *replace* the view-open trigger or *augment* it (view open but not
  monitoring → no mic, no Band Activity)? (b) interaction with the existing demand-driven
  refcount + CAT gate (Monitor would become the primary gate, subscriber-count secondary);
  (c) what the FT8 view shows when not monitoring (empty Band Activity + a "Listening off"
  state); (d) is Monitor per-tab UI state or daemon-owned (ties into the multi-tab
  operating-lock item — two tabs, who's monitoring?); (e) is the current behaviour actually
  a problem, or fine once the operator understands it (the trigger was a misunderstanding,
  not a fault)? Decide the model before any build. Surfaces if pursued: `Ft8Panel` (gate
  `startFt8`/subscription on a Monitor toggle), `ft8.svelte.ts`, possibly daemon capture
  gating. Related: the multi-tab rig-lock item (Bugs) shares the "which surface is doing
  what" question.

- **FT8 same-session dupe rule — extend to the daemon auto-workers.** Filed 2026-06-27.
  The SPA now blocks working/queuing a **same-session dupe** (a call already logged on the
  current band this session — contest-dupe style) from **Band Activity clicks**
  (`Ft8Panel` `workedThisSession`, gating `answerCq`/`workCaller`/enqueue; greyed rows).
  But the rule only covers operator clicks — the **daemon auto-workers don't honour it**:
  Call-CQ `auto_first` (`caller_sequencer.go` answerer-pick) and the pile-up drain could
  still work a station already logged this session. To make the "no same-session dupes"
  rule airtight, the daemon needs a session-dupe notion too — which is awkward because
  **the session is an SPA concept** (`sessionQsosState`, per-tab), not a daemon one (the
  daemon has no session_id, by design — see the session-scope memory). Options to weigh:
  (a) the SPA passes a "skip these calls" set / a since-timestamp to the daemon on
  `cq/start`; (b) the daemon dedupes against *today's* logbook rows on this band+mode (a
  proxy for "this session" — simpler, but not exactly session-scoped); (c) leave the
  auto-workers as-is and accept that hands-off modes may re-work a session dupe (document
  it). Decide the model before building. Until then, the SPA-click guard is the protection
  and the auto-workers are a known gap (noted in `docs/ft8.md`).

- **Operator log viewer (daemon diagnostics) — DB-manager tab to start.** Surfaced
  2026-06-30 while specifying ADR 0039's "loud startup log line" for the
  disabled-forwarder queue discard. The realisation: a loud `smd.log` line is
  worthless to an operator who won't `tail` a file — and external ops (7Q8AC etc.)
  have *no* window into daemon activity/errors. Distinct from the **live
  operational toasts** that already exist (rig-disconnected, upload-failed, bridge
  errors via SSE in the logging SPA) — this is a viewer for the **structured log
  history + admin-class events that never become toasts** (forwarder discards,
  migrations, the reference.db bootstrap, startup warnings). Decided shape (2026-06-30):
  **a diagnostics surface inside the DB-manager**, not a 5th standalone SPA — same
  admin/troubleshooting audience + cadence as queue-health/DB-health; promote to its
  own SPA later only if it grows teeth (live streaming, heavy filtering, multi-source).
  Daemon side stays narrow: a recent-log endpoint (`GET` over a ring buffer of the
  last N structured lines, or a bounded `smd.log` read), optionally an SSE tail for
  live — no coupling into log/forward internals. Build alongside the DB-manager SPA
  workstream.

- **SPA SSE consolidation — one multiplexed event stream (all SPAs).** The logging
  SPA opens 2-3 long-lived **SSE** streams per tab (`/v1/rig/events`,
  `/v1/ft8/events`, and the `/v1/events` firehose). Browsers cap **~6 connections
  per host** over HTTP/1.1, so several SM tabs each holding these **starve the
  browser** — new connections (a fresh tab, or even the SPA's own fetches) queue
  forever → "Connecting…" / frozen tab. Surfaced 2026-07-02: the (then new-tab)
  cross-SPA nav accumulated a tab per click and hung Firefox after 5-7 clicks.
  **Immediate fix shipped:** cross-SPA nav now navigates **same-tab** (only Manual
  opens a new tab), so only one SPA's SSE set is live at a time — removes the
  auto-accumulation. **Residual risk:** manually opening ~3+ logging tabs can still
  brush the 6-connection limit. **Durable fix:** collapse the per-topic SSE into ONE
  multiplexed stream (e.g. `GET /v1/stream` carrying rig/ft8/qso/bridge events tagged
  by type, SPA fans out client-side) so a tab holds ONE SSE regardless of how many
  event topics it watches — the events hub already multiplexes internally, so this
  is mostly a new combined endpoint + a client demultiplexer. NOT urgent (same-tab
  nav covers normal use); revisit if tab-starvation recurs or before a wider release.

- **FT8: operator-adjustable attempt limit before Next.** **Daemon side SHIPPED
  2026-07-03** — `ft8.tx.max_repeats` is surfaced on `/v1/config` as `ft8_max_repeats`
  ([1–10], resolved default 6) and **applied live** to the running sequencer via
  `Service.SetMaxRepeats`/`Sequencer.SetMaxRepeats` (mutex-guarded, affects an in-flight
  contact on its next slot — no restart). Decided: **logging FT8 Settings tab, live**
  (not the config SPA). Tests: sequencer clamp + service forward/nil-safe (`ft8`),
  GET/PUT/400/omit round-trip (`api`); docs: api-endpoints.md + config.md §11.5 (the one
  live-applied config field). **SPA field still pending.** Filed 2026-06-27 (dogfood),
  triaged 2026-07-03. In a big pile-up, if a station stops hearing you the sequencer
  works the full rung count (daemon-set `maxRepeats`) before the operator's Next can
  advance — wasting slots on a non-responder. The "N calls left" readout is display-only
  today (`Ft8Panel` Working banner, from `ft8State.qso.maxRepeats`, set by the daemon per
  rung — not SPA-editable). Add an operator control to **cap the attempts before
  auto-advancing** (a small numeric field beside Next, or a session default in the FT8
  Settings tab) so a dead contact is dropped sooner. Design points: SPA-only nudge of an
  early-Next threshold vs a daemon `max_repeats` override on `/v1/ft8/qso/*`; per-session
  vs per-QSO. Surfaces: `Ft8MsgPanel` (Next control), `ft8.svelte.ts`, possibly the
  sequencer rung count.

- **FT8 Field Day — FD-aware Operate ladder render (+ remaining FD UI). PARKED — blocked until the next Field Day contest.**
  Parked 2026-07-04: the FD UI can only be meaningfully exercised on-air during a Field
  Day contest, so it waits for the next one; ARRL/RAC-only, so it is not a 7Q8AC
  ship-gate item. Filed
  2026-06-28 (dogfood "correct the ladder display for the ARRL FD"), triaged 2026-07-03.
  FD-over-FT8 shipped + on-air-validated 2026-06-28 (ADR 0037, both directions), but the
  **Operate-tab message ladder still renders the standard exchange placeholders**
  (`<DX>`/`<GRID>`/`<RST>`), not the FD class+section exchange — so the ladder is wrong
  for an FD QSO. This is the documented FD remainder set (CLAUDE.md + memory
  `project_sm_ft8_field_day`): (1) the **FD-aware ladder render**; (2) **FD pile-up
  Ctrl-click** (enqueue an FD caller); and (3) the **config-SPA section dropdown**
  (`ft8.field_day.section`, validated by `goft8.ValidARRLFieldDaySection`). SPA-side for
  (1)/(2); config-SPA for (3). The daemon FD path is done — this is presentation/entry.

- **Settings help tooltips + beginner/expert mode (all SPAs).** Filed 2026-07-02
  (dogfood), triaged 2026-07-03. Many FT8 (and other) settings knobs are terse; add
  larger explanatory tooltips/help text, with an operator toggle to switch them off
  (**beginner ↔ expert** mode) so an experienced op isn't nagged. Cross-cutting UI: the
  beginner/expert flag is a durable pref → daemon `config.json` (settings-in-config
  rule), natural home the config-SPA General tab; the tooltip copy lives per component.
  Start with the FT8 Settings tab (densest), extend as friction surfaces. Pairs with the
  shared-theme / cross-SPA-shell work.

- **`actions/rigControl` — shift+ctrl freq-step key parity in FT8 (match phone/CW).**
  From dogfood-inbox 2026-07-03, graduated 2026-07-04. In phone/CW the Shift+Ctrl arrow
  cluster tunes the rig (±100 Hz / ±10 Hz / ±5 kHz band-hop, per CLAUDE.md rig-control);
  the operator wants the same freq-step keys live while an FT8 view is focused. Today those
  bindings are wired for the logging (phone/CW) surface only. Scope: decide whether FT8
  reuses the same `actions/rigControl` handler (routing set_freq/set_freq_b by selected VFO
  as today) and which FT8 focus contexts should capture the keys without clashing with FT8's
  own Shift+Ctrl shortcuts. Small, but needs a keymap-collision check against the FT8 panel.

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
