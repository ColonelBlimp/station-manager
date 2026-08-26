# Current work

Updated: 2026-08-26

- **Goal:** No workstream selected. W-0005 (operator-triggered forwarder queue clearing) closed 2026-08-26 and archived to [`archive/work/`](archive/work/W-0005-operator-triggered-forwarder-queue-clearing.md); W-0001 (durable notifications) closed earlier. Pick the next item from [`docs/backlog.md`](backlog.md); the operator owns priority.
- **State:** W-0005 shipped and CI-green on `main` (`60f3a67c`): pending+failed clear + per-forwarder counts, the two `/v1/forwarder`-queue endpoints, and the Settings → Forwarding "N queued · M in flight" + confirmed Clear-queue action with reconcile-after-clear safety (an indeterminate outcome re-reads, never leaving a stale re-fireable action).
- **Next:** Operator selects the next workstream. Backlog top is W-0002 (FT8 type-4 on-air validation — operator-initiated RF), then W-0003/W-0004. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) stays open: evidence `SQLITE_BUSY` fixed, but **bridge sub-item A (streaming-startup barrier) is unfixed and latent** — fix on next recurrence or when touching `internal/bridge`.
- **Decisions not to revisit:** W-0005 policy — clear `pending`+`failed` only (leave `in_progress`); clearing is separate from enable/disable; no `qso_history` audit.
- **Do not:** re-open a closed dossier (W-0001, W-0005) without a fresh one; raise a CI flake's timeout instead of making it deterministic (W-0017); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md), [`W-0005` archived](archive/work/W-0005-operator-triggered-forwarder-queue-clearing.md).
- **Coordination:** Leave committing and pushing to the operator; every non-Markdown commit draws a codex clean-room review to triage.
