---
number: 0032
title: FT8 transmit timing — synchronised timebase, truncate when late
status: Accepted
date: 2026-06-12
---

# 0032 — FT8 transmit timing — synchronised timebase, truncate when late

## Context

The answer-a-CQ sequencer (ADR 0031, shipped e3) replies to the worked station
in the slot opposite theirs. Its transmit path — `Service.seqTransmit` →
`TxController.TransmitNow` — plays the **full bare waveform shifted to "now"**:
when the decode of the partner's slot lands (~0.7–1.6 s into our slot), it keys
PTT and plays the entire ~12.96 s waveform from that moment. The sequencer gates
on the whole waveform still fitting inside the slot (`maxStartDt ≈ 1.74 s =
15 − 12.96 − 0.3`); past that it skips the rung and retries one cycle (+30 s)
later.

On-air validation surfaced that this feels wrong, and the QEX FT4/FT8 paper
(July/Aug 2020, *The FT4 and FT8 Communication Protocols*, Franke/Somerville/
Taylor) confirms it is off-spec. §7 + Figs 5/6 define the message sequence — and
SM's `Exchange` ladder (calling→reporting→confirming = Tx1/Tx3/Tx5) matches it
exactly, so the *sequence* is correct. The divergence is **timing**, which §8 +
Fig 9 specify:

- The nominal transmit start is **0.5 s after the interval boundary**, on a
  timebase **synchronised to the slot** (symbol 0 at boundary + 0.5 s); the reply
  goes in the interval immediately following the partner's, at the opposite parity.
- A late start is handled by **truncation, not shifting**: *"WSJT-X will skip the
  missed part of the transmission and start sending a correctly synchronised
  message for the remainder."* The receiver re-syncs on the Costas arrays
  (begin/middle/end) and decodes the partial.
- Late tolerance (Fig 9, SNR −10 dB): a reply started **up to ~5 s late still
  decodes with no AP, up to ~8 s late with "AP mycall"** — and AP-mycall always
  applies to the station we answered, since it knows its own call and expects
  replies addressed to it.

SM's full-waveform-shifted-late model decodes only because the receiver's DT
search absorbs a small (≲2 s) shift, and its `maxStartDt ≈ 1.74 s` full-fit gate
is far tighter than the spec's ~5–8 s tolerance — so a slow/dense slot needlessly
skips a rung. It is also the root of the answer-a-CQ first-rung delay (the opening
call has no `OnSlot` trigger until the next their-parity slot).

## Decision

Adopt the QEX timing model for the sequencer's transmit path. Transmissions ride
a **slot-synchronised timebase** with symbol 0 at boundary + 0.5 s. When the
decode (or the operator's click) lands after that nominal start, transmit the
**truncated synchronised waveform** — skip the symbols whose time has already
passed and emit the remainder in place — rather than shifting a full waveform to
start "now". The send guard becomes **"symbols remain to send within the late
window"** (bounded by the AP-mycall ~8 s tolerance, kept conservative), replacing
the `maxStartDt` full-waveform-fit gate.

## Alternatives considered

### (A — chosen) Synchronised timebase + truncate-when-late

Spec-correct, and strictly more robust: a reply lands at dt ≈ 0 when the decode is
timely, and degrades gracefully (truncated, still synchronised, still decodable)
when late — gaining the ~5–8 s late tolerance the receiver's AP affords us. It
also dissolves the answer-a-CQ first-rung problem: the opening call simply
transmits on the next opposite-parity boundary (truncated if the click was late),
with no special "fire immediately from StartQso" patch. Cost: `modulate` /
`EncodeToSlot` must support emitting a waveform aligned to a slot offset with the
elapsed head dropped, and `TransmitNow` becomes a slot-aware truncated send.

### (B) Keep the late-shift model, patch the two known bugs

Smaller (the first-rung-immediate + Abandon-stop fixes already in the backlog).
Rejected as the primary path because it leaves SM operating outside the protocol's
intended timing, keeps the tight 1.74 s gate (needless rung skips on slow slots),
and forfeits the AP-mycall late tolerance. The two patches are subsumed by (A) —
(A) fixes the first-rung case structurally — so they are not done separately.

### (C) Decode before the slot ends so replies hit +0.5 s exactly

Complementary, not a substitute. SM captures the full 15 s slot then decodes
(landing ~0.7 s into the next interval); WSJT-X decodes the ~12.64 s waveform
before the interval ends, so its reply can hit +0.5 s with dt ≈ 0. Decoding
SM's slot early (at ~13.2 s, before capture completes) would let replies start at
the nominal point with no truncation at all. Worth pursuing later, but a larger
capture/scheduler change and independent of (A); (A) is correct on its own and is
the prerequisite.

## Consequences

- **Spec-compliant timing** with the ~5–8 s (AP-mycall) late tolerance, so a slow
  or dense slot no longer skips a rung — it sends a truncated synchronised message
  that still decodes.
- **The answer-a-CQ first-rung delay is fixed structurally** — no `StartQso`
  immediate-fire patch needed; the opening call transmits on the next
  opposite-parity boundary, truncated if the click was late.
- `internal/ft8/modulate.go` / `EncodeToSlot` gain a slot-offset-aligned,
  head-truncated emission path; `TxController.TransmitNow` is replaced by (or
  becomes) a slot-aware truncated send; the sequencer's `maxStartDt` full-fit
  guard becomes a "symbols-remain within the late window" guard.
- The Abandon-doesn't-stop-TX bug is **independent** of this model and still needs
  its own fix (cancel the in-flight transmission without disarming).
- CLAUDE.md + `docs/ft8.md` timing prose (currently describing the shipped
  late-shift model) is updated when the code lands, not before — the prose is
  accurate for the shipped behaviour until then.

## Triggers to revisit

- **If we design our own sequencing/timing** (operator's stated future intent):
  any bespoke timing must stay interoperable with standard FT8 on the air — the
  Costas sync + 15 s cadence + 0.5 s nominal start are protocol, not SM choices.
  A genuinely new mode would need its own Costas arrays (per the QEX licence
  restriction on non-conforming streaming) and would not be "FT8".
- If SM moves to decoding before slot-capture completes (alternative C), the
  truncation path becomes a rarely-exercised edge case rather than the norm.

## References

- ADR 0029 (FT8 transmit, manual-first), ADR 0030 (PTT + slot controller), ADR
  0031 (manual sequencing, auto-advancing rungs) — this refines their timing.
- QEX, *The FT4 and FT8 Communication Protocols* (Jul/Aug 2020): §7 + Figs 5/6
  (message sequence), §8 + Fig 9 (transmit timing, late-start truncation, AP-mycall
  tolerance). Local copy: `~/Downloads/FT4_FT8_QEX.pdf`.
- `internal/ft8/sequencer.go` (`maxStartDt`, `OnSlot`), `internal/ft8/servicetx.go`
  (`seqTransmit`), `internal/ft8/txcontroller.go` (`TransmitNow`),
  `internal/ft8/modulate.go` (`Modulate`/`EncodeToSlot`).
- `docs/ft8.md` §2/§6 (timing); memory `project_sm_ft8_integration`.
