# Current work

Updated: 2026-08-30

- **Goal:** W-0008 (harden audited contract boundaries). Slices 1–2 DONE + pushed. **Slice 3 (API wire): sequence AW-2→AW-3→AW-5→AW-6→AW-4→AW-1 alpha.2. AW-2/AW-3/AW-5 shipped; AW-6 done (uncommitted).** [`docs/backlog.md`](backlog.md) owns priority; rulings in [[w0008-slice3-rulings]].
- **State:** AW-6 done: `handlePutQsos` caps the batch at `maxQsoBatchRows = 1000` — a >1000-row batch is rejected 400 `batch_too_large` BEFORE the record-slice alloc, logbook provisioning, store call, or transaction; the 32 MiB byte cap stays an independent bound. Mirrors the evidence handler's cap. Code-only (cloud ingest not in `api-endpoints.md`; historical records untouched). RED-first + reversion-proved (over-cap = batch_too_large before any store access, at-cap admitted); whole-tree tests/vet, gofmt, golangci, observatory green.
- **Next:** **AW-4** — router 404/405 under `/v1/` → JSON envelope via `httpkit.WriteError` (unknown path 404 `not_found`, method mismatch 405 `method_not_allowed` + accurate `Allow`); daemon + SM Cloud; scoped to `/v1/`, non-API static/manual/pprof 404s unchanged. Then AW-1 alpha.2. RED-first, present before commit. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
