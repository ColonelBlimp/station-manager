# Current work

Updated: 2026-10-07

- **Goal:** W-0021: QSO archives — finish the foundations: C.2, then 5C (config v6), then 5F (SM Cloud identity). [`backlog`](backlog.md) owns priority.
- **State:** 5C deployed and stripped on Home (`52077508`…`2e4e49eb`); C.2 accepted on screen 2026-10-06. 5F package ruled 2026-10-07 (ADR 0088); 5F.0 built (`4daf2a2e`…`d8edef3b`, codex-clean), not deployed.
- **Next:** deploy and check 5F.0 on screen, then 5F.1 (server schema). **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
