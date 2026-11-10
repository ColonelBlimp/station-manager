---
number: 0103
title: Authenticate smd to the desktop app with a pinned TLS identity
status: Proposed
date: 2026-11-10
---

# 0103 — Authenticate smd to the desktop app with a pinned TLS identity

**Proposed (2026-11-10; revised after codex-046 and codex-047). Design only. Nothing is
authorised or built.** This is W-0027's C1: the authenticated channel ADR 0102 requires before the
desktop app executes any daemon-served UI. It is for review by the operator and Codex before any
code.

## Context

ADR 0102 (Accepted) puts the UI in an Electron shell that loads it from `smd`. Its Requirements,
as corrected in review (codex-034, codex-035), say:
- **The shell authenticates `smd`, its content and every later connection.** It executes no
  daemon-served UI and grants no host capability until the content is authenticated as `smd`'s, and
  that authentication covers the transport carrying later requests, streams and reconnects.
- **Acceptance:** an impostor before startup or after daemon loss supplies no executable UI,
  receives no operating credential and gains no privileged host call; the local waiting page stays
  independent. A version response or an origin string is not authentication.
- **The operating credential** (C2) stays out of page script and is never sent off the machine.

Why this is needed today:
- `smd` listens on plain HTTP at `127.0.0.1:8080` by default (`internal/config/config.go:1358`).
  While it is down, **any process able to bind that address can listen there first**: other
  unprivileged users sharing the network namespace can, subject to OS policy; root and the accepted
  same-user limit (ADR 0102) are separate. Loopback is not a per-user boundary.
- A check made once is not enough: the renderer opens its own connections (`fetch`, `WebSocket`,
  `EventSource`), and a daemon restart or reconnect could reach a replacement listener.
- The A1 shell's availability probe says only that something answers (`desktop/src/waiting.mjs`).

Facts the choice rests on:
- The SPA uses relative URLs and derives its WebSocket scheme from the page:
  `location.protocol === 'https:' ? 'wss:' : 'ws:'`
  (`frontend/app/src/lib/api/socket-reviving.ts:67`); `EventSource` takes a relative URL
  (`sse-reviving.ts:60`). It runs unchanged over HTTPS.
