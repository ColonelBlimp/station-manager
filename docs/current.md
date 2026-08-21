# Current work

Updated: 2026-08-21

- **Goal:** Close the OSS documentation-maintainability architecture-map slice without mixing it into config or ADR 0070 lifecycle work.
- **State:** Documentation tickets 1–4 and the current architecture map are delivered. [`architecture.md`](architecture.md) is the Tier 1 topology, ownership, flow, safety, and change-routing map; `docs:check` validates relative links in live documents and public indexes. Compact work indexes, the retired session archive, context budgets, and CI ratchets remain in force. Contributor onboarding is trigger-bound; test residuals are routed to [W-0009](work/W-0009-maintainability-audit-residuals.md). CC-1 remains ratified in [ADR 0074](decisions/0074-reject-unknown-config-keys-before-any-write.md) / [W-0006](work/W-0006-reject-unknown-config-keys.md), with implementation paused.
- **Next:** Hand the completed architecture-map slice to the operator, then select any further work from [`backlog.md`](backlog.md). Do not begin config or lifecycle work as part of this slice.
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated; records stay cold; `docs/backlog.md` alone owns priority.
- **Do not:** mix documentation, config, or lifecycle commits; revive rolling histories; touch `config.md`; initiate RF/hardware actions; amend commits; or add agent taglines.
- **Relevant files:** [`architecture`](architecture.md), [`catalog.json`](catalog.json), [`generated map`](README.md), [`API reference`](v2-design/api-endpoints.md), [`backlog`](backlog.md), [`OSS plan`](reviews/oss-maintainability-plan.md).
- **Coordination:** Keep this documentation slice self-contained; leave committing and pushing to the operator.
