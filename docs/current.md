# Current work

Updated: 2026-08-20

- **Goal:** Keep documentation navigable and agent context bounded, and clear the ranked config-contract P1s, without mixing either into ADR 0070 lifecycle work.
- **State:** Documentation routing is live; the whole-tree logging-gaps audit is closed and its six package reviews deleted. OSS tidy-up tickets 1–3 (kernel, capsule, routing) are delivered; ticket 4 (Phase 1.4 living-work decomposition) is next. CC-1's reject-unknown-keys ruling is ratified — [ADR 0074](decisions/0074-reject-unknown-config-keys-before-any-write.md), dossier [W-0006](work/W-0006-reject-unknown-config-keys.md); implementation not started.
- **Next:** Implement CC-1 (W-0006) — reject unknown config keys before any write, absorbing CC-2; `config.md` + code + TDD in one commit. Then OSS Phase 1.4: decompose the ~164 KB backlog / ~193 KB inbox into bounded indexes + dossiers (routing, not a bulk rewrite).
- **Decisions not to revisit:** `AGENTS.md` is the <=8 KB kernel; `CLAUDE.md` is import-only; this file is <=2 KB; `docs/README.md` is generated; records stay cold; `docs/backlog.md` alone owns priority.
- **Do not:** mix documentation or config work into lifecycle commits, bulk-rewrite the backlog/inbox/archive, touch `config.md` before the CC-1 code lands, initiate RF or hardware actions, amend commits, or add agent taglines.
- **Relevant files:** [`backlog`](backlog.md), [`catalog.json`](catalog.json), [`config reference`](v2-design/config.md), [W-0006](work/W-0006-reject-unknown-config-keys.md), [`OSS plan`](reviews/oss-maintainability-plan.md), [`docscatalog`](../cmd/docscatalog).
- **Coordination:** Keep W-0003's remaining app-shell work separate from documentation/config slices. Derive Git facts from Git, commit only intended paths, and leave pushing to the operator.
