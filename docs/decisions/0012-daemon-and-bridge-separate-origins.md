---
number: 0012
title: Daemon and bridge are separate origins — load-bearing
status: Superseded by ADR 0013 (2026-05-02)
date: 2026-05-02
---

# 0012 — Daemon and bridge are separate origins

> **Superseded same day, 2026-05-02, by ADR 0013.** This ADR was drafted in response to noticing the SPA was about to call the daemon's origin for a bridge-owned endpoint, and codified "the bridge is a separate process, distinct origin" as the fix. Within hours the operator pushed back: the dominant deployment is single-operator-on-the-shack-PC, and forcing two processes / two origins / CORS-by-default into the personal-use shape is paying for split-host flexibility every day to support a topology that's used rarely. ADR 0013 collapses the bridge into the daemon binary as an internal subsystem (same-origin, single-binary), while preserving the *split-host* deployment as an opt-in via subsystem disable-flagging. The architectural separation between log/forward concerns and bridge concerns is preserved at package level rather than process level.
>
> Read 0013 for the current decision. This ADR is preserved as the record of how "two processes, two origins" was considered and the reasoning trail of why it was rejected. Specifically the alternatives section here ("daemon proxies the bridge SSE", "in-process bridge") is now relevant in reverse — "in-process bridge" was rejected at this ADR's writing because it was argued to nail the daemon to the rig host; ADR 0013 shows that argument was too strong, because a *disable-flagged* in-process bridge can be turned off in upstream-only deployments, decoupling the daemon from the rig host without requiring a separate process.

## Context

`topology.md` (refined 2026-04-30) already states that the bridge is a peer of the daemon, not a subordinate, and that the SPA connects to both directly. ADR 0001 chose a browser SPA hosted by the daemon. ADR 0010 specified the rig SSE wire shape but punted on which process serves the endpoint, deferring to `bridge.md` / `topology.md`.

Reviewing 0010 in 2026-05-02 surfaced an implicit drift: 0010's example URL (`GET /v1/rig/events`) reads as a daemon endpoint, and the SPA's planned `bridge.svelte.ts` consumer was on a path to call the daemon's origin for it. That is wrong, and the reason it is wrong needs to be a settled, named decision rather than something that has to be rediscovered every time a new feature touches CAT data.

The forcing constraint:

- The **daemon must be network-deployable.** It is the QSO-log owner, the forwarder, and the SPA host. Personal-use shapes include "daemon on a Raspberry Pi", "daemon on a home server", and "daemon on a VPS" — none of which are co-located with the rig.
- The **bridge must be operator-local** to the rig. The bridge owns the serial port; the rig is wired into a specific physical machine. The bridge cannot be relocated to a NAS or a VPS — it must run wherever the USB / RS-232 cable terminates.
- These two requirements are independent. Once the operator wants the daemon anywhere other than the shack PC, the daemon and the bridge are on different hosts.

ADR 0001 (browser SPA) made this unavoidable: a Gio client could have owned the serial port in-process, collapsing client and bridge into a single binary on the shack PC. The SPA cannot. So the bridge becomes a forced consequence of ADR 0001, and the topology asymmetry (daemon-network / bridge-local) becomes a forced consequence of the bridge being a separate process. This ADR makes the result explicit so future design work doesn't accidentally re-collapse them.

## Decision

**The daemon and the bridge are distinct services with distinct origins.** This is load-bearing for v2 and not subject to revision without a corresponding redesign of the deployment shape.

Concretely:

- The SPA holds two independent origin URLs in `configState`: `daemonUrl` and `bridgeUrl`. Both default to `http://localhost:<port>` for the all-on-one personal-use shape, and are operator-editable for split deployments.
- The SPA opens **two transport connections**: HTTP/JSON to the daemon (config, QSO submit, enrichment, forwarding status) and SSE to the bridge (rig state per ADR 0010).
- The daemon **never proxies, brokers, or relays bridge data.** Daemon and bridge do not talk to each other directly; the SPA is the only thing that talks to both. This preserves the "narrow daemon scope" invariant (daemon = log + forward) and the topology.md rule that bridge events flow bridge → SPA, never via the daemon.
- The bridge serves the `/v1/rig/events` SSE endpoint defined in ADR 0010 (and the rigctld-compatible TCP frontend per `bridge.md`). The endpoint paths are unchanged; only the host changes from "ambiguous" to "explicitly the bridge."
- The bridge sets CORS headers permissive enough for the daemon-hosted SPA to connect from a different origin (default `Access-Control-Allow-Origin: *` for single-user LAN deployments; tightenable per `topology.md`).
- For multi-rig (future), the SPA holds `configState.bridges[]`, not a single `bridgeUrl`. Wire shape per-rig is unchanged from ADR 0010; routing is added SPA-side.

