# internal/ft8 Follow-up Code Review - 2026-06-14

> **Resolution (2026-06-14): both findings fixed.**
> - **M1** — final-rung state is no longer cleared before the transmit is
>   accepted. The confirming exchange / cqRogering contact is captured but left
>   intact; a synchronous `ErrTxInFlight` now retries next slot (state untouched),
>   and on the answerer side a new QSO is refused (`ErrQsoInProgress`) while the
>   73 is still keying. A `Sequencer.sessionGen` token (bumped on
>   StartQso/StartCallCq commit + Abandon) guards the async `onDone` for **both**
>   logging and publishing, so an Abandon/disarm mid-flight can't let a stale
>   callback log a QSO or publish idle over a newer session. On success: answerer
>   → log + idle; caller → log + resume CQ. On RF failure: leave the rung so the
>   next slot retries (no silent drop). Tests:
>   `TestSequencer_FinalRungInFlightRetries`,
>   `TestCallerSequencer_FinalRR73InFlightRetries`,
>   `TestSequencer_DeferredFinal73LogsOnSuccess`,
>   `TestSequencer_AbandonDuringFinal73SuppressesStaleCallback` (+ the prior
>   async-fail tests, updated to the keep-active-and-retry semantic).
> - **M2** — the scheduler now skips any slot serviced later than a
>   `maxSlotLateness` budget (package var, default **2 s** — far above normal
>   sub-50 ms jitter, far below a 15 s slot) and resyncs at the next boundary,
>   instead of emitting a shifted-window, stale-stamped slot for any sub-slot
>   delay. `OffsetMs` already uses real service time. Test: `TestSlotTooLate`
>   (asserts `+100 ms` emits, `+5 s` / `+15 s` skip).
>
> Verified: `go test -race -short ./internal/ft8`; `go test ./internal/ft8
> ./internal/api ./internal/bridge ./cmd/smd`; `CGO_ENABLED=0 go test
> ./internal/ft8` + `CGO_ENABLED=0 go build ./...`; `go vet` — all green.

## Scope

Reviewed the landed FT8 fix commit `51a1b588` against the earlier
`internal/ft8` review in `docs/reviews/internal-ft8-2026-06-14.md`.

Files reviewed:

- `internal/ft8/*.go` and `internal/ft8/*_test.go`
- FT8 QSO API mapping in `internal/api/handler_ft8_qso.go`
- FT8 SSE replay/cached state in `internal/ft8/hub.go`
- Updated FT8 docs in `docs/ft8.md` and `internal/ft8/doc.go`

This is review-only. No production source fixes were made.

## Findings

### M1 - Final-rung state can still be lost around transmit acceptance/completion

The H1 false-log fix correctly moves QSO logging behind the final-rung
`onDone(ok)` callback, but the final-rung state is still cleared before the
transmit call has definitely accepted and finished owning that rung.

Answer-a-CQ clears the active exchange and switches to idle before calling
`transmit`:

- `internal/ft8/sequencer.go:330-339`

The final-rung callback then publishes `Active:false` unconditionally after the
TX goroutine finishes:

- `internal/ft8/sequencer.go:360-372`
- `internal/ft8/servicetx.go:291-296`

Meanwhile `StartQso` only checks that TX is armed, not whether a previous final
rung is still in flight:

- `internal/ft8/servicetx.go:307-318`

That leaves a real interleaving:

1. Final 73 is accepted and starts transmitting.
2. The sequencer is already idle, so the operator can start another QSO while
   the final 73 is still in flight.
3. The new QSO publishes an active `ft8-qso` state.
4. The old final-73 callback fires later and publishes `Active:false`.

Because the hub caches the latest `ft8-qso`, that older callback can overwrite
the newer active session for current and late SSE subscribers:

- `internal/ft8/hub.go:64-65`
- `internal/ft8/hub.go:97-104`

There is also a synchronous version of the same state-loss problem. The code
comments say non-terminal final-rung transmit errors such as `ErrTxInFlight`
"skip this slot and retry next cycle", but the answerer state has already been
set to idle, so the final rung is not retried:

- `internal/ft8/sequencer.go:376-390`

