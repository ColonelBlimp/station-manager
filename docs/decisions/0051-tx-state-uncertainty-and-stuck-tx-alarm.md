---
number: 0051
title: Model TX as idle/keyed/uncertain; alarm on uncertainty; defensive unkey on every confirmed connection
status: Accepted
date: 2026-07-18
---

# 0051 — Model TX as idle/keyed/uncertain; alarm on uncertainty; defensive unkey on every confirmed connection

## Context

The guaranteed-stop discipline (ADR 0027 tune, ADR 0030 FT8 TX, ADR 0042
teardown unkey) rests on an assumption the 2026-07-18 incident disproved
live: **that a successful serial write means the rig received the command.**
During a 30 m FT8 CQ run, the USB path's write endpoint stalled mid-
transmission (kernel: `urb stopped -32` on every write; the device stayed
enumerated). The rig was keyed by a TX1 that landed *before* the stall; the
unkey TX0 — and every backstop after it: the 18 s auto-off, the disarm on
CAT-drop, the teardown unkey — was accepted by the OS/driver, returned nil,
and never reached the radio. The bridge recorded `active=false`, cancelled
its timers, and logged a perfectly clean session while the rig transmitted a
dead carrier until the operator noticed and intervened manually.

The 2026-07-18 TX-safety review generalised the incident into three code
findings this ADR answers together, because they are one discipline change:

1. **(Critical)** Yaesu/Kenwood unkey success is fire-and-forget write
   acceptance (`writeKeyedLine` → `WriteCommandBytes`); the release paths
   treat nil as "carrier is down", clear state, and cancel the backstop.
   `clearTuneOnDisconnect`/`clearFt8TxOnDisconnect` further assume "the
   carrier physically drops with the rig" — false for a cable/hub/endpoint
   failure, which kills the *control path* while leaving the rig powered
   and keyed.
2. **(High)** Pipeline teardown bypasses `keyMu`: it unkeys, closes the
   client, and clears TX state without owning the TX-transition lock, so an
   in-flight key operation can interleave `teardown-TX0 → pending TX1 →
   close → state cleared` — a keyed rig with no software backstop. The
   teardown comment labels the racing late write "benign"; for TX1 it is
   the opposite.
3. **(High)** The stranded-TX marker (`strandedKeyed`) is in-memory only —
   a daemon restart forgets it — and the reconnect path clears it *before*
   the defensive TX0's outcome is known, an explicitly accepted ADR 0042
   residual this ADR supersedes.

The rig's own TX time-out timer was enabled and documented (manual, "Before
you transmit") the same day; it remains the independent final backstop, not
a substitute for the daemon telling the operator the truth.

## Decision

Three coupled changes to the bridge's TX discipline:

1. **TX state becomes a tristate — `idle` / `keyed` / `uncertain` — and only
   positive confirmation moves it to `idle`.** A write-accepted unkey moves
   `keyed → uncertain`, not to idle. Confirmation is protocol-appropriate:
   CI-V's awaited ACK confirms directly; for the ASCII fire-and-forget
   family, the rigdef gains an optional **`read_tx_status`** command (Yaesu:
   `TX;` → `TXn;`) issued after the unkey — a decoded RX answer confirms
   idle. A rig whose def has no status read stays `uncertain` briefly and
   falls to the alarm rule below only if liveness ALSO fails (the pragmatic
   floor: an alive, pushing rig whose unkey was written is overwhelmingly
   unkeyed).
2. **Uncertainty that persists alarms, loudly and durably.** Entering
   `uncertain` without confirmation within a bound, losing CAT liveness
   while `keyed`/`uncertain`, an unkey write error, or a teardown that
   cannot confirm its final unkey → ERROR log + a new **`tx-alarm` SSE
   event** (replay-cached like `bridge-error`, so a late-connecting SPA
   still sees it) that the SPA renders as a **persistent "CHECK YOUR RADIO —
   may still be transmitting" banner** until the operator dismisses it or a
   positive RX confirmation arrives. The FT8 service refuses to queue new
   transmissions while the alarm stands. Detection is cheap; the incident's
   real failure was silence.
3. **Teardown owns the TX transition, and every newly confirmed connection
   gets one unconditional defensive unkey.** Teardown takes `keyMu`, sets a
   closing latch that refuses new key attempts, waits out any in-flight key
   operation, sends TX0 as the final serialized wire action, and clears
   state while still owning the lock (the defensive reconnect unkey uses
   the same path). And `defensiveUnkeyIfStranded` + the `strandedKeyed`
   flag are **deleted** in favour of an unconditional defensive `tx_off` on
   every identity-confirmed connection: it needs no memory (so it survives
   daemon restarts, crashes, and power-cycle orderings the flag never
   could), costs one ~6-byte write per connect, and is a no-op on an idle
   rig. The H2 gate is preserved — it fires only after identity confirms.
   On the Yaesu family `TX0;` cancels CAT-initiated TX only; a manually
   keyed rig (mic PTT/footswitch, `TX2` state) is not unkeyed by it, so
   the defensive write cannot stomp an operator's manual transmission.

