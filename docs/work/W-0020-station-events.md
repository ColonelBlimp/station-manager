# W-0020 — Station Events: a full-page event section replacing the notification slide-over

**Status:** Selected — dossier and ADR 0061 dated update under operator review before code (revised
2026-09-14 after the second designer's review; one ruling outstanding: the recorder's overflow policy)
**Selected:** 2026-09-14
**Outcome:** The header's "Notification history" slide-over is replaced by a **Station Events** section that
takes the whole content area like the Logbook. It shows, newest first and filterable by category and
severity, the two durable failures the rail already keeps (a failed ADIF export, a terminal forwarding
failure) plus the **alarm family**: a TX alarm raised and cleared, a drive alarm raised and recovered, a
safety disarm the operator did not ask for, and an exchange terminated abnormally. Every row keeps ADR
0076's discipline — typed, bounded metadata written from the one boundary that knows the outcome, never
provider text, never a mirror of `smd.log`. Routine activity stays where it lives.

`W-0020` is an immutable identity. Its status may change, while priority and ranked position live only in
[`docs/backlog.md`](../backlog.md).

## Why this item exists

The rail is internally correct and product-incoherent (operator, 2026-09-13/14; second designer's review,
2026-09-14). A permanent icon in the global header labelled "Notification history" promises a notification
centre; it opens on "No notifications yet" nearly every time because by ratified decision it records only
two failure kinds, neither of which has occurred since the feature shipped. The operator's instinct was a
log viewer. The design answer is narrower: the events that matter after the fact are the ones that fired
while nobody was looking — a TX alarm that self-cleared, a disarm, an exchange the daemon ended — and those
already have producing boundaries in the daemon (ADR 0061 chose alarms as its pilot for exactly this reason,
before W-0001 shipped `notification` first). This dossier ships the alarm family into the existing store and
gives the whole store a surface worth its place in the navigation.

## Rulings (operator, 2026-09-14)

1. **Alarm-category retention** is the newest 500 rows, oldest-first eviction, matching the per-category
   precedent (`operatorEventRetentionPerCategory`).
2. **A routine linger disarm** after the last FT view closes is normal lifecycle and is **not** an abnormal
   session termination. If linger expiry actually terminates an exchange in progress, record **one**
   abnormal termination with cause `unattended`; do not duplicate it as a generic disarm event.
3. **Persistence (second designer, confirmed):** the existing `operator_event` table in
   `station-manager.db` — one table separated by category and typed kind, append-only rows with bounded
   JSON detail, severity, timestamp and build; new kinds through a migration and explicit producing
   boundaries; retention per category; writes in the store's own best-effort transaction, never inside a
   QSO/upload transaction. Not `smd.log`, not `qso_history`, not a new database — `smd.log` stays the
   independent diagnostic fallback, including when the local database itself is broken.
4. **Endpoint:** `GET /v1/station-events`; `GET /v1/notifications` retired in the same slice; the
   browser-ingestion `POST /v1/notifications` stays.
5. **`partner_call`** is included on a termination row, normalised (upper-case, trimmed) and bounded to 32
   characters — necessary context, still untrusted decoded input.
6. **Alarm dismissal in the SPA is not acknowledgement** — confirmed; acknowledgement stays ADR 0061's open
   question.
7. **Severity per kind:** raised alarms `error`; clears and recoveries `info`; automatic disarms and
   abnormal terminations `warn`.
8. From the converged design (operator + second designer): full-page section named **Station Events**,
   not "Logs"; the alarm family plus the two existing kinds only; nothing routine added to make the page
   look busy; the "warn/error/fatal from the logging health writer" sketch is rejected; generic daemon
   diagnostics and the SM Cloud surface stay Proposed in ADR 0061; own dossier.

## Scope

### Store — migration `0009`

- `operator_event` gains category `alarm` and kinds `tx_alarm.raised`, `tx_alarm.cleared`,
  `drive_alarm.raised`, `drive_alarm.cleared`, `tx.disarmed`, `session.terminated`. The CHECK enforces the
  **valid (category, kind) pairs**, not two independent allowlists — `notification` + `tx_alarm.raised`
  must be illegal — and the severity set stays `info | warn | error`.
