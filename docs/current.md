# Current work

Updated: 2026-11-09

- **Goal:** choose the UI host ([`W-0026`](work/W-0026-ui-host-evaluation.md)). [`backlog`](backlog.md) owns priority.
- **State:** W-0026 Electron POC (separate repo) **complete**; results and 10 open decisions in its final summary (`57b48e8f`). ADRs 0100 and 0101 **Proposed**, design only, review fixes and S1/S4 ruled (`e5924514`); 0100 O1–O3 deferred.
- **Open:** W-0026 decisions (host first); W-0024 O-a, O-b; backup/restore (item 4); deferred live U5 and keyed U6.
- **Next:** operator rulings on W-0026, then a UI-host ADR. Backlog owns priority.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account. Docking evaluated and stopped (W-0026).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; dispose of retained resources without a reviewed inventory; amend or push without operator direction.
- **Relevant files:** [`W-0026`](work/W-0026-ui-host-evaluation.md), [`ADR 0100`](decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md), [`ADR 0101`](decisions/0101-contest-occurrence-records-slice-serials-and-daemon-held-contest-mode.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review; agent reviews go through `.scratch/`.
