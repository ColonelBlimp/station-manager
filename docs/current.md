# Current work

Updated: 2026-10-16

- **Goal:** W-0021: QSO archives — finish the foundations through 5F (SM Cloud identity). [`backlog`](backlog.md) owns priority.
- **State:** implemented and CI-green: 5F.4 (`73a992b0`…`ea7b189e`) and 5F.5, restore by UUID (`6aa99773`); W-0017 C fixed (`20b879e3`). 5F.3 deployed 2026-10-09; Home adopted 2026-10-10. **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Open (operator acceptance):** the 5F.4 station drill (go-ahead that day); the identity wire's proof on the next on-air QSO, keeping the pre-schema-7/8 smcloud dumps until then; a restore rehearsal; alpha.3 acceptance. 5F stays open against AC 6 until its evidence is in.
- **Next:** whole-archive provisioning (5F Q7), design for review, no code.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
