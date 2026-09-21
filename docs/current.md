# Current work

Updated: 2026-09-21

- **Goal:** W-0010 outcome 9 (SELECTED 2026-09-20): failed rows read apart from waiting, auth failures recover, QRZ id-less delete is a no-op. [`backlog`](backlog.md) owns priority.
- **State:** outcome 9 slices 1–3 committed 2026-09-20; slice 4 (waiting/failed counts, `queue/retry`, card + gap link) built 2026-09-21; rulings (a)–(d) in the dossier. Station on a dev build; frozen RPM install deferred.
- **Next:** `task deploy:local:dev`, then check the preserved QRZ fixture reads as failed on the card. Order (2026-09-19): alpha.3 acceptance → outcome 9 → ADR 0071 archives (files first) → Settings → Logbooks → contesting; W-0012's 3 slices independent. **alpha.3 FROZEN** at `333427ea` (local tag); [record](reports/dogfood-acceptance-v2.0.0-alpha.3.md) Gate A pending the operator. W-0020 AC2 awaits passive rows. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660; the W-0019 G3/AC7 waiver is never cited for a timing or admission-edge change. `txConfirmTimeout` DEFERRED (on-air approval).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0010`](work/W-0010-forwarding-data-and-sync-reliability.md), [`alpha.3 record`](reports/dogfood-acceptance-v2.0.0-alpha.3.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
