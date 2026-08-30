# Current work

Updated: 2026-08-30

- **Goal:** W-0008 (harden audited contract boundaries). Slices 1–2 DONE + pushed. **Slice 3 (API wire): sequence AW-2→AW-3→AW-5→AW-6→AW-4→AW-1 alpha.2. AW-2/AW-3/AW-5/AW-6 shipped; AW-4 done (uncommitted).** [`docs/backlog.md`](backlog.md) owns priority; rulings in [[w0008-slice3-rulings]].
- **State:** AW-4 done: the daemon's `/v1/` namespace moved to its own `apiMux` (new `apiRouter`, `route_fallback.go`), off the SPA `GET /` catch-all that poisoned ServeMux classification (unknown non-GET → 405 `Allow: GET`; phantom GET). Unmatched `/v1/` now returns the JSON envelope — unknown → 404 `not_found` (any method, no Allow); mismatch → 405 `method_not_allowed` + accurate `Allow`. Mirrored in SM Cloud (`jsonRouteErrors`; no sub-mux — no catch-all there). RED-first + reversion-proved; gates green incl. observatory; `api-endpoints.md` updated.
- **Next:** **AW-1 alpha.2** (final Slice 3) — `qso_uuid` on `qso.*`/`forward.*` events, migrate SPA consumers/selection to UUID, public QSO projection, drop local cloud ids (RETAIN deprecated numeric fields through alpha.2); bounded alpha.3 removal + dated ADR 0016 update. RED-first, present before commit. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