- **Down-migration** to schema 0008: preserve every `notification` row; deliberately discard `alarm` rows
  (they cannot exist under the 0008 CHECKs), stated in the down file's header.
- Retention unchanged, 500 per category. The head bump moves every test that pins head 8:
  `internal/api/handler_version_test.go`, `internal/database/sqlite/migration_origin_test.go`,
  `migration_operator_event_test.go` (whole-tree tests before the commit — the W-0001 lesson).

### Producing boundaries — narrow typed facts, never a generic record call

The bridge must not import storage and FT8 stays isolated from the services around it (AGENTS.md), and ADR
0076 forbids producers an arbitrary category/kind/detail surface. So each producer emits a **typed fact**
through a narrow injected interface, and **assembly** (`cmd/smd`) converts facts into stored rows:

- `internal/bridge` — a `TxAlarmObserver` / `DriveAlarmObserver`-shaped seam with methods like
  `TxAlarmRaised(code string, at time.Time)`, `TxAlarmCleared(code string, raisedAt, at time.Time)`,
  `DriveAlarmRaised(code, at)`, `DriveAlarmRecovered(code, at)`. Called from the single publish sites
  (`txconfirm.go` `publishTxAlarm`, `drivealarm.go` raise ~596 / recovery ~686). **The clear today
  publishes an empty code and keeps no raised-at time** (`confirmTxIdle` → `publishTxAlarm(false, "")`),
  so the bridge service retains the standing alarm's identity — code and raised-at — from raise to clear
  and hands both to the observer; `active_ms` is computed at assembly from the two stamps. The hub event
  and its payload are unchanged.
- `internal/ft8` — two seams:
  - **Abnormal termination at the sequencer's teardown boundary**, `finishAbandonLocked` in
    `sequencer.go`, which already reads the partner before `abandonLocked` clears the exchange pointers
    (ft8-logging-gaps finding 2). It emits `ExchangeTerminated(cause, partnerCall, rung string, at
    time.Time)` **only when an actual partner exchange is in progress** — `partnerCallLocked() != ""` —
    for causes `unattended`, `cat_lost`, `dial_moved`, `dial_unknown`, `tx_not_armed`, `tx_bad_message`.
    This is the one place that sees every retirement, including the rung-driven ones (`tx_not_armed`,
    `tx_bad_message`, the dial refusals through `AbandonIfCurrent(gen, endReasonForRefusal(...))`) that
    never pass through the service disarm. Operator causes (abandon, stop, band change, shutdown) emit
    nothing.
  - **Idle automatic disarm at the service boundary**, `disarmTxLocked(cause)` in `servicetx.go`:
    `TxDisarmed(cause, at)` for `cat_lost` and `dial_moved` **when no partner exchange is in progress**;
    `unattended` while idle — including an armed Call-CQ run between contacts, where `Sequencer.Active()`
    is true but there is no partner — is routine lifecycle and emits nothing (ruling 2). `operator`,
    `band_change`, `shutdown` emit nothing. When an exchange IS in progress the sequencer boundary has
    already emitted the termination; the service emits no second row.
- Both seams are nil-safe: an unwired observer records nothing.

### The recorder — assembly-owned, non-blocking, bounded

A synchronous SQLite write from TX confirmation would stall the CAT read loop, and one from FT8 teardown
would hold the sequencing gates. So assembly owns a **bounded, lifecycle-managed recorder** (an ADR 0070
node, started after the store, stopped before it): producers hand it a fact with the **occurrence time
captured before enqueue**; a worker goroutine converts facts to `OperatorEventInput` (category, kind,
severity per ruling 7, build, typed detail) and calls `RecordOperatorEvent`. The enqueue never blocks a
safety path. A dropped fact (queue full) and a failed write are both **logged with the kind and a dropped
counter, never retried on the caller's thread**. Queue capacity and the overflow policy are the operator's
ruling (below); the proposal is capacity 64 and drop-newest — the alarm probe cadence bounds any burst far
below that, and drop-newest keeps the earlier rows that explain a burst.

