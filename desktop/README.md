# Station Manager desktop shell

The Electron desktop host decided in
[ADR 0102](../docs/decisions/0102-electron-desktop-host-for-the-ui.md), built slice by slice in
[W-0027](../docs/work/W-0027-electron-desktop-host.md). **Development only: not released.**

`smd` keeps every operating decision and serves the UI. The shell owns only windows, dialogs,
shortcuts and the local "waiting for the daemon" page. The SPA stays in `frontend/app`; the
shell never keeps a copy of it.

## What the shell runs

`npm start` opens the local `smd`'s UI over `smd`'s HTTPS app listener (`server.app_port`,
default 8443), trusting exactly one certificate: `smd`'s own identity (ADR 0103; W-0027 C1d).
The shell finds it through `smd`'s `config.json`: an explicit `--sm-config <absolute path>`,
else `$SM_WORKING_DIR/config.json`, else `$XDG_DATA_HOME/station-manager/config.json`. It reads
only `data_dir` and `server.app_port` from it, then checks
`<data_dir>/desktop-identity/cert.pem`: owner, modes and every parent directory. It never opens
`key.pem`, and never falls back to plain HTTP or another port.

Until `smd` has made its identity, or while it is refused or `smd` is down, the shell shows its
own waiting page. A refused identity is named on that page with its path and the reason. The
certificate is fixed for the app's lifetime; a new identity takes effect at the next launch.
`--sm-data-dir <absolute path>` gives the data root when `data_dir` is relative.

While the daemon may simply not be running, the waiting page says how to start it
(`smctl start`); a refused identity gives its own explanation instead (operator ruling Q3).

The shell writes one line to stderr, prefixed `station-manager-desktop:`, when its state changes:
the discovery's `waiting:` or `refused:` reason and path, `pinned <origin>`, each finished page
load (`loaded waiting page renderer <pid>` or `loaded app …`), a main-frame `load failed`, and a
`renderer gone`. It carries paths, origins and reasons only, never config or page contents.

The shell does not change how the browser works: until C2, the browser on 8080 still operates
the station, and the desktop app is one more client of the same `smd`.

## Installed and development runs (D1)

The shell decides first whether it is an installed app (`src/mode.mjs`, from Electron's
`app.isPackaged`, which is true when the runtime binary is renamed `station-manager-desktop`). An
installed app ignores `SM_DESKTOP_SCENARIO`, `SM_DESKTOP_USERDATA` and the fake-media switch, and
always takes the interactive path. The fixture harness and the test scenarios are imported only
on the development path and are not shipped: ESLint refuses any `src/` import other than
`electron`, `node:` built-ins and `./` siblings (`test/imports.test.mjs`).

## The window and session rules (A1)

The automated A1 and C1 checks use the built-in fixture harness (`harness/server.mjs`), never
`smd`. What A1 enforces:

- renderers are sandboxed and context-isolated, with no Node and no `<webview>`;
- requests reach only the served origin (others are cancelled), and a page can neither navigate
  away nor open windows;
- an external address goes to the default browser only when it is `https:`;
- every Electron permission is denied by default (both handlers, and device access);
- the main window's minimum size is 1024x640;
- there is no application menu;
- the window carries Station Manager's icon (`assets/icon.png`, the SPA's `logo.svg` rendered to
  256x256); launcher and native-Wayland icons come with the package (D1);
- the shell's own waiting page is shown while the origin is down, and again after a failed
  load, until the origin answers;
- the shell's availability probe (`GET /v1/version` through the pinned session) refuses
  redirects;
- a single instance runs: a second launch exits and raises the first;
- no spellchecker (Electron would download its dictionaries from Google's CDN).

## The C1 proof: a pinned TLS identity (fixtures)

`npm run check:c1` runs the shell against two TLS fixture listeners taking turns at one loopback
address: a genuine one whose synthetic certificate the shell pins (`src/pin.mjs`, ADR 0103) and
an impostor with another key. Every transport (page, `fetch`, `wss:`, EventSource, download, the
probe) is shown to work against the genuine one and to be refused by the impostor. The identities
are generated per run with `openssl`; nothing touches `smd`.

## Commands

```sh
npm ci                                  # install (Electron fetches its binary on first run)
npm start                               # interactive: the local smd (see above)
npm start -- --sm-config /abs/path/config.json   # another install (an absolute path)
npm run check:a1                        # A1 acceptance in Electron (xvfb-run -a without a display)
npm run check:c1                        # C1 proof: the pinned TLS identity (needs openssl)
npm run check:c1d-shell                 # C1d wiring: discovery, pin and waiting page, TLS fixtures
SM_DESKTOP_SMD=../build/bin/smd npm run check:c1d   # C1d acceptance against a real, already-built smd
npm run test:unit                       # discovery, connection, mode, trace and page logic, no Electron
npm run lint && npm run format:check
```

From the repository root, `task desktop:check` runs lint, format, the unit tests and the A1, C1
and C1d-shell scenarios. `task desktop:check:c1d` builds the static `smd` (rebuilding the SPA)
and runs the real-`smd` acceptance; to reuse a binary already built, run `check:c1d` directly as
above. CI runs both, the latter after its SPA and daemon builds. Every check uses a temporary
working directory, synthetic identities and free loopback ports; none touches the station.
