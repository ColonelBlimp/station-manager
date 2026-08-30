# Current work

Updated: 2026-08-30

- **Goal:** W-0008 (harden audited contract boundaries). Slices 1–2 DONE + pushed. **Slice 3 (API wire): sequence AW-2→AW-3→AW-5→AW-6→AW-4→AW-1 alpha.2. AW-2 shipped `d2f2654a`; AW-3 done (uncommitted).** [`docs/backlog.md`](backlog.md) owns priority; rulings in [[w0008-slice3-rulings]].
- **State:** AW-3 done: `qsoservice.Update` short-circuits a no-effective-change PATCH (empty/`{}`/unknown-only/immutable-only/canonically-equivalent) via an `editableView` projection — returns the existing row with ZERO side effects (no revision/modified bump, history row, re-arm, or `qso.updated`). Extracted `recomputeDedupeKey` (Update cognitive 54→50, baseline ratcheted). RED-first + reversion-proved + real-edit control; whole-tree tests/vet, gofmt, golangci, observatory green; api-endpoints.md updated.
- **Next:** **AW-5** — SM Cloud oversized body → 413 `body_too_large` (generic, no decoder string) on both ingest handlers via one shared decoder; syntax → 400 `invalid_body`; update ONLY api-endpoints.md. Then AW-6→AW-4→AW-1 alpha.2. RED-first, present before commit. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
