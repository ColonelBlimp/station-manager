---
number: 0069
title: Non-loopback TCP requires an explicit insecure-network acknowledgement (ST-3a)
status: Accepted (operator-ratified 2026-08-16; implemented same day)
date: 2026-08-16
---

# 0069 — Non-loopback TCP requires an explicit insecure-network acknowledgement (ST-3a)

## Context

The security-trust-boundary audit (`docs/reviews/internal-security-trust-boundary-audit.md`,
finding ST-3) found a design drift. The daemon's HTTP API has no authentication
— filesystem permissions on a Unix socket were the intended authorization model
(api.md §2), and TLS was said to be mandatory for any TCP listener (api.md §6).
But TCP is now the **first-run default** (loopback `127.0.0.1:8080`, for the
embedded SPA), and a **non-loopback** TCP bind is a supported configuration that
validation permitted with only an **advisory** — the daemon started normally.

A non-loopback bind exposes the *entire* unauthenticated, unencrypted API to
every host that can reach the port: not just QSO submission (all the old advisory
named), but reading/exporting the log, mutating station/SMTP/lookup/forwarder
config, restarting the daemon, commanding the rig, keying the tune carrier, and
operating FT8. The CSRF/rebinding guard (`requireSameOrigin`) is deliberately not
authentication — it allows non-browser requests without Origin, and its
loopback-Host trust is a DNS-rebinding defence a native LAN client can forge.

The full remedy — authenticated LAN access — is a real feature (a browser session
system, or loopback-only plus an authenticated TLS reverse proxy) and a topology
decision in its own right. It should not be rushed under audit pressure. But the
*silent* drift — an RF-capable control plane reachable unauthenticated over the
LAN because an advisory was easy to miss, plus docs that contradict the code —
can and should be closed now.

## Decision

Split ST-3 into **ST-3a (this ADR, done)** and **ST-3b (deferred, open)**.

**ST-3a:** A TCP bind on any address that is not recognisably loopback — a
specific LAN/public IP, a wildcard (`0.0.0.0` / `::` / empty host), or a
non-localhost hostname — is **startup-fatal** (`config.Load` aborts; PUT → 400)
**unless** the operator sets `server.allow_insecure_network: true`. With the
acknowledgement the daemon starts and logs a standing advisory that enumerates
the full API + RF exposure. The flag is config-file/startup-only and is
deliberately absent from the `/v1/config` wire surface, so it can never be set by
a remote client. Documentation is reconciled to three honest postures (loopback
TCP / owner-private Unix socket / acknowledged non-loopback TCP); the conflicting
historical api.md statements get status notes, not rewrites.

**ST-3b (deferred):** the actual secure-remote topology — Option 1 (make direct
non-loopback TCP invalid; loopback/socket + authenticated TLS proxy) vs Option 2
(build browser authentication as a system). Recorded here as an open decision.

## Alternatives considered

- **Keep the advisory (status quo).** Rejected: a security-relevant, RF-capable
  exposure that depends on the operator noticing one non-fatal log line is exactly
  the silent drift the audit flagged. Fail-closed is the honest default.
- **`allow_insecure_lan` naming.** Rejected in favour of `allow_insecure_network`:
  the daemon cannot prove an address is private, and a "specific IP" bind may be
  publicly routed. "network" does not over-promise "LAN".
- **A second, RF-specific acknowledgement** when bridge/FT8 TX is enabled on a
  non-loopback bind. Rejected: a second checkbox adds ceremony without adding
  authorization. One acknowledgement covers the whole daemon; its wording states
  plainly that it grants unauthenticated clients full enabled API **and RF**
  control.
- **Wildcard binds treated as loopback-only** (leaning on `hostAllowed`).
  Rejected: `hostAllowed`'s loopback-Host trust is a rebinding defence, not peer
  authentication — a native LAN client forges the Host — so a wildcard is exposed
  and must require the acknowledgement too.
- **Make the acknowledgement remotely settable (`/v1/config`).** Rejected: an
  insecure-posture switch that a reachable client could flip defeats the purpose.
  Server-bind settings were already off the wire surface; the flag inherits that.
- **Build the full auth layer now (ST-3b).** Deferred, not rejected: it is a
  feature and a topology decision, not a validation fix, and doing it under audit
  pressure risks the wrong architecture.

## Consequences

- A non-loopback bind that previously started with a warning now **refuses to
  start** until the operator adds `server.allow_insecure_network: true`. This is a
  deliberate, breaking change for that (rare, opt-in) deployment — the migration
  aid is the fatal message, which names the one switch and what it grants.
- The loopback-TCP first-run default and the Unix-socket deployment are
  unaffected; nothing changes for the common cases.
- The daemon can still be run insecurely on a LAN — but only as a conscious,
  documented, acknowledged choice, described honestly as a migration posture.
- The docs no longer contradict the code on default transport, supported
  deployments, and the auth/TLS obligation.
- This does **not** provide authenticated LAN access. Any claim that ST-3 is
  "fixed" must say ST-3a (silent exposure + doc drift) — ST-3b (the remedy) is
  open.

## Triggers to revisit

- ST-3b is scoped: pick Option 1 (loopback/socket + TLS proxy) or Option 2
  (browser auth). Either supersedes the acknowledgement as the primary control.
- The embedded SPA becomes a *supported* LAN feature for multiple operators —
  forces the browser-auth design (Option 2).
- A packaged/appliance deployment ships with a non-loopback default — the
  acknowledgement stops being a niche opt-in and the auth story becomes urgent.
