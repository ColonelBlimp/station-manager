# Current work

Updated: 2026-08-17

- **Goal:** Make the documentation navigable and agent context bounded, in small tidy-up slices that stay separate from ADR 0070 lifecycle code.
- **State:** The generic instruction kernel is live. The lifecycle orchestrator, real-graph acceptance gate, and evidence Supervisor adoption have shipped; this capsule replaces the growing handoff slice as session orientation.
- **Next:** Take one documentation-library slice at a time. Lifecycle work proceeds independently with pskreporter, then refresher, as separate behavior-preserving commits.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB canonical kernel; `CLAUDE.md` is import-only; this file is <=2 KB; records and history are never automatic context; `docs/backlog.md` alone owns priority.
- **Do not:** Mix documentation cleanup into lifecycle commits, reorganize the backlog/archive opportunistically, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/README.md`](README.md), [`docs/reviews/oss-maintainability-plan.md`](reviews/oss-maintainability-plan.md), [`docs/v2-design/lifecycle.md`](v2-design/lifecycle.md).
- **Coordination:** Derive branch, worktree, upstream, and recent-commit facts from Git; commit only intended paths and leave pushing to the operator.
