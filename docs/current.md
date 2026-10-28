# Current work

Updated: 2026-10-28

- **Goal:** W-0021: QSO archives — finish the foundations through 5F (SM Cloud identity). [`backlog`](backlog.md) owns priority.
- **State:** **v2.0.0-alpha.5** (`0da2cd6a`, RPM `3c10b6e1…`) is **dogfood-accepted** (2026-10-28) and runs on the live station; alpha.4 held, alpha.3 superseded. It fixed alpha.4's Session-isolation and false-success-export blockers: archive request guard, FT8 stream identity, per-archive Session stores, exact export/email accounting. Acceptance is not FT8-10/W-0002 closure or RF acceptance. Record: [`alpha.5`](reports/dogfood-acceptance-v2.0.0-alpha.5.md).
- **Open:** alpha.5 Findings #1 (W-0018) and #2–#4 (W-0012), all non-blocking. After an upgrade, reload every open tab (Finding #4). Guest `sm-alpha4-clean` to dispose.
- **Next:** Settings → Logbooks.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
