# Current work

Updated: 2026-08-28

- **Goal:** W-0008 (harden audited contract boundaries) — slice 1 (Persistence). **PT-3, PT-4, PT-5 COMPLETE + verified; PT-6/CC-5 next (needs an operator decision).** W-0001/W-0003/W-0004/W-0005 closed; [`W-0004`](archive/work/W-0004-complete-app-ui-cohesion.md) archived. [`docs/backlog.md`](backlog.md) owns priority.
- **State:** PT-5 shipped: the batch import now **aborts** (preserving both the insert and rollback causes) instead of replaying per-record after an **unverified** rollback; a confirmed/benign (`sql.ErrTxDone`) rollback still falls back with correct counts. `rollbackTx` classifies via `txutil.Rollback` (shared EH-6 policy). PT-3/PT-4 shipped earlier. RED→GREEN; qsoservice + sqlite (incl. a sqlmock import test in `sqlite_test`), `-race`, gofmt/vet, and both maintainability gates green. `main` green at `15b2232b`.
- **Next:** slice 1 finishes with **PT-6/CC-5** (crash-durable config replacement — `fsync` temp + dir; **needs an operator decision** on how to report a failure after rename but before the directory sync completes). Then slices 2–4. W-0002 (FT8 type-4 on-air) stays RF-gated. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) bridge sub-item A latent.
- **Decisions not to revisit:** W-0004 named palettes DECLINED (light/dark is the set). W-0008 operator decisions pending at their slice: PT-6, AW-3/AW-6/AW-1.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