The caller-side RR73 path has the same early state transition: it clears
`s.caller` and resumes calling CQ before `transmit` returns, so a synchronous
`ErrTxInFlight` drops the just-finished contact instead of retrying RR73:

- `internal/ft8/caller_sequencer.go:186-197`
- `internal/ft8/caller_sequencer.go:225-234`

Impact:

- The false QSO logging bug is fixed, but a final-rung `ErrTxInFlight` can still
  silently end or drop the contact instead of retrying as documented.
- An older final-73 callback can publish idle over a newer active answer-a-CQ
  session, leaving the UI/SSE cache inconsistent with the sequencer.

Recommendation:

- Do not clear final-rung answerer/caller state until the transmit request is
  accepted; on synchronous non-terminal errors, retain or restore the final-rung
  state so the next slot can retry.
- Tie async final-rung callbacks to a session generation/token, or keep the
  session in a pending-complete state, so an old callback cannot publish idle
  over a newer session.
- Add tests for final-rung `ErrTxInFlight` on both answerer and caller paths,
  plus an async callback ordering test where a second QSO starts before the old
  final-rung `onDone` fires.

### M2 - Scheduler still accepts very late sub-slot emits with stale sample windows

The scheduler now skips targets delayed to or past the next slot boundary, and
`OffsetMs` is computed from the real service time. That fixes the multi-boundary
catch-up case, but large delays inside the current 15-second slot still emit a
target-stamped slot using the current ring snapshot.

`Run` only skips when `staleTarget(now, target)` is true:

- `internal/ft8/scheduler.go:125-141`

`staleTarget` returns true only once `now >= target + SlotDuration`:

- `internal/ft8/scheduler.go:146-152`

The new regression test codifies that `target + 5s` and
`target + SlotDuration - 1ms` are still valid emits:

- `internal/ft8/review_findings_test.go:139-146`

For a delay that large, the ring no longer represents the exact completed slot
`[target-SlotDuration, target)`. It contains samples from after `target` and has
lost the same amount from the front of the target slot, but the emitted
`StartUTC` still says the samples cover the old window:

- `internal/ft8/scheduler.go:35-52`
- `internal/ft8/scheduler.go:160-168`

Impact:

- A stalled scheduler can feed decode/occupancy with a shifted sample window and
  a stale `StartUTC`/parity while still looking "not stale" to the guard.
- The sequencer consumes decoded slot parity, so a large sub-slot delay can still
  drive timing decisions from mis-stamped audio.

Recommendation:

- Add a maximum acceptable timer lateness that is much smaller than a full slot
  (for example the existing scheduler integration health budget, or a capture
  batch sized threshold), and drop/resync once that budget is exceeded.
- Alternatively, timestamp samples at capture time and snapshot by target window
  instead of treating the latest ring contents as the target slot.
- Add a deterministic scheduler test for `target + 5s` showing the slot is
  dropped or explicitly marked unusable rather than decoded as the target slot.

## Resolution Audit

The rest of the landed fixes held up under re-review:

- `operator_pick` is rejected before starting Call CQ, and the API maps it to
  `501 ft8_caller_mode_unsupported`.
- Answer-a-CQ and Call-CQ now validate their opening messages before committing
  an active session.
- Disarm/start is now serialized by `seqGate`, and the focused race test passed
  under `-race`.
- The package documentation has been updated to describe the current RX/TX,
  sequencer, keyer, and QSO-log-sink boundaries.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test ./internal/ft8 ./internal/api ./internal/bridge ./cmd/smd`
- `GOCACHE=/tmp/go-build go test -race -short ./internal/ft8`
- `GOCACHE=/tmp/go-build CGO_ENABLED=0 go test ./internal/ft8`
- `GOCACHE=/tmp/go-build go vet ./internal/ft8 ./internal/api ./internal/bridge ./cmd/smd`
- `GOCACHE=/tmp/go-build CGO_ENABLED=0 go build ./...`
- `git diff --check origin/main..HEAD`

Sandbox note: the first sandboxed test attempts failed because
`httptest.NewServer` could not bind localhost sockets (`operation not
permitted`). The affected package tests were rerun with localhost binding
allowed and passed.

`CGO_ENABLED=0 go build ./...` exited successfully but printed a Go module stat
cache warning because the module cache is read-only in this sandbox.
