# `internal/bridge` - code review (2026-06-19)

## Scope

Review-only pass over `internal/bridge` as a fresh package: service lifecycle,
SSE hub/handler behavior, serial pipeline and supervisor, CI-V command/ACK flow,
tune-carrier control, FT8 keying integration, test coverage, and the adjacent
API/daemon contracts that consume bridge state.

No production code changes were applied during this review. This document is the
only artifact from the pass.

Headline counts: **0 Critical**, **0 High**, **2 Medium**, **2 Low**.

## Medium findings

### M1. CI-V commanded frequency updates SSE state but leaves the bridge dial snapshot stale

**Files:**

- `internal/bridge/command.go:174`
- `internal/bridge/command.go:298`
- `internal/bridge/command.go:307`
- `internal/bridge/command.go:308`
- `internal/bridge/tune.go:384`
- `internal/bridge/tune.go:391`
- `internal/bridge/pipeline.go:581`
- `internal/bridge/pipeline.go:584`
- `cmd/smd/main.go:470`
- `cmd/smd/main.go:476`
- `cmd/smd/main.go:563`
- `cmd/smd/main.go:566`
- `internal/bridge/command_civ_test.go:101`
- `internal/bridge/command_civ_test.go:108`

For CI-V rigs, `sendCommandsCIV` waits for FB/FA and then calls
`publishCommandedState` for commands with `sets_state`. That function synthesizes
and publishes a `rig-state` event, but it only refreshes the tune restore
snapshot before publishing. It does not call `captureDialFreq`.

The decoded-frame path does update both snapshots before publishing: it calls
`captureTuneSnapshot` and `captureDialFreq` before `hub.publish`. That difference
matters because `CurrentDialMHz` is not just UI state; `cmd/smd` treats it as the
authoritative dial for completed FT8 QSO logging and PSK Reporter spots.

Impact:

- On an IC-7300-style CI-V rig, an acknowledged `set_freq` can immediately show
  the new VFO over SSE while `bridge.CurrentDialMHz()` still returns the previous
  dial until a later poll/read response lands.
- A completed FT8 QSO or PSK Reporter decode in that window can be logged or
  uploaded with the old absolute frequency.
- Existing CI-V tests assert the synthesized `rig-state` event, but do not assert
  that the internal dial snapshot used by daemon integrations changed too.

Suggested fix:

- In `publishCommandedState`, call `s.captureDialFreq(payload)` alongside
  `s.captureTuneSnapshot(payload)` before publishing.
- Add a CI-V regression test that seeds an initial dial, sends an ACKed
  `set_freq`, asserts the synthesized SSE payload, and then asserts
  `CurrentDialMHz()` returns the commanded frequency without waiting for poll.
- Add a non-frequency command assertion if needed to prove the call is harmless
  for mode/power-only payloads.

### M2. Some CI-V snapshot writes bypass the package-level command serialization contract

**Files:**

- `internal/bridge/service.go:156`
- `internal/bridge/service.go:159`
- `internal/bridge/service.go:164`
- `internal/bridge/service.go:432`
- `internal/bridge/service.go:441`
- `internal/bridge/pipeline.go:332`
- `internal/bridge/pipeline.go:337`
- `internal/bridge/pipeline.go:354`
- `internal/bridge/pipeline.go:355`
- `internal/bridge/pipeline.go:467`
- `internal/bridge/pipeline.go:474`
- `internal/bridge/pipeline.go:913`
- `internal/bridge/pipeline.go:917`
- `internal/bridge/pipeline.go:919`
- `internal/bridge/command.go:163`
- `internal/bridge/command.go:234`
- `internal/bridge/command.go:236`
- `internal/serial/serial.go:60`
- `internal/serial/serial.go:236`

The serial client serializes individual `WriteCommandBytes` calls, so this is not
byte-level corruption. The gap is at the higher CI-V sequence level.

`cmdMu` is documented and used as the one-outstanding-command guard for the CI-V
half-duplex bus. The poll loop takes it around `writeSnapshotReads`, and the
generic command/keying paths take it around their ACK sequences. However,
`TriggerBootstrap` writes the cached READ snapshot directly, and the read-loop
liveness recovery path writes INIT plus the READ snapshot directly after a read
deadline. For CI-V snapshots, `writeSnapshotReads` splits the READ list into
multiple frames with optional inter-frame gaps, so an SSE bootstrap/probe can
still slip between command, poll, or key/unkey frames at the sequence level.

Impact:

- The implementation does not consistently enforce its own CI-V
  "one command sequence on the bus" model.
- A new SSE subscriber or liveness probe can add read frames while a command
  batch or keyed TX ACK flow is in progress, increasing the chance of delayed
  ACKs, stale readback ordering, or hard-to-reproduce no-ACK outcomes on real
  radios.
