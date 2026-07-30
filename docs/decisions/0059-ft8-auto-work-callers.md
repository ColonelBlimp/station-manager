# 0059 — FT8: keep working callers after an operator-started QSO

- **Status:** Accepted
- **Date:** 2026-07-30
- **Supersedes / relates to:** extends ADR 0033 (caller-side sequencing) and
  ADR 0031 (manual send policy / auto-advance). Does not change either.

## Context

You answer someone's CQ. That contact completes, and now other stations are
calling you directly — `<yourCall> <theirCall> <grid>`. Today every one of them
needs a click: `StartWorkCaller` runs a single contact and returns to IDLE
(`work_sequencer.go`). The pile-up is visible in Band Activity and has to be
worked one manual pick at a time, which is the part that loses contacts.

Two pieces of machinery for this already exist and are not being rebuilt:

- **`pickAnswererLocked`** (`caller_sequencer.go`) already selects a station
  calling us from one slot's decodes, honouring `ft8.tx.caller_answer_mode`
  (`auto_first` / `auto_strongest`), skipping stations stalled at the repeat cap
  this round, and skipping compound/portable callers whose reply cannot be
  encoded.
- **A Call-CQ run** already works answerer after answerer from a single operator
  action until Abandon. That is the precedent this follows.

## Decision

A **run** that keeps working stations calling us, armed by an operator-started
QSO and continuing until Abandon. Gated by a new config knob, default OFF.

### The acceptance criterion

> When I have answered someone's CQ and stations then call me directly, SM works
> them one after another through the full ladder without my clicking each one,
> until I press Abandon — and at any moment I can tell whether the run is still
> **armed and waiting** (it will key the rig when the next caller appears) from
> **stopped**.

The third clause is the load-bearing one. An armed run with nobody calling looks
identical to nothing happening, and yet it will transmit. The operator must be
able to see which state the station is in without inferring it from silence.

### Operator's calls, 2026-07-30

1. **Armed only by an operator-started QSO** — answering a CQ, or picking a
   caller by hand. Never from idle.
2. **A QSO that ends without a completed exchange continues the run** (repeat cap,
   off-ramp, unencodable partner), matching the Call-CQ loop, which already keeps
   a stalled-calls list so one repeating station cannot starve the pile-up.
3. **Stops on:** Abandon, TX disarmed, rig disconnect / CAT lost, band or dial
   change.
4. **Duplicates:** unchanged from the Call-CQ loop's operator-ratified position
   (2026-07-26, `caller_sequencer.go`): no completed-call suppression. A partner
   who heard none of our RR73s and calls again genuinely never got the contact, so
   working them again is correct on-air behaviour; the defect in that case is the
   second log ROW, and it is fixed by duplicate detection at log level.

### Why this does not change the operator-initiated invariant

`internal/ft8/CLAUDE.md` states that every session is operator-initiated and the
daemon initiates none, citing the QEX FT8 specification's prohibition on
automatic operation and licence restrictions on unattended operation. **That
invariant is unchanged**, because decision 1 puts an operator action at the head
of every run: one action, then a sequence of contacts, until Abandon — exactly the
shape a Call-CQ run already has and ADR 0033 already accepted.

The rejected alternative is the one that would have crossed the line: arming from
idle, so that a station calling an otherwise-inactive daemon gets answered with no
operator action anywhere in the chain. That is daemon-initiated operation and is
out of scope here. If it is ever wanted it needs its own ADR amending the
invariant, not a config default.

SM still does not ENFORCE attendance, and this does not pretend to. A run
continues with nobody at the rig, as a Call-CQ run does. Remaining is the
operator's obligation under their licence.

## Consequences

- One more way for the daemon to key the rig without a per-contact click, so the
  armed/stopped distinction has to be visible, not implied.
- The knob defaults OFF: the feature transmits unattended, and a default that does
  that on upgrade would be wrong regardless of how useful it is.
- The four stop conditions mean the run holds no state that can silently resume.
  Dial and band changes reuse the session's existing pinned-dial invariant rather
  than adding a second dial-watching mechanism (the 2026-07-27 dial-guard arc
  settled where that check belongs: in the transmit path, not in a slot flag).
- Largely makes the FT8 pile-up drawer redundant for the auto modes; the drawer
  stays for `operator_pick`.
