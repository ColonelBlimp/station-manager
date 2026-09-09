# Current work

Updated: 2026-09-09

- **Goal:** Close alpha.2 dogfood acceptance (B2 surface inventory, then the operator's "Dogfood accepted" ruling) and ship the next candidate carrying CC-5 and CC-6 with its own clean-install evidence. [`backlog`](backlog.md) owns priority.
- **State:** the station runs dev builds past the frozen alpha.2 since 2026-09-07 (deploy-and-test loop per W-0012 fix: commit, `task deploy:local:dev`, operator verifies, record entry in a separate docs commit); now `2.0.0-alpha.2-23-ga364c21a`. Seventeen findings recorded; Findings #3, #4, #8, #12, #16 closed on the station (record entries 27–31), CC-5 and CC-6 shipped for the next candidate. B2 rows walked from now on belong to the next candidate's record.
- **Next:** W-0012 Finding #11 (the Rig Control card opened from Phone/CW or FT8 sets frequency and mode for that context) needs the operator's mode ruling for each context before any change; one untriaged inbox note (an `smd` start failure is not visible until the SPA fails to load) needs a routing ruling. Then the next candidate freeze with its own clean-install evidence (LOG-10b pending a two-logbook instance; the B1-01 waiver retires there). W-0018 runs page by page alongside; Finding 7 sits in W-0011 until passively reproduced. Hardware/rig-command/RF remain unauthorized; FT8-10 stays BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`dogfood gate`](dogfood-acceptance.md), [`install guide`](install.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`W-0018`](work/W-0018-bring-the-embedded-manual-to-release-readiness.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
