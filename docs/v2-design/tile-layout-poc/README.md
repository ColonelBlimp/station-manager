# Tile-layout POC — Operate surface draggable/pinnable cards

A **throwaway proof-of-concept**, deliberately **outside `frontend/app/`** (pure
HTML/CSS/JS, no Svelte, no build). It exists to let the operator *feel* the
interaction before it's lifted into the real SPA — dragging fights the responsive
Svelte layout, so we prototype in plain HTML first.

Open it directly in a browser:

```
xdg-open docs/v2-design/tile-layout-poc/index.html
```

## What it proves (the model we settled on, 2026-07-08)

- **Fixed-size tiles** — cards move/reorder but **never resize**.
- **No overlap** — tiles pack into columns and **reflow on drop**. Overlap is
  reserved for overlays (modals); content tiles tile.
- **Default is always the fallback** — never destroyed.
- **Global pin**, not per-card:
  - **Unpinned** = session-only; a restart returns to Default.
  - **Pinned** = the whole arrangement is saved and survives a restart.
- **Arrange chrome only in Arrange mode** — grip / title / hide appear when you
  click **Arrange layout**; during operation tiles are clean (protects the
  fast-path, costs zero pixels). Agreed chrome model: a uniform frame the layout
  layer supplies, not baked into each card.
- **Rail becomes show/hide-a-tile** (the "+ Add tile" menu) — retiring the
  single-slot InfoPanel workaround.

**Persistence is NOT wired.** "Simulate restart" fakes a fresh launch in memory
so the pin semantics can be felt without a storage backend.

## The one thing to feel

**Reflow-on-drop** — drop a tile and its neighbours shuffle to pack. Standard
dashboard behaviour; the point of the POC is to confirm it feels right here.

## Design guards for the lift into `frontend/app`

The POC already embodies these so the lift is mechanical, and so "make it
per-op-profile" (eventual) is wiring, not a refactor:

1. **Layout is a plain serialisable value** — ordered tile ids per column + a
   hidden set (see `DEFAULT_LAYOUT`). **No pixel coordinates.** The same shape
   persists to `localStorage` today or a `config.json` op-profile field later.
2. **State-driven** — the drag handler mutates the layout value and re-renders;
   the DOM is the transient view, the value is the source of truth. In
   `frontend/app` this becomes a `lib/operate/*.svelte.ts` rune module.
3. **Persistence is an injected seam** — when lifted, the state module gets
   `loadLayout` / `saveLayout` injected in `main.ts` (like `setMailer` /
   `setModeMappings`), never calling storage directly. localStorage now →
   `GET/PUT /v1/config` per active profile later, unchanged layout code.
4. **Profile-keyed** — persist under a composable key (`sm.layout.<profileId>`,
   `profileId = 'default'` for now). Real profiles = more keys, not a schema
   change; leaves room for a per-Operate-sub-mode key too
   (`<profileId>.<subMode>`) if Phone/CW and FT8 want different layouts.

**Out of scope here (on purpose):** the op-profile system itself (daemon profile
list, picker, config schema), real persistence, resize, and modal/overlay
behaviour. This POC is interaction-only.
