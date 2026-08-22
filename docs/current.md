# Current work

Updated: 2026-08-22

- **Goal:** Implement W-0001 (durable operator notification history) under [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md) — the notification-first pilot of ADR 0061's local `operator_event` store.
- **State:** Policy settled (ADR 0076; ADR 0061 partially superseded for the local-store shape + pilot order). **`operator_event` schema slice complete** — migration 0008 (categorised; closed category/kind/severity CHECKs; NOT-NULL no-default `build`; JSON `detail`; immutable-but-prunable), verified core table, sqlboiler model regenerated, TDD proofs green (incl. `-race` + mutation proof). Nothing pushed.
- **Next:** Persistence plus **atomic last-500-per-category insert/prune, oldest-first, outside every QSO transaction**. The producing-boundary writers follow (browser `export.adif_failed`, daemon terminal `forward.failed`).
- **Decisions not to revisit:** ADR 0076 settles the pilot policy (two durable kinds, typed bounded metadata, daemon-stamped severity/time/build, no ack); `docs/backlog.md` alone owns priority; recording is per-boundary, not hub- or toast-driven.
- **Do not:** persist `Reason` or any raw provider text; add a generic hub subscriber or toast-level recorder; add acknowledgement (`acknowledged_at`/mark-read/unread); stage `.idea/sqldialects.xml`; re-rank priority here; initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0001`](work/W-0001-durable-notifications.md), [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md), [`ADR 0061`](decisions/0061-consolidated-operator-event-log.md), [`backlog`](backlog.md).
- **Coordination:** Leave committing and pushing to the operator; every non-Markdown commit draws a codex clean-room review to triage.
