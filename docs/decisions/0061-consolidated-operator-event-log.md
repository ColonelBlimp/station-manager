---
number: 0061
title: A categorised operator event store fed from published events, with smd.log retained as the diagnostic sink; alarms ship first
status: Proposed
date: 2026-07-31
---

# 0061 — Consolidated operator logging: an event store, not a log mirror

## Context

The operator's stated shape (2026-07-31): **ALL logging into a DB table,
CATEGORISED** — qso, notification, daemon. Today that is three unrelated
mechanisms: QSO rows plus `qso_history` in SQLite, transient SSE events that
become toasts and then cease to exist, and `smd.log`.

The trigger was toasts. `frontend/app/src/lib/ui/toasts.svelte.ts` caps the
stack at 5 with drop-oldest and TTLs of 4/6/8 s, so an upload failure that fires
while the operator is looking at the rig is gone with **no trace anywhere**. The
older half of the same problem is `backlog.md:1119` (surfaced 2026-06-30): a
loud `smd.log` line is worthless to an operator who will not `tail` a file, and
external ops have no window into daemon activity at all.

**Measured on the live system, 2026-07-31** — `smd.log` at 14.36 MB / 81,978
lines over 15.1 days:

- **99.5% of lines are `info`** (81,557 info / 454 warn / 57 error).
- **Three message types are 65% of bytes**: `forwarding: success` 24%,
  `http request` 23%, `forwarding: submit` 18%.
- Growth ≈ 1.0–1.8 MB/day on current-shape builds; lumberjack already rotates
  at 100 MB / 5 backups / **30-day age**, so the age limit binds first and files
  already self-purge.
- `qso_history` holds **2 rows against 6,620 QSOs** — the trail works, it is
  simply rarely exercised.
- The log spans **58 distinct builds in 15.1 days**, longest single-build run
  0.95 days.

Two facts constrain the design more than anything else. First, the invariant
*"the only thing that should stop logging is a broken local DB"* — if daemon
diagnostics become DB rows, a DB fault destroys the evidence of the DB fault.
Second, `smd.log` holds ~170 `callsign provider error` lines whose text comes
from an **external provider**; a table that mixes diagnostics with an
operator-facing surface puts every future third-party string one query from a
browser — the shape of the two P1 credential leaks of 2026-07-25.

## Decision

**Proposed, not accepted.** An **operator-facing EVENT store**, fed from events
the daemon already publishes *for display* — never by mirroring `smd.log`:

1. **One categorised event table in `station-manager.db`**, with a JSON detail
   column and the **build version stamped on every row**. Follows `qso_history`
   (ADR 0016), which already establishes both patterns in this schema.
2. **`smd.log` is retained unchanged** as the diagnostic sink of last resort,
   and remains the only home for raw third-party error text.
3. **The `qso` category already exists.** `qso_history` works and has been
   accumulating correctly; what is missing is the way out (no HTTP route, no SPA
   surface, `FetchQsoHistoryByUUIDWithContext` called only from tests). That
   half is a **surfacing** job, not a design one.
4. **smcloud gets the same split** — a tenant-scoped structured event log for
   "what happened to this user's data", with application diagnostics staying in
   journald. One principle, two deployments.
5. **Alarms are the pilot slice** (see below).

## Alarms first, and why

The TX and drive alarms are the right first category, and deliberately not
because they are the most important:

- **The feed already exists.** `EventTxAlarm` and `EventDriveAlarm` go through
  the bridge hub today. Only the sink is missing.
- **Volume is trivial** — 9 published alarms in 15 days, across all builds.
- **They exercise every hard part**: build stamping, retention, an SPA surface,
  and the acknowledgement round-trip — at a scale where getting it wrong is
  cheap.
- **They unblock ADR 0060**, which is explicitly parked on alarm-behaviour data
  that does not currently exist in usable form.
- They prove the shape before `qso`, `notification` and `daemon` arrive together
  with competing constraints.

The alarm record today is **logged but not usable**, in four specific ways:
no build attribution (so a 15-day frequency blends 58 builds and means nothing);
not queryable without a bespoke script replaying startup markers over 82k lines;
**no operator half** — `dismissTxAlarm`/`dismissDriveAlarm` set a client-side
boolean and send nothing, so nothing records whether an alarm was ever seen or
acknowledged; and nothing distinguishes *raised and seen* from *raised into an
empty room*, since `smd` runs headless and an alarm with no browser attached
surfaces to nobody.

The third is the one that matters most: for a safety alarm, "was this seen, and
was it real?" is the question the operator keeps hitting, and it is precisely
the half no mechanism records.

## Alternatives considered

### Mirror `smd.log` into a table

