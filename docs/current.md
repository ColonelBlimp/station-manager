# Current work

Updated: 2026-09-21

- **Goal:** W-0021 (SELECTED 2026-09-21): first-class QSO archives per ADR 0071, files first — identities and in-place adoption before catalogue, activation, SPA and SM Cloud identity. [`backlog`](backlog.md) owns priority.
- **State:** W-0010 outcome 9 CLOSED 2026-09-21 (deployed `alpha.3-17`, schema 11). W-0021 dossier opened, five slices, nothing in code yet. Station on a dev build; frozen RPM deferred.
- **Next:** rulings (a)–(c) in the W-0021 dossier (adopted-file ownership, logbook UUID backfill, SM Cloud identity as slice 5); then slice 1 (migration 0012 `archive_metadata` + `logbook.uuid`, catalogue, `default_logbook_id` projection) RED-first. Order: archives → Settings → Logbooks → contesting; W-0012's 3 slices independent. **alpha.3 FROZEN** at `333427ea`; [record](reports/dogfood-acceptance-v2.0.0-alpha.3.md) Gate A pending. W-0020 AC2 awaits passive rows. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660; the W-0019 G3/AC7 waiver is never cited for a timing or admission-edge change. `txConfirmTimeout` DEFERRED (on-air approval).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`ADR 0071`](decisions/0071-first-class-qso-archives.md), [`W-0010`](work/W-0010-forwarding-data-and-sync-reliability.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
