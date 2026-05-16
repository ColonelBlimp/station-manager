---
number: 0020
title: Bridge pipeline supervises and retries transient failures
status: Accepted
date: 2026-05-16
---

# 0020 — Bridge pipeline supervises and retries transient failures

## Context

The bridge pipeline (`internal/bridge/runPipeline`, shaped under ADRs 0013 + 0019) was originally a one-shot goroutine: open the serial port, send INIT (`AI1;`), drop into a read loop, and on any failure publish a `bridge-error` / `rig-disconnected` event and return. The supervising `Service.Start` spawned it once and never spawned it again — recovery required a daemon restart.

Two startup orderings collide with that shape:

1. **PC up first, rig up later.** This is the recommended sequence — PC power-on can send spurious bytes down the CAT line, so operators boot the PC, settle, then power the rig. With the FTdx10 / FT-710 family (built-in USB CAT), the `/dev/ttyUSBn` node only appears when the rig is powered on, so `serial.Open` fails at daemon startup with "no such file or directory". The original pipeline published one `bridge-error` and exited; the rig coming online later was invisible to the daemon, and the SPA stayed in manual mode until the operator restarted `smd`.

2. **Mid-session power disruption.** A power spike, a momentarily-loose USB plug, or the rig auto-resetting drops the serial connection. The original pipeline's terminal-read-error branch published `rig-disconnected` and returned — leaving the operator with a live SPA and no path back to bridge-driven state without restarting the daemon mid-QSO.

Both cases share a shape: the rig is *eventually* available again, the daemon just doesn't know it.

This change is also load-bearing in a way the original M3a scope didn't expose: AUTO mode is not on by default; the rig must receive `AI1;` to start pushing. Even if the port stayed open across a reconnect, the daemon would have to re-send INIT for the AUTO data flow to resume — meaning the recovery path can't be "just keep waiting on the same client," it has to actually re-run the pipeline.

## Decision

