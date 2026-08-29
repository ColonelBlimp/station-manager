# Current work

Updated: 2026-08-29

- **Goal:** W-0008 (harden audited contract boundaries) — slice 1 (Persistence). PT-3/PT-4/PT-5 shipped CI-green. **PT-6/CC-5 DONE (backend + SPA, uncommitted) — one combined commit closes slice 1.** W-0001/W-0003/W-0004/W-0005 closed. [`docs/backlog.md`](backlog.md) owns priority.
- **State:** PT-6/CC-5 verified (uncommitted). Backend: crash-durable `config.WriteJSON` (unique temp + fsync-before-rename + parent-dir fsync) via an unexported `fsOps` seam; the 3 `Service` persist methods return `Durability`, publish in-memory on a dir-fsync failure (coherent) as **applied-durability-uncertain**, and the PUT surfaces optional `durability:"unconfirmed"` (+ logs). SPA: shared `config/durability.ts` shows ONE combined caveat and suppresses the saved toast across all 8 config sections + first-run setup. Tests reversion-proved; all backend + frontend gates green (both Go maintainability gates, vitest 1375).
- **Next:** **Land the one combined PT-6 commit (backend + SPA + docs + tests); then slice 1 DONE → slices 2–4 (AW-3/AW-6/AW-1 need decisions).** [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) bridge sub-item A latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. Operator decisions recorded for PT-6: applied-uncertain / warning-field+log / small unexported fsOps interface.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