- `smd` has no TLS listener today (no `crypto/tls` use in `internal/api` or `cmd/smd`).
- Electron lets the main process decide every TLS server-certificate verification for a session
  (`session.setCertificateVerifyProc`; [Electron session API](https://www.electronjs.org/docs/latest/api/session#sessetcertificateverifyprocproc)).
  Its behaviour for WebSocket and EventSource connections is stated from the documentation and is
  to be confirmed in the implementation's tests.
- The working directory (`utils.WorkingDir()`) is created `0755`
  (`internal/utils/working_dir.go:66`), but `os.MkdirAll` leaves an **existing** directory as it
  is, and nothing validates its owner, permissions or path components today. Its resolution can
  also fall back to the executable's directory (`AbsDirPathForExecutable`, line 44). So the
  working directory cannot be assumed to be a trust anchor; the identity needs its own checks
  (point 1).

## Decision

**`smd` serves the desktop app over HTTPS on loopback with its own per-install TLS identity, and
the shell trusts that one certificate and nothing else.**

1. **The identity, in a protected place (codex-046).** `smd` keeps a key pair and a self-signed
   certificate for `127.0.0.1` (Go standard library, `crypto/ecdsa` P-256, `crypto/x509`) in a
   dedicated **identity directory** under the canonical working directory, created `0700`.
   - **Validated on every use, with different roles (codex-047):**
     - **both `smd` and the shell** check the protected path: the identity directory and its
       files are owned by the operator's user, the directory is `0700`, the certificate is not
       writable by group or others, and no component of the path from the user's home (or the
       canonical data root) down to the directory is writable or replaceable by another user;
     - **`smd`** also checks its private key (`0600`, parseable) and that the key and certificate
       belong together;
     - **the shell** never opens the private key: it validates the path and the certificate and
       pins it, and TLS itself establishes that the peer holds the matching key.

     Anything else is refused with an error naming the path, never repaired or replaced silently.
     Creating a new key with mode `0600` alone is not enough: the certificate is the pin, so its
     location must be as protected as the key's.
   - **Creation, published as one unit (codex-047):** when no identity directory exists, `smd`
     prepares a temporary directory beside it (owner-only) and, in order and each checked: writes
     and syncs both files, **syncs the prepared directory itself** (a file sync does not persist its
     directory entry; codex-048), publishes it with **one** rename of the directory to its final
     name, and syncs the parent. Any failure before the rename prevents publication. If only the
     final parent sync fails, the identity is published but its durability is unconfirmed: `smd`
     reports that state and never silently replaces the identity (its own handling, not the config
     writer's abstraction). Acceptance asserts the order of these barriers and injects failures on
     both sides of publication; process-termination tests alone do not prove power-loss durability.
     A crash before that rename leaves no identity (only an unpublished temporary directory, removed
     at the next start); after it, a complete one. A published directory with a missing, malformed,
     mismatched or unexpected file is refused, with the files named. Acceptance interrupts each
     boundary (before the files are complete, before the rename, after it) and distinguishes an
     unpublished identity from a published partial one.
   - **Replacement** is deliberate: delete the directory; the next `smd` start creates a new
     identity. A running app keeps its pin (point 3) and shows the daemon as unavailable until it
     is restarted, which re-reads the identity.
2. **The app listener.** `smd` gains a **second listener for the desktop app: HTTPS on
   `127.0.0.1`**, at **port 8443 by default, configurable** (operator ruling 2026-11-10: "yes, 8080
   and 8443"). A fixed port keeps the app's origin stable, so the page's own storage, such as
   floating-panel positions, survives restarts. It serves the same handlers, guards and embedded SPA
   as the HTTP listener. The existing HTTP listener is unchanged at **8080**: the browser keeps
   operating the station until the operating-client check (C2) is switched on, and becomes the
   view-only listener afterwards (ADR 0102 ruling 1).
   - **If the app port cannot be bound** (operator ruling 2026-11-10): until C2 is switched on,
     `smd` starts anyway and reports it plainly, naming the port, in `smd.log` and as a Station
     Event, because the browser is still the operating client; once C2 is on, it is a start-up
     error. A foreign program holding the port only ever fails the pin (the app shows the waiting
     page), so the report is what makes that situation understandable.
3. **The pin.** At launch the shell finds the identity directory by the same canonical
   resolution `smd` uses: an explicit working directory, then `SM_WORKING_DIR`, then the packaged
   XDG default (`internal/utils/working_dir.go`). It never applies the daemon's
   executable-directory fallback to Electron's executable, and never learns anything from the
   network; where automatic discovery cannot establish the same location (for example a portable
   or development run), an explicit matching path is required. It validates the location as in
   point 1 and computes the SHA-256 fingerprint of the certificate's **DER encoding** (not the PEM
   file text).
   - **The verifier contract** (`session.setCertificateVerifyProc`, Electron 44.9.3): it answers
     `0` (accept) **only** when the host is `127.0.0.1` and the presented certificate's fingerprint
     equals the pin, and `-2` (reject) for everything else, including certificates the system
     would trust; it never delegates (`-3`). The exact pin deliberately replaces normal PKI trust
     for this self-signed identity. There is no other bypass: no `certificate-error` handler
     accepts anything. The verifier is installed before the first request in **every** session an
     app window uses. Electron caches verification results, which is consistent with a fixed pin.
   - **The pin is fixed for an app lifetime.** It is never learned or replaced from a handshake. A
     later change of identity on disk takes effect at the app's next start.
   - **App started before `smd` has ever run** (no identity yet): no pin, so nothing is trusted;
     the waiting page shows, and the shell re-reads the identity directory on each probe until a
     valid identity appears, then fixes the pin.
   - **Validity dates** (operator ruling 2026-11-10: "yes, 10 years and dates not enforced"): with
     the pin authoritative, the verifier and the probe both accept the pinned certificate
     **regardless of its dates**, consistently. The certificate is issued with a **10-year**
     validity so other tools do not complain.
   - **The port** comes from the same local configuration `smd` reads (8443 unless configured),
     never from the network.
4. **What that gives, connection by connection.** Every connection Chromium opens to the app's
   origin, page loads, `fetch`, `wss:` and `EventSource` alike, and every reconnect after a daemon
   restart, completes only with the holder of `smd`'s private key (the TLS handshake proves
   possession of it; RFC 8446 §4.4.3). A listener that took the port cannot complete the handshake,
   so it supplies no content and receives no request headers (and so no credential). Because only
   pinned-TLS content can run at the app's origin, an origin check is a check for authenticated
   content; ADR 0102's actual IPC sender-frame, approved-window and argument checks still apply.
5. **Confinement and Origin, exactly (codex-046).**
   - **The shell's request filter** allows exactly `https://127.0.0.1:<app port>` and
     `wss://127.0.0.1:<app port>`, and rejects `http:` and `ws:` (no downgrade), any other port, and
     off-origin redirects. The A1 filter allows `ws:` only beside an `http:` origin
     (`desktop/src/hardening.mjs`), so it needs this HTTPS/WSS adaptation.
   - **The daemon checks Origin per listener.** `wsOriginExact` already derives the expected scheme
     from `r.TLS` and compares scheme plus host and port (`internal/api/stream_ws.go`); the HTTPS
     listener requires its own exact origin, and the HTTP and Unix listeners keep theirs. No global
     rule admits either origin on either listener. Tests cover a wrong scheme, a wrong port and an
     absent Origin before stream admission.
6. **Waiting page versus a loaded workspace (codex-046).**
   - **Cold start and failed main-page loads:** the shell's local waiting page, as in A1. Its probe
     asks `GET /v1/version` through the pinned session (`ses.fetch`, not Node's global `fetch`,
     which does not use the session's verifier), keeping A1's redirect refusal and timeout; a
     listener without the key never counts as up.
   - **A loaded, authenticated workspace that loses the daemon** does not navigate to the waiting
     page: a failed `fetch` or stream handshake shows the app's single "daemon unavailable" state
     (ADR 0102 ruling 6), with operating actions disabled, and recovers when the real `smd` returns,
     keeping drafts and panel state. Every connection to a replacement listener is refused
     throughout. A reconnect is never an app or session boundary (ADR 0100); C4's stop and disarm on
     presence loss apply, and nothing restores a run or arms TX.
7. **The operating credential rides on this channel** (C2, its own design): the shell's main
   process adds it as a request header only to requests for the pinned origin, so it is sent only
   inside an authenticated connection and never reaches page script.

## Requirements this carries

- **Acceptance (from ADR 0102):** with an impostor listening on the app port before start, after a
  daemon stop, and between a restart and a reconnect: no page from it executes, no request reaches
  it past the handshake, no credential is sent to it, and no host call is granted. What the
  operator sees depends on where the app was (point 6): **at cold start or after a failed main-page
  load**, the local waiting page; **in an already loaded workspace**, the "daemon unavailable"
  state with drafts and panels kept. Either recovers when the real `smd` returns. A version string
  served by an impostor without the key never counts.
- **The impostor matrix (fixture-only, never the station):** for page load, `fetch`, `wss:`,
  `EventSource`, downloads, the probe, and reconnects **after a prior successful verification**,
  each against: a listener with a different key; a listener presenting the **public pinned
  certificate without its private key**; a redirect off the origin; and identity-file failures
  (unsafe ownership or permissions, a substituted certificate, a mismatched or partial pair). The
  measure is application requests that never arrive, not verifier callback counts. WebSocket and
  EventSource coverage is a hypothesis until this matrix proves it.
- **Reversion proof (codex-047):** remove **only the fingerprint comparison**, keeping the
  verifier's acceptance path (returning `0` for the fixture host), so that the different-key case
  starts to succeed and proves the test reaches the pin. Removing the whole custom verifier would
  make both self-signed fixtures fail under default PKI and prove nothing. The copied-certificate
  case (the public pinned certificate with a different private key) must still fail with the
  comparison removed, because TLS key possession is a separate check. Normal pinned recovery
  still works with the comparison in place.
- **A loaded workspace survives an impostor:** type a draft, replace the daemon with an impostor,
  restore the real daemon: the draft and panels remain, and no request reached the impostor.
- **Both listeners under the lifecycle graph (ADR 0070):** one set of application services, guards
  and admission state behind both listeners (not a second station); partial-start cleanup, stop
  accepting and draining HTTP, SSE and hijacked WebSockets on both. `api.Server` owns a single
  listener today (`listener`, `boundListener`, `internal/api/server.go:35,129`), so calling its
  `ListenAndServe` twice is not enough. The ruled bind failure is exercised before and after C2,
  and no sibling listener survives a failed start.
- **No fallback to plain HTTP** for the app: a TLS failure is an unavailable daemon, never a reason
  to try `http:`.
- **Nothing leaves the machine:** the C1 proof found Electron's spellchecker downloading a
  dictionary from Google (`redirector.gvt1.com`); the shell disables the spellchecker before any
  request, and a check asserts that the pin is never asked about any host but `127.0.0.1`.
- **Private key handling:** created `0600`, never logged, never served, never sent anywhere; only
  `smd` opens it, and the shell reads only the certificate. An identity is created only when none
  is published; a published partial or mismatched identity is an error that names the files (no
  silent replacement).
- **Same-user limit (ADR 0102, accepted):** a process running as the operator can read the key. The
  pin keeps out other users and stray listeners, not the operator's own processes.
- **Host guards** accept the app listener's host and port as well as the existing ones
  (`internal/api/csrf.go`); Origin is checked per listener (point 5).

## Alternatives considered

### Unix socket and a main-process proxy

`smd` already offers a Unix socket in an owner-private directory (`server.go:696-728`), so the
filesystem would authenticate the listener. But Chromium cannot open a Unix socket: every request,
stream and download would go through a custom protocol handler or proxy in the shell's main
process. A custom scheme does not carry `WebSocket` (the SPA's socket transport; stated from
Electron's protocol documentation, to be confirmed), so it needs a substantial adapter, and an
**unauthenticated** loopback TCP proxy in front of the socket would recreate the impostor gap
(codex-034). Not chosen for this slice's scope; an authenticated proxy is not ruled out in
principle.

### A challenge-response check before loading

The shell challenges the listener to prove it knows a shared secret, then loads the page. Rejected:
it authenticates one connection, while Chromium opens its own later connections and reconnects,
which a replacement listener could answer (codex-034).

### Checking who owns the listening socket

The shell reads the listener's owner from `/proc/net/tcp` and loads only if it is the operator.
Rejected: Linux-only, and racy between the check and Chromium's connections.

### Mutual TLS (client certificates) for the operating credential

Would let `smd` authenticate the app at the TLS layer. Not chosen here: Electron selects client
certificates from the system store through an event, not from an application file, and C2 is its
own design. Recorded for C2.

### TLS on the existing listener

Simpler (one listener), but plain-HTTP browsers would lose access during the transition, and the
view-only browser path would face a self-signed certificate. Rejected for now; a single listener can
be revisited once browsers are view-only.

## Consequences

- **`smd`:** a key and certificate in the working directory, a second listener with its own
  configuration (`docs/v2-design/config.md`), and Host/Origin guards that accept two origins. TLS
  adds no new dependency (Go standard library).
- **The shell:** the pin and verifier, a pinned availability probe, and tests with impostor
  listeners for every connection type. The A1 fixture harness gains a TLS mode with its own key, so
  the tests never touch the station.
- **A1's restriction lifts only after this is implemented and validated:** the shell may then load
  the real `smd`'s UI over the app listener.
- **The browser path is unchanged** until C2 is switched on.
- **Operator-visible:** nothing, unless the identity files are deleted, in which case the app
  follows the new certificate at its next launch.

## Open questions

- **Whether the WebSocket and EventSource behaviour under the verifier holds as documented:**
  answered for the fixture by the C1 proof (W-0027, 2026-11-10): the verifier covers page loads,
  `fetch`, `wss:`, EventSource, downloads and the probe, including reconnects after a prior
  successful verification. It is still to be confirmed against `smd`'s own listener.

## Triggers to revisit

- Browsers become view-only (C2 on): consider moving to one TLS listener.
- Electron changes the certificate-verification API or its coverage of WebSocket.
- Remote operation becomes a requirement (ADR 0102 trigger): loopback pinning no longer suffices.

## References

- ADR 0102 (Requirements: authentication of `smd`, its content and every later connection;
  operating credential; host interface), W-0027 C1, C2 and C4; codex-034, codex-035 (the review
  findings this answers).
- `internal/config/config.go:1358`; `internal/api/csrf.go`; `internal/api/stream_ws.go`;
  `internal/api/server.go:696-728`; `internal/utils/working_dir.go:66`;
  `frontend/app/src/lib/api/socket-reviving.ts:67`, `sse-reviving.ts:60`;
  `desktop/src/waiting.mjs`.
- [Electron session API: `setCertificateVerifyProc`](https://www.electronjs.org/docs/latest/api/session#sessetcertificateverifyprocproc).
- [Electron 44.9.3 Session reference](https://raw.githubusercontent.com/electron/electron/v44.9.3/docs/api/session.md)
  (verifier results `0`, `-2`, `-3`; cached results; `ses.fetch`);
  [RFC 8446 §4.4.3](https://www.rfc-editor.org/rfc/rfc8446#section-4.4.3) (CertificateVerify);
  [Go `os.MkdirAll`](https://pkg.go.dev/os#MkdirAll); codex-046 (this revision's review).
