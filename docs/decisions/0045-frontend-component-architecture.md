---
number: 0045
title: Frontend component architecture — decoupled, relocatable by construction
status: Accepted
date: 2026-07-06
---

# 0045 — Frontend component architecture — decoupled, relocatable by construction

## Context

The consolidated SPA (ADR 0044) is being built in `frontend/app/` (Svelte 5 +
Vite). As the operating surface grows — logging card, right-rail info panels,
CAT/rig gate, pile-up drawer — the question of *how components are wired* is being
answered one component at a time, and that early shape is hard to change later.

A future feature crystallised the stakes: **draggable / pinnable cards** (captured
in `docs/dogfood-inbox.md`), where the operator freely positions content cards and
pins the layout. Whether or not that ships, it exposed a principle — a card can
only be "picked up and moved" if it is **self-contained and knows nothing about
where it lives**. A component that reaches up into layout, sideways into a sibling,
or embeds a data source / state machine cannot be relocated, reused, or tested in
isolation — and that entanglement cannot be retrofitted without a rewrite. The
responsive layout already shipped (ADR 0044 amendment: auto-collapse, main-as-
scroll-container, card-never-overlapped) likewise assumes cards are self-contained
flow units a wrapper positions.

This is the **client-side parallel to ADR 0043's backend coupling doctrine**:
tighten what's stable, loosen what's uncertain — applied to Svelte components.

## Decision

Build frontend components — content cards especially — **DRY, self-contained, and
decoupled from the start**: presentation is separated from state/logic, backend
coupling is injected or subscribed (never wired into the component), and no
component assumes its position or layout context. Cards are **relocatable by
construction**.

The principles:

1. **Presentation ≠ state.** Components render; reactive state and logic live in
   shared `.svelte.ts` rune modules (`ui`, `router`, `operate`, …). Mirrors the
   daemon's "state is not the view" separation.
2. **Data/actions in, nothing reached out.** A component takes what it needs via
   props or reads a shared state module. It never reaches *up* into layout or
   *sideways* into a sibling component.
3. **Backend coupling is injected or subscribed** — the same seam-injection
   discipline the daemon uses (ADR 0013/0029/0027 seams: capture source, TX keyer,
   QSO sink). A component subscribes to state or receives an action callback; it
   does not import/instantiate the data source or embed a state machine. This keeps
   the "narrow scope by import graph" spirit on the client.
4. **No positioning assumptions.** A card does not know or care whether it is
   centred, in a panel, or dragged. Layout is a wrapper's job. This is what keeps
   the draggable/pinnable feature (and the responsive flow) feasible.
5. **DRY.** One component per concept, reused; shared classes + `@theme` tokens
   (`app.css`) authored once. *Nuance vs "build specific, not generic":* DRY here
   means **don't duplicate** a concrete component/token — not build a speculative
   generic framework. The v1 `internal/adapters` cautionary tale still stands; a
   reflection-driven "card engine" is not the goal, a clean prop/state seam is.

## Alternatives considered

### Build fast and coupled now, refactor to decouple later

Wire components directly to data sources / each other to move quickly, clean up
when a feature needs it. Rejected: decoupling is the one thing that *cannot* be
cheaply retrofitted — an entangled component is a rewrite, not a refactor — and it
would silently foreclose the draggable-cards feature and make isolation testing
impossible. The discipline is nearly free *if applied from the first component*
and expensive to add after.

### Build the drag/pin engine now to force the decoupling

Let the feature drive the architecture immediately. Rejected (ADR 0044 amendment):
it is premature — the cards are placeholders, it detours from the ship-gating core
(CAT gate, logging, enrichment), and it commits to the free-canvas content model
before that fork is deliberately decided. The *principle* gives us the decoupling
benefit now without building the feature.

### Leave it implicit

Rely on habit. Rejected: "how is a component wired" is exactly the kind of early,
per-component choice that drifts without a written rule — the same reason ADR 0043
wrote down the backend coupling doctrine rather than trusting it was understood.

## Consequences

- **Components are testable in isolation** (Vitest/Testing-Library) — props in,
  rendered output + emitted actions out — with no need to stand up the whole shell.
- **The draggable/pinnable feature stays on the table** — a future positioning
  wrapper can move any card because no card resists it. The canvas-vs-flow
  content-model decision is deferred, not foreclosed.
- **Reuse across surfaces** — a card built for Phone/CW that doesn't assume its
  context can serve FT8 (the shared session log, Worked/Details panels) without
  forking.
- **Cost accepted:** more state modules + prop-threading than the "component owns
  everything" shortcut. But it is the *same* discipline already in use
  (`ui`/`router`/`operate` state modules), so it is a continuation, not new tax.

## Triggers to revisit

- If decoupling a component would require inventing an interface whose only consumer
  is a test mock, stop — that's the "delete mock-only interfaces" lesson; pass the
  concrete shared-state module instead.
- If the draggable-cards feature is decided *against* permanently, principles 1–3
  and 5 still hold (they're plain good hygiene + testability); only #4 (no
  positioning assumptions) relaxes to "no *hard-coded* positioning."
- If a shared state module grows into a god-object that every component couples to,
  split it by surface (as the backend split `internal/api` per ADR 0043) — the same
  breadth smell applies.

## References

- ADR 0043 — coupling principles for v2 + the `internal/api` split (the **backend**
  doctrine this is the client-side parallel of).
- ADR 0044 — consolidate operator SPAs into one shell (the app this governs); its
  2026-07-06 amendment (responsive layout; draggable-cards deferred as a
  content-model fork).
- ADR 0013 / 0027 / 0029 — daemon seam-injection precedent (capture source, tune
  controller, TX keyer / QSO sink injected, not imported).
- `docs/v1-analysis/lessons-for-v2.md` — "build specific, not generic"
  (`internal/adapters` cautionary tale — the nuance on DRY above).
- `docs/dogfood-inbox.md` — the draggable/pinnable cards note (the forcing function).
- Anchored code: `frontend/app/src/lib/{ui,router,operate}/*.svelte.ts` (the
  state-module pattern); `frontend/app/src/lib/operate/*.svelte` (cards that read
  shared state + take actions, no layout assumptions).
