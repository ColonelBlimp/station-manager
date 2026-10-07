# Current work

Updated: 2026-10-07

- **Goal:** W-0021: QSO archives — finish the foundations: C.2, then 5C (config v6), then 5F (SM Cloud identity). [`backlog`](backlog.md) owns priority.
- **State:** 5C deployed and stripped on Home (`52077508`…`2e4e49eb`); C.2 accepted on screen 2026-10-06. 5F.0 deployed (`d8edef3b`); 5F.1 deployed on smcloud; 5F.2 server identity wire built (`840a4cb5`…`2e16f02b`, codex-clean), not deployed. Keep the pre-schema-7 dump.
- **Next:** deploy smcloud 5F.2 (schema 8, `pg_dump` first), then 5F.3 (client: probe, Home adoption, ambiguity rule). **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
