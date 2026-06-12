# Backlog — deferred work

Bugs and enhancements that are **known but deliberately not-now**. This is the
"we'll get to it" list, not the active-cycle task list (that lives at the top of
`docs/session-handoff.md`). FT8-internal mechanics also get captured in
`docs/ft8.md`; this file is the cross-cutting backlog the operator drives.

Convention: one bullet per item, newest at the bottom of its section. Lead with
the surface (file/subsystem) so it's greppable. Strike through or delete an item
when it ships — don't let this rot into a graveyard.

## Bugs

- **`Ft8MsgPanel.svelte` — Ladder ignores answer-a-CQ.** The message ladder always
  renders the *caller* (Call-CQ) sequence; when answering a CQ (`qso.active`) it
  should branch and render an *answer* ladder built from `qso.theirCall` /
  `qso.state` (`<them> 7Q5MLV KH78` → R<rst> → 73). Display-only — the daemon TX
  path is correct. Branch on `qso.active`: answer ladder when active, caller
  ladder when idle.

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

- **Answer-a-CQ: first rung timing — VERIFY ON AIR (ADR 0032 landed 2026-06-12).**
  The first-rung "waits a full cycle (~30 s)" symptom should now be gone: the
  synchronised-timebase + truncate-when-late rework means the opening call fires in
  the immediate opposite-parity slot (head-truncated if the click/decode was late).
  Unit tests cover the timing logic; **still needs on-air confirmation** that
  clicking a CQ now transmits in the very next slot, not a cycle later. Close this
  once validated on the rig.

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
- **Migrate FT8 SPA prefs to daemon config.** Row cap, feed mode, and highlight
  colours currently persist in localStorage; per the 2026-06-10 directive any
  non-session setting belongs in `config.json` (`ft8.display`), not per-browser
  storage. (Selected TX offset stays localStorage — operating state, not a
  setting.)
- **FT8 "Main Freq" band buttons — config-driven, tune-on-click, highlight
  current.** The `Ft8Panel` "Main Freq" column is dumb today (`Button.svelte`
  band labels, no handler, no highlight). Make each band button **jump the rig to
  that band's FT8 dial frequency** and **highlight the button matching the current
  dial frequency**. Two parts:
  - *Daemon config:* a new configurable per-band FT8 dial-frequency block in the
    single `config.json` under `ft8` (e.g. `ft8.frequencies` map band→Hz), typed
    in `internal/types/ft8.go` (`Ft8Config`) with a `Resolve…` default = the IARU
    band plan (operator is Region 1 / 7Q — Region-1 freqs). Configurable per the
    no-magic-numbers rule; mirrored to the SPA via `/v1/config` (`configState` in
    `config.svelte.ts`, alongside `ft8Display`).
  - *SPA wiring:* click → drive the **existing `set_freq` op** (`FA`; already
    exposed, same path `nudgeFreq` in `actions/rigControl.ts` uses) to the band's
    configured Hz when CAT is live; when CAT is off, set the selected VFO's
    `manualState` freq (mirror `nudgeFreq`'s live/manual split). Highlight the
    button whose configured freq matches the current dial freq (`opFreq`, ±a small
    tolerance). **No new daemon op or endpoint** — tuning is just `set_freq`.
  - *Note:* the existing button labelled **"18m" is the 17m band** (18.100 MHz) —
    fix the label when this lands. Bands present: 160/80/60/40/30/20/17/15/12/10/6.
- **Show the next slot's parity (even/odd) in the slot countdown.** The "Next slot
  in Ns" header should also say whether that next slot is even or odd, e.g. "Next
  slot in 7s · even". NB the countdown actually lives in **`Ft8MsgPanel.svelte`**
  (the `secondsToNextSlot` header, line ~141), not `Ft8Panel.svelte` (which only
  carries the `· even/odd ·` parity in its `slotLabel`). Derive the next slot's
  parity from the epoch slot index (`Math.floor(nowSec / 15) + 1`) and match the
  daemon's even/odd convention (`SlotRefFromTime` / `ft8State.slot.period`) so the
  two readouts agree.
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
- **FT8 semi-auto response to a session watch-list — UNDER CONSIDERATION (grayline,
  NOT decided).** Idea: the operator manually selects a set of callsigns into a
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
- **Rename the "Ladder" tab to "Operate"** in `Ft8Panel.svelte`. The lower-section
  tab currently titled "Ladder" (`tabs` array, id `ladder`, renders `Ft8MsgPanel`)
  should read **Operate**. Tab title only — the `id`/`role`/`aria` wiring can stay
  `ladder` unless a fuller rename is wanted.

## Scope notes (NOT backlog — recorded so they aren't mistaken for it)

- **FT8 call-CQ caller-side sequencing** is *deferred scope*, not a bug: today
  Call CQ is a single-shot button; full caller-side sequencing (multi-answerer
  management) is operator-initiated attended work for a later increment.
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
