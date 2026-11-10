# Station Manager desktop shell

The Electron desktop host decided in
[ADR 0102](../docs/decisions/0102-electron-desktop-host-for-the-ui.md), built slice by slice in
[W-0027](../docs/work/W-0027-electron-desktop-host.md). **Development only: not released.**

`smd` keeps every operating decision and serves the UI. The shell owns only windows, dialogs,
shortcuts and the local "waiting for the daemon" page. The SPA stays in `frontend/app`; the
shell never keeps a copy of it.

## Slice A1: fixture harness only

The shell runs **no daemon-served UI** until C1's authentication is implemented and validated
(ADR 0102, Requirements). Until then it loads only the built-in fixture harness
(`harness/server.mjs`): a stand-in page on a port the OS chooses. The harness never connects to
`smd`, the rig or the network, and there is deliberately no setting that points the shell at
`smd`.

What A1 enforces:

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
- the shell's availability probe refuses redirects; it says only that something answers, never
  that it is `smd` (that is C1);
- a single instance runs: a second launch exits and raises the first;
- no spellchecker (Electron would download its dictionaries from Google's CDN).

## The C1 proof: a pinned TLS identity, still fixture-only

`npm run check:c1` runs the shell against two TLS fixture listeners taking turns at one loopback
address: a genuine one whose synthetic certificate the shell pins (`src/pin.mjs`, ADR 0103) and
an impostor with another key. Every transport (page, `fetch`, `wss:`, EventSource, download, the
probe) is shown to work against the genuine one and to be refused by the impostor. The identities
are generated per run with `openssl`; nothing touches `smd`.

## Commands

```sh
npm ci                                  # install (Electron fetches its binary on first run)
npm start                               # interactive: the fixture page
SM_DESKTOP_HARNESS_DELAY_MS=5000 npm start   # the waiting page for 5 s, then the page
npm run check:a1                        # A1 acceptance in Electron (xvfb-run -a without a display)
npm run check:c1                        # C1 proof: the pinned TLS identity (needs openssl)
npm run lint && npm run format:check
```

From the repository root, `task desktop:check` runs lint, format and the A1 and C1 scenarios, as
CI does.
