# Current work

Updated: 2026-09-11

- **Goal:** W-0019: FT4 on the station for the Africa FT4 DX Contest (Sat 2026-09-12 15:00–18:00Z); then close alpha.2 dogfood acceptance and ship the next candidate. [`backlog`](backlog.md) owns priority.
- **State:** W-0019 slices 1–4 shipped (`57853f94`…`8701a6de`): FT4 profile, decoder core, claim + wire, third Operate item, MFSK/FT4 logging, mode catalogue corrected. Station still runs `2.0.0-alpha.2-29-g17fc6a7e` (dev builds since 2026-09-07). Findings #3, #4, #8, #12, #16, #18 closed (record 27–33).
- **Next:** push, read CI; `task deploy:local:dev`; gates G2 (passive RX on 14.080), G3 (keyed test, per-occasion agreement), G4 (first QSO) — operator-controlled, a record entry each. W-0012 Finding #11 awaits the mode ruling; inbox: `smd` start-failure visibility, Excel export. Then the next candidate freeze (LOG-10b; B1-01 waiver retires). W-0018 alongside; Finding 7 in W-0011 until reproduced. RF unauthorized; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private. FT8 timing stays +0.500/+0.660 during W-0019.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0019`](work/W-0019-ft4-for-the-africa-ft4-dx-contest.md), [`ADR 0080`](decisions/0080-ft4-as-a-profile-of-the-ft8-subsystem.md), [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`dogfood gate`](dogfood-acceptance.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`W-0018`](work/W-0018-bring-the-embedded-manual-to-release-readiness.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review to triage.
