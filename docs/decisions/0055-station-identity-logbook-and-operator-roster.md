---
number: 0055
title: Split station identity across logbook, config, and an operator roster
status: Accepted
date: 2026-07-22
---

# 0055 — Split station identity across logbook, config, and an operator roster

## Context

Today the operating callsign is a single global config field
(`config.LoggingStation.StationCallsign`, the "My Station" bucket). The QSO
submit gate requires `STATION_CALLSIGN == the selected logbook's callsign`
(`qsoservice/submit.go`), and FT8 TX identity + PSK Reporter read the same
global. This forces **single-callsign operation**: switching your call means
editing the global, which orphans the default logbook (its QSOs then fail
`callsign_mismatch`). The 2026-07-22 `internal/api` review (`e5da1945`,
`128b6e80`) added 409 guards to fence that footgun — but guards only patch a
model that shouldn't allow the state in the first place.

The operator holds multiple calls (7Q5MLV, 7Q8AC) and runs special-event /
contest-group operations: one physical station transmits under one call
(e.g. `7Q1…`, licence-owner `7Q7CT`) while the person at the key rotates through
a group of operators (`7Q5MLV`, then others) against a single logbook. "My
Station" (`types.LoggingStation`) is a non-ADIF bucket that bundles three
distinct concerns — the call in use, the physical shack, and the person
operating — which vary independently. ADIF has no such grouping; it has the
individual fields (`STATION_CALLSIGN`, `OWNER_CALLSIGN`, `OPERATOR`, `MY_*`).

## Decision

Dissolve the "My Station" bucket and re-home its ADIF fields by what they
describe: **`STATION_CALLSIGN` + `OWNER_CALLSIGN` become logbook attributes**
(the logbook *is* the station identity / call in use); **the `MY_*`
location/equipment fields stay in a physical-station config** (one shack per
daemon); and **`OPERATOR` (+ `MY_NAME`) float**, sourced from a config **operator
roster** (`operators: [{callsign, name}]` + `default_operator`) and a transient
per-session "current operator". Every live QSO is stamped by the daemon —
`STATION_CALLSIGN`/`OWNER_CALLSIGN` from the selected logbook, `OPERATOR`/`MY_NAME`
from the current operator, `MY_*` from config — so the operating callsign is
**derived, never client-supplied**.

## Selection UX (amended 2026-07-22)

The "selected logbook" and "current operator" are **global selectors in the app
shell header**, not per-panel controls: the operator picks a logbook once and it
spans the whole sitting across bands AND modes — the canonical session is FT8
80→40→30→20 m then phone 20 m, one call and one logbook throughout. The header
shows the current logbook's name + QSO count and the current operator.

Crucially the current logbook is a **persisted setting, not daemon session
state**: it rides on the existing `default_logbook_id` (already daemon-persisted;
the FT8 sink already reads it). Switching the header selector updates
`default_logbook_id`; every operation still carries the logbook to the daemon
(Phone/CW `?logbook=`, FT8 `logbook_id`, both defaulting to the current), so the
daemon stays **stateless**. This setting-vs-session distinction is what keeps the
rejection of a daemon-held operating session (below) intact: a *setting* is
shared across tabs and survives reload; *session state* would not, and would
collide with the multi-tab operating lock (ADR 0049). The current operator is a
small header picker over the roster; it and the logbook switch independently
(change logbook to run the 7Q1 event; change operator when someone else takes the
key). FT8 therefore needs no logbook selector of its own — it uses the current
logbook, which is what the sink already does.

## Alternatives considered

### Keep the global station callsign + the interim 409 guards (status quo)

Rejected: the guards (block a callsign change that orphans the default logbook,
block deleting the default, reject setup mismatch) fence the footgun but keep
single-callsign operation, which the operator genuinely needs. They also created
a first-run deadlock (their own follow-up finding). This model is what forces the
guards; removing the global removes the need for them.

### Logbook = full station profile (call + owner + all `MY_*`)

