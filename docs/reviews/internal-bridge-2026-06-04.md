# Code Review: internal/bridge

Date: 2026-06-04

Scope reviewed:

- `internal/bridge`
- Related `internal/cat` rigdef and command contracts where bridge behavior depends on them
- Related `internal/serial` write/read semantics where bridge safety depends on them

Verification:

- `go test ./internal/bridge`: pass when rerun with localhost listener permission.
- `go test -race ./internal/bridge`: pass when rerun with localhost listener permission.
- Initial sandboxed `go test ./internal/bridge` failed before test execution because `httptest.NewServer` could not bind `tcp6 [::1]:0` under the sandbox.

## Findings

### High: FTdx10 split `ON+` is decoded as split off

The FTdx10 rigdef maps `ST2` to `SPLIT=ON+` (`internal/cat/rigs/yaesu-ftdx10.json:62`). `mapStatusToPayload` converts a decoded split value to a boolean with `strings.EqualFold(v, "ON")` (`internal/bridge/pipeline.go:540`). That makes `ON+` publish `splitOverride=false`.

Impact:

- A real FTdx10 reporting split `ON+` tells the SPA that split is off.
- The logging SPA derives TX/RX frequency behavior from the split flag, so this can produce incorrect displayed state and incorrect QSO frequency fields.
- Existing tests cover `ST1` and `ST0` but not `ST2` (`internal/bridge/pipeline_test.go:146`).

Recommended fix:

- Treat any decoded split value other than an explicit `OFF` as true, or add a rigdef-level boolean normalization for split values.
- Add a test with FTdx10 `ST2` that expects `SplitOverride != nil && *SplitOverride == true`.

### High: Identity mismatch publishes an error but leaves the command/tune path live

The read loop verifies the first `IDENTITY` response and publishes `BridgeErrCodeIdentityUnrecognised` or `BridgeErrCodeIdentityMismatch` when the rig does not match the configured driver (`internal/bridge/pipeline.go:385`). It then continues through `mapStatusToPayload`, `captureTuneSnapshot`, and `hub.publish` (`internal/bridge/pipeline.go:397`). The pipeline remains active.

The service also marks `activeClient` before any identity has been verified (`internal/bridge/pipeline.go:219`). `SendCommands` only checks that `activeClient` is non-nil, then encodes commands using the configured driver and writes them to the port (`internal/bridge/command.go:51`, `internal/bridge/command.go:65`). The tune controller has the same underlying active-client trust model.

Impact:

- A misconfigured daemon can keep decoding and publishing state from a rig that already identified as a different model.
- More importantly, outbound commands can be encoded using the wrong rigdef and sent to the wrong radio.
- This risk grew when the bridge stopped being read-only and gained generic commands plus tune carrier control.

Recommended fix:

- Track identity verification as service state, not just a local read-loop boolean.
- Do not expose `activeClient` to `SendCommands`, `StartTune`, or `TriggerBootstrap` until identity is verified, or add a separate `ErrRigIdentityUnverified`.
- On identity mismatch/unrecognised identity, close the client and return a permanent pipeline exit instead of continuing.
- Add tests that after an identity mismatch no later rig-state is published and `SendCommands`/`StartTune` fail.

### High: Tune-off encoding can silently omit `tx_off`

`StartTune` validates the tune-on line by calling `encodeTuneOn` before committing tune state (`internal/bridge/tune.go:88`). The release path does not validate the safety-critical tune-off line: `releaseTune` calls `encodeTuneOff`, writes whatever bytes it returns, and then calls `finishTune` on any nil write error (`internal/bridge/tune.go:165`).

`encodeTuneOff` silently ignores failures for every component, including `tx_off` (`internal/bridge/tune.go:290`). If a future rigdef contains `set_mode`, `set_power`, and `tx_on` but is missing or breaks `tx_off`, `StartTune` can key the transmitter, while `StopTune`/auto-off can write only restore commands or an empty command. The serial layer treats an empty write as success (`internal/serial/serial.go:204`), after which `finishTune` publishes `active=false`.

Impact:

- The daemon can report that the tune carrier is down without actually sending an unkey command.
- This is a rigdef drift foot-gun in the safety-critical path.

Recommended fix:

- Validate complete tune capability, including `tx_off`, in `StartTune` before mutating tune state.
- Make tune-off encoding return `([]byte, error)` and require `tx_off` to encode successfully.
- Treat failed restore-mode/power encoding as best-effort only after `tx_off` is guaranteed to be first in the line.
- Add a test rigdef missing `tx_off`: `StartTune` must fail before any write and `StopTune` must not mark the tune inactive after an empty/no-unkey write.

### High: Tune auto-off is not actually bounded by a write timeout

The auto-off comment says the background safety action is bounded by "the serial write timeout" (`internal/bridge/tune.go:191`). The implementation calls `releaseTune(context.Background(), "auto-off")` (`internal/bridge/tune.go:197`), so the bridge provides no cancellation deadline of its own.

The serial layer configures read timeout only (`internal/serial/serial.go:149`), and `WriteCommandBytes` checks context before each `port.Write` call but cannot interrupt a blocking `port.Write` (`internal/serial/serial.go:218`, `internal/serial/serial.go:225`). The rigdef has `write_timeout_ms` available (`internal/cat/rig.go:41`), but `buildSerialConfig` drops it because `serial.Config` has no write-timeout field (`internal/bridge/pipeline.go:602`).

Impact:

- If the OS/driver blocks a serial write during auto-off, the timer callback can block indefinitely.
- The retry loop only re-arms after `releaseTune` returns an error, so a stuck write means no retry and the safety guarantee is weaker than the code claims.

