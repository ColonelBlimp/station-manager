# W-0005 — Operator-triggered forwarder queue clearing

**Status:** Open — enabled-forwarder in-flight coordination policy required
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
- **What remains is entirely the operator-triggered path.** There is no API endpoint or app
  UI for an operator to invoke the discard on demand, and — critically — no coordination
  policy for an **enabled** forwarder. Because the primitive's delete scope includes
  `InProgress`, a clear issued while an enabled forwarder's worker is mid-push would race and
  could delete a row the worker is actively uploading. The disabled-at-startup path is
  race-free only because the worker for a disabled forwarder is never spawned.

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

## Decisions required before implementation

- **Enabled-forwarder in-flight coordination policy (open).** Options include: quiesce the
  worker (pause/drain) for the duration of the clear; restrict the operator-triggered clear to
  `pending` + `failed` and leave `in_progress` to the worker; or confirm-through-in-flight
  (clear pending/failed, then let the current in-flight row complete or be re-evaluated). This
  is the substance of "what remains" and must be settled before building; it changes both the
  daemon operation's row-scope and its concurrency guarantees.
- Should disabling a forwarder from the UI **offer** to clear its queue at the same time, or
  stay a separate deliberate action? (The startup auto-discard already handles the
  daemon-restart case; this is about the live disable.)
- Should a purge be recorded in the audit trail (`qso_history`) so a cleared backlog is
  explainable later, or is the reported discard count sufficient?
- Where does the operator-facing explanation live — the (stub) manual forwarding chapter —
  and what does it say: adding a forwarder queues *future* QSOs; disabling stops sending but
  not queue growth; this is the lever to empty it; the inverse (backfill an existing log) is a
  separate feature.

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
