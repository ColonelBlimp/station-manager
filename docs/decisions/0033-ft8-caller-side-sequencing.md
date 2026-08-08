---
number: 0033
title: FT8 caller-side sequencing — work the answerers, switchable selection
status: Accepted
date: 2026-06-12
---

# 0033 — FT8 caller-side sequencing — work the answerers, switchable selection

## Context

Answer-a-CQ shipped end-to-end (ADR 0029/0030/0031, e1–e4) and was proven on air on
2026-06-12 — a full DL9UW exchange through 73, logged. That flow is the *answering*
station: the operator clicks a decoded CQ, the daemon auto-advances the CQ→73 ladder
(ADR 0031), and a completed exchange logs (e4).

The reverse — *calling* CQ and working the stations that answer you — was deferred as
the "call-CQ caller-side scope." Its absence bit on the same day: a real 7Q (Malawi,
rare prefix) pile-up — DK8IF, DL9UW and others all calling `7Q5MLV` — was unworkable in
SM, because Call CQ is a single-shot button with no follow-through. The operator chose
caller-side as the next FT8 increment, to full logging, tested.

The pure resolver (`internal/ft8/sequence.go`, e2) already models the answerer ladder as
a deterministic per-contact state machine. The caller ladder is its mirror — and the
open question is *who picks which answerer to work* when several reply.

## Decision

Add a caller-side per-contact resolver (`CallerExchange` in `internal/ft8/caller.go`) —
the mirror of the answerer `Exchange`: once a station has answered our CQ, we send a
report, they roger with their report, we send RR73, and the QSO logs (reusing the e4
sink). Calling CQ and **selecting the answerer** are sequencer-level policy, switchable
by a daemon-config knob **`ft8.tx.caller_answer_mode: auto_first | operator_pick`**
(default `auto_first`). `auto_first` works the first valid answerer (WSJT-X "Auto Seq"
style); `operator_pick` queues answerers on a stack the operator pops (the pile-up
work-stack). Both are attended — the operator initiates by calling CQ, is present, and
can Abandon.

## Alternatives considered

### Single fixed mode (auto-only or manual-only)

Pick one behaviour and hardcode it. Rejected: the two are genuinely useful in different
conditions — `auto_first` for a quiet band (least clicking), `operator_pick` for a
pile-up where *whom to work* is the operator's call (the SM manual-sequencing principle,
ADR 0031). The modes share the entire ladder + sequencer; only answerer selection
differs, so a config knob is cheap and avoids forcing the choice.

### Embed answerer selection in the pure resolver

Let `CallerExchange` itself consume CQ answers and pick one. Rejected: "which of N
answerers, and (for `operator_pick`) await the operator's pop" is policy + UI state, not
a deterministic next-message lookup. Keeping it out preserves the resolver's purity and
testability (the property that made the answerer ladder solid), and lets the same
resolver serve both modes unchanged.

### Caller resolver includes the CQ-calling state

Model `CallerExchange` starting at a "calling CQ" rung. Rejected: a CQ is addressed to
*no one* — there is no `TheirCall` until someone answers — so a per-*contact* state
machine can't own it. The CQ repeat + answer collection lives in the sequencer;
`CallerExchange` begins once an answerer is chosen, with their call, grid, and our SNR of
their answer known.

### Await the partner's closing 73 before logging

Hold the QSO open until the answerer sends its final `<us> 73`. Rejected: the caller's
contact is complete when it transmits RR73 — both reports have been exchanged and
rogered. The closing 73 is courtesy and often not copied; waiting for it would strand
completed QSOs unlogged. WSJT-X logs the caller at RR73; we match.

## Consequences

- Two ladders coexist in the package — the answerer `Exchange` and the caller
  `CallerExchange` — sharing the message model (`parseMessage`, `formatReport`, the
  report fields). Deliberate duplication over a generic role-parameterised machine (the
  "build specific" lesson): each ladder reads as exactly its role.
- **Call CQ becomes a sequenced mode**: it must repeat the CQ until answered and then
  hand off to a `CallerExchange`, rather than the current single-shot send. The
  single-shot button's behaviour changes when this lands.
- A new config knob `ft8.tx.caller_answer_mode` (daemon `config.json`, served on
  `/v1/config`, Settings-tab toggle). `auto_first` ships first (no new UI);
  `operator_pick` adds the stack drawer as a follow-on increment — no resolver rework,
  since selection is sequencer-level.
- Logging is mostly free: a finished `CallerExchange` produces the same `CompletedQso`
  the e4 sink already logs; the new part is the caller-side report direction (we send the
  bare report, receive the R-report).
- Attended-only is preserved and unchanged: the operator initiates every session by
  calling CQ, supervises, and can Abandon; there is no auto-CQ cycle. This is the WSJT-X
  "Auto Seq after Call CQ" model, not unattended/robotic operation (still out of scope,
  QEX-forbidden).

## Triggers to revisit

