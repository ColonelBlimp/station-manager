---
number: 0098
title: Best-effort report-driven band memory and a refuse-only restore for Phone/CW
status: Proposed
date: 2026-11-03
---

# 0098 — Best-effort report-driven band memory and a refuse-only restore for Phone/CW

**Proposed (2026-11-03). Not accepted; implementation and bench execution are on hold.** It records
the design that review of W-0012 slice 3 has reached (revision 4) and the alternatives weighed. The
acceptance criteria, rulings and review chronology live in
[W-0012](../work/W-0012-operator-experience-followups.md) ("Proposed slice — Phone/CW keeps its own
frequency and mode per band", slice 3). Endpoint, event and field names below are placeholders; the
outcomes are what is proposed.

## Context

Phone/CW is to remember its own frequency and mode per band, persistently, including hand-tunes
(W-0012 AC4), and return to its last position after FT8 under today's contract — both VFOs, the
selection and the mode (W-0012 Q2). Slices 1–2 built destination selection and a confirmed live
selection sequence (`frontend/app/src/lib/operate/bandSequence.svelte.ts`).

- **Attribution is best effort.** A CAT answer carries only command letters and parameters; the
  FTDX10 CAT Operation Reference Manual (`FTDX10_CAT_OM_ENG_2308-F`) says nothing about processing
  order, buffering or telling a solicited answer from Auto Information output. Whether a report
  reflects a hand-tune, a delayed command or an earlier state cannot be proven. Ruling 2026-11-03:
  report-driven memory with best-effort attribution is accepted; the possible result is a **stale or
  incorrect remembered position**, of unestablished frequency and size.
- **Write outcomes can be uncertain.** A 202 means written, not that the rig is at X
  (`internal/bridge/command.go`); 504 `rig_command_no_ack`, a generic `rig_command_failed`, a
  transport failure, or a CI-V batch rejected after earlier frames applied (`sendCommandsCIV`) leave
  effects unknown. `normalizeRigSend` (`frontend/app/src/lib/api/seams.ts`) maps all of them to
  "refused" today.
- **Activity is cross-page.** Writes and FT8 runs come from any page; `transmitting()` reads local
  `ft8State`, which only an FT view populates.
- **The return must be refuse-only** — it must never stop or disarm FT8. `handleRigCommand` stops TX
  before any `set_freq`/`set_freq_b`; `armTx` admits under `txMu` without `seqGate`, so neither a
  browser precheck nor `seqGate` excludes a concurrent arm; and the FT8 dial guard ends a session when
  the dial moves (`internal/ft8/sequencer.go`, `EndReasonDialMoved`), so a return write landing after
  a new arm would end that run.

**Constraint — ADR 0057.** It accepts anonymous CAT replies, rejects a marker-query barrier on the
unkey path, and admits no new TX-safety mechanism without an observed failure. This ADR does not
amend it. Nothing here sits on a keying or unkey path or gates a stop; memory correctness is not a
TX-safety mechanism. The restore reservation refuses *new* TX admission while a return is running
or unresolved: a **workflow exclusion** that keeps the return refuse-only, not RF protection
(operator review, 2026-11-03).

## Decision

Proposed: record band memory from the rig's own reports on a best-effort basis, excluding known
activity and incomplete operations; publish the two daemon signals that exclusion needs; treat
uncertain write outcomes as uncertain; and perform the return as one daemon operation under a
reservation.

1. **Recording rule (browser, Phone/CW view).** Record the rig's *reported* frequency and mode,
   under the band the reported frequency falls in, only when all hold: CAT connected and the selected
   VFO's frequency and mode confirmed by report; the Phone/CW view owns the rig; daemon TX activity
   idle (point 3); no known incomplete operation — this page's band sequence and return idle, and the
   daemon reports no rig write in flight and no restore active or in recovery (point 2). A commanded
   value is never recorded before the rig reports it. Startup and reconnect record nothing until both
   replayed signals arrive. Recording never keys TX; recall changes frequency and mode and keeps every
   existing guard and refusal rule.
2. **In-flight writes signal.** The daemon counts rig writes from entry to `SendCommands` (before
   `keyMu`) to return, and publishes the count on `/v1/rig/events`, replayed to late subscribers. The
   same signal marks a restore (point 5) incomplete for its whole life, active or in recovery, so a
   zero write count between its steps never lets a page record an intermediate position. A request
   not yet at the handler is invisible — a documented residual.
3. **TX activity signal.** The daemon publishes FT8 armed / session / in-flight and tune state as one
   `tx-activity` event on `/v1/rig/events`, replayed — not via the FT presence subscription.
4. **Outcome mapping.** Only a definite refusal before the write means "nothing changed". 504
   `rig_command_no_ack`, generic `rig_command_failed` and transport failures are *unknown*, and
   operator wording never claims no change for them.
5. **Restore operation and reservation.** The return is one daemon operation `restore {selected_vfo,
   vfo_a, vfo_b, mode}`. Under `seqGate → txMu` — the order `SealTxAdmission` uses — it refuses if
   FT8 is armed, in a session or in flight, or tune is up; otherwise it sets a reservation that every
   admission path checks (`armTx`, session starts, `TransmitNext`, `ClaimProfile`, bridge
   `StartTune`), refusing with "rig being restored", and releases the mutexes. Disarm, stop and
   unkey are never blocked. It writes without a TX stop; no mutex is held across the operation and no
   browser round trip is inside it.
   - **Sequential confirmation.** Steps run in order — VFO selection, VFO-A, VFO-B, mode — each
     issued only after the previous one is confirmed by report. Reports are observed before each step
     is dispatched, so a confirmation arriving during the write counts. Each issued step has **2 s
     after its write completes successfully** (as slice 2); unrelated reports never restart that
     deadline. Expiry abandons the remaining steps and enters recovery; it never releases the
     reservation.
   - **Confirmed** requires every step confirmed *and* the complete destination still matching the
     latest reported state at completion (as slice 2's final check), so a position change after an
     earlier step never produces false success or overwrites memory.
   - **Invalidated** — only when every issued step is confirmed, no write remains outstanding, and
     the complete destination no longer matches the latest reported state. The restore is abandoned
     without retrying, memory is left unchanged, and only this restore's reservation is released
     (ordinary TX guards remain). The operator sees "The rig position changed before the return
     completed." — never a claim that the operator moved the rig, since the mismatch alone does not
     establish its cause.
   - **Outcomes** are classified from the actual write and confirmation evidence, never from a
     rejection code alone (a CI-V step can span several frames; earlier frames may apply before the
     rejection):

     | Evidence | Result |
     |---|---|
     | Every step confirmed; complete destination still reported at completion | *Confirmed*; reservation released |
     | Every issued step confirmed; nothing outstanding; destination no longer matches the latest report | *Invalidated*; reservation released |
     | Nothing applied; definite refusal | *Failed*; reservation released |
     | Earlier steps confirmed; the rejected step definitely had no effect; nothing outstanding | *Failed with partial changes*; reservation released |
     | Any issued step's effect uncertain or unconfirmed | *Recovery* |

     A known partial failure (e.g. "frequency restored, mode refused", both established) reports those
     facts, preserves the previous memory, abandons the remaining steps and does not block arming.
   - **Recovery.** The reservation stays, owned by the daemon, until the rig reports the destination
     (the same best-effort report evidence the confirmation uses), the operator presses **Release**,
     or the daemon stops. No timer releases it. A persistent notice on every Operate view says the
     return is unconfirmed and that arming FT8 and tune are blocked.
   - **Release** clears only this restore's reservation. Settlement of the rig's position stays
     unknown, recording rules are unchanged, and the TX-alarm and readiness guards are untouched. A
     released restore is finished: no step resumes, and a later matching report does not revive it.
     The notice and log say "reservation released by the operator", never "rig confirmed".
   - **Scope for other writes.** While *active*, ordinary rig writes from any other page are refused
     (`not_written`) — interleaving would make the restore's confirmations meaningless. In
     *recovery* they are allowed, as the operator's recovery actions; TX admission stays refused.

## Alternatives considered

### Command ledger with reconciliation (earlier revision 3)

Per-client sequence numbers, a reconcile call with a void floor, acknowledgements and a
`sessionStorage` outstanding list, so the daemon could prove a request finished or would never
execute. Its purpose was strict attribution of reports. Not adopted once best-effort attribution was
accepted: the in-flight signal (point 2) covers the "incomplete operations" exclusion, and a delayed
write that executes later shows up as where the rig actually is.

### Guaranteed settlement by daemon readback (earlier revision 3)

After any write, read the operating state back and publish `rig-settlement`, gating report-driven
recording and the release of a recovering restore. Not adopted: the FTdx10 documentation gives no
query identity or ordering guarantee, a bench run could only report "no counterexample observed
under the recorded conditions", and switching Auto Information off around the readback was rejected
(FT8's frequency checks and dial-movement detection depend on the reports; the liveness probe's INIT
re-enables them; reports off still identify nothing). Recovery now releases on a matching report or
the operator's Release.

### Browser-only outstanding registry, "which ids did you receive?" query, expiry or presumed order

Per-page state misses other pages' writes; an "absent" answer can precede a delayed arrival; elapsed
time and arrival order prove nothing (operator ruling 2026-11-03). Rejected.

### Holding `seqGate`, or a browser precheck, for the return

`armTx` admits under `txMu` alone, browser state can be stale or another page's, and the daemon
stops TX before any frequency write. Rejected for the reservation.

### Memory from completed selections only

Save only destinations of completed selections or returns. Simpler, but hand-tunes are never
remembered (AC4 fails). Not adopted; available only through an explicit amendment to the hand-tuning
requirement.

### No live return

The return only shows the remembered band. Removes the reservation but contradicts the return
contract (W-0012 Q2); the reservation is a workflow exclusion, so ADR 0057 does not force this.
Available only through an explicit change to the return requirement.

## Consequences

- New daemon state and events: an in-flight write count (including restore lifetime), a
  `tx-activity` event, the restore operation with its reservation and recovery state, a Release
  endpoint, and a "rig being restored" refusal on every TX admission path.
  `docs/v2-design/api-endpoints.md` changes with them.
- The SPA's outcome mapping distinguishes refusal from uncertainty.
- The remembered position can be stale or incorrect; the operator sees it on recall and corrects it
  by tuning. This is a stated limitation, not a defect to be guarded further.
- A restore whose effects stay uncertain blocks FT8 arming and tune until the rig reports the
  destination or the operator releases it.
- No bench run gates this design; the bench plan in W-0012 is kept as a record only.

## Acceptance cases (for 3a)

- Recording refused while another tab's FT8 is armed or tune is up (signal replayed on subscribe),
  and while another page's write is in flight; resumes on a hand-tune once both are idle.
- A commanded value is never recorded before its report; nothing recorded after startup or
  reconnect until both replayed signals arrive.
- Recording refused between restore steps although the write count is zero, and throughout recovery.
- 504 and generic 500 shown as uncertain, never as "nothing changed".
- Restore refused when FT8 is armed, in a session or in flight, or tune is up — never stopping it.
- Arm, session start, manual send, profile claim and tune during an active or recovering restore
  refused with "rig being restored"; disarm, stop and unkey still work.
- Another page's write during an active restore: `not_written`; during recovery: allowed, TX
  admission still refused.
- Per-step deadline counted from write completion; a confirmation arriving during the write counts;
  an unrelated report does not restart it; expiry abandons remaining steps and enters recovery.
- All steps confirm, then a reported position changes before the final check: *invalidated*, not
  confirmed; no retry; memory unchanged; only this reservation released; the message is "The rig
  position changed before the return completed."
- Definite refusal of the first step: *failed*, released. Frequency confirmed then mode definitely
  refused: *failed with partial changes*, both facts reported, memory unchanged, arming not blocked.
  A CI-V multi-frame step rejected after an earlier frame: recovery.
- Recovery released by a matching report or by Release, never by a timer. After Release: only this
  reservation clears; recording rules unchanged; TX-alarm and readiness guards still refuse when
  their conditions hold; no step resumes; nothing says the rig position was confirmed.

## Triggers to revisit

- Observed stale or incorrect memory that operators find unacceptable: revisit attribution, or
  amend AC4 toward completed selections only.
- A second CAT protocol family or rig whose reports behave differently.
- An observed late-landing restore write after recovery was released.

## References

- [W-0012](../work/W-0012-operator-experience-followups.md) — rulings, AC1–AC7, slice 3 reviews,
  revision 4, bench plan (record only).
- [ADR 0057](0057-tx-safety-scope-cat-confirmation-is-detection-not-guarantee.md) — TX safety scope.
- [ADR 0071](0071-first-class-qso-archives.md) — archive activation and its TX admission seal
  (`SealTxAdmission`).
- `internal/api/handler_rig_command.go`, `internal/bridge/command.go`, `internal/ft8/servicetx.go`,
  `internal/ft8/sequencer.go`, `frontend/app/src/lib/api/seams.ts`,
  `frontend/app/src/lib/operate/bandSequence.svelte.ts`.
