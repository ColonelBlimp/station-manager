# internal/ft8 Code Review - 2026-06-14

> **Resolution (2026-06-14): all six findings fixed.**
> - **H1** — the final-rung completion now fires from the transmit goroutine
>   only on a true on-air success: `startTransmission`/`seqTransmit` take an
>   `onDone(ok bool)` callback (ok=false on cancel/error), and the sequencer logs
>   the QSO inside it instead of inline after "queued." A synchronous final-rung
>   error no longer completes — `ErrTxNotArmed`/`ErrTxBadMessage` are terminal
>   (abandon), anything else (e.g. `ErrTxInFlight`) skips the slot. Both
>   answerer (`sequencer.go`) and caller (`caller_sequencer.go`) paths. Tests:
>   `TestSequencer_FinalRungAsyncFailDoesNotLog`,
>   `TestSequencer_FinalRungBadMessageAbandonsNoLog`,
>   `TestCallerSequencer_FinalRR73AsyncFailDoesNotLog`.
> - **H2** — `StartCallCq` rejects `operator_pick` up front with
>   `ErrCallerAnswerModeUnsupported` → 501 `ft8_caller_mode_unsupported` (no more
>   silent auto-pick). Test: `TestStartCallCq_RejectsOperatorPick`.
> - **M1** — `StartQso`/`StartCallCq` validate the opening message
>   (`goft8.EncodeStandardMessage`) before committing/publishing; an unencodable
>   call returns `ErrTxBadMessage` → 400 `ft8_tx_bad_message`. Tests:
>   `TestSequencer_StartQsoRejectsUnencodableCall`,
>   `TestCallerSequencer_StartCallCqRejectsUnencodableCq`.
> - **M2** — the scheduler skips a target it was delayed past (resyncs at the
>   next future boundary) and stamps `OffsetMs` from actual service time. Test:
>   `TestStaleTarget`.
> - **M3** — a `seqGate` mutex makes the armed-check + sequencer commit atomic
>   w.r.t. disarm; disarm clears `txArmed` (under `txMu`) before abandoning.
>   Test: `TestService_StartDisarmRace` (passes under `-race`).
> - **L1** — `doc.go` rewritten to describe the current RX/TX/sequencer shape,
>   the TxKeyer seam, attended-only model, and the QSO-log-sink boundary.
>
> Verified: `go test -race -short ./internal/ft8`; `go test ./internal/ft8
> ./internal/api ./internal/bridge ./cmd/smd`; `CGO_ENABLED=0 go test
> ./internal/ft8` + `CGO_ENABLED=0 go build ./...`; `go vet` — all green.

## Scope

Reviewed the live FT8 subsystem and the adjacent contracts that make its
behavior observable:

- `internal/ft8/*.go` and `internal/ft8/*_test.go`
- FT8 HTTP handlers in `internal/api`
- bridge keying contract in `internal/bridge/ft8tx.go`
- daemon composition and QSO-log sink in `cmd/smd/main.go`
- FT8 config shape in `internal/types/ft8.go`
- current FT8 design notes in `docs/ft8.md` and ADR 0033

This is review-only. No source fixes were made.

## Findings

### H1 - Completed QSOs can be logged before the final transmission actually succeeds

The sequencer treats the final 73/RR73 as complete once the transmit request is
queued, not when the key/play/unkey goroutine has succeeded.

`startTransmission` returns `nil` immediately after it starts the tracked
goroutine:

- `internal/ft8/servicetx.go:244-267`

The actual `TxController` call and failure classification happen later inside
that goroutine:

- `internal/ft8/servicetx.go:246-264`

The answer-a-CQ path captures completion before it calls `transmit`, then calls
`onComplete` after `transmit` returns:

- `internal/ft8/sequencer.go:315-350`

The caller-side path does the same for RR73:

- `internal/ft8/caller_sequencer.go:179-209`

That means `onComplete` can run after "queued" but before the final audio ever
keys. If the bridge disconnects before keying, `KeyTx` fails, or playback fails
after the goroutine starts, the QSO has already been handed to the `cmd/smd`
sink and can be submitted plus emitted as `ft8-logged`.

There is also a synchronous version of the same issue: on the final rung,
`ErrTxInFlight` or `ErrTxBadMessage` logs a warning but still falls through to
completion. Only `ErrTxNotArmed` returns before `onComplete`:

