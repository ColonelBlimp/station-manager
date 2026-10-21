# Current work

Updated: 2026-10-21

- **Goal:** W-0021: QSO archives — finish the foundations through 5F (SM Cloud identity). [`backlog`](backlog.md) owns priority.
- **State:** implemented and CI-green: 5F.4 (`73a992b0`…`ea7b189e`) and 5F.5, restore by UUID (`6aa99773`); W-0017 C fixed (`20b879e3`). 5F.3 deployed 2026-10-09; Home adopted 2026-10-10. 5F Q7 provisioning (ADR 0071 refinement) implemented, reviewed, and CI-green through `bb796a11` (run 39374098343, includes `a37a50c0`). SMC and SMD deployed 2026-10-21. The same day, drills A, B and C (scratch restore and provisioning) passed and **5F's AC 6 closed** (Home baseline 8,130 QSOs; dump retention satisfied, disposal the operator's). **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Open:** the replacement candidate's release gates under alpha.3 Option 1 (alpha.3 superseded).
- **Next:** cut the replacement candidate; refresh the backup before the live upgrade. Settings → Logbooks follows.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
