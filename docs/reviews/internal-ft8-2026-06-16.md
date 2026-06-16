# internal/ft8 Code Review - 2026-06-16

## Scope

Thorough read-only review of the current FT8 subsystem after the 2026-06-14
fixes.

Reviewed:

- `internal/ft8/*.go` and `internal/ft8/*_test.go`
- FT8 API handlers in `internal/api`
- bridge FT8 keying readiness in `internal/bridge/ft8tx.go`
- FT8 UI offset/frequency callers in `frontend/logging`
- current FT8 design docs in `docs/ft8.md` and ADR 0029

No production source changes were made during the review. This document was
added afterward at the operator's request.

Headline counts: **0 Critical**, **1 High**, **4 Medium**, **0 Low**.

## Findings

### H1 - Documented daemon-side clear-offset enforcement is missing

The FT8 docs and ADR say Station Manager enforces good FT8 practice by refusing
or snapping a TX offset that overlaps current occupancy:

- `docs/ft8.md:241`
- `docs/ft8.md:249`
- `docs/ft8.md:349`
- `docs/decisions/0029-ft8-transmit-manual-sequencing.md:152`

The current transmit paths do not implement that gate.

Manual one-shot TX only validates that the message can encode, then starts the
transmission at the requested offset:

- `internal/ft8/servicetx.go:198`
- `internal/ft8/servicetx.go:203`
- `internal/ft8/servicetx.go:209`

Sequenced answer-a-CQ and Call-CQ starts reject only non-positive offsets before
storing the operator/client-provided offset:

- `internal/ft8/sequencer.go:166`
- `internal/ft8/sequencer.go:196`
- `internal/ft8/caller_sequencer.go:23`
- `internal/ft8/caller_sequencer.go:58`

The package already has the occupancy admission primitive needed for this check:

- `internal/ft8/occupancy.go:397`

And the service exposes the latest occupancy report:

- `internal/ft8/service.go:414`

The UI does not close the gap. It intentionally lets every occupancy cell be
clicked, including busy/red cells:

- `frontend/logging/src/lib/ui/panels/Ft8OccupancyStrip.svelte:117`
- `frontend/logging/src/lib/ui/panels/Ft8OccupancyStrip.svelte:126`

The panel detects a busy selected offset only for display:

- `frontend/logging/src/lib/ui/panels/Ft8Panel.svelte:221`
- `frontend/logging/src/lib/ui/panels/Ft8Panel.svelte:232`

Impact:

- A restored persisted offset that has since become occupied can still key RF.
- A direct API caller can transmit on an occupied or out-of-passband offset.
- The runtime behavior contradicts the documented "good practice is enforced"
  contract.

Recommendation:

- Add a daemon-side TX offset admission check before `TransmitNext`,
  `StartQso`, and `StartCallCq` commit or start RF.
- Reuse the current occupancy/passband rules, including guard margin.
- Decide and test the intended no-current-occupancy behavior: refuse until a
  fresh slot exists, or allow with an explicit "best effort" contract.
- If snapping remains desired, implement it explicitly and report the chosen
  offset back to the UI; otherwise document and test refusal.

### M1 - Sequenced sessions can be committed after the rig becomes unready

`ArmTx(true)` verifies bridge readiness once:

- `internal/ft8/servicetx.go:95`
- `internal/ft8/servicetx.go:109`

After that, `StartQso` and `StartCallCq` only check the sticky `txArmed` flag
before committing an active sequencer session:

- `internal/ft8/servicetx.go:307`
- `internal/ft8/servicetx.go:312`
- `internal/ft8/servicetx.go:327`
- `internal/ft8/servicetx.go:340`

`startTransmission` also checks only `txArmed` and `txInFlight` before launching
the async TX goroutine:

- `internal/ft8/servicetx.go:239`
- `internal/ft8/servicetx.go:249`
- `internal/ft8/servicetx.go:258`

The bridge keying path still refuses an actually unready rig:

- `internal/bridge/ft8tx.go:78`
- `internal/bridge/ft8tx.go:86`

And `TxReady` describes the live readiness predicate the FT8 service could
re-check:

- `internal/bridge/ft8tx.go:265`
- `internal/bridge/ft8tx.go:270`

The sequencer only gets an async completion callback for final rungs:

- `internal/ft8/sequencer.go:372`
- `internal/ft8/caller_sequencer.go:210`

So a non-final async key failure is visible as an `ft8-tx` failure event, but it
does not directly unwind or disarm the active sequencer state.

Impact:

- If the rig disconnects or identity becomes unverified after arming, the API can
  still return `202` and publish an active QSO/CQ session.
- The first/non-final rung can fail asynchronously while the sequencer keeps
  waiting and retrying as though the session is viable.
- A future synchronous `ErrTxNotReady` check would currently fall through
  `writeFt8QsoError`'s default malformed-request mapping rather than a clear
  readiness response.

