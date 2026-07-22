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
