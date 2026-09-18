# Current work

Updated: 2026-09-18

- **Goal:** close W-0019 after the Africa FT4 DX Contest, clear the FT4 findings, then close alpha.2 dogfood acceptance and ship the next candidate. [`backlog`](backlog.md) owns priority.
- **State:** station on `2.0.0-alpha.2-106-gdfd0898f` (deployed 2026-09-16; later commits not deployed). 2026-09-15 soak: 309 QSOs, 0 alarms → **W-0011 TX-alarm item CLOSED**, entry 39. **W-0020 Station Events SELECTED**. Inbox rulings of 2026-09-16 all built: ALC label, bag tooltip, W-0011 warn line, liveness warn-once, Occupancy age, Settings repeat cap.
- **Next:** operator's on-screen tune check. W-0020 slices 1–3 (store, recorder, producing boundaries) built 2026-09-18; slice 4 (API) on direction. Open rulings: repeat-hold memory; landing view. Then G3/AC7 (per-occasion) or waive; start-failure visibility; Excel export; next freeze. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private. FT8 timing stays +0.500/+0.660 during W-0019. `txConfirmTimeout` change DEFERRED (on-air approval).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0011`](work/W-0011-ft8-and-rig-refinements.md), [`W-0019`](work/W-0019-ft4-for-the-africa-ft4-dx-contest.md), [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`W-0020`](work/W-0020-station-events.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review to triage.