- The current tests cover CI-V ACKs, polls, and bootstraps separately, but not a
  bootstrap/probe racing an in-flight `cmdMu` operation.

Suggested fix:

- Route all CI-V snapshot writers through a single helper that takes `cmdMu`
  before `writeSnapshotReads`; keep non-CI-V behavior unchanged.
- Preserve the documented lock order: no caller should hold `mu` or `keyMu` when
  taking `cmdMu` unless it is already in the established `keyMu -> cmdMu -> mu`
  path.
- Add tests that hold or block a CI-V command/key ACK flow and prove
  `TriggerBootstrap` and the liveness READ do not write until the in-flight
  sequence releases `cmdMu`.

## Low findings

### L1. Tune/FT8 mutual-exclusion errors are untyped, so adjacent API paths cannot classify them

**Files:**

- `internal/bridge/tune.go:120`
- `internal/bridge/tune.go:124`
- `internal/bridge/ft8tx.go:79`
- `internal/bridge/ft8tx.go:81`
- `internal/api/handler_rig_tune.go:49`
- `internal/api/handler_rig_tune.go:60`
- `internal/api/handler_rig_command.go:115`
- `internal/api/response.go:58`
- `internal/bridge/ft8tx_test.go:111`
- `internal/bridge/ft8tx_test.go:127`
- `internal/ft8/servicetx.go:279`
- `internal/ft8/txcontroller.go:141`

`SendCommands` has `ErrTxActive`, and the rig-command API maps it to HTTP 409
`rig_tx_active`. The keyed paths do not use the same taxonomy. `StartTune`
returns an unwrapped message when FT8 TX is active, and `KeyFt8Tx` returns an
unwrapped message when the tune carrier is active.

Impact:

- A direct `POST /v1/rig/tune {"active":true}` during FT8 TX falls through
  `writeRigTuneError` to `writeServerError`, returning 500 `rig_tune_failed`
  for an expected state conflict.
- An FT8 readiness race can similarly turn a tune/FT8 conflict into a generic
  async transmit failure instead of a typed conflict.
- Tests currently assert only that the conflicting key/tune calls return an
  error, not that callers can identify the error class.

Suggested fix:

- Return `ErrTxActive` or a dedicated keyed-transmission conflict sentinel from
  `StartTune` and `KeyFt8Tx` for this mutual-exclusion case.
- Map that sentinel in `writeRigTuneError` to HTTP 409 `rig_tx_active`.
- Strengthen bridge tests to assert `errors.Is(err, ErrTxActive)` and add an API
  handler regression for tune-during-FT8.

### L2. Current package/config comments still describe pre-write-path bridge behavior

**Files:**

- `internal/bridge/doc.go:31`
- `internal/bridge/doc.go:32`
- `internal/bridge/doc.go:33`
- `internal/types/bridge.go:3`
- `internal/types/bridge.go:5`
- `internal/types/bridge.go:7`
- `internal/types/bridge.go:8`
- `internal/ft8/txkeyer.go:20`
- `internal/ft8/txkeyer.go:21`
- `internal/bridge/events.go:24`
- `internal/bridge/events.go:25`
- `internal/bridge/pipeline.go:559`
- `internal/bridge/pipeline.go:565`
- `internal/bridge/hub_test.go:90`
- `internal/bridge/hub_test.go:92`

Several active comments no longer match the current behavior:

- `internal/bridge/doc.go` says FT8 stack / PTT-for-operating are not in v1,
  but this package now has `ft8tx.go` and is wired as the FT8 keyer.
- `internal/types/bridge.go` still describes bridge config as read-only with no
  inbound command path and no PTT awareness.
- `TxKeyer.TxReady` says readiness is connected + identity verified, but
  `bridge.TxReady` also correctly gates on no active tune/FT8 transmission.
- `EventRigDisconnected` says CAT identity check failure fires
  `rig-disconnected`, while a definite mismatch currently publishes a permanent
  `bridge-error` and exits.
- `hub_test.go` describes identity mismatch as advisory and followed by
  `rig-state`, but the current pipeline exits permanent on mismatch.

Impact:

- The stale comments obscure which bridge paths can transmit and how state
  conflicts are surfaced.
- This is especially risky around safety reviews, because the active docs make
  the package look less capable than the code is.

Suggested fix:

- Refresh the package/config/interface comments to describe the current read and
  write surfaces: rig events, rig commands, tune, FT8 keying, identity gates, and
  keyed-state readiness.
- Keep historical ADRs historical, but make active Go doc and tests describe
  current runtime behavior.

## Security notes

- I did not find a direct authorization/authentication issue inside
  `internal/bridge`; it is an internal daemon package behind the API wiring.