`Service.Start` now launches `runSupervisor` instead of `runPipeline` directly. The supervisor classifies each `runPipeline` return as **permanent** (config / rigdef error — retrying won't fix it), **transient** (runtime fault — retry with backoff), or **context-cancelled** (deliberate shutdown — return silently). On a transient exit it sleeps for an exponentially-backed-off interval (1s start, doubling, capped at 30s — the same window as `livenessTimeout`) and re-enters `runPipeline`. INIT is re-sent on every pipeline restart because `runPipeline` is unchanged in that regard, so AUTO mode is re-armed automatically.

To prevent the SPA toast stream from flooding while the supervisor retries the same failure every few seconds, exit-causing publishes go through `publishExitBridgeError` / `publishExitDisconnect` helpers that compare against a `Service.lastPublishedExitKey` dedup token. The supervisor clears the token after a pipeline run survives past 10 seconds, so a different failure later (or the same failure after a successful session) surfaces cleanly.

## Alternatives considered

### Fail-fast, operator restarts the daemon

The original behaviour. Simple, but it makes both the FTdx10 first-boot ordering and mid-session power disruption recovery require a daemon restart. For first-boot the operator has to remember to start the daemon AFTER powering the rig (reversing the recommended order); for mid-session the live SPA loses CAT mid-QSO with no recovery path. Rejected because the daemon is configured to auto-start via systemd, so "start it manually after the rig" isn't a real option without removing the systemd unit.

### Operator manually starts the daemon after rig power-on

A deliberate-init path: the daemon does NOT auto-start; the operator runs `systemctl start smd` after switching on the rig. Solves the first-boot ordering by inverting the question. Rejected because it doesn't help the mid-session case at all — a power-spike during operation would still wedge the bridge and force a daemon restart with the SPA reloaded. The mid-session case was the load-bearing argument: a live QSO must not require service-level intervention to recover from a momentary rig disruption.

### Supervisor + retry loop (chosen)

The current implementation. Handles both startup ordering and mid-session recovery with the same mechanism. Cost is some additional complexity in the pipeline's exit-classification path and a dedup token to manage toast flood. Both costs are small relative to the alternative of "operator restarts the daemon."

### Active health-check / polling

Periodically send a poll command (e.g. `ID;`) to detect rig presence even when the rig isn't pushing. Rejected for v1 because the passive 30s liveness timeout (ADR 0010) is sufficient — once INIT lands and AUTO mode is armed, the rig pushes naturally; we don't need to interrogate it. Active polling becomes worth considering only if the wedged-but-streaming-waterfall scenario (ADR 0019 triggers) bites in real operating use.

## Consequences

**Gained:**

- **First-boot ordering self-heals.** Daemon up before rig is now the normal case, not a failure case. Operator's recommended startup sequence (PC, then rig) works without intervention.
- **Mid-session disruption recovers automatically.** Power spike, USB reseat, rig auto-reset — all clear themselves once the rig is back, without restarting the daemon or reloading the SPA. The session continues from where it was.
- **Identity verification re-fires on each pipeline restart.** Hot-swap of one rig for another on the same port (off, swap, on) is now handled — identity check runs against the new rig's first IDENTITY response on the next supervisor cycle. Previously a per-pipeline-instance check that required a daemon restart to re-verify (per the pipeline review's #9 note).
- **The cost line in ADR 0019** ("rig state lost only when the rig itself is power-cycled") is now narrower — rig power-cycle recovers as long as the operator powers it back on; only daemon restart still loses state.

**Accepted costs:**

- **Operator may see a brief "Serial port open failed" toast even on healthy boots** if the daemon races the udev rule that creates `/dev/ttyUSBn` (rare — udev is typically faster than service-manager). The supervisor's next retry succeeds within 1–2 seconds and the SPA flips to bridge-driven; the dangling toast is the only artefact.
- **Log noise during prolonged rig-off periods.** Each open attempt logs at ERROR level. If the rig stays off for hours, journalctl accumulates ~120 ERROR entries per hour (1 per 30s steady-state). Acceptable — operators rarely leave the daemon running with the rig off for long, and the journal is grep-friendly.
- **Backoff timings are package-level vars, not config knobs.** Tests dial them down via the vars; operators have no way to tune them without rebuilding. Acceptable — the defaults (1s start, 30s cap) match the operator mental model and no friction has surfaced. Promote to config if friction emerges.
- **Pipeline lifecycle is one indirection deeper.** `Start` → `runSupervisor` → `runPipeline` → `readLoop` instead of `Start` → `runPipeline` → `readLoop`. New contributors have one more function to trace, but the supervisor itself is short and the classification model is local to each exit site.

## Triggers to revisit

- **Toast dedup proves insufficient.** If the single-token dedup misses cases (e.g. flapping between two different error codes during a flaky boot causing both to fire repeatedly), the dedup widens to a small recent-history set with a time window.
- **Backoff timings need operator tuning.** If a deployment with unusually slow USB enumeration (industrial PC, custom kernel) sees the first retry land before the device node appears, surface `bridge.reconnect_initial_ms` / `bridge.reconnect_max_ms` as config keys.
- **Active polling becomes necessary** for the wedged-but-streaming-waterfall scenario from ADR 0019 — the supervisor's passive design doesn't help if the rig is alive enough to send waterfall data but not responding to state pushes. Active health-check would slot in alongside the current shape (separate goroutine, runs while the read loop is healthy).
- **Multi-rig.** When `bridge.rigs[]` becomes a list per ADR 0019's multi-rig trigger, each rig gets its own supervisor + pipeline + dedup token. The shape generalizes cleanly because the supervisor closes over the per-rig state.
- **Operator surfaces "I need a persistent log of what just fired."** The chatty-info "stop nagging" angle is already addressed via the My Station → Notifications mute toggles (`MyStationPanel.svelte`) and the per-level TTL auto-dismiss in `toasts.svelte.ts`. What's NOT shipped is a notification-log panel or a banner mode for must-acknowledge events (rig identity mismatch and similar). Speculative for now; revisit when operator surfaces "the warn flew by and I have no idea what it said" or "I want to see the last N events." Touches toast subsystem (ADR 0008), not the supervisor.

## References

- ADR 0010 — `rig-sse-wire-shape.md` (event codes; passive liveness model preserved)
- ADR 0013 — `daemon-owns-bridge-as-subsystem.md` (default deployment in which the supervisor lives)
- ADR 0019 — `bridge-subsystem-v1-design.md` (the v1 design this amends; the "no persistent state across daemon restart" cost narrows)
- `internal/bridge/pipeline.go` — `runSupervisor`, `runPipeline`, dedup helpers
- `internal/bridge/supervisor_test.go` — coverage for open-retry, terminal-read-retry, permanent-no-retry, dedup-across-retries

## Revision — 2026-05-16 (session 66): timeouts promoted to config, liveness default lowered

Promoted the four package-level timeout vars to operator config under `bridge.timeouts.*`, executing the "Promote to config if friction emerges" trigger noted in this ADR's Accepted Costs. Friction surfaced during dogfooding the supervisor on the real FTdx10: that rig's USB-serial layer keeps `/dev/ttyUSBn` open when the rig is powered off (reads simply go silent, no kernel-level disconnect), so the 30s `livenessTimeout` was the only signal the daemon had to detect rig-off. For any rig-off duration shorter than ~25s, the disconnect was detected nearly simultaneously with the recovery — the warn toast appeared and was instantly replaced by the reconnect info toast (operator-visible "flash" only).

**Config keys** under `bridge.timeouts` (all optional, all milliseconds; zero or omitted = use built-in default):

| Key                          | Default | Replaces                              |
| ---------------------------- | ------- | ------------------------------------- |
| `liveness_ms`                | 5000    | `livenessTimeout` (was 30s)           |
| `backoff_initial_ms`         | 1000    | `supervisorInitialBackoff` (was 1s)   |
| `backoff_max_ms`             | 30000   | `supervisorMaxBackoff` (was 30s)      |
| `steady_state_threshold_ms`  | 10000   | `supervisorSteadyStateThreshold` (10s)|

**Default `liveness_ms` lowered from 30000 to 5000.** Safe because the no-data branch's INIT+READ probe means a false-positive disconnect during legitimate idle self-recovers within milliseconds (the probe's READ pulls a snapshot from an alive rig, `rig-state` arrives, SPA's reconnect handler dismisses the warn and fires the info — same flash UX as a real short outage, but acceptable as a tradeoff for catching real outages faster). Operators who want fewer false-positive flashes can dial back up via `bridge.timeouts.liveness_ms`.

**Validation:** Each value must be 0 (default) or between 50 ms and 3 600 000 ms (1 hour); `backoff_initial_ms` must not exceed `backoff_max_ms`. Caught at daemon startup via `validateBridge`.

**Test pattern preserved.** Tests dial the package vars before `Service.New` runs — `New` snapshots package vars into Service fields when the cfg.Timeouts value is zero, so the existing `livenessTimeout = N * time.Millisecond` test idiom keeps working. New `TestServiceNew_SnapshotsConfiguredTimeouts` covers the config-override path; new `TestValidateBridge_TimeoutRangeChecks` covers the range checks.

## Revision — 2026-05-16 (session 66): SPA-side flash suppression for brief outages

Lowering `liveness_ms` from 30s to a smaller default (5s, or 10s if the operator dials it back up) didn't eliminate the operator-visible "flash" — it shifted the boundary. With `liveness_ms=10000` and a 10s rig-off cycle, the daemon detects no-data within milliseconds of the rig coming back; `rig-disconnected` and `rig-state` SSE events arrive in adjacent event-loop ticks, Svelte renders the warn toast between them, and the reconnect handler immediately replaces it with the info toast. Operator sees a fleeting warn then an info — technically correct, UX-noisy for blips the operator caused intentionally (intentionally cycling the rig, brief power spikes).

**Decision:** SPA-side timer-based flash suppression in `bridge.svelte.ts`. On `rig-disconnected`, the warn toast push is **scheduled** via `setTimeout(..., FLASH_SUPPRESS_MS)` rather than pushed immediately. If `rig-state` arrives within that window, the rig-state handler `clearTimeout`s the scheduled push and skips the reconnect info too — no UI churn at all for the suppressed cycle. If the window elapses without recovery, the warn pushes normally (sticky `ttl=0`) and a later `rig-state` triggers the existing dismiss + reconnect-info path.

`FLASH_SUPPRESS_MS = 800` is a module-level constant in `bridge.svelte.ts`. Sized to comfortably exceed the probe round-trip + SSE delivery latency on local serial (typically <200ms), with headroom for slower setups; below 1s so genuine outages still surface visibly within ~1.5s of the daemon's detection.

**Three-state machine on the SPA side:**

| State | Meaning | `pendingDisconnectTimerId` | `pendingDisconnectToastId` |
| ----- | ------- | -------------------------- | -------------------------- |
| **A** | idle — no known disconnect | `null` | `null` |
| **B** | disconnect scheduled, warn NOT yet pushed | set | `null` |
| **C** | warn toast visible on screen | `null` | set |

Transitions:

- `rig-disconnected` in A → B (schedule timer)
- `rig-disconnected` in B → B (cancel prior timer, schedule new — latest payload wins)
- `rig-disconnected` in C → B (dismiss visible toast, schedule new)
- Timer fires in B → C (push the warn)
- `rig-state` in A → A (no-op; existing partial-payload merge proceeds)
- `rig-state` in B → A (cancel timer; no toasts surface — suppressed cycle)
- `rig-state` in C → A (dismiss warn, push `bridge.reconnected` info toast)

`closeSource()` clears both pending fields and the timer; without this a deferred warn would fire after the EventSource closed, pushing a stale toast for an event no longer relevant.

**Alternatives considered:**

- **Do nothing.** Accept the flash. Rejected because the flash is operator-confusing — they didn't disconnect anything, the rig was off for 1s, why are toasts flickering?
- **Lower `liveness_ms` to ~1s.** Shrinks the flash window but doesn't eliminate it (any rig-off duration close to `liveness_ms` flashes). Also causes more false-positive flashes during legitimate idle (operator on a phone call). Rejected — symptomatic, doesn't address the root cause.
- **Server-side suppression.** Daemon could delay publishing `rig-disconnected` for a short window, cancelling if data resumed. Considered but rejected because it complicates the daemon's hot path for a UX concern best handled where the UX lives. SPA already owns the toast subsystem and the timer machinery is trivial.

**Verified live on the FTdx10.** Brief rig-off + on (≤1s, simulated power blip) produces no SPA toasts. Genuine outage (10s+ rig-off with the daemon's `liveness_ms=10000`) surfaces the warn toast around 10.8s after the rig went off, persists until the rig comes back, replaced by "Rig reconnected." info.

**Tests:** `bridge.test.ts` uses `vi.useFakeTimers()` for the disconnect describe block; new test `SUPPRESSES the flash when rig-state arrives within the suppression window` proves the suppressed-cycle path; existing tests updated to advance timers past the window when they want to assert on the visible toast.
