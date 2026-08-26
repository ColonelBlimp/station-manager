# W-0005 — Operator-triggered forwarder queue clearing

**Status:** Open — decisions settled and implementation landed (2026-08-26); closure sign-off pending CI-green
**Selected:** 2026-08-20
**Outcome:** An operator can deliberately discard a forwarder's queued (not-yet-uploaded)
backlog — "don't send the backlog, start forwarding from now" — without losing real upload
history, from the consolidated app's Forwarding settings, with a defined policy for an
enabled forwarder whose worker may be mid-batch.

`W-0005` is an immutable identity. Its status may change, while priority and ranked position
live only in [`docs/backlog.md`](../backlog.md).

## Verified current state

The primitive and the disabled-forwarder path already exist; the operator-triggered surface
and the enabled-forwarder policy do not.

- **The discard primitive exists and preserves history.**
  [`DiscardQueuedUploadsForForwarderWithContext`](../../internal/database/sqlite/api_context.go)
  (`internal/database/sqlite/api_context.go:2521`) deletes the named forwarder's `qso_upload`
  rows whose status is `Pending`, `InProgress`, or `Failed`, and only those — `uploaded` /
  `success` rows are never matched, so real upload history survives. Scope is per-forwarder
  (a `forwarder_name` equality), not global, and it returns the number of rows removed.
- **Disabled forwarders are already auto-discarded at startup.** `startWorkers`
  (`cmd/smd/lifecycle_adapters.go`, the "orphan sweep + disabled-forwarder discard" step)
  calls that primitive for each configured-but-disabled forwarder before spawning the enabled
  workers. (The backlog's prior reference to `cmd/smd/main.go:464` is stale: the ADR 0070
  lifecycle refactor moved this into the lifecycle adapters; the behaviour is unchanged.) This
  is the shadow side of ADR 0022's enqueue-by-presence — a disabled forwarder silently
  accumulates `pending` rows, so enabling it later would flush the whole backlog unless it was
  discarded first.
- **The operator-triggered path was the whole of the remaining work.** At selection there was
  no API endpoint or app UI to invoke the discard on demand, and — critically — no coordination
  policy for an **enabled** forwarder: the startup primitive's delete scope includes
  `InProgress`, so a clear issued while an enabled forwarder's worker is mid-push would race and
  could delete a row the worker is actively uploading (the disabled-at-startup path is race-free
  only because the worker for a disabled forwarder is never spawned). That is now built — see
  **Decisions settled** and **Implementation** below; the coordination policy was resolved by
  scoping the operator-triggered clear to `pending`+`failed`, leaving `in_progress` untouched.

## Scope

This work item owns the operator-triggered clear and the policy that makes it safe on a live
forwarder:

- an operator-triggered daemon operation (per-forwarder) that discards `pending`/`failed`
  queued rows and preserves `uploaded`/`success` history, reusing the existing primitive;
- a defined, observable coordination policy for an **enabled** forwarder's in-flight batch so
  the clear cannot delete a row the worker is actively uploading; and
- the app **Settings → Forwarding** surface (queued count + a confirmed "Clear queue" action
  per forwarder), paired with the enable/disable control.

It does **not** own: the already-shipped startup auto-discard of disabled forwarders or the
discard primitive's row-scope contract (both preserved as-is); a global (all-forwarder)
clear; the inverse "bulk-forward an existing log to a newly enabled service" backfill
feature; the ClubLog `putlogs.php` bulk-backfill route (a separate, cross-linked backlog
item — see References); legacy-SPA retirement or app-shell/route work (W-0003); or any change
to how live logging-time enqueue works (ADR 0022).

## Operator-observable acceptance criteria

1. An operator can clear a **named** forwarder's queued backlog; afterwards its `pending`,
   `in_progress`, and `failed` rows are gone and its `uploaded`/`success` rows remain
   untouched. The nearest confusable outcome — losing real upload history, or clearing every
   forwarder when one was named — must fail the test.
2. For a **disabled** forwarder the clear is race-free (its worker is not running); this must
   remain true and be characterised, not merely assumed.
3. For an **enabled** forwarder, the chosen coordination policy (see Decisions) prevents the
   clear from removing a row the worker is in the middle of uploading, and its effect is
   observable — a row genuinely in flight is either preserved or the clear waits for it, so a
   row is never silently dropped mid-transmit.
4. The app **Settings → Forwarding** surface shows each forwarder's queued count and offers a
   confirmed "Clear queue" action next to its enable/disable control; a successful clear
   updates the visible count and reports how many rows were discarded.
5. Enrichment and logging are unaffected: clearing a queue never blocks or fails a QSO submit,
   and the operation degrades safely (reports an error, changes nothing) if the daemon write
   fails.

## Decisions settled (2026-08-26)

