# Current work

Updated: 2026-08-27

- **Goal:** No workstream selected. W-0004 (app UI cohesion + build identity) closed 2026-08-27 and archived to [`archive/work/`](archive/work/W-0004-complete-app-ui-cohesion.md); W-0001, W-0003, W-0005 closed earlier. Pick the next item from [`docs/backlog.md`](backlog.md); the operator owns priority.
- **State:** W-0004 shipped and green on `main` in three commits — occupancy palette `d6fa11cd`, build identity `86f3f833`, ordering-guard fix `84214b76` — full app suite green (1370). All seven ACs met: FT8 pickers read one fixture-validated light/dark `--color-occ-*` palette; the Sidebar footer + tab title show the running daemon build (DEV-marked, honest "unavailable"); named palettes declined. Pushed to `origin/main` (closure `75d99222`).
- **Next:** Operator selects the next workstream. Backlog top is W-0002 (FT8 type-4 on-air validation — operator-initiated RF), then W-0008. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) stays open: evidence `SQLITE_BUSY` fixed, but **bridge sub-item A (streaming-startup barrier) is latent/unfixed** — fix on next recurrence or when touching `internal/bridge`.
- **Decisions not to revisit:** W-0004 — named palettes DECLINED; light/dark is the complete supported theme set (device-local, ADR 0044, no config/API surface). `docs/backlog.md` alone owns priority.
- **Do not:** re-open a closed dossier (W-0001, W-0003, W-0004, W-0005) without a fresh one; initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md), [`W-0004` archived](archive/work/W-0004-complete-app-ui-cohesion.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
