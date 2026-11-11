# Current work

Updated: 2026-11-11

- **Goal:** build ADR 0102 in reviewed slices ([`W-0027`](work/W-0027-electron-desktop-host.md)); [`backlog`](backlog.md) owns priority.
- **State:** [`ADR 0102`](decisions/0102-electron-desktop-host-for-the-ui.md) Accepted; W-0027 A1, C1c (smd's pinned HTTPS app listener) and C1d (the desktop app opens the real smd) **accepted** 2026-11-11, live on the station. ADRs 0103, 0100 and 0101 **Proposed**.
- **Open:** W-0024 O-a, O-b; backup/restore (item 4); deferred live U5 and keyed U6.
- **Next:** W-0027 C2 (operating client) or A2 (window set), on the operator's direction.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account. Docking evaluated and stopped (W-0026).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; dispose of retained resources without a reviewed inventory; amend or push without operator direction.
- **Relevant files:** [`W-0027`](work/W-0027-electron-desktop-host.md), [`ADR 0102`](decisions/0102-electron-desktop-host-for-the-ui.md), [`W-0026`](work/W-0026-ui-host-evaluation.md), [`ADR 0100`](decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md), [`ADR 0101`](decisions/0101-contest-occurrence-records-slice-serials-and-daemon-held-contest-mode.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review; agent reviews go through `.scratch/`.
