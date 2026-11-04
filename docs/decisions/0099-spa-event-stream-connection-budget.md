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

**FT8 presence.** The FT8 subscriber count drives the capture device and the attended-only disarm.
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
- **Startup sequence — open question Q3.** Today the shell's stream opens at module load. CAT and
  setup state arrive later, from the asynchronous config read inside `bootArchiveScoped`. The rig
  stream opens in `applyStationContext` only after that read, guarded by `rigEventsOpen`, because the
  function runs again after first-run setup. The design must say how the first combined connection
  gets its `rig=1` choice. Options to compare:
  1. Open shell-only at load, then make one deliberate upgrade to `rig=1` once the context shows
     `catEnabled` and the tab is not a settled Map tab. The upgrade must not fire the shell's
     `onReconnect`, build identity or archive re-verification as if it were a drop.
  2. Hold the shell stream until the config resolves. A failed config read must still open it
     shell-only, because ADR 0079's reconnection signal is needed most when config is unreachable.

  Both must also cover:
  - first-run completion, where setup completes in place and the rig part must be added without a
    reload
  - a later config change
  - the settled-Map exception (ADR 0079 update, and the `setup_complete` refinement): a settled Map
    tab never asks for the rig part
- **Daemon integration — open question Q4.** The new path must be added explicitly to:
  - **SSE classification.** `isSSEPath` (`internal/api/middleware.go:47`) enumerates paths, and a
    stream left out would hold an ordinary request slot under `limitConcurrent`.
  - **Subscriber admission.** One `limitEventSubscribers` admission per combined connection; its H3
    log counts SSE requests.
  - **Shutdown.** Observe `shutdownCh` as both existing handlers do.
  - **Bounded writes.** One per-write deadline (`armWrite`) across frames from both sources.
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
  - `rig-clients` would read 1 however many windows are open, losing the multi-window warning unless
    it is re-derived.
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
