# Backlog — deferred work

Bugs and enhancements that are **known but deliberately not-now**. This is the
"we'll get to it" list, not the active-cycle task list (that lives at the top of
`docs/session-handoff.md`). FT8-internal mechanics also get captured in
`docs/ft8.md`; this file is the cross-cutting backlog the operator drives.

Convention: one bullet per item, newest at the bottom of its section. Lead with
the surface (file/subsystem) so it's greppable. Strike through or delete an item
when it ships — don't let this rot into a graveyard.

## Bugs

- **Contacted-station update uses the unguarded generated `Update(Infer)`
  pattern.** Filed from the `internal/database` review (2026-06-14, M2 follow-up).
  The contacted-station update helper converts to a fresh SQLBoiler model and
  calls `model.Update(boil.Infer())` with a primary-key-only predicate and an
  ignored rows-affected count — the same shape that let a stale QSO update write
  through a soft-deleted row. The table has `deleted_at`. The **QSO** path
  (`updateActiveQso`) and the **logbook** path (api review M3, 2026-06-14) are
  now fixed with the active-row `UpdateAll WHERE id AND deleted_at IS NULL` +
  `ErrNotFound`; contacted-station should get the same treatment for consistency.
  Lower risk (it's an enrichment-cache row, rarely soft-deleted, not directly
  API-reachable for update), hence deferred. See
  `docs/reviews/internal-database-2026-06-14.md`.

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

- **FT8 capture grabs the mic with no CAT / rig off.** Capture is demand-driven
  (acquired when the first `/v1/ft8/events` SSE subscriber connects). On PC boot
  `smd` autostarts and, if the SPA reopens to the FT8 view from last session, it
  subscribes immediately and the daemon grabs the audio input device — even when
  CAT is not live (rig off / not connected). The rig should not be powered before
  the PC is up anyway, and grabbing the mic in that state is wrong. **Gate FT8
  capture acquisition on CAT being live:** no live rig → no mic, regardless of an
  open FT8 view; acquire only once CAT comes up (and release the mic if CAT
  drops). Stay fail-soft (subsystem idle, no crash) when CAT is absent.

- **Rx Frequency table shows duplicate rows when feed mode ≠ "single".** With the
  FT8 display feed mode set to anything other than `single` (i.e. `accumulate`),
  duplicate entries appear in the Rx Frequency pane. Likely the `rxDecodes` filter
  in `Ft8Panel.svelte` surfaces the same station decoded across multiple
  accumulated slots (filter is by callsign in-QSO / offset±tol idle, with no
  per-call/per-offset dedup), which reads as duplicates. Investigate: confirm
  whether it's genuine repeat decodes across slots vs. a keying/accumulation bug,
  and decide whether the Rx pane should collapse to the latest decode per station.

- **Answer-a-CQ: Abandon does not stop an in-flight transmission.** Clicking
  Abandon mid-transmit clears the sequencer but the current rung's waveform plays
  to completion with PTT keyed. `Service.AbandonQso` (`internal/ft8/servicetx.go`)
  only calls `seq.Abandon()` (clears `s.ex`, prevents the *next* rung) but never
  cancels the in-flight TX. Disable TX (`disarmTx`) already does the right thing
  via `s.txCancel()` (controller honours `ctx.Done()` → `player.Stop()` + deferred
  PTT unkey, txcontroller.go). **Fix:** `AbandonQso` should also call the in-flight
  `s.txCancel()` (snapshot under `txMu`), **without disarming** — stay armed + keep
  the output device so the operator can answer another CQ. The tracked goroutine
  then flips `txInFlight` false and republishes; the cancel is a normal stop (no
  error toast). Guaranteed-stop-adjacent — operator expects RF to stop on Abandon.

