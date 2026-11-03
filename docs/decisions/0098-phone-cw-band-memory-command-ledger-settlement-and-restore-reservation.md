---
number: 0098
title: Command ledger, rig settlement and a restore reservation for Phone/CW band memory
status: Proposed
date: 2026-11-03
---

# 0098 — Command ledger, rig settlement and a restore reservation for Phone/CW band memory

**Proposed (2026-11-03). Not accepted; no implementation is authorised.** It records the design that
review of W-0012 slice 3 has reached, the alternatives weighed, and the evidence still missing. The
acceptance criteria, rulings and review history live in
[W-0012](../work/W-0012-operator-experience-followups.md) ("Proposed slice — Phone/CW keeps its own
frequency and mode per band"). Endpoint, field and code names below are placeholders; the outcomes
are what is proposed.

## Context

Phone/CW is to remember its own frequency and mode per band, persistently, and return to its last
position after FT8 (W-0012 rulings 1–6, AC1–AC7). Slices 1–2 built destination selection and a
confirmed live selection sequence. Slice 3 must decide what a remembered position is: a value the
rig reported while Phone/CW owned it, or a hand-tune — never the delayed result of an earlier
command. Review established that the browser cannot decide that alone:

- A rig write is confirmed only asynchronously ("202 means written, not the rig is at X";
  `internal/bridge/command.go`), and its outcome can be unknown: a browser timeout, a 504
  `rig_command_no_ack`, or a CI-V batch rejected after earlier frames applied (`sendCommandsCIV`).
- Acquiring `keyMu` in `SendCommands` is not cancellable, though the serial write loop checks
  cancellation before writing (`internal/serial/serial.go` `writeAll`): a request the browser gave up
  on may or may not execute.
- Writes and FT8 activity come from any page; a page's own state covers neither. `transmitting()`
  reads local `ft8State`, which only an FT view populates.
- The return to Phone/CW must keep today's contract (both VFOs, selection, mode) and be refuse-only:
  it must never stop or disarm FT8. But `handleRigCommand` stops TX before any `set_freq`/`set_freq_b`,
  and `armTx` admits under `txMu` without `seqGate`, so neither a browser precheck nor holding
  `seqGate` excludes a concurrent arm.

**Constraint — ADR 0057.** It accepts anonymous CAT replies, rejects a marker-query confirmation
barrier on the unkey path, and admits no new TX-safety mechanism without an observed failure. This
ADR does not amend it. Nothing proposed here sits on a keying or unkey path, gates a stop, or claims
to make TX safer; memory correctness must not become another TX-safety mechanism. The one point of
contact — the restore reservation refusing *new* TX admission while a return runs or awaits
settlement — is a **workflow exclusion**: it keeps the return refuse-only (an operator requirement
for the feature) and keeps an FT8 run from starting on a rig whose position the return has not
established. It does not protect against RF faults, and touching admission does not by itself make
it a TX-safety mechanism under ADR 0057 (operator review, 2026-11-03).

## Decision

Proposed: track every rig write in a daemon-side **command ledger** that can prove a request
finished or will never execute; publish **rig settlement** separately from request completion; let
Phone/CW record report-driven memory only while the daemon reports the rig settled, idle and free of
admitted writes; and perform the return as **one daemon operation under a restore reservation**.

1. **Command ledger.** The daemon has an instance id per process. A page registers a client bound to
   that instance and numbers its writes `seq` 1, 2, 3 …; every write carries `{instance, client,
   seq}`. A stale instance or unknown client is refused before any effect. A repeated `(client, seq)`
   returns the recorded status and never executes twice.
2. **Reconciliation with a void floor.** `reconcile(client, through n)` — atomic with admission of
   that client's writes — returns the recorded status of every seq ≤ n the daemon has admitted, and
   raises the client's floor to n so any seq ≤ n *never admitted* is void and refused on arrival.
   **Retention:** raising the floor never alters an admitted request. Terminal outcomes
   (`not_written`, `written`, `partial`, `unknown`) are immutable: a repeated reconciliation (for
   example after a lost response) returns the same terminal outcome for every request that had one,
   so `written` can never become `not_written`. A request admitted below the floor and still
   *pending* stays pending and may advance to its real terminal outcome between reconciliations; it
   never regresses. Status entries are released only by a separate acknowledgement (`ack through m`)
   the page sends after storing them, and an acknowledgement releases terminal entries only: it never
   discards a pending request's entry, and a released `(client, seq)` stays below the floor, so a
   duplicate of it is refused and never executes again. Until acknowledged, entries stay
   recoverable. A page keeps
   its outstanding list in `sessionStorage` and reconciles on any timeout or transport failure, on
   event-stream reconnect (missed outcome events), and after a reload — the previous page's client
   included. An evicted client is refused entirely, so eviction can never let a request execute.
3. **Outcomes.** `not_written` — proven zero effect: refused before any byte reached the wire, or
   void — is the only outcome meaning "nothing changed". `written` (all bytes, or every CI-V frame
   ACKed), `partial {applied, of}` and `unknown` (no ACK, I/O error, or cancellation once writing
   began) all leave effects unresolved. HTTP status codes are mapped through these, never read alone.
4. **Settlement, separate from completion.** After any write that may have had an effect the daemon
   marks the rig *unsettled* and reads the operating state back; it publishes `rig-settlement
   {settled, epoch}` on `/v1/rig/events`, replayed to late subscribers. **The evidence for this is
   unresolved** (below): the readback is valid only if a complete, fresh answer can be identified as
   reflecting every earlier effect. Settlement has exactly two consumers: report-driven memory
   recording (D5) and release of a restore reservation held in recovery (D6). Through the second it
   indirectly governs when FT8 arming and tune are admitted again after an unconfirmed return. It
   never gates keying of an admitted run, unkeying, or any stop.
5. **Recording.** Report-driven recording (every confirmed position, including hand-tunes) only while
   the daemon reports settled, no write admitted from any client, and authoritative TX activity
   (published on `/v1/rig/events`, not the FT presence subscription) idle. Otherwise only the
   confirmed destination of a completed selection or return, and only if settled at completion.
   Startup and reconnect stay restricted until both replayed states arrive.
6. **Restore operation and reservation.** The return is one daemon operation (`restore {selected_vfo,
   vfo_a, vfo_b, mode}`). Under `seqGate → txMu` — the order `SealTxAdmission` uses — it refuses if
   FT8 is armed, in a session or in flight, or tune is up; otherwise it sets a reservation that every
   admission path checks (`armTx`, session starts, `TransmitNext`, `ClaimProfile`, bridge
   `StartTune`), refusing with a distinct "rig being restored" error. Disarm, stop and unkey are never
   blocked. No mutex is held across the operation and no browser round trip is inside it.
   - **Two states.** *Active*: the operation writes and awaits each confirmation; it ends with a
     request outcome (`confirmed`, `failed`, `unknown`) returned to the caller. *Recovery*: entered
     when the operation ends with effects unresolved; the reservation stays, owned by the daemon (not
     the page), until settlement arrives — then it is released — or until the operator releases it
     explicitly. No timer releases it.
   - **Operator feedback in recovery:** a persistent notice on every Operate view — the return is
     unconfirmed; arming FT8 and tune are blocked until the rig state is confirmed — with a
     **Release** action, logged, and daemon stop also ends it.
   - **What Release does, and does not establish.** It clears only this restore's reservation, so TX
     admission falls back to its ordinary guards. It does not confirm the rig's position: settlement
     stays unknown until a readback establishes it, report-driven memory recording stays restricted
     (D5), and the existing TX-alarm and readiness guards are untouched. A released restore is
     finished — none of its remaining steps resume, and a later settlement does not revive it. The
     notice and the log say "reservation released by the operator", never "rig confirmed".
   - **Scope for other writes.** While *active*, ordinary rig writes from any other client or page
     (frequency, mode, VFO select, band ops) are refused as `not_written`: interleaved writes would
     make the restore's confirmations and settlement meaningless. While in *recovery*, ordinary writes
     are allowed — they are the operator's recovery actions — and TX admission stays refused.

## Alternatives considered

### Browser-only outstanding registry (revision 2)

Per page, so it misses other pages' writes; it treated request completion as settlement (selection
B succeeds, old A reports `written`, the registry empties, A's late report is saved over B). Rejected.

### "Which ids have you received?" query

Can answer "absent" before the delayed original arrives and executes. Rejected for the void floor,
which makes "absent" permanent.

### Time-based expiry, or clearing by presumed command order

Elapsed time does not prove an old command cannot land, and arrival order is an unverified
assumption. Rejected (operator ruling 2026-11-03).

### Holding `seqGate` for the return

`armTx` admits under `txMu` alone, so `seqGate` does not exclude an arm. Rejected for a reservation
checked on every admission path.

### Refuse-only return by browser precheck, or by omitting `band_selection`

Browser state can be stale or another page's, and the daemon stops TX before any frequency write
regardless of the flag — a return could disarm another page's FT8 run. Rejected.

### Memory from completed selections only (no report-driven recording)

Save only the destination of a selection or return that completed; never a report. Removes the
ledger, settlement and TX-activity machinery (D1–D5) and stands on a stated policy, in the spirit of
ADR 0057 and the project's preference for one simple guard over machinery against unobserved
failures. Costs: hand-tunes and knob changes are never remembered — W-0012 AC4's required case (hand
tune, confirm, reload, recall) fails — and memory drifts from where the operator actually operated.
**Not rejected**: it is the candidate if the settlement mechanism is rejected or its cost judged too
high — but adopting it requires an explicit change to the agreed hand-tuning requirement (AC4), not
just a failed bench result.

