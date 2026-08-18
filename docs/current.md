# Current work

Updated: 2026-08-18

- **Goal:** Make the documentation navigable and agent context bounded, in small tidy-up slices that stay separate from ADR 0070 lifecycle code.
- **State:** The live catalog and bounded P1 reconciliation are shipped; the first stable-ID work dossier, `W-0001`, now routes the verified durable-notification release gap out of the backlog.
- **Next:** Review the dossier pilot, then migrate the next verified open backlog item in a separate bounded slice; leave the dogfood inbox for its own pilot.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB canonical kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated from the live catalog; records are convention-routed and never automatic context; `docs/backlog.md` alone owns priority.
- **Do not:** Mix documentation cleanup into lifecycle commits, bulk-rewrite the backlog/inbox/archive, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/catalog.json`](catalog.json), [`docs/README.md`](README.md), [`docs/backlog.md`](backlog.md), [`docs/work/W-0001-durable-notifications.md`](work/W-0001-durable-notifications.md), [`docs/reviews/oss-maintainability-plan.md`](reviews/oss-maintainability-plan.md).
- **Coordination:** Derive branch, worktree, upstream, and recent-commit facts from Git; commit only intended paths and leave pushing to the operator.
