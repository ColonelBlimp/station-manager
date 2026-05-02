---
number: 0013
title: Daemon owns the bridge as an internal subsystem; single binary by default
status: Accepted
date: 2026-05-02
supersedes: 0012
---

# 0013 — Daemon owns the bridge as an internal subsystem

## Context

ADR 0012 was drafted earlier the same day to fix a drift where the SPA was about to call the daemon's origin for a bridge-owned endpoint. 0012 codified "the bridge is a separate process, distinct origin" as the load-bearing topology. Within hours of that ADR landing, the operator stepped back to question whether the topology it was protecting is the topology actually being run.

Three observations forced the reconsideration:

1. **The dominant deployment shape is single-operator-on-the-shack-PC.** Personal use, one rig, one operator, everything on one machine. Network-deployed daemon (Pi, NAS, VPS) is a real but rare topology — the operator has named it as a possibility, not as a daily mode.
2. **The cost of the two-process / two-origin shape is paid every day for the rare case.** Two URLs in `configState`, CORS as a default config concern, two transports with two failure modes, two processes to install / manage / log / monitor. For a personal-use tool with no shipping deadline and one user, this is significant operator-experience cost.
3. **Most of the architectural separation that ADR 0012 protected is a property of the *code*, not the *process boundary*.** The narrow-daemon-scope invariant says "log/forward subsystems must not reach into rig state." That can be enforced by package boundaries and import discipline inside a single binary just as well as by process boundaries between two binaries — provided the discipline is named and the package graph is enforced.

The redesign also benefits from a piece of design hygiene: separating "daemon owns the bridge as a subsystem" (this ADR) from "what would let cluster/upstream-forwarding deployments work later" (ADR 0014). The latter is *not* part of this decision; this ADR settles only the topology of the v1 / personal-use shape.

## Decision

**The bridge becomes an internal subsystem of the daemon binary.** One process, one origin, one binary in the default deployment.

Concrete shape:

- **Single binary.** `cmd/smd` (the daemon) imports a bridge package — call it `internal/bridge` — that owns serial-port acquisition, AUTO-mode CAT parsing, the rigctld-compat TCP listener, the in-house SSE wire (per ADR 0010), the current-state cache, and PTT arbitration. There is no `cmd/bridge` separate executable in the default shape.
- **`/v1/rig/events` is served by the daemon's HTTP layer**, with the bridge subsystem registered as a route handler. Same origin as `/v1/qso`, `/v1/config`, the SPA. CORS is not a default config concern.
- **Subsystem disable flag.** Daemon config grows `bridge.enabled` (default `true`). When `false`: no serial port acquisition, no rigctld TCP listener, no `/v1/rig/events` route registered, the bridge package's wiring is a no-op. This is what makes the network-deployed-daemon-without-rig case (and the upstream-forwarder case in ADR 0014) work — the same binary, configured to skip its rig-side subsystem.
- **Static-ownership discipline preserved at package boundaries.** The bridge package exposes only its public Go interface (route registration, internal-API for getting current rig state). Packages that own log/forward concerns (`internal/storage`, `internal/forwarder`, etc.) **do not import** the bridge package. Conversely, the bridge package does not import the storage or forwarder packages. The only shared imports are types and the HTTP server wiring layer. This is the same protection ADR 0009 gave us through static state-object ownership, lowered to the package graph.
- **Split-host deployment preserved as an opt-in.** A separate `cmd/bridge` executable can still be built (sharing the same `internal/bridge` package code) for the rare case of "daemon on a network host, bridge on the rig host." When that's used, the daemon runs with `bridge.enabled: false` and the SPA's `configState.bridgeUrl` is operator-set to the standalone bridge process's address. The split-host shape is now an opt-in topology rather than the default.
- **`configState.bridgeUrl` and `configState.daemonUrl` both still exist.** In the default deployment they are equal (`bridgeUrl` defaults to `daemonUrl`); in the split-host deployment the operator sets `bridgeUrl` to the bridge's address. Keeping both fields means the SPA's URL composition logic doesn't have to branch — it always reads `configState.bridgeUrl` for rig SSE and always reads `configState.daemonUrl` for everything else.

This ADR **does not** make any decision about cluster mode, upstream forwarding, federation, multi-daemon deployments, or master/worker topologies. Those concerns are addressed (and deliberately deferred) in ADR 0014.

## Alternatives considered

### Two-process topology (ADR 0012, superseded)

Daemon and bridge as separate binaries, separate origins. SPA holds two URLs, two transports, two failure surfaces. CORS configured on the bridge by default. Static ownership enforced by process boundary.

Rejected as a default for this project: the shape that gets used 95% of the time should not be the shape that's the most expensive operationally. The split-host topology is real but rare; the personal-use topology is real and constant. Building for the rare case at the cost of the common one is the wrong trade for a personal-use tool with a single operator. ADR 0012's reasoning about narrow-daemon-scope is preserved by package-boundary discipline rather than process-boundary discipline; the protection is identical, the cost is much lower.

