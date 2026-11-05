---
number: 0099
title: Keep three SPA windows usable together within the browser's connection budget
status: Proposed
date: 2026-11-04
---

# 0099 — Keep three SPA windows usable together within the browser's connection budget

**Proposed (2026-11-04). Design only. The mechanism is not accepted and nothing is built.** This ADR
compares the candidates against the reviewer's outcomes and records what each must still establish.
The smaller candidate, one combined shell-and-rig stream per window, is evaluated first; that order
is review direction, not a decision. The operator ruled D1 and D2 on 2026-11-04 (see Operator
rulings); mechanism approval and implementation remain pending.

**Feasibility probe (2026-11-04):** a held lookup made otherwise-fast requests take about 5.2 s
with five streams open, against D2a's 1 s (see "Feasibility probe"). The B/C comparison now takes
priority over refining A.

**B transport probe (2026-11-05):** through a TLS proxy, HTTP/2 kept every timed ordinary request
at 2–4 ms with one, three and four distinct lookups held; the same proxy on HTTP/1.1 starved them
(about 4.4 s). This is transport evidence only (see "B transport probe").

**WebSocket pool probe (2026-11-05):** in Firefox 159, open WebSockets did not consume the six
HTTP slots. With 9 open and 4 HTTP requests held, ordinary requests took 1–2 ms; SSE in their place
starved them. B is paused, and WebSocket event delivery is proposed as the leading candidate (see
"WebSocket event transport").

**WebSocket design comparison (2026-11-05):** recommends W1, separate shell and rig WebSockets
with FT kept on SSE: a transport-only change. **W1, the dependency, Origin policy and timeouts were
ruled by the operator on 2026-11-05**; the implementation contract stays pending review of the
grace-enforcement correction (see "WebSocket event delivery").

**Mechanism approval is blocked (reviewer, 2026-11-04) on:**
1. evidence for D1-ii under a representative request workload (see "Request workload in D1-ii");
2. the Q3 startup design;
3. Q4's bootstrap ownership.

Candidate A stays Proposed until these are resolved. If A fails, the agreed fallback is B or C.
Because of A's D1-ii workload risk, B's and C's costs are worked up alongside A's feasibility
evidence (reviewer, 2026-11-04). Implementation remains pending.

