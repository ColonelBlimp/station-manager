# v2 design — service topology and deployment

**Status:** rewritten 2026-05-02 to reflect ADR 0013 (daemon owns the bridge as an internal subsystem) and ADR 0014 (upstream forwarding deferred). Supersedes the 2026-04-30 version of this doc, which described a two-process daemon-and-bridge-as-peers topology. The architectural separation that prior version protected (log/forward must not couple with rig state) is preserved at the **package-import graph** rather than the process boundary; see ADR 0013 for the reasoning.

## Default topology — single-binary, single-origin

For the dominant deployment shape (single operator, one rig, one shack PC) the daemon is one binary that owns:

- The SQLite log + upload queue (storage subsystem).
- Forwarder workers for QRZ / ClubLog / LoTW (forwarder subsystem, driver-shaped per ADR 0014).
- The HTTP API surface (`/v1/qso`, `/v1/config`, `/v1/enrich/callsign`, etc.).
- The embedded SPA (`//go:embed frontend/dist`, served at `/`, per ADR 0001).
- The CAT/serial subsystem — `internal/bridge` package — which owns the serial port, AUTO-mode CAT parsing, the rigctld-compat TCP listener, the SM-native SSE (`/v1/rig/events` per ADR 0010), the current-state cache, and PTT arbitration (ADR 0013).

```
┌──────────────────────────────────────────────────────┐
│                     Daemon binary                    │
│ ┌──────────────┐  ┌────────────┐  ┌───────────────┐  │
│ │   storage    │  │ forwarder  │  │     HTTP      │  │
│ │  (sqlite,    │  │ (drivers:  │  │  /v1/qso      │  │
│ │   upload q)  │  │  QRZ,      │  │  /v1/config   │  │
│ │              │  │  ClubLog,  │  │  /v1/forward  │  │
│ │              │  │  LoTW)     │  │  /  (SPA)     │  │
│ └──────────────┘  └────────────┘  └───────┬───────┘  │
│                                           │          │
│                  ┌────────────────────────┴───────┐  │
│                  │       internal/bridge          │  │
│                  │  (serial, CAT, rigctld TCP,    │  │
│                  │   /v1/rig/events SSE, PTT)     │  │
│                  └────────────────────────────────┘  │
└─────────────────────────┬────────────────────────────┘
                          │ same origin, single port
                          ▼
                  ┌───────────────┐
                  │    Browser    │
                  │  (SPA from    │
                  │   /, SSE from │
                  │   /v1/rig/    │
                  │   events)     │
                  └───────────────┘
```

**Static-ownership discipline at the package level:** the `internal/bridge` package exposes only its public Go interface (route registration, internal-API for rig state). Other internal packages — `internal/storage`, `internal/forwarder`, etc. — must not import it. Conversely, `internal/bridge` does not import storage or forwarder. The only shared imports are `internal/types` and the HTTP-server wiring layer that registers all routes. This enforces the narrow-daemon-scope invariant inside one binary just as well as the prior two-process topology did between binaries.

## Alternative deployments (opt-in)

The single-binary shape is the default. Other shapes are supported by configuration, not by separate code paths.

### Headless / SPA-less daemon

`cfg.Server.ServeSPA = false` (existing flag). Daemon serves only the API; the SPA is not embedded into HTTP routing. Useful for Unix-socket-only deployments, tests, scripted ingest. (ADR 0001.)

### Network-deployed daemon without local rig

Daemon runs on a Pi / NAS / VPS / home server. No rig is locally attached. Set `bridge.enabled: false` in daemon config (ADR 0013). Daemon does not open serial, does not register `/v1/rig/events`, does not run the rigctld TCP listener. The SPA loaded from this daemon either:

- has `bridgeUrl` left blank → SPA shows "rig: not configured" and operator works in manual-state mode (ADR 0009);
- has `bridgeUrl` set to a separate bridge process's URL → split-host deployment below.

This is also the shape a future upstream-forwarder daemon takes (ADR 0014).

### Split-host deployment (daemon and bridge on different machines)

Operator wants the daemon on a Pi / VPS for durable storage and reliable outbound, but the rig is on a different host (the shack PC). Run two binaries:

- Daemon on the storage host with `bridge.enabled: false`. Hosts SPA, serves `/v1/qso` etc., does not touch a serial port.
- Standalone bridge — `cmd/bridge` — on the rig host. Same `internal/bridge` package, wrapped as its own executable. Serves `/v1/rig/events` (and rigctld-compat TCP) on its own port.

