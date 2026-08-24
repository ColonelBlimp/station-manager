# Current work

Updated: 2026-08-24

- **Goal:** Implement W-0001 (durable operator notification history) under [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md) — the notification-first pilot of ADR 0061's local `operator_event` store.
- **State:** Policy settled (ADR 0076). Schema+persistence+retrieval pushed & CI-green (origin/main=`ca83a339`). Producer slice **A+B done, LOCAL**: **A** = best-effort `forward.failed` in `markFailed` (never Reason); **B** = strict `POST /v1/notifications` (`{count,outcome}` only, hostile input rejected, rest stamped server-side) + `ExportDialog` posting on failure. Lesson: a schema-head bump also breaks `handler_version_test` — run whole-tree tests.
- **Next:** Retrieval/history **UI** (a rail reading newest-N `notification` events) + the **reload-level end-to-end proof** (toast expiry + reload, both kinds) before closure. Use a reachable outcome for the browser end-to-end — `network` can't be durable when the daemon is unreachable.
- **Decisions not to revisit:** ADR 0076 settles the pilot policy (two durable kinds, typed bounded metadata, daemon-stamped severity/time/build, no ack); `docs/backlog.md` alone owns priority; recording is per-boundary, not hub- or toast-driven.
- **Do not:** persist `Reason` or any raw provider text; add a generic hub subscriber or toast-level recorder; add acknowledgement (`acknowledged_at`/mark-read/unread); stage `.idea/sqldialects.xml`; re-rank priority here; initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0001`](work/W-0001-durable-notifications.md), [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md), [`ADR 0061`](decisions/0061-consolidated-operator-event-log.md), [`backlog`](backlog.md).
- **Coordination:** Leave committing and pushing to the operator; every non-Markdown commit draws a codex clean-room review to triage.