- If `auto_first` works the "wrong" station in pile-ups often enough to annoy, flip the
  **default** to `operator_pick`.
- If a future requirement needs the daemon to *initiate* a CQ cycle without an operator
  present, this decision's attended framing breaks — but that is explicitly out of scope
  (QEX §9; see memory `project_sm_ft8_attended_only`), so the trigger is really "don't."
- If the answerer and caller ladders accrete enough shared transition logic that the
  duplication hurts, reconsider a single role-parameterised resolver behind a common
  interface.

## Amendment (2026-06-17) — "work a caller" entry point

Shipping the caller side surfaced a gap the original framing missed: stations call
`7Q5MLV` directly even when we are **not** calling CQ — e.g. tail-enders that pile on
right after we finish answering someone else's CQ (the `seqAnswering` flow). The
operator's `7Q5MLV PA3KUS JO21` scenario is exactly this: a directed-at-us opening
(`<ourCall> <theirCall> <grid>`) with no CQ of ours behind it. The Call-CQ session
(`auto_first`/`operator_pick`) does not cover it, because there is no CQ phase.

Added a third sequencer entry point, **`StartWorkCaller`** (mode `seqWorking`,
`internal/ft8/work_sequencer.go`; `POST /v1/ft8/qso/work`): the operator picks a
station calling us from the Band-Activity **pile-up** (directed-at-us decodes are
tinted live and clickable when armed + idle) and we run a `CallerExchange` against
*that* station. It reuses the caller ladder unchanged, but unlike Call-CQ it has **no
CQ phase** and on completion/off-ramp it goes **idle** rather than looping back to a
CQ (there is no standing CQ to resume). The report we send is our SNR of the picked
decode (the SPA passes `their_snr`). Attended throughout — the operator initiates each
contact by clicking it.

This is the foundation the pile-up stack sits on (below). Why not fold work-a-caller
into the answerer flow (`StartQso`)? Because the *roles* differ — a station that sent us
a grid has opened the exchange, so **we report first** (the caller ladder), whereas
`StartQso` answers a CQ by sending our grid first (the answerer ladder). Reusing
`CallerExchange` keeps each ladder true to its role (the "build specific" lesson), as the
original decision argued for the two-ladder split.

## Amendment (2026-06-17) — pile-up callsign stacking supersedes daemon `operator_pick`

The `operator_pick` answerer-selection was originally scoped as a **daemon** Call-CQ
mode (the sequencer collects answerers to our CQ and the operator pops one). On air the
operator instead asked for a simpler, more general shape that builds on work-a-caller:
an **SPA-owned FIFO** of stations calling you. **Ctrl/Cmd+click** a calling-you decode
to enqueue it (`ft8PileupStack.svelte.ts`); the Operate view **drains** the queue
oldest-first via `StartWorkCaller`, advancing as each contact completes, while the
operator keeps adding. Capture is available in **any state** (mid-QSO, disarmed — pure
capture, no TX), which is the point: callers are only visible in your RX parity and
work-now is gated on armed+idle, so you grab them when seen and the SPA works them when
it can. Drawer + Operate-tab depth badge; Abandon pauses the drain (queue kept, Resume);
Clear-all + per-entry remove. **SPA-only — the daemon is untouched** (reuses
work-a-caller + the `ft8-qso` idle signal); the queue is in-memory (erased on tab close,
like the Phone/CW `callsignStack`).

This **supersedes the daemon `caller_answer_mode: operator_pick` mode**, which stays
`501`-rejected at `StartCallCq` and is now unlikely to be built: the SPA stack delivers
operator-chosen working for *anyone* calling you (whether or not you called CQ), without
new daemon state/endpoints. `auto_first` Call-CQ remains as the hands-off "call CQ +
auto-work the answerers" loop. Attended throughout — the operator explicitly Ctrl+clicks
every station; the daemon only does the protocol sequencing it already does for
`auto_first`/work-a-caller (strictly *more* attended than `auto_first`, which the
operator never chose per-station).

## References

- ADR 0029 (FT8 transmit, manual-first), 0030 (PTT/slot controller), 0031 (manual send
  policy — auto-advance rungs; this extends it to the caller role), 0032 (TX timing).
- `internal/ft8/sequence.go` (answerer `Exchange`), `internal/ft8/caller.go` (new
  `CallerExchange`).
- `docs/backlog.md` — "FT8 caller-side sequencing — pile-up work-stack."
- Memory `project_sm_ft8_attended_only`.

## Dated note (2026-08-08) — the default flipped to operator_pick

The revisit trigger above fired, though on different grounds than anticipated:
the default `caller_answer_mode` is now `operator_pick` for LICENSING reasons
(automatic operation must be an explicit opt-in), operator-ratified
2026-08-08. Reasoning and consequences recorded in ADR 0065's dated note;
`auto_first`/`auto_strongest` behaviour is unchanged for operators who set
them.