SPA's `configState`:
- `daemonUrl` → daemon's address.
- `bridgeUrl` → standalone bridge's address.

This adds the cross-origin concern back: the standalone bridge sets `Access-Control-Allow-Origin` headers permissive enough for the SPA loaded from `daemonUrl` to subscribe (default `*` for single-operator LAN; tightenable). Auth via static bearer token (ADR 0014's auth-header threading) for any deployment exposed beyond LAN.

The split-host shape is preserved primarily for "the operator buys a home server" and "remote operating with bridge at home, daemon on a tunnel-terminating box" topologies. It is **not** the default and should not be assumed the dominant shape in code organization or operator documentation.

### Multi-rig

`invariants.md` "Multi-rig is a first-class assumption" — multi-rig is supported. In the default deployment, a single daemon binary's bridge subsystem can manage N serial ports (one per rig), with the rigctld frontend exposing each on its own TCP port (4532, 4533, ...) per hamlib convention, and the SM-native SSE carrying rig identifiers so SPA clients can subscribe per-rig.

If multi-rig means N physically distinct hosts, that's a split-host deployment per rig. Each rig host runs its own bridge (or its own daemon with bridge enabled, forwarding to a master per ADR 0014's deferred upstream-forwarding shape).

### Future: upstream forwarding / federation

ADR 0014 defers cluster / multi-daemon work explicitly. A future shape — local daemons forward QSOs to a master daemon, master forwards to QRZ/ClubLog/LoTW — is supported by adding one upstream-forwarder driver to the existing forwarder layer. The master is "just another daemon" with `bridge.enabled: false`. No new daemon mode, no new binary, no new topology — the prep-work in ADR 0014 (driver-shaped forwarders, threaded auth, namespaced enable-flags, `additional_data` provenance) is what makes that addition a one-driver change rather than a refactor.

**Do not pre-build cluster infrastructure.** ADR 0014 explicitly forecloses on master discovery, federation routing, multi-daemon UI, etc. The shape becomes real when a real driver appears.

### Future: SM Cloud (multi-tenant hosted service)

ADR 0016 defers the public-facing **SM Cloud** product (multi-user / multi-logbook hosted service, browser-accessible from anywhere, off-site backup) explicitly. Distinct from upstream forwarding above: SM Cloud is a separate codebase serving unrelated operators, not a master-daemon among one operator's own machines. When the cloud is built, the local daemon talks to it via one more forwarder driver (per ADR 0014's driver-shaped layer); the cloud itself decides its own storage and auth model in its own ADR.

ADR 0016 commits two **schema-shaped prep items** that are cheap now and unrecoverable later: globally-unique time-ordered QSO IDs (**UUIDv7 — SHIPPED 2026-05-06** as the canonical external identifier on every API response and on every ADIF record via `APP_SM_QSO_ID`; local int PK stays as storage detail) and a `qso_history` append-only audit table for edit/delete provenance (**SHIPPED 2026-05-07** — audit scope is UPDATE+DELETE only, FK by `qso_uuid`, full `json.Marshal(types.Qso)` snapshot in `before_image`, append-only enforced both by daemon code path and by `BEFORE UPDATE`/`BEFORE DELETE` triggers). Both are independently valuable for v1 (stable external IDs, "what did I change?" auditing) and they make a future SM Cloud upload a one-driver change rather than a log-wide re-identification.

**Do not pre-build cloud infrastructure.** ADR 0016 forecloses Postgres migration, user/account models, OAuth flows, multi-tenant code paths, public-facing API surface, cloud-aware SPA. SQLite stays the local storage; daemon stays single-operator.

## How a QSO actually flows (default deployment)

1. SPA opens an EventSource against `${configState.bridgeUrl}/v1/rig/events`. In the default deployment `bridgeUrl == daemonUrl`, so this is the daemon's own HTTP server, served by `internal/bridge`.
2. Bridge subsystem sends initial `rig-state` event from its current-state cache; SPA populates `catState`.
3. Operator turns the dial → rig pushes deltas via AUTO-mode CAT → bridge filters SPA-relevant fields → SSE delta event → SPA's `catState` merge updates → `displayedState` derivations re-run → VFO display reflects the new frequency.
4. Operator hits "Log Contact" → SPA `POST`s to `${daemonUrl}/v1/qso` with the QSO payload **including** the freq / mode / VFO captured from `catState` / `displayedState`. The daemon's storage subsystem accepts the payload as the source of truth for that QSO.
5. Daemon writes the QSO row + upload-queue row(s) atomically (`invariants.md` "One-fails-all-fail for QSO writes"). Forwarder worker picks up the upload-queue row asynchronously and dispatches to the appropriate driver(s).
6. Bridge subsystem knows nothing about the QSO ever happening. Storage / forwarder subsystems know nothing about the rig's current state — they only see what the SPA submitted.

