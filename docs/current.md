# Current work

Updated: 2026-08-28

- **Goal:** W-0008 (harden audited contract boundaries) — slice 1 (Persistence). **PT-3** (session-email revision stamping) COMPLETE + verified; **PT-4 next**. W-0001/W-0003/W-0004/W-0005 closed; [`W-0004`](archive/work/W-0004-complete-app-ui-cohesion.md) archived. [`docs/backlog.md`](backlog.md) owns priority.
- **State:** PT-3 shipped: the session-email stamp is revision-guarded (matches `(id, revision)` via `json_each`, scale-safe to the 10k request cap) and `emailed` reports only durably-stamped rows; a stamp error still returns an empty set. RED→GREEN; api `-race` + sqlite + gofmt/vet green. `main` green at `4a2576c5`.
- **Next:** slice 1 continues **PT-4** (partial logbook PATCH concurrent-revision guard), then PT-5 (import rollback), PT-6/CC-5 (config crash-durability; needs an operator decision). Then slices 2–4. W-0002 (FT8 type-4 on-air) stays RF-gated. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) bridge sub-item A latent.
- **Decisions not to revisit:** W-0004 named palettes DECLINED (light/dark is the set). W-0008 operator decisions pending at their slice: PT-6, AW-3/AW-6/AW-1.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