- `internal/ft8/sequencer.go:338-350`
- `internal/ft8/caller_sequencer.go:196-209`

Impact:

- A failed final 73/RR73 can create a stored QSO and session-list row for a
  contact that was not actually completed on air.
- Forwarders can upload that false QSO because the log sink uses the normal
  `qsoservice.Submit` path.
- The later `ft8-tx` failure event does not retract the already-submitted QSO.

Recommendation:

- Change the sequencer/TX boundary so the final-rung completion callback fires
  only after the transmit goroutine reports success.
- Treat any synchronous final-rung transmit error as "not complete", not just
  `ErrTxNotArmed`.
- Add tests that force final-rung `ErrTxInFlight`, `ErrTxBadMessage`, and an
  async key/play failure, asserting no `CompletedQso` and no `ft8-logged`.

### H2 - `operator_pick` is accepted as a caller mode but the sequencer always auto-picks the first answerer

The config model declares both `auto_first` and `operator_pick` as valid and
`ResolveFt8CallerAnswerMode` returns `operator_pick` when configured:

- `internal/types/ft8.go:202-224`

The service passes that resolved value into `StartCallCq`:

- `internal/ft8/servicetx.go:292-301`

The sequencer stores the value:

- `internal/ft8/caller_sequencer.go:45-50`

But `onSlotCalling` ignores it and always works the first valid answerer:

- `internal/ft8/caller_sequencer.go:92-104`

The code comment explicitly says `operator_pick` is later work, while the
runtime accepts it today:

- `internal/ft8/caller_sequencer.go:11-16`

Impact:

- A hand-edited `ft8.tx.caller_answer_mode: "operator_pick"` config does not
  do what it says. Pressing Call CQ can still transmit a report to the first
  decoded answerer.
- In a pile-up, this violates the operator-selection policy that ADR 0033
  introduced for `operator_pick`.

Recommendation:

- Until the stack exists, do not resolve `operator_pick` as an active mode, or
  reject/start-fail with an explicit unsupported-mode error.
- When implemented, `operator_pick` should collect answerers and wait for an
  operator pop before creating `CallerExchange` and transmitting the report.
- Add a test with `operator_pick` and multiple answerers proving no report is
  transmitted until the operator chooses one.

### M1 - Sequencer start accepts sessions whose next FT8 message cannot encode

`StartQso` commits and publishes an active answer-a-CQ session after only
validating offset and slot time:

- `internal/ft8/sequencer.go:154-181`

`StartCallCq` trims/uppercases the configured identity and commits an active
caller session, but does not validate that the CQ message encodes:

- `internal/ft8/caller_sequencer.go:23-64`

The first encoder check for sequenced rungs is inside `seqTransmit`:

- `internal/ft8/servicetx.go:199-203`

When `seqTransmit` returns `ErrTxBadMessage`, the sequencer logs the failure but
continues unless the error is `ErrTxNotArmed`:

- `internal/ft8/sequencer.go:338-344`
- `internal/ft8/sequencer.go:419-424`
- `internal/ft8/caller_sequencer.go:196-201`

This matters because the adjacent CQ parser accepts compound/portable calls
with `/`, while the current encoder explicitly supports only standard
structured messages:

- `frontend/logging/src/lib/utils/ft8Message.ts:16-27`
- `frontend/logging/src/lib/utils/ft8Message.ts:56-63`
- `docs/ft8.md:665-667`

Impact:

- Clicking an unsupported CQ such as a portable/compound call can return `202`
  and publish an active `ft8-qso`, but no RF can ever be generated.
- A malformed operator/station callsign or grid can make Call CQ enter a
  `calling-cq` loop that repeatedly fails to encode; that phase has no repeat
  cap by design.
- The operator sees an active ladder rather than an immediate client error.

Recommendation:

- Validate the opening `TxMessage` / CQ with the same encoder before committing
  sequencer state or publishing `ft8-qso`.
- Treat `ErrTxBadMessage` as terminal for an active sequencer session and
  publish inactive state.
- Either make the SPA only initiate answerable standard CQs, or return a clear
  "unsupported FT8 message" response for unsupported decoded calls.

### M2 - Scheduler missed-boundary handling can emit stale, mis-stamped slots

The scheduler emits for the current `target`, advances by exactly one slot, and
resets the timer using `time.Until(target)`:

- `internal/ft8/scheduler.go:125-128`

