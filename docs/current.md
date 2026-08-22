# Current work

Updated: 2026-08-22

- **Goal:** Implement W-0001 (durable operator notification history) under [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md) — the notification-first pilot of ADR 0061's local `operator_event` store.
- **State:** Policy settled (ADR 0076). Schema (migration 0008) + **persistence complete**: `RecordOperatorEvent` does a store-owned insert plus atomic last-500-per-category oldest-first prune in one tx, takes no caller `*sql.Tx`, and stays outside every QSO transaction. Proven: 501st insert leaves events 2–501; a forced prune failure rolls both back (ring intact at 500) while a separate QSO survives. Green incl. `-race`. Nothing pushed.
- **Next:** Newest-N-per-category **retrieval** read-path, then the producing-boundary writers. The producer slice must prove typed/bounded **hostile-input rejection** — the storage writer validates JSON syntax only.
- **Decisions not to revisit:** ADR 0076 settles the pilot policy (two durable kinds, typed bounded metadata, daemon-stamped severity/time/build, no ack); `docs/backlog.md` alone owns priority; recording is per-boundary, not hub- or toast-driven.
- **Do not:** persist `Reason` or any raw provider text; add a generic hub subscriber or toast-level recorder; add acknowledgement (`acknowledged_at`/mark-read/unread); stage `.idea/sqldialects.xml`; re-rank priority here; initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0001`](work/W-0001-durable-notifications.md), [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md), [`ADR 0061`](decisions/0061-consolidated-operator-event-log.md), [`backlog`](backlog.md).
- **Coordination:** Leave committing and pushing to the operator; every non-Markdown commit draws a codex clean-room review to triage.
