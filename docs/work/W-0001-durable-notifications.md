# W-0001 — Durable operator notification history

**Status:** Open — policy settled ([ADR 0076](../decisions/0076-notification-history-pilot-operator-event-store.md), 2026-08-22); implementation next
**Selected:** 2026-08-18
**Outcome:** Important operator notifications remain retrievable after their transient toast expires
or the browser tab closes.

This is the first stable-ID work dossier created by the OSS-maintainability programme. `W-0001` is
an immutable identity; its status may change, while priority and ranked position live only in
[`docs/backlog.md`](../backlog.md).

## Why this item is still open

The original ship-gate entry grouped four missing durable facts. Three no longer represent open
work:

- config saves gained structured change records in `7b21b2b1`;
- the claim that QSO deletes were unlogged was false—delete has logged since `d516d816`, and the
  delete transaction also writes `qso_history`;
- every diagnostic log record gained the canonical full build version in `116cf34b`.

Notification durability is the remaining gap. The current toast state is deliberately transient:
[`toasts.svelte.ts`](../../frontend/app/src/lib/ui/toasts.svelte.ts) keeps at most five items,
drops the oldest on overflow, and expires ordinary entries after 4/6/8 seconds. Production has many
call sites, including browser-only failures such as ADIF export in
[`ExportDialog.svelte`](../../frontend/app/src/lib/operate/ExportDialog.svelte). Closing or
reloading the tab erases that evidence. There is no notification-history persistence or retrieval
surface in the current daemon or SPA.

[`ADR 0061`](../decisions/0061-consolidated-operator-event-log.md) records the proposed larger event
store. It remains Proposed for its alarm, daemon-diagnostics, and SM Cloud decisions, and it
*originally* chose alarms as its pilot;
[`ADR 0076`](../decisions/0076-notification-history-pilot-operator-event-store.md) supersedes that
pilot order — **notification** ships first — and is the accepted authority for this work's
local-store shape. ADR 0061 still treats missing notification events as a prerequisite rather than
silently deriving them from `smd.log`.

## Scope

This work item owns the open notification-coverage outcome:

- identify which operator-relevant facts must survive beyond a toast;
- create canonical, structured event facts at the boundary that knows the outcome;
- provide a local history surface that remains useful after toast expiry and page reload;
- define how acknowledgement, retention, and build attribution apply to those records.

It does not authorize mirroring `smd.log`, persisting arbitrary third-party error text, changing
QSO-history retention, building the SM Cloud admin surface, or redesigning alert placement. Those
remain governed by ADR 0061, ADR 0060, and the SM Cloud security work.

## Operator-observable acceptance criteria

1. A selected notification produced while the operator is looking elsewhere remains retrievable
   after its toast TTL and after a page reload. The nearest confusable outcome—toast shown and then
   forgotten—must fail the test.
2. History records carry a stable event kind, severity, occurrence time, and build attribution;
   diagnosis does not depend on parsing display prose.
3. At least one browser-originated failure and one daemon-originated notification are exercised end
   to end, so the design does not accidentally cover only HTTP responses or only SSE events.
4. Raw request bodies, credentials, full URLs, and uncontrolled third-party error text never enter
   the operator-facing history. Tests use hostile representative text to prove this boundary.
5. Notification-history failure is observable but does not roll back or prevent an otherwise valid
   QSO write. `smd.log` remains the independent diagnostic sink.
6. Volume and retention are bounded by an explicit operator decision, and the tests distinguish a
   deliberate expiry from accidental disappearance.

## Decisions settled ([ADR 0076](../decisions/0076-notification-history-pilot-operator-event-store.md), 2026-08-22)

- **Two durable kinds first:** `export.adif_failed` (browser-originated) and a terminal
  `forward.failed` (daemon-originated). Bridge/CAT/enrichment kinds come later, one boundary at a time.
- **Typed, bounded metadata only.** `forward.failed` persists QSO ID, forwarder, action, attempts —
  never `Reason`. The browser endpoint takes an allowlisted typed request per kind, not an arbitrary
  `{kind, severity, detail}`; the daemon stamps severity, time, and build.
- **One local `operator_event` table in ADR 0061's categorised shape, `notification` category wired
  now.** Recording is explicit at each producing boundary — no generic hub subscriber, no toast-level
  recorder; the hubs stay ephemeral.
- **Retention: last 500 rows per category, oldest-first**, with insert/prune outside every QSO
  transaction. No acknowledgement in this pilot (no `acknowledged_at`, mark-read, or unread workflow).
- **Build attribution:** the canonical full build-version string, stamped daemon-side for both sources.
- **One thin end-to-end pilot:** schema, production, persistence, retrieval, and history UI may be
  separate TDD commits, but no partial release counts as W-0001.

The SM Cloud exposure/retention, alarm acknowledgement, and `daemon` diagnostics stay outside this
local pilot; ADR 0061 and the ADR 0040 security assessment gate those later surfaces.

## Verification standard

Behavior tests must make the durable and transient outcomes differ at a public boundary: create an
event, let the toast disappear or reconstruct the SPA state, then retrieve the event from history.
Tests that merely assert a toast or log line exists do not close this item. Failure-path fixtures
must also prove that persisted structured fields exclude the unsafe raw text named above.

## References

- [`docs/backlog.md`](../backlog.md) — authoritative ranking.
- [`ADR 0061`](../decisions/0061-consolidated-operator-event-log.md) — proposed event-store shape and
  rejected log-mirroring alternatives.
- [`ADR 0060`](../decisions/0060-operator-alert-surfaces-and-stuck-tx-overlay.md) — alert placement,
  deliberately separate.
- [`ADR 0008`](../decisions/0008-notifications-toast-system.md) — transient toast behavior.
- [`docs/reviews/oss-maintainability-plan.md`](../reviews/oss-maintainability-plan.md) — dossier and
  living-work decomposition rationale.
