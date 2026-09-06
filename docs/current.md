# Current work

Updated: 2026-09-06

- **Goal:** Close alpha.2 dogfood acceptance (B2 surface inventory, then the operator's "Dogfood accepted" ruling) and ship the next candidate carrying CC-5 and CC-6 with its own clean-install evidence. [`backlog`](backlog.md) owns priority.
- **State:** alpha.2 is live on the dogfood station (both B1-01 passes; B1-01 WAIVED for Finding 6). Fresh deployment A1-02/A2-01–A2-07 plus guide §5/§6 executed on the host 2026-09-06 and the station restored from a verified copy. Seventeen findings recorded; CC-5 and CC-6 shipped for the next candidate.
- **Next:** Rulings applied 2026-09-06 (clean-install and B1 rows PASS; B1-01 WAIVED; LOG-10b pending the next fresh deployment); Findings 8–17 routed (W-0012, W-0018 new for the manual, W-0009). Operator walks the B2 inventory; then the W-0012 UI fixes and the next candidate. W-0018 runs page by page alongside; Finding 7 sits in W-0011 until passively reproduced. Hardware/rig-command/RF remain unauthorized; FT8-10 stays BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`dogfood gate`](dogfood-acceptance.md), [`install guide`](install.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`W-0018`](work/W-0018-bring-the-embedded-manual-to-release-readiness.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