### No live return (return shows the remembered band; the operator selects it)

Removes the reservation (D6) and any contact with TX admission. Contradicts the ruling to keep
today's return contract (W-0012 Q2, 2026-11-03). Review (2026-11-03) treats the reservation as a
workflow exclusion, so ADR 0057 does not force this alternative; it remains available only through
an explicit change to the agreed return requirement.

### Marker-query barrier per write

The readback in D4 is close to the barrier ADR 0057 rejected — but for unkey. Here it serves memory
only, sits on no TX path, and its failure blocks nothing but report-driven recording. Whether it can
work at all is the unresolved evidence below.

## Unresolved evidence

The FTDX10 CAT Operation Reference Manual (Yaesu, `FTDX10_CAT_OM_ENG_2308-F`, 25 pages) shows set,
read and answer forms (page 4: set `FA014250000;`, read `FA;`, answer `FA014250000;`). It says nothing
about processing order, buffering, processing time, or telling a solicited answer from Auto
Information output, and an answer carries only the command letters and parameters — nothing ties it
to the query it answers. Ordinary readback is supported; the guarantee D4 needs — identifying a
complete, fresh response after every earlier effect has finished — is **not established** and stays
explicitly unverified. A non-transmitting bench plan is prepared in W-0012 for separate operator
approval. A bench run can reject the mechanism with one counterexample; it cannot establish the
missing protocol guarantee — the best it can report is "no counterexample observed under the
recorded conditions", and only for cases where the test can tell the solicited answer from Auto
Information output. Any measurement supports the tested model, firmware and conditions only, not
every supported rig; CI-V rigs, which ACK each frame, need their own argument.

