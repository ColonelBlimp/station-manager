# Current work

Updated: 2026-11-25

- **Goal:** build ADR 0102 in reviewed slices ([`W-0027`](work/W-0027-electron-desktop-host.md)); [`backlog`](backlog.md) owns priority.
- **State:** ADRs [`0102`](decisions/0102-electron-desktop-host-for-the-ui.md) and 0100 Accepted; 0103 and 0101 **Proposed**. W-0027: A1, C1c, C1d, C2 (operating-client check, at the station), C4 (presence) and D1 (desktop RPM) accepted. A3 (closing) built. W-0028 (P1) built, station test accepted. W-0029 parts 1 to 4 built; part 5 built, not connected.
- **Open:** closing W-0028; W-0024 O-a, O-b; backup/restore (item 4); deferred live U5 and keyed U6.
- **Next:** by [`W-0029`](work/W-0029-operating-sessions.md)'s brief, part 6; station: alpha.5.332 (`e40a710d`) with A3, W-0030, W-0031. A2 waits on W-0029. Migrations go up only.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account. Docking evaluated and stopped (W-0026).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; dispose of retained resources without a reviewed inventory; amend or push without operator direction.
- **Relevant files:** [`W-0027`](work/W-0027-electron-desktop-host.md), [`ADR 0102`](decisions/0102-electron-desktop-host-for-the-ui.md), [`ADR 0100`](decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review; agent reviews go through `.scratch/`.
