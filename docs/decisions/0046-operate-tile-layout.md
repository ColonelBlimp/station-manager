---
number: 0046
title: Operate surface — draggable/pinnable tile layout (fixed-size, no-overlap, global pin)
status: Superseded by 0058
date: 2026-07-08
---

# 0046 — Operate surface — draggable/pinnable tile layout

> **Superseded by [ADR 0058](0058-retire-operate-tile-layout.md) (2026-07-27).**
> The tile layout was built and operated for three weeks. Its premise — that
> per-card chrome would otherwise accrete and a general placement feature would
> supersede it — did not hold: no arrangement friction appeared in use, the real
> complaint turned out to be *consistency* between workspaces (fixed by giving the
> Rig and Session panels one ambient home), and with those two panels ambient the
> board was left arranging two tiles. The reasoning below is preserved as written;
> 0058 explains what changed.


## Context

The consolidated operator SPA (ADR 0044) builds its cards *relocatable by
construction* (ADR 0045): presentation split from state, no positioning
assumptions, backend injected. That was done specifically to keep a **draggable /
pinnable cards** feature on the table without committing to it — the inbox note
(`docs/dogfood-inbox.md`, 2026-07-08) explicitly flagged it as **an ADR-level
fork**: does the Operate content area *become a free canvas* (dashboard widgets)
or *stay responsive flow*, decided only once the surface has real content.

That precondition is now met — the surface has a logging card, live
Worked/Session/Rig panels, the rig SSE, and a contact overlay. And it started to
*cost*: each new need was being met with bespoke per-card interaction (the rail's
single-slot InfoPanel that swaps one panel at a time; a contact overlay). The
operator's framing: stop investing in per-card interactions that a general
placement feature would supersede — "a waste of time with moveable, pinnable
cards." So the fork has to be resolved now, deliberately, before more chrome
accretes.

The model was refined interactively (2026-07-08) toward the least-complex shape
that still delivers "let the operator arrange the surface," and validated with a
throwaway pointer-drag POC (`docs/v2-design/tile-layout-poc/`).

## Decision

The Operate content area is a **tiling layout of fixed-size cards**, arranged in
an explicit mode and persisted by a single global pin:

- **Fixed-size tiles.** Cards can be moved/reordered but **never resized**.
- **No overlap.** Tiles pack into columns and **reflow** when one is dropped.
  Overlap is reserved for *overlays* (modals: duplicate/export/contact) — content
  cards tile, they don't stack.
- **Default is non-destructive.** The built-in responsive layout is always the
  fallback; a botched arrangement is one action from sane.
- **Global pin, not per-card.** One switch for the whole arrangement:
  *unpinned* = session-only (a restart returns to Default); *pinned* = the whole
  arrangement is saved and survives a restart.
- **Cards are unchanged.** Size and content of every card body stay exactly as
  they are; the tile system is purely additive. A uniform **CardFrame** wrapper
  supplies the arrange chrome (grip/title/hide) and is **shown only in an explicit
  "arrange layout" mode** — during operation cards are clean, costing zero
  vertical pixels on the fast path.
- **The rail becomes show/hide-a-tile** — retiring the single-slot InfoPanel that
  swaps one panel at a time.

Not built now: the op-profile system, real persistence, resize, and any change to
overlay/modal behaviour.

## Alternatives considered

### Full free canvas (absolute pixel positioning, drag anywhere)

The dashboard-widget model; largely supersedes the responsive layout. Rejected:
most code (drag + viewport clamp + overlap resolution + z-order + click-to-raise +
resize + persistence) for the least benefit on a single-op, fixed-desk, fast-path
surface where **layout stability = muscle memory is itself a feature** (the same
reason the movable-*nav* idea was deferred). "No overlap" alone deletes z-order,
raise-to-front, pixel-clamping, and collision resolution.

### Per-card pinning (the original inbox reconciliation model)

Each card carries `{x, y, pinned}`; pinned cards are absolute overrides that don't
reflow while unpinned cards keep flowing. Rejected in favour of **global** pin: the
mixed flow-plus-absolute reconciliation is the fiddly, bug-prone part, and a single
"is my custom layout saved or not?" switch is a cleaner mental model and far less
code. All-or-nothing (Default *or* your saved arrangement) fits how one operator
works.

