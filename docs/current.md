# Current work

Updated: 2026-10-06

- **Goal:** W-0021: QSO archives — finish the foundations: C.2, then 5C (config v6), then 5F (SM Cloud identity). [`backlog`](backlog.md) owns priority.
- **State:** 5C built (`52077508`…`2f6a3677`); gates, cloud tests, `ci:local`, drill pass; awaiting deploy. C.2 (`b02354e0`) still awaits the on-screen placeholder check.
- **Next:** deploy — a v6 start strips config.json only with Home active, its seed committed and config.v5.json written (else it waits, logged) — then 5F. **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
