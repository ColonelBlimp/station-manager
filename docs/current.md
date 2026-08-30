# Current work

Updated: 2026-08-30

- **Goal:** W-0008 (harden audited contract boundaries). Slices 1–2 are complete and pushed. Slice 3's alpha.2 sequence AW-2→AW-3→AW-5→AW-6→AW-4→AW-1 is complete; AW-1 remains open only for its release-gated alpha.3 removal. [`docs/backlog.md`](backlog.md) owns priority.
- **State:** AW-1 alpha.2 is complete: `qso.*`/`forward.*` events carry `qso_uuid`; public QSO responses use an explicit projection; SM Cloud payloads omit daemon-local QSO/logbook ids; and the SPA requires UUIDs and keys selection, rendering, edit replacement, and re-enrichment on them. Deprecated public numeric QSO identities remain for alpha.2 compatibility only. ADR 0016 records the dated outcome and [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md) owns the finalized alpha.3 checklist. Gates are green; the source change is committed locally and not pushed.
- **Next:** Operator selection of the next numbered W-0008 slice. Do not pull the alpha.3 removals into alpha.2; execute that checklist as one coherent `v2.0.0-alpha.3` change. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) is latent; W-0002 remains RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`ADR 0016`](decisions/0016-sm-cloud-deferred-with-prep.md), [`API reference`](v2-design/api-endpoints.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
