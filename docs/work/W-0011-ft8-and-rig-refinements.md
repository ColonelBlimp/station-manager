# W-0011 — Refine operator-initiated FT8 and rig behavior

**Status:** Deferred — choose a recognized operator problem, not the whole inventory
**Selected:** Not selected
**Outcome:** Selected FT8 or rig refinements improve a concrete operating workflow while preserving
single-flight keying, guaranteed stop, and operator-initiated session boundaries.

## Current inventory

- **FT8 sequencing/UI:** type-4 free text; reachable type-4 work-a-caller after the hashed-callsign
  decision; callsign ignore list; unanswered Call-CQ feedback; a layer-2 recent-answerer pool; clean
  next-slot work opening; attempt-limit Settings control; and the still-open arbitrary-click offset
  snap decision.
- **Band Activity defect:** stale decode fading must be visually distinct from the worked mute. The
  2026-08-10 report was diagnosed as a visual collision, not a false worked state; implementation
  awaits an operator-chosen presentation.
- **Power/occupancy:** per-band generated-waveform attenuation, tune-carrier occupancy treatment
  after passive/hardware evidence, and an optional paired ALC-ceiling/PO-collapse overdrive signal
  after thresholds and presentation are chosen.
- **Rig state:** one owner for commanded-but-not-yet-reported rig position; no timeout substitute for
  per-field report sequence. Characterize every frequency/mode/VFO path before restructuring.
- **Safety-adjacent deferred evidence:** rig TOT surfacing/clamp, FT-710 meter-selector verification,
  meter-tail semantics, output-sink logging, playback reopen after a reproduced collapse, and
  persistent TX-state escalation only after an operator duration threshold.
- **Later operating aids:** auto band-hop, semi-auto watch list, occupancy waterfall, CAT poll mode,
  `MY_RIG` from the connected rig, and one source for frequency-to-band data distinct from future
  regional band-plan policy.

## Exclusions and gates

- [W-0002](W-0002-ft8-type4-on-air-validation.md) owns reduced type-4 on-air validation.
- Field Day UI remains blocked until the relevant contest; daemon-initiated sequencing remains out
  of scope.
- No keyed, tune, rig-command, or hardware test occurs without operator agreement for that occasion.
- The six-item FT8 cluster was parked by the operator on 2026-07-31; do not present it as a target
  list unless the operator names a specific pain point.

## References

- [`docs/ft8.md`](../ft8.md) — canonical current FT8 behavior.
- [`ADR 0027`](../decisions/0027-tune-carrier-control.md)
- [`ADR 0057`](../decisions/0057-tx-safety-scope-cat-confirmation-is-detection-not-guarantee.md)
- Expanded historical evidence: `d0391ed7:docs/backlog.md` and
  `d0391ed7:docs/dogfood-inbox.md`.
