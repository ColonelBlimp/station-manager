# Current work

Updated: 2026-10-13

- **Goal:** W-0021: QSO archives — finish the foundations through 5F (SM Cloud identity). [`backlog`](backlog.md) owns priority.
- **State:** 5F.4 implemented (`73a992b0`…`ea7b189e`), review cleared, CI green; operator acceptance pending. 5F.3 deployed 2026-10-09; Home adopted 2026-10-10. Keep the pre-schema-7/8 smcloud dumps until the identity wire is proven. **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Open:** the 5F.4 station drill (needs the operator's go-ahead that day); the identity wire's direct proof (the next on-air QSO).
- **Next:** W-0022, the HTTP listener double close at shutdown, committed (`c1b9fbbd`, ADR 0093); it closes once CI is green. W-0017 A closed (`544fb62a`).
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`W-0022`](work/W-0022-http-shutdown-double-close.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