Recommended fix:

- Add write timeout support to `serial.Config` and bridge `buildSerialConfig`, or wrap every tune unkey attempt in a short `context.WithTimeout`.
- Avoid `context.Background()` for safety-critical writes unless the lower layer has an enforced write deadline.
- Add a fake serial client whose `WriteCommandBytes` blocks until context cancellation; assert auto-off returns/retries on the configured deadline.

### Medium: Transient bridge errors are cached forever and replay after recovery

The hub caches every `EventBridgeError` and intentionally never clears it during the service lifetime (`internal/bridge/hub.go:29`). The publish path stores all bridge errors in `lastBridgeError` (`internal/bridge/hub.go:91`), and subscribe replays that cached event to every new subscriber (`internal/bridge/hub.go:146`).

Some cached bridge errors are transient supervisor-cycle errors, not permanent misconfiguration:

- Serial open failure is published as a bridge error and classified transient (`internal/bridge/pipeline.go:154`).
- INIT write failure is also transient and retried by the supervisor.

When a later pipeline run succeeds and rig-state starts flowing, `hub.publish` clears only the cached rig-disconnected event on `EventRigState`; it does not clear `lastBridgeError` (`internal/bridge/hub.go:103`).

Impact:

- Operator starts daemon before powering the rig.
- Bridge publishes and caches `serial_open_failed`.
- Supervisor later reconnects successfully.
- A SPA tab opened after recovery still receives the stale `serial_open_failed` bridge-error toast.

Recommended fix:

- Distinguish permanent bridge errors from transient retry errors in the event/cache layer.
- Clear transient cached bridge errors when a pipeline reaches verified rig-state or survives the steady-state threshold.
- Add a supervisor test: fail open once, recover, publish rig-state, late subscriber should not receive stale `serial_open_failed`.

### Medium: SSE writes can block indefinitely after clearing the HTTP write deadline

The rig SSE handler clears the response write deadline for the lifetime of the stream (`internal/bridge/handler.go:39`). It then writes event frames and keepalives synchronously in the select loop (`internal/bridge/handler.go:87`, `internal/bridge/handler.go:100`).

Impact:

- If a client or proxy stops reading while keeping the TCP connection open, a write or flush can block indefinitely.
- While blocked, the handler cannot observe `shutdownCh`, cannot unsubscribe promptly, and can hold resources until the kernel/socket stack returns.
- This risk is especially relevant for SSE because the code deliberately disables the server's normal write timeout.

Recommended fix:

- Set a bounded write deadline around each event/keepalive write and flush, then clear or extend it for the next idle period.
- Add a slow-client test with a response writer that blocks writes and assert the handler exits on deadline/shutdown.

### Medium: Rigdef modem-control/write-timeout fields are parsed but ignored by the bridge

`cat.RigSerial` includes `WriteTimeoutMS`, `RTS`, and `DTR` (`internal/cat/rig.go:34`), and the shipping Yaesu rigdefs set these fields. `buildSerialConfig` maps baud/data/parity/stop/delimiter/read-timeout into `serial.Config` but drops write timeout, RTS, and DTR (`internal/bridge/pipeline.go:602`).

Impact:

- The rigdef appears to declare port behavior that the running bridge does not apply.
- If a supported or future rig actually requires RTS/DTR assertion, bridge startup may fail or behave intermittently despite a correct rigdef.
- The dropped write timeout also contributes to the tune auto-off issue above.

Recommended fix:

- Either plumb these fields through `serial.Config` and apply them in `serial.Open`, or remove them from rigdefs until the code honors them.
- Add a build-serial-config test that proves every non-zero rigdef serial field is either applied or intentionally rejected.

### Low: Package documentation still describes the bridge as read-only

`doc.go` still says the v1 shape is "read-only" with "no inbound command path" (`internal/bridge/doc.go:7`). That is stale: `command.go` implements generic outbound rig commands, and `tune.go` keys and unkeys the transmitter.

Impact:

- The package-level safety and boundary summary is misleading for future reviewers.
- The stale wording masks the reason identity verification and tune unkey guarantees now need to be stricter.

Recommended fix:

- Update `doc.go` to describe the current read/write bridge shape, including command and tune safety constraints.

### Low: Handler bootstrap/fan-out tests use weakened subscription barriers

Several handler tests use recorded write counts as a proxy for "the HTTP handler subscribed and bootstrapped." That became weaker after `runPipeline` started writing a post-INIT READ before any HTTP client connects:

- `TestHTTPHandler_BootstrapFiresOnSubscribe` waits for one startup write, then expects the next write to prove bootstrap (`internal/bridge/handler_test.go:260`). The next write can already be the pipeline's post-INIT READ.
- `TestHTTPHandler_FanOutToMultipleSubscribers` waits for `numClients+1` writes, but the pipeline now contributes INIT plus post-INIT READ, so the barrier can pass before all clients subscribe (`internal/bridge/handler_test.go:332`).

Impact:

- These tests can pass without proving the handler-level invariant they claim.
- The direct `TriggerBootstrap` test still covers the service method, but the HTTP-level tests are easier to regress or flake.

Recommended fix:

- Use `s.hub.subscriberCount()` as the test barrier for handler subscription readiness.
- Gate on `2 + numClients` writes when asserting bootstrap READs, or assert both subscriber count and bootstrap count explicitly.

## Review Notes

The package has unusually strong lifecycle and retry coverage for a hardware-facing subsystem, and `go test -race ./internal/bridge` is clean. The main risks are in the newer outbound-control surface: identity mismatch handling, tune-off safety, and write timeout assumptions need to be treated as part of the same safety boundary rather than as ordinary SSE/pipeline details.