Rejected: the physical shack is one QTH; duplicating grid/lat-lon/antenna/rig/
zones across every logbook is redundant and drift-prone. The operator's own case
is "run `7Q1` **from my own shack**" — the call and owner differ, the location
does not — so location belongs to the shack (config), not the call.

### Call-only logbook, owner in config

Rejected: `OWNER_CALLSIGN` must be able to differ per call without a config edit —
`7Q1`'s owner is `7Q7CT`, `7Q5MLV`'s is itself. Owner tracks the call/licence, so
it rides the logbook.

### Daemon-held "current operating session" (a stateful active-logbook/operator)

Rejected: makes the daemon stateful about who is operating what, collides head-on
with the multi-tab operating-lock (ADR 0049), and buys nothing — every value can
be resolved per-operation (the logbook per submit / FT8 arm; the operator passed
alongside). Stateless is simpler and safer.

### Single operator config default (no roster)

Rejected: the multi-op case (10 ops, one logbook) would retype call + name every
rotation. A roster lets each op **pick themselves**. Kept in config (like `rigs`/
`forwarders`, no DB table); per-operator QSO counts still come free from
`GROUP BY operator` on the stamped `OPERATOR`. The roster entry is a minimal,
extensible *operator profile* — the one thing here that legitimately is a profile.

### Client-supplied `STATION_CALLSIGN` (SPA sends the call)

Rejected: weakens the "identity is daemon-owned, not client-supplied" property —
load-bearing for FT8 TX (real RF). Daemon-stamping from the *selected* logbook is
strictly stronger: the client picks among its own logbooks and can never invent a
call.

## Consequences

- **The `callsign_mismatch` gate is removed** — `STATION_CALLSIGN` is derived from
  the logbook, so it cannot disagree with it.
- **The 2026-07-22 review's orphan/delete/setup 409 guards are retired** — the
  class of footgun they fenced (a global callsign drifting from the logbook) no
  longer exists.
- Config `station_callsign` **demotes to the seed** for the home logbook (and a
  "primary call" display); it is no longer stamped on QSOs.
- **Schema:** the logbook table gains `owner_callsign` (defaults to its
  `station_callsign`); config gains `operators` + `default_operator`; the
  `LoggingStation` struct's identity fields split out from the `MY_*` set.
- **FT8:** gains a logbook selector (defaults to the default logbook = today's
  behaviour) and takes the current operator at arm time; the on-air TX call is the
  selected logbook's callsign (`OPERATOR` is logged, not transmitted).
- **Phone/CW:** submit stops trusting the SPA's `STATION_CALLSIGN` and stamps it
  from `?logbook=N`; the SPA passes the current operator.
