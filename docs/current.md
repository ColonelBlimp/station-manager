# Current work

Updated: 2026-10-01

- **Goal:** W-0021: QSO archives; saved-QSO recovery per [ADR 0085](decisions/0085-preserve-qso-drafts-with-their-original-archive.md) and [ADR 0086](decisions/0086-open-saved-qsos-from-a-header-indicator.md). [`backlog`](backlog.md) owns priority.
- **State:** ADR 0084, Default tags, Settings Logbooks and archive-form leave guard built. ADR 0085 rules 1–2 and rule 3 slice 1 built; cross-tab saved-QSO lists, ADR 0086 header/Map panel and Escape fixes committed. Through `47bdbd68`: pushed, CI passed, clean tree, no pending Codex reviews (operator report). Restore remains unbuilt.
- **Next:** set out Restore acceptance criteria, including exclusive cross-tab claims, for operator review. Post-deploy visual checks: narrow header and Map panel. "Reading when saved" needs a live observation recording view and CAT state. Separate inbox/queue: country entry, W-0017 completion barrier, rig onboarding/drills C–F, `-74` C.2 ruling, 5C/5F, wording sweep. **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