## Features / enhancements

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
- **FT8 Band Activity — float CQ calls to the top (config toggle, feed mode
  `single`).** In `single` feed mode the operator wants every **CQ** decode grouped
  at the **top** of the Band Activity list (`ft8State.decodes`, the `{#each}` at
  `Ft8Panel.svelte` ~line 253) so answerable calls are immediately visible. Make it a
  **daemon-config knob in the Settings tab** (per the settings-in-config rule),
  alongside feed mode / row cap / highlight colours:
  - *Daemon:* add `ft8.display.cq_first` (bool) to the Ft8 display config
    (`internal/types`, mirrored in `config.svelte.ts` `Ft8DisplayView`, `/v1/config`
    round-trip next to the `feed_mode` / `history_max` / `highlight_*` siblings).
    Default TBD — lean **ON** (the operator asked for it).
  - *Settings tab:* a checkbox bound to `configState.ft8Display.cqFirst`, saved the
    same way as the other ft8Display prefs.
  - *SPA render:* a `$derived` view over `ft8State.decodes` that **stable-partitions**
    CQ rows (`parseCqCall(d.text) !== null`) to the top, non-CQ rows keeping their
    order below — gated on `feedMode === 'single' && cqFirst`, not a store mutation.
    In `accumulate` mode newest-slot-on-top chronology is the point — leave it alone.
  Open point: within the CQ group, preserve daemon order or sub-sort (by SNR /
  offset)? Lean: just partition, keep order within each group.
- **FT8 e4 — `TIME_ON` should be the QSO start, not the completion instant.**
  `ft8.BuildQso` (`internal/ft8/qsolog.go`) stamps both `TIME_ON` and `TIME_OFF`
  from `now` (the moment the 73 is sent) because `CompletedQso` carries no start
  time. `TIME_OFF` is therefore correct; `TIME_ON` is up to ~2 min late. Harmless
  for QSL time-matching (±30 min window) but not strictly accurate. Fix: thread the
  `StartQso` instant into `CompletedQso` (a `StartedAt`) and use it for `TIME_ON`,
  keeping `now` for `TIME_OFF`. The first-rung fix (shipped 2026-06-12) already
  injects `now` into `Sequencer.StartQso`, so the wall-clock is in hand — this item
  is now just capturing it onto `CompletedQso` and stamping `TIME_ON` from it.
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
  - **REMAINS — `operator_pick` (the pick-a-caller stack) — PRIORITISED 2026-06-12,
    top FT8 build item (validated need on air).** First time operating the pile-up, the
    operator hit the exact gap: answering a CQ from a silent station while OTHER
    stations called *them*, with **no way to switch to a caller** — those callers send
    directed `7Q5MLV <them> …` lines, which aren't clickable CQ rows. So the scope is
    **broader than "Call-CQ only": a "stations calling me" pick-list that works in BOTH
    modes** — while calling CQ *and* while answering a CQ — letting the operator tap a
    caller to switch to working them (the WSJT-X "double-click a station calling you"
    behaviour). The callers (decodes `<ourcall> <them> …` directed to us) populate a
    stack/queue (mirroring the Phone/CW `callsignStack` + `StackingDrawer`) the operator
    pops, instead of `auto_first` taking the first. The sequencer already carries
    `answerMode` and `CallerExchange` is selection-agnostic, so no resolver rework —
    `operator_pick` adds: (1) a daemon-side "stations-calling-me" collector +
    pop-to-start-a-`CallerExchange`, available whether idle/calling-CQ OR mid
    answer-a-CQ (switching abandons the current contact); (2) the SPA stack drawer in the
    Operate tab; (3) the **Settings-tab toggle** for `caller_answer_mode`. **Today's
    workaround:** Abandon the silent station → Call CQ → `auto_first` works the first
    caller (no choice of which).
  - **Attended either way:** operator initiates by calling CQ, is present, Abandon stops
    it instantly; **no auto-CQ cycle, no auto-fire-on-watch-match** — which is why this
    **supersedes the auto-responder framing** of the watch-list item above.
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