### API

`GET /v1/station-events?category=&severity=&limit=` returns `{items: [OperatorEvent…]}` newest first
across categories (or one when given), the DTO `GET /v1/notifications` serves today. `GET
/v1/notifications` is retired in the same slice (ruling 4); `POST /v1/notifications` stays. Detail shapes
per kind are enumerated in `docs/v2-design/api-endpoints.md`.

### SPA

A `stationEvents` view — nav item **Station Events** between Logbook and Settings, full content area, lazy
chunk like Logbook — listing rows newest first with category and severity filter chips and a count line so
an empty filtered list reads as a filter, not a fault; kind wording mapped client-side per kind (ADR 0010;
unknown detail shapes read "Details unavailable", never stringified). The header button,
`NotificationRail.svelte`, `ui.notifications` state and their tests are removed; `keyboard-shortcuts.md`
if the rail had a binding. TX-alarm dismissal is unchanged and records nothing (ruling 6).

### Manual

One page for the section; the existing notification wording folds into it.

## Non-goals

- Acknowledgement ("was this alarm seen, was it real"), unread counts, badges — ADR 0061's open question,
  still open. Toast dismissal, alarm dismissal and viewing the page are not acknowledgements (ADR 0076 §6).
- A `daemon` diagnostics category, any read of `smd.log`, any severity-driven mirror of the logger.
- Routine activity: QSOs logged (the Logbook), successful uploads (their surfaces), FT session start and
  stop, decodes, the routine linger disarm.
