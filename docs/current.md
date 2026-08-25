# Current work

Updated: 2026-08-25

- **Goal:** No workstream selected. W-0001 (durable operator notification history) closed 2026-08-25 and archived to [`archive/work/`](archive/work/W-0001-durable-notifications.md). Pick the next item from [`docs/backlog.md`](backlog.md); the operator owns priority.
- **State:** W-0001 shipped and CI-green on `main` (`e7237b3a`): both durable kinds, the `operator_event` store, `GET /v1/notifications`, and the header notification-history rail, with reload/fresh-mount proofs. Follow-up `dc0f7184` (race-free evidence-flake test) pushed; its CI run is the last confirmation.
- **Next:** Operator selects the next workstream. Backlog top is W-0002 (FT8 type-4 on-air validation — operator-initiated RF), then W-0005 (forwarder queue clearing). [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) stays open: evidence `SQLITE_BUSY` fixed, but **bridge sub-item A (streaming-startup barrier) is unfixed and latent** — fix on next recurrence or when touching `internal/bridge`.
- **Decisions not to revisit:** ADR 0076 settles the notification pilot (two durable kinds, typed bounded metadata, daemon-stamped severity/time/build, no ack); `docs/backlog.md` alone owns priority.
- **Do not:** re-open W-0001 to add acknowledgement/unread or new kinds without a fresh dossier; raise a CI flake's timeout instead of making it deterministic (W-0017); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md), [`W-0001` archived](archive/work/W-0001-durable-notifications.md).
- **Coordination:** Leave committing and pushing to the operator; every non-Markdown commit draws a codex clean-room review to triage.
