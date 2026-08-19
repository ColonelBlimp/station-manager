# Current work

Updated: 2026-08-19

- **Goal:** Keep documentation navigable and agent context bounded without mixing it into ADR 0070 lifecycle work.
- **State:** Documentation routing is live. All three legacy operator SPAs are retired under preservation tags, and `frontend/app` is the sole embedded operator SPA. The canonical config reference is now a current contract rather than review/redesign chronology. [W-0003](work/W-0003-retire-legacy-operator-spas.md) remains open for the canonical-root and route-level lazy-loading acceptance work.
- **Next:** Migrate the first active unmigrated P2 item—operator-triggered forwarder queue clearing—from the ranked backlog into `W-0005`, preserving its rank and narrowing its acceptance boundary without implementing it.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated; records stay cold; `docs/backlog.md` alone owns priority.
- **Do not:** Fold W-0003's remaining shell work into documentation cleanup, mix documentation cleanup into lifecycle commits, bulk-rewrite the backlog/inbox/archive, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/catalog.json`](catalog.json), [`docs/backlog.md`](backlog.md), [`config reference`](v2-design/config.md), [W-0003](work/W-0003-retire-legacy-operator-spas.md), [`docscatalog`](../cmd/docscatalog), [`OSS maintainability plan`](reviews/oss-maintability-plan.md).
- **Coordination:** Legacy-SPA retirement is complete; keep W-0003's remaining app-shell changes separate from the documentation-library slices. Derive Git facts from Git, commit only intended paths, and leave pushing to the operator.
