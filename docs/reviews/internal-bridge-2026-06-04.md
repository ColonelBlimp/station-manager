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

---

## Triage / recommendation (2026-06-04) — analysis only, NO code changed yet

Each finding was validated against the actual code (five parallel read-only
passes, exact `file:line` evidence). **All 9 are code-accurate — none false.**
Three are *narrower in real-world impact* than their label, given this is a
single-operator, single-rig (FTdx10) deployment with the QSO **log** write path
untouched — the risks are all on the newer rig-**control** surface. Verdicts
below; the two open **decisions** and the **batch plan** are at the end. The
fix sketches are the intended approach for pickup; re-read the cited code before
editing.

### H1 — split `ON+` → `splitOverride=false`. **FIX (med-high).**
- Evidence: `mapStatusToPayload` `pipeline.go:540-547` does `split := strings.EqualFold(v, "ON")`; `EqualFold("ON+","ON")` is false. Rigdef `yaesu-ftdx10.json` maps ST `0/1/2 → OFF/ON/ON+`. `pipeline_test.go` covers ST0/ST1, **not** ST2.
- Accurate. Real **iff** the FTdx10 actually emits ST2 (likely "quick split") — the review didn't prove emission; the rigdef author added the mapping deliberately. Consequence if emitted: SPA shows split OFF → wrong TX/RX freq logged (a logged-ADIF data-integrity concern the operator cares about). `SplitOverride` is `*bool`, which is fine — the boolean means "split engaged", and ON+ should be `true`.
- Fix: `split := !strings.EqualFold(v, "OFF")` (any decoded non-OFF state ⇒ engaged) + an ST2 test asserting `*SplitOverride == true`. ~1 line + test.

### H2 — identity mismatch leaves command/**tune** path live. **FIX (high) — needs a decision (below).**
- Evidence: `readLoop` gates on a *local* `identityVerified` bool (`pipeline.go:307`); after publishing `BridgeErrCodeIdentityMismatch`/`Unrecognised` it **falls through** to `mapStatusToPayload`/`captureTuneSnapshot`/`hub.publish` with no `return`/`break` (`pipeline.go:385-404`) and keeps looping. `activeClient` is set **before** any identity check (`pipeline.go:219-221`) and cleared only on pipeline teardown (`:175-178`) — never on mismatch. `SendCommands` guards only `cl == nil` (`command.go:68`); `StartTune` guards single-flight + snapshot + `activeClient`, **no identity** (`tune.go:91-104`). The identity error uses `publishBridgeError` (advisory) not `publishExitBridgeError`.
- Accurate. Severity is **moderate not high** for this codebase (trigger = a `bridge.cat.driver` typo, single-operator localhost; the two shipping rigdefs are both Yaesu so cross-driving is plausible). Sharper edges the review under-stressed: (a) the **tune path transmits**, and `StartTune`'s snapshot guard (`ErrTuneStateUnknown`) only *incidentally* blocks a truly-foreign rig; (b) a rig that **never** sends a parseable IDENTITY leaves `identityVerified=false` forever, publishes *no* error, yet the write path stays live — a wider hole than mismatch. Note: `exitPermanent`'s own doc comment (`pipeline.go:54-58`) **lists "identity mismatch" as permanent**, but the code never implements it.
- Fix: track identity-verified as **service state**; gate the write paths (`SendCommands`, `StartTune`, and bootstrap) on it (new `ErrRigIdentityUnverified`); and/or on a definite mismatch close the client + `return exitPermanent`. See decision (1).

### H3 — tune-off can silently omit `tx_off` (keyed TX, falsely reported down). **FIX (high).**
- Evidence: `encodeTuneOff` discards the `tx_off` encode error (`tune.go:290-306`, the `if … ; err == nil` at `:292`); `releaseTune` writes the bytes and calls `finishTune` (publishes `active=false`) on any nil write error (`:167-173`); serial treats an **empty** write as success (`serial.go:204`). The ON path *does* validate loudly (`encodeTuneOn` returns the `tx_on` error; `StartTune` aborts before keying). `TuneSupported` **does** require `tx_off` (`tune.go:312-316`) **but is only wired to the SPA UI hint** `BridgeInfo.Tune` (`handler_config.go:390`) — `handleRigTune`/`StartTune` never call it, so a direct `POST /v1/rig/tune` bypasses the gate.
- Accurate, **narrowed**: not reachable on the FTdx10 (it has `tx_off`). It's a future-rigdef-drift foot-gun — a rigdef with `tx_on` but missing/broken `tx_off` keys TX in `StartTune`, then strands the carrier while reporting `active=false`. Directly defeats the ADR-0027 "daemon owns the guaranteed stop" invariant; the retry backstop can't save it (it re-arms only on *write* error, and an empty/partial line writes as success). Cheap to harden, high safety value.
- Fix: in `StartTune`, validate full tune capability incl. `tx_off` **before** keying (call `TuneSupported`, or encode the off-line up front); make `encodeTuneOff` return `([]byte, error)` and require `tx_off` to encode; guarantee `tx_off` leads the release line, restore mode/power best-effort *after*.

