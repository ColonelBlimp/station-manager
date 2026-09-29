# Current work

Updated: 2026-09-29

- **Goal:** W-0021 (SELECTED 2026-09-21): first-class QSO archives per ADR 0071; slice 5 = per-logbook destination bindings per [ADR 0082](decisions/0082-per-logbook-destination-bindings-in-the-archive.md). [`backlog`](backlog.md) owns priority.
- **State:** 5A–5E shipped; the post-test bundle is built (`3fc11918`..`9f2a35d0`; [W-0021](work/W-0021-qso-archives.md)), not deployed. ClubLog confirmed key embedding ([ADR 0083](decisions/0083-every-distributed-build-carries-the-clublog-application-key.md)). ADR 0084 archive contents slices 1–3 complete; full local CI green.
- **Next:** operator deploys; rig onboarding from clean, drills C–F; the `-74` C.2 refusal ruling; styled dialogs; archive-activation draft rule (inbox); 5C, 5F. **alpha.3 FROZEN** at `333427ea`. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
