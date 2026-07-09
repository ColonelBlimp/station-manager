---
number: 0047
title: FT8 operating view — three-anchor fixed layout, no sub-tabs
status: Accepted
date: 2026-07-09
---

# 0047 — FT8 operating view — three-anchor fixed layout, no sub-tabs

## Context

The FT8 operating surface is being ported into the consolidated SPA
(`frontend/app`, ADR 0044) as the sibling of Phone/CW under **Operate**. The
shipping FT8 UI (`frontend/logging` `Ft8Panel.svelte`, ~1131 lines) grew inside
the logging app where it was fighting for width, so it packed everything into
**four sub-tabs** (Occupancy / Operate / Session / Settings) plus a few
always-visible panes (Main Freq, Band Activity, Rx Frequency, a slot countdown).
The consolidated shell has far more room and already provides shared cards (Rig,
Session), an Operate-wide util rail, and a draggable/pinnable tile system for
Phone/CW (ADR 0046).

The operator — who runs FT8 heavily and has on-air-validated the answer/work/
Field-Day flows — walked the shipping UI part by part to decide what carries
over, what drops, and how it should be laid out. The governing insight was a
**most-watched ranking**: the *Operate/ladder* pane (the control center — when to
advance the pile-up, how far through the message sequence, when to abandon) is
watched most; **Band Activity** (scan, enqueue callers, answer CQs) next; **Rx
Frequency** least (its "someone's on my offset" signal duplicates Occupancy).
That ranking, plus "which panes must be visible *at once*," drives the layout.

## Decision

Lay the FT8 view out as **three fixed anchors** — **Operate/ladder** and **Band
Activity** as co-primary panes at the top, and **Occupancy** as a **full-width
strip across the bottom** — with the shared **Rig** and **Session** cards
**rail-toggled** (collapsed by default, the header chip is the glance) and
**Settings on the RH util rail**. **The shipping sub-tab structure is
eliminated** — every surface is either an always-visible anchor, a shared card,
or rail-triggered. Occupancy is a **three-view switcher (Waterfall · Spectrum ·
Channels)**, waterfall-ready from day one.

Per-part verdicts (the walkthrough result):

- **Operate/ladder + TX** (Arm / Call CQ / Abandon / **Next**, role-aware message
  ladder) — **keep, co-primary anchor** (side-by-side with Band Activity, ~470 px).
  Panel top = the reused enrichment card (left) + worked-call / role (right), a
  divider, then a **slot-timing indicator** directly above the ladder rungs, the
  rungs, pile-up next-up, and the control bar. The control bar groups the **actions**
  (Call CQ, Abandon, **Next**) with the master **Arm/disarm** toggle separated
  below a divider at the very bottom — TX-enable is a deliberate, distinct action,
  not one of the per-slot buttons. **Next** is **conditional** (`canAbandon &&
  pile-up count > 0 && !callerActive`; disabled during TX, advances only in the
  RX half): it drops the current caller and jumps to the next in the pile-up —
  the "when to move on" control. The SP/L antenna-path choice lives *in* the enrichment card (its
  built-in SP/LP toggle), not on the TX bar.
- **Band Activity** — **keep, co-primary anchor, front & centre.** It also owns
  the **per-CQ bearing** column, which *is* the bearing home (see below).
- **Occupancy** (TX-offset picker) — **keep, own full-width bottom panel.** Three
  switchable views (Waterfall · Spectrum · Channels); the ▾ clear-offset markers
  + ★ recommended marker are **kept** on both Spectrum and Channels (fed by the
  daemon `suggested` field). The separate **Clear-Offsets *list* is dropped**
  (redundant with the markers).
- **Main Freq** — **keep as the shared Rig card** (ADR 0046). Its band grid uses
  an FT8 `onPick` (watering-hole `set_freq` + data-mode `set_mode`, vs Phone/CW's
  `set_band`); live-only (FT8 requires CAT); band list = `operating_bands` ∩
  bands with an `ft8_frequencies` entry.
