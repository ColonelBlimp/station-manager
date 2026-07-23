# 0057 — TX safety scope: CAT confirmation is detection, the rig's TOT is the guarantee

- **Status:** Accepted
- **Date:** 2026-07-23
- **Supersedes / amends:** none (scopes ADR 0051, does not replace it)

## Context

Three stuck-transmit incidents in six days, each a different mechanism:

| Date | Mechanism | Evidence |
|---|---|---|
| 2026-07-18 | USB write-endpoint stall — every `TX0;` accepted into a dead pipe | kernel `urb` / EPROTO errors |
| 2026-07-21 | 30 m, CAT electrically healthy, rig ignored the unkey | kernel log CLEAN; RFI suspected |
| 2026-07-23 | 20 m / 30 W, 2-second tune, rig answered `TX1;` after `TX0;` | CAT healthy throughout — it both raised and cleared the alarm |

Each incident produced a round of hardening, and the hardening began producing
its own defects. In the seven days to 2026-07-23 the subsystem took **12
commits**; on 2026-07-23 alone, **four consecutive clean-room reviews each found
a real defect in the previous fix**, and one fix (per-cycle reply counting) was
net-harmful — it could deny recovery on a healthy rig after a reconnect — and was
reverted. `internal/bridge`'s TX paths reached **28 state fields across ~1,700
lines**.

The operator's judgement, and the trigger for this ADR: the protection had
"turned into slop."

The decisive observation is what actually happened on 2026-07-23. None of the
accumulated machinery prevented or ended the stuck carrier. It ran for two
minutes and stopped when the operator **switched the radio off**. What did work
was three simple, long-standing guards.

## Decision

**CAT-level confirmation is best-effort DETECTION. The rig's own TX time-out
timer is the guarantee.** We stop engineering the former as though it were the
latter.

Concretely:

1. **The rig TOT is a documented operator prerequisite for TX features**, not a
   nice-to-have. It is the only stop that survives a dead wire, a rig ignoring
   CAT, and RFI on the control path — all three observed.
2. **Keep the three guards that earned their place** (all proven in the
   2026-07-23 incident):
   - skip the power/mode restore unless positively confirmed idle — this stopped
     a full-power write into a possibly-keyed rig;
   - refuse to key while a prior transmission is unconfirmed — this refused
     three re-tune attempts;
   - raise a persistent operator alarm.
3. **Keep the two simple 2026-07-23 additions**, both of which address observed
   failures rather than theoretical ones: the bounded alarm re-probe loop (the
   alarm could previously latch itself out of every clear path) and the re-unkey
   retry on a positive "still transmitting" answer (re-asserting the stop is the
   correct response to that evidence; a banner alone is half a reaction).
4. **ACCEPT the anonymous-reply hazard. Do not build the barrier.** TXSTATUS
   frames carry nothing tying them to the query they answer, and the stream mixes
   solicited answers with unsolicited pushes, so a delayed reply can in principle
   confirm a later unkey. Closing it needs a marker-query barrier on **every
   unkey** — an extra CAT frame on the most safety-critical path, with an
   unvalidated interaction with the FTdx10's TX→RX transition tail (it is already
   known to drop commands there, which is why the tune restore needs a settle
   delay). The hazard needs a deep conjunction: a reply delayed by seconds, the
   alarm cleared in between, a new transmission keyed, AND its unkey also failing.
   Marginal risk reduction, real added risk, four review rounds of churn already
   spent. Not worth it.
5. **No new TX-safety mechanism without an observed failure.** Layers get added
   in response to something that happened on the air, not to something a review
   can construct.

## Consequences

- The scaffolding for the barrier (`read_identity` in the two Yaesu rigdefs and
  the `readIdentityCommand` constant) is **removed** rather than left dead.
- Clean-room reviews have no memory and will re-raise the anonymous-reply finding
  every round — two of the four rounds on 2026-07-23 were exactly that. **This
  ADR is the standing answer**: cite it and move on. That is the point of writing
  it down rather than leaving the decision in a code comment.
- We accept that a stuck carrier may need operator intervention. The daemon's job
  is to detect it, refuse to make it worse, keep re-asserting the stop, and tell
  the operator loudly. It is not to guarantee the rig obeys — it cannot.
- If a **fourth** distinct stuck-TX mechanism appears, the response is to revisit
  this scope decision deliberately, not to add another layer reflexively.

## Alternatives considered

- **Finish the barrier.** Rejected per point 4 — see the reverted counting attempt
  for how this class of fix has behaved in practice.
- **Deeper cut** (drop the re-probe loop and re-unkey retry too). Rejected: both
  are simple, both target observed failures, and dropping the re-probe restores
  the alarm latch that stranded the operator for 13 minutes on 2026-07-21.
- **A hardware PTT line (RTS/DTR) as the unkey path.** Genuinely more RFI-robust
  than CAT `TX0;`, which two incidents have now undermined. Not rejected — parked
  as the one avenue worth exploring if incidents continue, because it changes the
  guarantee rather than adding another detection layer.
