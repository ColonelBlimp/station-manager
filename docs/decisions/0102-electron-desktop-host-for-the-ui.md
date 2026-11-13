---
number: 0102
title: Run the Station Manager UI in an Electron desktop host
status: Accepted
date: 2026-11-09
---

# 0102 — Run the Station Manager UI in an Electron desktop host

**Accepted (operator, 2026-11-09), after design review with no remaining findings
(codex-032 to codex-035).** Building it is tracked in [W-0027](../work/W-0027-electron-desktop-host.md),
slice by slice, each reviewed by the operator and Codex before the next. Every decision below was
ruled by the operator on 2026-11-09 after the W-0026 evaluation and its Electron proof of concept
(POC); the evidence, the reviewers' recommendations and the rulings are in
[W-0026](../work/W-0026-ui-host-evaluation.md). This ADR records the resulting architecture and
the requirements the rulings carry; implementation needs its own authorisation and work items.

## Context

Station Manager's UI is one Svelte SPA served by `smd` and run in the operator's browser. A
browser cannot own the application's windows: the operator opens any number of tabs on any route,
the browser keeps keys such as Ctrl+W, reload is always available, exports go through the
browser's download path (there is no consistent native Save dialog across supported browsers:
Chromium offers `showSaveFilePicker`, Firefox does not), and several-window behaviour has to be
reconstructed in the daemon (ADR 0100). The
operator: "With another technology, we can control how many windows are open and what they
contain - we cannot do this with a Browser", and "once SM gets out into the wild - it's whole
different ball game!".

