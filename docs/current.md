# Current work

Updated: 2026-10-02

- **Goal:** W-0021: QSO archives; saved-QSO recovery per [ADR 0085](decisions/0085-preserve-qso-drafts-with-their-original-archive.md) and [ADR 0086](decisions/0086-open-saved-qsos-from-a-header-indicator.md). [`backlog`](backlog.md) owns priority.
- **State:** Restore commit 1 and attribution fixes committed. Through `dd8f0d97`: pushed, CI passed (operator report). Context refresh: `ea6f2ec5`. Commit 2 claim/form internals: `5ebb09ca`; operator and Codex reviews found no blockers, full local gate and 18 reversion proofs passed. Restore remains unavailable until commit 4.
- **Next:** Restore commit 3 edits/Clear/owning-tab Discard and rebind flush, then commit 4 logging/public entry. Acceptance, review follow-ups and evidence live in W-0021; RS8/RS11 browser ownership/closure remain pending the operator's two-window drill. Post-deploy: narrow header/Map panel and "Reading when saved" observation. Other follow-ups route through the inbox/backlog. **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