### Layout presets only

A few curated arrangements the operator switches between — ~80% of the tailoring
for a fraction of a drag engine. Rejected as the *primary* model (the operator
wanted real free arrangement), but it remains a valid cheaper fallback if the drag
engine ever proves not worth its keep; presets could also layer on top later.

### Keep the single-slot InfoPanel + add interaction per card

The status quo: meet each need with bespoke card chrome. Rejected — it's exactly
the accreting per-card cost the operator called out, and it doesn't deliver
"arrange the surface" at all.

### HTML5 drag-and-drop for the drag mechanism

Tried first in the POC; drops silently failed to commit on the dev browser
(Firefox) even with the `setData` workaround and board-level `preventDefault`.
Rejected for **Pointer Events** (mousedown/move/up with a floated ghost +
placeholder), which worked reliably and is the mechanism the real implementation
will use.

## Consequences

- **Retires layout plumbing, not cards:** the single-slot InfoPanel container, its
  self-centring (`ml-[calc(...)]` on the logging-card axis — the one place ADR
  0045's "no positioning assumptions" isn't yet honoured), and the rail-swap all
  go away, replaced by the tile layer. The panel *bodies*
  (`WorkedPanel`/`SessionPanel`/`RigPanel`) are untouched.
- **Overlays are unaffected** — modals remain the only thing allowed to sit on top.
- **A drag layer must be built** (Pointer Events; ~move/reorder/pack/reflow). "No
  overlap" and "no resize" keep it small; global pin keeps persistence trivial.
- **Design guards make the eventual op-profile move wiring, not a refactor** — the
  same guards the POC already embodies:
  1. **Layout is a plain serialisable value** — ordered tile ids per column + a
     hidden set; **no pixel coordinates**. Same shape for localStorage now or a
     `config.json` op-profile field later.
  2. **State-driven** — a `lib/operate/*.svelte.ts` rune module owns the layout
     value; the DOM is the transient view.
  3. **Persistence is an injected seam** — `loadLayout`/`saveLayout` injected in
     `main.ts` (like `setMailer`/`setModeMappings`), never storage calls inline;
     localStorage now → `GET/PUT /v1/config` per profile later, unchanged code.
  4. **Profile-keyed** — persisted under a composable key (`sm.layout.<profileId>`,
     `profileId = 'default'` for now), leaving room for a per-Operate-sub-mode key
     (`<profileId>.<subMode>`).
- **Deferred to lift time (not decided here):** one shared arrangement vs. one per
  Operate sub-mode (Phone/CW vs FT8); whether the column count adapts to window
  width.

## Triggers to revisit

- If the drag engine proves more trouble than it's worth in `frontend/app`, fall
  back to **layout presets** (the cheaper model kept in reserve above).
- If operators end up wanting cards to *float over* content (not just tile), the
  no-overlap decision reopens — but that's an overlay, and should be modelled as
  one, not as tiling.
- If per-op-profile layout lands and a single global pin feels too coarse (e.g. an
  operator wants a stable spine with one movable card), revisit per-card pinning —
  the serialisable layout value can carry it without a rewrite.
- If "no resize" becomes a real limitation (a card that must grow, e.g. a
  scrolling log), reconsider a column-span (not free pixel resize) as the minimal
  concession.

## References

- ADR 0044 — consolidate operator SPAs into one shell (the app this governs); its
  2026-07-06 amendment deferred draggable-cards as a content-model fork — **this
  ADR resolves that fork**.
- ADR 0045 — frontend component architecture (relocatable by construction) — the
  precondition that makes this feasible; this is the feature it was protecting.
- `docs/v2-design/tile-layout-poc/` — the pointer-drag POC that validated the
  model (interaction only; no persistence, no Svelte).
- `docs/dogfood-inbox.md` — the draggable/pinnable-cards notes (2026-07-06 model +
  2026-07-07 chrome decision) this supersedes/settles, and the operator-profiles
  idea the persistence guards anticipate.
