# Current work

Updated: 2026-08-18

- **Goal:** Keep documentation navigable and agent context bounded without mixing it into ADR 0070 lifecycle work.
- **State:** Documentation routing is live. The logging SPA is retired under tag `legacy-logging-spa-retired`; current behavior routes to `frontend/app` and [W-0003](work/W-0003-retire-legacy-operator-spas.md). The config and logbook legacy SPAs remain.
- **Next:** After the active config-SPA retirement settles, remove its obsolete routing and reduce the canonical config reference to a current contract; then resume the next eligible dossier as `W-0005`.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated; records stay cold; `docs/backlog.md` alone owns priority.
- **Do not:** Overlap the active config-SPA retirement, mix documentation cleanup into lifecycle commits, bulk-rewrite the backlog/inbox/archive, initiate RF or hardware-dependent actions, amend commits, or add agent taglines.
- **Relevant files:** [`AGENTS.md`](../AGENTS.md), [`docs/catalog.json`](catalog.json), [`docs/backlog.md`](backlog.md), [W-0003](work/W-0003-retire-legacy-operator-spas.md), [`docscatalog`](../cmd/docscatalog), [`OSS maintainability plan`](reviews/oss-maintability-plan.md).
- **Coordination:** The operator is retiring the config SPA in parallel; re-read that tree before changing its docs. Derive Git facts from Git, commit only intended paths, and leave pushing to the operator.
