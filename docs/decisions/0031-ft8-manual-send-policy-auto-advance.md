---
number: 0031
title: FT8 manual sequencing — operator-initiated, auto-advancing rungs
status: Accepted
date: 2026-06-10
---

# 0031 — FT8 manual sequencing — operator-initiated, auto-advancing rungs

## Context

FT8 transmit is being built manual-first (ADR 0029). Two of its layers have
shipped: the pure next-message resolver (e2, `internal/ft8/sequence.go`) and the
daemon TX path + the SPA Ladder-tab Arm/Call-CQ UX (e1). The next layer, e3, is
the **manual sequencer** — the UX that walks a contact through the CQ→73 ladder —
and its whole interaction model depends on one question ADR 0029 left open.

ADR 0029 originally framed manual sequencing as "the operator advances each rung
of the CQ→73 ladder." The step-(e) design then *proposed* a refinement and flagged
it explicitly unresolved: the operator's judgement is **whom to work**, not the
mechanics of advancing, so the rungs should **auto-advance** once a QSO is
initiated. This ADR resolves that tension.

Two facts shape the answer. First, the e2 resolver proved that rung advance is a
**deterministic lookup** — given the QSO state and the latest decode (with its
SNR), the next message is mechanically determined; there is no judgement to make
in moving from one rung to the next, only in choosing whom to call and whether to
give up. Second, FT8's **15-second slot cadence**: a per-rung manual confirmation
forces the operator to click within each ~13 s window or miss the slot entirely.

## Decision

Manual sequencing is **operator-initiated and auto-advancing**. The operator's
two acts of judgement are: **whom to work** (click a highlighted CQ row in Band
Activity) and **arm TX**. After that the daemon walks the ladder itself via the
e2 resolver — transmitting each rung on its slot, advancing on the matching
decode — and logs the completed exchange (e4). The operator intervenes only to
**retry** or **abandon**. Per-rung confirmation is **not** required; the Arm-TX
gate (e1) is the deliberate-consent point for RF.

This makes manual a **strict subset** of the deferred auto-sequence: the e2
resolver and auto-advance-within-a-QSO are shared by both; the *only* difference
is initiation — **manual** = the operator clicks each QSO into being; **auto**
(a later ADR) = the daemon also picks whom to work and initiates.

Auto-advance carries required off-ramps / watchdogs (the answer-a-CQ machine
already specifies these):

- **Stop after N unanswered repeats** (config) — the opening call repeats until
  answered or the cap is hit, then returns to idle.
- **Never auto-start a fresh CQ cycle** — completion, abandon, or timeout returns
  to idle; a new contact requires a new click.
- **Abort on operator action** — Abandon (or Disarm TX) drops the exchange and PTT.
- **Never switch targets automatically** — if the worked station answers someone
  else, stay in Calling (repeat or abandon), don't re-aim.
- **Advance only on a decode parsing `<ourCall> <theirCall> <token>`** from the
  worked station (the e2 `Advance` rule).

## Alternatives considered

### B — Strict per-rung confirm

The resolver pre-fills each next message; the operator clicks "send" for every
rung before it is queued. Rejected: the 15 s cadence makes this frantic and
miss-prone (a missed click = a missed slot), and it largely defeats the purpose
of a sequencer. The "no RF without a deliberate click" safety it offers is already
provided by the Arm-TX gate plus the daemon's guaranteed stop (controller
deferred-unkey + bridge auto-off + single-flight) — so per-rung clicking adds
friction without adding real safety.

### C — Configurable (default auto-advance, per-rung as a toggle)

Most flexible, but it introduces a new send-policy knob and two interaction paths
to build and maintain from day one. Deferred, not rejected outright: a "hold /
confirm each rung" toggle can be added later as a *layer* on top of A without
reversing this decision, if auto-advance proves too autonomous in field use.

## Consequences

- **e3 sequencer state lives daemon-side.** The live QSO/sequencer state and the
  decode→advance loop run in the daemon (ADR 0004 — daemon owns orchestration and
  shared state; auto-sequence will need it there; QSO completion is daemon-side).
  The SPA is a thin sequencer view: it shows the current rung / next message and
  offers Abandon (+ retry); initiation is clicking a highlighted CQ row. The
  Ladder tab's Call-CQ-only placeholder (e1) is replaced by this.
- **New daemon surface:** a sequencer that consumes the FT8 decode stream, drives
  `TransmitNext` per slot through the e1 path, applies the off-ramps, and detects
  completion → builds a `types.Qso` → submits via `qsoservice` (e4). Direction
  stays one-way: `internal/ft8` imports `qsoservice`, never the reverse, so
  narrow-daemon-scope (ADR 0013) still holds by import graph.
- **Watchdog tunables** (N unanswered repeats, etc.) land under `ft8.tx` in the
  single `config.json` (per the one-config-file rule), with code-constant defaults.
- **Auto-sequence becomes a clean follow-on** — daemon-initiated calling on top of
  the same machinery — rather than a reopening of this decision.
- **The trust surface is bounded:** one click + armed TX yields one auto-completing
  exchange (or a timeout), not open-ended autonomy; the per-slot guaranteed stop
  still applies to every transmission.

## Triggers to revisit

- If auto-advance feels too autonomous in real operating, add a per-rung
  **hold/confirm** toggle (Option C as a layer — not a reversal).
- If the N-unanswered-repeats / timeout defaults prove wrong in practice, tune the
  `ft8.tx` watchdog config (no design change).
- If a second operating position / contest topology lands (the N-writers model),
  TX arbitration across stations and unattended auto-sequencing become pressing —
  the single-position manual model needs revisiting (shared with ADR 0029).

## References

- ADR 0029 — FT8 transmit, manual-sequenced first (the parent; this resolves its
  open send-policy question).
- ADR 0030 — PTT + slot-timing controller (the TX mechanism each rung drives).
- ADR 0027 — tune-carrier control / daemon-owned guaranteed stop (reused by TX).
- ADR 0004 — daemon owns orchestration + shared state (why the sequencer is
  daemon-side).
- ADR 0013 — narrow daemon scope / import-graph enforcement (`internal/ft8` →
  `qsoservice`, never the reverse).
- `internal/ft8/sequence.go` — the e2 `Exchange` resolver this auto-advances.
- `docs/ft8.md` §5 — step-(e) design + the answer-a-CQ state machine.
