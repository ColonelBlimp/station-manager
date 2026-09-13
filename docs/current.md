# Current work

Updated: 2026-09-13

- **Goal:** close W-0019 after the Africa FT4 DX Contest, clear the operator-facing FT4 findings, then close alpha.2 dogfood acceptance and ship the next candidate. [`backlog`](backlog.md) owns priority.
- **State:** station on `2.0.0-alpha.2-65-gba2abec7`. Contest worked 2026-09-12 15:00–17:14Z: 51 QSOs (40 on 20 m, 11 on 40 m), 458 rungs, every upload first attempt — record entry 37. G2/G4 met; AC7/G3 waived until the post-contest measurements. Built, NOT deployed (one deploy): TX-alarm re-send + ms stamps; FT Mode readout; filter chip; QRZ key redaction; draft age; enrich cap 2; repeat hold (W-0011/12/19).
- **Next:** one deploy (`task deploy:local:dev`) before the operator's FT8/FT4 session, then watch the still-keyed events, the Mode readout and any repeat hold on air. Operator: SARL log due Thu 2026-09-17 21:59Z. Then G3 dummy-load + AC7 window (per-occasion); inbox: map on the shell stream, start-failure visibility, Excel export. Then the next candidate freeze. RF only by per-occasion agreement; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private. FT8 timing stays +0.500/+0.660 during W-0019.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0011`](work/W-0011-ft8-and-rig-refinements.md), [`W-0019`](work/W-0019-ft4-for-the-africa-ft4-dx-contest.md), [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review to triage.
