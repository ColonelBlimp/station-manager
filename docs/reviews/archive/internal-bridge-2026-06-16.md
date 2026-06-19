# Code Review: internal/bridge — TX safety (keying / command path)

Date: 2026-06-16

Scope reviewed:

- `internal/bridge` keyed-transmission paths (`tune.go`, `ft8tx.go`, `command.go`)
- The guaranteed-stop / single-flight safety model (ADR 0027 / ADR 0030 / ADR 0034)
- CI-V (`icom_civ`) wait-for-ACK contract vs the FT8/tune key/unkey writes

Verification (post-fix):

- `go test ./...` (CGO on): pass.
- `go test -race -count=1 ./internal/bridge ./internal/ft8`: pass.
- `go vet ./internal/bridge ./internal/cat ./internal/api ./internal/ft8`: pass.
- `CGO_ENABLED=0 go build ./...`: pass.

## Findings

### High: Generic rig commands could write while PTT is active

`SendCommands` (`command.go`) gated only on `activeClient` + `identityConfirmed`,
not on `tuneActive` / `ft8TxActive`. So `POST /v1/rig/command` could change
frequency/mode/power during a tune carrier or an FT8 transmission. Worse than the
"one keyed transmission at a time" violation: `set_power` is **Exposed**, so a
command could override the ADR 0027 tune-power clamp and transmit at full power.

### High: CI-V FT8 key/unkey bypassed the ACK contract

The generic CI-V path (`sendCommandsCIV`) waits for each frame's FB/FA ACK, but the
FT8/tune key+unkey used plain `WriteCommandBytes`. On an `icom_civ` rig (IC-7300)
`tx_on`/`tx_off` are CI-V commands the rig confirms with FB; the bridge reported
success on bytes-written alone. The dangerous case: a rig that NAKs or never ACKs
`tx_off` looked like a clean unkey → the release cancelled the auto-off backstop →
**PTT stranded with no backstop**. `tx_on` could also follow an unconfirmed mode switch.

### Medium: Tune/FT8 release paths not serialized

`releaseFt8Tx` / `releaseTune` read `active`, unlocked, then wrote + settled +
restored and only cleared `active` at the end (`finishFt8Tx` / `finishTune`). Two
concurrent releases (operator stop + auto-off, or a double stop) could both pass the
`active` check and run; a stale one could fire an old `tx_off`/restore over a new
transmission. The ≥150 ms settle makes the window real.

### Lower: `TxReady` over-reported during active tune/FT8

`TxReady` checked only connection + identity, so an active tune still reported
"ready" to the FT8 arm/transmit gate, producing a false-accepted flow that then
failed at `KeyFt8Tx`.

## Resolution (2026-06-16)

All four fixed; regression tests added; full verification above is green.

- **H1 — command-during-PTT.** New `ErrTxActive` sentinel; `SendCommands` refuses
  when `tuneActive || ft8TxActive`. API maps it to **409 `rig_tx_active`**
  (`handler_rig_command.go`). Tests: `TestSendCommands_RefusedWhileTransmitting`
  (both flags, asserts no wire write).
- **H2 — CI-V key/unkey ACK.** Factored the per-frame write-then-await-ACK loop out
  of `sendCommandsCIV` into `writeCIVFramesAwaitAck` (lock-free, caller holds
  `cmdMu`), and added protocol-aware `writeKeyedLine`: CI-V waits for FB/FA per
  frame, other protocols stay fire-and-forget (unchanged). Used by **both** the FT8
  (`KeyFt8Tx`/`releaseFt8Tx` + mode restore) and tune (`StartTune`/`releaseTune` +
  restore) paths. A NAK/no-ACK `tx_off` now keeps TX armed so the backstop retries
  rather than stranding PTT. Tests: `TestKeyUnkeyFt8TxCIV_ConfirmedByAck`,
  `TestUnkeyFt8TxCIV_NoAckKeepsArmed`.
- **M — release serialization.** New `keyMu` held across the full
  `KeyFt8Tx`/`StartTune`/`releaseFt8Tx`/`releaseTune` bodies (shared with key, so no
  key starts mid-release). Lock order `keyMu → cmdMu → mu`. Second release observes
  `active=false` and no-ops. Tests: `TestReleaseTune_ConcurrentStopsReleaseOnce`
  (exactly one `tx_off`), `TestKeyRelease_ConcurrentStartStopNoDeadlock` (race
  detector).
- **L — `TxReady`.** Now also requires `!tuneActive && !ft8TxActive`. Test:
  `TestTxReady_FalseWhileTransmitting`.

Note: the CI-V poll loop already holds `cmdMu` (`pipeline.go:354`), and
`writeKeyedLine`'s CI-V branch takes it too, so a poll's FB can never be misrouted
into a key/unkey ACK wait. H1 additionally keeps `SendCommands` out of `cmdMu` while
a transmission is keyed.

**On-air note:** H2 changes the bench-validated IC-7300 FT8 TX path (it now waits
for the `tx_on`/`tx_off` ACKs). Re-validate FT8 TX on the IC-7300 before relying on
it on air; the FTdx10 (Yaesu, fire-and-forget) path is unchanged.
