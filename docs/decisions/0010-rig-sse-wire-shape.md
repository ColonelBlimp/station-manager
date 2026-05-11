---
number: 0010
title: Rig SSE wire shape — single endpoint, three event types, passive liveness via rig data flow
status: Accepted (revised four times — see revision notes)
date: 2026-05-01
---

# 0010 — Rig SSE wire shape

> **Revision notes.**
>
> *First revision (2026-05-02, ADR 0012):* this ADR originally deferred the question "which Go process answers `GET /v1/rig/events`" to `bridge.md` / `topology.md`. ADR 0012 promoted that to a named decision: bridge serves the endpoint, daemon does not.
>
> *Second revision (2026-05-02, ADR 0013):* ADR 0012 was superseded within hours. The dominant deployment is single-operator-on-the-shack-PC; forcing a separate bridge process there is ceremony for the case the operator actually lives in. ADR 0013 collapses the bridge into the daemon binary as an internal subsystem. **In the default deployment the daemon hosts `/v1/rig/events` directly, with the bridge subsystem providing the underlying data.** The split-host deployment (network-deployed daemon with no bridge, separate bridge process on the rig host) is preserved as an opt-in via subsystem disable-flagging.
>
> *Third revision (2026-05-10, ADR 0019):* the **"Bridge-side current-state cache" section below is removed** (revised by ADR 0019). The v1 bridge is a stateless filter — no cache, no delta computation. SPA-side `catState` (Svelte 5 `$state` proxy) provides the value-persistence the cache was solving for; snapshot-on-connect is now an active CAT poll at SSE-open time, not a cached send. **The wire shape on `/v1/rig/events` is unchanged** — same three event types, same payload format, same merge-into-`catState` semantic on the SPA side. Only the bridge's internal implementation changed. The "Bridge-side current-state cache" subsection below is preserved for the reasoning trail of why a cache was originally proposed and why it was later dropped; treat it as historical, not current.
>
> *Fourth revision (2026-05-10, M3a.3 implementation):* two implementation-level clarifications that don't change the architectural shape but matter for SPA wiring (M3a.4):
> 1. **`splitOverride` is wire-presence-significant.** The Go bridge implementation uses `*bool` so the SPA can distinguish "rig didn't push split this frame" (field omitted from JSON) from "rig pushed split=OFF" (`"splitOverride": false`). Pre-fix the bridge had `bool` + `omitempty` which collapsed the two cases on the wire. The SPA's `bridge.svelte.ts` consumer should treat field-presence as the merge gate, not field-truthiness — `splitOverride: false` IS a meaningful update.
> 2. **`bridge-error` events have a hub-side cache.** The hub holds one cached `bridge-error` slot and replays it to every new subscriber as their first event. This means a SPA tab opening AFTER a startup-time bridge-error (typo'd `bridge.cat.driver`, port permission denied, etc.) still receives the toast. Per-Service-instance lifetime; never cleared within a Service. This is a deliberate exception to ADR 0019's "no cache" stance — rig state stays uncached, but operator-actionable errors are too valuable to forget. Documented in ADR 0019's revised cache section.
>
> *Fifth revision (2026-05-11, M3a.4 implementation):* SPA-side wiring landed; the wire-shape itself is unchanged but three implementation details earned their place in the reasoning trail:
> 1. **The three-flag rule's first flag (`configState.station.enabled`) is now daemon-authoritative.** Originally framed as "operator config" in the SPA, the SPA had no UI toggle and no daemon mirror; the EventSource never opened. Fix: `/v1/config` response gained a `bridge` block with `enabled: boolean` (mirrored daemon-side from `cfg.Bridge.Enabled`), SPA hydrates into `configState.station.enabled` via `applyResponse`. Operator's only way to flip CAT on/off is `config.json` + daemon restart, matching ADR 0003's "operator owns config.json directly" pattern for SMTP creds / hardware config.
> 2. **`/v1/config` also surfaces `bridge.rig_name`** — the rigdef's human-readable name (e.g. "Yaesu FTdx10") resolved daemon-side from `cat.Lookup(Bridge.Cat.Driver).Name`. SPA mirrors into `configState.station.rigName`; `displayedState.rigName = isLive ? rigName : ''` is read by My Station Equipment panel (read-only display when CAT live) and used as the ADIF MY_RIG fallback. This is distinct from `catState.rigIdentity` (the IDENTITY-tag-mapped value the rig actually reports, e.g. "FTdx10") — `rigName` is the operator's chosen driver's name, `rigIdentity` is what the wire says. Both useful; `rigName` is the human-readable one.
> 3. **Daemon middleware needs `Unwrap()`.** `internal/api/middleware.go`'s `responseRecorder` wraps `http.ResponseWriter` for access-log instrumentation. Without `Unwrap() http.ResponseWriter`, `http.ResponseController.SetWriteDeadline(time.Time{})` in SSE handlers can't traverse the wrapper to clear the server's `WriteTimeout`, and SSE connections are force-closed every `WriteTimeoutSec` seconds (30s by default). Symptom: SPA reconnects silently but frequency pushes that arrived during the reconnect gap are lost. Pinned by the unbounded SSE durations in live-test logs post-fix (~85s observed; pre-fix every connection was exactly 30007ms).
>
> The wire shape below — three event types, deltas, passive liveness — is unchanged across all four revisions. Only the host (ambiguous → bridge process → daemon with bridge subsystem) and the cache strategy (cached → stateless → stateless-except-for-bridge-error) have changed. The SPA composes the URL as `${configState.bridgeUrl}/v1/rig/events`, where `bridgeUrl` defaults to `daemonUrl` in the single-binary deployment and is operator-overridable for the split-host case.

## Context

ADR 0006 / 0009 settled SPA-side state ownership but left the wire format between bridge and SPA undefined. Implementation of `bridge.svelte.ts` (per ADR 0009's four-object decomposition) needs the wire contract to know what events to listen for, what payloads to expect, and what the SPA's three flags (`enabled`, `connected`, `rigResponding`) are derived from.

Two things constrain the design that are worth surfacing before the decision:

- **AUTO-mode CAT is the assumed rig protocol shape** (per memory `project_sm_serial_bridge`). In AUTO mode the rig pushes data to the bridge — frequency / mode / VFO changes when the operator turns the dial, plus continuous data the SPA doesn't care about (waterfall noise, S-meter telemetry). The bridge listens, filters, and forwards SPA-relevant deltas. **The continuous-flow nature is load-bearing** for liveness detection: it means "data on the wire" can be used as a passive heartbeat without the bridge having to ping or poll.
- **Serial port disconnection is hard to detect cleanly.** The OS doesn't tell the bridge when the rig is unplugged or powered off; the bridge only learns by trying to read (and getting `EIO`) or by noticing data flow has stopped. This makes liveness detection inherently best-effort.

A separate concern (now settled by ADR 0012): ADR 0001 chose a daemon-hosted SPA, but `topology.md` makes the bridge a *peer* service to the daemon, not a subordinate. The SSE endpoint **is hosted by the bridge process**, not the daemon. The SPA opens an EventSource against `bridgeUrl` (from `configState`), which is independent of `daemonUrl`. The wire contract below is host-agnostic; ADR 0012 explains why the host is the bridge.

## Decision

### Endpoint

`GET /v1/rig/events` — an SSE stream. Single endpoint, no separate snapshot endpoint. The SPA composes the URL as `${configState.bridgeUrl}/v1/rig/events`.

In the **default deployment** (single-binary, ADR 0013), the daemon serves this path; `bridgeUrl == daemonUrl` and the SPA's connection is same-origin. The bridge subsystem inside the daemon binary populates the underlying data.

In the **split-host deployment** (opt-in), the bridge runs as a separate process on the rig host; the SPA's `bridgeUrl` is set to that process's address; the daemon's bridge subsystem is disabled (`bridge.enabled: false`) so the daemon never opens a serial port. The wire shape on either host is identical.

The SPA opens the EventSource conditionally on `configState.station.enabled`. If CAT is disabled in operator config, the SPA never opens the connection.

**CORS.** In the default deployment, no CORS — same origin. In the split-host deployment, the standalone bridge sets `Access-Control-Allow-Origin` headers permissive enough for the SPA loaded from the daemon to subscribe (default `*` for single-operator LAN; tightenable per `topology.md`). This is bridge-side configuration in the split case, not part of the wire shape, but worth naming here so SPA-side debugging starts in the right place when split deployments are in use.

### Event types

Three named SSE events:

#### `rig-state`

Partial JSON of CAT-relevant fields. Carries:

- All fields on the **initial** event after a new SSE connection (full snapshot from the bridge's current-state cache).
- Only **changed fields** on subsequent events (delta).

The SPA's handler is the same in both cases: merge the payload into `catState` field-by-field. No distinction between snapshot and delta at the protocol or handler level.

Fields (initial set; grows as more rig data is wired up):

```json
{
    "rigIdentity": "IC-7300",
    "vfoA": 14250000,
    "vfoB": 14250000,
    "mode": "USB",
    "subMode": "",
    "selectedVfo": "A",
    "splitOverride": false,
    "power": 100
}
```

Frequency in Hz (consistent with `cat.svelte.ts`). Mode/subMode follow ADIF naming. Power in watts (raw rig output — `displayedState.effectivePower` is the multiplier-applied value, computed SPA-side per ADR 0009).

#### `rig-disconnected`

Sent when the bridge concludes the rig is no longer alive. Triggers (any of):

- **CAT identity fails at bridge startup.** Bridge sends a query for rig identity; gets nothing, garbage, or a timeout. Rig was never confirmed alive.
- **Data flow stalls.** No data has arrived on the wire for N seconds (suggested 30s starting value, tunable). In AUTO mode the rig sends continuous waterfall/telemetry data; absence of *any* data implies the rig is gone.
- **Serial port error.** `EIO` on read, port disappears from the OS, framing errors, etc.

Payload:

```json
{ "reason": "serial port closed" }
```

Reason is a short human-readable string for the SPA to show in a toast (per ADR 0008) and on the stale-values indicator. The SPA does **not** clear `catState` on `rig-disconnected` — last-known values persist, marked stale via `bridgeState.rigResponding === false`.

#### `bridge-error`

Sent when the bridge encounters an operator-relevant error (port permission denied, baud-rate mismatch, rig identification failed, etc.). Distinct from `rig-disconnected` — `bridge-error` is for "something happened the operator should know about"; `rig-disconnected` is the steady-state "rig isn't alive."

Payload: `{ "message": string }`. SPA toasts it via ADR 0008.

Don't surface every protocol-level retry or transient hiccup as `bridge-error` — only operator-actionable conditions.

### Reconnection: implicit

There is no `rig-reconnected` event. The first `rig-state` event after a `rig-disconnected` implies reconnect. The SPA flips `bridgeState.rigResponding` back to `true` on receiving any `rig-state` event.

This keeps the protocol to three events instead of four. Since `rig-state` always merges into `catState`, the merge naturally re-populates whatever fields the rig is now reporting.

### Bridge-side current-state cache

> **Superseded by ADR 0019 (2026-05-10).** The v1 bridge is stateless. No cache. Snapshot-on-connect is now an active CAT poll at SSE-open time. SPA's `catState` provides the value-persistence the cache was solving for. The text below is preserved as the reasoning trail of why the cache was originally proposed; it does NOT describe the current implementation.

The bridge maintains an internal cache of the last-known rig state. It serves three purposes:

1. **Delta computation.** When a rig push arrives, the bridge compares to the cache and emits only the changed fields.
2. **Initial snapshot on new SSE connection.** When the SPA reconnects (browser reload, network blip), the bridge sends the cached state as the first `rig-state` event.
3. **Cache survives `rig-disconnected`.** When the rig goes away, the cache holds the last-known values. When the rig comes back, the cache continues to be updated by new pushes.

### Behaviour when SPA opens SSE while rig is already disconnected

Per Q-B confirmed during design conversation: bridge sends the cached `rig-state` first (last-known values), then `rig-disconnected`. The SPA briefly shows the last-known values, then marks them stale.

If the bridge has **no cache** (first-ever launch, rig has never been alive in this bridge's lifetime), only `rig-disconnected` is sent. The SPA's `catState` keeps its hardcoded defaults (per ADR 0003 / 0009); `displayedState` falls back to `manualState` because `rigResponding === false`.

### SPA flags and `editable`

The SPA's bridge module owns three flags:

| Flag | Source | Meaning |
|---|---|---|
| `configState.station.enabled` | Operator config (`/v1/config`) | Operator wants CAT |
| `bridgeState.connected` | `EventSource.readyState === OPEN` | SSE channel is open |
| `bridgeState.rigResponding` | `false` until first `rig-state`; `false` after `rig-disconnected`; `true` after any `rig-state` | Rig is actively reporting |

The `editable` derived helper from ADR 0006 / 0009 expands to:

```ts
const editable = $derived(
    !(configState.station.enabled
      && bridgeState.connected
      && bridgeState.rigResponding)
);
```

Operator can edit when **any** of the three is false — CAT disabled, bridge unreachable, or rig not responding.

### What's deferred

- **Last-Event-ID** for replay across brief reconnects. Not used in v1. Re-snapshotting on reconnect (which is free given the bridge's cache) covers the same ground.
- **Synthetic heartbeat / keepalive.** The rig's own waterfall/telemetry data flow IS the heartbeat. No additional ping needed.
- **Authentication.** No auth in v1 (LAN-only deployment). Static-token-via-`Authorization` for remote deployment per `topology.md` is future work.
- **Per-rig liveness configurability.** The 30-second data-flow timeout is global; per-rig tuning if it becomes necessary.

## Alternatives considered

### Full state in every event (no deltas)

Originally my recommendation in design conversation. The user pushed back: in AUTO mode the rig genuinely pushes deltas natively, and the bridge has to maintain current-state internally anyway (to know what to query at startup, to filter SPA-relevant fields, to detect changes). So forcing the bridge to send full state on every event throws away information the bridge already has.

Rejected. Deltas are correct: lower bandwidth (probably negligible at SM scale, but still), match the rig's natural protocol, and the merge-into-`catState` is one line on the SPA side. Same merge logic handles initial snapshot and deltas — no client-side complexity introduced.

### Separate snapshot endpoint (`GET /v1/rig` + `GET /v1/rig/events`)

`GET /v1/rig` returns current state as one-shot JSON. `GET /v1/rig/events` is deltas only.

Rejected: SPA never wants snapshot without live updates. A separate snapshot endpoint is only useful for non-streaming consumers (CLI tools, monitoring scripts) which we don't have. Keeps the surface smaller.

### One event type with `type` field in payload

`event: message` for everything; payload has `{type: "state" | "disconnected" | "error", ...}`. Client switches on the type field.

Rejected: named SSE events are exactly what the SSE protocol provides; using them is more idiomatic. Client handler shape is cleaner — `eventSource.addEventListener('rig-state', ...)` per type, no inner switch.

### Active polling instead of AUTO-mode-driven push

Bridge polls the rig at fixed intervals (e.g. 100 ms) regardless of whether the rig is in AUTO mode. Simpler liveness detection (no data → poll fails → disconnected).

Rejected for v1: contradicts the established `AUTO-mode CAT assumed` choice (memory `project_sm_serial_bridge`). Also unnecessary — the rig's continuous waterfall data already gives liveness. Trigger to revisit: encountering a rig that doesn't have continuous data flow (forces explicit polling or synthetic heartbeat).

### Synthetic heartbeat (`event: keepalive` every N seconds)

Bridge emits a periodic keepalive event so SPA can detect transport problems even when the rig hasn't changed.

Rejected: redundant with the rig's own continuous data flow. The whole point of using waterfall data as a passive heartbeat is to avoid the synthetic version. Trigger to revisit: per ADR 0006's deferred-heartbeat note, if silent rig stalls happen in non-AUTO mode.

### `rig-reconnected` as its own event type

Originally on the table as a fourth event. Rejected (Q-A confirmed): the first `rig-state` after a `rig-disconnected` is unambiguously a reconnection. The fourth event type carries no information the third doesn't.

### Clear `catState` on `rig-disconnected`

`rig-disconnected` carries empty/zeroed values; SPA `catState` clears.

Rejected: bad operator UX. Operator was on 14.250 MHz USB; rig powers off; SPA shows blank. When the rig comes back, the values are immediately repopulated. The transition is a brief blank-out window for no benefit. Keeping last-known values marked stale is more useful — operator sees what the rig was last on, knows it's stale.

## Consequences

**Signed up for:**

- **Bridge implementation owns a current-state cache.** Required for: delta computation, initial-snapshot-on-connect, surviving `rig-disconnected`. Likely a small struct in the bridge process keyed by field name.
- **Liveness detection is best-effort.** A rig that's wedged-but-streaming-waterfall looks alive. A rig that's powered off but the OS hasn't surfaced the serial-port closure looks alive until the timeout fires. Operators may occasionally see stale values that don't immediately mark stale; documented as a v1 limitation.
- **30-second data-flow timeout is a tunable.** Will probably need to be revisited based on real operating use. Per-rig configurability if different rigs have different baseline data rates.
- **SPA's bridge module subscribes to three event types** and maintains the three flags. ~50 lines for the consumer logic + EventSource wiring + reconnect handling (browser default).
- **`bridgeState.rigResponding` derived from event sequence,** not from a single field on `catState`. The rule: `rig-disconnected` flips it false; any `rig-state` flips it true.

**Accepted costs:**

- **Wedged-but-streaming rig** is undetectable. Acceptable for v1; revisit if it bites.
- **Brief stale-values window** is possible if the rig disappears between data pushes and the timeout fires. Mitigated by marking stale visually (operator can see "wait, this is from 30 seconds ago").
- **The bridge has to track which fields the SPA cares about.** Forwarding all rig data (waterfall, S-meter) over the wire to the SPA would be wasteful; the bridge filters. Filter list grows as new fields are added — implementation detail, not architectural cost.

**Gained:**

- **Three flags cleanly separate three concerns** (configured / transport-up / rig-alive). Each is independently observable; the `editable` derivation is one line.
- **No heartbeat infrastructure to build.** The rig's own data flow does the work.
- **Reconnection is automatic.** Browser's default `EventSource` retry + bridge's "send full snapshot on connect" gives stateless recovery.
- **Cache survives transient operator confusion.** Operator reloads the SPA → bridge re-sends cached state → operator sees what the rig was on, no blank-screen moment.

## Triggers to revisit

- **30-second data-flow timeout turns out wrong.** Too short → false-positive disconnects during normal-but-quiet operation. Too long → stale data shown beyond operator-acceptable window. Tune based on real operating use; may need per-rig tuning.
- **Encountering a rig without continuous data flow.** "AUTO-mode CAT assumed" is v1. If a rig is encountered that only emits on changes (no waterfall/telemetry stream), passive liveness collapses and the bridge needs explicit polling or a synthetic heartbeat. ADR 0006's deferred-heartbeat path becomes live.
- **Wedged-but-streaming rig becomes a real problem.** Operator is "on a frequency" that hasn't actually been set because the rig stopped responding to commands but is still spamming waterfall. Detection requires explicit command-acknowledge tracking, which the bridge doesn't do today.
- **Many SPA tabs / clients connecting simultaneously.** Each opens its own SSE; bridge fans out from a single rig source. If fan-out becomes expensive (Go-side allocation correlated with subscriber count), worth re-examining — but this is in the noise per `cat-performance.md`'s analysis at the codec layer.
- **Multi-rig deployment.** SSE endpoint becomes `GET /v1/rig/{id}/events` or similar. Wire shape per-rig is unchanged; routing layer added.
- **Last-Event-ID becomes worthwhile** if SPA-bridge connection becomes flaky enough to cause flicker between disconnect and reconnect. Replay buffer joins the bridge cache.
- **Auth required** for remote-VPS deployment. Static token via `Authorization` header; EventSource constructor doesn't accept headers natively, so polyfill or query-string token. Per `topology.md`.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — daemon-hosts-SPA premise; also names the SPA-cannot-own-serial consequence that forces CAT into the daemon-side process.
- ADR 0012 (`0012-daemon-and-bridge-separate-origins.md`) — *superseded*. Preserved for the reasoning trail of why "two processes, two origins" was considered before being collapsed.
- ADR 0013 (`0013-daemon-owns-bridge-as-subsystem.md`) — current decision on which process serves this endpoint (the daemon, with the bridge as an internal subsystem); split-host deployment preserved as an opt-in.
- ADR 0003 (`0003-spa-config-daemon-only.md`) — `configState.station.enabled` source; also where `bridgeUrl` lives. In default deployment `bridgeUrl == daemonUrl`.
- ADR 0004 (`0004-daemon-vs-spa-responsibilities.md`) — daemon owns external-service orchestration; bridge is the rig-side service.
- ADR 0006 (`0006-cat-state-precedence-rule.md`) — precedence rule that this wire shape implements; `editable` helper depends on the three flags this ADR defines.
- ADR 0008 (`0008-notifications-toast-system.md`) — `bridge-error` and `rig-disconnected` reasons surface as toasts.
- ADR 0009 (`0009-cat-state-decomposition.md`) — `catState` is the SPA-side mirror that this stream populates; merge logic lives in `bridge.svelte.ts`.
- `docs/v2-design/topology.md` — bridge as a peer of daemon; informs which Go process hosts the endpoint.
- `docs/v2-design/bridge.md` (forthcoming) — bridge architecture, including which process serves this endpoint, internal cache shape, AUTO-mode CAT integration, serial port handling.
- Memory `project_sm_serial_bridge` — `AUTO-mode CAT assumed`, the assumption this ADR's liveness model depends on.
- Memory `project_sm_cat_precedence_rule` — the `editable` helper using all three flags from this ADR.
- (Planned) `frontend/logging/src/lib/states/bridge.svelte.ts` — SPA-side EventSource consumer; subscribes to the three event types defined here.