The literal reading of "ALL logging into a DB table". Rejected on the
measurements: 99.5% of it is `info` and 65% of the bytes are three mechanical
sources, so it would pay QSO-grade write costs to store noise, put ~5,400
inserts/day against the one write path that must never stall
("one-fails-all-fail for QSO writes"), and drag uncontrolled third-party error
strings into a browser-reachable table.

### A separate logging database

Keeps log writes off the QSO write path entirely. Rejected **at this volume** —
a selectively-fed event store is ~100–150 rows/day (~50k/year), which is nothing
for SQLite, so a second file buys isolation nobody needs while adding a second
thing to back up, restore and migrate. Revisit if the category set grows to
include something high-frequency.

### Files only, with better tooling around them

`smd.log` plus a good query tool. Rejected because it cannot serve the two
surfaces that motivated this: an in-app notification rail, and remote admin
troubleshooting of *someone else's* tenant. It also leaves the exfiltration
problem exactly where it is.

### Extend `qso_history` to carry all categories

Tempting — it exists, it works, it is already categorised by `op`. Rejected
because its shape is QSO-specific: `op` is `CHECK (op IN ('update','delete'))`
and `before_image` is a full QSO JSON round-trip. Widening both to carry
alarms and notifications would make it a worse audit trail without becoming a
good event log.

### journald / syslog for everything

Rejected: no SPA surface, no tenant scoping, and on the smcloud side it is
exactly what exists today and is the reason "my QSOs aren't appearing" is
currently unanswerable for another operator's tenant.

## Consequences

- **Retention becomes a decision rather than an inferred default.** Files
  already self-purge at 30 days; the event table would not, and at ~50k rows/year
  there is no pressure forcing an answer — which is precisely why it must be
  chosen rather than defaulted.
- **`qso_history` must NOT share the event table's retention policy.** It is
  provenance, not diagnostics; purging it destroys the audit trail. At 2 rows
  per 6,620 QSOs it costs nothing to keep indefinitely.
- **Build stamping stops being optional.** At 58 builds in 15 days, a table
  without a version column is as un-attributable as the log is today. See the
  ship-gate backlog entry for the format trade-off (+22% full string vs +8%
  bare hash) — the same decision applies to both sinks and should be made once.
- **The acknowledgement round-trip is new surface.** Recording "the operator saw
  this" needs a route; today dismissal dies in browser state.
- **Feeding from published events, not `smd.log`, is load-bearing** and must not
  be relaxed later for convenience. It is the entire defence against the
  third-party-string exfiltration shape.
- The four **ship-gate coverage gaps** (config saves, QSO deletes, notification
  events, build attribution) are prerequisites, not part of this store: they are
  events that currently do not exist to be recorded.

## Open questions — operator's call, deliberately not filled in

- **Is the smcloud admin surface internet-facing, or behind WireGuard/Tailscale?**
  Raised 2026-07-20, still unanswered, and it gates the smcloud half entirely.
  Phase 2 is already blocked on the ADR 0040 security assessment; an admin
  endpoint serving logs belongs inside that assessment.
- **Retention for the event table** — age, row cap, or keep-forever.
- **Version format**, once, for both sinks.
- **Whether `notification` and `daemon` join this table or stay separate.** The
  three constraints above apply differently per category; alarms as the pilot
  should answer it with evidence rather than argument.
- **smcloud retention posture.** Tenants consented to a *backup*; "diagnostics
  retained indefinitely on a VPS" is a wider posture and needs its own answer.

## Triggers to revisit

- **If a category arrives that is genuinely high-frequency**, the "same DB" and
  "no separate store" conclusions both reopen — they rest on ~150 rows/day.
- **If the event table is ever proposed to be fed from `smd.log`**, this ADR is
  being violated rather than revisited; the measurements above are the argument.
- **If `qso_history` grows a `create` or `forward` op**, re-check whether the
  audit trail and the event store are still two things.
- **If a second operator is supported locally** (not just via smcloud), the
  single-operator assumption behind "the operator is the admin" breaks.

## References

- ADR 0016 — `qso_history` (the existing audit table; JSON `before_image`).
- ADR 0060 — alert surfaces; parked on alarm data this store would produce.
- ADR 0040 / `docs/v2-design/sm-cloud-p1.md` — the security assessment that must
  contain any admin log surface.
- ADR 0052 — smcloud passive store; a diagnostics layer sits alongside it, never
  entangled with its tables or transactions.
- ADR 0008 — the toast system whose 5-deep, TTL-expiring stack is the
  disappearing surface that started this.
- `docs/v1-analysis/invariants.md` — "the only thing that should stop logging is
  a broken local DB"; "one-fails-all-fail for QSO writes".
- `backlog.md` — the SHIP GATE log-coverage entry (four gaps) and the
  operator-log-viewer entry (2026-06-30).
- `internal/bridge/txconfirm.go`, `internal/bridge/drivealarm.go` — the alarm
  events that would feed the pilot.