If the scheduler goroutine is delayed past one or more boundaries, `target` can
still be in the past. The next reset fires immediately, so the scheduler can
emit catch-up slots using the current ring snapshot while stamping them with old
slot times:

- `internal/ft8/scheduler.go:142-145`

`OffsetMs` also uses the timer's `fired` value, not the wall time at which the
slot was actually serviced:

- `internal/ft8/scheduler.go:142-145`

The existing scheduler tests cover normal real-time emission and consumer
backpressure, but not an overdue target/catch-up path:

- `internal/ft8/scheduler_test.go:53-100`
- `internal/ft8/scheduler_test.go:151-177`

Impact:

- A delayed scheduler can publish decodes and occupancy with the wrong
  `StartUTC` / parity for the samples actually decoded.
- The sequencer consumes that parity through `OnSlot`, so stale slot stamps can
  drive rung timing decisions from the wrong slot.
- Delay diagnostics can look healthier than reality because `OffsetMs` can miss
  servicing delay.

Recommendation:

- On timer receive, compare `time.Now().UTC()` with `target`; if one or more
  boundaries were missed, skip stale targets and resume at the next future
  boundary instead of emitting backlogged snapshots.
- Compute `OffsetMs` from actual service time, not only the timer payload.
- Add a clock/timer-injected test for one-slot and multi-slot overdue targets.

### M3 - Disarm can race with QSO start and leave an active sequencer after TX is disarmed

`disarmTx` abandons the sequencer before it takes `txMu` and clears `txArmed`:

- `internal/ft8/servicetx.go:135-149`

`StartQso` and `StartCallCq` read `txArmed` under `txMu`, release the lock, and
then call into the sequencer:

- `internal/ft8/servicetx.go:275-283`
- `internal/ft8/servicetx.go:292-301`

That leaves an interleaving where a start call observes `txArmed=true`, a
disarm abandons the then-idle sequencer and clears `txArmed=false`, and the
start call then commits a new active session after the disarm's abandon already
ran.

Impact:

- `ArmTx(false)`, `Stop`, or the last-subscriber auto-disarm path can leave an
  active `ft8-qso` until the next rung tries to transmit and discovers
  `ErrTxNotArmed`.
- RF should not key because `txArmed` is false, but the state contract "disarm
  aborts the contact" is not atomic.

Recommendation:

- Order disarm so the armed state is cleared/cancelled under `txMu` before the
  sequencer abandon, or introduce a shared start/disarm gate around "armed check
  plus sequencer commit".
- Add a concurrent start/disarm test that proves no QSO remains active after
  disarm returns.

### L1 - Package documentation still describes `internal/ft8` as a decode-only wrapper

The package comment says the package is deliberately thin and documents only
`DecodeSlot` and `DecodeFile`:

- `internal/ft8/doc.go:1-23`

The package now owns live capture, occupancy, SSE, TX, PTT orchestration,
answer-a-CQ sequencing, caller-side sequencing, and QSO-log payload assembly.

Impact:

- Future reviewers can miss the current outbound-RF and logbook responsibilities
  because the package-level boundary summary is materially stale.

Recommendation:

- Update `doc.go` to describe the current RX/TX/sequencer shape, the bridge keyer
  seam, attended-only model, and QSO-log sink boundary.

## Notes

- The package has strong focused coverage for the pure resolvers, TX key/unkey
  controller, hub replay, CGO-free capture behavior, and QSO log projection.
- The bridge `TxKeyer` contract is much safer than the older tune path: it gates
  on identity confirmation and dry-runs `tx_off` before keying.
- `internal/ft8` still preserves the intended storage boundary: completed QSO
  assembly lives in the package, while submit/enrichment/forwarding stay in
  `cmd/smd` and `qsoservice`.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test -count=1 ./internal/ft8 ./internal/api ./internal/bridge ./cmd/smd`
- `GOCACHE=/tmp/go-build go test -race -count=1 ./internal/ft8`
- `GOCACHE=/tmp/go-build CGO_ENABLED=0 go test -count=1 ./internal/ft8`
- `GOCACHE=/tmp/go-build go vet ./internal/ft8 ./internal/api ./internal/bridge ./cmd/smd`

The initial sandboxed test runs failed because `httptest.NewServer` could not
bind localhost sockets (`operation not permitted`). The same focused test suites
passed when rerun with localhost binding allowed.
