# Current work

Updated: 2026-11-06

- **Goal:** next in the data-model chain: W-0023 follow-on ADR (contest logging, design only). [`backlog`](backlog.md) owns priority.
- **State:** W-0024 (Settings → Logbooks) **ACCEPTED** 2026-11-06 on `b2a16f5a`, which the station runs (ADR 0099 W1 WebSockets; schema 19). U1, U5, keyed U6 waived on tests (entries 31–33).
- **Open:** deferred live U5 and keyed U6 exercises; follow-ups O-a, O-b (acceptance entry 30); inbox items from 2026-11-06 (FT8 repeat hold after Ignore, session-email mark race, Map vs Session, backup/restore path). ADR 0099 still Proposed; W-0012 slice 3 = ADR 0098 (Proposed), on hold.
- **Next:** inbox triage; W-0023 follow-on ADR (design only).
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account. Shutdown cancels an in-flight stream write; 1001 is best effort.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0024 acceptance`](reports/acceptance-W-0024-set-as-default.md), [`W-0023`](work/W-0023-contest-logging.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
