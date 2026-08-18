# Current work

Updated: 2026-08-18

- **Goal:** Make the documentation navigable and agent context bounded, in small tidy-up slices that stay separate from ADR 0070 lifecycle code.
- **State:** The live catalog and bounded P1 reconciliation are shipped; the dossier pilot held, and `W-0001` plus `W-0002` now route two verified open outcomes without duplicating backlog priority.
- **Next:** Reconcile the closed logging-audit rows out of the live backlog in a separate bounded slice, then migrate the next verified open item; leave the dogfood inbox for its own pilot.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB canonical kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated from the live catalog; records are convention-routed and never automatic context; `docs/backlog.md` alone owns priority.
- **Do not:** Mix documentation cleanup into lifecycle commits, bulk-rewrite the backlog/inbox/archive, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/catalog.json`](catalog.json), [`docs/README.md`](README.md), [`docs/backlog.md`](backlog.md), [`docs/work/W-0001-durable-notifications.md`](work/W-0001-durable-notifications.md), [`docs/work/W-0002-ft8-type4-on-air-validation.md`](work/W-0002-ft8-type4-on-air-validation.md), [`docs/reviews/oss-maintainability-plan.md`](reviews/oss-maintainability-plan.md).
- **Coordination:** Derive branch, worktree, upstream, and recent-commit facts from Git; commit only intended paths and leave pushing to the operator.