Recommendation:

- Re-check `keyer.TxReady()` while holding `txMu` in `startTransmission`, and/or
  before `StartQso` / `StartCallCq` commit their sessions.
- Map `ErrTxNotReady` explicitly in `writeFt8QsoError`.
- Consider disarming or publishing armed=false after an async bridge readiness
  failure, so the operator cannot remain in a stale armed state.
- Add regression coverage for "armed, bridge becomes unready, start QSO/CQ".

### M2 - Caller auto-pick can abandon Call-CQ on an unencodable answerer

The message parser accepts slash calls:

- `internal/ft8/sequence.go:22`
- `internal/ft8/sequence.go:29`

The answer-a-CQ path now validates that the opening message encodes before it
commits the session:

- `internal/ft8/sequencer.go:175`
- `internal/ft8/sequencer.go:180`

Call-CQ also validates that our own CQ encodes before starting:

- `internal/ft8/caller_sequencer.go:35`
- `internal/ft8/caller_sequencer.go:42`

But the caller-side auto-first path stores the first answerer without validating
that our response to that answerer can encode:

- `internal/ft8/caller_sequencer.go:99`
- `internal/ft8/caller_sequencer.go:103`
- `internal/ft8/caller_sequencer.go:105`
- `internal/ft8/caller_sequencer.go:106`

It builds the next TX message afterward:

- `internal/ft8/caller_sequencer.go:130`

If that message is rejected by `seqTransmit`, the caller path treats
`ErrTxBadMessage` as terminal and abandons the entire Call-CQ session:

- `internal/ft8/caller_sequencer.go:237`
- `internal/ft8/caller_sequencer.go:242`
- `internal/ft8/caller_sequencer.go:243`

Impact:

- A portable or compound station answering our CQ can stop our whole Call-CQ
  loop instead of being skipped.
- Other valid answerers in the same slot are not considered once the first
  unencodable answerer is committed.

Recommendation:

- Before assigning `s.caller`, build the candidate `CallerExchange`, call
  `TxMessage`, and validate the message with the FT8 encoder.
- Skip unencodable answerers and continue scanning the slot for another valid
  answerer; if none exist, continue calling CQ.
- Add a regression test for an auto-first slot containing `K1ABC/P` followed by
  a standard answerer.

### M3 - Bare caller-side roger can log false FT8 RST_RCVD=59

Caller-side exchange advancement intentionally accepts a bare `RRR` / `RR73`
after we send the answerer a report:

- `internal/ft8/caller.go:104`
- `internal/ft8/caller.go:109`
- `internal/ft8/caller.go:118`

The comment notes that this advances "without an RST_RCVD to log":

- `internal/ft8/caller.go:110`

`BuildQso` preserves that by setting `RstRcvd` only when the completed QSO has a
received report:

- `internal/ft8/qsolog.go:51`
- `internal/ft8/qsolog.go:54`

The downstream submit path then applies generic voice-style defaults to empty
RST fields:

- `internal/qsoservice/submit.go:203`
- `internal/qsoservice/submit.go:207`

Impact:

- A caller-side FT8 QSO completed through the bare-roger path can be stored with
  `RST_RCVD=59`, which is not the report sent by the other station.
- This contradicts the FT8-specific comment and creates false log data.

Recommendation:

- Decide whether the caller-side bare-roger path is loggable. If FT8 logs require
  both signal reports, do not complete/log the QSO without a received report.
- If the path remains loggable, prevent `qsoservice` from applying `59` defaults
  to FT8 records with intentionally absent reports.
- Add an integration-style test from `CompletedQso` through the submit sink for
  the bare-roger case.

### M4 - Client-provided operating frequency is not validated before RF

The QSO and Call-CQ API requests accept `operating_freq_mhz` from the client:

- `internal/api/handler_ft8_qso.go:17`
- `internal/api/handler_ft8_qso.go:25`
- `internal/api/handler_ft8_qso.go:68`
- `internal/api/handler_ft8_qso.go:72`

The handlers pass that value directly into the FT8 service:

- `internal/api/handler_ft8_qso.go:55`
- `internal/api/handler_ft8_qso.go:99`

The sequencers store it without validation:

- `internal/ft8/sequencer.go:196`
- `internal/ft8/caller_sequencer.go:59`

The completed QSO is built from that value:

- `internal/ft8/qsolog.go:27`
- `internal/ft8/qsolog.go:29`
- `internal/ft8/qsolog.go:40`

The generic QSO submit path rejects missing or invalid band/frequency only after
the RF exchange has already completed:

- `internal/qsoservice/submit.go:85`
- `internal/qsoservice/submit.go:122`
- `internal/qsoservice/submit.go:175`
- `internal/qsoservice/submit.go:179`