- **Import is unchanged:** an imported ADIF record keeps its own `STATION_CALLSIGN`
  (historical fidelity — today's `isImport` relaxation), rather than being stamped
  from the target logbook.
- **`types.Qso` is unchanged** — `STATION_CALLSIGN`/`OWNER_CALLSIGN`/`OPERATOR`/
  `MY_NAME`/`MY_*` remain ADIF fields on the QSO; only their *source* moves. The
  "adding an ADIF field is a one-line change" invariant holds.
- Both SPAs need work (logbook-as-identity selector, operator picker, config
  split). This is a multi-commit feature, not a single change.

## Triggers to revisit

- **Per-operator location.** If an operator needs their own `MY_*` (true per-person
  portable/rove sharing one daemon), location may need to move onto the operator
  profile or the logbook — reopening the "full station profile" option.
- **Operator relations beyond counting.** If the roster needs per-op stats/awards
  that outgrow `GROUP BY` (a scoring engine, per-op QSL tracking), promote it from
  config to a DB table.
- **Second physical station on one daemon/DB.** If one install serves multiple QTHs
  (multi-shack), the "`MY_*` in config = one shack" assumption breaks and location
  moves to the logbook.
- **Gateway/relay callsigns.** If a client legitimately must supply an arbitrary
  `STATION_CALLSIGN` (a relay), the daemon-stamped-from-logbook rule needs an
  explicit escape hatch.

## Open design gaps (surfaced by review 2026-07-22)

The clean-room review of the first implementation commit flagged five gaps this
ADR under-specified. They must be resolved during implementation:

1. **The default-logbook delete guard must STAY** (do not blind-retire it in the
   "retires the guards" consequence). Beyond the callsign-orphan reason, it
   prevents `default_logbook_id` from pointing at a soft-deleted row →
   `logbook_not_found` on every submit, and a **stuck restart** (`ensureDefaultLogbook`
   can't re-insert `"Default"` because the soft-deleted row still holds the
   unique-name slot). Retire it only once deletion **atomically re-points or
   clears** the default.
2. **Forwarding is global — this likely GATES multi-callsign use.** Every live
   submit queues every enabled forwarder, but a QRZ key / ClubLog credential is
   bound to ONE remote callsign. QSOs logged under a *different* local logbook
   would upload to the **wrong remote account** or fail terminally. Forwarders
   need a per-logbook/callsign binding (or routing rule) before operating a
   second call is safe.
3. **Migration must preserve existing identity.** An upgrade with a set
   `OPERATOR`/`MY_NAME` must seed the roster + `default_operator` from them (the
   `SeedOperatorRoster` fill does this). When `owner_callsign` eventually moves to
   the logbook, the default logbook's owner must seed from the old
   `OWNER_CALLSIGN` (not silently default to the call) — moot while owner stays
   config-sourced (the "no schema change" decision), but a prerequisite of the
   owner-on-logbook follow-up.
4. **PSK Reporter identity switching is undefined** (confirmed by the c93da89b
   review). The uploader captures the RX callsign at startup for public reports +
   self-decode filtering and buffers spots without per-spot identity; switching
   the current logbook would report under the old call and could relabel buffered
   spots (`pskreporter.Service.SetReceiver` supports live changes but nothing
   calls it). Needs a flush / partitioned-buffer transition tied to the current
   logbook — or an explicit decision that PSK stays on the home call.
5. **FT8-exchange delete race.** A freshly-selected secondary logbook is empty
   until an exchange completes, so another tab could delete it mid-air (deletion
   only checks *stored* QSOs); completion then submits against a missing logbook
   and loses the contact. Pin the selected logbook (or reject deletion) while an
   FT8 session references it (related to gap 1).
6. **FT8 identity pinned at arm.** DONE for the two on-air paths, which ends the
   self-decode-filter review loop (4 rounds): the callsign is resolved ONCE at arm
   (the `/v1/ft8/qso/*` handlers, fail-closed) and carried on the exchange; the
   **self-decode filter** reads it via `Sequencer.ActiveCallsign()` (no per-slot DB
   lookup, no fallback, no cache — all that code deleted), and **TX** uses the same
   pinned exchange call. REMAINING (logging half): the QSO still logs to the
   CURRENT default logbook at completion — `qsoservice.Submit` derives
   `STATION_CALLSIGN` from whichever logbook the sink passes — so a mid-exchange
   logbook *switch* could still relabel the QSO. The fix is to pin the
   **logbook_id** at arm (thread it through `Start*` → exchange → `CompletedQso`,
   submit to it), not the callsign; a focused follow-up, and it also closes gap 5.

## References

- 2026-07-22 `internal/api` review — commits `e5da1945` (guards added) and
  `128b6e80` (deadlock fix); this ADR retires those guards.
- ADR 0049 — daemon-owned operating sessions (why "current operating session" state
  is avoided; multi-tab operating-lock).
- `docs/backlog.md` — "Per-logbook operating identity" (the deferred item this ADR
  specifies).
- Invariant: "`types.Qso` follows the ADIF specification"
  (`docs/v1-analysis/invariants.md`).
- Anchored files: `internal/types/logging_station.go`, `internal/config/config.go`,
  `internal/api/handler_qso.go`, `internal/api/handler_config.go`,
  `internal/qsoservice/submit.go`, `cmd/smd/main.go` (FT8 e4 sink).
