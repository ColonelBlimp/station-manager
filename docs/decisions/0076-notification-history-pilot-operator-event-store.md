---
number: 0076
title: Notification-history pilot — the local operator_event store, notification category first
status: Accepted (operator-ratified 2026-08-22)
date: 2026-08-22
---

# 0076 — Notification-history pilot: the local operator_event store, notification first

## Context

[W-0001](../archive/work/W-0001-durable-notifications.md) closes the one remaining ship-gate
notification-coverage gap: a toast (`frontend/app/src/lib/ui/toasts.svelte.ts` — five deep, TTL
4/6/8 s, drop-oldest) that fires while the operator is looking at the rig is gone with no trace
anywhere — including browser-only failures like ADIF export.

[ADR 0061](0061-consolidated-operator-event-log.md) (Proposed) designed the larger answer: one
categorised operator event store in `station-manager.db`, fed from *published* events and never from
`smd.log`, build-stamped, following the `qso_history` pattern
([ADR 0016](0016-sm-cloud-deferred-with-prep.md)). It chose
**alarms** as the pilot and left several questions to the operator: retention, version format,
whether `notification`/`daemon` share the table, and the SM Cloud admin surface.

This ADR settles exactly the subset W-0001 needs — the **local-store shape and the pilot order** —
and defers the rest. It does **not** accept ADR 0061 wholesale: its alarm-behaviour and SM Cloud
decisions remain open. W-0001 replaces "alarms first" with a **notification** pilot.

## Decision

Ratified 2026-08-22.

1. **Two durable kinds to start** — chosen to force both ingestion paths without breadth:
   - `export.adif_failed` — **browser-originated**.
   - a **terminal** `forward.failed` — **daemon-originated**.
   Bridge/CAT/enrichment and other kinds are added later, deliberately, one boundary at a time.

2. **Typed, bounded metadata only — never raw third-party text.** `forward.failed` persists QSO ID,
   forwarder, action, and attempts; it **never** persists `Reason` (the upstream/provider string).
   The browser ingestion endpoint accepts an **allowlisted, typed** request per kind — not an
   arbitrary `{kind, severity, detail}` envelope. The **daemon** stamps severity, occurrence time,
   and build version; the client supplies only the typed, kind-specific fields. This is the entire
   defence against the third-party-string exfiltration shape ADR 0061 names.

3. **One `operator_event` table in ADR 0061's categorised shape** (a `category` column, a typed/JSON
   detail, and the build version on every row), but **only the `notification` category is wired
   now**. No generic event table is populated for `daemon`/`alarm`/`qso` by this work.

4. **Recording is explicit at each producing boundary.** There is **no generic hub subscriber and no
   toast-level recorder**: a durable event is written by the specific call site that knows the
   outcome, while the event hubs remain ephemeral in-memory pub/sub feeding live SSE. A toast is
   display feedback; it is not the trigger for persistence.

5. **Retention: the last 500 rows per category, oldest-first eviction.** Insert and prune run
   **outside every QSO transaction** — notification-history failure is observable but never rolls
   back or blocks a QSO write, and `smd.log` stays the independent diagnostic sink. `qso_history`
   keeps its own indefinite retention and never shares this policy.

6. **No acknowledgement in W-0001.** No `acknowledged_at`, no mark-read endpoint, no unread workflow.
   Toast dismissal and viewing the history are **not** acknowledgements. Alarm acknowledgement has
   different ("was this safety alarm seen, and was it real?") semantics and belongs with the future
   alarm pilot.

7. **One version format, daemon-stamped.** The event rows carry the **same canonical full
   build-version string** the diagnostic logs already carry (established `116cf34b`), stamped
   daemon-side for both the browser- and daemon-originated sources.

8. **One thin end-to-end pilot.** Schema, production, persistence, retrieval, and the history UI may
   land as separate TDD commits, but **no partial release counts as W-0001**: the item closes only
   when a durable event survives toast expiry and page reload end to end, for both a browser- and a
   daemon-originated kind.

## Consequences

- The `operator_event` table exists in the categorised shape, so **if** a later category (alarm,
  daemon) is adopted it can reuse that shape without a throwaway table or a consolidation migration.
  Those categories remain Proposed under ADR 0061; `qso_history` stays its own audit table and is not
  folded in.
- Persistence is decoupled from the QSO write path; a history-store fault degrades observability, not
  logging integrity (the load-bearing invariant).
- Because recording is per-boundary and typed, adding a durable kind is a deliberate, reviewable act
  — never an accidental firehose from the hub or the toast queue.
- Retention is bounded and testable: cap eviction (deliberate) is distinguishable from accidental
  disappearance.
- Acknowledgement, alarm attribution, daemon diagnostics, and any SM Cloud/admin surface remain
  future work under ADR 0061 and the ADR 0040 security assessment.

## Alternatives considered

- **A generic hub subscriber or a toast-level recorder** that persists everything published or every
  toast. Rejected: it re-imports the exfiltration and volume problems ADR 0061 rejected, and makes
  durability an accident of the display layer rather than a decision at the boundary that knows the
  outcome.
- **A notification-only table now, consolidated later.** Rejected: it buys a throwaway schema and a
  later migration for no benefit over building ADR 0061's categorised shape and wiring one category.
- **Acknowledgement in the pilot.** Deferred: the ACs need only retrievability after expiry/reload;
  alarm-grade "was it seen" semantics differ and belong to the alarm pilot.
- **Accepting ADR 0061 wholesale.** Rejected: its alarm-behaviour and SM Cloud decisions are still
  open; ratifying them here would decide questions W-0001 does not touch.

## Relationship to other work

- **Partially supersedes** [ADR 0061](0061-consolidated-operator-event-log.md) for the **local-store
  shape and pilot order** only: the categorised local table is adopted, and the pilot becomes
  notification rather than alarms. ADR 0061's alarm, daemon-diagnostics, and SM Cloud decisions are
  untouched and remain Proposed.
- **Builds on** [ADR 0016](0016-sm-cloud-deferred-with-prep.md) (the `qso_history` pattern: a
  categorised audit row with a JSON payload) and [ADR 0008](0008-notifications-toast-system.md) (the
  transient toast surface this makes durable).
- **Out of scope**, per ADR 0061 and [ADR 0040](0040-sm-cloud-p1-backup-restore.md): the SM Cloud
  tenant-scoped store and any internet-/WireGuard-facing admin log surface.
- **Work item:** [`W-0001`](../archive/work/W-0001-durable-notifications.md). The `operator_event` schema,
  the browser ingestion endpoint, the daemon-boundary producers, the retrieval route + SPA rail, and
  their TDD land under it; this ADR records only the settled policy.
