# Current work

Updated: 2026-09-05

- **Goal:** Install the frozen `v2.0.0-alpha.2` candidate on the dogfood station after the operator's fresh local backup, then smoke-check it. [`backlog`](backlog.md) owns priority.
- **State:** `task ci:local` green at `b8e0356f`; frozen PocketFFT RPM SHA-verified. Gate A closed 2026-09-05: evidenced rows PASS; nspawn and rollback-rehearsal rows NOT EXECUTED by operator scope decision (rebuild plus restore/import recovery accepted). Operator holds a fresh stopped-daemon copy of the working directory. Live alpha.1 inactive, untouched; stopped nspawn image awaits operator-sudo deletion.
- **Next:** Operator rules Ready to deploy; remove the retired `fedora44` image/override. Verify the frozen RPM SHA (do not rebuild from docs-only HEAD), install it live, daemon-reload/start, then check version, migration, QSO/queue counts and basic UI. Hardware/rig-command/RF remain unauthorized; FT8-10 stays BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`dogfood gate`](dogfood-acceptance.md), [`install guide`](install.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`ADR 0078`](decisions/0078-ambiguous-write-outcome-policy.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
