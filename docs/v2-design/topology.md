# v2 design — service topology and deployment

**Status:** refined 2026-04-30. Builds on the existing daemon + clients + bridges decision (recorded in memory `project_sm_restructure`) and the existing `bridge.md` design. This document pins down a load-bearing distinction that previous discussions had been ambiguous about: **the bridge is a peer of the daemon, not a subordinate.**

## The load-bearing distinction

Every v2 service belongs in one of two buckets, determined by physics:

- **Host-bound concerns** — anything that needs physical attachment to the radio. Serial port, USB, sound card, PTT lines, audio bridges for FT8 capture. These services have to live on the host the rig is wired to. They cannot be relocated.
- **Network-shaped concerns** — anything that needs only disk and outbound HTTPS. SQLite log, forwarding to QRZ/ClubLog/LoTW, the SPA, REST APIs. These services can live anywhere with a network path.

The CAT bridge is host-bound. The daemon is network-shaped. **Coupling them locks the daemon to the rig host, which it doesn't need to be.** This contradicts the existing CLAUDE.md "narrow daemon scope" invariant.

## Three peer services, no parent/child

```
┌─────────────┐        ┌─────────────┐        ┌─────────────┐
│   Bridge    │        │   Daemon    │        │   Client    │
│ (rig host)  │        │  (anywhere) │        │  (anywhere) │
├─────────────┤        ├─────────────┤        ├─────────────┤
│ rigctld TCP │        │ /v1/qso     │        │ subscribes  │
│ /v1/rig     │        │ /v1/forward │        │ to both via │
│ /v1/rig/    │        │ /v1/log     │        │ HTTP + SSE  │
│   events    │        │ /  (SPA)    │        │             │
└─────────────┘        └─────────────┘        └─────────────┘
       ▲                       ▲                       │
       │                       │                       │
       └───────────────────────┴───────────────────────┘
                  (clients talk to both directly)
```

**Crucial property: the bridge and the daemon never talk to each other directly.** They share clients. The client subscribes to the bridge for live rig state and submits QSOs to the daemon, carrying the freq/mode it captured from the bridge in the QSO payload. This is what makes the two services independently deployable.

## How a QSO actually flows

1. Client subscribes to the bridge's `GET /v1/rig/events` (SSE). Receives live frequency, mode, VFO updates as they happen.
2. Client renders the logging form. Frequency/mode fields are prefilled from the live stream.
3. Operator hits "Log Contact" → client `POST`s to the daemon's `/v1/qso` with the QSO payload **including** the freq/mode/VFO captured from the bridge.
4. Daemon stores + queues for forwarding. Knows nothing about rigs — accepts whatever freq/mode the client submitted.
5. Bridge knows nothing about that QSO ever happening.

The daemon never asks the bridge "what's current?" — the client carries that information because it's already subscribed.

## Service responsibilities (precisely)

### Bridge (`cmd/bridge` — to be built)

- Owns the serial port and the rig connection.
- Polls (or listens to async/auto-tx events from) the rig and maintains current state.
- Exposes two frontends:
  - **rigctld-compatible TCP** for third-party tools (WSJT-X, fldigi, JTDX, etc.) — preserves Hamlib interop.
  - **SM-native HTTP/SSE** for in-house clients — `GET /v1/rig`, `GET /v1/rig/events`, `POST /v1/rig/freq`, `POST /v1/rig/mode`, etc.
- Stateless about QSOs. Knows nothing about the log, forwarding, lookups, or anything else.
- See `bridge.md` for the existing detailed design.

### Daemon (`cmd/smd` — already exists, will grow)

- Owns the SQLite log and the upload queue.
- Forwards QSOs to QRZ/ClubLog/LoTW per the existing forwarder design.
- Hosts the SPA (`//go:embed frontend/dist`) — see [ui-toolkit.md](ui-toolkit.md).
- Exposes the REST API the SPA consumes: `/v1/qso`, `/v1/lookup/{call}`, `/v1/forward/status`, etc.
- Stateless about rigs. Never opens a serial port. Never asks the bridge anything.

### Client (`frontend/` — to be built)

- Browser tab(s) loaded from the daemon's origin.
- Subscribes to the bridge for rig state.
- Submits QSOs and reads forwarding status from the daemon.
- Holds session-list state (per `project_sm_session_scope` memory — session list is client-side, no daemon endpoints).

