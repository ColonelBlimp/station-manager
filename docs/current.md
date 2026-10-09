# Current work

Updated: 2026-10-09

- **Goal:** W-0021: QSO archives — finish the foundations: C.2, then 5C (config v6), then 5F (SM Cloud identity). [`backlog`](backlog.md) owns priority.
- **State:** 5C deployed and stripped on Home (`52077508`…`2e4e49eb`); C.2 accepted on screen 2026-10-06. 5F.0 deployed (`d8edef3b`); 5F.1 and 5F.2 deployed on smcloud (schema 8, `identity_protocol: 1`); keep the pre-schema-7/8 dumps until adoption is checked.
- **Next:** 5F.3 commits 1–3 pushed, CI-green (`681cd208`…`a3037540`); 4a (durable name reservation, ADR 0091) `5b9ccb2e`; 4b1 (account fingerprint, migration 0017) `7fc69365`; 4b2 (adopter) `45c8c126` + test fix `7bbd5cf5`; 4b3 (SPA status line) `53a4c45e` + B3 polling `87804ec6`; 5a (identity wire at start, held uploads, T7) `14e9b94a`, unpushed; next 5b (SPA held line). No daemon deployment until 5F.3 is built and reviewed. **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
