# Internal security and trust-boundary audit

**Status:** ST-1..ST-7 all FIXED (2026-08-16); ST-3b (authenticated LAN access) is the one open follow-up, tracked in ADR 0069  
**Reviewed:** 2026-08-14  
**Scope:** network listeners, browser trust, authentication, credential transport and
storage, filesystem authorization and permissions, tenant isolation, path containment,
CAT/RF control gates, and sensitive logging under `internal/`; build/deployment files
read only where they define an `internal/` package's security boundary  
**Code changes:** none; this document is the review deliverable

## Executive summary

The code has substantial security engineering already in place. Mutating daemon
requests use a fixed destination allowlist plus Origin checking; configuration APIs
mask stored secrets; credential-shaped error data is deliberately scrubbed; CAT
commands are default-deny, value/range checked, identity gated, and excluded while TX
is active or uncertain; SM Cloud authenticates before every private handler and scopes
tenant reads/writes; request bodies, response bodies, batches, concurrent requests and
long-lived streams are generally bounded.

The review found **seven action themes**: four P1 and three P2. The most direct default-
deployment defect is that the DNS-rebinding Host allowlist is skipped for every GET.
A rebound page can read the loopback daemon's API—including QSO/config-derived data,
SSE streams, and opt-in pprof—because the service never validates the attacker-owned
Host on a “safe” method. The embedded control UI is also frameable, allowing a hostile
page to induce same-origin operator actions through clickjacking.

The design documents say authentication and TLS must be revisited once TCP/non-loopback
exposure exists. TCP is now the first-run default and non-loopback binding is supported;
the implementation only warns and still starts an unauthenticated control plane that
can change configuration and drive RF-capable features. That documented trigger has
arrived.

Credentialed forwarders accept remote plain HTTP, and their default clients follow
redirects without a credential-origin/downgrade policy. Dummy-credential probes showed
QRZ, ClubLog and SM Cloud constructors all accepting a remote HTTP destination, and a
307 replaying a QRZ credential-bearing POST from HTTPS to a different HTTP origin.
SM Cloud's plain-HTTP LAN staging posture is intentional, so remediation needs an
explicit insecure-LAN exception rather than silently breaking that workflow.

Filesystem authorization is inconsistent with its documentation: a Unix socket became
`0777` under umask `000`, while the design says its permissions are the whole auth
story. A pre-existing `0755` database directory yielded a `0644` SQLite database, and
sent-ADIF archives are deliberately created as `0755` directories plus `0644` files.
These are local/multi-user risks, hence P2, but they should not be left to ambient umask
or installation history.

Seven overlay-only probes reproduced the current behavior and were removed. No real
credential was read or printed; all credential probes used dummy values. No production
source was changed.

## Findings at a glance

| ID | Priority | Area | Disposition |
|---|---:|---|---|
| ST-1 | P1 | GET/HEAD bypass the TCP Host allowlist, so DNS rebinding can read the loopback API | New |
| ST-2 | P1 | Embedded operator SPAs are frameable and therefore clickjackable | New |
| ST-3 | P1 | Non-loopback TCP starts without authentication or TLS after the documented revisit trigger | Known assumption whose trigger has arrived |
| ST-4 | P1 | Credentialed HTTP clients allow plaintext destinations and unrestricted credential redirects | New; SM Cloud LAN exception is intentional |
| ST-5 | P2 | Unix-socket authorization depends on ambient umask | New |
| ST-6 | P2 | QSO-bearing databases, archives and legacy logs are not consistently owner-private | New consolidation |
| ST-7 | P2 | The extractable ClubLog build key has no enforced private-artifact boundary | Accepted risk with an unenforced trigger |

Priority meanings follow the other internal reviews: P0 is release-gate work, P1
should be closed before a serious release, P2 is important correctness or
operability work, and P3 is useful hardening.

## ST-1 — safe methods bypass the rebinding destination check (P1)

`requireSameOrigin` returns immediately for GET, HEAD and OPTIONS before calling
`hostAllowed` at
[`internal/api/csrf.go:31`](../../internal/api/csrf.go). The comment treats those
methods as harmless because they do not mutate state. That is only the CSRF half of the
browser boundary. The Host allowlist is the DNS-rebinding half and must protect
confidential reads too.