## Consequences

- If accepted: new daemon state (instance id, client ledger, settlement flag, restore reservation),
  new `/v1/rig/events` payloads (`rig-command-outcome`, `rig-settlement`, `tx-activity`), new
  endpoints (client registration, reconcile, acknowledge, restore, release), and a refusal reason on
  every TX admission path. `docs/v2-design/api-endpoints.md` changes with them.
- Every rig write from the SPA carries ledger identity; tune is excepted (its controller owns its
  own outcome).
- A restore whose effects never settle blocks FT8 arming and tune until the operator releases it.
- Settlement depends on rig behaviour not yet evidenced; until it is, D4, D5 and D6's
  release-on-settlement cannot be accepted.

## Acceptance cases (for 3a)

- Delayed arrival after cancellation: the void floor refuses it; it never executes.
- Missed outcome event: reconcile recovers the recorded status.
- Lost reconcile response, then a second reconcile: every terminal outcome is unchanged; `written`
  never becomes `not_written`; a request that was pending may advance to its real terminal outcome,
  never regress.
- Acknowledgement covering a pending seq: the pending entry is kept and still reports its outcome; a
  duplicate of an acknowledged seq is refused and never executes again.
- `written` before the rig's report: unsettled until readback; no report-driven recording meanwhile.
- Arm, session start, manual send, profile claim and tune start during an active restore or in
  recovery: refused with "rig being restored"; disarm, stop and unkey still work.
- Another page's frequency, mode or VFO write during an active restore: `not_written`; during
  recovery: allowed, TX admission still refused.
- Settlement never arrives: the restore returns `unknown`; recovery notice on every Operate view;
  Release clears it and is logged; no timer clears it.
- Release in recovery: only this restore's reservation clears; settlement still reads unknown;
  report-driven recording stays restricted; the TX-alarm and readiness guards still refuse when
  their conditions hold; no remaining restore step runs, and a settlement arriving afterwards does
  not resume it; nothing operator-visible says the rig position was confirmed.
- Partial CI-V batch: `partial`, effects unresolved, never "nothing changed".

## Triggers to revisit

- The bench finds a counterexample, or cannot identify the solicited answer: the settlement
  mechanism (D4) is rejected. Adopting "memory from completed selections only" or "no live return"
  then needs an explicit change to the agreed hand-tuning or return requirement.
- A second CAT protocol family needs settlement: re-derive D4 per family.

## References

- [W-0012](../work/W-0012-operator-experience-followups.md) — rulings, AC1–AC7, slice 3 reviews.
- [ADR 0057](0057-tx-safety-scope-cat-confirmation-is-detection-not-guarantee.md) — TX safety scope.
- [ADR 0071](0071-first-class-qso-archives.md) — archive activation and its TX admission seal (`SealTxAdmission`).
- [ADR 0094](0094-retain-session-lists-per-archive-within-each-tab.md) — archive-scoped browser storage.
- `internal/api/handler_rig_command.go`, `internal/bridge/command.go`, `internal/ft8/servicetx.go`,
  `internal/serial/serial.go`, `frontend/app/src/lib/api/seams.ts`.
