# Current work

Updated: 2026-08-17

- **Goal:** Make the documentation navigable and agent context bounded, in small tidy-up slices that stay separate from ADR 0070 lifecycle code.
- **State:** The generic root and bridge kernels plus the bounded current capsule are live. FT8's duplicated shipped chronology has been removed from automatic context, cutting its legacy scoped file below 20 KB.
- **Next:** Distil the explanations around FT8's retained safety invariants and coordinated-edit checklist to approximately 10 KB, then migrate that scoped file to `AGENTS.md`. Lifecycle work proceeds independently.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB canonical kernel; `CLAUDE.md` is import-only; this file is <=2 KB; records and history are never automatic context; `docs/backlog.md` alone owns priority.
- **Do not:** Mix documentation cleanup into lifecycle commits, reorganize the backlog/archive opportunistically, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/README.md`](README.md), [`docs/reviews/oss-maintainability-plan.md`](reviews/oss-maintainability-plan.md), [`docs/v2-design/lifecycle.md`](v2-design/lifecycle.md).
- **Coordination:** Derive branch, worktree, upstream, and recent-commit facts from Git; commit only intended paths and leave pushing to the operator.