### In-process bridge with no disable flag

Daemon binary always includes a running bridge subsystem. No `bridge.enabled` knob.

Rejected: forecloses the network-deployed daemon. If the daemon is on a Pi or VPS with no rig attached, an always-on bridge subsystem would either fail (trying to open a non-existent serial port) or be ceremony (running an idle CAT parser). The disable flag is cheap (a few `if cfg.Bridge.Enabled` guards in the wiring layer) and it's what makes the same binary deployable in both shapes.

### Build-tag-gated bridge subsystem

Compile two daemon binaries: one with the bridge subsystem (for the shack PC) and one without (for the network-deployed case). Use Go build tags to gate.

Rejected: doubles the binary surface — two binaries to release, two binaries to test, two binaries for the operator to choose between. The runtime config flag is operationally simpler for the operator (set `bridge.enabled: false` in the daemon's config; restart). The build-time gate is warranted only if the bridge subsystem brings in heavyweight or sensitive dependencies that must not ship to the network-deployed case; today it does not.

### Daemon proxies a separately-running bridge process even on the same host

Bridge process runs alongside the daemon on the shack PC; daemon proxies bridge events to the SPA so the SPA only ever talks to one origin.

Rejected: pays the operational cost of two processes (two service files, two log streams, two failure modes, two configs) and pays the additional cost of building a proxy layer in the daemon, just to recover same-origin. Same-origin is recovered for free by the in-process subsystem. This was an "alternatives considered" entry under ADR 0012 (rejected there because it gave the daemon rig-state knowledge); under 0013 it loses for a different reason — operational complexity for no architectural gain.

### Bridge as a long-running goroutine inside the daemon, no package boundary

In-process, but no `internal/bridge` package — bridge code is sprinkled across the daemon's existing packages.

Rejected: throws away the static-ownership discipline that's been doing real work in the design (ADR 0009 for SPA state, ADR 0010's three-flag rule, etc.). The protection that "log/forward code never reaches into rig state" is what keeps the daemon's log/forward surface stable as new bridge features are added. Lose the package boundary and the narrow-daemon-scope invariant degrades to a code-review convention. Keeping the bridge in its own package costs one directory and pays for itself the first time someone proposes coupling them.

## Consequences

**Signed up for:**

- **Single binary becomes the default ship target.** `cmd/smd` produces one executable that is the entire shack-PC deployment. CI builds one binary; release artifacts are one binary; operator install is one binary. `cmd/bridge` (separate) ships only if/when split-host deployments are exercised.
- **Daemon config grows a `bridge` section.** `bridge.enabled` (default `true`), `bridge.serial.port` (path), `bridge.serial.baud`, `bridge.rigctld_listen` (TCP address for the rigctld-compat frontend), etc. All bridge-specific config lives under this section so the daemon's existing config stays uncluttered.
- **`internal/bridge` becomes a real package with a documented public interface.** Other internal packages must not import it except for the explicit wiring layer that registers HTTP routes. This is the package-boundary discipline that replaces the process-boundary discipline.
- **The `bridge.enabled: false` deployment shape must work cleanly.** Daemon starts, has no serial port, has no rigctld listener, has no `/v1/rig/events` route. SPA loaded from this daemon has `bridgeUrl == daemonUrl` by default — but if the operator hasn't overridden it, the SPA's bridge connection will fail. This is correct behaviour: the SPA should show "rig: not configured" / "bridge unreachable", and the operator either sets `bridge.enabled: true` (gives the daemon a rig) or sets `bridgeUrl` to a separate bridge process.
- **CORS handling moves to "only required for split-host."** The daemon's HTTP server still needs `Access-Control-Allow-Origin` for split-host but not for the default. A future settings UI can flip it on; the default config has it absent.
- **The operator's mental model shifts from "two processes, two URLs" to "one daemon, one URL, one config."** Better for the personal-use case; the split-host case retains a configuration story but is no longer the path of least resistance.

**Accepted costs:**

- **Static-ownership discipline now depends on package-import discipline.** A future code change could violate the bridge package boundary by accident. Mitigation: a lint rule (or a doc.go-level comment + code review) that flags imports between the bridge package and the storage/forwarder packages. Could be a `go vet`-style check.
- **The daemon binary grows slightly.** Bridge code (rigctld parsing, AUTO-mode CAT decoding, serial library) ships even when `bridge.enabled: false`. For Go binaries this is a few hundred KB at most; not meaningful for a daemon that already embeds a full SPA bundle.
- **Tests for the bridge subsystem run in the daemon's test surface.** The bridge package's tests run as part of the daemon's `go test ./...`. This is fine — they were going to be co-located in the same Go module anyway.
- **The split-host deployment has a slightly less obvious entry point.** Standalone `cmd/bridge` binary exists but isn't the primary release artifact. Documented in `topology.md` for the operator who needs it.