In a rebinding attack, JavaScript is loaded from `attacker.example`, then that name is
resolved to `127.0.0.1`. The browser sends `Host: attacker.example:8080` and regards the
daemon response as same-origin with the hostile page. Unsafe requests are correctly
rejected because `hostAllowed` compares against loopback/configured bind destinations
at [`internal/api/csrf.go:71`](../../internal/api/csrf.go). GET never reaches that code.

The readable surface is not just a health endpoint. It includes masked-but-sensitive
station configuration, QSO/logbook/history data, hardware identifiers, live general/
rig/FT8 SSE, and—with profiling explicitly enabled—heap, goroutine, CPU and trace
endpoints registered at
[`internal/api/server.go:372`](../../internal/api/server.go). Some nominal GETs also
initiate work: callsign enrichment can schedule an upstream refresh. SOP/CORS does not
save this case because rebinding makes the daemon response same-origin with the
attacker's name.

### Reproduction

An overlay probe sent `GET /v1/config` through the real middleware with both Host and
Origin set to `attacker.example:8080`. It returned 200. The same Host on a mutating
request is rejected by the existing tests, confirming that the gap is method placement,
not the allowlist itself.

### Action

Separate destination validation from Origin validation:

1. For every TCP request, validate Host before the method switch. A foreign Host must
   get the same static 403 regardless of GET/HEAD/POST. Preserve the current Unix-
   socket policy separately; an arbitrary curl Host on a directly opened Unix socket
   is not a DNS signal.
2. Apply Origin checks only to unsafe methods as today. A cross-origin GET with an
   allowed destination remains subject to normal browser SOP, while a rebound GET is
   stopped by Host.
3. Keep wildcard binds fail-closed to loopback Hosts. Do not weaken the allowlist to
   make LAN hostnames convenient; add explicit allowed public hosts only as part of the
   authenticated LAN design in ST-3.
4. Use one outer middleware for the host decision so new API, SPA, manual and pprof
   routes cannot bypass it by registration order.

### Required tests

For TCP loopback, specific-IP and wildcard binds, table-test GET/HEAD/POST with allowed,
foreign and rebound Host values. Exercise `/v1/config`, an SSE route, a static SPA asset
and opt-in pprof through the real handler. The rebound GET must be 403 before the route
runs; normal loopback and configured-specific-IP reads must continue to work. Retain
the existing Origin tests for unsafe methods.

