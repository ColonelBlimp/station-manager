# Current work

Updated: 2026-09-10

- **Goal:** W-0019: FT4 on the station for the Africa FT4 DX Contest (Sat 2026-09-12 15:00–18:00Z); then close alpha.2 dogfood acceptance and ship the next candidate. [`backlog`](backlog.md) owns priority.
- **State:** the station runs dev builds past the frozen alpha.2 since 2026-09-07 (deploy-and-verify loop per fix); now `2.0.0-alpha.2-29-g17fc6a7e`. Findings #3, #4, #8, #12, #16, #18 closed (record entries 27–33). ADR 0080: FT4 is a profile of `internal/ft8` plus a third Operate item; go-ft8 v0.9.0 has the decoder.
- **Next:** W-0019 slices in order (bump, profile+timing, decoder adapter, claim+wire, logging/SPA); gates G2–G4 are operator-controlled, no keyed test without per-occasion agreement. W-0012 Finding #11 awaits the mode ruling; the inbox `smd` start-failure note needs routing. Then the next candidate freeze (LOG-10b; B1-01 waiver retires). W-0018 alongside; Finding 7 in W-0011 until reproduced. RF unauthorized; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0019`](work/W-0019-ft4-for-the-africa-ft4-dx-contest.md), [`ADR 0080`](decisions/0080-ft4-as-a-profile-of-the-ft8-subsystem.md), [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`dogfood gate`](dogfood-acceptance.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`W-0018`](work/W-0018-bring-the-embedded-manual-to-release-readiness.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review to triage.