**Gained:**

- **The default deployment shape is the simple shape.** One binary, one config file, one log stream, one systemd unit, one port to firewall. The operator-experience improvement for the case the operator actually lives in is non-trivial.
- **Same-origin SPA by default.** No CORS configuration for the default case. Browser fetch / EventSource Just Works for both `/v1/*` and `/v1/rig/events`.
- **Single connection-status indicator covers both daemon and bridge concerns** in the default deployment. Two indicators only become meaningful in split-host shape, which is also when the operator has explicitly opted into the complexity.
- **The architectural invariants are preserved.** Narrow daemon scope (log/forward must not couple with rig state) survives via the package boundary. ADR 0010's wire shape is unchanged. ADR 0009's SPA-side static ownership is unchanged. Nothing important is lost; the change is purely about where the boundary is enforced.
- **Future split-host topologies remain supported.** The same `internal/bridge` package can be wrapped in a `cmd/bridge` standalone executable when needed. Package code is reused; only the wiring layer differs.
- **Sets the foundation for ADR 0014's deferred upstream forwarding.** A network-deployed daemon (with `bridge.enabled: false`) is exactly the shape an upstream-forwarder daemon would take. Building this disable-flag now means upstream forwarding becomes a forwarder-driver question later, not a topology question.

## Triggers to revisit

- **Bridge subsystem code grows large enough to feel like its own product.** If `internal/bridge` becomes a substantial codebase that justifies its own release cadence (independent versioning, separate documentation, third-party plugins), splitting it back out into a separate binary is the natural answer. Today it is small.
- **A bridge feature emerges that genuinely cannot ship inside the daemon binary.** Examples: a hardware library with a license incompatible with the daemon's, a binary footprint problem, a security model that requires running the bridge as a different OS user. None foreseen for v1; trigger to revisit if any appear.
- **The split-host deployment becomes the dominant shape.** If real operating use shifts to "daemon on a Pi, bridge on the shack PC" as the common case (e.g. because the operator buys an always-on home server for storage), the cost-benefit reverses and the default should follow. Today the operator has named this as rare.
- **Multi-rig changes the picture.** Multi-rig in the default shape means one daemon manages N serial ports through the bridge subsystem (per `invariants.md` "Multi-rig is a first-class assumption"). If multi-rig means N physically distinct hosts each with their own rig, that's the split-host deployment by another name and the topology question reopens — but per ADR 0014, that's an upstream-forwarding question not a "redo 0013" question.
- **Cluster / upstream forwarding becomes a real driver.** ADR 0014 forecloses on building cluster infrastructure speculatively, but the moment a real driver appears (multi-station club, contest aggregation, etc.), the network-deployed daemon's role is revisited. Even then, this ADR's structural decision (bridge as a daemon subsystem) holds; only the question of *how* multiple daemons cooperate gets answered.
- **`bridge.enabled: false` deployments turn out to need their own binary anyway.** If the daemon's bridge dependencies bloat the binary unacceptably, the build-tag-gated alternative becomes worth reconsidering. Cheap to do later if needed.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — browser SPA choice; the upstream cause of why CAT can't live with the UI. The "Serial / CAT cannot live in the SPA" consequence bullet was reworded as part of this ADR's landing to no longer say "must be a separate process" — the new wording is "must run somewhere other than the browser," which the daemon can be.
- ADR 0010 (`0010-rig-sse-wire-shape.md`) — wire shape; second revision note added to clarify host (daemon, with bridge subsystem) without changing the wire format.
- ADR 0012 (`0012-daemon-and-bridge-separate-origins.md`) — superseded by this ADR. Preserved as the reasoning trail of how "two processes, two origins" was considered before being collapsed.
- ADR 0014 (`0014-upstream-forwarding-deferred.md`) — explicitly defers cluster / federation work; names the four prep-work items (driver-shaped forwarders, auth header from day one, subsystem disable-flag *which is settled here*, additional_data provenance) that make a future upstream-forwarding driver a one-driver addition rather than a refactor.
- `docs/v1-analysis/invariants.md` "Daemon scope is explicitly narrow" — restated as part of this ADR's landing to be a package-boundary rule rather than a process-boundary rule. Same protection, lower-level enforcement.
- `docs/v2-design/topology.md` — substantially rewritten as part of this ADR's landing. Default topology is single-binary; split-host and federation are alternative deployments.
- `docs/v2-design/bridge.md` — bridge architecture; in-scope for review when `internal/bridge` is implemented. The "bridge as a peer service" framing in the existing doc is updated to "bridge as a daemon subsystem with a separately-buildable standalone front."
- Memory `project_sm_serial_bridge` — updated to reflect bridge as a daemon subsystem with split-host as an opt-in.
- CLAUDE.md "narrow daemon scope" invariant — restatement same as invariants.md.