The SPA can initiate answer-a-CQ or Call-CQ without requiring a live/valid rig
frequency at that moment:

- `frontend/logging/src/lib/ui/panels/Ft8Panel.svelte:150`
- `frontend/logging/src/lib/ui/panels/Ft8Panel.svelte:159`
- `frontend/logging/src/lib/ui/panels/Ft8MsgPanel.svelte:47`
- `frontend/logging/src/lib/ui/panels/Ft8MsgPanel.svelte:144`

The comments in the API/UI also still say the logged QSO frequency is
dial-plus-audio-offset, while `BuildQso` now logs the dial frequency by design:

- `internal/api/handler_ft8_qso.go:22`
- `internal/api/handler_ft8_qso.go:70`
- `frontend/logging/src/lib/ui/panels/Ft8Panel.svelte:157`
- `frontend/logging/src/lib/ui/panels/Ft8MsgPanel.svelte:37`
- `internal/ft8/qsolog.go:17`

Impact:

- An invalid or stale client frequency can let an RF exchange complete, then fail
  the QSO log submission afterward.
- The operator may believe the contact was completed and logged because the on-air
  sequencer finished, while the persistence path rejects it later.
- Stale comments make the current logging contract harder to reason about.

Recommendation:

- Validate `operating_freq_mhz` before committing a sequenced RF session.
- Prefer deriving the dial frequency server-side from current bridge state if
  that state is available and identity-verified.
- Update the stale dial-plus-offset comments to match `BuildQso`'s dial-frequency
  convention.
- Add tests for zero, negative, unparsable/out-of-band, and valid FT8-band
  operating frequencies.

## Test gaps

- No regression covers the missing occupancy/TX offset gate.
- No regression covers "armed but bridge becomes unready before QSO/CQ start".
- No regression covers caller auto-pick with an unencodable first answerer.
- No integration-style regression covers bare-roger FT8 logging through
  `qsoservice`.
- `handleFt8CqStart` has less direct API coverage than `handleFt8QsoStart`.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test ./internal/ft8 ./internal/api`
- `GOCACHE=/tmp/go-build go test -race ./internal/ft8 ./internal/api`
- `CGO_ENABLED=0 GOCACHE=/tmp/go-build go test ./internal/ft8`
- `CGO_ENABLED=1 GOCACHE=/tmp/go-build go test -tags pocketfft ./internal/ft8`
- `GOCACHE=/tmp/go-build go vet ./internal/ft8 ./internal/api`

All focused validation passed.

Sandbox note: the first sandboxed package-test attempt failed before normal test
execution because `httptest` could not bind localhost sockets
(`listen tcp6 [::1]:0: socket: operation not permitted`). The affected tests
were rerun with localhost binding allowed and passed.

## Resolution (2026-06-16)

Operator decision: fix M1/M2/M3/M4-comments; document H1 as best-effort (no
daemon gate).

- **H1 — documented as best-effort, not enforced.** SM is attended-only, so the
  TX offset is the operator's choice; the occupancy strip is *guidance* (ranks +
  highlights clear spots, shades busy ones), not a hard gate. A daemon-side
  admission gate was considered and deliberately left out — it would fight the
  attended-operation model. Reworded `docs/ft8.md` (offset-strip + ranking
  sections) and ADR 0029 to drop the "enforced" / "refuses or snaps" claims.
- **M1 — fixed.** `startTransmission`, `StartQso`, and `StartCallCq` now re-check
  the LIVE `keyer.TxReady()` (not just the sticky `txArmed`) under `txMu` and
  return `ErrTxNotReady`; `writeFt8QsoError` maps it to `503 rig_not_ready`.
  Regression: `TestStartSession_RefusesWhenRigBecomesUnready`.
- **M2 — fixed.** The caller auto-first path now builds the candidate reply and
  validates it encodes before committing the answerer; unencodable
  (compound/portable) answerers are skipped and the slot keeps scanning, so a
  `/P` caller no longer abandons the pile-up. Regression:
  `TestCallerSequencer_AutoFirstSkipsUnencodableAnswerer`.
- **M3 — fixed.** `qsoservice.Submit` no longer applies the voice-style `59` RST
  default to FT8 records, so a bare-roger contact logs an empty (omitted)
  `RST_RCVD` rather than a false `59`. Regression:
  `TestFt8CallerBareRogerNoFalseRst`.
- **M4 — comments only (per operator).** The stale "dial+offset" comments in
  `handler_ft8_qso.go` and the two SPA panels now match `BuildQso`'s
  dial-frequency convention. The client-frequency *validation* (deriving the dial
  server-side / rejecting bad freq before RF) was NOT done — deferred; an invalid
  client freq still fails only at submit, after the exchange.

Verification: `gofmt`, `go test ./...` (CGO on), `CGO_ENABLED=0 go build ./...`,
and `svelte-check` all green.
