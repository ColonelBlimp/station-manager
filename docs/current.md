# Current work

Updated: 2026-09-05

- **Goal:** W-0008 (harden audited contract boundaries) — finish slice 4's rig/FT8 confirm-by-push (F-04), then freeze a dogfood candidate for the borrowed IC-7300. [`backlog`](backlog.md) owns priority.
- **State:** F-04 confirm-by-push shipped for tune, rig commands and FT8 arm ([`ADR 0078`](decisions/0078-ambiguous-write-outcome-policy.md)): a timed-out write reconciles against pushed state — observed, or unknown — never a definite failure. The rig lane shares one `RigWriteResult`; FT8 arm keeps its own result. Unpushed. AW-1 alpha.3 removals stay release-gated ([`W-0008`](work/W-0008-harden-audited-contract-boundaries.md)).
- **Next:** `task ci:local`; freeze the candidate — one PocketFFT build through Gate A ([`dogfood acceptance`](dogfood-acceptance.md)), deploy that exact artifact, then the IC-7300 in order: passive state, no-RF CAT, idle arm/disarm, tune/keyed safety, FT8 TX. ft8-sequencer and rig-recheck slices follow. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`ADR 0078`](decisions/0078-ambiguous-write-outcome-policy.md), [`API reference`](v2-design/api-endpoints.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
