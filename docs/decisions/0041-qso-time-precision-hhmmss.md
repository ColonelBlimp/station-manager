---
number: 0041
title: QSO times store + export native HH:MM:SS precision; dedupe stays minute-precision
status: Accepted
date: 2026-07-01
---

# 0041 — QSO time precision (HH:MM:SS)

## Context

The `internal/utils` review (2026-06-19) found that the time helpers accept ADIF
`HHMMSS` but the sqlite schema's `time_on`/`time_off` CHECK required exactly 4
digits, so a submitted `HHMMSS` failed to store. The decision then was **M2 →
truncate accepted `HHMMSS` to `HHMM`** before it reached sqlite — the least
disruptive fix given the schema, the FT8 path (which emitted `HHMM`), and the
frontend contract (SPA used `HHMM`).

That calculus was overturned by a lived data loss. The operator's QSL manager —
Tim Beaumont (M0URX, United Radio QSL Bureau / OQRS) — is fed by SM
**session-email ADIFs**, and his OQRS dedupe matches on the **full timestamp
including seconds**. Meanwhile **QRZ stores only `HHMM` and strips seconds**, so a
QRZ round-trip into SM destroyed the local seconds that older SM exports (and
Tim) still had. Truncating to `HHMM` was throwing away data a real downstream
needs. Phone/CW contest runs (two or three contacts in one minute) and FT8 (real
15 s slot seconds) also want the precision. v0/v1 SM stored `HHMMSS`, so keeping
it *restores* prior behaviour rather than inventing new scope.

## Decision

Store and export `TIME_ON`/`TIME_OFF` at **native ADIF precision — `HHMM` or
`HHMMSS`** (migration `0003_allow_time_seconds` relaxes the CHECK to 4- or
6-digit). **Dedupe stays minute-precision.** **Display stays `HH:MM`.** These
three rules are coupled and must stay coupled.

## Alternatives considered

### Truncate to `HHMM` on store (the prior 2026-06-19 decision)

Simplest against the old schema, and it kept storage/FT8/SPA all at one width.
Lost because it silently discards seconds that M0URX's OQRS matches on and that
same-minute contest QSOs need — a real, lived data loss, not a hypothetical.

### Store seconds *and* dedupe on seconds

Rejected on two grounds. (1) It would invalidate every existing `HHMM` dedupe key
(all 4825 historical QSOs are `HHMM`). (2) A seconds-stripped **QRZ re-import**
would then hash *differently* from the stored `HHMMSS` original, so it would NOT
be caught as a duplicate — and would overwrite the good seconds with `…00`. Keeping
dedupe at minute precision means the re-import hashes identically → caught as a dup
→ the stored seconds are never clobbered. So `ComputeDedupeKey` is fed
`utils.TimeToHHMM(timeOn)` (that helper was repurposed from a storage-truncator to
the dedupe/coherence normaliser).

### Show seconds in the SPA time inputs

Rejected as clutter. `<input type="time">` needs `step=1` to expose a seconds
field; the operator types `HH:MM` and seconds are captured invisibly
(`qsoDraft.timeOnFull` pins the real start second; `submitTimeOff` captures the
submit second; `reconcileSeconds` emits `:00` only if the minute is hand-edited).

## Consequences

- Local log, session-email ADIF, and ClubLog keep full seconds; the Go ADIF
  encoder derives field width from the value, so `HHMMSS` exports as `<TIME_ON:6>`.
- **Forwarding to QRZ still loses seconds** — that's QRZ's behaviour, unavoidable.
  Corollary rule: **never reconcile M0URX from a QRZ export**; feed him from SM
  session ADIFs.
- Existing `HHMM` dedupe keys stay valid; a QRZ re-import can't overwrite stored
  seconds. Overnight time-coherence comparison also uses minute precision (avoids a
  lexical mis-compare of unequal-width strings).
- The edit path preserves seconds: a same-minute `HHMM` edit keeps the stored
  seconds via the server-side `preserveSeconds` guard in `update.go`.
- The `/v1/logbook/{id}/qso` `after` cursor's `time_on` component is now
  variable-width (`HHMM` or `HHMMSS`); ordering stays consistent because a bare
  `HHMM` sorts before same-minute `HHMMSS` lexically.
- The 4825 historical QSOs remain `HHMM` and are unrecoverable (QRZ never had
  their seconds; even old v1 exports were re-seeded from QRZ). Tim accepted those.
  This concrete loss is a driver for durable off-site backup (ADR 0040).

## Triggers to revisit

- **A downstream needs second-precision *dedupe*** (not just storage) → revisit the
  minute-precision key, but only with a migration plan for existing keys + the
  QRZ-re-import overwrite hazard above.
- **QRZ ever preserves seconds** → the "never reconcile M0URX from a QRZ export"
  corollary can relax.
- **The SPA gains a genuine need to show/enter seconds** → revisit the `HH:MM`-only
  input decision (add `step=1`).

## References

- Memory `project_sm_time_precision` (the operating rules), `project_sm_online_db_community`
  (the durable-backup driver this loss motivates), ADR 0040.
- `docs/reviews/archive/internal-utils-2026-06-19.md` — the review whose M2
  truncate-to-`HHMM` decision this ADR reverses.
- Migration `internal/database/sqlite/migrations/log/0003_allow_time_seconds.{up,down}.sql`;
  `internal/qsoservice/{submit,update}.go`, `internal/ft8/qsolog.go`, `internal/utils/date_time.go`.
