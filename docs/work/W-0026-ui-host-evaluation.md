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

## Next step

The operator rules on the decisions above. Then a short spike, if still warranted: one desktop
shell (the leading candidate after the rulings) loading the existing SPA from `smd` unchanged,
exercising §2 and §4. Only then a UI-host ADR.
