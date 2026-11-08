# W-0026 — UI host evaluation: browser or desktop application

**Status:** Open — investigation only. No priority, no implementation and no migration decision.
**Opened:** 2026-11-07. The operator: "I am still thinking if there is an alternative UI framework -
what about using Electron? Keeping the daemon, but using Electron to be the frontend?"; "With
another technology, we can control how many windows are open and what they contain - we cannot
do this with a Browser."; "once SM gets out into the wild - it's whole different ball game!"
**Outcome:** The operator's required application experience is written down, scenario by scenario,
and each candidate host (browser, Electron, Tauri, Wails) is compared on the application code,
recovery rules and maintenance it needs to deliver that experience. If the requirements and
trade-offs support a choice, a UI-host ADR follows. "Technically possible in a browser" is not, on
its own, an acceptance criterion.

`W-0026` is an immutable identity. Its status may change, while priority and ranked position live
only in [`docs/backlog.md`](../backlog.md).

**Not in scope:** ADR 0100 (operating sessions) is unchanged by this dossier. `smd` stays the one
authority for sessions, membership, contest mode and every operating control in every option; a
desktop shell may coordinate windows but never owns operating state.

**How to read the scenarios:** each gives the current behaviour with repository evidence, a
**recommended** operator-visible behaviour (agent recommendation, not a ruling), the hosts' support
with sources, what it costs in application code, recovery rules and maintenance, and the decision
needed from the operator. Platform facts were fetched on 2026-11-07; anything not confirmed is
marked UNKNOWN.

## Sources

