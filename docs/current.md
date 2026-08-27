# Current work

Updated: 2026-08-27

- **Goal:** W-0003 (complete the app shell) — move the consolidated SPA to the canonical root and add route-level lazy loading. Dossier: [`W-0003`](work/W-0003-retire-legacy-operator-spas.md). W-0001 and W-0005 closed and archived.
- **State:** **Slice A (root move) DONE, local** — the app serves at `/` (root `GET /` SPA mount); the `/config`/`/logbook` 307 redirects are gone (now shell routes); `/app*` 301-redirects to root (query preserved); the `spaHandler` guard 404s the `/v1`+`/debug/pprof` server namespaces; `vite base: '/'` + rebuilt `dist/index.html`; `api-endpoints.md` updated (AC7). AC1/AC5/AC7 met.
- **Next:** **Slice B (route-level lazy loading, AC6)** — Operate (Phone/CW) eager; lazy-load Logbook + Settings (mirror Map's `{#await import()}` in `App.svelte`) and split the FT8 subtree out of the eager Operate chunk (`Operate.svelte`'s `Ft8View`); prove the lazy chunks aren't fetched on the initial Operate load. Then W-0003 closure sign-off. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) stays open (bridge streaming flake latent/unfixed).
- **Decisions not to revisit:** W-0003 — Dashboard + `startup_view` deferred to a separate dossier (`/` keeps the placeholder, do NOT build Dashboard); `/app*` redirects PERMANENT (301, bookmarks); root SPA fallback keeps `/v1`+`/debug/pprof` honest 404s; a headless daemon registers no SPA routes.
- **Do not:** build the Dashboard or the whole-log map under W-0003; re-open a closed dossier (W-0001, W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0003`](work/W-0003-retire-legacy-operator-spas.md), [`backlog`](backlog.md), [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