The package-boundary discipline keeps these flows clean: storage and forwarder packages have no imports from `internal/bridge`; the bridge package has no imports from storage or forwarder.

## Service responsibilities (precisely)

### Daemon (`cmd/smd`)

- Owns the SQLite log and the upload queue (storage subsystem).
- Forwards QSOs to QRZ / ClubLog / LoTW per the driver-shaped forwarder design (forwarder subsystem; ADR 0014).
- Hosts the SPA (`//go:embed frontend/dist`) when `cfg.Server.ServeSPA == true` (ADR 0001).
- Hosts the bridge subsystem (`internal/bridge`) when `cfg.Bridge.Enabled == true` (ADR 0013) — owns serial, AUTO-mode CAT, rigctld-compat TCP, `/v1/rig/events`, PTT arbitration. Otherwise the bridge subsystem is wired as a no-op.
- Exposes the REST API the SPA consumes: `/v1/qso`, `/v1/config`, `/v1/enrich/callsign` (ADR 0017), `/v1/qso/{uuid}/uploads`, `/v1/rig/events` (when bridge subsystem enabled), etc.
- Auth header threading on every request (ADR 0014); validator a no-op by default, registerable for network-deployed cases.

### Standalone bridge (`cmd/bridge`) — opt-in

- Same `internal/bridge` package as the daemon's subsystem, wrapped as its own binary.
- Serves `/v1/rig/events` and the rigctld-compat TCP listener on its own port.
- Knows nothing about QSOs. Same boundary as the daemon's bridge subsystem.
- CORS configured (default `*` for LAN; tightenable).
- Used only in split-host deployment.

### SPA (`frontend/`)

- Browser tab(s) loaded from the daemon's origin (per ADR 0001).
- Two URL fields in `configState`: `daemonUrl` (everything except rig SSE) and `bridgeUrl` (rig SSE only). In default deployment they are equal.
- Two transports: HTTP/JSON to the daemon (config, QSO submit, enrichment) and SSE to the bridge endpoint (rig state per ADR 0010).
- Holds session-list state client-side (per `project_sm_session_scope` memory — session list is client-side, no daemon endpoints).
- Static-ownership for SPA state per ADR 0009 (catState / manualState / configState / displayedState).

## Practical concerns

### CORS

- **Default deployment:** no CORS — same origin (daemon serves SPA and `/v1/rig/events` from the same HTTP server).
- **Split-host deployment:** standalone bridge sets `Access-Control-Allow-Origin: *` for single-operator LAN; tightenable to specific daemon origins for stricter setups.

### Authentication

- **Default deployment:** no auth needed. SPA still sends `Authorization: Bearer <token>` header if `configState` has a token configured (ADR 0014); daemon middleware ignores when no validator is registered.
- **Network-deployed daemon (LAN or beyond):** static token in operator config; daemon registers a validator that requires the header. Operator types the token into the SPA's settings on first load; SPA stores it in `configState` (which is daemon-authoritative per ADR 0003 — chicken-and-egg fix: token goes through localStorage initially or via a one-time bootstrap form).
- **Split-host bridge:** same static-token shape, separately configured per service. Don't build a login flow for a single-user personal tool.

### Service discovery

Static config in the SPA (`daemonUrl`, `bridgeUrl`) — types in localStorage / `configState`. For default deployment, defaults to `http://localhost:<port>` and Just Works. mDNS / Bonjour is foreclosed by ADR 0014 — overkill for personal use; revisit only if multi-operator scenarios become real.

### Bridge subsystem failures

The SPA must degrade gracefully when the bridge connection drops or the bridge subsystem is disabled. Show a stale-state indicator on rig-state values and let the operator type frequency by hand (per ADR 0009's `manualState` and `displayedState.editable` derivation; ADR 0010's `bridgeState.rigResponding` flag). Same shape as `invariants.md` "enrichment never blocks logging" applied to rig state — external (or subsystem) source down, log without it.

### Subsystem disable-flag pattern

