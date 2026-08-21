# Current work

Updated: 2026-08-21

- **Goal:** Finish the OSS documentation-maintainability sweep without mixing it into config or ADR 0070 lifecycle work.
- **State:** Documentation-library tickets 1–4 are delivered. The backlog is a 5.5 KB stable-ID index, the inbox is empty and routed, W-0007..W-0016 hold the remaining workstreams, the 1.61 MB session archive is retired to Git with a summary record, and `docs:check` prevents regrowth. Contributor onboarding is deferred until there is external demand. CI now publishes Go/frontend maintainability distributions and ratchets exact complexity and duplication debt. CC-1 is ratified in [ADR 0074](decisions/0074-reject-unknown-config-keys-before-any-write.md) / [W-0006](work/W-0006-reject-unknown-config-keys.md), but implementation is deliberately paused.
- **Next:** Continue the OSS current-architecture-map ticket: add `docs/architecture.md`, register it as Tier 1, check internal links, and reconcile live documentation. Keep it separate from CC-1 and ADR 0070 lifecycle work.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated; records stay cold; `docs/backlog.md` alone owns priority.
- **Do not:** mix documentation, config, or lifecycle commits; revive rolling histories; touch `config.md`; initiate RF/hardware actions; amend commits; or add agent taglines.
- **Relevant files:** [`backlog`](backlog.md), [`dogfood inbox`](dogfood-inbox.md), [`catalog.json`](catalog.json), [`work dossiers`](work/), [`session-history record`](reports/session-history-retirement.md), [`OSS plan`](reviews/oss-maintainability-plan.md).
- **Coordination:** Keep the architecture-map slice separate from config and lifecycle work; leave pushing to the operator.
