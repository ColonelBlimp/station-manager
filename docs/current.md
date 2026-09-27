# Current work

Updated: 2026-09-26

- **Goal:** W-0021 (SELECTED 2026-09-21): first-class QSO archives per ADR 0071; slice 5 = per-logbook destination bindings per [ADR 0082](decisions/0082-per-logbook-destination-bindings-in-the-archive.md). [`backlog`](backlog.md) owns priority.
- **State:** 5A–5E shipped; station on `alpha.3-80` (schema 15). A first-install test found onboarding gaps; [ADR 0083](decisions/0083-every-distributed-build-carries-the-clublog-application-key.md): every distributed build carries the ClubLog key.
- **Next:** the post-test build ruled in the [inbox](dogfood-inbox.md) and the W-0021 dossier (rig picker, reload after restart, FT8/FT4 switch, Settings text); then drill re-run C–F; styled dialogs; archive contents; 5C, 5F. **alpha.3 FROZEN** at `333427ea`. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md), [`ADR 0082`](decisions/0082-per-logbook-destination-bindings-in-the-archive.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