- **Session** — **keep as the shared Session card**; the shipping Session sub-tab
  is dropped.
- **Settings** (display prefs, feed mode, caller-answer-mode, max-repeats, Field
  Day) — **move to the RH util rail** (rail-triggered panel); sub-tab dropped.
- **Pile-up drawer** — **keep, reuse the existing drawer chrome**; FT8 adds its
  own **Ctrl+click capture + daemon auto-drain + Abandon/Resume** wiring (distinct
  from Phone/CW's manual callsign stack).
- **Band-Activity filter popover** — **keep, but upgrade** (richer filters —
  spec deferred).
- **Rx Frequency** — **keep the info, not a standing pane.** Folded into the
  **Occupancy panel header** (the exact station[s] on/near your TX offset, shown
  beside the view-switcher); the standalone pane is dropped.
- **Slot-timing indicator** — **keep, reworded + rehomed.** Placed **directly
  above the ladder rungs** (the rungs *are* the slots, so timing reads in
  context). Names the current + next slot in plain language — "Transmit slot ·
  listen in N s" / "Listen slot · transmit in N s" — **not** a WSJT-X-style
  progress bar. It **replaces** the redundant "rung N of M" progress text (the
  ladder shows progress visually); the unanswered-repeats count moves onto the
  active rung ("… · sent ×1").
- **Enrichment** — **keep, by reusing the Phone/CW `EnrichmentCard` as-is** at
  the **top-left of the Operate panel** (fixed 224×180, the same `w-56 h-45` box
  as Phone/CW). The card is already FT8-appropriate — flag · country · DXCC +
  **NEW** · bearing + distance · **SP/LP** toggle · their local time (there is no
  name/QTH in it), so there is nothing to trim. This also permanently homes the
  **SP/L path choice** and the active-station **bearing**. *(This supersedes the
  walkthrough's initial "conditional drop": once the panel had the room, reusing
  the familiar card beat a bespoke working-station card — maximum reuse +
  cross-mode consistency.)* **Cost:** the card currently reaches into
  `draft.callsign`; reuse needs its observed call made a **prop** (ADR 0045),
  fed the FT8 worked station — a small injection that also brings the card into
  0045 compliance.

**Settled via the throwaway mockup** (`docs/v2-design/ft8-mock/index.html`,
placement-in-context — the ADR-0044 method, before any Svelte): **anchor
geometry = side-by-side** (Band Activity dominant left, Operate ~470 px right);
**Rig/Session = rail-toggled** (collapsed by default); **enrichment = the reused
card, top-left of Operate** (which also homed the SP/L path choice); **slot
countdown = Operate top-right**; **Rx-Freq = folded into the Occupancy header**.
Nothing material is left open at the layout level; the filter upgrade and the
Rx-Freq fold-in wiring remain follow-on specs.

## Alternatives considered

### Port the shipping four-sub-tab structure as-is

Lowest-effort: reproduce Occupancy / Operate / Session / Settings tabs. Rejected
because the tabs were a **space workaround** for the cramped logging app, and they
force the two most-watched surfaces (Operate-ladder and Band Activity) apart — you
cannot watch the exchange control *and* the pile-up/CQs at once, which is exactly
the FT8 operating loop. In the roomy shell, tabs are strictly worse; two of the
four (Session, Settings) also cease to be FT8-specific once the shell's shared
card + rail absorb them, so the structure was already hollowing out.

### Reuse the ADR 0046 free-tile system wholesale (FT8 as arrangeable tiles)

Maximal consistency with Phone/CW. Rejected as the *primary* model because FT8's
panes are denser and more functionally coupled than the free-form logging cards,
and the spectrum/waterfall wants to be **wide** — fixed-size tiles packed into
columns serve that poorly. FT8 has a natural, near-fixed operating geometry
(control + scan up top, wide spectrum below) that free tiles would fight. The
tile system's *shared cards* (Rig, Session) and the persistence seam are still
reused; only the anchor panes are fixed.

### Fixed WSJT-X clone (everything pinned, nothing moveable)

Rejected as too rigid the other way: the Rig/Session cards genuinely benefit from
moving/pinning as acreage varies across monitors, and the operator explicitly
wants them flexible "depending on acreage." Hence the **hybrid**: fixed anchors
for the operating spine, moveable/pinnable shared cards around them.

### Keep Rx Frequency and the Enrichment box as standing panes

Rejected/downgraded on the operator's own usage: Rx-Freq's occupancy signal
duplicates the Occupancy panel, so it survives only as **folded-in detail** in
the Occupancy header, not a standing pane. Enrichment as a *bespoke* pane was
also rejected — but it was **reinstated by reusing the existing `EnrichmentCard`
as-is** (top-left of Operate) once the panel had the room: max reuse beat both a
bespoke card and dropping it (see Decision). What lost here is the *standing
bespoke pane*, not the information.

## Consequences

**Signed up for (good):**

- The two surfaces the operator watches most (Operate-ladder, Band Activity) are
  visible **simultaneously**, front and centre — the shipping tab split is gone.
- **Occupancy gets full width**, which is exactly what a future scrolling
  **waterfall** (backlog: daemon-side sub-slot FFT streaming) needs; the view
  switcher carries its slot from day one, so adding it later is SPA-local.
- Heavy **reuse**: the Rig and Session **shared cards**, the util rail, and the
  pile-up drawer chrome all carry over; only FT8-specific behaviour (watering-hole
  band `onPick`, capture + auto-drain, the ladder/sequencer, the offset picker) is
  new. Consistent with "same shell, mode-specific content" (band grid, rail).
- **No new daemon surface.** The `/v1/ft8/*` endpoints + 5 SSE events are
  unchanged; this is a client-side layout decision.

**Accepted (cost):**

- FT8 does **not** use the same free-tile model as Phone/CW — two layout models
  coexist under Operate (fixed-anchor FT8, free-tile Phone/CW). Justified by the
  genuinely different pane density/coupling, but it is a divergence to hold in
  mind.
- The layout is now **fully settled** via the mockup (geometry, enrichment,
  countdown, Rx-Freq, Rig/Session) — but the mockup only de-risks the **cheap
  half**. It says nothing about the expensive half (splitting
  `Ft8Panel`/`ft8.svelte.ts` into presentation + injected seams per ADR 0045,
  view-scoped SSE for the demand-driven audio device, making `EnrichmentCard`'s
  observed call a prop) — that is de-risked in code, not HTML.
- The **filter upgrade** and the **Rx-Freq fold-in** are follow-on specs, not
  fully designed here.

## Triggers to revisit

- If the real build can't keep both co-primary anchors usable at the target
  min-width (the mockup validated side-by-side at desktop widths), reconsider
  whether one (likely the ladder) becomes a rail/drawer element instead of a top
  anchor.
- If the waterfall never materialises (daemon FFT-streaming cost judged not
  worth it), the full-width bottom strip could be narrowed and the Rig/Session
  cards given more room.
- If a second FT8-heavy operator finds the fixed anchors wrong for their
  workflow, revisit whether FT8 should also become arrangeable tiles after all.
- If FT8 and Phone/CW's layout divergence causes real maintenance drag, revisit
  unifying both under one model.

## References

- ADR 0044 — consolidate operator SPAs into one shell (Operate hosts Phone/CW +
  FT8; the build-mock-before-Svelte method).
- ADR 0045 — frontend component architecture (presentation + injected seams; the
  `Ft8Panel`/`ft8.svelte.ts` split this port requires).
- ADR 0046 — Operate tile layout (the shared Rig/Session cards + rail this reuses;
  the free-tile model FT8 deliberately does *not* adopt for its anchors).
- ADR 0037 — FT8 Field Day (a Settings-panel consumer: class/section).
- `docs/ft8.md` — the single FT8 capture point (sequencer, offset picker, SSE
  contract).
- Mockup: `docs/v2-design/ft8-mock/index.html` — throwaway layout prototype for
  the deferred placement calls. Delete once the real FT8 view ships.