## Alternatives considered

### Daemon proxies the bridge SSE

The daemon would expose `/v1/rig/events` and internally subscribe to the bridge's stream, fanning it back out to SPA subscribers. Single origin for the SPA; no CORS needed.

Rejected on three counts:

1. **Contradicts the narrow-daemon-scope invariant.** The daemon would now hold rig-state knowledge to relay it — even if the relay is dumb, the surface area is no longer "log + forward". Adding rig-aware code paths to the daemon is the slope this invariant exists to refuse.
2. **Forces the daemon and bridge to be reachable from each other.** In the "daemon on VPS, bridge on shack PC" topology, the daemon would have to reach back into the operator's home network to subscribe to the bridge — which means port-forwarding a CAT endpoint over the open internet, or running a tunnel. The SPA-direct path makes the bridge reachable only from the operator's LAN/loopback by default; the daemon-proxy path leaks the bridge surface to wherever the daemon lives.
3. **Adds a hop for no gain.** The SPA already runs on the operator's machine (or near-LAN) where the bridge is reachable directly. Routing rig events through a remote daemon and back adds latency and a failure mode (daemon-unreachable kills CAT visibility too).

### In-process bridge (collapse bridge into daemon)

Bridge code lives in the daemon binary, opens the serial port directly. Single process, single origin, no CORS, no ADR.

Rejected: this is exactly the "daemon needs serial-port access" thing that `topology.md` and the narrow-scope invariant rule out. It also nails the daemon to the rig host — the operator can't run the daemon on a Pi or a VPS anymore. The whole point of the daemon being a separate service is that it doesn't depend on physical attachment to the rig.

This was the *Gio-era shape*, where the client owned both UI and CAT. ADR 0001 walked away from Gio; collapsing CAT back into the daemon would be reintroducing the same coupling on the wrong side.

### SPA connects to bridge only; daemon is a sidecar to the bridge

Inverted topology — the bridge becomes the primary, the daemon becomes a forwarder it talks to. The SPA loads from the bridge.

Rejected: the daemon is the QSO log. Logs need to be reliable, network-reachable, and survive shack-PC outages. The bridge is host-bound to the rig and goes away whenever the operator reboots the shack PC or unplugs the radio. Hosting the SPA from the bridge means the SPA dies whenever the rig host dies — even when the operator just wants to review old QSOs from a phone. The daemon-hosts-SPA shape (ADR 0001) is the correct primary because the log is the durable thing.

### Same-origin via reverse-proxy on the SPA host

Operator runs nginx (or similar) on the SPA's host, reverse-proxying both daemon and bridge under one origin so the SPA sees same-origin paths.

Rejected for v1 as ceremony for a personal-use tool. Operator can do this if they want, but the SPA shouldn't *require* it — the default shape should be "two origins, CORS configured, just works." Leaving this to operator infrastructure means SM doesn't carry a hidden assumption that a reverse proxy is in place.

## Consequences

**Signed up for:**

