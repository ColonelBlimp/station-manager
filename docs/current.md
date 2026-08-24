# Current work

Updated: 2026-08-24

- **Goal:** Implement W-0001 (durable operator notification history) under [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md) — the notification-first pilot of ADR 0061's local `operator_event` store.
- **State:** W-0001 implementation complete; producing boundaries, durable store, retrieval endpoint, reachable rail, and reload/fresh-mount proofs are done. Pushed & CI-green baseline `06326bdb` (schema, persistence, retrieval, producers A+B); the retrieval-endpoint + rail closure slice is LOCAL. Lesson: a schema-head bump also breaks `handler_version_test` — run whole-tree tests.
- **Next:** Commit, clean-room review, and CI; then closure sign-off.
- **Decisions not to revisit:** ADR 0076 settles the pilot policy (two durable kinds, typed bounded metadata, daemon-stamped severity/time/build, no ack); `docs/backlog.md` alone owns priority; recording is per-boundary, not hub- or toast-driven.
- **Do not:** archive W-0001 or drop it from `docs/backlog.md` before closure sign-off (only after this commit passes review + CI); persist `Reason` or any raw provider text; add a generic hub subscriber or toast-level recorder; add acknowledgement (`acknowledged_at`/mark-read/unread); stage `.idea/sqldialects.xml`; re-rank priority here; initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0001`](work/W-0001-durable-notifications.md), [`ADR 0076`](decisions/0076-notification-history-pilot-operator-event-store.md), [`ADR 0061`](decisions/0061-consolidated-operator-event-log.md), [`backlog`](backlog.md).
- **Coordination:** Leave committing and pushing to the operator; every non-Markdown commit draws a codex clean-room review to triage.
