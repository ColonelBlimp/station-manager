---
number: 0058
title: Retire the Operate tile layout — responsive flow plus one ambient panel home
status: Accepted
date: 2026-07-27
---

# 0058 — Retire the Operate tile layout

## Context

ADR 0046 (2026-07-08) resolved an explicit fork: does the Operate content area
become a free canvas or stay responsive flow? It chose a middle option — a
tiling layout of fixed-size, non-overlapping cards with an explicit arrange mode
and a single global pin. The reasoning was sound on its premises: the surface had
real content, and each new need was being met with bespoke per-card chrome that a
general placement feature would supersede.

Three weeks of operating the thing changed the premises rather than the design.

**No friction appeared.** The decisive evidence is negative and only obtainable by
use: working the UI on air produced nothing that pushed for a different
arrangement. 0046 was written to get ahead of an accreting cost, and that cost did
not accrue.

**Consistency turned out to be the real complaint.** What the operator did notice
(2026-07-27) was that the same rail click produced a panel in a *different place*
depending on workspace — a board tile in Phone/CW, an overlay in FT8. That was
fixed by giving Rig and Session one ambient home in every workspace, and it is a
consistency property, not an arrangement one. Tiling does not deliver it and to
some extent works against it.

**There is not enough room for tiling to pay.** With Rig and Session ambient, the
board arranges exactly two tiles — `logging` and `worked`. A drag engine, an
arrange mode, a persistence seam and a reflow packer exist to reorder two cards on
a display that does not have room to benefit.

## Decision

Remove the tile board and arrange mode from the Operate surface. Phone/CW renders
its cards in responsive flow, matching FT8, and the rail-toggled ambient panels
(Rig, Session) overlay in one fixed position in every workspace.

## Alternatives considered

### Keep the tile layout as built

It works and is paid for. Rejected because "already built" is a sunk cost, not a
reason: it carries a drag layer, an arrange mode, a CardFrame wrapper, layout
state and a persistence seam, all to reorder two tiles, and every future card has
to be taught to participate. Deleting it removes a standing tax on work that has
nothing to do with layout.

### Keep it for Phone/CW only, leave FT8 as-is

The status quo before this change. Rejected: it *is* the inconsistency the
operator objected to. Two workspaces behaving differently for the same rail click
is precisely what prompted looking at this at all.

### Layout presets instead of dragging

0046 named this as the cheaper fallback if the drag engine did not earn its keep.
Rejected for the same reason as the drag engine: with two arrangeable tiles there
is nothing for a preset to express. Still the right first step if the question
reopens on a larger surface.

### Keep the layout state, drop only the UI

Retain `layout.svelte.ts`'s column model and persistence, delete the board and
arrange bar, in case tiling returns. Rejected as the worst of both — dead
machinery that no longer has a consumer, which is exactly the shape the package
review flagged elsewhere in this codebase. The ambient/workflow split stays
because it has a live consumer; the column model goes.

## Consequences

- **Deleted:** `TileBoard.svelte`, `ArrangeBar.svelte`, and the column/arrange
  half of `layout.svelte.ts` (ordered-ids-per-column, the reflow packer, the
  global pin and its persistence seam).
- **Kept:** the ambient/workflow split (`AMBIENT_TILES` / `WORKFLOW_TILES` /
  `isAmbient`) and rail show/hide. Those are what give Rig and Session one home
  across workspaces, which is the property actually wanted.
- **Cards are untouched.** As in 0046, this is a layout decision only: no card
  body changes. ADR 0045's "relocatable by construction" discipline stays — it
  costs nothing and is what would make a future revisit wiring rather than a
  refactor.
- **Persisted layouts are abandoned, not migrated.** Any saved arrangement under
  `sm.layout.*` becomes inert. With one operator and a session-scoped default
  there is nothing worth migrating; the keys are simply ignored.
- **A capability is genuinely lost.** The operator can no longer reorder the
  Operate surface. That is accepted knowingly on the evidence that nobody wanted
  to.

## Triggers to revisit

- **A desktop client on a real toolkit.** The operator's explicit caveat: "if this
  were a QT UI that would be something different." A native desktop surface has
  the room and the native affordances, and this decision should not constrain it.
  Parked, not rejected.
- **Substantially more screen.** A larger display or multi-monitor setup changes
  the "not enough room" premise directly. Start with presets, per 0046.
- **More than a handful of workflow cards.** Two arrangeable tiles is the core of
  the argument. If Operate grows to five or six, the packing question is real
  again.
- **A second operator.** The single-operator assumption underpins "no friction
  appeared" — that is one person's experience, on one desk, in one workflow.
- **Friction that is actually about arrangement.** Specifically: wanting two cards
  side by side that the responsive flow stacks, or wanting a card *gone* rather
  than merely toggled. Consistency complaints are not this trigger — those are
  fixed where the ambient host is.

## References

- ADR 0046 — the tile layout this supersedes.
- ADR 0044 — the consolidated operator SPA (`frontend/app/`).
- ADR 0045 — cards relocatable by construction; retained.
- `frontend/app/src/lib/operate/Operate.svelte` — the ambient host.
- `frontend/app/src/lib/operate/ambientPanels.test.ts` — the ambient/workflow
  split's rules, written 2026-07-27.
