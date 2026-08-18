# Current work

Updated: 2026-08-18

- **Goal:** Make the documentation navigable and agent context bounded, in small tidy-up slices that stay separate from ADR 0070 lifecycle code.
- **State:** The live catalog, bounded P1 reconciliation, and dossier pilot are shipped; nine resolved logging-audit rows now live in the archive, while `W-0001`–`W-0004` route verified open outcomes. W-0004 narrows the stale cross-SPA theme cluster to its remaining app identity and palette decisions.
- **Next:** Verify and migrate the next eligible ranked open item into `W-0005`; honor the parked FT8 cluster and leave the dogfood inbox for its own pilot.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB canonical kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated from the live catalog; records are convention-routed and never automatic context; `docs/backlog.md` alone owns priority.
- **Do not:** Mix documentation cleanup into lifecycle commits, bulk-rewrite the backlog/inbox/archive, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/catalog.json`](catalog.json), [`docs/backlog.md`](backlog.md), [`W-0004`](work/W-0004-complete-app-ui-cohesion.md), [`OSS maintainability plan`](reviews/oss-maintainability-plan.md).
- **Coordination:** Derive branch, worktree, upstream, and recent-commit facts from Git; commit only intended paths and leave pushing to the operator.
