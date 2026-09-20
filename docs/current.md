# Current work

Updated: 2026-09-20

- **Goal:** W-0010 outcome 9 (SELECTED 2026-09-20): failed forwarder rows read apart from waiting ones, auth failures recover, QRZ id-less delete is a no-op. [`backlog`](backlog.md) owns priority.
- **State:** outcome 9 slice 1 (QRZ no-op delete) built and green 2026-09-20; post-commit review required durable upstream-ID success order (ADR 0081, log migration 0010). Rulings (a)–(d) for slices 2–4 are settled in the dossier. Station on dev build `2.0.0-alpha.3-1-gbe613793`; frozen RPM install deferred.
- **Next:** slice 2, durable nullable failure class via log migration 0011. Then boot re-arm and the card/API slice. Order (2026-09-19): alpha.3 acceptance → outcome 9 → ADR 0071 archives (files first) → Settings → Logbooks → contesting; W-0012's 3 ruled slices independent. **alpha.3 FROZEN 2026-09-19** at `333427ea` (local tag); [record](reports/dogfood-acceptance-v2.0.0-alpha.3.md) Gate A pending the operator. W-0020 AC2 awaits passive rows. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660; the W-0019 G3/AC7 waiver is never cited for a timing or admission-edge change. `txConfirmTimeout` DEFERRED (on-air approval).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0010`](work/W-0010-forwarding-data-and-sync-reliability.md), [`alpha.3 record`](reports/dogfood-acceptance-v2.0.0-alpha.3.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`W-0020`](work/W-0020-station-events.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review to triage.