**Severity (reviewer's ruling):** P1 availability, blocking W-0024 acceptance (AC-6). An ordinary
three-window workflow becomes unusable while the daemon stays healthy. No data corruption or
RF-safety failure has been demonstrated.

The defect is tracked in W-0012. The evidence is in the W-0024 acceptance record, entries 30 and 32.

## Context

**The incident.** On 2026-11-04 the station was on `b766a145`, with CAT enabled and the rig off.
Three windows were open:
- A, Settings
- B, Operate Phone/CW
- C, Logbook

A's Settings request timed out after 15 s, and C showed "Archive binding unproven". Direct daemon
reads stayed healthy. Closing C and retrying restored Settings at once, in about 1 ms. Which window
held each socket was not measured.

**The streams.** Each SPA window holds these long-lived streams:

| Stream | Opened by | Purpose |
|---|---|---|
| `/v1/events` | `main.ts:719`, at module load, never closed (ADR 0079) | reconnection signal (count re-fetch, archive generation); QSO events for the map, shared within a window since 2026-09-14 |
| `/v1/rig/events` | `main.ts:418`, inside `applyStationContext`, once the asynchronous config shows `catEnabled`, unless the tab is a settled Map tab | CAT state, meters, tune state, TX alarm, `rig-clients`; build identity and archive re-verification ride its reopen (W-0004 AC3) |
| `/v1/ft8/events` | the FT8/FT4 view only | decodes and occupancy; **the open subscription is the operator-presence signal** (see below) |

**FT8 presence.** The FT8 subscriber count drives the capture device and a TX disarm after the last
FT subscription has stayed absent through the linger. That disarm does not prove human attendance.
When the **last** subscriber leaves, `onSubscriberRemoved` starts the 5 s `captureLinger`
(`internal/ft8/service.go:463`). When the linger expires, `onLingerExpired` re-checks the aggregate
`subCount`. If any subscriber has returned, it keeps the session. Only with none does it disarm TX
and release the device. So an ordinary reconnect inside the linger does not disarm a QSO.

**Why three windows starve.** Three CAT-enabled windows hold six streams. The rig stream opens
whether or not the rig is powered.
- The daemon serves a plain listener through `http.Server.Serve` (`internal/api/server.go:595`),
  with no TLS and no unencrypted HTTP/2, so browsers speak HTTP/1.1
  (https://pkg.go.dev/net/http#Server.Serve).
- Firefox's six persistent connections per host are shared across windows.
- SSE holds connections that ordinary requests also need. Mozilla's issue describes this pattern
  (https://bugzilla.mozilla.org/show_bug.cgi?id=906896), and MDN's SSE page notes the per-browser
  limit over HTTP/1.1
  (https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events).

**The second occurrence.** On 2026-09-11 and 2026-09-13, two tabs (Operate on FT8, plus Map) held six
streams and the map's request starved (alpha.2 acceptance record, entries 36–37).
- The fix then (ADR 0079 dated update, 2026-09-14) shared `/v1/events` within a window and dropped
  the rig stream from a Map tab.
- The operator declined to change FT behaviour to make room for the map: "leave the map as a
  separate tab and manage the internal browser connections. I don't want to disturb the FT side of
  things" (W-0012, contacts map item, ruled 2026-09-14).
- W-0013 lists "one multiplexed SPA event stream" as the trigger-bound next step. That trigger has
  now fired.

**The daemon's own cap.** The daemon caps event subscribers at a shared 16 (`max_event_subscribers`),
counted per SSE request across all its event endpoints. That cap is not what is being hit here.

## Required outcomes (reviewer, 2026-11-04)

- **O1 Simultaneous use.** Settings, Operate Phone/CW and Logbook stay usable at the same time,
  including their ordinary API requests: config, archive verification, counts and saves.
- **O2 Restart recovery.** All three windows recover by themselves across a daemon restart. The
  default-switch case still proves W-0024 AC-6: the drafts it warned about are discarded, and every
  window lands on the correct destination.
- **O3 No dependence on hidden or idle windows.** Recovery holds across reconnects, visibility
  changes and window closure, without relying on hidden windows releasing connections.
- **O4 FT semantics intact.** FT8 subscription and presence, capture-device ownership and
  guaranteed-stop semantics are unchanged. This keeps the 2026-09-14 ruling (W-0012, contacts map
  item): FT operating behaviour is not disturbed to make room for other views, and connections are
  managed instead.
- **O5 Real-browser proof.** A headed Firefox demonstration shows the starvation is gone (see
  below). Mocked `EventSource` tests alone cannot establish it.
- *Nearest confusable outcome:* a change that frees connections only when a window is idle or
  hidden, or that "passes" because one window was closed. The simultaneous default-switch reload
  then still stalls.

## Decision (proposed for review)

Evaluate first **one combined shell stream per window**, carrying the shell and rig streams on a
single connection. As a scoped decision, `/v1/ft8/events` keeps its current transport. The operator
has ruled the **supported configurations** (D1) and **usability thresholds** (D2). A must pass
both configurations. If it fails D2 in either, measured, the decision moves to B (HTTP/2) or C (one
connection shared across windows).

## Connection budget

Under HTTP/1.1 the browser's per-host pool is 6. Let:
- **W** be the open windows that are not settled Map tabs
- **M** be the settled Map tabs
- **F** be the windows showing FT8/FT4

| | Persistent connections C | Request capacity R = 6 − C |
|---|---|---|
| Today (CAT enabled) | 2W + M + F | |
| Candidate A | W + M + F | |

| Configuration (windows, of which FT) | Today C / R | A: C / R |
|---|---|---|
| 3, none on FT | 6 / **0** | 3 / 3 |
| 3, one on FT | 7 / **starved** | 4 / 2 |
| 4, one on FT | 9 / **starved** | 5 / 1 |
| 5, none on FT | 10 / **starved** | 5 / 1 |
| 3, all on FT | 9 / **starved** | 6 / **0** |

A configuration with R = 0 starves by construction. R ≥ 1 is necessary, not sufficient: with one
free connection, every ordinary request in every window queues on it. Whether that latency is
acceptable is a D2 measurement, never inferred from the arithmetic. Under B the six-socket pool stops
binding SSE. Under C the shell costs 1 connection in total, plus F.

## Candidates

### A. One combined shell stream per window (smaller; evaluated first)

*Shape.* A new endpoint, provisionally `GET /v1/shell/events?rig=1`, in `internal/api`, which already
imports `internal/bridge`. There are no new import edges, and storage and the forwarder are untouched.
`/v1/events` and `/v1/rig/events` stay served, with unchanged wire contracts, for other clients and
tests. The SPA moves to the new endpoint.

*What it must establish:*

- **Connection budget.** The formula and table above. A meets O1 only for configurations with R ≥ 1
  that also pass D2.
- **Request workload in D1-ii (code-supported risk, not a measured failure).**
  - *The mechanism:*
    - FT enrichment allows two concurrent `/v1/enrich/callsign` lookups per window
      (`FT8_ENRICH_CONCURRENCY = 2`, `frontend/app/src/lib/operate/ft8Enrich.svelte.ts:63`).
    - The handler replies only after `Enrich` returns (`internal/api/handler_enrich.go:77`).
    - A lookup can take a long time. HamNut's default HTTP timeout is 10 s
      (`internal/lookup/hamnut/service.go:40`). The alpha.2 acceptance record, entry 36, measured
      1.5–2.5 s per enrichment request, daemon-side (not per individual upstream lookup).
      Citation: `docs/reports/dogfood-acceptance-v2.0.0-alpha.2.md:1281`.
  - *The effect:* under A, D1-ii holds five persistent connections, so one slow lookup can occupy
    the only free connection. An otherwise fast Settings request or archive check then waits beyond
    D2a.
  - *What would miss it:* an FT window with no decodes makes no lookups, so it would not show this.
  - *Required before A is selected:* the offline browser case in demonstration step 2b. If A fails
    it, the decision moves to B or C.
- **Event routing.** The two vocabularies are disjoint:
  - `qso.*` and `forward.*` (`internal/events`)
  - `rig-state`, `rig-meters`, `rig-disconnected`, `bridge-error`, `tune-state`, `tx-alarm`,
    `rig-clients` and `drive-alarm` (`internal/bridge/events.go`)

  Clients dispatch by `event:` name as now. Order is kept within each source. Order across the two
  sources was never guaranteed, because they were separate connections, and none is promised.
- **Server bootstrap.** Both hubs are subscribed **before** the 200 is observable, keeping the
  `/v1/events` contract (open first, then fetch). `: connected` comes first, then the rig bootstrap
  poll (`bridge.TriggerBootstrap`, ADR 0019), as today. That is one poll per window with the rig part,
  so the count is unchanged.
- **Startup sequence (Q3; reviewer prefers option 2).** Today the shell's stream opens at module
  load. CAT and setup state arrive later, from the asynchronous config read inside
  `bootArchiveScoped`. The rig stream opens in `applyStationContext` only after that read, guarded
  by `rigEventsOpen`, because the function runs again after first-run setup.
  - *Option 1 (not preferred).* Open shell-only at load, then replace the stream with `rig=1` once
    the context shows CAT. This replaces a connection on every normal CAT-enabled boot.
  - *Option 2 (reviewer's preference, 2026-11-04).* Open **once**, after the initial configuration
    attempt settles:
    1. install the handlers;
    2. choose shell-only or shell-plus-rig;
    3. open the combined stream before route consumers start.

    A failed configuration read still produces a shell-only stream, and the existing boot identity
    gate and recovery watch (`bootArchiveScoped`, `verifyArchiveGeneration`) are preserved.
  - *Distinctions to preserve:*
    - CAT disabled, and a settled Map tab, use shell-only.
    - A Map-route window awaiting first-run setup may need rig events. That exception is
      characterized by `frontend/app/src/main.mapsetupboot.test.ts`.
    - Re-applying an unchanged context must not reopen the stream.
    - First-run completion must keep any necessary late rig subscription.
    - Restart-bound Settings changes keep their existing restart and reload semantics.
  - *Any later replacement must define its recovery.* This covers first-run completion adding the
    rig part, and any other case. The streams keep no backlog, so an intentional replacement can
    still leave an event gap: genuine outage state is kept, and the affected baselines are refreshed.
- **Daemon integration (Q4).** The handler composition is accepted in principle. **Bootstrap
  ownership is unresolved.** The design must assign it. The target shape:
  - one writer serving both typed subscriptions (`events.Hub` and the bridge hub);
  - one `limitEventSubscribers` admission per combined connection (its H3 log counts SSE requests);
  - **both** subscriptions established before the response opens. Today the rig handler subscribes
    after `WriteHeader` and the shell handler before; the combined handler subscribes both first;
  - `: connected` written and flushed immediately;
  - the rig bootstrap poll (`bridge.TriggerBootstrap`, ADR 0019) run after that. Naming an owner is
    not enough: the bootstrap **must not block shell event forwarding**, and the design must define
    its outstanding work, its cancellation and how it completes at shutdown. Today the call is
    synchronous in the rig handler, and on CI-V it waits on `cmdMu` with a plain `Lock()`
    (`internal/bridge/pipeline.go:1413`), which request cancellation cannot interrupt. Inside a
    combined handler, that wait would stall QSO events for the window. This is why ownership
    remains unresolved;
  - both subscriptions released on request cancellation, on `shutdownCh`, or when either hub ends
    its channel;
  - the new path added to `isSSEPath` (`internal/api/middleware.go:47`, which enumerates paths), so
    the stream never holds an ordinary `limitConcurrent` slot;
  - one per-write deadline (`armWrite`) across frames from both sources.
- **Reconnect.** If **either** hub evicts a slow reader or closes, the whole stream ends. The client
  reconnects and re-syncs both sides. *Consequence:* a rig-meter eviction now also re-fetches the
  count and the archive state. `openReviving` is unchanged: it revives only a dead stream, and only
  in a visible window.
- **Q2, one verification per combined reconnect.** *Reviewer recommendation:* one archive/default
  verification per combined reconnect, owned by the shell, replacing today's two:
  - `verifyArchiveGeneration`, on the shell's reconnect
  - `verifyAfterRigReconnect`, on the rig's reopen

  It must preserve:
  - the boot bracket (`bootArchiveScoped`)
  - fail-closed behaviour
  - protection against overlapping checks

  A later reconnect must not be absorbed by an older verification still in flight. Characterize
  those boundaries with tests before consolidating.
- **Subsystem boundaries.** The bridge keeps its hub, its late-subscriber replay (the TX alarm's
  cached slot) and its frame encoding. The api handler composes the two sources; it does not
  reinterpret them. `rig-clients` keeps meaning "windows subscribed to the rig", because each window
  still subscribes the bridge hub once. The SPA's transports stay transport-only (ADR 0045):
  `log-events.ts`'s per-window ref-counted multiplexer grows into the shell transport, and
  `rig-sse.ts` keeps its parsing and handler surface.
- **FT8, scoped decision.** FT8's transport stays unchanged (O4 and the 2026-09-14 ruling). That is a
  choice of scope, not a claim that FT could never share a connection. Folding it in would change
  presence accounting: a combined stream's revive or eviction would remove and re-add the FT
  subscriber, which starts the linger when it was the last one. It would also make presence depend
  on streams that windows without an FT view hold.

### B. HTTP/2

*Shape.* Serve TLS so that Firefox negotiates HTTP/2 (ALPN `h2`), where streams multiplex on one
connection.
- Go's server supports HTTP/2 transparently over TLS (https://pkg.go.dev/net/http, package
  overview).
- Go 1.24 and later also offer unencrypted HTTP/2 through `Server.Protocols`
  (https://github.com/golang/go/issues/67816).
- Mozilla explicitly declined cleartext h2c for Firefox
  (https://bugzilla.mozilla.org/show_bug.cgi?id=1418832#c2), so h2c does not help the browser.

*What it must establish:*

- **Actual Firefox negotiation** on the station, measured from the Network panel's protocol column
  and socket evidence, not assumed.
- **Installation, certificate and trust.** This does not necessarily mean manually filling a
  Firefox-only store. Mozilla documents trusting OS certificate authorities, and Linux p11-kit
  integration (https://support.mozilla.org/en-US/kb/setting-certificate-authorities-firefox). The
  design still has to cover:
  - the RPM and first run
  - certificate issuance, rotation and expiry
  - the clean-install acceptance
- **Localhost and LAN access.** The certificate must name every address operators use (127.0.0.1,
  the hostname, the LAN IP). The origin changes from `http://` to `https://`, so browser storage
  keyed to the old origin is left behind.
- **Fallback.** Two choices, each with a cost:
  - keep plain HTTP: two origins, and the old one still starves
  - remove it: other clients and scripts change

  It must also say what happens when TLS setup fails.
- **Remaining limits.** HTTP/2 removes this six-socket bottleneck, not every capacity limit:
  - the daemon's 16-subscriber cap still applies
  - the server's advertised concurrent-stream limit applies per connection
- *Assessment:* B removes the budget problem outright, but as an installation and trust change it
  needs its own ruling.

### C. One connection shared across windows

*Shape.* A `SharedWorker`, or a leader window chosen through Web Locks fanning out over
`BroadcastChannel`, owns one shell stream for all windows of the origin.

*What it must establish:*

- **Ownership.** Who opens and closes the stream, and how a window owner, as opposed to a worker,
  hands over.
- **Window closure and crash.** Takeover without a gap that loses events. The streams keep no
  backlog, so after a takeover every window re-syncs its baseline.
- **Late subscribers.** A window opening later gets the current state: the rig bootstrap, the cached
  TX alarm and the reconnection baseline.
- **Independence from the first window.** No behaviour depends on which window opened first. The
  per-window archive guard (`X-SM-Archive-Id`, `frontend/app/src/lib/api/archive-guard.ts`) and
  reload decisions stay per window.
- **Known costs:**
  - `rig-clients` counting semantics would change: it would read 1 however many windows are open.
    The current SPA does not display that count (`frontend/app/src/lib/api/rig-sse.ts:13`), so no
    visible warning is lost today; a future consumer would need it re-derived.
  - FT8 keeps its per-window transport by the same scoped decision as A, so each FT window still
    adds a connection.
  - Worker test infrastructure is new to the project.
- *Assessment:* C bounds the shell to one connection however many windows are open, at the price of
  a cross-window coordination layer the project does not have today.

### Rejected without further study

- **Closing streams in hidden or idle windows.** Fails O3 by construction; it is the nearest
  confusable outcome.
- **Polling instead of streams.** Rejected in ADR 0079: it misses the restart moment and fetches
  when nothing changed.
- **Raising Firefox's per-host limit** (`about:config`). A per-profile operator workaround, not a
  product fix.
- **A WebSocket transport.** Not studied. Whether Firefox counts WebSockets against the same per-host
  pool is unverified, and the change would replace SSE's built-in reconnect and the reviving wrapper.
  Reopen only with a cited answer to the pool question.

## Real-browser demonstration (O5; applies to whichever candidate is chosen)

**Setup.** An isolated guest (no hardware, no live data) on the new candidate build, with headed
Firefox driving the normal UI.

**Evidence, three kinds side by side:**
1. **The daemon's H3 subscriber log** (`internal/api/middleware.go:239`). It counts active **SSE
   requests** globally, not TCP connections. Under HTTP/2, several of them share one socket.
2. **Browser evidence.** The Network panel's protocol column and per-request timing for the ordinary
   requests in every window.
3. **Socket evidence** on the guest: established TCP connections to the daemon's port, counted at
   each step.

**Steps.** Every limit is a maximum per exercised request or per window, never an average.

1. **D1-i, three windows.** A Settings, B Operate Phone/CW with a partial entry, C Logbook, with CAT
   enabled. The SSE requests, sockets and request capacity match the candidate's budget, and ordinary
   requests meet D2a. Before the run, establish whether the guest's bridge serves the rig stream with
   no rig attached.
2. **D1-ii, four windows, one of them on FT8/FT4.** The FT window's stream opens and its subscriber
   appears, and every window's ordinary requests still meet D2a.
   2b. **D1-ii under a controlled pending lookup (offline).** Enrichment points at a guest-local
   responder that holds lookup responses open on command. With lookups held in flight, as the FT
   window's two-lookup cap allows, exercise Settings loads and archive verification from the
   other windows; each must meet D2a. **Barrier:** before the timed requests, confirm that a
   cache miss reached the local responder and is still pending, from the responder's own log and
   the browser's pending request. Otherwise a cached enrichment answers at once and the pass is
   misleading. Two things must be established before the run:
   - an offline decode source that makes Band Activity issue lookups. Otherwise a Phone/CW
     callsign lookup stands in. That substitute can establish the connection-contention case, but
     not the FT window's two-lookup workload, and the record labels it as a substitute;
   - that the lookup provider's URL policy allows a loopback responder.
3. **Default switch from A, in each of D1-i and D1-ii.** Every window reloads by itself onto the
   destination, and B's partial entry is gone as warned (AC-6 shape). Then a plain daemon restart.
   Both meet D2b, measured per window from the new instance being observed ready (the `/v1/version`
   instance, read outside the browser) until the window is usable on the correct binding.
4. **Disturbances.** Hide and restore windows; close one and reopen it. Make a **real network
   interruption** (the guest's interface taken down and up), kept distinct from **dispatching a
   synthetic `online` event**: the first exercises the browser's teardown, the second only the
   reviving wrapper's trigger. The remaining windows stay usable, and none depends on another
   releasing a connection.
5. **Negative control.** The same script on `b766a145` reproduces the stall. This is the
   real-browser reversion proof.

**FT capture and guaranteed stop are proven separately.** CAT enabled with no live rig gates capture
off (the reviewer's reading), so the browser run alone cannot establish capture behaviour.
- The browser run asserts subscriptions only: the FT stream opens and closes, and subscriber counts
  move as expected.
- Capture, linger and guaranteed-stop semantics stay proven by the FT8 package's tests, extended
  where the change touches them, or by a named offline fixture that supplies audio and a simulated
  rig.

Unit and integration tests still cover routing, eviction, bootstrap order, reconnect transitions and
Q2's verification boundaries. The demonstration is the acceptance evidence for O1–O3 and O5.

## Feasibility probe (2026-11-04): a held lookup starves ordinary requests

**This is a transport-equivalent contention probe. It is not acceptance of candidate A, and not a
D1-ii result.** It reproduced A's D1-ii connection count, five persistent streams on one origin, on
the existing build, to test the workload risk directly.

*Setup.* Entirely offline, with no hardware and no production change:
- an isolated guest (`sm-w0099-probe`, Fedora 44, the image verified as before) running the R0
  artifact `b766a145-dirty`;
- synthetic setup (`7Q5MLV`);
- the bridge enabled against an absent serial port (`/dev/ttyPROBE-ABSENT`). The serial open
  failed and no pipeline started, yet `/v1/rig/events` was served (200), so the SPA treated CAT as
  enabled;
- HamNut pointed at a guest-local responder that logs each request on arrival and holds it until
  released.

*Browser.* Headless Firefox 159 with a fresh, unmodified profile, driven over WebDriver BiDi through
the tunnel `127.0.0.1:18080`. Three windows:
- w1 `/operate/phone`: 2 streams
- w2 `/config` (Settings): 2 streams
- w3 `/map`, a settled Map tab: 1 stream

The ordinary requests timed were transport-equivalent page `fetch` calls on the same origin:
`/v1/config`, `/v1/version` (the identity read behind archive verification), `/v1/qso-archives` and
`/v1/default-logbook`. The held lookup was the request a Phone/CW callsign lookup makes
(`/v1/enrich/callsign`): a substitute for connection contention, not the FT window's two-lookup
workload.

*Evidence.* `/home/mveary/sm-w0024/probe-evidence/` (0700, `MANIFEST.sha256`):
- the driver's log
- the responder journal
- the guest `smd.log` extract
- the driver and responder sources
- attempt 1, which stopped on a bug in my driver before any lookup

| Step | Result |
|---|---|
| 1. Streams | daemon SSE subscribers **5** (H3 log); Firefox sockets to the tunnel **6** |
| 1. Baseline, nothing held | all four requests **1–2 ms** (delay before request start ≤ 1 ms) |
| 2. Barrier | cache miss `9X0PRB` **reached the responder** at 13:07:53.146Z (`ARRIVED /v1/call-signs/prefixes?prefix=9X0PRB`); browser lookup **pending**; still 5 subscribers and 6 sockets |
| 3. Held, from w2 | after 5 s all four still pending; on completion **5,158–5,159 ms each, of which 5,157–5,158 ms elapsed before the request started** (`requestStart − startTime`; the driver's raw field is named `queued_ms`) |
| 4. Release (13:07:58.520Z) | lookup completed (200, 5,519 ms; daemon 5,516 ms); the four held requests reached the daemon at 13:07:58.662–.664Z with **0 ms** processing, then completed; afterwards **2–3 ms** each |

*Reading.*
- **The held lookup made otherwise-fast requests exceed D2a's 1 s by about five times.** Almost all
  of the delay came before each request started. `requestStart − startTime` is not by itself a
  measure of connection queueing: in Resource Timing it spans everything before the request is sent
  (https://www.w3.org/TR/resource-timing/#dom-performanceresourcetiming-requeststart). Connection
  contention is the reading of the combined evidence:
  - five streams plus six sockets;
  - the pending lookup confirmed at the responder;
  - the daemon receiving the four requests only in the millisecond the lookup completed, with 0 ms
    processing;
  - immediate recovery on release.
- They recovered at once on release.
- The sixth socket was the one connection left for ordinary requests. While the lookup held it,
  nothing else could pass.
- The hold length was artificial. Real enrichment requests measured 1.5–2.5 s each, daemon-side
  (alpha.2 record, entry 36), and HamNut's default timeout is 10 s. Whether a given overlapping
  request breaches D2a depends on how long the lookup keeps occupying the connection after that
  request is issued, not on the lookup's total duration alone.

*Consequence (the reviewer's rule).* The B/C cost comparison takes priority over refining A's Q3 and
Q4. A in its proposed form keeps one free connection in D1-ii. An ordinary request breaches D2a when
the lookup continues occupying the sole available connection for more than 1 s after that request
is issued. Any variant of A would have to remove that contention, not only reduce the stream
count.

## B/C comparison (2026-11-04; design only)

Authorised by the reviewer after the feasibility probe. Mechanism approval and implementation remain
pending. Both candidates are assessed against:
- the concurrent FT lookups
- D2a and D2b
- AC-6
- FT subscription lifetime, which stays per window, unchanged, in both

Revised after the reviewer's corrections of 2026-11-05 (see "Review of the comparison").

### Request capacity under load (both)

The probe showed that the binding quantity is **connections free for ordinary requests while
lookups are in flight**, not persistent streams alone. In D1-ii, at least four lookups can be in
flight at once:
- **FT decode-row enrichment:** up to `FT8_ENRICH_CONCURRENCY = 2`
  (`frontend/app/src/lib/operate/ft8Enrich.svelte.ts:63`).
- **FT worked-station card:** 1. `Ft8Operate.svelte:375` mounts an `EnrichmentCard`, which drives the
  separate callsign enricher (`enrich.svelte.ts`). That enricher keeps one request in flight per
  tab, aborting a superseded one, and is outside the FT cap.
- **Phone/CW logging card:** 1, from its `EnrichmentCard` (`LoggingCard.svelte:610`).

All four are `GET /v1/enrich/callsign` requests. No `singleflight` coalescing was found in
`internal/`, so a card lookup and a decode-row lookup of the same call are separate requests. That
comes from a repository search, not a measurement.

This is a **sufficient contention example, not an established maximum**: other windows' lookups
were not surveyed.

| | Persistent | Free | Free with 3 lookups held more than 1 s | Free with 4 held |
|---|---|---|---|---|
| Today | 9 (4 windows × 2, plus FT) | starved | starved | starved |
| A | 5 | 1 | starved | starved |
| **B** (HTTP/2) | streams on one connection | not bound by the six-connection limit | see below | see below |
| **C** (shared shell and rig, FT per window) | 3 | 3 | **0** | **0** |

**C** fails D2a **if** three lookups keep their connections occupied for more than 1 s after an
ordinary request is issued. To pass, it needs an additional rule that always leaves an
ordinary-request connection free, for example a lookup budget shared across windows. Any such rule
changes enrichment behaviour, so it needs its own ruling.

**B** removes this six-connection exhaustion mechanism. Stream count is not its only constraint:
- **Flow control and scheduling** still govern how streams progress (RFC 9113 §5.2,
  https://www.rfc-editor.org/rfc/rfc9113.html#section-5.2).
- **The advertised stream limit.** The server advertises `SETTINGS_MAX_CONCURRENT_STREAMS`. If
  the daemon terminated TLS itself, Go's default of 250 would apply (`http2defaultMaxStreams`,
  `/usr/local/go/src/net/http/h2_bundle.go:4042`). **In the proxy probe, the proxy's advertised
  limit applies instead.**
- **The daemon's own limits** (`internal/config/config.go:556`, `:561`):
  - `max_concurrent_requests`, default 128. SSE paths are exempt (`isSSEPath`).
  - `max_event_subscribers`, default 16. One count is shared by all three SSE paths
    (`internal/api/server.go:362`, `:402`, `:434`); D1-ii today uses 9.

Whether B meets D2a with four held lookups is an inference from the protocol. The probe measures it.

### B. HTTP/2 over TLS

- **Certificate provisioning.** Two shapes:
  - **Per-install self-signed certificate:** SANs `127.0.0.1`, `localhost` and the host name,
    generated at first run into the working directory, with the key at 0600.
  - **Per-install local CA** that issues the server certificate. Renewal is then automatic, but a
    trusted CA key can impersonate any site for this host's users unless it is name-constrained.

  Which shape is the narrower grant depends on the trust mechanism that installs it (below). A
  self-signed certificate installed with CA trust is itself a trusted CA.
- **Trust on the station, measured on this host.**
  - **Measured wiring:** the Fedora-packaged Firefox (`firefox-159.0-1.fc44`) resolves NSS's
    built-in roots module `libnssckbi.so` through alternatives to
    `/usr/lib64/pkcs11/p11-kit-trust.so` (`p11-kit-trust-0.26.5-1.fc44`).
  - **Inferred, not verified:** it reads Fedora's system trust store, so an anchor in
    `/etc/pki/ca-trust/source/anchors` plus `update-ca-trust` should be trusted.
  - **Not established:** how narrowly that anchor is trusted, as a CA or only for this server.
  - **Root:** anchoring needs root, through an RPM scriptlet or an install step.
- **Trust elsewhere.** Source: Mozilla's policy reference,
  https://firefox-admin-docs.mozilla.org/reference/policies/certificates/, last updated
  2026-11-04.
  - **`ImportEnterpriseRoots`** "only works on Windows and macOS. For Linux discussion, see bug
    1600509."
  - **`Certificates.Install` locations.** It installs into the Firefox certificate store. Bare
    filenames are looked up on Linux in `/usr/lib/mozilla/certificates`,
    `/usr/lib64/mozilla/certificates` and `~/.mozilla/certificates`, with the user directory
    searched first.
  - **Trust flags.** "Certificates are installed using the trust string `CT,CT,`." In NSS's
    certutil reference
    (https://nss-crypto.org/reference/security/nss/legacy/tools/nss_tools_certutil/index.html):
    - `C` is "Trusted CA to issue server certificates" and `T` is "Trusted CA to issue client
      certificates". Both imply `c`, "Valid CA".
    - This route therefore trusts the certificate **as a CA** for SSL and email. It does not
      implement narrowly scoped leaf trust.
    - The reference lists `P`, "Trusted peer", without defining its effect.
  - **Peer-only trust is pending.** Whether any peer-only trust route exists for this purpose
    awaits verification of the actual mechanism.
  - **Persistence.** "Certificates added with `Install` stay in each user's profile even after you
    remove them from the policy." On this route, uninstall cannot withdraw trust.
  - **Not covered by the page:** the Linux `policies.json` location and p11-kit. Both are to verify.
- **Renewal.** Certificate lifetime, the expiry warning surface, and who re-anchors. Clean install,
  upgrade and uninstall each touch a trust store. Removing a system anchor is possible, but a
  `Certificates.Install` copy persists in profiles (above).
- **Localhost and LAN.** Loopback is the supported posture. A non-loopback TCP bind is refused unless
  `server.allow_insecure_network` is set (`internal/config/config.go`, ST-3b), and TLS adds no
  authentication to that posture. B serves loopback names only, and LAN stays out of scope.
- **Browser-origin data.** `http://127.0.0.1:8080` and `https://…` are different origins. Left behind
  on the old origin:
  - `localStorage`: `sm-theme`, `sm-nav`, `sm-util`, `sm-op-mode`, `sm.ft8.cqModifier`
  - per-tab `sessionStorage`: `sm.session.qsos.*` (the Session list of ADR 0094) and
    `sm.ft8.engaged.*`

  An ordinary upgrade reload **keeps** the Session list. `session.svelte.ts` deliberately persists it
  to `sessionStorage` so a redeploy or F5 does not lose the sitting's contacts
  (`frontend/app/src/lib/operate/session.svelte.ts:5`). An origin change would lose it. That loss is
  new, not existing restart behaviour, and needs its own migration or reset decision.

  The options include an announced one-time reset, or a one-shot hand-over from the old origin that
  carries the five preferences and the tab's Session list. Whether per-tab `sessionStorage` content
  can be handed over safely is still to design.
- **Verified negotiation.** Measured, not assumed:
  - the Network panel's Protocol column;
  - `performance.getEntriesByType('navigation')[0].nextHopProtocol === 'h2'`, and the same check
    for the tested resources;
  - a single socket to the TLS port while the D1-ii windows are open.
- **Redirects, separately from HTTP/1.1 fallback.**
  - **Redirect.** The plain-http listener stays for non-browser clients (`smctl`,
    `scripts/ukeicc-cabrillo.py --from-daemon`). For browsers it can redirect to HTTPS, so an old
    bookmark redirected to HTTPS runs over h2 and does not keep the starvation.
  - **Fallback.** Serving the SPA itself over HTTP/1.1 does keep it. That covers a plain-http SPA
    that is not redirected, ALPN settling on `http/1.1` behind an intermediary, and
    `network.http.http2.enabled` set to false.
  - **Warning.** The SPA can detect a non-`h2` `nextHopProtocol` and say so. That makes the
    limitation visible but **does not satisfy D1/D2**: on HTTP/1.1, B is not delivered.
- **D2b and AC-6.** A restart drops the one connection; all of a window's streams reconnect over a
  new TLS connection, and the reload logic is unchanged. FT's stream rides the same connection, but
  its subscription lifetime is untouched. The transport probe does not establish this.

### C. Share the existing shell and rig streams across windows (variant without A's composition)

One `SharedWorker` per origin owns `/v1/events` and `/v1/rig/events`, exactly as served today, and
fans their events out to windows over `MessagePort`s. FT stays per window. D1-ii holds 3 persistent
connections. No daemon change.
- **Late-window initialisation.**
  - **Shell:** unchanged open-then-fetch. The worker must acknowledge a window's port before that
    window fetches its baseline.
  - **Rig:** there is **no REST rig snapshot** today, only `GET /v1/rigs`, the catalogue. A late
    window currently gets state from its own subscription's bootstrap poll (`TriggerBootstrap`) and
    the hub's cached TX alarm. Under C, one of two changes is needed:
    - the worker caches the latest frame of each rig event kind. `rig-state` is merged
      client-side, so the cache must merge the same way;
    - a new bootstrap trigger is added. A bootstrap is a CAT read sent to the rig.
- **Owner failure.**
  - **Windows closing.** MDN: "A shared worker will remain alive as long as any open page holds a
    reference to it" (https://developer.mozilla.org/en-US/docs/Web/API/SharedWorker). This covers
    windows closing.
  - **The spec gives no failure signal.** The WHATWG `MessagePort` interface
    (https://html.spec.whatwg.org/multipage/web-messaging.html#message-ports, §9.4.4) defines
    `postMessage`, `start()` and `close()`, with only `onmessage` and `onmessageerror` handlers.
    `close()` disentangles without notifying the peer. The non-normative §9.4.5 advises "a simple
    protocol whereby the other side acknowledges it still exists".
  - **To verify.** Whether Firefox signals a crashed or terminated SharedWorker to its windows is
    browser behaviour the spec text does not settle.
  - **Detection.** Windows need a heartbeat to detect a lost owner.
  - **Recovery must preserve the connection budget.** Reverting every window to its own streams
    recreates the defect, so it **cannot count as successful recovery**. Recovery means
    re-establishing one shared owner within D2b, for example a new worker instance or a single
    elected window. Until then the windows are degraded and must say so.
- **Reconnect recovery.**
  - **Broadcast.** The worker sees each drop and reopen and broadcasts the transition.
  - **Baseline refresh, per window.** Each window then runs its own count and its archive and
    default verification (Q2's one verification per reconnect, per window).
  - **`openReviving` cannot move as it is.** It keys on `document.visibilityState` and `window`
    `online`, which a worker lacks. Windows must report visibility and `online` to the worker, and
    "revive only when dead" must be preserved.
- **Bounded buffering.** The spec states no bound on a port message queue (§9.4.4–9.4.6), so
  `postMessage` to a throttled or frozen hidden window is assumed to queue without bound. The
  worker needs a per-port bound with eviction and a forced resync, mirroring the daemon's
  slow-reader policy. That eviction must not depend on the hidden window to release anything (O3).
- **Browser support.**
  - **SharedWorker.** MDN marks it "Baseline 2026 … Newly available". The per-browser table did not
    render for the fetcher.
  - **Module workers.** `type: 'module'` is a documented constructor option
    (https://developer.mozilla.org/en-US/docs/Web/API/SharedWorker/SharedWorker).
  - **EventSource.** It is "available in Web Workers"
    (https://developer.mozilla.org/en-US/docs/Web/API/EventSource). Its availability in a
    SharedWorker specifically is to verify.
- **Upgrade skew** (cf. alpha.5 Finding #4). The worker script's hashed name changes per build, so
  windows of two builds run two workers during a rollout, transiently doubling the shell's
  connections.
- **Semantics.** `rig-clients` would count 1 worker rather than windows; the SPA does not display it
  today. Archive guard and reload decisions stay per window.
- **D2b and AC-6.** On a restart the worker's streams reconnect once for all windows. Each window's
  default-switch reload is unchanged, but a reload can briefly leave the worker with no clients, and
  it may be torn down and re-created. Recovery must not depend on which window reconnects first.
- **What can be probed offline.** C's request capacity can be probed without building a worker:
  three existing streams (shell, rig and FT) plus three held lookups. Its ownership, failure and
  recovery semantics cannot be probed until the worker exists.

### Assessment and the next evidence

- **B** is the only candidate that removes the six-connection exhaustion mechanism. It remains
  bounded by flow control, scheduling, the advertised stream limit and the daemon's caps. Its costs:
  - installation and trust: anchoring, renewal, uninstall, and the persistence of profile copies;
  - the origin migration of preferences and the Session list;
  - HTTP/1.1 serving, which leaves D1/D2 unmet.

  B remains the preferred next investigation (reviewer, 2026-11-05).
- **C** needs no install change, but adds a cross-window coordination layer: a heartbeat, a rig
  cache, a revive relay and per-port bounds. Its recovery must also keep the budget. Even then, it
  fails D2a whenever three lookups stay held for more than 1 s, unless a lookup budget is also
  ruled, and that budget changes enrichment behaviour.
- **Next evidence: the B transport probe.** The reviewer approved it on 2026-11-05 as transport
  evidence only. It does not establish production trust, D2b/AC-6 recovery, or acceptance of B.
  1. **Isolation.**
     - The control `sm-w0099-probe` stays untouched. The probe uses an offline clone and a
       throwaway Firefox profile.
     - A TLS-terminating reverse proxy sits in front of the unchanged daemon: HTTP/2 to the browser,
       HTTP/1.1 upstream, SSE unbuffered.
     - The certificate is trusted only in that profile.
  2. **Cases.** Baseline, held lookup, release, and recovery. Then three and four overlapping held
     requests. Before timing, confirm that each is a distinct cache miss that reached the responder
     and is still pending in the browser.
  3. **Criterion.** Each ordinary request finishes within 1 s while the lookups remain held.
  4. **Records.**
     - protocol evidence for the tested traffic (`nextHopProtocol`);
     - daemon subscriber counts;
     - both proxy legs: the browser-to-proxy sockets and the proxy-to-daemon connections;
     - the proxy configuration, including its advertised HTTP/2 limits;
     - per-request timings;
     - SSE flushing: events arrive unbuffered;
     - cancellation propagation: an aborted browser request ends the upstream request.
  5. **Negative control.** HTTP/1.1 through the same proxy, with five streams and one held lookup, to
     isolate the effect of HTTP/2.

### Review of the comparison (reviewer, 2026-11-05)

The reviewer approved committing this comparison after six corrections, all applied above:
1. **Session list.** It survives an ordinary upgrade reload, so its loss on an origin change is a
   separate decision.
2. **Lookup overlap.** 2 + 1 is a sufficient example, not a maximum. The FT worked-station card adds
   a lookup, so the probe includes a fourth held request. C's failure is stated conditionally.
3. **B's capacity claim.** It is narrowed to the six-connection mechanism. Flow control,
   scheduling, the 16-subscriber cap and, in the probe, the proxy's advertised limit still apply.
4. **C's fallback.** Reverting to per-window streams is not recovery. Crash signalling is cited from
   the MessagePort interface and left to verify in browsers. C's capacity can be probed without a
   worker.
5. **Certificate costs.** They come from Mozilla's current policy reference and NSS's trust flags.
   `CT,CT,` is CA trust, so narrow leaf trust is pending verification.
6. **Redirects.** They are separated from HTTP/1.1 serving. A warning does not satisfy D1/D2.

The reviewer also approved the transport probe above. B remains the preferred next investigation;
mechanism approval is still pending.

## B transport probe (2026-11-05): HTTP/2 keeps ordinary requests fast under four held lookups

**This is transport evidence only.** It does not establish production trust, D2b/AC-6 recovery, or
acceptance of B. It ran with the reviewer's approval of 2026-11-05.

*Setup.*
- **Guest.** A clone, `sm-w0099-probe-b`, with its own copy of the control's overlay and seed, its
  own name and MAC, and SSH on port 2223. It shares only the read-only base image.
- **Same build and config as the control.** The R0 artifact `b766a145-dirty`, synthetic setup, the
  bridge against an absent serial port, and HamNut pointed at the same guest-local holding responder.
- **Default route removed.** The clone's default route was removed before the probe, and an
  outbound connection attempt then failed. The connected `192.168.1.0/24` route remained
  (`guest-state.txt`), so this is not strict network isolation.
- **Control untouched.** `sm-w0099-probe` stayed shut off. Its overlay, domain definition and seed
  were byte-identical before and after (`control-before.sha256`).
- **Proxy.** A purpose-built Go 1.26.2 reverse proxy (`main.go` in the evidence) in front of the
  unchanged daemon:
  - TLS on the browser leg, HTTP/1.1 upstream;
  - `FlushInterval: -1`, so every write is flushed;
  - advertised `SETTINGS_MAX_CONCURRENT_STREAMS` 100;
  - two instances: `:8443` offering h2, and `:8444` with HTTP/1.1 only, for the negative control.

  Every request and connection is logged on both legs.
- **Browser.** Headless Firefox 159 with fresh throwaway profiles, through an SSH tunnel. The same
  three windows as the feasibility probe: `/operate/phone`, `/config` and `/map`, giving 5 SSE
  subscribers.
- **Measured workload.** Four ordinary GETs, `/v1/config`, `/v1/version`, `/v1/qso-archives` and
  `/v1/default-logbook`, were timed as page `fetch` calls from the Settings window (`/config`).
  The other windows kept their streams open meanwhile. The held lookups were issued from the
  Phone/CW window.
- **Trust, profile-only.**
  - **Attempt 1.** A self-signed leaf (`CA:FALSE`), added to the profile with `P,,` (trusted peer),
    was **refused by Firefox: `MOZILLA_PKIX_ERROR_SELF_SIGNED_CERT`**. That run is kept as
    `attempt1-h2-selfsigned-peer-trust.jsonl`.
  - **Runs used.** A throwaway CA instead (`CA:TRUE`, `pathlen:0`, name-constrained to
    `127.0.0.1` and `localhost`), trusted `C,,` in each profile only, issuing the proxy's leaf.

  This bears on B's provisioning shape: Firefox did not accept peer trust of a self-signed leaf.

*Evidence.* `/home/mveary/sm-guest-probe-b/evidence/` (0700, `MANIFEST.sha256`):
- the driver and proxy sources;
- both drivers' logs and both proxies' logs;
- the responder journal and the guest `smd.log` extract;
- the guest state, the tooling versions and the certificate fingerprints.

The private keys are not in the evidence.

| Case | HTTP/2 (`:8443`) | HTTP/1.1 control (`:8444`) |
|---|---|---|
| Streams | 5 SSE subscribers; **1** Firefox socket; navigation and all timed requests `nextHopProtocol` `h2`; the proxy logged all 6 stream requests (4 `/v1/events`, including the flush check, and 2 `/v1/rig/events`) as `HTTP/2.0` | 5 subscribers; **7** Firefox sockets; `http/1.1` |
| Baseline | 2–4 ms | 2–4 ms |
| 1 held (`9X0PRA`) | barrier: distinct miss at the responder, pending in the browser. Then **3 ms** each, with the lookup still pending afterwards | barrier as for HTTP/2. After 4 s, still pending; completed after release at **4,437–4,439 ms**, of which 4,436–4,439 ms came before request start |
| Recovery | 2–3 ms | 2–3 ms |
| 3 held (`9X0PRC`–`E`) | 3 distinct misses arrived; all pending. **2–3 ms** each, still pending afterwards | not run (control scope) |
| 4 held (`9X0PRF`–`J`) | 4 distinct misses arrived; all pending. **2–3 ms** each, still pending afterwards | not run |
| Cancellation (`9X0PRK`) | browser abort, then the proxy's request ended with `context canceled`, its upstream connection closed (13 → 12), and the daemon's enrich handler returned at the same moment (512 ms, the time of the abort) | not run |
| SSE flushing | `fetch('/v1/events')` read through the proxy: headers and first body bytes (`: connected`) at **3 ms** | not run |

*Reading.*
- **Through the proxy, HTTP/2 held D2a's 1 s with one, three and four distinct lookups pending.**
  Every timed ordinary request (the four GETs above, from Settings) took 2–4 ms. The lookups were
  confirmed still pending after those requests completed.
- **The HTTP/1.1 control reproduced the starvation.** The setup was the same apart from the
  browser-leg protocol: same proxy, same daemon, five streams, one held lookup. The control result
  matches the feasibility probe over plain HTTP. So the difference is attributable to HTTP/2 on the
  browser leg, not to the proxy.
- **Observed but not explained:** the control showed 7 Firefox sockets, not 6. The control still
  starved, so a seventh socket did not carry the ordinary requests. Firefox may pool some
  connections separately (an inference, not verified).
- **What this does not show:**
  - the daemon serving h2 itself;
  - Go's default stream limit;
  - flow-control behaviour under heavy streams;
  - D1-ii's four-window layout with real FT enrichment;
  - restart recovery;
  - any production trust route.

  The lookups were page `fetch` calls of the request enrichment makes, as in the feasibility probe.

*Disposal.* Both guests are shut off and still defined. Disposal of both guests is the operator's
decision, now that this comparative evidence is recorded.

## B trust-lifecycle probe: run sheet (2026-11-05; prepared, not run)

**Reviewer direction (2026-11-05).** Transport feasibility now has evidence. Provisioning is still
an unresolved cost of choosing B, so the next probe tests the certificate-trust lifecycle on a
disposable clone, with Firefox running inside the guest so that trust-store changes stay there. It
measures four things:
- **Installation:** does the system-anchor route give warning-free HTTPS and verified h2, in both
  an existing and a fresh profile?
- **Trust scope:** is an otherwise-valid certificate from that CA, issued for an out-of-scope
  hostname, rejected?
- **Renewal:** does replacing the leaf under the same CA need any new trust action?
- **Removal:** does removing the anchor restore rejection after a Firefox restart, in both
  profiles?

It records the exact steps and the trust state before and after each. Amended after the
reviewer's approval (2026-11-05): explicit certificate validation, tightened rejection criteria,
profile-trust checks rather than an empty database, and keys kept apart from the evidence. Network containment must be
established first, because default-route removal alone was insufficient. B remains Proposed.
Origin migration and D1/D2 recovery need their own resolution. Both guests stay shut off while this
sheet is prepared. Nothing below has been run.

**Route under test (the proposed system-anchor route).**
- A per-install CA, name-constrained to the loopback names (`permitted;IP:127.0.0.1/32`,
  `permitted;DNS:localhost`; `CA:TRUE`, `pathlen:0`), issues the server leaf.
- Install: `install -m 0644 <ca> /etc/pki/ca-trust/source/anchors/station-manager-local-ca.pem`,
  then `update-ca-trust`.
- Remove: delete that file, then `update-ca-trust`.
- Firefox is expected to see the anchor through NSS's `libnssckbi.so` → `p11-kit-trust.so` (the
  wiring measured on the host); step 3 re-measures it in the guest.

A leaf-only anchor is not tested. The transport probe found that Firefox refused peer trust of a
self-signed leaf, and leaf-only renewal cannot reuse an anchor. `Certificates.Install` (CA trust
`CT,CT,`, persistent in profiles) is also not tested.

**Guest.** A new clone, `sm-w0099-probe-t`, gets its own copy of the control's overlay and seed,
created while both existing guests are shut off. The control `sm-w0099-probe` and the transport
clone `sm-w0099-probe-b` stay untouched and shut off; the control's checksums are verified before
and after, as last time. The new clone is disposable, and its disposal is the operator's decision
after the evidence is recorded.

**Containment (established before anything is copied in or run).**
1. **The domain has no network interface.**
   - `domain.xml` drops `<interface>` and adds `<vsock model="virtio"><cid auto="no"
     address="3099"/></vsock>`.
   - Control is over AF_VSOCK only: `ssh -i guest_key smtest@vsock/3099`, through the host's
     `systemd-ssh-proxy`. File transfer uses `scp` over the same path.
   - The guest's `systemd-ssh-generator` binds sshd to vsock port 22 when it runs in a VM with
     AF_VSOCK and sshd is installed (`systemd-ssh-generator(8)`; the guest side is an inference until
     step 2 shows it).
2. **Verify containment before step 3, and stop if any check fails** (do not re-add a network
   interface):
   - host: `virsh domiflist sm-w0099-probe-t` lists no interfaces;
   - guest: `ip -br link` shows only `lo`, and `ip route` and `ip -6 route` are empty;
   - guest: `curl -m 3 https://1.1.1.1`, `curl -m 3 https://fedoraproject.org` and
     `getent hosts fedoraproject.org` all fail;
   - record all output.
3. **Hostname mapping and resolver.**
   - The probe hostname `scope-out.test` resolves only through `/etc/hosts` (`127.0.0.1
     scope-out.test`), added in the guest.
   - Each profile's `user.js` sets `network.trr.mode` to 5, so DNS-over-HTTPS cannot be attempted.
     This is the only **explicit** probe preference change, recorded per profile. Firefox's
     automation machinery may apply further preferences of its own
     (https://github.com/mozilla-firefox/firefox/blob/main/remote/shared/RecommendedPreferences.sys.mjs);
     the driver records the effective values of the certificate-related ones it can read.

**Staging (host side; nothing in the guest fetches anything).**
4. **Download the browser and driver packages.**
   - `dnf download --resolve --alldeps --destdir pkgs firefox-<host's VERSION-RELEASE> nodejs
     nss-tools openssl p11-kit-trust`. Every tool the run needs (`firefox`, `node`, `certutil`,
     `openssl`, `trust`) must then be present in the guest from that local staging. If any is
     missing, stop.
     If the host's exact Firefox build has left the repositories, take the available one and record
     the version.
   - Verify every package with `rpm -K` against Fedora's key (`fedora.gpg`, already used for the
     image).
   - Write a SHA-256 manifest.
5. **Make the certificates on the host** in a separate private working directory,
   `~/sm-guest-probe-t/keys/` (0700, keys 0600). Only public certificates, their text dumps and
   fingerprints are copied into `trust-evidence/`; no key ever enters it:
   - **CA-S**, the scoped CA, constrained as above.
   - **leaf-1**, issued by CA-S: SANs `IP:127.0.0.1`, `DNS:localhost`; `serverAuth`.
   - **leaf-2**, the renewal: a new key and serial from CA-S, with the same SANs.
   - **leaf-out**, issued by CA-S: SAN `DNS:scope-out.test` only, and otherwise valid (dates, EKU,
     key usage). Its SAN matches the tested hostname, so a rejection cannot be a hostname mismatch.
   - **CA-U**, an unconstrained positive-control CA, issuing **leaf-ctl** for `DNS:scope-out.test`.
     It is served on a different port (`:8446`) under the same hostname and `/etc/hosts` mapping.
   - Record each certificate's text dump (`openssl x509 -noout -text`) and SHA-256 fingerprint.
6. **Copy into the contained guest over vsock:** `pkgs/`, the certificates and leaf keys, the
   existing `probe-proxy` (SHA `d593b734…`), and the driver. Then install from local files only:
   `sudo dnf install --disablerepo='*' ./pkgs/*.rpm`. Record the transaction and
   `rpm -q firefox nodejs nss-tools openssl p11-kit-trust`. The leaf keys are copied into the guest
   for the proxies; they never leave `keys/` and the guest.

**Proxy instances in the guest** (unchanged daemon upstream, as in the transport probe):

| Port | Leaf | Purpose |
|---|---|---|
| `:8443` | leaf-1, then leaf-2 | installation and renewal |
| `:8445` | leaf-out (CA-S) | scope |
| `:8446` | leaf-ctl (CA-U) | scope positive control |

**The trust-state snapshot (taken at every state below).**
- **System:**
  - `ls -l /etc/pki/ca-trust/source/anchors/`;
  - `trust list --filter=ca-anchors`, filtered to the probe labels;
  - `readlink -f /usr/lib64/libnssckbi.so`;
  - `rpm -q p11-kit-trust ca-certificates`.
- **Per profile, checking profile trust rather than requiring an empty or identical
  database.** In NSS, a certificate's presence and its assigned trust are distinct
  (https://nss-crypto.org/reference/security/nss/legacy/tools/certutil/index.html).
  - `certutil -L -d sql:<profile>`: every nickname with its trust attributes.
  - `certutil -L -d sql:<profile> -n <nick>`: the SHA-256 fingerprint of any entry.
  - The requirement: no profile entry for CA-S, CA-U or any probe leaf (matched by fingerprint)
    carries trust attributes of its own (`C`, `T`, `c` or `P` in any position).
  - `cert_override.txt` must not exist or must hold no probe entry, since a stored exception would
    not count as warning-free.
- **Served leaf:** the fingerprint of the certificate on each port, from `openssl s_client`.

**The browser check (per profile, per state).** Headless Firefox in the guest, driven over local
WebDriver BiDi by the driver. Every session is requested with `acceptInsecureCerts: false`
(https://www.w3.org/TR/webdriver/#capabilities), and the capability the session returns is
recorded. An absent `cert_override.txt` alone does not show that validation was on. It navigates
to:
- `https://127.0.0.1:8443/operate/phone`
- `https://localhost:8443/operate/phone`
- `https://scope-out.test:8445/`
- `https://scope-out.test:8446/`

For each, it records:
- success, or the navigation error code (for example `SEC_ERROR_UNKNOWN_ISSUER`, or a
  name-constraint error);
- listener health at the same moment: from the guest, `openssl s_client` completes a handshake on
  each port and reports the served fingerprint. A rejection only counts when its listener is
  healthy;
- on success: `location.protocol`, the navigation entry's `nextHopProtocol` (`h2` required), and
  that the SPA document loaded;
- after the run, a check that no `cert_override.txt` was written.

**Sequence.**

| # | State | Action | Expected |
|---|---|---|---|
| T0 | before install | create profile **P-old** (empty, 0700); run the browser check; snapshot | every URL fails with a certificate-trust rejection, listeners healthy; no anchor; no trusted probe entry in P-old |
| T1 | install | anchor CA-S and CA-U with the steps above; snapshot | both anchors listed by `trust list`; no trusted probe entry in either profile |
| T2 | installed, existing profile | restart Firefox on P-old; run the check | `:8443`: both names succeed with `h2`, and no exception is stored. **Scope enforcement is established only if `:8445` fails with a name-constraint validation error (e.g. `SEC_ERROR_CERT_NOT_IN_NAME_SPACE`) while `:8446` succeeds**, both listeners healthy. A timeout, a refused connection or an unknown-issuer error is **inconclusive** for scope |
| T3 | installed, fresh profile | create **P-new** after the install; run the check | the same as T2 |
| T4 | renewal | swap leaf-2 onto `:8443` and restart that proxy (dropping its connections), with no trust action; snapshot (served fingerprint = leaf-2); run the check in both profiles: first in the running Firefox, then after a restart | `:8443` succeeds in both profiles, both times; the anchors are unchanged; still no trusted probe entry in either profile |
| T5 | removal | remove both anchors with the steps above; snapshot; run the check once in the still-running Firefox (informational: it may cache trust), then restart Firefox and run the check in both profiles | after the restart, every URL fails with a **certificate-trust rejection** (an unknown-issuer class error) in both profiles, **with every listener healthy**; no anchor; no independently trusted profile copy and no exception |

**Records** (to `trust-evidence/`, 0700, with a SHA-256 manifest):
- the containment proof (step 2);
- the package manifest and the `rpm -K` output;
- the public certificate dumps and fingerprints (no keys);
- the exact commands run at each step, with their output;
- every snapshot and every browser-check result;
- the guest journal extract;
- the control's checksums, before and after.

**Not established by this probe:**
- the production install path (RPM scriptlet, upgrade, uninstall);
- non-Fedora or Flatpak Firefox;
- other browsers;
- whether p11-kit honours name constraints for non-NSS consumers (curl or Python in the guest
  may be recorded as side observations only);
- origin migration;
- D2b/AC-6.

## WebSocket event transport: the pool probe (2026-11-05)

**Why this, before more of B.** The operator questioned the cost of the B trust probe. A browser
page has only browser networking (fetch, EventSource, WebSocket, WebTransport, WebRTC), and RPC
libraries ride on those. But WebSockets need not draw from the HTTP per-host pool:
- **Firefox 159's built-in defaults on this host** (read from `omni.ja`):
  - `network.http.max-persistent-connections-per-server` 6;
  - `network.websocket.max-connections` 200;
  - `network.http.max-connections` 900.
- **Chromium's source.** The reviewer cited it as separating the normal and WebSocket pools, with
  per-group defaults of 6 and 255
  (https://chromium.googlesource.com/chromium/src/+/refs/heads/main/net/socket/client_socket_pool_manager.cc).

**Reviewer's direction (2026-11-05).**
- Assess WebSockets **for events** before moving any requests to RPC. If moving the persistent
  event streams out of the HTTP pool is enough, the current enrichment API stays, cancellation
  included, and no asynchronous-job design is needed.
- One WebSocket per window is optional. Keeping FT on its own connection may preserve its ownership
  and presence semantics more directly.
- Capacity sketch, a calculation and not a latency guarantee; four lookups is not an established
  maximum:
  - all events on WebSockets: four held HTTP lookups leave 2 of the 6 HTTP slots free;
  - shell and rig on WebSockets with FT kept on SSE: they leave 1 free.
- Asynchronous enrichment over a socket is not free. Response matching, cancellation, concurrency
  limits and settling pending requests after a disconnect are application design. It is added only
  if the event-transport change proves insufficient.
- The transport change itself still needs:
  - explicit Origin validation, because the current guard skips Origin for GET
    (`internal/api/csrf.go:42`);
  - reconnect and state recovery;
  - bounded buffering;
  - daemon shutdown handling;
  - unchanged FT subscription lifetime: an ordinary open window must not become an FT presence
    signal.
- B and its trust-lifecycle probe are paused. The guest `sm-w0099-probe-t` is shut off and
  retained: it is contained, staged, and nothing has run in it.

**Setup.**
- **Server.** A throwaway loopback server, not Station Manager: stdlib Go with a minimal RFC 6455
  endpoint (`main.go` in the evidence). One host and port, `127.0.0.1:18700`, served the page, the
  ordinary endpoints, SSE (headers and an initial `data: hello` flushed at once) and the
  WebSocket endpoint (sends `hello`, echoes).
- **Held requests** blocked until released. The driver read server state and released holds over a
  separate control port, so its own reads never used the browser's connections.
- **Browser.** Headless Firefox 159, fresh profile, BiDi session with `acceptInsecureCerts: false`.
  One page. The per-host pool is browser-wide, so the windows were not modelled.
- **Timed requests.** Four uncached GETs (`Cache-Control: no-store`, a unique query string,
  `transferSize` about 360 B), measured through complete body receipt.
- **Barriers.**
  - Every WebSocket opened, received `hello` and echoed a message before timing.
  - Every EventSource received its first message.
  - Every held request was confirmed both received and holding at the server, and pending in the
    browser.
- **Chromium was not installed, so it was not tested.**

*Evidence.* `/home/mveary/sm-ws-probe/evidence/` (0700, `MANIFEST.sha256`): the driver, server
and page sources, plus the driver and server logs.

| Configuration | Open (server count; Firefox sockets) | Baseline | 1 held | 4 held | Recovery |
|---|---|---|---|---|---|
| **All events on WebSockets** (9 WS ≈ D1-ii's 4 × 2 + FT) | 9 WS, all echoed; 10 sockets | 1–3 ms | **1–2 ms**, hold still pending | **1–2 ms**, all 4 still pending; 15 sockets | 1–3 ms |
| **Shell and rig on WebSockets, FT on SSE** (8 WS + 1 SSE) | 8 WS + 1 SSE; 14 sockets | 0–1 ms | **0–2 ms**, still pending | **0–1 ms**, all 4 still pending; 14 sockets | 1–2 ms |
| Negative control: 6 SSE | 6 SSE, all first messages; 6 sockets | **not complete after 4 s** | — | — | — |
| Negative control: 5 SSE + 1 held | 5 SSE; 5 sockets, then 6 | 1 ms | **4,026 ms** each, 4,025–4,026 ms of it before request start (completed only after release) | — | 1–2 ms |

*Reading.*
- **In Firefox 159, open WebSockets did not consume the six HTTP slots.** With 9 WebSockets open
  and 4 HTTP requests held, ordinary requests completed in 1–2 ms. The same server, page and
  browser, with SSE in place of WebSockets, starved exactly as in the daemon probes. The socket
  counts match the capacity sketch:
  - 8 WS + 1 SSE + 4 held + 1 free = 14;
  - the HTTP pool's six held 1 SSE, the 4 holds and 1 ordinary connection.
- **With event connections on WebSockets, four synthetic HTTP holds did not block ordinary
  requests.** This supports retaining the current enrichment API for the SMD implementation and
  acceptance run. The probe exercised synthetic held requests, not SMD's enrichment API.
- **Not established:**
  - SMD itself;
  - four-window acceptance;
  - restart recovery (D2b/AC-6);
  - Chromium or any other browser;
  - more than four held requests;
  - Origin validation, buffering, shutdown and FT presence semantics, which are design work for
    the candidate.

**Next step (reviewer, 2026-11-05):** a design comparison for WebSocket event delivery.
- **Starting shape:** shell and rig on WebSockets, with FT remaining on its existing SSE
  connection. That configuration was measured, and it disturbs FT's lifetime semantics least.
  Moving FT is compared only where there is a concrete benefit.
- **It covers:** Origin, reconnect, recovery, bounded buffering and shutdown, while retaining the
  agreed four-window (D1) and restart (D2b/AC-6) acceptance criteria.
- **Unchanged:** B stays paused; RPC and asynchronous enrichment remain deferred.

## WebSocket event delivery: design comparison (2026-11-05; design only)

**Scope (reviewer, 2026-11-05).** Two options:
- **W1:** separate shell and rig WebSockets, keeping their current subscription ownership.
- **W2:** one combined shell/rig WebSocket per window, only if it demonstrably simplifies the
  implementation.

Fixed for both:
- FT stays on SSE and enrichment on its existing HTTP API.
- The probe removes any need to combine streams to save HTTP slots, so the option with fewer
  lifecycle changes is preferred.
- No transport or certificate probe is needed before this review.

Nothing is built. Mechanism approval is pending.

### Current paths, traced

**Startup** (`frontend/app/src/main.ts`).
1. **Shell stream.** At module load the shell calls `openLogEvents` (`main.ts:719`). It opens
   `/v1/events` once per tab, ref-counted so the map joins it rather than opening its own
   (ADR 0079, 2026-09-14 update). It is never closed.
2. **Boot bracket.** `bootArchiveScoped` (`lib/config/archives.svelte.ts:458`) brackets the station
   context and catalogue reads between two identity reads. It runs with the check counter raised, so
   guarded requests wait. On agreement it binds the archive. On disagreement it reloads. On an
   unreadable identity it latches the gate and watches for the daemon.
3. **Rig stream.** `applyStationContext` (`main.ts:328`) opens `/v1/rig/events` once
   (`rigEventsOpen`), only when `catEnabled` is set and not in a settled Map tab (`main.ts:418`). It
   runs again after first-run setup, and the guard prevents a second open.

**Rig bootstrap** (`internal/bridge/handler.go`). The handler:
1. sets headers and flushes;
2. calls `Subscribe()`, which replays the hub's cached TX alarm to a late subscriber
   (`service.go:957`);
3. writes `: connected`;
4. calls `TriggerBootstrap(r.Context())` **synchronously**, a CAT identity and state poll whose
   replies fan out to every subscriber;
5. loops.

So a late window learns rig state from its own subscription's bootstrap.

**Reconnect.**
- **`openReviving`** (`lib/api/sse-reviving.ts`) wraps every EventSource. The browser owns transient
  retries. The wrapper recreates a stream that is CLOSED, or CONNECTING after an error, when the tab
  becomes visible or `online` fires, and only while it is visible. A healthy stream is never touched.
- **Shell.** Per subscriber, `sawError` turns the next `open` into `onReconnect`. That calls
  `refreshLogbookCount()` and `verifyArchiveGeneration()` (`main.ts:723`).
- **Rig.** Every `open` calls `catLink.onOpen()`, `noteStreamReopen()` and
  `verifyAfterRigReconnect()` (`main.ts:431`):
  - `noteStreamReopen()` re-fetches build identity only after a drop (W-0004 AC3, which ratified
    that build identity rides the rig stream);
  - `verifyAfterRigReconnect()` does nothing before the boot bracket has proven a baseline.

**Archive and default verification** (`archives.svelte.ts:535`, `:588`).
- Both verifiers read identity with retries. The result decides:
  - the same archive and default: nothing happens;
  - another archive or another default: the page reloads;
  - unreadable: `switchUnresolved` latches, fail-closed.
- `verifyArchiveGeneration` also reloads when there is no proven baseline.
- A reconnect that drops both streams therefore runs one verification per stream. Q2 (one
  shell-owned verification per combined reconnect) applied only to A's composition.

**Daemon side.**
- **Shell handler** (`internal/api/handler_events.go`): Subscribe before the 200. `: connected` is
  the first body bytes. Each write gets a 10 s deadline (`sseWriteTimeout`), and there is a 30 s
  keepalive. It returns on `r.Context()`, on `shutdownCh`, on a closed hub channel (slow-reader
  eviction), or on a write error.
- **Rig handler:** the same shape, plus the bootstrap.
- **Admission:**
  - `limitEventSubscribers` wraps each stream route (`server.go:362`, `:402`, `:434`), with one
    shared cap, default 16;
  - `isSSEPath` exempts the stream paths from `limitConcurrent` (`middleware.go:47`).
- **Middleware chain** (`server.go:214`): `securityHeaders → logRequests → rejectWhenDraining →
  limitConcurrent → recoverPanic → requireSameOrigin`.
  - Host is validated for every method.
  - **Origin is checked only for unsafe methods** (`csrf.go:42`), and `originAllowed` accepts any
    loopback origin on any port (`csrf.go:112`).
  - `responseRecorder` implements `Unwrap` (`middleware.go:367`), which
    `http.ResponseController.Hijack` follows (Go 1.26 `net/http/responsecontroller.go:66`).
- **Shutdown** (`server.go:784`): it closes `shutdownCh` first, then calls
  `http.Server.Shutdown`.

### Responsibilities WebSockets add, common to both options

Library: `github.com/coder/websocket` v1.8.15 (ISC, GPL-3.0-compatible per `docs/licensing.md`;
the reviewer's recommendation; https://pkg.go.dev/github.com/coder/websocket). The library facts
below are from its v1.8.15 docs and source (`accept.go`, `close.go`, `read.go`, `write.go`,
`conn.go`, `hijack.go`) and must be re-checked against the pinned version when it is added.

**1. Origin: exact same origin, checked by SMD before `Accept`.**
- **Why it is needed.** The browser reads an SSE response only for a same-origin page, because
  EventSource fetches in CORS mode. A WebSocket is not subject to that: a page from another origin
  can open it and read every frame unless the server refuses the handshake.
- **The rule** (an explicit SMD check; the library's own check is not relied on):
  - `Origin` must be present. A browser always sends it on a WebSocket handshake (RFC 6455 §4.1,
    §10.2).
  - It must parse as an absolute URL with no userinfo, path, query or fragment. `null` and
    malformed values are refused.
  - Its **scheme** must equal the daemon's own: `http` when `r.TLS == nil`, otherwise `https`.
  - Its **host:port** must equal `r.Host` exactly, with the host compared case-insensitively.
    `r.Host` has already passed `hostAllowed`, as today.
  - Otherwise SMD answers 403 before calling `Accept`, logged like the existing refusals.
- **Why not the library's check.** The library's default is not this rule. Its `authenticateOrigin`
  returns nil for an absent `Origin`, and compares `u.Host` with `r.Host` while ignoring the scheme
  (`accept.go` v1.8.15, https://github.com/coder/websocket/blob/v1.8.15/accept.go). SMD therefore
  performs its own check and then calls `Accept` with `InsecureSkipVerify: true`, so that only one
  rule, SMD's, applies. The alternative of keeping the library check as a second gate would be
  redundant at best.
- **Dev proxy.**
  - Today's string shorthand `'/v1': 'http://localhost:8080'` (`vite.config.ts`) is expanded by
    Vite 8.1.3 to `changeOrigin: true` (`node_modules/vite/dist/node/chunks/node.js:18271`), so it
    **does** rewrite `Host`.
  - The `/v1` entry becomes an object:
    `{ target: 'http://localhost:8080', ws: true, changeOrigin: false }`, with no
    `rewriteWsOrigin`. Vite rewrites `Origin` only when `rewriteWsOrigin` is set (`node.js:18252`).
  - A dev page on the configured port 5176 (`vite.config.ts:16`) then arrives with
    `Host: localhost:5176` and `Origin: http://localhost:5176`, so it passes the rule. `hostAllowed`
    accepts loopback on any port.
- **Tests.** Allowed:
  - the production same origin, `http://127.0.0.1:8080` and `http://localhost:8080`, each with its
    matching `Host`;
  - the dev case above.

  Refused:
  - a missing `Origin`;
  - `null`;
  - a malformed value;
  - a different scheme (`https://` against plain http);
  - a different port on the same host;
  - a foreign host;
  - an `Origin` carrying a path.

**2. Timeouts after the upgrade.**
- **What Go does.** Go 1.26 clears the connection's deadlines on hijack (`hijackLocked`:
  `rwc.SetDeadline(time.Time{})`, `/usr/local/go/src/net/http/server.go:317`), so the server's
  `ReadTimeout` and `WriteTimeout` no longer apply. The required behaviour is set through the
  library instead.
- **Writes.** Every frame is written with `Conn.Write(wctx, MessageText, …)`, where
  `wctx = context.WithTimeout(stopCtx, sseWriteTimeout)` (10 s, as today). If `wctx` expires while
  bytes are in flight, the library closes the connection (`setupWriteTimeout`'s `AfterFunc` in
  `conn.go`), so a wedged peer ends the handler within the bound. A shutdown cancels `stopCtx` and
  ends an in-flight write the same way.
- **Reads: an SMD-owned reader, not `CloseRead`.**
  - **Why not `CloseRead`.** In v1.8.15, on a data message its goroutine calls
    `Close(StatusPolicyViolation, …)` from inside itself. `Close` then waits in `waitGoroutines` for
    `closeReadDone`, which that same goroutine closes only after it returns: a wait on itself until
    the library's 15 s timer (`read.go` L60–86, `close.go` L217–252,
    https://github.com/coder/websocket/blob/v1.8.15/read.go#L60). Closing the socket does not
    interrupt that channel wait.
  - **The reader.** One goroutine per connection loops on `c.Reader(readCtx)`. `readCtx` is
    `context.Background()`, never cancelled, because a cancelled read context makes the library
    close the connection without a handshake.
    - Inside `Reader` the library still handles protocol control frames: it answers pings, records
      pongs, and on a peer close frame echoes the close and closes the connection (`read.go`
      `readLoop` and `handleControl`, L183–218, L303–366).
    - **A data message:** `Reader` returns with no error. The reader reports
      `unexpectedData` to the handler's close coordinator and exits, without reading the message.
    - **Any error:** the reader exits. That includes a peer close frame (a `CloseError`), a closed
      connection (`net.ErrClosed`), or a transport error.
    - **Done signal.** On exit it closes `readerDone` (deferred), and records why it exited.
      `readerDone` is the explicit completion signal; no context cancellation is relied on to show
      a goroutine has finished.
  - **The reader never closes the connection itself.** Only the coordinator (responsibility 7)
    does.
- **Liveness.** A 30 s `{"event":"keepalive"}` text frame, mirroring the SSE keepalive. A dead
  peer fails its write deadline.

**3. Contexts.** The request context is not used after `Accept`. The library advises against it,
and Go cancels it only "until the Request's ServeHTTP method returns"
(`/usr/local/go/src/net/http/server.go:202`), so it no longer signals a disconnect. Each handler
owns:

| Context | Cancelled when |
|---|---|
| `stopCtx` | `shutdownCh` closes |
| `workCtx` | `stopCtx` is cancelled, or the reader has exited (peer gone, peer close, transport error, unexpected data); used for the rig bootstrap |

`readerDone` is a channel, not a context: it is the reader's own completion signal.

**4. Frame envelope.** One JSON text frame per event: `{"event": "<name>", "data": <payload>}`.
- The payload is the same object the SSE `data:` line carries today, and names are unchanged (the
  shell's `qso.*` and `forward.*`; the rig's `rig-state`, `tx-alarm` and the rest).
- `{"event":"connected"}` is the first frame after Subscribe, matching today's `: connected` contract.
- Keepalive is as above.
- **Adapting to the existing decoders.** Today's decoders take a JSON **string**:
  `decodeFrame(data: string, …)` (`lib/api/sse-decode.ts:39`) and `decodeQsoEvent(data: string)`
  (`lib/api/log-events.ts`). Each parses and then validates. The envelope instead delivers `data`
  as an already-parsed value, so the adaptation is explicit:
  - each decoder is split into a **value-level validator entry**, for example
    `decodeFrameValue(value: unknown, …)` and `decodeQsoEventValue(value: unknown)`;
  - the existing string functions become `JSON.parse`, then that entry. The SSE path and its tests
    are unchanged, and one validation implementation serves both transports;
  - the socket transport parses each frame once. It drops the frame with the existing throttled
    warning if the frame is not JSON, not an object, has no string `event`, or has no `data`
    member (except for `connected` and `keepalive`). It then dispatches `data` by `event` to the
    value-level entry;
  - an unknown `event` name is ignored, as EventSource ignores an unregistered event type today;
  - nothing is re-stringified or double-encoded.

**5. Bounded buffering: unchanged.**
- The handler consumes the same hub channel.
- **Slow-reader eviction** closes it. The handler then ends the stream as below, and the client
  reconnects and resyncs, as with SSE.
- **Wedged peer:** it fails the per-write deadline.
- **Inbound:** the browser sends nothing but control frames, and data messages are refused (1008).

**6. Admission: unchanged in meaning.**
- **Routes:** the new routes sit behind `limitEventSubscribers`, and `isSSEPath` (renamed
  `isStreamPath`) includes them. They count against the shared cap of 16 and are exempt from
  `limitConcurrent`.
- **Draining:** `rejectWhenDraining` applies before the upgrade.
- **The admission slot is held for the handler's whole life:** `limitEventSubscribers` releases it
  when the handler returns, which is after cleanup (below).
- **Capacity:** D1-ii still uses 9 subscribers.

**7. Shutdown contract.** Go's `Shutdown` "does not attempt to close nor wait for hijacked
connections such as WebSockets" (`server.go:3142`). So `api.Server` gains a small stream registry
(a mutex, a `closing` flag and a `sync.WaitGroup`):
- **Registration happens before the connection leaves HTTP ownership.** The handler calls
  `register()` before `Accept`. If `closing` is set, it answers 503 and never upgrades. `Shutdown`
  sets `closing` under the same mutex, in the existing `shutdownOnce`, before closing `shutdownCh`.
  So no handler can register after shutdown has begun, and every upgraded connection is counted.
- **Interruption.** `stopCtx` is cancelled by `shutdownCh`:
  - **A handler in its select loop** sees `stopCtx` and closes.
  - **A blocked `Write`** is ended by `stopCtx`; the library closes the connection.
  - **The SMD reader** is not interrupted by `stopCtx`. It ends when the coordinator's close
    completes, or when the transport is closed at grace expiry.
  - **A handler inside the rig bootstrap** is covered under 8.
- **Close: best effort, then forced at the transport.** In v1.8.15, `CloseNow()` called after
  `Close()` has started does not interrupt it. Both go through `casClosing()`, and the second caller
  only waits in `waitGoroutines()`, for up to 15 s, until the first reaches `Conn.close()`
  (`close.go` L99–155, L230–252, https://github.com/coder/websocket/blob/v1.8.15/close.go#L123).
  Calling `CloseNow` at grace expiry therefore cannot guarantee closure. The helper closes the
  underlying transport itself:
  1. **Capture the `net.Conn`.** The helper passes `Accept` a thin wrapper `ResponseWriter` whose
     `Hijack()` calls `http.NewResponseController(inner).Hijack()` and records the returned
     `net.Conn` before handing it on. `Accept` obtains its connection through that method
     (`accept.go` L129, L160) and uses it as the `Conn`'s transport (L173). Because the wrapper
     is itself a Hijacker, it is reached before any `Unwrap`. A test asserts that the captured
     connection is non-nil after `Accept`.
  2. **One close coordinator per connection** owns every close. Its transitions depend on why it
     starts (corrected after review, 2026-11-05):

     | Trigger | Action | What ends the wait |
     |---|---|---|
     | **Unexpected data** (the reader reports, then exits) | start `Close(1008, "unexpected data message")` once | `closeDone`, or grace expiry. The reader has **already** finished, so its completion is not a reason to force closure; a cooperative peer gets its grace to reply |
     | **Shutdown** (`stopCtx`) or **hub eviction** | start `Close(1001, …)` once | `closeDone`, or grace expiry. A later reader exit only **records progress** and does not end the grace |
     | **Peer close or transport failure** (the reader exits with an error, no close in progress) | **clean up directly**, with no wait for a later shutdown, eviction or keepalive: `CloseNow()` (no other close is in progress, so it does not wait on one) | `CloseNow` returns |

     - **One close at a time.** If a second trigger arrives while a close is underway, no second
       close starts. Shutdown during a policy close only moves the deadline to the earlier of the
       two.
     - **No self-wait.** A 1008 close runs in the coordinator's goroutine after the reader has
       exited, so it never waits on itself. `waitGoroutines` has no `closeReadDone` to wait for,
       because `CloseRead` is never called.
  3. **Grace.** The grace runs to `min(start + 1 s, the shutdown context's deadline)` (the
     operator's ruling), for the policy close and the shutdown close alike. The registry hands each
     handler the shutdown context's deadline.
  4. **Forced closure at grace expiry.** If `closeDone` has not completed by the deadline, the
     coordinator calls `netConn.Close()` on the captured transport. Go's `net.Conn.Close` contract
     is that "any blocked Read or Write operations will be unblocked and return errors"
     (`/usr/local/go/src/net/net.go:136`):
     - a pending `writeClose` write fails, and `writeClose` ignores `net.ErrClosed` (`close.go`
       L192);
     - `waitCloseHandshake`'s read, or its `readMu` wait, ends;
     - a still-running SMD reader's read fails.

     `Close` then reaches `Conn.close()` (L117), which closes `c.closed`. `waitGoroutines` waits only
     for `c.closed`.
  5. **Completion notifications that stay observable.** The `Close` operation and the reader each
     publish a **completion**: its result is stored in a field first, and then a channel is closed,
     never sent on. The coordinator can therefore observe `closeDone` while it waits, and the
     cleanup can join it afterwards, without either consuming a one-shot value. The same holds for
     `readerDone`.
  6. **Join.** Cleanup waits on both completions, then reads their stored results.
  A wedged peer never receives 1001 or 1008, but its transport is closed at grace expiry. Two
  library paths were not read here: `readMu`'s lock implementation, and `write.go`'s control-frame
  write. Both are covered by the tests below rather than assumed.
- **Order of cleanup, then done.**
  1. The transport is closed (step 4, or the handshake completed).
  2. The `Close` operation is joined (`closeDone`).
  3. The reader is joined (`readerDone`).
  4. The hub subscription is released (`unsub`).
  5. The registry marks the handler done.
  6. The handler returns, which releases the admission slot.
- **What `Shutdown` promises.** After `httpServer.Shutdown`, `Server.Shutdown` waits for the
  registry with the caller's context. Only a return before the context expires means **every
  tracked handler has ended**. A context-expired return reports the deadline, as today's drain
  timeout does, and makes no such promise.
- **Budget.** The default `shutdown_timeout_sec` is 10. The grace is capped by what remains of it.
  A bootstrap wait (8) is not bounded, so it may consume the budget; in that case `Shutdown`
  returns the deadline.
- **If the library does not unwind.** If a join does not complete after the transport is closed,
  the handler is not marked done. `Shutdown` then returns at its deadline without the promise,
  rather than reporting a completion that did not happen.

**8. The rig bootstrap under a hijacked connection** (W1's rig handler; the shell has none).
- **The call.** The handler calls `TriggerBootstrap(workCtx)` synchronously, as today, after
  Subscribe and the `connected` frame.
- **Today's SSE path.** Inside `TriggerBootstrap` (`internal/bridge/service.go:1030`):
  - The pipeline-not-running and CI-V-while-keyed skips return at once, as today.
  - On CI-V, `underCmdMuCIV` takes `cmdMu.Lock()` (`pipeline.go:1413`). **That wait observes no
    context.** It ends only when the mutex is granted, which can come after **every holder queued
    ahead** releases, not just the current one. Each individual hold is bounded by its own limits:
    - CI-V ACK waits are bounded per frame (`civAckTimeout`, `pipeline.go:535` comment);
    - meter polls run under `ft8MeterPollTimeout` (`meterpoll.go`);
    - every serial write is bounded by the write watchdog (`internal/serial/serial.go:255`).
  - Inside the lock, `writeSnapshotReads(ctx, …)` checks `ctx` between frames
    (`pipeline.go:1438`). `writeAll` checks it before every write, including the first
    (`serial.go:324`). So a cancelled `ctx` writes no further frame.
- **Under W1.** On a disconnect or a shutdown, `workCtx` is cancelled: the reader exits (a peer
  close or a transport error), or `stopCtx` fires.
  - **Waiting for `cmdMu`:** the handler still waits until the mutex is granted. It then writes
    nothing, because `writeAll` sees the cancelled context, and returns. The loop sees the
    cancelled context and cleans up per 7.
  - **No total bound is claimed.** Per-operation timeouts bound each hold, not the wait across
    queued holders and batches. Command bursts, meter polls during a live FT8 capture, and other
    bootstraps can all be queued, and Go's `sync.Mutex` makes no fairness promise that would cap
    the queue.

    This is today's behaviour too: the SSE handler waits on the same mutex. W1 adds no new wait.

    While waiting, the handler is not draining its hub channel, so slow-reader eviction still
    bounds its buffering. Its admission slot stays held until it returns, as today.
  - **Mid-snapshot:** the bootstrap returns at the next frame boundary.
- **What changes and what doesn't.** Under SSE today the same wait exists; the request context's
  cancellation is what ends the snapshot early. W1 keeps that behaviour, but with `workCtx` in
  place of the request context. The bootstrap stays in the handler's own goroutine: not combined,
  and not unowned.
- **Shutdown during a held lock.** `Shutdown` can wait as long as the current `cmdMu` holder. If
  that exceeds the context, `Shutdown` returns the deadline without the all-ended promise (7).
  Before any shutdown change ships, a test with a held `cmdMu` should show the bootstrap ending
  after release with no frame written.

**9. Client reconnect: `openRevivingSocket`, a state machine beside `openReviving`.**
- **States:** `connecting` (one active attempt), `open`, `waiting` (one retry timer armed), and
  `closed` (torn down).
- **Rules:**
  - **One attempt and one timer.** There is at most one active WebSocket and at most one armed
    retry timer.
  - **Retries.** A failed attempt, or a drop from `open`, arms the timer once. An `error` followed by
    `close` is the same failure and is deduplicated: it gives one `onTransportError` and one timer.
    The timer fires after the retry interval (proposed fixed 1 s), with no backoff ramp.
  - **Visibility and `online` revive only a `waiting` socket.** When the tab becomes visible, or
    `online` fires, a socket in `waiting` cancels its timer and connects now. **A `connecting`
    socket is not dead and is left alone**, as is an `open` one. This keeps "revive only when
    dead", including the initial connect.
  - **Automatic retries do not depend on visibility.** They match the browser's own EventSource
    retries in a hidden tab today. Visibility and `online` only bring a waiting retry forward,
    which keeps the existing distinction between automatic retry and revival.
  - **Stale callbacks.** Each socket's handlers check that it is still the current one. Events from
    a replaced socket are ignored.
  - **Teardown.** Cancels the timer, closes the current socket, removes the `visibilitychange` and
    `online` listeners, and moves to `closed`. No later callback acts.
- **Events to callers** are as today: `onOpen` on every open, with `sawError` turning it into
  `onReconnect` (once per drop, never on the boot open), and `onTransportError` once per failure.
- **Proof.** The 1 s interval is a design choice. It does not by itself prove D2b; the restart
  demonstration does.

**10. Access log.** A hijacked connection's line is written when the handler returns, and its status
is not the recorder's. It is logged as `101` with the connection duration. The H3 subscriber
transitions (`middleware.go:239`) come from the shared admission layer and are unchanged.

**11. The SSE endpoints stay.** `cmd/loadgen` and the documented API use `/v1/events`, and FT
keeps `/v1/ft8/events`. The WebSocket routes are added beside them, for example `/v1/events/ws`
and `/v1/rig/events/ws`. The canonical API reference `docs/v2-design/api-endpoints.md` is updated
in the same change. Historical design documents stay as records.

### W1: separate shell and rig WebSockets

Two routes, each a WebSocket twin of today's SSE handler with the same body:
- **Shell:** Subscribe before the 101 is observable. A `connected` frame is first. Hub events are
  forwarded as `{event, data}` text frames.
- **Rig:** Subscribe (TX-alarm replay unchanged), then `connected`, then `TriggerBootstrap`
  synchronously as today, then forward.

**Client side.** `log-events.ts` and `rig-sse.ts` keep their handler interfaces and decoders; only
the transport under them changes. Their callers in `main.ts` and `mapData.svelte.ts` are
unchanged. Paths that stay as they are:
- the shared, ref-counted shell connection;
- the rig open in `applyStationContext`, with the same `catEnabled` and Map-tab conditions;
- build identity on the rig stream;
- the two verifications per double drop.

**Lifecycle changes:** the transport only. No ownership, bootstrap or verification change. Q3 and
Q4 do not arise, because there is no combined writer and no combined admission.

### W2: one combined shell/rig WebSocket per window

One route carrying both vocabularies. What it would have to add:
- **Conditional rig subscription.** The rig half depends on config read after the socket opens
  (`catEnabled`, settled Map tab), so it needs an in-band subscribe or unsubscribe message, or a
  reconnect, when the context arrives or changes. This is the Q3 problem.
- **Bootstrap ownership.** `TriggerBootstrap` is synchronous and, on CI-V, takes `cmdMu.Lock()`
  (`internal/bridge/pipeline.go:1413`), which cannot be cancelled. On a combined writer it must not
  stall shell forwarding, so it moves to its own goroutine with a single writer. This is the Q4
  problem.
- **Admission.** One connection stands for two subscriptions, both taken before the 101.
- **One reconnect for both halves.** It drives today's two verifications and the build-identity
  re-fetch from a single transition, which needs Q2's single-verification design.

**Simplification claimed:** none that applies. It would save one WebSocket connection per window.
That is a real benefit, but it does not outweigh the composition work: Q2, Q3 and Q4 return.

### Recommendation: W1

W1 changes the transport and nothing else. W2 would save WebSocket connections, not HTTP slots;
that benefit does not outweigh the three questions it brings back from A.

**Concrete changes.**
- **Daemon (`internal/api`, `internal/bridge`):**
  - a WebSocket stream helper in `internal/api`, built on `github.com/coder/websocket`. It holds:
    - the exact-Origin check, then `Accept` with `InsecureSkipVerify: true`;
    - registration;
    - the SMD-owned reader and its `readerDone` signal (no `CloseRead`);
    - the close coordinator;
    - the per-write `Write` context;
    - the keepalive;
    - the envelope;
    - close on eviction or shutdown, with the grace;
    - the cleanup order in responsibility 7;
  - the shell twin of `handleEvents`;
  - the rig WebSocket handler, in `internal/api` beside the shell's. `internal/api` already depends
    on `*bridge.Service` (`server.go:66`), so it calls the bridge's exported `Subscribe` and
    `TriggerBootstrap` directly. The bridge keeps owning its bootstrap and imports nothing new;
  - route registration behind `limitEventSubscribers`;
  - `isStreamPath`;
  - the stream registry and its wait in `Server.Shutdown`;
  - the dependency, added with the code that uses it, with `docs/licensing.md` noting it if it
    lists dependencies;
  - `docs/v2-design/api-endpoints.md` updated (the canonical reference).
- **SPA (`frontend/app`):**
  - `openRevivingSocket` beside `openReviving`;
  - `log-events.ts` and `rig-sse.ts` moved onto it, with decoders and handler interfaces unchanged;
  - the Vite proxy `/v1` entry as an object with `ws: true` and `changeOrigin: false`.
- **Unchanged:** FT8 SSE and its presence and linger semantics, the enrichment API, the archive
  guard, and the boot bracket.

**Tests, written RED first.**
- **Daemon:**
  - Origin: the allowed and refused cases in responsibility 1, with every refusal a 403 before the
    upgrade;
  - Subscribe happens before the 101 is observable;
  - the rig bootstrap is still sent on connect;
  - an evicted slow reader gets a close frame;
  - a wedged peer is ended by the write deadline;
  - **written first (RED):**
    - a policy close where the reader has already finished and the cooperative peer has not yet
      replied: the transport is not closed before the 1008 close frame is sent, and the peer gets
      its grace;
    - an ordinary disconnect, with no shutdown and no hub event: the handler cleans up at once,
      without waiting for a keepalive, eviction or shutdown;
  - unexpected data:
    - an inbound data message makes the reader report and exit;
    - the coordinator closes with 1008, and a cooperative peer completes the handshake promptly,
      well inside the 15 s library timer;
    - a peer that never answers has its transport closed at grace expiry;
    - shutdown arriving while that policy close is underway starts no second close, honours the
      earlier deadline, and the handler completes the cleanup order;
    - `readerDone` and `closeDone` are both observed before the handler is marked done;
  - shutdown:
    - a registration after `closing` gets 503 and no upgrade;
    - a responsive peer receives 1001;
    - a wedged peer is force-closed after the grace;
    - `Shutdown` returns success only after every handler has completed the cleanup order;
    - an expired context returns the deadline;
  - the bootstrap with `cmdMu` held, then the client disconnecting: after release, no frame is
    written and the handler ends;
  - shutdown with `cmdMu` held past the shutdown context: `Shutdown` returns the deadline and does
    not report completion;
  - the grace enforcement: a peer that never answers the close handshake, with the transport
    closed at grace expiry, and then `Close` and the reader both joined. With a cooperative peer,
    `Close` completes before the grace and the transport is not closed early;
  - the captured `net.Conn` is the `Conn`'s transport (closing it fails the `Conn`'s next write);
  - envelope adaptation: a valid frame reaches the same handler as the SSE path; malformed frames
    and unknown events are dropped or ignored, as specified;
  - the subscriber cap is applied;
  - the stream path is exempt from `limitConcurrent`.
- **SPA:** `openRevivingSocket`, against the rules in responsibility 9:
  - a `connecting` or `open` socket is never replaced by visibility or `online`;
  - a `waiting` one is brought forward;
  - one attempt and one timer at a time;
  - an `error` followed by `close` counts once;
  - callbacks from a replaced socket are ignored;
  - teardown cancels everything and leaks no listener;
  - `onReconnect` fires once per drop and never on the boot open.
- **Reversion proofs** for the Origin rule, the registration-before-upgrade rule and the shutdown
  wait.

**Validation against the agreed criteria** (the real-browser demonstration in this ADR):
1. **D1-i and D1-ii.**
   - The windows: Settings, Operate Phone/CW, Logbook, and a fourth on FT8/FT4.
   - Socket evidence: the HTTP pool holds only FT's SSE; the WebSockets are in their own pool.
   - Timing: D2a is measured in the browser for ordinary requests, including with lookups held
     (the barrier case, as in the probes).
2. **D2b and AC-6.** A real daemon restart, not a synthetic `online`: every window is usable on the
   correct binding within 10 s of the new instance being observed ready. The default-switch reload
   is unchanged.
3. **Negative control.** The same build with the SPA forced back to SSE reproduces the starvation.
4. **FT capture.** Proven separately, as already required: an ordinary open window is not an FT
   presence signal.
5. **Refreshed R0–R2, then a fresh R4–R6** with a new R4 backup, per the W-0024 record.

### Operator rulings (2026-11-05)

1. **W1** is approved as the mechanism.
2. **`github.com/coder/websocket` v1.8.15** is confirmed.
3. **Reconnect:** a fixed 1 s retry interval is approved.
4. **Shutdown grace:** 1 s maximum, capped by the remaining shutdown budget.
5. **Origin:** the specified SMD check and the dev-proxy configuration are approved.

These need no further decision round. **The implementation contract stays pending** the
grace-enforcement correction (applied above: an independent transport close at grace expiry, then
joins), together with:
- the envelope adaptation to the existing string decoders;
- the explicit statement that no total CI-V mutex-wait bound is claimed.

**Reviewer, 2026-11-05 (follow-up).** The independent transport close fixes grace enforcement. The
previously unread reader path showed that `CloseRead` would self-wait on a data message. The design
now uses an SMD-owned reader built on `Reader`, a single close coordinator for the 1008 and 1001
closes, and explicit `readerDone` and `closeDone` signals. Unexpected-data tests, including
shutdown during a policy close, were added. The envelope adaptation and the qualified CI-V wait
description were accepted.

**Reviewer, 2026-11-05 (coordinator transitions).** As first written, `readerDone` would have forced
transport closure before a cooperative peer could answer a 1008 close, and peer closure or
transport failure was missing from the coordinator's triggers. Both are corrected in
responsibility 7:
- the trigger table;
- completions that stay observable, with results stored separately;
- two RED-first tests.

The W1 build proceeds RED-first. The four-window and restart acceptance gates are unchanged.

B stays paused. RPC and asynchronous enrichment stay deferred. Still Proposed.

**Review of this comparison (reviewer, 2026-11-05).** W1 recommended; commit after corrections. The
corrections, all applied above:
- the dev-proxy trace (the shorthand sets `changeOrigin: true`, and the port is 5176), with the
  object configuration;
- exact same origin including scheme, with null, missing and malformed values refused;
- the shutdown contract: registration before upgrade, interruption, best-effort 1001, the cleanup
  order, and what a successful return promises;
- the bootstrap traced under hijack;
- the reconnect state-machine rules;
- `coder/websocket`, with its contexts, write timeout, close behaviour and envelope resolved;
- Go 1.26 clearing deadlines on hijack;
- W2 saving WebSocket connections, not HTTP slots;
- the dependency added with its code, and the canonical API reference updated.

## Operator rulings (2026-11-04)

These settle the acceptance criteria. The mechanism is still not approved, and nothing is built.

- **D1 Supported configurations (confirmed):**
  - **D1-i:** Settings, Operate Phone/CW and Logbook open at the same time. Under A, 3 connections
    and R = 3.
  - **D1-ii:** those three plus a fourth window on FT8/FT4. Under A, 5 connections and R = 1.
- **D2a Ordinary request latency:** at most **1 s** for each ordinary local API request exercised.
  It is measured in the browser, so it includes connection queueing and response transfer, not just
  daemon processing. SSE lifetimes are excluded.
- **D2b Restart recovery:** at most **10 s**. The clock starts when the new daemon instance is
  observed ready independently (not through the SPA), and stops when every window is usable with the
  correct archive and default binding. A default switch must also meet AC-6: every window reloads by
  itself, and the drafts it warned about are discarded.
- **Maximums, not averages.** Both limits apply to every case demonstrated. An average must not hide
  a stalled window.
- **A must pass both D1-i and D1-ii** against D2a and D2b. Free connection capacity alone does not
  establish acceptance. D1-ii, with one free connection shared by every ordinary request in four
  windows, is expected to be the binding case. If A fails it, the decision moves to B or C.

## Consequences

- **A new candidate.** Any fix produces a new candidate identity. W-0024's artifact facts, R1
  (`ci:local`) and the guest rehearsal (R2) are refreshed before a fresh R4–R6 run, which starts with
  a new R4 backup.
- **Documentation.** Candidate A adds one endpoint to `docs/v2-design/api-endpoints.md` in the same
  change. ADR 0079 and ADR 0010 (the rig SSE wire shape) get dated cross-references, not rewrites.
- **The ceiling.** Under A the six-socket ceiling remains. The supported configurations become a
  stated limit the operator can be shown, not an accident of how many windows are open.

## Triggers to revisit

- The operator needs configurations that A cannot meet within D2: take up B or C.
- The daemon gains TLS for another reason, such as LAN exposure or remote access: B's cost falls
  sharply.
- The supported browser set changes to one whose per-host limit or SSE behaviour differs.

## References

- **Records:** W-0024 acceptance record, entries 30–33; W-0012, the item "Three open windows exhaust
  the browser's connections" and the contacts-map ruling (2026-09-14); W-0013 inventory ("one
  multiplexed SPA event stream"); alpha.2 acceptance record, entries 36–37.
- **ADRs:** 0079 (and its 2026-09-14 update), 0010, 0019, 0045.
- **Frontend:** `frontend/app/src/main.ts` (`:418`, `:719`, `bootArchiveScoped`,
  `applyStationContext`, `setSetupSave`); `frontend/app/src/lib/api/log-events.ts`, `rig-sse.ts`,
  `ft8-sse.ts`, `sse-reviving.ts` and `archive-guard.ts`.
- **API:** `internal/api/handler_events.go`; `server.go:595`; `middleware.go` (`isSSEPath` at `:47`,
  `limitEventSubscribers`, and `logSSESubscriber` at `:239`).
- **Bridge:** `internal/bridge/handler.go`; `events.go`; `service.go` (`Subscribe`,
  `TriggerBootstrap`).
- **FT8:** `internal/ft8/service.go` (`onSubscriberRemoved` at `:463`, `onLingerExpired`,
  `captureLinger`).