Recognized config pattern (ADR 0014's prep-work item #3): subsystems that hold host-bound resources or operator-specific roles get `<subsystem>.enabled: bool` config flags with sensible defaults. Today's instances:

- `bridge.enabled: true` (default; flip to `false` for headless / network-deployed daemon).
- `cfg.Server.ServeSPA: true` (default for TCP deployments; flip for headless).

Future subsystems follow the same shape rather than inventing per-subsystem ad-hoc flags.

### Multiple SPA clients / SSE fan-out

Daemon's `/v1/rig/events` (or standalone bridge's, in split-host) fans the underlying bridge data out to N concurrent SSE subscribers. Standard pattern: a slice of subscriber channels, fan-out goroutine per subscriber writing from a buffered channel (depth ~16). Slow subscriber disconnected when the buffer fills.

### Localhost vs LAN binding

Daemon binds on `127.0.0.1` by default (loopback only — secure for the all-on-one shape). Operator opts into LAN binding via config. This makes the default deployment secure-by-default and the multi-host case a config flip.

## What this rules out

- **Daemon log/forward subsystems reaching into bridge state at the package-import level.** Forbidden import: `internal/storage` or `internal/forwarder` → `internal/bridge`. Reviewers (and any future lint) treat this as a violation of the narrow-daemon-scope invariant.
- **Bridge subsystem importing storage or forwarder.** Forbidden in the other direction. Bridge knows about rigs; storage knows about QSOs; the only shared types are in `internal/types`.
- **Daemon-as-bridge-broker.** No proxying-bridge-data-from-a-separate-bridge-process pattern. In default deployment the daemon *is* the bridge (subsystem). In split-host deployment the SPA talks to the bridge directly; daemon does not relay.
- **Speculative cluster infrastructure.** Per ADR 0014: no master discovery, no federation routing, no cluster config schema, no multi-daemon UI, no master-daemon-specific code. Upstream forwarding is a future driver, not a topology.
- **Speculative cloud infrastructure.** Per ADR 0016: no Postgres migration, no user/account model, no auth flows, no multi-tenant code, no public-facing API, no cloud-aware SPA. SM Cloud is a future codebase, not a v1 topology concern.
- **Two-process default.** ADR 0012's "two processes, two origins" topology is superseded by ADR 0013. The split-host deployment is opt-in, not the default.

## Connection to existing decisions

- Reinforces `invariants.md` "Daemon scope is explicitly narrow" — boundary moved from process to package, but the rule is unchanged.
- Reinforces `invariants.md` "Multi-rig is a first-class assumption" — the bridge subsystem (whether in-daemon or standalone) is multi-rig-capable from day one.
- Reinforces `invariants.md` "Two-frontend bridge shape" — rigctld-compat TCP and SM-native SSE / NDJSON. The frontends live inside `internal/bridge` regardless of which binary hosts that package.
- Reinforces `invariants.md` "Forwarding never blocks logging" — driver-shaped forwarders dispatch asynchronously per ADR 0014.
- Compatible with `bridge.md`'s two-frontend shape; the package boundary does not change which frontends exist.
- Compatible with `api.md`'s daemon REST surface; bridge endpoints (`/v1/rig/events`) are added under the same API surface in default deployment.
- Compatible with `forwarding.md` and `forwarding-implementation.md` — those are forwarder-subsystem-only, never need to know about rigs.
- Per ADR 0001, the SPA / `cmd/logging` Gio-era code is parked, not deleted, until SPA reaches feature parity.

## Cross-references

- ADR 0001 — browser SPA hosted by daemon; `ServeSPA` flag is the precedent for the namespaced enable-flag pattern.
- ADR 0010 — rig SSE wire shape; second-revision note pins the host to the daemon (with bridge subsystem) in default deployment.
- ADR 0012 — superseded; preserved as the reasoning trail of how "two processes, two origins" was considered.
- ADR 0013 — daemon owns the bridge as an internal subsystem; this document's load-bearing decision.
- ADR 0014 — upstream forwarding deferred; names the four prep-work items justified by v1 scope today.
- ADR 0016 — SM Cloud deferred; names two schema prep items (globally-unique time-ordered QSO IDs + edit/delete audit table) that are cheap now and unrecoverable later.
- `bridge.md` — bridge architecture; `internal/bridge` package shape.
- [ui-toolkit.md](ui-toolkit.md) — UI toolkit reconsideration; depends on this topology being SPA-friendly.
- [cat-performance.md](cat-performance.md) — bridge-side perf analysis.
- Memory `project_sm_serial_bridge` — bridge as daemon subsystem in default; split-host as opt-in.
- Memory `project_sm_restructure` — captures the daemon + clients + bridges decision; this document narrows the topology shape.
