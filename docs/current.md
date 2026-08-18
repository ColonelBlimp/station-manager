# Current work

Updated: 2026-08-18

- **Goal:** Make the documentation navigable and agent context bounded, in small tidy-up slices that stay separate from ADR 0070 lifecycle code.
- **State:** The live catalog, bounded P1 reconciliation, and dossier pilot are shipped; nine resolved logging-audit rows now live in the archive, while `W-0001`–`W-0003` route the first verified open outcomes. W-0003 corrects the stale premise that all three legacy SPAs remain live.
- **Next:** Verify and migrate the next ranked open backlog item into `W-0004`; leave the dogfood inbox for its own pilot.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB canonical kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated from the live catalog; records are convention-routed and never automatic context; `docs/backlog.md` alone owns priority.
- **Do not:** Mix documentation cleanup into lifecycle commits, bulk-rewrite the backlog/inbox/archive, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/catalog.json`](catalog.json), [`docs/README.md`](README.md), [`docs/backlog.md`](backlog.md), [`docs/backlog-archive.md`](backlog-archive.md), [`docs/work/W-0001-durable-notifications.md`](work/W-0001-durable-notifications.md), [`docs/work/W-0002-ft8-type4-on-air-validation.md`](work/W-0002-ft8-type4-on-air-validation.md), [`docs/work/W-0003-retire-legacy-operator-spas.md`](work/W-0003-retire-legacy-operator-spas.md), [`docs/reviews/oss-maintainability-plan.md`](reviews/oss-maintainability-plan.md).
- **Coordination:** Derive branch, worktree, upstream, and recent-commit facts from Git; commit only intended paths and leave pushing to the operator.
