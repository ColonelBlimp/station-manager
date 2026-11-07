# Current work

Updated: 2026-11-07

- **Goal:** close the 2026-11-06 inbox items. [`backlog`](backlog.md) owns priority.
- **State:** W-0024 **ACCEPTED**. Session-email mark fix (item 1, `c3826281`) built and deployed; CI green (operator report). ADRs 0100 and 0101 **Proposed**, design only. 0100: activity plus deliberate actions, default gap 2 h, contest suspension ruled. 0101 U1–U3 recorded in `8c8c8cf9`. Map design in 0100 §7 ruled (operator).
- **Open:** ADR 0100 O1–O8 and counted actions; O-a, O-b; backup/restore (item 4, with O7); deferred live U5 and keyed U6.
- **Next:** resolve ADR 0100's counted actions, overdue expiry at contest end, and open questions. Backlog owns priority.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account. Shutdown cancels an in-flight stream write; 1001 is best effort.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; dispose of retained resources without a reviewed inventory; amend or push without operator direction.
- **Relevant files:** [`ADR 0100`](decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md), [`ADR 0101`](decisions/0101-contest-occurrence-records-slice-serials-and-daemon-held-contest-mode.md), [`inbox`](dogfood-inbox.md), [`W-0023`](work/W-0023-contest-logging.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
