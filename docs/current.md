# Current work

Updated: 2026-09-26

- **Goal:** W-0021 (SELECTED 2026-09-21): first-class QSO archives per ADR 0071; slice 5 = per-logbook destination bindings per [ADR 0082](decisions/0082-per-logbook-destination-bindings-in-the-archive.md). [`backlog`](backlog.md) owns priority.
- **State:** slices 1–4 shipped; 5A, 5B, 5D, 5E shipped and deployed (`alpha.3-66`, schema 15, Home seeded with 4 bindings, config still v5). CI red from `7990011b` fixed by `3073c8de`.
- **Next:** Settings explanations → manual ⓘ links (4 inbox notes); operator drills B–F; 5C (config v6, rollback on copies only); then 5F. **alpha.3 FROZEN** at `333427ea`; Gate A pending. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account (2026-09-26).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`ADR 0082`](decisions/0082-per-logbook-destination-bindings-in-the-archive.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
