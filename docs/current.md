# Current work

Updated: 2026-08-27

- **Goal:** W-0003 (complete the app shell) — move the consolidated SPA to the canonical root and add route-level lazy loading. Dossier: [`W-0003`](work/W-0003-retire-legacy-operator-spas.md). W-0001 and W-0005 closed and archived.
- **State:** **Slices A + B DONE, local.** A (root move): app serves at `/`; `/app*`→root 301 (open-redirect-safe); `/config`/`/logbook` are shell routes; the `spaHandler` guard 404s `/v1`+`/debug/pprof`; `vite base '/'`; `api-endpoints.md` updated. B (lazy loading, AC6): Logbook, Settings and the FT8 subtree are separate on-demand chunks (`{#await import()}`) — the eager entry dropped **383→199 kB** (gzip 116→59). AC1/AC5/AC6/AC7 met.
- **Next:** **W-0003 closure sign-off** (archive the dossier, drop from backlog, advance this file) once pushed and CI-green. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) stays open (bridge streaming-startup flake latent/unfixed).
- **Decisions not to revisit:** W-0003 — Dashboard + `startup_view` deferred to a separate dossier (`/` keeps the placeholder, do NOT build Dashboard); `/app*` redirects PERMANENT (301, bookmarks); root SPA fallback keeps `/v1`+`/debug/pprof` honest 404s; a headless daemon registers no SPA routes.
- **Do not:** build the Dashboard or the whole-log map under W-0003; re-open a closed dossier (W-0001, W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0003`](work/W-0003-retire-legacy-operator-spas.md), [`backlog`](backlog.md), [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
