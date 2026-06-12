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
