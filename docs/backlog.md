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

- **Answer-a-CQ: first rung waits a full cycle (~30 s) — SUBSUMED by ADR 0032.**
  `Sequencer.StartQso` only sets up the exchange and never transmits; the only
  transmit trigger is `OnSlot`, and the one that would fire the opening call
  already ran with `s.ex == nil` (it's what produced the clickable CQ), so the
  first reply waits for the next their-parity slot. **Not fixed as a standalone
  patch** — the ADR 0032 timing rework (synchronised timebase + transmit on the
  next opposite-parity boundary, truncated if late) fixes this structurally. Kept
  here as a symptom to verify gone once 0032 lands.

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

- **Implement ADR 0032 — FT8 TX timing: synchronised timebase + truncate-when-late.**
  Replace the sequencer's full-waveform-shifted-late model (`seqTransmit` →
  `TransmitNow`, gated on `maxStartDt ≈ 1.74 s`) with the QEX §8 model: ride a
  slot-synchronised timebase (symbol 0 at boundary + 0.5 s), transmit each rung on
  the next opposite-parity boundary, and when the decode/click lands late emit the
  **truncated** synchronised waveform (drop the elapsed head) instead of shifting a
  full one — gaining the ~5–8 s (AP-mycall) late tolerance. Touches
  `internal/ft8/modulate.go`/`EncodeToSlot` (slot-offset-aligned, head-truncated
  emission), `txcontroller.go` (`TransmitNow` → slot-aware truncated send), and
  `sequencer.go` (drop the full-fit guard for a "symbols-remain" guard). Fixes the
  first-rung delay structurally. Update CLAUDE.md + `docs/ft8.md` timing prose when
  it lands. See ADR 0032.
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
