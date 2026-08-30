# Current work

Updated: 2026-08-30

- **Goal:** W-0008 (harden audited contract boundaries). Slices 1–2 DONE + pushed. **Slice 3 (API wire): sequence AW-2→AW-3→AW-5→AW-6→AW-4→AW-1 alpha.2. AW-2/AW-3 shipped+pushed; AW-5 done (uncommitted).** [`docs/backlog.md`](backlog.md) owns priority; rulings in [[w0008-slice3-rulings]].
- **State:** AW-5 done: SM Cloud ingest routes both handlers (`handlePutQsos`/`handlePutEvidence`) through one shared `decodeSingleJSON`/`rejectBody` — an oversized body (`*http.MaxBytesError` on either decode) → 413 `body_too_large` (generic, matching the daemon), syntax/trailing → 400 `invalid_body` with the decoder detail LOGGED not returned; one-document + 32 MiB cap preserved. Code-only (no `api-endpoints.md` change: it doesn't own the SM Cloud ingest contract; historical `api.md`/`frontend-spa.md` records left untouched). RED-first + reversion-proved; whole-tree tests/vet, gofmt, golangci, observatory green.
- **Next:** **AW-6** — SM Cloud QSO batch: cap 1,000 rows (`maxQsoBatchRows`), reject 1,001 with `batch_too_large` BEFORE recs-slice alloc / logbook provision / store / txn; keep the 32 MiB byte cap independent. Then AW-4→AW-1 alpha.2. RED-first, present before commit. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