## Deployment topologies this enables

The peer model means the same code runs in every shape without modification:

- **All-on-one (default for personal use):** daemon + bridge + browser all on the shack PC. Everything localhost. No CORS, no auth, no network configuration.
- **Server + shack PC:** daemon on a home server (or VPS) for stable storage and reliable outbound HTTPS to QRZ/ClubLog. Bridge on the shack PC because that's where the rig is. Browser on either machine.
- **Remote operating:** bridge on the shack PC at home, daemon wherever, laptop browser somewhere else with a tunnel.
- **Multiple rigs:** each rig host runs its own bridge instance. One daemon aggregates QSO storage. Browser shows whichever bridge is selected.

You only get all four for free if the bridge is genuinely a peer, not a daemon plugin.

## Practical concerns

### CORS

When the SPA loads from the daemon's origin and fetches the bridge's origin, the browser enforces CORS. Bridge sets `Access-Control-Allow-Origin: *` for single-user personal use, or scopes to the daemon's origin for stricter setups. Easy, but real — needs to land in the bridge's HTTP setup.

For the all-on-one case (daemon, bridge, browser on the same host) the SPA can be configured to talk to `localhost` for both, which is same-origin if the bridge is colocated and the daemon proxies the bridge's events. But adding the proxy is premature complexity — direct calls + CORS is one config line.

### Authentication

For LAN-only use, no auth needed. For "daemon on a VPS, bridge at home over the open internet," add a static token in config (operator-supplied) and require it as a header on every request. Frontend reads from localStorage on first load. Don't build a login flow for a single-user personal tool.

### Service discovery

Static config in the client (`daemon.url`, `bridge.url`). For the all-on-one case, defaults to `http://localhost:<port>` and Just Works. mDNS/Bonjour would be cute but overkill for personal use.

### Bridge crashes / disconnect

The SPA must degrade gracefully. Show "rig: disconnected" and let the operator type frequency by hand. This is the existing **"enrichment never blocks logging" invariant** applied to rig state — same shape: external service down, log without it.

### Multiple clients / fan-out

Bridge SSE stream needs to support N concurrent subscribers. Each subscriber gets the same event stream. Standard pattern: bridge maintains a slice of subscriber channels, fan-out is a goroutine per subscriber writing from a buffered channel. Buffered (depth ~16) so a slow subscriber doesn't stall the others. A lagging subscriber gets disconnected when the buffer fills.

### Localhost vs LAN choice

Daemon and bridge both bind on `0.0.0.0` by default (LAN-reachable) but firewall to localhost-only when no LAN config is present. This makes the all-on-one case secure-by-default and the multi-host case a config flip.

## What this rules out

- **Daemon-as-bridge-broker** — earlier ambiguous wording in design notes ("daemon brokers events from the bridge") is wrong if "brokers" means "events flow daemon → clients." Events flow bridge → clients directly.
- **Daemon needing serial-port access** — never. If a feature needs a serial port, it goes in the bridge or a sibling host-bound service.
- **Bridge needing the QSO log** — never. If a bridge feature ever needs to know what's been logged, the design should be reconsidered.

## Connection to existing decisions

- Reinforces the CLAUDE.md "narrow daemon scope" invariant.
- Compatible with `bridge.md`'s existing two-frontend (rigctld + SM-native) design.
- Compatible with `api.md`'s daemon REST surface.
- Compatible with `forwarding.md` and `forwarding-implementation.md` — those are daemon-side only, never need to know about rigs.
- The 2026-04-21 Gio choice is reconsidered separately in [ui-toolkit.md](ui-toolkit.md); the topology decision here is independent of which UI toolkit wins.

## Cross-references

- `bridge.md` — the bridge's detailed design (rigctld + SM-native frontends, transport abstraction)
- `api.md` — the daemon's REST surface
- [ui-toolkit.md](ui-toolkit.md) — UI toolkit reconsideration; depends on this topology
- [cat-performance.md](cat-performance.md) — bridge-side perf analysis
- Memory: `project_sm_serial_bridge` — captures the bridge model
- Memory: `project_sm_restructure` — captures the daemon + clients + bridges decision
