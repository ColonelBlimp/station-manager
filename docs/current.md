# Current work

Updated: 2026-09-21

- **Goal:** W-0010 outcome 9 (SELECTED 2026-09-20): failed rows read apart from waiting, auth failures recover, QRZ id-less delete is a no-op. [`backlog`](backlog.md) owns priority.
- **State:** outcome 9 slices 1–4 deployed as `2.0.0-alpha.3-17-gc295b627` (schema 11). QRZ card reads 0 waiting / 0 failed / 0 in flight; the preserved failed-row fixture had already been discarded on 2026-09-12 while QRZ was disabled. Tests prove failed-row behavior; the live failed state is unobserved.
- **Next:** do not backfill QSO 7025 to QRZ for acceptance: the operator confirmed it was a UI test, not a contact (alpha.2 Finding 19). Decide whether test proof plus passive station evidence closes outcome 9; then ADR 0071 archives (files first). Order (2026-09-19): alpha.3 acceptance → outcome 9 → archives → Settings → Logbooks → contesting. **alpha.3 FROZEN** at `333427ea` (local tag); [record](reports/dogfood-acceptance-v2.0.0-alpha.3.md) Gate A pending the operator. W-0020 AC2 awaits passive rows. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660; the W-0019 G3/AC7 waiver is never cited for a timing or admission-edge change. `txConfirmTimeout` DEFERRED (on-air approval).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0010`](work/W-0010-forwarding-data-and-sync-reliability.md), [`alpha.3 record`](reports/dogfood-acceptance-v2.0.0-alpha.3.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