- The important hardware-control security property, "do not write to an
  unverified or definitely mismatched rig", is consistently represented in the
  current code paths reviewed: `SendCommands`, `StartTune`, and `KeyFt8Tx` all
  check identity before writing, and a definite identity mismatch exits the
  pipeline permanently.
- The remaining security-relevant concerns are reliability/taxonomy issues:
  sequence-level CI-V write ordering, stale dial state used by integrations, and
  conflict errors that should be typed rather than treated as server failures.

## Test coverage notes

Strong coverage already exists for:

- SSE hub replay and per-write deadline behavior.
- Serial pipeline startup, identity classification, disconnect/recovery, and
  decoded rig-state mapping.
- Generic command validation, command batching, identity gates, and CI-V
  ACK/NAK/no-ACK behavior.
- Tune and FT8 key/unkey single-flight, auto-off, disconnect release, CI-V
  key/unkey ACKs, and race-sensitive release paths.

Coverage gaps tied to the findings:

- No CI-V test asserts `CurrentDialMHz()` after a synthesized ACKed frequency
  command.
- No CI-V test proves `TriggerBootstrap` and liveness READ writes serialize
  behind an in-flight command/key sequence.
- Mutual-exclusion tests assert an error exists but not its sentinel/taxonomy,
  and the API tune handler does not cover tune-during-FT8.
- Documentation comments are not pinned by tests; they need direct maintenance.

## Verification

Commands run during this review:

- `GOCACHE=/tmp/go-build go test ./internal/bridge ./internal/cat ./internal/api ./internal/ft8 -count=1`
  - First sandboxed attempt failed because `httptest.NewServer` could not bind
    loopback in the restricted environment.
  - Rerun with loopback allowed: pass.
- `GOCACHE=/tmp/go-build go test -race ./internal/bridge ./internal/ft8 -count=1`
  - Pass.
- `GOCACHE=/tmp/go-build go test -race ./internal/api`
  - Pass.
- `GOCACHE=/tmp/go-build go vet ./internal/bridge ./internal/cat ./internal/api ./internal/ft8`
  - Pass.
- `GOCACHE=/tmp/go-build go test ./cmd/smd -count=1`
  - Pass.

## Resolution (2026-06-19)

All four findings addressed.

- **M1 (fixed).** `publishCommandedState` now calls `captureDialFreq(payload)`
  alongside `captureTuneSnapshot(payload)` before publishing, so an ACKed CI-V
  `set_freq` updates `CurrentDialMHz` immediately (both helpers no-op on a
  payload lacking their fields, so mode/power-only commands are unaffected).
  Test: `TestSendCommandsCIV_UpdatesDialSnapshotOnAck`.
- **M2 (fixed).** Added `underCmdMuCIV(civ, fn)` — runs the write under `cmdMu`
  for CI-V, directly otherwise — and routed every CI-V snapshot writer through
  it: the startup snapshot, `TriggerBootstrap`, the poll loop (replacing its
  manual lock), and the liveness-recovery INIT+READ (now one atomic sequence
  under the lock). Lock order keyMu→cmdMu→mu preserved (no caller holds mu/keyMu
  when entering; `writeSnapshotReads` takes no lock). Test:
  `TestTriggerBootstrapCIV_SerialisesBehindCmdMu` (race-clean, ×3).
- **L1 (fixed).** `StartTune` (FT8 active) and `KeyFt8Tx` (tune active) now wrap
  their mutual-exclusion conflict with `ErrTxActive`, and `writeRigTuneError`
  maps it to 409 `rig_tx_active` (was a 500 `rig_tune_failed`). Bridge tests
  `TestKeyFt8Tx_RefusesDuringTune` / `TestStartTune_RefusesDuringFt8Tx` now
  assert `errors.Is(err, ErrTxActive)`; new
  `TestWriteRigTuneError_TxActiveIsConflict` pins the handler mapping.
- **L2 (fixed).** Refreshed the stale comments to match current behavior: the
  bridge `doc.go` trigger-list and `types.BridgeConfig` doc now describe the
  shipped inbound command path (ADR 0026) + tune (ADR 0027) + FT8 keying (ADR
  0030); `TxKeyer.TxReady` documents the no-active-tune/FT8 gate; and
  `EventRigDisconnected` + the `hub_test` comment now correctly state that a
  definite identity mismatch is a *permanent bridge-error that halts the
  pipeline* (an unrecognised ID is the advisory case), not `rig-disconnected`.

Verified: `gofmt`/`go vet` clean; `go build ./...`; `internal/bridge`,
`internal/api`, `internal/types`, `cmd/smd` pass; `go test -race
./internal/bridge` clean.
