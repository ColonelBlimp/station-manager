# Current work

Updated: 2026-08-31

- **Goal:** W-0008 (harden audited contract boundaries). Slices 1–2 done+pushed; slice 3 alpha.2 done (AW-1's alpha.3 removal release-gated); slice 4 F-03 done+pushed, F-04a done (unpushed). [`backlog`](backlog.md) owns priority.
- **State:** Slice 4 shipped — F-03 validates SPA success/safety wire records (submit, RF/FT8 SSE, list/page/edit; [`ADR 0077`](decisions/0077-spa-runtime-wire-decoders.md)); F-04a makes a timed-out QSO PATCH / upload enqueue reconcile to committed-or-unknown, never definite failure ([`ADR 0078`](decisions/0078-ambiguous-write-outcome-policy.md)). AW-1 alpha.2 done (UUID at every boundary, public/cloud QSO projections, SPA keys on `uuid`; numeric ids kept for alpha.2 only — ADR 0016, alpha.3 checklist in [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md)). Through `eb23d1cf` pushed + CI-green; `9491d56c` (F-04a) unpushed.
- **Next:** Remaining F-04 write surfaces (config re-read, rig/FT8 confirm-by-push, email/export, restart) not started. Run the alpha.3 removals as one coherent `v2.0.0-alpha.3` change. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`ADR 0016`](decisions/0016-sm-cloud-deferred-with-prep.md), [`ADR 0077`](decisions/0077-spa-runtime-wire-decoders.md), [`ADR 0078`](decisions/0078-ambiguous-write-outcome-policy.md), [`API reference`](v2-design/api-endpoints.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
