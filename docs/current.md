# Current work

Updated: 2026-09-05

- **Goal:** Install the frozen `v2.0.0-alpha.2` candidate on the dogfood station after the operator's fresh local backup, then smoke-check it. [`backlog`](backlog.md) owns priority.
- **State:** alpha.2 is live on the dogfood station after both B1-01 passes (stopped-daemon, then running-daemon swap); Gate A closed 2026-09-05 under the reduced scope. Seven findings; #1 (SM Cloud cleartext ack) and #6 (alpha.1-normalised qrzcq `action_filter` refused at start) each needed a live config edit, #6 is upgrade-blocking. Stopped nspawn image awaits operator-sudo deletion.
- **Next:** Operator rules the B1/A5-03 rows and triages Findings 1–7 (record + dogfood inbox); B2 surface inventory in Firefox per the record; Finding 6 must be fixed in the next candidate or explicitly waived before dogfood acceptance. Backlog candidates: load-time `action_filter` reconciliation, `config-check` running full Validate, false TX alarm at bridge open, SM Cloud https on the LAN. Hardware/rig-command/RF remain unauthorized; FT8-10 stays BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`dogfood gate`](dogfood-acceptance.md), [`install guide`](install.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`ADR 0078`](decisions/0078-ambiguous-write-outcome-policy.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
