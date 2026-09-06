# W-0002 — Validate the FT8 reduced type-4 ladder on air

**Status:** Open — awaiting operator validation
**Selected:** 2026-08-18
**Outcome:** One operator-initiated contact with a real nonstandard station completes through the
reduced type-4 ladder and is logged without fabricated exchange data.
**Sighting:** 2026-09-05 — a real nonstandard station (VU24DX, hash-rendered `<VU24DX>`) was decoded on
the dogfood station under alpha.2, the trigger named for this item; the attempt stays operator-initiated.

`W-0002` is an immutable identity. Its status may change, while priority and ranked position live
only in [`docs/backlog.md`](../backlog.md).

## Why this item is still open

The implementation shipped in `488cfd8c` and has since received the ordinary FT8 sequencer
hardening. Both reduced paths exist in `internal/ft8`: answer a nonstandard station's CQ and work a
nonstandard caller. The available SPA path routes an operator-selected nonstandard station into the
answer flow and renders the reduced ladder.

The offline RF-safety gate is current and green. `TestType4_RoundTrip` proves every message used by
both ladders encodes, modulates, and decodes back to the same canonical text without transmitting.
[`docs/ft8.md`](../ft8.md) documents the shipped behavior, while
[`ADR 0048`](../decisions/0048-ft8-reduced-type4-compound-ladder.md) remains Proposed until a real
nonstandard station is worked end to end on air. Passing the offline test is therefore necessary
but does not close this validation item.

## Scope

This work item owns one validation outcome:

- with the operator's explicit agreement for that occasion, select a real nonstandard or compound
  station already heard on the band;
- observe the reduced type-4 exchange through completion;
- confirm the resulting QSO record contains the spelled partner call and only data actually known
  or measured by Station Manager;
- record enough evidence to decide whether ADR 0048 can move from Proposed to Accepted.

It does not authorize starting an FT8 session without the operator's action for that occasion, a
generic keyed test, or a transmission merely to manufacture a validation opportunity. It also does
not add the deferred SPA trigger for working an ambiguous nonstandard caller, type-4 Call CQ, hash
resolution, or a different ladder. A defect found during validation returns to a separately scoped
TDD change before another on-air attempt.

## Operator-observable acceptance criteria

1. The operator explicitly initiates the attempt from a decoded real nonstandard station; Station
   Manager never starts the validation transmission independently.
2. The active QSO surface identifies the session as type-4 and shows the reduced ladder rather than
   the standard grid/report ladder.
3. A qualifying `RR73` from the selected spelled partner advances the answer flow, and the existing
   diagnostic record confirms the final courtesy `73` transmitted. The nearest confusable
   outcome—transmitting the opening but receiving no qualifying reply—does not count as a completed
   validation.
4. Exactly one QSO is logged after completion, with the spelled partner call, measured SNR in
   `RST_SENT`, blank `RST_RCVD`, and no fabricated grid or signal report.
5. An attempt abandoned before the selected partner's qualifying `RR73` does not create a completed
   QSO. A final courtesy-`73` failure after that `RR73` follows the existing final-rung policy and
   may still log the contact, but it does not satisfy criterion 3. A far station that cannot resolve
   the hashed standard call is an expected protocol limitation, not evidence that the ladder passed
   or failed incorrectly.
6. The normal guaranteed-stop controller and operator-visible FT8 safety state remain in force; no
   validation-specific keying or teardown path is introduced.

## Validation procedure boundary

Before the on-air attempt, rerun the focused offline round-trip test and confirm the ordinary FT8
session preconditions. During the attempt, capture the UTC time, dial frequency, selected callsign,
and decoded/transmitted ladder messages from the existing UI or diagnostic record. Afterwards,
inspect the stored QSO at the normal operator surface and compare it with criterion 4.

No automated test command, coding agent, or documentation task may initiate this procedure. The
operator chooses the occasion and gives explicit agreement immediately before RF is used.

## Close condition

Close this item and move the dossier to `docs/archive/work/` only after one contact satisfies all
six criteria and ADR 0048 records the acceptance evidence. An unavailable station, no reply, or a
remote hash-resolution failure leaves the item open without implying a Station Manager defect.

## References

- [`docs/backlog.md`](../backlog.md) — authoritative ranking.
- [`docs/ft8.md`](../ft8.md) — canonical current type-4 behavior and operator flow.
- [`ADR 0048`](../decisions/0048-ft8-reduced-type4-compound-ladder.md) — protocol constraints,
  selected ladder, alternatives, and acceptance trigger.
- [`internal/ft8/AGENTS.md`](../../internal/ft8/AGENTS.md) — FT8 transmit and validation rules.
- [`internal/ft8/type4_roundtrip_test.go`](../../internal/ft8/type4_roundtrip_test.go) — zero-RF
  encode/modulate/decode gate.
