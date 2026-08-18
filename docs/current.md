# Current work

Updated: 2026-08-18

- **Goal:** Make the documentation navigable and agent context bounded, in small tidy-up slices that stay separate from ADR 0070 lifecycle code.
- **State:** The consumer audit's routing corrections are applied: dead scopes now fail validation, current Settings/FT8 paths resolve, nine logging-audit rows live in the backlog archive, and the [W-0001–W-0004 dossiers](README.md#work-items-and-dossiers) route verified outcomes.
- **Next:** After the active legacy-SPA retirement settles, re-verify and reduce the canonical config reference to a current contract; then resume the next eligible dossier as `W-0005`.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB canonical kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated from the live catalog; records are convention-routed and never automatic context; `docs/backlog.md` alone owns priority.
- **Do not:** Overlap the active logging/config SPA retirement, mix documentation cleanup into lifecycle commits, bulk-rewrite the backlog/inbox/archive, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/catalog.json`](catalog.json), [`docs/backlog.md`](backlog.md), [`docscatalog`](../cmd/docscatalog), [`OSS maintainability plan`](reviews/oss-maintainability-plan.md).
- **Coordination:** The operator is retiring the logging/config SPAs in parallel; re-read that tree before changing its docs. Derive Git facts from Git, commit only intended paths, and leave pushing to the operator.
