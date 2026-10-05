# Current work

Updated: 2026-10-05

- **Goal:** W-0021: QSO archives; saved-QSO recovery per [ADR 0085](decisions/0085-preserve-qso-drafts-with-their-original-archive.md) and [ADR 0086](decisions/0086-open-saved-qsos-from-a-header-indicator.md). [`backlog`](backlog.md) owns priority.
- **State:** Discovery check PASS on deployed `152fb36b`; S2.5 corrected to FAIL (missing saved attribution), D1 blocked. Option 1 selected: retain attribution unconfirmed across disconnect for preservation. Fix and dated ADR 0085 update are uncommitted; real main-path regression and reversion proofs recorded in W-0021. Recovery redesign remains parked.
- **Next:** local gate PASS and PocketFFT RPM built; deploy needs operator sudo. Refresh both windows, discard the old `7Q7CT` browser record, repeat S2 with a fresh record, then RS8/RS11. Evidence lives in W-0021. Post-deploy: narrow header/Map panel and "Reading when saved" observation. **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
