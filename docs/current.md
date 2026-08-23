# Current work

Updated: 2026-08-22

- **Goal:** Implement W-0001 (durable operator notification history) under [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md) — the notification-first pilot of ADR 0061's local `operator_event` store.
- **State:** Policy settled (ADR 0076). Schema (0008) + persistence + **retrieval complete**: `RecordOperatorEvent` (store-owned insert + atomic last-500-per-category oldest-first prune, no caller `*sql.Tx`, outside every QSO tx) and `FetchOperatorEventsByCategoryWithContext` (newest-first, limit ∈ [1,500] or op-tagged error, read-only). All TDD-proven, green incl. `-race`. Nothing pushed.
- **Next:** The producing-boundary writers — browser `export.adif_failed`, daemon terminal `forward.failed`. That slice MUST prove typed/bounded **hostile-input rejection**; the storage writer validates JSON syntax only.
- **Decisions not to revisit:** ADR 0076 settles the pilot policy (two durable kinds, typed bounded metadata, daemon-stamped severity/time/build, no ack); `docs/backlog.md` alone owns priority; recording is per-boundary, not hub- or toast-driven.
- **Do not:** persist `Reason` or any raw provider text; add a generic hub subscriber or toast-level recorder; add acknowledgement (`acknowledged_at`/mark-read/unread); stage `.idea/sqldialects.xml`; re-rank priority here; initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0001`](work/W-0001-durable-notifications.md), [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md), [`ADR 0061`](decisions/0061-consolidated-operator-event-log.md), [`backlog`](backlog.md).
- **Coordination:** Leave committing and pushing to the operator; every non-Markdown commit draws a codex clean-room review to triage.