Platform facts were fetched on 2026-11-07. Each claim below links its source beside it. Wails
documentation is linked as source pinned to release tags, because wails.io refused automated
fetches (HTTP 403): v3 at [`v3.1.0`](https://github.com/wailsapp/wails/releases/tag/v3.1.0)
(v3 stable since [`v3.0.0`](https://github.com/wailsapp/wails/releases/tag/v3.0.0), 2026-10-26),
v2 at [`v2.17.1`](https://github.com/wailsapp/wails/releases/tag/v2.17.1). The quoted Wails
passages were checked at those tags.

## 1. Remote access (decides much of the rest)

- **Today:**
  - `smd` binds `127.0.0.1:8080` by default (`internal/config/config.go:1352-1356`); a Unix
    socket is also supported (`config.md` §5.2).
  - A non-loopback bind is refused unless `server.allow_insecure_network` is set in the file; with
    it, the daemon starts with a standing warning and **adds no authentication**
    (`config.md` §5.1: "The API has no network authentication or transport encryption").
  - The only gate is `requireSameOrigin` (`internal/api/csrf.go`): a Host allowlist on every
    request and an Origin check on unsafe methods. **A request with no Origin header (curl, a
    script, the CLI) is allowed** (`csrf.go:20-28`). So any program on the station machine, and on
    the network when the acknowledgement is set, can read and write the log, change config,
    restart the daemon, command the rig, key TX and run FT8 (`config.go:599-601`).
- **The current trust model, recorded as such** (not judged acceptable merely because there is one
  operator): no network authentication and no transport encryption; a non-loopback bind needs the
  explicit, file-only `allow_insecure_network` acknowledgement (`config.md` §5.1); Host validation
  and the Origin check on unsafe methods protect against the specified browser-origin attacks
  (DNS rebinding, cross-site requests; `internal/api/csrf.go`). Requests without an Origin header
  are permitted, so **these checks do not authenticate native clients**: any local process, and any
  networked client when the acknowledgement is set, is fully trusted.
- **Why it matters: two separate questions.**
  - **Must remote operation remain possible?** It does not require a browser: a desktop client on
    another machine could connect to a remote `smd` (with §1's transport and authorization
    questions answered for the network).
  - **Must remote operation work in a browser?** If yes, a browser stays a first-class operating
    client alongside any desktop host. For that client the application cannot control how many
    windows exist or what they show (§2), and the browser-window combinations, with most of
    ADR 0100's several-window handling, stay.
- **Recommended:** full operating control only from the station's own desktop application.
  Other clients (another machine, a plain browser on the station) are **view-only**, or disabled
  unless enabled in `config.json`.
- **Window ownership and API authorization are separate controls.** Wrapping the existing HTTP
  client in a desktop shell leaves the daemon's access policy exactly as it is; a rule that one
  window holds operating control does not establish that a request came from an approved
  application. Authorization is a **daemon** change in every option, browser included.
- **Cost of authorization:** a daemon-side **operating-client check**, for example a per-install
  secret the client must present, with operating routes refusing requests without it. One
  middleware with tests, a secret lifecycle (creation, rotation), and a rule for which routes count
  as "operating". **Its limit must be stated:** a secret in a `0600` file keeps out other users
  and other machines, but **not other processes of the same user**, which can read the file. What
  it can and cannot keep out is part of the decision.
- **Decisions needed:** (1) must another machine retain full operating control, view-only access,
  or no access? (2) if any remote operation remains, must it work **in a browser**, or may it
  require the desktop client? Only a "yes" to the browser keeps the browser-window combinations.

## 2. Controlling the windows (the operator's requirement)

- **Today:** the SPA opens the Map, the manual and QRZ lookups as new browser tabs via
  `target="_blank"` links (`ui/Sidebar.svelte:130-136,172-174`; `operate/LoggingCard.svelte:696-699`);
  it never calls `window.open`. The operator can open any number of tabs on any route
  (`router.svelte.ts:33-70`). Cross-tab coordination is limited to a theme `storage` listener
  (`ui/state.svelte.ts:40-50`); `BroadcastChannel` and Web Locks are forbidden by test
  (`utils/noBrowserStorage.test.ts`). The daemon caps event subscribers at 16
  (`MaxEventSubscribers`, `server.go:421`).
- **Recommended:** the application owns a **fixed window set**: one **main window** (Operate, with
  Phone, FT8 and FT4; Logbook; Events; Settings, as views inside it), one **Map window** (for a
  second monitor) and one **Manual window**. Each exists at most once; asking for it again focuses
  the existing one. Window positions and sizes are restored at the next start.
- **Hosts:**
  - **Browser:** not possible; the user opens tabs.
  - **Electron:** several `BrowserWindow`s supported; no built-in position persistence (store
    `getNormalBounds()` and reapply; [BrowserWindow](https://www.electronjs.org/docs/latest/api/browser-window)).
  - **Tauri v2:** windows created from code (`core:window:allow-create`;
    [window API](https://v2.tauri.app/reference/javascript/api/namespacewindow/)); the
    [window-state plugin](https://v2.tauri.app/plugin/window-state/) saves and restores positions and sizes (Linux
    supported).
  - **Wails v2:** effectively one window (the v3 migration guide: v2's single `wails.Run()` "made
    it difficult to create multiple windows"; [v2-to-v3.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/migration/v2-to-v3.md)).
    **Excluded by this requirement.**
  - **Wails v3:** native multiple windows ([windows/multiple.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/features/windows/multiple.md));
    positions saved by application code.
- **Cost:** window-set logic in the shell. This is **window coordination only**: it removes the
  states the operator creates by opening tabs, not other clients. Without §1's authorization a
  browser can still be pointed at `127.0.0.1:8080`, so "one operating window" holds only for
  clients that follow it.
- **What controlled windows remove, and what they do not:**
  - **Removed:** user-opened tabs and duplicate windows, arbitrary routes in arbitrary windows,
    browser navigation and history, a tab left open across an upgrade.
  - **Still to define in every host:** a crash of the shell or a window; a lost connection to
    `smd`; a daemon restart; an upgrade while windows are open; an older installed application
    release talking to a newer `smd` (or the reverse); and other clients unless §1 authorizes.
- **Decision needed:** confirm the window set (main, Map, Manual), and whether any other view
  needs its own window.

## 3. Four distinct actions: what triggers what

Closing a window, stopping an FT8 run, ending contest mode and closing an operating session are
**different actions**. Today one of them is coupled to another; the recommended rules are:

| Action | Today | Recommended consequence |
|---|---|---|
| **Close the main window** (or quit) | Closes **that tab's** FT8 stream only. The run stops only when the **last** `/v1/ft8/events` subscriber, from any tab or client, has been gone for 5 s: the daemon then disarms TX and abandons any QSO, reason `unattended` (`internal/ft8/service.go:448-518`; `doc.go:36-41`). **Closing one window while another stays subscribed stops nothing.** Contest mode (`localStorage` `sm.contest`) is unaffected; the per-tab session list is lost with the tab | **Proposed, a stronger rule than today's:** closing the main window **stops the FT8 run**. **Does not** end contest mode or close the operating session (both daemon state). A confirmation is shown first when it would stop something (§4). See the note below for other connected clients |
| **Close the Map or Manual window** | Closes a tab; no effect on operating | No effect on operating; no confirmation |
| **Stop the FT8 run** (Abandon or Stop) | Ends the run only | Ends the run only; the window, contest mode and session continue |
| **End contest mode** (Stop or its window's end) | Per browser in `localStorage` today; daemon-held in ADR 0101 (Proposed) | Ends contest mode only; under ADR 0100 P2 an **overdue** session then closes, at its last activity. It never stops an FT8 run or closes a window |
| **Close the operating session** (New, gap) | Per tab today; daemon-owned in ADR 0100 | Closes the session only; it never stops a run, ends contest mode or closes a window (ADR 0100 §9) |

- **The proposed rule with another client still connected.** Today any subscription counts as
  presence, so another tab or client keeps the run alive. The proposal: **only the operating
  client's FT8 subscription counts as presence**; view-only clients (§1) never keep a run alive, so
  closing the main window stops the run whatever else is connected. This needs the daemon to tell
  the operating client's subscription from others, which is §1's client authorization. **Without
  it, the daemon cannot make that distinction, and today's rule (last subscriber, after 5 s)
  applies.**
- **Decision needed:** adopt the stronger rule (closing the main window stops the FT8 run, and
  only the operating client counts as presence), or keep today's last-subscriber rule; either way,
  closing the main window does nothing else.

## 4. Closing a window

- **Today:** one `beforeunload` listener, in Settings for unsaved edits
  (`config/unsaved.ts:199-218`). Nothing else prompts: an FT8 run stops as above, and an unsaved
  Phone/CW draft is lost silently (in memory only; ADR 0087 removed saved drafts;
  `archives.svelte.ts:116-117`).
- **Recommended:** closing the main window asks **only when it would stop something**, and says
  what: "An FT8 run is active: closing stops the run and disarms TX", "A Phone contact is not
  logged: closing discards it". Cancel keeps the window. Otherwise it closes without a prompt.
- **Hosts:**
  - **Browser: partial.** `beforeunload` shows only "a generic browser-specified string" that
    "cannot be controlled by the webpage code", and only if the page has had a user gesture
    ([MDN, beforeunload](https://developer.mozilla.org/en-US/docs/Web/API/Window/beforeunload_event)).
    The same prompt covers close, navigation and reload.
  - **Electron:** the `close` event; "Calling `event.preventDefault()` will cancel the close"
    ([BrowserWindow, event close](https://www.electronjs.org/docs/latest/api/browser-window#event-close)).
  - **Tauri v2:** `onCloseRequested`; `preventDefault()` "Must be called before the handler
    returns… otherwise the window is destroyed as usual"
    ([window API](https://v2.tauri.app/reference/javascript/api/namespacewindow/)).
  - **Wails v2:** `OnBeforeClose` returning true keeps the window ("good for confirming with the
    user"; [options.mdx](https://github.com/wailsapp/wails/blob/v2.17.1/website/docs/reference/options.mdx)). **Wails v3:** a `WindowClosing` hook with
    `e.Cancel()` ([windows/events.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/features/windows/events.md)).
- **Cost:** the shell's close handler asks the page what would stop (FT8 state from the daemon,
  draft state from the page), shows one dialog, and closes or cancels. Small in every desktop host;
  in the browser the wording cannot be specific.
- **Decision needed:** confirm which conditions prompt (FT8 run active or TX armed; a contact in
  progress; unsaved Settings).

## 5. Reloading

- **Today:** any tab can be reloaded (F5, Ctrl+R). The SPA reloads itself only through
  `reloadPage()` (`utils/reload.ts:15-17`, `location.replace`, the W-0024 F6 Firefox workaround),
  after a Settings restart or an archive or default change. A reload loses the Phone/CW draft and
  the callsign stack; `sessionStorage` (session list, FT8 engagement, session timer) and
  `localStorage` (theme, contest, rig context, FT8 offset and CQ modifier, map settings) survive.
- **Recommended:** in a desktop host, **no user reload**: F5 and Ctrl+R do nothing. The
  application reloads a window itself only when it must (§7, an archive or default change), and
  asks first if that would discard a contact in progress.
- **Hosts:** **Browser:** cannot prevent it; the generic `beforeunload` prompt at most.
  **Electron:** catch the keys with `before-input-event`
  ([Keyboard Shortcuts](https://www.electronjs.org/docs/latest/tutorial/keyboard-shortcuts)) and remove the default menu
  (`Menu.setApplicationMenu(null)`; [Menu](https://www.electronjs.org/docs/latest/api/menu)); whether the default menu binds reload
  is UNKNOWN. **Tauri v2:** no built-in switch (maintainers: "disable/overwrite everything
  manually"; [discussion 8019](https://github.com/tauri-apps/tauri/discussions/8019)); a
  third-party plugin exists. **Wails:** UNKNOWN.
- **Decision needed:** confirm "no user reload".

## 6. The daemon restarts while windows are open

- **Today:** the shell and rig WebSockets retry every 1 s (`api/socket-reviving.ts:43`); the FT8
  EventSource reconnects, re-claiming when needed (`api/sse-reviving.ts`;
  `operate/ft8.svelte.ts:1098-1118`). There is **no global "daemon unavailable" state**: the CAT
  pill shows "lost", FT8 shows "Waiting for slot…", the Map "offline", and individual requests
  "Cannot reach the daemon." A fresh load while `smd` is down gets the browser's error page,
  because `smd` serves the SPA.
- **Recommended:** one application-wide state, "Station Manager daemon unavailable —
  reconnecting", replacing per-panel symptoms; operating controls disabled until it returns; on
  return, windows refresh their state. A desktop host shows this even when started while `smd` is
  down (a local page), instead of an error page.
- **Hosts:** all desktop hosts can show a local page; the browser cannot (the page itself comes
  from `smd`). The application-wide state itself is SPA work and helps the browser too.
- **Design point:** the desktop shell should still **load the UI from `smd`**, so the UI and the
  daemon are always the same version; the shell carries only its waiting page.
- **Decision needed:** confirm the single unavailable state, and that the UI stays served by `smd`.

## 7. Upgrading

- **Today:** `/v1/version` is fetched at boot and on a rig-stream reconnect only
  (`ui/buildIdentity.svelte.ts`); **nothing compares builds**. After an upgrade an open tab runs old
  code while its badge shows the new version, and a lazy chunk then receives the SPA fallback page,
  leaving Settings blank (W-0012, alpha.5 finding #4; `spa.go:63-71`). The archive and default
  request guards (`X-SM-Archive-Id`, `X-SM-Default-Logbook`) protect identity, not the build.
- **Recommended:** on reconnect, a window that sees a different daemon build reloads itself,
  asking first if a contact is in progress. This is needed in **every** host, including the
  browser, and is independent of this evaluation. The desktop shell itself is upgraded by the
  package manager with the RPM.
- **Hosts (shell updates):** Electron has "no built-in support for auto-updater on Linux" and
  recommends the package manager ([autoUpdater](https://www.electronjs.org/docs/latest/api/auto-updater)); Tauri's updater supports RPM
  since 2.10.0 ([updater 2.10.0](https://v2.tauri.app/release/updater/v2.10.0/)); Wails v3 updates a binary or
  `.tar.gz`, RPM not listed ([updater.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/guides/updater.md)). With RPM distribution none is
  needed.
- **Decision needed:** confirm "reload on a changed build" as a fix to make regardless of host.

## 8. Keyboard shortcuts

- **Today:** two window-level families (`docs/keyboard-shortcuts.md`): the rig keys
  `Ctrl+Shift+…` (`operate/RigKeys.svelte`) and the Phone/CW logging keys. Known conflicts: Firefox's
  `Ctrl+Shift+PageUp/Down` "won't yield to preventDefault" (`RigKeys.svelte:13`), so page keys are
  avoided; `Ctrl+Alt+↑/↓` may be taken by the window manager; F3 is pre-empted from Find Next.
- **Recommended:** shortcuts act only while a Station Manager window has focus (no global
  shortcuts), with no browser conflicts.
- **Hosts:** **Browser:** `Ctrl+T`, `Ctrl+N` and `Ctrl+W` cannot be prevented
  ([Mozilla bug 1490665](https://bugzilla.mozilla.org/show_bug.cgi?id=1490665)); Keyboard Lock is
  Chromium-only ([MDN, Keyboard.lock](https://developer.mozilla.org/en-US/docs/Web/API/Keyboard/lock)).
  **Desktop hosts:** menu accelerators or input events within the window. Global shortcuts on
  Wayland go through a portal with a user prompt (Electron [globalShortcut](https://www.electronjs.org/docs/latest/api/global-shortcut);
  Wails v3 [global-shortcuts.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/features/keyboard/global-shortcuts.md)); Tauri's
  [global-shortcut plugin](https://v2.tauri.app/plugin/global-shortcut/) has an open Wayland issue
  ([global-hotkey#28](https://github.com/tauri-apps/global-hotkey/issues/28)). Not needed if
  shortcuts stay window-local.
- **Decision needed:** confirm window-local shortcuts only.
- **Shortcuts across windows (operator, 2026-11-08).** The proof of concept found that the rig
  keys are mounted with the main window's workspace, so they do nothing in a detached Rig Control
  window.
  - **Direction:** the rig keys must work in the main window **and** the Rig Control window at a
    minimum; a standard set that works in all windows is "probably a good move".
  - **Ruling:** keys that belong to one window (for example the logging card's Escape = clear
    draft and Ctrl+Enter = log) are bound **only** in that window: elsewhere they are unbound and
    never invoke a different action. **Dialogs are the exception:** inside an open dialog, Escape
    and Ctrl+Enter act on that dialog, in any window.
  - **Standard set (Claude proposal, Codex supported; built in the POC on the operator's
    direction):** the keys acting on shared, daemon-owned state, starting with the rig family,
    work with the same meaning in every Station Manager window (main, Rig Control, Session),
    defined once, one handler per window; they stand down under a dialog and during a daemon
    outage. A key that raises the main window and focuses its callsign is **deferred**.
  - **Ruling, typing:** the rig keys do **nothing while typing in an ordinary text field**
    (search, comment, name…); the Callsign field keeps its deliberate exception (search-and-pounce
    tuning). This changes today's product, where band, swap and digit keys fire regardless of
    focus (`RigKeys.svelte`).
  - **Ruling, rig state without CAT (option b):** the manual rig context (band, frequency,
    confirmation) stays in the main window, which logs from it; off CAT a detached Rig Control
    window is a read-out and the rig keys act only in the main window. Sharing the manual context
    (option a) is the likely product answer and belongs with daemon-owned operating state
    (ADR 0100), not this evaluation.

## 9. Files: export and import

- **Today:** session ADIF export downloads through a `Blob` and an `<a download>` click into the
  browser's download folder (`api/session-export.ts:101-111`), plus a server-side backup copy
  (`handler_session_export.go`). There is no Logbook-wide export, no in-product Cabrillo
  (`scripts/ukeicc-cabrillo.py`), and import is CLI-only (`smd import`, `smctl import`).
- **Recommended:** a native Save dialog for exports (ADIF, later Cabrillo), naming the location,
  and later a native Open dialog for import.
- **Hosts:** **Firefox:** the File System Access API is not supported; Chromium 86+ supports it
  ([MDN, showSaveFilePicker](https://developer.mozilla.org/en-US/docs/Web/API/Window/showSaveFilePicker)).
  **Electron** ([dialog](https://www.electronjs.org/docs/latest/api/dialog)), **Tauri** ([dialog plugin](https://v2.tauri.app/plugin/dialog/)) and
  **Wails** ([v2 dialog.mdx](https://github.com/wailsapp/wails/blob/v2.17.1/website/docs/reference/runtime/dialog.mdx);
  [v3 build/linux.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/guides/build/linux.md)): native dialogs, with Linux portal caveats
  (Electron's `defaultPath` needs portal backend version 4+; Wails v3 GTK4 dialogs go through the
  portal).
- **Decision needed:** whether a native Save dialog matters to you, or the download folder is fine.

## 10. Starting, notifications and tray

- **Today:** `smd` is a systemd user service (`packaging/smd.service`; `systemctl --user enable
  --now smd`, `docs/install.md`), and the operator opens `http://127.0.0.1:8080` by hand. Attention
  is in-page only: toasts, banners and the Events page; no sounds, Notification API or title badge.
- **Recommended:** a desktop launcher entry that opens the application; `smd` stays a systemd
  service and the application never starts or stops it. Desktop notifications and a tray icon are
  **not** in a first version.
- **Hosts:** autostart: Electron's login-item API is macOS and Windows only ([app](https://www.electronjs.org/docs/latest/api/app)),
  so an XDG autostart file would be written by hand; Tauri has an
  [autostart plugin](https://v2.tauri.app/plugin/autostart/); Wails v3 writes an XDG autostart file
  ([autostart/basics.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/features/autostart/basics.md)). Tray on Linux: Electron uses
  StatusNotifierItem ([Tray](https://www.electronjs.org/docs/latest/api/tray)); Tauri's tray click events are not emitted on Linux
  ([system tray](https://v2.tauri.app/learn/system-tray/)); Wails v3's tray "varies by desktop environment"
  ([systray.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/features/menus/systray.md)).
- **Decision needed:** confirm launcher only, no tray or notifications for now.

## 11. Suitability for a public release (cross-cutting)

The operator: complexity "comes with an increased risk of bugs and unexpected behaviour… with a
single install/operator - that's okay… once SM gets out into the wild - it's whole different ball
game!"

**Operator evidence (2026-11-08):** "Previous incarnations used wails v2 - the issue there is
that WebKitGTK does not render the same as Firefox or Chrome." That is first-hand evidence against
the WebKitGTK hosts on Linux (Tauri, Wails v2 and v3). Wails v3 moves to WebKitGTK 6.0; nothing
found shows that this closes the rendering gap, so the risk is treated as unchanged until a test
shows otherwise. Electron renders with Chromium, the engine the SPA is already developed and tested
in.

Compared on four **separate** dimensions; a desktop host may improve the first and reduce some
variation, but its presence alone resolves neither authorization nor update responsibility.

| Dimension | Browser | Electron | Tauri v2 | Wails v3 |
|---|---|---|---|---|
| **Window coordination** | none: the user opens tabs and routes | application-owned windows (several `BrowserWindow`s) | application-owned windows; window-state plugin | application-owned windows |
| **Client authorization** | unchanged: the daemon's policy (§1) | **unchanged** unless the daemon adds §1's check | unchanged unless the daemon adds it; IPC to a remote origin needs a capability, and on Linux Tauri cannot tell an iframe from the window ([Capabilities](https://v2.tauri.app/security/capabilities/)) | unchanged unless the daemon adds it; no localhost guidance found |
| **Deployment variation** | each user's browser and version | the engine is **pinned per application release** ("targets Chromium even-number versions"; [Electron Releases](https://www.electronjs.org/docs/latest/tutorial/electron-timelines)), but users may run **different application releases** | the system WebKitGTK, varying by distribution ("very hard to compile accurate information about WebKitGTK on the various distros"; [Webview versions](https://v2.tauri.app/reference/webview-versions/)) | the system WebKitGTK 6.0 with GTK4 (Fedora 40+); GTK3 build only "until v3.1" ([faq.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/faq.md)) |
| **Update responsibility** | the browser vendor ships engine security fixes | **Station Manager ships them**: a major every 8 weeks, latest three supported ([Electron Releases](https://www.electronjs.org/docs/latest/tutorial/electron-timelines)); the security checklist (context isolation, sandbox, no Node integration for remote content; [Security](https://www.electronjs.org/docs/latest/tutorial/security)) is ours to keep | the distribution ships WebKitGTK fixes; Tauri itself: no published cadence | the distribution ships WebKitGTK fixes; Wails: weekly releases ([release-policy.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/contributing/release-policy.md)), v3 stable since 2026-10-26 ([v3.0.0](https://github.com/wailsapp/wails/releases/tag/v3.0.0)) |
| Packaging | none beyond `smd` | a second package or bundled into the RPM; size UNKNOWN | small (minimal app "less than 600KB"; [Start](https://v2.tauri.app/start/)) | single binary "around 10MB" ([faq.md](https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/faq.md)) |
| Licence | — | MIT ([LICENSE](https://github.com/electron/electron/blob/main/LICENSE)) | MIT or MIT/Apache-2.0 ([README](https://github.com/tauri-apps/tauri)) | MIT ([LICENSE](https://github.com/wailsapp/wails/blob/v3.1.0/LICENSE)) |

Compatibility of these licences with the project's GPL-3.0-only licence is for `docs/licensing.md`
to confirm; they are stated here only.

## Fixes worth making in any host

- Reload on a changed daemon build (§7).
- One application-wide "daemon unavailable" state (§6).
- Client authorization (§1) is independent of the host: it is needed, or explicitly declined, in
  every option, with its limit stated.

## Decisions needed (summary)

1. Remote access: full control, view-only, or none; whether remote operation must work in a
   browser; and, separately, whether to add client authorization, given what it can and cannot
   keep out (§1).
2. The fixed window set (§2).
3. The stronger close rule: closing the main window stops the FT8 run and only the operating
   client counts as presence, or today's last-subscriber rule (§3).
4. Which conditions prompt on close (§4).
5. No user reload (§5).
6. One "daemon unavailable" state; the UI stays served by `smd` (§6).
7. Reload on a changed build, in any host (§7).
8. Window-local shortcuts only (§8).
9. Native Save dialog wanted or not (§9).
10. Launcher only; no tray or notifications for now (§10).

## Unknowns to close before an ADR

- Whether Electron's default menu binds reload; reload handling in Wails.
- Electron package size on Linux.
- WebKitGTK limitations affecting SSE, WebSocket or a localhost origin (none found).
- Whether Tauri global shortcuts work on GNOME Wayland (an open issue suggests not; not needed if
  shortcuts stay window-local).
- Localhost-specific security guidance for Electron and Wails.
- Release cadence and support windows for Tauri v2.

## Proof of concept: agreed design (2026-11-08)

Agreed with the operator ("I would like to digress a little and do a POC for Electron") and two
reviewers. It evaluates the **host and the workspace**, not production session correctness, which
stays ADR 0100's daemon work. Nothing in it is shipped or wired to `smd`.

- **Hosts:** **Electron** for the full evaluation; **Wails v3** for a rendering comparison only.
  The Wails comparison is extended only if its rendering passes and its smaller distribution
  offers enough practical benefit. Tauri and Qt are not included (Qt would mean rebuilding the UI;
  it stays on record as the native-docking option).
- **Real components and styles, not lookalikes.** Representative current Svelte components and
  styles are reused, so the evaluation meets the rendering problems it is meant to find.
  **Narrowly scoped POC adapters** are allowed where the current frontend cannot do what is being
  evaluated, and **every adaptation is recorded** with the result, so the proof of concept never
  appears to claim that today's frontend already supports it. Known adaptation: the Session list.
  `SessionPanel.svelte` reads a frontend store that `session.svelte.ts` persists in per-tab
  `sessionStorage`, so separate windows would still get separate sessions; the proof of concept
  supplies an adapter for that store backed by the fixture server.
- **Backend: a fixture API server** on a dedicated proof-of-concept origin. No proxy to, and no
  fallback to, the live daemon. It answers only the routes the chosen components use, with fixture
  data, and can simulate state changes (for example session changes) and disconnects on demand.
  **Unknown requests fail visibly**, exposing missing fixtures. It never touches the station's
  data, the rig or the transmitter.
- **Window roles:** a finite set, including **Rig Control** and **Session**. Each panel has **one
  active home**: docked in the main window or detached in its own window. A second request reveals
  the existing panel rather than creating another. Both **docking and separate OS windows** are
  tested; the docking mechanism (a layout library or our own) is chosen after these criteria, as
  an implementation choice.
- **Acceptance scenarios (Electron):**
  1. Detach Session, change simulated state, close and reopen it, and recover from a simulated
     disconnect.
  2. Detach and re-dock Rig Control and Session **without losing state**: shared data **and**
     panel-local state (for example Session search text, the selected row and the scroll
     position). Recreating the rows but losing the operator's place counts as a partial success.
  3. Several windows show one shared state; a change in one appears in all.
  4. Closing: the application's own confirmation when something would stop (§4); no user reload
     (§5); the "daemon unavailable" page (§6).
  5. Window positions restored at start, tested under **native Wayland and XWayland**: Electron
     documents that programmatic positioning is unavailable under native Wayland
     ([BrowserWindow, `setPosition`](https://www.electronjs.org/docs/latest/api/browser-window#winsetpositionx-y)),
     so exact restoration is a test, not an assumption.
  6. Window-local shortcuts, including keys a browser reserves (§8), and a native Save dialog (§9).
  7. Measured package size and memory use.
- **Rendering comparison (Electron, Wails v3, and Firefox as today's reference):** the same
  components, fixtures, viewport, theme and scale in each engine; specific differences recorded
  with screenshots. Its purpose is to determine whether the previously observed WebKitGTK problems
  remain.
- **Where it lives:** a separate directory outside the product build, so Electron, Wails and their
  tooling never enter `smd`'s build or dependencies. Its results are recorded in this dossier.

## Proof of concept: desktop results (operator's desktop)

- **Second launch (2026-11-08, Electron 44.9.3, Wayland session).** In both runs the second
  launch exits, and the first instance logs that it received the launch and asked to reveal its
  main window. **Native Wayland** (plain `npm start`): the window did **not** come to the front.
  **XWayland** (`--ozone-platform=x11`): the window **came to the front**. The application
  reaches the compositor in both cases; under native Wayland the compositor did not raise the
  window. Why, and whether an activation token passed from the second launch would change that,
  is not established (unverified inference: Wayland compositors restrict focus changes that lack
  an activation token).
- **No user reload (2026-11-08, native Wayland).** F5 and Ctrl+R in the main window and a
  detached Session window: a typed callsign (main) and Session search text (detached) both
  survived, so neither key reloaded. Passes. (The offscreen scenario cannot prove this: synthetic
  keys never reach Electron's reload accelerators.)
- **Close confirmation (2026-11-08, native Wayland).** With the simulated FT8 run on and callsign
  7Q7EB typed, closing the main window showed the application's own native dialog ("Closing
  Station Manager will stop:") listing both: the FT8 run (stops the run, disarms TX) and the
  unlogged contact. **Cancel** kept everything; **Close and stop** closed all windows. Passes.
- **Window positions (2026-11-08).** **Native Wayland:** after a restart the main window kept its
  size but not its position; the re-detached Session window likewise kept its size but not its
  position (as Electron documents: programmatic positioning is unavailable under native Wayland).
  **XWayland:** the main window's size and position were restored after a restart. The Session
  window's size and position were restored on re-detach **within a run**, but after a restart its
  size came back and its position did not. Cause (a POC bug, not the desktop): closing the main
  window destroys the detached windows, and `destroy()` emits no `close` event, so the detached
  window's final placement was never saved (Electron `BrowserWindow.destroy()` documentation).
  Fixed in the POC with an automated check (the saved placement now matches the window's last
  bounds); **desktop re-test 2026-11-08: Session position and size now restored after a restart, both when closed through Close and stop and when closed with nothing to stop (no dialog).**
- **Pattern so far:** under native Wayland the desktop owns window placement and focus (no
  raise on second launch, no position restore); under XWayland both work. Whether a released
  Electron build should run under XWayland, or accept native-Wayland placement, is a decision
  for the operator (not decided here).
- **Open question for the operator (not decided):** the proof of concept starts with every panel
  docked; which panels were detached is not remembered across a restart. Whether Station Manager
  should restore the workspace layout (which panels are detached) at start is a design decision.

## Proof of concept: package size and memory (2026-11-08)

Measured, not judged (method and caveats in the POC README):

- **Size:** Electron packages to 297 MB (92 MB compressed); almost all is Chromium (the app code
  is 0.14 MB). Wails v3 is an 11.6 MB binary (3.6 MB compressed) but depends on the distribution's
  WebKitGTK 6.0, 136 MB installed, which was not installed on the operator's machine before this
  evaluation. A browser adds nothing. The UI is 2 MB in every case.
- **Memory (PSS of the whole process tree in MiB, Operate with Rig and Session; validated runs:
  live host, 1400x900 page, DPR 1, expected panels, each extra window loaded):** Electron 304 MiB
  with the main window, 379 MiB with Map, Manual and a detached Session (+75 MiB for that mix);
  Wails v3 309 MiB with one window; Firefox 554-637 MiB across runs with one window in a fresh
  profile (the whole browser). Software rendering in a virtual display; a desktop measurement
  would refine the GPU share. (A first, unvalidated run reported 306/391/310/621; superseded.)

## Proof of concept: Map and Manual windows (2026-11-08)

- **Built in the Electron host** (no UI change; §2's recommended fixed window set): the app's
  existing `target="_blank"` links are routed by the host. Map and Manual each open one
  application window; asking again reveals it (a manual section link moves the Manual to that
  section). Other app routes stay with the main window. External links (QRZ lookups, links in
  the manual) open in the default browser, never in an app window. Window sizes are remembered;
  closing the main window closes the set. Evidence in the POC README (real clicks, reversion
  runs); the manual there is a synthetic placeholder for smd's page.
- **Operator ruling (2026-11-08):** external links open in the default browser: "the browser is
  fine. The app does not need to open QRZ.com". (Reasons given: the operator's logins live in the
  browser; app windows are trusted and talk to smd, so remote sites stay out of them.)
- **Desktop check (operator, 2026-11-08): "Map and manual are good."**

## Proof of concept: movable docking (2026-11-08)

- **Operator rulings:** only the panels move (Rig Control, Session); the workspace (the logging
  card, or the FT8 view) stays fixed as the centre. Docked panels may split around it or stack
  as tabs, with resizable dividers. A panel docked again after Detach returns to its previous
  place. Restoring the layout across restarts remains a separate decision.
- **Proposed, not ruled** (reviewer recommendation, built in the POC so it can be seen): if the
  previous place has gone, the panel joins the nearest surviving panel group; failing that, the
  default place.
- **Built** (POC A16, `dockview-core` 8.6.2, MIT, in the POC only): one active home per panel,
  and shared and panel-local state (search text, scroll position) preserved through move,
  resize, tabbing, detach and re-dock; Hide and Show use the same memory as Detach. Automated
  evidence in the POC README; dragging by hand is a desktop check.
- **Finding:** side by side, the panels take width from the logging card, where the product's
  ambient overlay (ADR 0058) covered part of it instead; at 1400 px the card scrolls
  horizontally until the divider is moved.
- **Operator's assessment on the desktop (2026-11-08):** dragging between dock positions works by
  hand; but "The docked windows are not that pretty :-(" and "They don't immediately stand out
  as useful - they just seem to take up real estate...". Observed causes: each docked panel
  stacks three headers (the dock's tab strip, the Detach button, the card's own title and close),
  and a docked panel claims permanent width the overlay only borrowed while open.
- **Finding, width:** at a 1400 px window the logging card, Rig (448 px minimum, its card is
  417 px) and Session (440 px minimum; every table column needs about 704 px) do not fit as three
  columns; panels squeeze or scroll sideways. The POC's docking logic was verified at 1800 px.
- **Operator ruling (2026-11-08): record and stop.** Docking is recorded as evaluated and not worth
  the space it claims; the floating overlay plus detach-to-own-window stays the model (as ADR 0058
  retired the tile board for the same reason). The POC keeps A16 as evidence, off by default.
  Operator's view: "More useful is docked windows as with the browser version and then detached
  - detached being (in my view) more useful." Detached windows are the primary use; this raises
  the weight of the two open window decisions in §8's results (restoring which panels were
  detached, and native Wayland versus XWayland, which decides whether their positions come back).
- **Operator's idea (2026-11-08, not decided):** "having panels resizable and positionable and
  then allow them to be minimise and restored" — floating panels over the workspace (the overlay
  model made movable and resizable), minimised and restored to the same place and size. Notes
  (Claude): positions inside the app's own window are app-controlled, so they come back under
  native Wayland too; it is plain web UI, so it does not depend on the host (a browser could do
  it); the one-home rule extends to floating-in-window or detached, never both.
  Operator's refinement: minimise/restore is the same as closing a panel and opening it again
  from the rail, so it may be pointless; what matters is that a panel can be positioned and
  sized, and opens at its set position (or the default). Notes (Claude): closing discards the
  panel's local state (Session search text and scroll) where a minimise would keep it; the
  shared list itself returns intact. A panel left near an edge must be pulled back inside the
  window when the window shrinks.
- **Built (POC A17, opt-in `?poc-float=on`, 2026-11-08):** Rig Control and Session float over the
  workspace, move by their own title row, resize from edges and corner, come to the front on a
  press, on focus and when a dialog opens in them, return to their last place and size when
  closed and reopened or detached and docked back, and are pulled back inside a shrinking window
  (even below their minimum). Automated evidence with real mouse input in the POC README.
  **Operator's verdict on the desktop:** "Now, this looks a lot more useful..." (contrast:
  docking "just seem[s] to take up real estate").
  **Operator rulings (2026-11-08):** floating panels stay **opt-in** in the POC (not the
  default); a panel's **position and size survive a restart** (built: saved in the app's own
  storage, restored at start and clamped to the window; only the operator's moves and resizes
  are saved, so a temporary pull-back is undone when the window grows). Desktop check (operator,
  2026-11-08): positions and sizes came back after a real application restart. Open: which panels are
  open at start; a minimum window size.
- **Operator's idea (2026-11-08, "just thinking", not decided):** the Phone/CW logging panel
  might also need to be movable and resizable; the FT8 view stays fixed for now, as in the
  browser version. Notes (Claude): the Worked panel sits in the same Phone/CW column and opens on
  Tab beside the logging card, so it would move with it or become its own floating panel; with
  the logging card floating, the Phone/CW workspace beneath becomes empty background, unlike FT8.
- **Scope note:** the Worked panel stays beside the logging card; the product classes it as part
  of the Phone/CW workflow, not a reference panel. Whether it should move too is open.

## Proof of concept: rendering comparison (2026-11-08, first pass)

Operator's description of the earlier Wails v2 problem: "Fonts, borders and the overall look (hard
to describe) look old fashion". Method: the POC's frozen UI and fixtures, four views, 1400x900,
scale 1, light and dark app theme, in Firefox 159 (reference), Electron 44 (Chromium 152), and
Wails v3.1.0 built two ways: GTK4/WebKitGTK 6.0 (the v3 default) and GTK3/WebKit2GTK 4.1 (the API
Wails v2 used); both WebKitGTK 2.54.2 (Fedora 44). Automated captures in a private Xvfb display
with measured computed styles; details and images in the POC (`README.md`, `out/compare-*`).

- **Same CSS and font face everywhere.** Computed styles match; a sample text measures
  184.5-184.6 px in Firefox, Electron and Wails GTK4.
- **WebKitGTK draws native form controls with the GTK theme, not the page's styling.** Selects
  are grey GTK controls with a solid arrow, 37 px tall against 27 (Firefox) and 29 (Electron), which
  also enlarges the Rig panel. In the dark theme the header's contest select is a light-grey
  control with faded text in a dark header, and the Contest checkbox a plain white square. This
  matches "borders … old fashion"; it holds for both Wails builds.
- **Wails GTK3 (WebKit2GTK 4.1, the Wails v2 API) sets text about 2% wider** with whole-pixel
  metrics (188 px for the same sample); GTK4 matches Chromium and Firefox.
- Elements the app styles itself (text inputs, buttons, tables, cards) render the same in all
  four engines.
- **Experiment (POC A15, same day):** with selects and checkboxes drawn by the app
  (`appearance: none`, an app-drawn arrow and box in the app's own colours), they look the same in
  all four engines in both themes: select height 28 px everywhere (27/29/37/37 before), and no
  light-grey select or white checkbox in the dark header. So the form-control gap is closable with
  CSS that would apply to every host, the browser included. Not covered: the open select popup,
  which stays native in every engine (a GTK popup in WebKitGTK).
- **Desktop look, operator's screen (2026-11-08, native Wayland, Wails GTK4, dark theme,
  without A15):** worse than the Xvfb captures. The Rig Mode select is a white control with
  near-white text, unreadable (the desktop's GTK theme draws the box light while the app's dark
  theme sets light text); the header's contest select is a light-grey block with faded text; the
  Contest checkbox a plain white square. Text otherwise renders cleanly at the desktop's scale.
  **With A15 on the same desktop:** the Mode select reads clearly in the app's dark styling at the
  height of the fields beside it, the contest select is dark with a matching outline, and the
  checkbox is the app-drawn box: the experiment fixes all three on the operator's screen.
  The open select list (still native, a GTK popup): "looks okay, but not great" (operator).
  Text in Firefox on the same desktop: "looks the same as Wails" (operator), consistent with the
  measured identical font face; the text itself is not where the old-fashioned look came from.
- **Not yet established:** whether the GTK3 text-width difference matters once the GTK3 build
  is gone (Wails: "until v3.1"); whether the open select list warrants app-drawn dropdowns (a
  product decision: keyboard and accessibility become the app's). Open; nothing here is decided.

## Next step

The proof of concept above, then its results recorded here; the operator's rulings on the
decisions above; only then a UI-host ADR.
