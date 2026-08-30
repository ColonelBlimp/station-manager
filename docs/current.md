# Current work

Updated: 2026-08-29

- **Goal:** W-0008 (harden audited contract boundaries). Slices 1–2 DONE + pushed (Persistence PT-3/4/5/6; Config CC-3 `f7d1de4a` + CC-4 `d72e0bfb` + docs `b354ac77`). **Slice 3 (API wire) in progress — approved rulings, sequence AW-2→AW-3→AW-5→AW-6→AW-4→AW-1 alpha.2. AW-2 done (uncommitted until directed).** W-0001/W-0003/W-0004/W-0005 closed. [`docs/backlog.md`](backlog.md) owns priority; rulings in [[w0008-slice3-rulings]].
- **State:** AW-2 done: the 3 command booleans (rig tune `active`, FT8 tx `armed`, FT8 skip `armed`) are presence-aware `*bool` via a strict `readCommandJSON` — absent → 400 `missing_required_field` (never the silent `false` op), unknown/duplicate/malformed → 400 `invalid_json`, no service call; the 2 FT8 QSO-start routes reject unknown fields except the `auto_work` alias. RED-first + reversion-proved; api + gofmt/golangci/observatory green; api-endpoints.md updated.
- **Next:** **AW-3** — empty/no-effective-change QSO PATCH → 200 no-op (canonical-equivalence = no change; unknown-only stays lenient), zero side effects. RED-first, present before commit. Then AW-5→AW-6→AW-4→AW-1 alpha.2. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