- **`configState` carries both `daemonUrl` and `bridgeUrl`.** Both have hardcoded `http://localhost:<port>` defaults for first-paint / first-install per ADR 0003. Both are persisted via the daemon's `/v1/config`. The bridge URL specifically must be reachable from the operator's *browser*, not from the daemon — so the default must work on the operator's machine even when the daemon lives elsewhere.
- **Bridge serves CORS headers.** The bridge's HTTP setup includes `Access-Control-Allow-Origin` (default `*` for single-operator LAN; configurable). This is bridge-side work, not daemon-side.
- **SPA holds two transport lifecycles.** Daemon connection (HTTP, intermittent) and bridge connection (SSE, long-lived) fail independently. The UI must tolerate either being down without taking the other with it. (`bridgeState.connected` and a parallel `daemonState.connected` once the daemon-connection-status indicator lands.)
- **Two failure surfaces visible to the operator.** "Daemon unreachable" (config can't save, QSOs can't submit) and "Bridge unreachable" (no live rig state — manual mode per ADR 0009) are distinct conditions and surface as distinct toasts / banners.
- **Multi-rig future work means `configState.bridges[]`,** not a daemon-side rig registry. Per-bridge URLs in operator config; SPA routes the right SSE per rig. Daemon stays out of rig identity entirely.

**Accepted costs:**

- **Operator must configure two URLs** for split deployments. For all-on-one localhost, defaults work; for split, the operator types in one extra URL. This is documented in the SPA's settings UI when that lands.
- **CORS is a real config line on the bridge.** Misconfigured CORS = SPA can't connect to bridge. Mitigation: the bridge defaults to `*` for personal use; tightening is opt-in.
- **No single point of failure** is also no single point of recovery — operator may need to think about which service is down when something goes wrong. Two independent connection-status indicators in the UI is the antidote.

**Gained:**

- **Daemon stays narrow.** The "log + forward" scope is preserved with no rig-state code paths sneaking in. Future-us can read the daemon source and never find a CAT field.
- **Daemon is freely network-deployable.** Pi, NAS, home server, VPS — all viable without changing anything bridge-side.
- **Bridge stays operator-local.** No tunneling required to reach a serial port from a remote daemon; the only thing that needs the bridge is the SPA, which runs in the operator's browser.
- **Multi-rig generalizes cleanly.** Each rig host runs its own bridge. The daemon never has to track which rig is where; it just stores QSOs that arrive with rig identity in the payload.

## Triggers to revisit

- **A reason emerges that the daemon genuinely needs rig-state knowledge.** None foreseen — the QSO payload already carries freq/mode/VFO from the SPA, so the daemon never needs to ask the bridge anything. If a feature requires the daemon to know live rig state without an SPA in the loop, the assumption breaks and the topology needs reconsidering.
- **A native client appears that runs in-process with the bridge** (e.g. a future Gio-style app for the shack PC that owns serial directly, talking to the daemon for storage). Not a reason to revisit *this* ADR — just means the SPA isn't the only client. The daemon-bridge separation still holds.
- **Web Serial becomes viable** (cross-browser, no-prompt, persistent permission). Even then, the bridge's host-bound services (PTT, audio) keep the bridge as a separate process; the SPA-direct serial path would be an alternate route for CAT specifically, not a topology collapse.
- **Single-binary "smd-all" wrapper** is requested for the all-on-one case. That's a packaging decision, not a topology one — daemon and bridge can ship in one binary while remaining distinct origins on distinct ports inside it. ADR doesn't need to change for that.
- **Tunneling / discovery / auth complexity grows past static config.** If the operator needs zeroconf to find the bridge from the SPA, or mTLS between bridge and SPA, that's a new ADR; the two-origin separation here is unchanged.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — browser SPA choice; the upstream cause of why CAT can't live with the UI.
- ADR 0010 (`0010-rig-sse-wire-shape.md`) — wire shape; this ADR pins the host of that endpoint to the bridge.
- ADR 0003 (`0003-spa-config-daemon-only.md`) — `configState` source; both `daemonUrl` and `bridgeUrl` are operator-configurable fields per that model.
- ADR 0004 (`0004-daemon-vs-spa-responsibilities.md`) — daemon-vs-SPA split; this ADR adds the bridge as the third party in the topology.
- `docs/v2-design/topology.md` — full topology document; this ADR distills its load-bearing conclusion into a named decision.
- `docs/v2-design/bridge.md` — bridge architecture; this ADR ratifies bridge ownership of `/v1/rig/events`.
- CLAUDE.md "narrow daemon scope" invariant — the rule this decision protects.
- Memory `project_sm_serial_bridge` — bridge as a peer service; updated to record the daemon-network / bridge-local asymmetry.
