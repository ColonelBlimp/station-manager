# Current work

Updated: 2026-11-06

- **Goal:** close the 2026-11-06 inbox items. [`backlog`](backlog.md) owns priority.
- **State:** W-0024 **ACCEPTED** on `b2a16f5a`, which the station runs. Inbox triage recorded. ADR 0100 (daemon-owned operating sessions) **Proposed**: U1 (b), U3 (b), default gap 2 h ruled; O1–O8 open.
- **Open:** item 1 comparison policy approved by review; implementation awaits operator authorisation. O-a, O-b; backup/restore (item 4, with O7); Map (item 5); deferred live U5 and keyed U6.
- **Next:** session-email mark fix (item 1), once authorised; then the W-0023 follow-on ADR with daemon-owned contest state.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account. Shutdown cancels an in-flight stream write; 1001 is best effort.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; dispose of retained resources without a reviewed inventory; amend or push without operator direction.
- **Relevant files:** [`ADR 0100`](decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md), [`inbox`](dogfood-inbox.md), [`W-0023`](work/W-0023-contest-logging.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