### H4 — tune auto-off not bounded by a write timeout. **FIX comment now; real deadline = investigate.**
- Evidence: comment claims "the serial write timeout bounds it" (`tune.go:191`) but the impl calls `releaseTune(context.Background(), "auto-off")` (`:198`). Serial sets a **read** timeout only (`serial.go:149`); `WriteCommandBytes` checks ctx only *between* writes and can't interrupt a blocking `port.Write` (`serial.go:218-225`). `RigSerial.WriteTimeoutMS` exists (`rig.go:41`) but `buildSerialConfig` drops it (`pipeline.go:602-610`) and `serial.Config` has no write-timeout field. Retry re-arms only on `releaseTune` error → a hung write = no retry, and it also blocks a manual `StopTune` on `writeMu`.
- Accurate; latent (tiny CAT writes to USB-CDC rarely block; needs a driver/HW fault). The **in-code safety claim is false** — fix that now. The *real* write-bound is harder than the review implies: Go can't interrupt a blocking `port.Write` via context, so it needs an OS write deadline — and `go.bug.st/serial` likely exposes only `SetReadTimeout`. **Pickup task:** check whether the serial lib supports a write deadline; if yes, plumb `write_timeout_ms` (couples with M3); if no, document the limit and consider a close-port escape hatch.

### M1 — transient bridge-errors cached forever, replay after recovery. **FIX (med).**
- Evidence: `hub.publish` caches *every* `EventBridgeError` in `lastBridgeError` (`hub.go:93-96`); the `EventRigState` case clears only `lastRigDisconnected`, never `lastBridgeError` (`:103-112`); `subscribe` replays it to every new subscriber (`:149-154`). `serial_open_failed` (`pipeline.go:161`) and `init_write_failed` (`:191`) are `exitTransient`. Never cleared during the Service lifetime (deliberate "once observed, stays observable" — but wrong for the transient subset).
- Accurate; cosmetic but on the **normal first-boot path** (daemon started before rig power-on → cached `serial_open_failed`; supervisor recovers; a tab opened *after* recovery still gets the stale toast).
- Fix: distinguish transient vs permanent bridge-errors; clear the transient `lastBridgeError` on `EventRigState` (permanent codes — `unknown_driver` etc. — never see a RigState, so stay cached correctly). Composes with a fixed H2 (mismatch halts → no RigState → identity error persists). Add the supervisor recovery test the review suggests.

### M2 — SSE writes can block after clearing the write deadline. **FIX (med-low).**
- Evidence: handler clears the deadline once (`SetWriteDeadline(time.Time{})`, `handler.go:42-45`), then writes frames + keepalives synchronously on the handler goroutine (`:77-106`); no per-write deadline; this escapes the server's 30s `WriteTimeout` (`server.go:221`). While blocked it can't observe `shutdownCh`/unsubscribe.
- Accurate but **bounded**: the hub already evicts slow subscribers (non-blocking send; close+delete on the 64-deep buffer, `hub.go:113-120`), so a stalled client stops receiving after ≤64 queued events and never back-pressures the publisher. Only a *wedged* peer (holds socket open, never reads, never RSTs) hangs the one goroutine until TCP times out. Operator's network is "slow/unreliable", so not impossible.
- Fix: bounded write deadline around each event/keepalive write+flush (via `http.NewResponseController`), reset/extend for the next idle period. Add a slow-client test.

### M3 — rigdef `write_timeout_ms`/`RTS`/`DTR` parsed but dropped. **IGNORE as standalone.**
- Evidence: `RigSerial` declares all three (`rig.go:34-44`); both rigdefs set `rts:true`/`dtr:true`/`write_timeout_ms:20`; `buildSerialConfig` drops them (`pipeline.go:602-610`); `serial.Config` has no fields for them.
- Accurate but **~nil present impact**: `go.bug.st/serial` defaults DTR=true/RTS=true when `InitialStatusBits==nil` (which SM always passes), so the dropped `true` values **coincide with the library default**; for USB-CDC Yaesu rigs RTS/DTR aren't flow-control anyway, and the dropped write-timeout guards writes that never block. Only bites a *future* rigdef wanting `rts:false`/`dtr:false`.
- Disposition: don't fix standalone. Fold `write_timeout_ms` into H4 *if* the serial lib supports a write deadline; for RTS/DTR add a doc-note (or a small `SetRTS`/`SetDTR` plumbing) — low priority.

