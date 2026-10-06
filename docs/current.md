# Current work

Updated: 2026-10-05

- **Goal:** W-0021: QSO archives; switching per [ADR 0087](decisions/0087-switch-archives-only-from-settings-and-state-the-loss.md) (supersedes 0085 / 0086). [`backlog`](backlog.md) owns priority.
- **State:** ADR 0087 removal shipped (`55178b58`, `c31c0d7e`; CI green through `245b7dde`); operator acceptance PASS 2026-10-05.
- **Next:** dogfood-inbox triage (proposal made 2026-10-05, deferred to the next session; rulings pending). **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
