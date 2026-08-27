# @station-manager/app

The consolidated operator SPA (ADR 0044) — one Svelte 5 + Vite app that replaces
the separate `logging`, `config`, and `logbook` SPAs behind a single shell
(dashboard · Operate · Logbook · Settings; the manual stays a separate zero-JS
site per ADR 0036).

**Status: shipped.** The sole embedded operator SPA — it replaced and retired the
separate `logging`, `config`, and `logbook` clients and is served at the canonical
root `/` (W-0003). The design is settled in two places:

- **Visual / IA spec:** `docs/v2-design/shell-mock/index.html` (the throwaway
  Tailwind-v4 mock the shell was designed in).
- **Architecture / behaviour spec:** `docs/decisions/0044-consolidate-operator-spas-into-one-shell.md`
  (see the 2026-07-06 amendment for the operating-surface + CAT-gate decisions).

## Dev

```
npm install
npm run dev      # http://localhost:5176 (proxies /v1 → :8080)
npm run check    # svelte-check
npm run lint
npm run test
```

Desktop-only (64rem min-width floor); no mobile/tablet layout — that's a separate
future effort tied to online/smcloud access, not a responsive pass on this app.

The design system (theme tokens, `[data-theme]` dark swap) lives in
`src/styles/app.css`, carried 1:1 from the mock. Tailwind compiles at build time
via `@tailwindcss/vite` — no CDN.