### L1 — `doc.go` still says "read-only / no inbound command path". **FIX (trivial).**
- Evidence: `doc.go:7-11` is contradicted by `command.go` (ADR 0026 inbound commands) and `tune.go` (ADR 0027 TX keying). Per "keep all docs current", and the stale wording masks *why* the identity/tune-unkey guarantees now need to be stricter.
- Fix: rewrite the package doc for the current read/**write** shape + the command + tune-safety constraints.

### L2 — handler test barriers weakened by the post-INIT READ. **FIX (cheap).**
- Evidence: `TestHTTPHandler_BootstrapFiresOnSubscribe` (`handler_test.go:240-272`) waits `>=1` then `>=2` writes and checks `writes[1]` — but the pipeline's post-INIT READ (`pipeline.go:209`) means `writes[1]` can be the **startup** READ, not the on-subscribe bootstrap (masked because both encode identically → green without proving the invariant). `TestHTTPHandler_FanOutToMultipleSubscribers` (`:332-340`) waits `numClients+1` but should now be `numClients+2` → real flake potential. `hub.subscriberCount()` **exists** and is purpose-built (`hub.go:206-210`).
- Fix: switch both barriers to `s.hub.subscriberCount()`; assert bootstrap count explicitly.

### Decisions needed before implementing

1. **H2 strictness** — (a) block writes only (keep read-only state for diagnosis, gate `SendCommands`/`StartTune` on a verified flag); (b) halt entirely on mismatch (close client + `exitPermanent`, matches the doc's intent, loses diagnostic state); (c) both (recommended — block writes always + permanent-exit on a *definite* mismatch). Also decide whether the *no-IDENTITY-ever* rig should be treated as unverified (block writes) — recommended yes.
2. **H4 depth** — fix the false comment now regardless; decide whether to pursue a real write deadline (needs a `go.bug.st/serial` capability check; couples with M3's `write_timeout_ms`) or just document the limitation.

### Proposed batches (priority order)

- **Batch A — Tune-safety (H3 + H4-comment).** `tune.go`: require full tune capability incl. `tx_off` in `StartTune` before keying; `encodeTuneOff` → `([]byte, error)` with `tx_off` leading; fix the misleading auto-off comment. Highest safety value (defends ADR-0027 guaranteed-stop).
- **Batch B — Identity write-gate (H2).** Needs decision (1).
- **Batch C — Split `ON+` (H1).** `mapStatusToPayload` `!EqualFold(v,"OFF")` + ST2 test.
- **Batch D — Hub transient-error clearing (M1).** Transient/permanent split; clear transient on recovery + supervisor test.
- **Batch E — SSE per-write deadline (M2).**
- **Batch F — Hygiene (L1 doc.go + L2 test barriers + M3 disposition).**

Nothing here touches the QSO log write path. All `go test ./internal/bridge`
(+ `-race`) must stay green; each batch adds the test(s) named in its finding.

## Resolutions

### Batch A — Tune-safety (H3 + H4 comment). **DONE 2026-06-05.**
`internal/bridge/tune.go` + `tune_test.go`:
- **H3 fixed.** `encodeTuneOff` now returns `([]byte, error)` and requires
  `tx_off` to encode — a rigdef with `tx_on` but no usable `tx_off` yields an
  error instead of a line that silently omits the unkey. `releaseTune` handles
  that error by staying armed + logging loudly and **not** calling `finishTune`,
  so the carrier is never falsely reported down when no TX-off was sent.
  `StartTune` gained a pre-key gate (`cat.Encode(def, tx_off)`) that refuses to
  key TX unless the unkey is proven encodable — defending the ADR-0027
  guaranteed-stop invariant at key-time. New test `TestEncodeTuneOff_RequiresTxOff`;
  `TestEncodeTuneOff` updated for the new signature.
- **H4 comment fixed.** `tuneAutoOff`'s doc no longer claims "the serial write
  timeout bounds it" (false — the serial layer sets only a read timeout and a
  blocking `port.Write` isn't ctx-interruptible). A real write deadline remains
  open work (needs a `go.bug.st/serial` capability check; couples with M3's
  `write_timeout_ms`).
- Note: the StartTune negative branch (tx_off missing) is not independently
  reachable with the two shipped rigdefs (FTdx10 fully tune-capable; FT-710 has
  no tune commands at all, so `encodeTuneOn` fails first), and `cat.rigDB` has
  no test-injection hook — so the `tx_off`-missing contract is asserted at the
  `encodeTuneOff` unit level instead. `go test ./internal/bridge ./internal/cat`
  + `-race` green; `go vet` + `gofmt` clean.

**Separately surfaced (NOT a review finding): tune mode-restore bug — task #270.**
Operator reproduced on the FTdx10 that a tune cycle goes USB → RTTY-U correctly
but does **not** restore to USB on stop (stays in RTTY-U); power restore works.
The restore code path exists (`encodeTuneOff` appends `set_mode(restoreMode)`
and the round-trip is test-pinned in `cat/commands_test.go`), so the failure is
likely wire-ordering/timing on the rig (MD0 sent in the same burst right after
TX0 may be ignored) rather than a missing-restore. Tracked for separate
investigation in #270; out of scope for Batch A.