W-0026 compared the browser, Electron, Tauri and Wails on the operator's required experience, and
a POC (a separate repository: the frozen SPA, a fixture daemon, an Electron 44 host and a Wails
v3 host) tested it. Electron delivered every evaluated behaviour, within these limits (W-0026's
summary table): one window per role; detached panels sharing the fixture daemon's state; the
close confirmation; no user reload; one "daemon unavailable" state with recovery, tested in the
main window and a detached panel window (the Map window by construction, untested; the static
Manual page has none); the native Save dialog (the download path and dialog options verified
automatically, the dialog seen and saving on the operator's desktop; choosing a destination and
cancelling not reported separately); browser-reserved keys reaching the page; remembered window
placement (under XWayland, on the operator's desktop); movable floating panels; and package size
and memory measurements. Wails v3 was
evaluated for rendering only: its "old-fashioned" look came from WebKitGTK drawing native form
controls with the GTK theme, which app-drawn CSS fixes in every host.

## Decision

Station Manager's primary UI host is an **Electron desktop shell**, with this boundary:

- **`smd` owns** operating state, persistence, rig control and transmission, and **serves the UI**
  (frontend and daemon versions stay together). Nothing operating moves into the shell.
- **The shell owns** windows, native dialogs, application shortcuts and the local "waiting for the
  daemon" page, through a **narrow host interface**; renderers stay sandboxed with context
  isolation and no Node, and reach only `smd`'s origin.

The operator's rulings (W-0026, 2026-11-09) define the experience:

1. **Remote access:** other machines and plain browsers are **view-only**, off unless enabled in
   `config.json`; a browser is fine for viewing; operating always requires the desktop app.
   `smd` adds an **operating-client check** (a per-install secret the desktop app presents;
   operating routes refuse requests without it). Its stated limit is accepted: it keeps out other
   users and machines, not other processes of the same user.
   **Amended 2026-11-13: no view-only browser client is built in C2; see "Update 2026-11-13"
   below.**
2. **Window set:** one main window (Operate, Logbook, Events, Settings as views), one Map window,
   one Manual window, and Rig Control and Session detachable into their own windows; each exists
   at most once, and asking again brings it forward. External links open in the default browser.
3. **Placement:** Linux runs under **XWayland** by default (on the operator's tested desktop,
   XWayland restored positions and a second launch raised the window; native Wayland restored
   sizes only; other desktops are not yet accepted). At start everything comes back **as
   the operator left it** (open panels, floating places, detached windows). The main window's
   minimum size is **1024x640**.
4. **Panels:** Rig Control and Session are **floating panels** (move, resize, reopen in place);
   whether the Phone/CW logging panel moves too is decided later; the FT8 view stays fixed.
   Docking was evaluated and rejected. Form controls are app-drawn; dropdown **lists stay native**.
5. **Shortcuts** (W-0026 §8, normative here):
   - Keys that belong to one window (the logging card's Escape = clear draft, Ctrl+Enter = log)
     are bound **only** in that window; elsewhere they are unbound and never invoke a different
     action. **Dialogs are the exception:** inside an open dialog, Escape and Ctrl+Enter act on
     that dialog, in any window.
   - The station-wide keys acting on shared, daemon-owned state (the rig family first) are
     defined once, with one handler per window, and work with the same meaning in the main, Rig
     Control and Session windows (not Map or Manual). They stand down under a dialog and during a
     daemon outage, and do **nothing while typing in an ordinary text field**; the Callsign field
     keeps its search-and-pounce exception.
   - Off CAT, the manual rig context stays in the main window, which logs from it; a detached Rig
     Control window is then a read-out and the rig keys act only in the main window.
   - These are page-level bindings; capturing browser-reserved keys moves no operating decision
     into the shell's main process.
6. **Closing and reloading:** closing the main window **stops the FT8 run and disarms TX**, and
   only the operating desktop app counts as presence; closing does nothing else (*amended
   2026-11-10, operator: a confirmed exit also closes the operating session; see ADR 0100's
   2026-11-10 rulings*). Closing asks only
   when something would stop (FT8 run or TX armed, a contact being entered, unsaved Settings).
   There is no user reload. One "daemon unavailable" state is required in every window that shows
   daemon state (the POC's tested scope is above). A window that sees a changed daemon build
   **waits until nothing is in progress**: while a contact is being entered, a Logbook or
   Settings edit is open, an FT8 run is active or TX is armed, the reload is held and a banner says
   a new version is ready; the window reloads when idle **or when the operator chooses**. Busy
   operating state (FT8 run, TX armed) comes from the daemon, not only from the current view's
   fields.
7. **Files and desktop integration:** exports use the **native Save dialog** (later Cabrillo, and
   an Open dialog for import); **launcher only**, no tray or desktop notifications.
8. **Updates and delivery:** releases ship through **our own signed dnf repository**. A
   desktop-app update takes effect only when the operator next shuts down or restarts the UI;
   nothing interrupts user activity. Security releases skip the ordinary settling period with a
   **7-day response target**, counted **from an applicable upstream fix becoming available** and
   met **when a tested, signed update is published to the repository** (the scenario gates still
   apply; opening a pull request or starting triage does not count; installation and activation
   at the next UI restart are separate milestones). How often security releases are monitored is
   not ruled; it must be frequent enough to meet the target. A
   **daily** support check (and one at release time) warns **8 weeks** before the pinned Electron
   line's end of support and blocks releases on it at **2 weeks**; it never stops an installed app.
9. **Deployments** (ruled 2026-11-09): two packages, the daemon (`smd`, which serves the UI) and
   the shell; the shell depends on the daemon, never the reverse. **Daemon + shell** is the
   operating station. **Daemon only** is a supported deployment for the central role of
   [ADR 0014](0014-upstream-forwarding-deferred.md) (a team or DXpedition server that receives
   QSOs from operating stations, deduplicates and does the final forwarding); it cannot operate
   a station. The topology itself stays deferred as ADR 0014 rules.

### Update 2026-11-13: no view-only browser client in C2

**Operator ruling (chat, 2026-11-13): "yes, drop view-only for C2".** It amends ruling 1. The
rulings above are left as they were made; this is what changes.

- **What is ruled.** C2 does not build a view-only class of client. The UI and the API are for
  the desktop app: one default-deny check for the app's credential, with a short explicit list
  of exceptions. A plain browser is not a supported client once C2 is on, for viewing or for
  operating.
- **Why.** A view-only class means sorting every route and event stream into view or operate,
  and keeping it sorted. The daemon declares some eighty routes, and a number of them need a
  judgment the method does not settle. That work buys local browser viewing, which has little use once
  the app is installed, and remote viewing, which was off by default and is not built. The
  operator's aim is that the desktop reduces what has to be supported. The evidence and the
  inventory are in W-0027 ("C2 scope question" and "Inventory").
- **What does not change.**
  - Operating always requires the desktop app (ruling 1's core).
  - The operating-client check and its stated limit: other users and machines are kept out,
    not other processes of the same user.
  - **Presence is still its own rule.** The credential says who is calling. The build must
    still name the one operating subscription that is the operator's presence, and how its
    loss stops and disarms through the guaranteed-stop controller (W-0027's C4).
  - The request guards for archive, default and session generation (ADR 0100 §5).
  - The daemon submitter of ADR 0014 keeps its own, narrower credential: QSO submission only,
    never rig or TX control or operating presence. Dropping the view-only class does not give
    that caller the operating credential.
  - The earlier ruling on timing (2026-11-11): the browser path is not retired until a user
    can install and launch the desktop app.
- **What it supersedes in this ADR.** Where the text below speaks of the browser as a view-only
  client, of "view-only enforcement", or of "the view-only route classification" as daemon
  work, read: not built in C2. That is the first bullet under "Requirements the rulings carry",
  the wording of the daemon-submitter bullet, the "Daemon work" line under Consequences, and
  the closing sentence of "Stay in the browser".
- **Deferred, with its trigger.** View-only or remote access is designed if and when it is
  needed. "Remote operation becomes a requirement" under "Triggers to revisit" already names
  it; a need for remote viewing alone is the same trigger.
- **Left to C2's design, not ruled here:** what becomes of the plain-HTTP listener on 8080;
  how the development loop and the tools that read the daemon over HTTP present the
  credential; the exception list; and the acceptance evidence. C2 gets its own brief, reviewed
  before anything is built.

## Requirements the rulings carry

These follow from the rulings; the reviewers named them (recorded in W-0026, "Carry into the
ADR" and the update plan) and they must be met by the implementation, not assumed:

- **View-only means a complete route classification:** every log, config and operating mutation,
  not only RF commands or hidden buttons. The browser fallback is view-only, not a fallback
  operating client.
- **Presence needs an authenticated operating subscription:** the daemon must define which
  subscription carries the operating app's presence and how its loss stops and disarms through the
  existing guaranteed-stop controller. A per-install credential alone does not define that. A
  normal close, an app or renderer failure and a reconnect stay distinct, and presence is a
  connection signal, not proof that a person is at the desk.
- **Restoring the workspace never restores operation:** no TX armed and no FT8 run at start;
  closing leaves contest mode alone (ADR 0101). *Amended 2026-11-10 (operator):* a confirmed exit
  closes the operating session, after FT8 stop/disarm and completion logging; Continue never
  resumes an FT8 run or arms TX (ADR 0100's 2026-11-10 rulings).
- **Idle-only reload needs asset compatibility:** a window waiting to reload after an `smd`
  upgrade must still be able to load lazy views (today's hashed chunks vanish with the old build
  and `spa.go` answers `index.html`); asset/API compatibility or a coordinated update must cover
  every open window.
- **Updates need package acceptance:** an in-place upgrade while the app runs (editing, a detached
  window open) must keep every window working and apply the new runtime only after a restart;
  daemon package scriptlets must not interrupt `smd` during operation.
- **Route classification has room for a daemon submitter:** besides view-only and operating
  clients, ADR 0014's daemon-to-daemon QSO submission is a third kind of caller with its own
  credential (ADR 0014's bearer-token preparation), so the operating-client check and view-only
  enforcement must not foreclose it; that credential grants QSO submission only, never rig or TX
  control or operating presence (ruled 2026-11-09, on codex-034's recommendation). How a
  headless central daemon is administered (the shell installed there, later authenticated remote
  administration, or local-only administration) is decided when the topology is built.
- **The shell authenticates `smd`, its content and every later connection.** While `smd` is
  down (the waiting page), another process able to reach its TCP port can listen there first.
  Checking a listener once is not enough: a later connection, a daemon restart or a reconnect
  could reach a replacement, and a counterfeit UI at the expected origin could call the host
  interface without ever learning the secret. So the shell executes no daemon-served UI and
  grants no host capability until the content is authenticated as `smd`'s, and authentication
  is bound to the transport or session that carries the subsequent requests, streams and
  reconnects. **Acceptance:** an impostor before startup or after daemon loss supplies no
  executable UI, receives no operating credential and gains no privileged host call; the local
  waiting page stays independent of the daemon. The mechanism is left open for its own review: a
  version response or an origin string is not authentication. A Unix socket in an owner-private
  directory is viable only as a main-process transport with an explicit adapter covering UI
  assets, requests, downloads, event streams (WebSocket, EventSource), reconnect and FT8
  presence (the SPA uses browser `fetch`, `WebSocket` and `EventSource` today); it must not be
  fronted by an unauthenticated TCP proxy that recreates the gap.
- **The credential never reaches page script or the network.** It lives in an owner-only file;
  the shell's main process attaches it only to requests for `smd`'s origin, across redirects and
  every supported request and stream path, so an injected script in the UI cannot read it; it is
  never sent over plain HTTP to another machine (operating stays local). **Residual:** injected
  same-origin script can still issue requests that carry the injected credential; the sandbox
  and CSP do not stop an authorised renderer from misusing the API.
- **The host interface stays minimal and checks its caller.** It offers windows and the native
  dialogs (a Save writes only the file the operator chose), checks every call's actual sender
  frame (approved windows and frames, authenticated origin) and its arguments, constrains
  navigation and window creation, and gives the page no general file, process or shell access.
- **Electron permissions are denied by default.** Electron approves permission requests
  automatically without a handler; every session sets both `setPermissionRequestHandler` and
  `setPermissionCheckHandler`, granting only identified needs (no camera, microphone, device
  access or notifications by accident).
- **The daemon package stands alone:** it installs, runs and upgrades without the shell; only
  the shell declares the dependency and the compatibility contract.
- **The shell ships Electron's codec-free FFmpeg** if any distribution requires it (Electron
  publishes one per release; the POC passed with it substituted).

## Alternatives considered

### Stay in the browser

No shell to build or update, and remote use stays natural. Rejected because the operator's
requirement is window control, which a browser cannot provide (tabs, routes, reserved keys,
reload), and because supported browsers offer no consistent native file-dialog API. The SPA
stays browser-compatible, and a browser remains the view-only client.

### Wails v3

An 11.6 MB binary using the distribution's WebKitGTK, with similar measured memory (311 MiB
against Electron's 303 MiB). Not chosen now: only its rendering was evaluated (its window roles,
lifecycle and recovery would need the full acceptance work), it depends on each distribution's
WebKitGTK version (not installed on the operator's machine; 136 MB when added), and Wails v3 has
been stable only since 2026-10-26. Its appearance problem is not a reason to reject it: app-drawn
controls fix it. It keeps an installation-size advantage.

### Tauri

Not evaluated in the POC. It shares WebKitGTK on Linux with Wails, with the same variance; W-0026
records its window and global-shortcut support from documentation only.

### Docking inside the main window

Built and evaluated (POC A16): dragging between dock positions worked by hand, but the operator
found docked panels "just seem to take up real estate", they do not fit a 1400 px window as three
columns, and each docked panel stacked three headers. The operator ruled "record and stop": the
experiment stays parked as evidence, its known limits unfixed and needing no further work.
Floating panels plus detached windows replace it.

## Consequences

- **Security updates are Station Manager's to ship.** Electron bundles Chromium and Node; it
  publishes a major every 8 weeks and supports the latest three stable majors, and only the
  latest minor in each. The release routine gains dependency monitoring, a scenario-gated CI, the
  7-day security target and the support check. Recommended for that check (codex-030, recorded
  in W-0026; not separately ruled): check the minor as well as the major, refresh the dates
  (Electron marks future dates tentative), and report a failed lookup as unknown.
- **A new package and a repository to run:** a signed shell package beside `smd` with a
  compatibility contract, and our own dnf repository (signing key custody and rotation, a release
  RPM carrying the `.repo` file and key, HTTPS hosting, `updateinfo` if security-only updates are
  wanted). Today releases are manual RPM installs.
- **Size:** the POC's packaged Electron app is 297 MB (92 MB as an `xz` archive); these are
  size-only packaging figures, not a production RPM or its dependency closure, which are not yet
  measured. **Memory** (PSS of the whole process tree, software rendering in a virtual display):
  about 303 MiB with one window and 378 MiB with Map, Manual and a detached Session. Firefox, as
  the whole browser with one window in a fresh profile, used 554-727 MiB across runs.
- **Daemon work:** the operating-client check, the view-only route classification, the
  presence/subscription rule, idle-only reload with asset compatibility, and the stronger close
  rule's stop path.
- **The shell must keep its privileged surface narrow:** windows, dialogs, shortcuts and the
  waiting page, nothing operating; sandboxed renderers limited to `smd`'s origin (the POC's
  request filter), with a Content Security Policy still to add.
- **Production hardening still to do** (the HTTP API's own exposure is unchanged by the host:
  `smd` binds loopback by default, refuses other binds without `server.allow_insecure_network`,
  checks Host against DNS rebinding and Origin against cross-site requests,
  `internal/api/csrf.go`):
  - untrusted text (callsign lookups, FT8 decodes, ADIF imports) stays escaped (no `{@html}` in
    `frontend/app/src` today, verified by search, not a complete XSS audit), with the Content
    Security Policy above; the renderer sandbox and the 7-day security target
    limit the damage of a Chromium flaw;
  - external links open only for `https:`, never `file:` or other scheme handlers;
  - Electron's fuses turn off running the binary as Node (`RunAsNode`), `NODE_OPTIONS` and the
    Node inspector arguments, each a separate switch; no remote-debugging port or developer
    tools ship (disabling developer tools alone does not cover the main-process inspector);
  - Electron documents embedded ASAR integrity for macOS and Windows only, and it would cover the
    shell's archive, not the UI `smd` serves. On Linux the RPM relies on signed distribution and
    root-owned installed files, without claiming runtime verification.
- **The operating-client check closes a gap that exists today:** on the default TCP listener
  (`127.0.0.1:8080`), requests without an Origin (curl, scripts) are allowed and no user is
  authenticated, so any process able to reach that listener (other unprivileged users sharing
  the network namespace, unless OS policy isolates them; loopback is not a per-user boundary)
  can operate the station now. The Unix-socket mode already requires an owner-private directory
  and a 0600 socket. WebSocket upgrades require an exact Origin, which a native client can
  supply, so that is not authentication either. The check stops other users, not the same
  user's processes (its accepted limit); root is outside any per-user guarantee.
- **ADR 0100 remains necessary:** the shell removes the states browser tabs create, not the need
  for daemon-owned sessions.

## Triggers to revisit

- A hard requirement for a small install or no bundled runtime: re-evaluate Wails v3, this time
  for window roles, lifecycle and recovery, not rendering alone.
- Electron's update cadence proves too heavy in practice (missed 7-day targets, or releases
  blocked on an unsupported line).
- Native Wayland gains reliable window placement and raising for Electron: reconsider the
  XWayland default.
- Remote operation becomes a requirement: revisit view-only access, transport security and the
  operating-client check.
- The Phone/CW logging panel decision (deferred) is taken.

## References

- [W-0026](../work/W-0026-ui-host-evaluation.md): the evaluation, POC results, both host
  recommendations, the operator's rulings and the requirements above.
- [ADR 0100](0100-daemon-owned-operating-sessions-with-explicit-membership.md) (operating
  sessions), [ADR 0099](0099-spa-event-stream-connection-budget.md) (event streams),
  [ADR 0058](0058-retire-operate-tile-layout.md) (the ambient overlay that floating panels
  extend), [ADR 0036](0036-operator-manual-embedded-zero-js-site.md) (the manual).
- POC repository `sm-electron-poc` (local): stage commits `3189553` to `d879f5b`, final acceptance
  `c91f501`; its README records every scenario and reversion run.
- [Electron release timelines](https://www.electronjs.org/docs/latest/tutorial/electron-timelines),
  [Electron security](https://www.electronjs.org/docs/latest/tutorial/security).