## Alternatives considered

### Keep write-success-as-confirmation (status quo)

Rejected by the incident: the failure mode is real, silent, and its cost is
an unbounded unattended carrier. The entire remaining discipline (backstops,
teardown unkey) inherits the same blind spot, so no amount of extra retries
through the same dead pipe helps.

### Alarm without the tristate model

A "liveness dropped recently after a key" heuristic could fire a warning
without restructuring TX state. Rejected: the alarm needs a truthful state
to key off, or it either misses cases (unkey written, link died a moment
earlier) or cries wolf (rig confirmed idle, liveness blips later). The
tristate is the *minimal* honest model — it adds exactly one state, and
`uncertain` is precisely "what the daemon actually knows."

### Persist `strandedKeyed` to disk

Solves the daemon-restart amnesia the review flagged, but keeps the flag
machinery, still misses crash-before-persist orderings, and adds a
file/DB write on the teardown path. Rejected in favour of the unconditional
defensive unkey, which is simpler, total, and stateless. This supersedes
ADR 0042's accepted residual ("a failed defensive write does not re-arm") —
0042 keeps its status; a note points here.

### Hardware TOT as the whole answer

Necessary, insufficient. It caps the damage at minutes of dead carrier,
depends on a menu setting the operator must make (most rigs ship with it
OFF), and tells the operator nothing. It stays what it is: the independent
final backstop, now documented as a pre-TX prerequisite in the manual.

### Priority mutex / bus arbiter for TX0 (review finding 5)

Deliberately NOT part of this ADR: the CI-V snapshot-blocks-TX0 issue is
real but orthogonal (a lock-scheduling problem, not a state-model problem),
Yaesu is unaffected, and the cheap skip-snapshots-while-keyed fix ships in
the companion batch. An arbiter is over-engineering until a real CI-V
station proves otherwise.

## Consequences

- `internal/bridge`: TX state fields become one tristate per path (tune /
  FT8 TX) with a confirmation step in the release flows; `keyMu`'s contract
  ("serialises every TX transition") becomes actually true — teardown and
  the defensive unkey go through it; `strandedKeyed` and
  `defensiveUnkeyIfStranded` are removed; a `tx-alarm` hub event with one
  replay slot joins `bridge-error`/`rig-disconnected`/`tune-state`.
- Rigdefs: `yaesu-ftdx10.json` + `yaesu-ft710.json` gain `read_tx_status`
  (`TX;`, response `TXn;` decode). Optional — a def without it degrades to
  the liveness-qualified rule, never blocks.
- SPA (frontend/app + the shipping logging SPA): a persistent dismissible
  alarm banner driven by `tx-alarm`; i18n code per ADR 0010.
- FT8 service: `TxReady`/queueing honours the alarm state (composes with
  the companion batch's strike-aware readiness predicate).
- Tests: the review measured `ft8TxAutoOff` at 0% and `tuneAutoOff` at 28.6%
  coverage — the backstop callbacks get direct tests with the rework, plus
  race tests for teardown-vs-key and the uncertainty transitions.
- ADR 0042 gains a pointer note (status unchanged; one residual superseded
  by this ADR). ADR 0027/0030's "guaranteed stop" language should be read
  through this ADR: the guarantee is now *confirm-or-alarm*, not
  *write-and-assume*.

## Triggers to revisit

- A rig family with neither an ACKing protocol nor a readable TX status —
  the liveness-qualified fallback would carry more weight; reconsider a
  mandatory status-read capability gate for TX features.
- Alarm fatigue: if the banner fires on flaky-but-recovering links often
  enough that the operator starts dismissing it reflexively, add a
  confirmation retry window before alarming (trade latency for precision).
- A second concurrent TX consumer (beyond tune + FT8) — the per-path
  tristate may want unifying into one owner object.

## References

- The 2026-07-18 incident: `docs/session-handoff.md` Session 224; kernel
  evidence + root-cause chain in the dogfood inbox P0 note.
- 2026-07-18 TX-safety review findings 1, 2, 4 (this ADR) and 3, 5, 6, 7
  (companion batch).
- ADR 0027 (tune guaranteed stop), ADR 0030 (FT8 TX), ADR 0042 (teardown
  unkey + the superseded residual), ADR 0010 (error codes / i18n).
- Code anchors: `internal/bridge/tune.go` (release/auto-off/disconnect),
  `internal/bridge/ft8tx.go` (same for FT8 TX + `TxReady`),
  `internal/bridge/pipeline.go` (teardown defer, `defensiveUnkeyIfStranded`),
  `internal/bridge/command.go` (`writeKeyedLine`).