- **Enabled-forwarder in-flight coordination: clear `pending` + `failed` only.** The
  operator-triggered clear leaves `in_progress` to the worker — "finish the currently claimed
  batch, drop the remaining backlog." Race-free by construction with no worker coordination:
  `ClaimPendingUploads` moves a whole batch `pending`→`in_progress` in one atomic UPDATE, so
  `in_progress` is exactly the batch a worker is processing; leaving it alone lets those rows
  complete while the not-yet-claimed backlog is dropped. The rejected alternatives: quiescing the
  worker needs a per-forwarder worker registry that does not exist; clearing `in_progress` under
  the re-arm guard risks an upload that succeeds upstream with no local record.
- **Clearing is a separate, deliberate action**, independent of enable/disable. Disabling does
  not offer to clear the queue: enable/disable is restart-to-apply, so coupling a live clear to a
  config change needing a restart is muddled, and the startup auto-discard already covers the
  disable-then-restart case.
- **The clear is not recorded in `qso_history`.** The reported discard count is sufficient; the
  QSO itself is untouched (only its pending/failed upload-attempt rows are removed), so a
  queue-clear is not a QSO edit and `qso_history` stays for actual QSO changes.
- **The operator-facing explanation lives in the manual's forwarding chapter** ("Clearing a
  queued backlog"): Clear queue drops a destination's not-yet-sent backlog now, in-flight uploads
  finish, upload history is kept; the inverse — backfilling an existing log — is a separate
  feature.

## Implementation (landed 2026-08-26)

Shipped as five TDD slices, each with a reversion or mutation proof:

- **Store** (`internal/database/sqlite`): `DiscardClearableUploadsForForwarderWithContext`
  (`pending`+`failed` only, per-forwarder, preserving `in_progress`/`uploaded`) and
  `ForwarderQueueCountsWithContext` (per-forwarder `{clearable, in_flight}` via one `GROUP BY`).
- **API**: `GET /v1/forwarder-queues` (every configured forwarder, `{0,0}` default) and
  `POST /v1/forwarder/{name}/queue/clear` (`{discarded}`; `400 invalid_forwarder`,
  `404 unknown_forwarder`; the exact configured name is round-tripped, not trimmed). Documented in
  [`api-endpoints.md`](../v2-design/api-endpoints.md).
- **App** (Settings → Forwarding): "N queued · M in flight" per destination and a confirmed
  "Clear queue" button reporting the discarded count. A clear reconciles against the daemon before
  re-enabling — an indeterminate outcome (timeout, dropped connection, or unreadable 200) or a
  failed refresh invalidates the stale count rather than leaving a re-fireable action.
- **Docs**: this dossier + the manual's "Clearing a queued backlog" section.

All six operator-observable acceptance criteria are met; the daemon operation clears only
`pending`+`failed` for the named forwarder and never touches `in_progress`, `uploaded`, or other
forwarders (proven at the storage boundary).

## Verification standard

Write failing assertions first. Prove the primitive's row-scope preservation directly
(seed `pending`/`in_progress`/`failed`/`uploaded` rows for two forwarders; clear one; assert
only that forwarder's non-uploaded rows are gone and every `uploaded` row on both survives).
Characterise the disabled-startup path. Once the enabled-forwarder policy is chosen, test its
in-flight guarantee with an observable barrier rather than a timing sleep. Cover the app
Settings → Forwarding surface (queued count, confirmed clear, reported count, error path) with
the app's lint/format/Svelte-check/Vitest loop. No RF, audio, CAT, or hardware-dependent
action is needed.

## References

- [`docs/backlog.md`](../backlog.md) — authoritative ranking (this item's P2 rank lives there).
- [`ADR 0022`](../decisions/0022-forwarder-enqueue-by-config-presence.md) — enqueue by config
  presence, the reason a disabled forwarder accumulates a queue.
- [`ADR 0039`](../decisions/0039-forwarder-enabled-gates-enqueue-config-driven.md) —
  forwarder-enabled gating of enqueue, and the startup purge of a disabled forwarder's
  non-uploaded rows.
- [`internal/database/sqlite/api_context.go`](../../internal/database/sqlite/api_context.go)
  `DiscardQueuedUploadsForForwarderWithContext` (`:2521`) — the history-preserving primitive.
- [`cmd/smd/lifecycle_adapters.go`](../../cmd/smd/lifecycle_adapters.go) `startWorkers` — the
  existing startup disabled-forwarder discard call site.
- [`docs/v2-design/config.md`](../v2-design/config.md) — the `forwarders[]` configuration
  contract the Settings → Forwarding surface edits.
- ClubLog `putlogs.php` bulk-backfill item in [`docs/backlog.md`](../backlog.md) (Forwarding /
  data cluster) — **cross-linked, not absorbed**: it notes the same startup purge and the
  "disabling ClubLog mid-403 loses failed-row retry eligibility" provenance limitation, which
  this item's clear policy interacts with but does not resolve.
