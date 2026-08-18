# Current work

Updated: 2026-08-18

- **Goal:** Make the documentation navigable and agent context bounded, in small tidy-up slices that stay separate from ADR 0070 lifecycle code.
- **State:** The live-document catalog is shipped and the bounded P1 reconciliation is complete; the operator closed the remaining session-192/193 behavioral reruns on 2026-08-18.
- **Next:** Verify the highest-ranked P2 release-gate finding against current code, then pilot one stable-ID dossier under `docs/work/`; leave the dogfood inbox for a separate pilot.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB canonical kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated from the live catalog; records are convention-routed and never automatic context; `docs/backlog.md` alone owns priority.
- **Do not:** Mix documentation cleanup into lifecycle commits, bulk-rewrite the backlog/inbox/archive, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/catalog.json`](catalog.json), [`docs/README.md`](README.md), [`docs/backlog.md`](backlog.md), [`docs/reviews/oss-maintainability-plan.md`](reviews/oss-maintainability-plan.md).
- **Coordination:** Derive branch, worktree, upstream, and recent-commit facts from Git; commit only intended paths and leave pushing to the operator.