> ✅ **FIXED (working tree, awaiting operator's push).** `4b500ad9`, operator rulings
> 2026-08-16. `requireSameOrigin` now runs `hostAllowed` BEFORE the method switch, so the
> DNS-rebinding destination check applies to every method (GET/HEAD/OPTIONS included) and is
> host-first (a request that is both rebound and cross-origin is rejected on Host). The Origin
> check stays unsafe-method-only; a foreign Host gets the same static `cross_origin` /
> "host not allowed" 403 for any method; Unix sockets (no DNS vector) are unaffected. Tested
> across loopback / specific-IP / wildcard binds × GET/HEAD/OPTIONS/POST × allowed/rebound
> Host, asserting 403 AND the route handler did not run, plus a full-chain rebound-GET on
> `/v1/config` and `/`. Collateral: full-chain GET tests that relied on the default
> `example.com` Host now set a loopback Host.

## ST-2 — the embedded control UI is frameable (P1)

The daemon serves the configuration, logbook and consolidated operator SPAs directly
at [`internal/api/server.go:388`](../../internal/api/server.go). `spaHandler` sets only
`Cache-Control` before delegating to `http.FileServer` at
[`internal/api/spa.go:27`](../../internal/api/spa.go). Neither that handler nor an outer
middleware emits `Content-Security-Policy: frame-ancestors ...` or
`X-Frame-Options`.

A hostile page can therefore place the loopback UI in a transparent or disguised
iframe. The attacker cannot directly read the framed DOM, but can induce the operator
to click UI controls. Requests made by the framed Station Manager JavaScript originate
from the daemon's own origin, so the current Origin/Host guard correctly sees them as
same-origin. The reachable effects include configuration saves, rig retunes, tune/
FT8 actions, QSO edits/deletes, email/export operations and restart. This is distinct
from ST-1: fixing rebound reads does not stop a legitimate loopback page being framed.

### Reproduction

An overlay probe fetched `/app/` through the real server. It returned 200 with neither
`X-Frame-Options` nor a CSP `frame-ancestors` directive.

### Action

Add a response-security middleware for browser-served content. At minimum emit:

- `Content-Security-Policy: frame-ancestors 'none'` (the authoritative modern control);
- `X-Frame-Options: DENY` for older clients; and
- `X-Content-Type-Options: nosniff` plus a conservative `Referrer-Policy` while this
  boundary is centralized.

`frame-ancestors` must be an HTTP response header; a meta tag is not equivalent. A
full script/style CSP can be introduced separately after inventorying the built SPA
assets—it is not required to close this finding.

### Required tests

Assert the frame-denial headers on `/app/`, `/config/`, `/logbook/`, `/manual/`, root
redirects and SPA fallback routes. Test both HTML entry documents and assets so a
future handler replacement cannot silently remove the outer boundary.

> ✅ **FIXED (working tree, awaiting operator's push).** `e594cce2`, operator rulings
> 2026-08-16. A single OUTERMOST `securityHeaders` middleware (outside `logRequests` /
> `requireSameOrigin` / `recoverPanic`) sets, via `Header().Set` before the handler runs, on
> EVERY response — API, SPA, redirects, ST-1 403s, recovered-panic 500s, SSE, opt-in pprof:
> `Content-Security-Policy: frame-ancestors 'none'`, `X-Frame-Options: DENY`,
> `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`. CSP is `frame-ancestors`
> only; a full script/style CSP is separate work. Tested across the root redirect (unfollowed),
> a SPA document, a SPA fallback, a real static JS asset, a representative API response, and an
> ST-1 rebound 403.

## ST-3 — non-loopback TCP is an unauthenticated, unencrypted control plane (P1)

The configuration layer explicitly knows the risk. A non-loopback bind produces only
an advisory saying the API has no auth and accepts QSO submissions from every reachable
host at [`internal/config/config.go:1287`](../../internal/config/config.go). Validation
does not reject it and the server starts normally.

The impact is broader than QSO submission. A native LAN client can send the expected
Host, omit Origin, and access every route: read/export log data, change stored station/
SMTP/lookup/forwarder configuration, restart the daemon, command the rig, start a tune
carrier, and operate FT8 automation when those subsystems are enabled. The browser
guard is not authentication and intentionally allows non-browser requests without
Origin.

This is design drift with an explicit historical trigger. The transport design says
Unix-socket permissions are the authorization mechanism and there is no network/LAN
story at [`docs/v2-design/api.md:57`](../v2-design/api.md). It also says authentication
must be revisited upon TCP exposure and TLS is mandatory for any TCP listener at
[`docs/v2-design/api.md:321`](../v2-design/api.md). TCP is now the code's first-run
default, and non-loopback TCP is a supported configuration. The later status text still
says to revisit auth when non-loopback exposure appears at
[`docs/v2-design/api.md:756`](../v2-design/api.md). It has appeared.

The loopback default reduces remote-native exposure and should remain usable, but
“trusted LAN” is not a durable authorization model for RF-capable control: guest Wi-Fi,
compromised IoT systems, containers and other local principals are common boundary
crossings.

### Action

Choose and document one supported remote topology before keeping non-loopback binding:

1. **Narrowest safe option:** make direct non-loopback TCP invalid. Serve the daemon on
   loopback or an owner-private Unix socket and require an authenticated TLS reverse
   proxy for remote access.
2. If the embedded LAN SPA remains a supported feature, design authentication as a
   browser system, not just a handler token check. Native `EventSource` cannot attach
   an arbitrary Authorization header, so use a secure HttpOnly session cookie after a
   credential bootstrap, or replace SSE consumption with a fetch-based stream that can
   send a bearer header. Keep the Origin/Host controls as CSRF/rebinding defenses; auth
   does not replace them.
3. Until that design lands, require an explicit `allow_insecure_lan`-style acknowledgement
   for the existing posture and make the warning name all read/config/RF effects, not
   only QSO submit. This is a migration aid, not the final security control.
4. Update the transport ADR/API text so code defaults, supported deployments, auth and
   TLS requirements no longer contradict one another.

### Required tests

Pin startup rejection for non-loopback binds without the required security posture,
authentication on every private route (including SSE and pprof), uniform 401 behavior,
token/session redaction, CSRF behavior after authentication, logout/revocation, and
TLS/proxy header assumptions. Include a native client that forges Host and omits Origin;
it must not become authorized by looking browser-compatible.

> ✅ **ST-3a FIXED (committed on main).** `505e0566` + review-fix `e78c7617`, operator
> rulings + ADR **0069**, 2026-08-16. Split into **ST-3a (done)** and **ST-3b (open)**.
> ST-3a makes a non-loopback TCP bind (specific IP, wildcard `0.0.0.0`/`::`/empty host, or
> non-localhost hostname) **fail-closed**: startup-fatal (`Load` aborts / PUT 400, code
> `insecure_bind_unacknowledged`) unless the operator sets `server.allow_insecure_network:
> true`, at which point the daemon starts and logs a standing advisory enumerating the
> **full** API + RF exposure (not just QSO submit). Wildcards require the ack too — the CSRF
> guard's loopback-Host trust is a rebinding defence, not peer auth. The ack is
> config-file/startup-only (absent from the `/v1/config` wire surface, so not remotely
> writable). The pprof advisory now names in-memory disclosure and stays advisory on an
> acknowledged bind. Docs reconciled: `config.md` §5.1 (three postures), `api.md` status
> notes on the conflicting historical auth/TLS text, ADR 0069. Tests pin: Load-refuses per
> non-loopback shape; acknowledged-starts + comprehensive advisory; loopback/Unix (incl.
> zoned `::1%zone`) never insecure; ack not remotely writable. The review-fix corrects
> `isLoopbackBind` to accept an IPv6 zone via `netip.ParseAddr`.
>
> ⚠️ **ST-3b REMAINS OPEN (the actual remedy).** ST-3a closes the *silent/unacknowledged*
> exposure and the doc drift; it does **not** provide authenticated LAN access. The topology
> decision — Option 1 (loopback/owner-socket + authenticated TLS proxy) vs Option 2 (browser
> auth as a system) — is deferred and recorded in ADR 0069. Do not mark ST-3 wholly fixed.

## ST-4 — credentialed HTTP clients allow plaintext and permissive redirects (P1)

Transport rules vary by integration:

- QRZ lookup already requires HTTPS for credentialed remote URLs, with a loopback HTTP
  test exception.
- QRZ Logbook resolves a config endpoint and constructs a default client without
  validating the URL at
  [`internal/forwarding/qrz/qrz.go:142`](../../internal/forwarding/qrz/qrz.go).
- ClubLog does the same for its realtime/delete endpoints at
  [`internal/forwarding/clublog/clublog.go:227`](../../internal/forwarding/clublog/clublog.go).
- SM Cloud validates only that the scheme is either HTTP or HTTPS, so any remote HTTP
  host passes at
  [`internal/forwarding/smcloud/smcloud.go:150`](../../internal/forwarding/smcloud/smcloud.go).
- `validateForwarders` checks names, actions and numeric limits but no endpoint transport
  policy at [`internal/config/config.go:1428`](../../internal/config/config.go).

These requests carry a QRZ API key, ClubLog account application password plus shared
application key, or an SM Cloud bearer token and full QSO/evidence data. Remote HTTP
exposes them to passive LAN/path observers and active intermediaries. SM Cloud's LAN
staging runbook deliberately permits plain HTTP, but the config contains no explicit
marker that distinguishes that accepted temporary posture from an accidental Internet
HTTP URL.

All credentialed clients also use the standard redirect policy. There is no
`CheckRedirect` under `internal/`. Go replays POST bodies for 307/308 and copies an
Authorization header to exact/subdomain matches; it does not reject HTTPS-to-HTTP
downgrades. Thus transport validation on only the first URL would still be incomplete.
The same policy concern applies to QRZ lookup and SM Cloud reconcile/export/evidence
sync, which reuse credentials on additional requests.

### Reproduction

Overlay probes used dummy values only:

- production constructors for QRZ, ClubLog and SM Cloud each accepted a credentialed
  `http://remote.example` destination; and
- an HTTPS test server returned a 307 to a different HTTP test origin. The QRZ client
  replayed the form body, and the sink received the dummy API key.

### Action

Centralize a credentialed-client policy rather than fixing one integration at a time:

1. Validate the final configured URLs during both Load and PUT/build. Require HTTPS for
   credential-bearing remote endpoints. Retain HTTP for loopback tests. For intentional
   SM Cloud LAN staging, require an explicit `allow_insecure_http` acknowledgement and
   emit a high-signal startup warning; do not infer consent merely from an RFC1918
   address.
2. Give credentialed clients a `CheckRedirect` policy that rejects scheme downgrade and
   cross-origin redirects. Prefer exact scheme+host+effective-port equality; do not
   forward a bearer token to a subdomain merely because the standard library regards it
   as related.
3. Apply the helper to QRZ lookup, QRZ/ClubLog forwarders, SM Cloud submit/reconcile/
   export and evidence sync. Keep per-call timeouts and response limits intact.
4. Sanitize redirect-policy errors with the same URL discipline already used for QRZ
   query credentials and config diagnostics.

### Required tests

Cover HTTPS success; remote HTTP rejection; loopback HTTP test support; the explicit
SM Cloud LAN exception; HTTPS→HTTP, cross-host, cross-port and subdomain redirects;
relative same-origin redirects; 301/302/303/307/308; and both body-carried credentials
and Authorization headers. A refused redirect's sink must receive no request, and no
error/log may contain the dummy credential.

> ✅ **FIXED (committed on main).** ST-4a `cf269984` + ST-4b `63322da3`, operator rulings
> 2026-08-16. Centralized in a new **`internal/securehttp`** package, applied to all nine
> credential-bearing clients (qrz/qrzcq/clublog/smcloud forwarders, smcloud reconcile +
> export, evidence sync, qrz + qrzcq lookups).
> - **ST-4a — transport:** a credentialed URL must be https, or plain http only to a
>   loopback host (`netip`, so zoned `::1` counts); a remote-http endpoint fails
>   construction (daemon refuses to start / PUT 400), URL-free error. The QRZ + QRZCQ
>   lookup predicates were folded into the shared policy. SM Cloud's LAN-staging cleartext
>   is acknowledged by a **config-file-only** `forwarders[].allow_insecure_http`, valid for
>   the `smcloud` type only (rejected elsewhere, not ignored), not on the wire surface,
>   preserved by `mergeForwarders`; the daemon logs a standing startup warning naming the
>   forwarder. Docs reconciled (`config.md`, `smcloud-deploy.md`).
> - **ST-4b — redirects:** credentialed clients follow only **same-origin** redirects
>   (scheme+host+effective-port vs the *original* origin; relative allowed; downgrade,
>   upgrade, cross-host, cross-port, subdomain refused; uniform across 301/302/303/307/308;
>   refused target gets zero requests). `securehttp.Do` replaces net/http's `*url.Error`
>   wrapper — which embeds the redirect target incl. query — with a URL-free sentinel, so a
>   refusal never leaks the target. Reversion-proofed both wiring halves (Harden + Do).
>
> Scope note (operator, 2026-08-16): pre-existing runtime dial/TLS/timeout errors were
> **deliberately out of scope** for this pass — if a later review proves an initial-request
> runtime error can expose query credentials or userinfo, it is a **separate** sanitization
> finding, not a gate for ST-4.

## ST-5 — Unix-socket authorization depends on ambient umask (P2)

The design says filesystem permissions on the Unix socket are the entire authorization
story at [`docs/v2-design/api.md:59`](../v2-design/api.md). `ListenAndServe` safely
refuses to unlink a regular file, but after `net.Listen` it stores the listener and
serves it without setting or verifying the socket mode at
[`internal/api/server.go:468`](../../internal/api/server.go).

Socket permissions therefore come from `0777 & ^umask`. Under the common `0022` mask
the result is typically `0755`, whose lack of group/other write usually prevents other
users from connecting. Under group-friendly `0002` it becomes `0775`; under `0000`,
`0777`. Any principal with socket write permission is fully authorized to the same
read/config/RF surface described in ST-3.

The Unix fallback path is also `/tmp/smd.sock`, not an owner-private runtime directory.
Sticky-bit `/tmp` limits deletion but does not repair a permissive socket inode.

### Reproduction

An isolated overlay probe set process umask to `000`, started the real Unix listener,
and observed mode `0777`. The probe restored the umask and removed the temporary socket.

### Action

Make the parent directory part of the authorization boundary:

1. Default Unix sockets under an owner-private `0700` runtime directory (prefer
   `$XDG_RUNTIME_DIR/station-manager`, with a documented private fallback), not directly
   under `/tmp`.
2. After bind and before `Serve`, set the socket to `0600`, then stat and verify its
   type, owner and mode. Fail startup if the guarantee cannot be established.
3. Validate operator-supplied socket parents. Binding permissively in a shared parent
   and chmodding afterward leaves a small pre-serve connection race; a private parent
   closes it.
4. Update docs/defaults that still describe Unix as the default and permissions as an
   already-satisfied auth mechanism.

### Required tests

Run listener tests under umasks `000`, `002`, `022` and `077`; the effective socket must
always be owner-only. Cover insecure/existing parents, a non-owner/stale socket, failure
to chmod/stat, normal curl access by the owner, and denial to a separate local UID in an
integration test where CI permits it.

> ✅ **FIXED (committed on main).** `e66a33ab` + review-fixes `88c94ccf` / `70573e5d` /
> `5b86f93b` / `6f2abfb1`, operator rulings 2026-08-16. The Unix bind path is now a real
> authorization boundary: the default socket resolves under an owner-private runtime dir
> (`$XDG_RUNTIME_DIR` → state-home → `$HOME/.local/state`; never `/tmp`; unresolvable is a
> fatal config finding); before bind the socket's immediate parent must be euid-owned,
> non-symlink and `0700`, and the WHOLE ancestry up to `/` must be non-symlink, root/euid-
> owned and not group/other-writable-without-sticky (closing an ancestor-rename race);
> after bind the socket is chmod'd `0600` and verified, unlinking + refusing to serve on
> failure. Strict root-or-euid ownership (no overflow-uid exception — Option A); positive
> bind tests skip only in namespaced envs where `/` isn't root/euid-owned.

## ST-6 — local QSO-bearing artifacts are not consistently owner-private (P2)

Several code paths rely on directory history or deliberately create readable files:

- SQLite creates a missing database directory as `0700`, but accepts any existing
  directory without inspecting or tightening it at
  [`internal/database/sqlite/internal.go:160`](../../internal/database/sqlite/internal.go).
  SQLite then creates the database/WAL/SHM according to umask. The split bootstrap also
  creates its backup directory as `0755` at
  [`internal/database/sqlite/bootstrap.go:74`](../../internal/database/sqlite/bootstrap.go).
- Sent-ADIF archives contain full exported QSO records. Their directory is `0755` and
  each exclusive-created file is `0644` at
  [`internal/api/handler_session_email.go:364`](../../internal/api/handler_session_email.go).
- New log files are requested as `0600`, but `OpenFile(..., 0600)` does not change an
  existing file's mode at
  [`internal/logging/internal.go:35`](../../internal/logging/internal.go). A legacy
  readable log and rotated copies can therefore remain readable indefinitely.
- The top-level working directory is intentionally created as `0755` at
  [`internal/utils/working_dir.go:53`](../../internal/utils/working_dir.go), so private
  child modes are load-bearing rather than cosmetic.

A probe with an accessible pre-existing `0755` DB directory observed the new database
as `0644`; another observed a sent-ADIF archive as `0644`. Workspace metadata corroborates
the migration risk: `build/db/data.db`, its WAL/SHM and `build/log/smd.log` are currently
`0644`. Their contents were not opened during this audit.

The files contain operator identity, callsigns, timestamps, locations, notes, contact
history and station activity. They are not normally authentication secrets, so this is
P2 rather than P1. The boundary still matters on shared workstations, multi-user shack
systems, support bundles and portable installs.

### Action

Define one private-state filesystem policy:

1. Application-owned database, backup, export, evidence and log directories should be
   `0700`; files containing station/QSO data should be `0600` unless a specific export
   action explicitly requests sharing.
2. On startup, inspect existing default/application-owned paths. Tighten safe known
   files and directories, including SQLite sidecars and rotated logs, or fail/warn with
   an exact path when ownership makes tightening unsafe. Do not silently chmod an
   arbitrary operator-supplied shared directory without an explicit policy.
3. Change sent-ADIF archive creation to `0700`/`0600`. A later user-initiated “export to
   chosen path” may use the operator's requested sharing semantics; the daemon's
   automatic backup should stay private.
4. Make log initialization chmod the opened existing file before accepting it, and
   define rotated-backup modes.

### Required tests

Under permissive umask, cover fresh and pre-existing `0755`/`0777` directories, legacy
`0644` DB/log/archive files, SQLite WAL/SHM creation, bootstrap backups, log rotation,
and refusal when a path is not owned by the daemon user. Assert effective modes, not
only the mode argument passed to an open call.

> ✅ **FIXED (committed on main).** `52b6943a` + review-fixes `88e4fc8e` / `23563596` /
> `05e1e319`, operator rulings 2026-08-16. New `internal/fsperm` carries the one
> private-state policy: application-owned paths (symlink-aware containment within the
> working dir, euid-owned) are chmod'd to `0600`/`0700` and **verified** (fatal for the
> databases); operator-supplied paths outside the working dir are never mutated but warned
> when group/world-accessible; symlinks are never chmod'd through. Applied to the log +
> reference databases (+ `-wal`/`-shm` + dir), the bootstrap backup dir/db incl. existing
> backups, and the sent-ADIF archive — in **every** flow that opens them (daemon startup,
> `import`, `restore`). Log files (probe + pre-logger startup writer) chmod an existing
> file to `0600`. The sent-ADIF archive requires affirmative `Contained` before writing (an
> ancestor-symlink can't redirect QSO data out of the working dir); the residual write-time
> TOCTOU is documented as bounded by the euid-owned `0755` working dir. Working-dir root
> stays `0755` by design.

## ST-7 — the ClubLog build-key boundary is documented but not enforced (P2)

`internal/forwarding/clublog` declares a shared application API key populated by
linker flags at [`internal/forwarding/clublog/clublog.go:125`](../../internal/forwarding/clublog/clublog.go).
The accepted ADR correctly records the limitation: the key is extractable with
`strings`, is acceptable only for the current private-build dogfood model, and must be
revisited before public pre-built artifacts at
[`docs/decisions/0054-clublog-api-key-build-injection.md:108`](../decisions/0054-clublog-api-key-build-injection.md).

The problem is not that this limitation was missed; it is that nothing enforces the
boundary. The same linker injection appears in ordinary build, dev-RPM and release-RPM
paths. Built executables are normally `0755` and packages are ordinary readable
artifacts. Accidentally attaching/copying a release RPM, publishing a CI artifact, or
moving from private dogfood to public releases silently crosses the ADR trigger while
carrying the one shared confidential key.

No real artifact was scanned for the key during this audit. Source/build wiring and
file metadata are sufficient to establish extractability, and the ADR already states
it explicitly.

### Action

Enforce the accepted boundary in build/release automation:

1. Split clearly named private/dogfood and public artifact tasks. A public build must
   fail if a non-empty shared key would be injected; a private keyed build must require
   an explicit acknowledgement and produce a non-publishable artifact marker.
2. Keep keyed RPMs and binaries out of CI artifact upload, release directories and
   support bundles by policy plus automated checks. Tests should use a dummy sentinel,
   never print/scan the real value.
3. Before any third-party binary distribution, choose the ADR's triggered replacement:
   per-deployer/provider-issued runtime credentials, ClubLog approval to treat an
   embedded client identifier as public, or a server-side broker. A public client
   cannot keep one common static key confidential through obfuscation.
4. If a keyed artifact has already crossed the private boundary, rotate the ClubLog key
   after changing the distribution model.

### Required tests

Build private and public artifacts with a dummy sentinel. Assert it is present only in
the explicitly private artifact, absent from public binaries/packages, and never echoed
by build logs. Pin release-task failure when a public build environment contains the
key.

> ✅ **FIXED (committed on main).** `a81948b8`, operator rulings 2026-08-16. The build
> paths are split by construction: the PRIVATE path (`dev-rpm.sh`) requires
> `CLUBLOG_API_KEY`, stamps `buildinfo.BuildScope=private`, outputs to `build/private/` and
> marks the RPM `PRIVATE-BUILD-DO-NOT-DISTRIBUTE` (`nfpm.private.yaml`); the PUBLIC path
> (`release-rpm.sh`/`release.sh`) injects no key and **fails** if one is present after
> `.env` load; generic dev builds no longer consume the key. `scripts/test-build-boundary.sh`
> (in `task ci:local` **and** CI) proves the sentinel lands only in the private binary,
> the public binary is clean, and the public path refuses a present key without echoing
> it. Scope: guards, artifact separation, markers and tests only — the credential
> replacement / brokering remains ADR 0054's separate trigger.

---

**All ST-1..ST-7 findings are now FIXED** (ST-3 as ST-3a; **ST-3b — authenticated LAN
access — remains the one open topology decision**, tracked in ADR 0069). The P2s ST-5/6/7
close out this audit.

## Verified boundaries — no finding

- Unsafe browser requests validate a fixed configured destination before Origin; the
  current wildcard-bind fail-closed behavior prevents mutating DNS-rebinding attacks.
- Config responses expose only credential-presence metadata, and PUT merges blank
  password fields without returning stored secrets. URL/error logging has dedicated
  sanitization tests.
- CAT generic commands are explicit `Exposed` operations, templates are validated,
  mapped/numeric values are checked, TX/playback operations are non-exposable, rig
  identity/liveness must be confirmed, and command writes are excluded while TX is
  active or uncertain.
- Tune and FT8 TX are controller-owned, bounded, release-on-failure paths rather than
  generic CAT operations. No command-injection route was found in the shipped rigdefs.
- SM Cloud private routes are bearer-wrapped; token comparisons are constant-time for
  equal-length candidates; per-logbook handlers perform ownership checks; QSO/evidence
  queries and uniqueness are tenant-scoped; cross-tenant existence is returned as 404.
- Cloud access logging does not use forwarded addresses for authorization and ignores
  remote-client X-Forwarded-For unless the immediate peer is loopback.
- SQL values are parameterized. The few dynamic SQLite identifiers come from closed
  internal table/migration sets rather than request/config strings.
- Session archive filenames are bare-name validated, lexically contained and
  exclusive-created, so the permission issue in ST-6 is not also path traversal or
  overwrite.
- SMTP enables STARTTLS by default, verifies the configured server name, and fails
  closed when STARTTLS was requested but unavailable.

## Verification performed

### Overlay probes (removed)

Seven temporary test functions asserted and logged current behavior:

1. rebound Host/Origin `GET /v1/config` returned 200;
2. `/app/` returned no frame-denial header;
3. sent ADIF was `0644` under an accessible directory;
4. the Unix listener was `0777` under umask `000`;
5. SQLite created `0644` in a pre-existing `0755` DB directory;
6. QRZ, ClubLog and SM Cloud accepted credentialed remote HTTP; and
7. a 307 replayed a dummy QRZ key from HTTPS to another HTTP origin.

All probes passed because they pinned the current unsafe behavior. Probe files and the
temporary API test compatibility adjustment were removed afterward.

### Existing suites

The following existing packages passed with `-count=1`:

```text
./internal/config
./internal/cat
./internal/bridge
./internal/forwarding/...
./internal/cloud/...
./internal/email
./internal/logging
./internal/database/sqlite
```

The complete `internal/api` package currently has an unrelated pre-existing compile
failure: `handler_evidence_test.go` compares pointer-valued `Status.UsageBytes` with an
integer after the concurrent evidence-status change. The overlay API probes were run
with a temporary compile-only pointer dereference, then that existing test was restored
exactly. This did not affect any security observation.

## Recommended action order

1. Close **ST-1** and **ST-2** first: both affect the default loopback browser
   deployment and are small, centrally testable middleware changes.
2. Decide **ST-3** before advertising or relying on LAN access. The lowest-risk interim
   is to reject non-loopback direct binds and put authenticated TLS at a proxy boundary.
3. Close **ST-4** across all credentialed clients in one shared transport policy;
   preserve SM Cloud LAN staging only through explicit acknowledgement.
4. Close **ST-5** and **ST-6** together as a private-runtime-directory/state-mode pass,
   including safe migration of existing files.
5. Put the **ST-7** public/private artifact gate in place before the next release-
   distribution workflow; rotate only if a keyed artifact has left the private scope.

