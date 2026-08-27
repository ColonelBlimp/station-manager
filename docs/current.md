# Current work

Updated: 2026-08-27

- **Goal:** No workstream selected. W-0003 (complete the app shell) closed 2026-08-27 and archived to [`archive/work/`](archive/work/W-0003-retire-legacy-operator-spas.md); W-0001 and W-0005 closed earlier. Pick the next item from [`docs/backlog.md`](backlog.md); the operator owns priority.
- **State:** W-0003 shipped and CI-green on `main` (`a7e12ca4`), all seven ACs met — the app serves at the canonical root (`/config`/`/logbook` shell routes; `/app*`→root 301, open-redirect-safe), server namespaces stay honest 404s, a headless daemon serves no SPA, and Logbook/Settings/FT8 are on-demand chunks (eager entry 383→199 kB).
- **Next:** Operator selects the next workstream. Backlog top is W-0002 (FT8 type-4 on-air validation — operator-initiated RF), then W-0004 (app UI cohesion). [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) stays open: evidence `SQLITE_BUSY` fixed, but **bridge sub-item A (streaming-startup barrier) is latent/unfixed** — fix on next recurrence or when touching `internal/bridge`.
- **Decisions not to revisit:** W-0003 — Dashboard + `startup_view` deferred to a separate dossier (`/` keeps the placeholder); `/app*` redirects PERMANENT (301). `docs/backlog.md` alone owns priority.
- **Do not:** build the Dashboard/whole-log map without a fresh dossier; re-open a closed dossier (W-0001, W-0003, W-0005) without a fresh one; initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md), [`W-0003` archived](archive/work/W-0003-retire-legacy-operator-spas.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