- The `qso` category surfacing (`qso_history` has no route) — separate item if ever wanted.
- SM Cloud tenant events and the admin surface (ADR 0061, gated on the ADR 0040 assessment).
- A generic hub subscriber, a toast-level recorder, or a generic `RecordOperatorEvent` callback handed to
  producers (ADR 0076 §4; second designer's review).

## Acceptance criteria (operator-observable)

| # | Criterion | Nearest confusable outcome it must be distinguished from |
| --- | --- | --- |
| AC1 | The header shows no notification icon; the sidebar shows **Station Events** between Logbook and Settings, and it opens a full-width page like the Logbook. | The slide-over surviving beside the new page. |
| AC2 | A transient TX alarm that raised and cleared while the operator was away appears as two rows — raised (`error`) then cleared (`info`), both carrying the alarm code, the cleared row how long it stood — with the build that produced them. A drive alarm that raised and recovered appears the same way with its code. | A raise with no clear because the clear was published but not recorded; a clear with an empty code or no duration; a drive alarm missing while a TX alarm is present. |
| AC3 | Closing the browser on an idle armed FT8 session records nothing — including an armed Call-CQ run waiting between contacts, where the sequencer is active but no station is being worked; closing it mid-exchange records exactly one `session.terminated` row (`warn`) with cause `unattended`, the partner call and the rung, and no `tx.disarmed` row. | Two rows for one event; a row for the routine linger disarm; a Call-CQ run between contacts counted as an exchange (ruling 2). |
| AC4 | The dial guard stopping a session records one row (`session.terminated` with cause `dial_moved` mid-exchange, `tx.disarmed` with cause `dial_moved` when idle); a rung that cannot key (`tx_not_armed`) or cannot encode (`tx_bad_message`) mid-exchange records one termination; an operator Stop, Abandon, band change or daemon shutdown records nothing. | An operator's own action appearing as an alarm; a rung-driven retirement missed because it never reached the service disarm. |
| AC5 | The two existing kinds (export failure, terminal forward failure) still appear, on the new page, with their existing detail. | Rows lost in the endpoint move. |
| AC6 | Filtering by category or severity narrows the list; the count line and the empty state name the filter. Each category holds at most its newest 500 rows across restarts. | An empty page that could be either a filter or a fault. |
| AC7 | No row ever carries provider or rig free text: every `detail` is a fixed set of typed fields per kind, `partner_call` normalised and at most 32 characters (pinned by a test that enumerates the kinds). | A helpful `reason` string slipping into an alarm row; an over-long or raw decoded call. |
| AC8 | With the store stalled or the recorder's queue full, TX confirmation, the alarm probe and FT8 teardown complete in their usual time and the drop is logged with the kind. | A safety path waiting on a database write. |

## Slices

1. **Store + version head** — migration 0009 (pairs, down-migration policy), the head tests; the fact and
   observer types.
2. **Recorder** — the assembly-owned bounded recorder as an ADR 0070 node, its conversion table (kind,
   severity, detail per fact), tests with a blocked store (AC8) and for overflow per the ruling.
3. **Daemon producing boundaries** — bridge alarm identity retention and observer calls; the sequencer
   termination seam and the service idle-disarm seam per ruling 2; `cmd/smd` wiring; tests with fakes at
   each boundary (fakeKeyer note: model the keyed rung), including the Call-CQ-between-contacts no-row case
   and every rung-driven retirement.
4. **API** — `GET /v1/station-events`, retire `GET /v1/notifications`; `api-endpoints.md`.
5. **SPA** — the section, the filters, the kind wording; remove the rail and header button; Codex triage.
6. **Docs and manual** — the page; ADR 0076 gains a dated note that the `alarm` category joined the table
   on these rulings (ADR 0061's dated update was made at selection).

Each slice is one atomic commit with its tests; TDD with reversion proof; the observatory before Go commits.

## Verification boundary

No RF and no rig command is needed for any slice: every producing boundary is exercised through the
existing fakes (the bridge's gated client, the FT8 fake keyer and sequencer tests). The first real rows
arrive passively — the transient TX alarm has occurred fourteen times since July without any action — and
are read on the page, which is the acceptance evidence for AC2. A keyed test remains per-occasion only.

## Open rulings for the operator

- **Recorder queue capacity and overflow policy.** Proposed: capacity 64; on overflow drop the newest fact
  and log one `warn` line with the kind and a running dropped counter; a failed write is logged the same
  way and never retried on the producer's thread. Alternatives: drop-oldest (keeps the latest state, loses
  the onset), or block with a short timeout (rejected: reintroduces the stall on a safety path).

## Evidence

- 2026-09-14 — selected; dossier written; ADR 0061 dated update; backlog P2 entry. Second designer's
  review the same day: termination recording moved from the service disarm to the sequencer's teardown
  boundary with `tx_bad_message` added; "active exchange" defined as an actual partner exchange; typed
  facts through narrow observers instead of a generic record callback; a non-blocking bounded recorder;
  migration pairs, down-migration policy and the full head-test list; drive alarm in AC2; the TX clear's
  empty code and missing raised-at named as work; severities pinned. Inbox thread 2026-09-13/14 under
  "Nothing listed in the Notifications section".

## References

- ADR 0076 (store shape, pilot order, typed-metadata rule), ADR 0061 (event store, never from `smd.log`;
  dated update 2026-09-14), ADR 0010 (client-side wording), ADR 0051/0057 (TX alarm), ADR 0060 (alert
  surfaces, parked on alarm data), ADR 0070 (lifecycle nodes), W-0001 (archived; the rail this replaces).
- `internal/database/sqlite/operator_event.go`, `migrations/log/0008_operator_event.up.sql`,
  `internal/api/handler_record_notification.go`, `internal/forwarding/worker/worker.go` (the best-effort
  write pattern), `internal/bridge/txconfirm.go` (`confirmTxIdle`, `publishTxAlarm`),
  `internal/bridge/drivealarm.go`, `internal/ft8/sequencer.go` (`finishAbandonLocked`, end reasons),
  `internal/ft8/servicetx.go` (`disarmTxLocked`, `endReasonForRefusal`),
  `frontend/app/src/lib/ui/NotificationRail.svelte`, `Header.svelte`, `lib/api/notifications.ts`.
