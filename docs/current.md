# Current work

Updated: 2026-09-05

- **Goal:** Complete Gate A for the frozen `v2.0.0-alpha.2` dogfood candidate, then stop for the operator's ready-to-deploy decision. [`backlog`](backlog.md) owns priority.
- **State:** F-04 confirm-by-push complete; `task ci:local` green at `b8e0356f`; PocketFFT RPM built once, SHA-256-protected. Gate A ratified through A4-01: identity, `rpm -V`, the A1-04 live-config preflight (clean), the local A3 archive and its restore-check are evidenced in the acceptance record, which owns the cases and resume contract. Live alpha.1 daemon inactive, untouched.
- **Next:** Continue Gate A per the record's resume contract: `rpmrebuild` the installed alpha.1 (operator sudo); copy the A3 archive to the trusted SMC LAN host and verify its SHA; provision the Fedora 44 `systemd-nspawn` rootfs (`core` + python3/jq, `.nspawn` `VirtualEthernet=no`); run A2, then the PKG-10a/10b rollback rehearsals, then A4-01. Stop before installing on the live station; B1 and every hardware/rig-command/RF group need later operator decisions. FT8-10 stays BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`dogfood gate`](dogfood-acceptance.md), [`install guide`](install.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`ADR 0078`](decisions/0078-ambiguous-write-outcome-policy.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
