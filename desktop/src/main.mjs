// Station Manager desktop shell (ADR 0102; W-0027). Not yet released (D2-D6).
//
// An installed app (W-0027 D1) decides that first (src/mode.mjs): it ignores every development
// selector and takes only the interactive path. The fixture harness and the test scenarios are
// imported lazily, on the development path only, and are not shipped.
//
// Interactive runs load the local smd's UI over its pinned HTTPS listener (ADR 0103; W-0027
// C1d): the shell finds smd's identity through config.json (src/discovery.mjs), trusts that one
// certificate and nothing else, and shows its own waiting page until smd answers. A1's
// fixture-only restriction lifted with C1d's real-smd acceptance (npm run check:c1d). The
// automated checks keep their fixture harnesses (A1, C1, C1d-shell) or run a temporary smd
// (C1d); none touches the station.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { app, Menu, session, shell } from 'electron';
import { disableSpellchecker, hardenSession } from './hardening.mjs';
import { parseArgs } from './daemon.mjs';
import { launchTrusted } from './launch.mjs';
import { installPin } from './pin.mjs';
import { createMainWindow, externalOpener } from './window.mjs';
import { loadWhenUp, returnToWaitingOnFailure } from './waiting.mjs';
import { startupMode } from './mode.mjs';

// First, before the profile, any switch or the instance lock (W-0027 D1).
const mode = startupMode({ isPackaged: app.isPackaged, env: process.env });
const { scenario, automated } = mode;

if (mode.userData) app.setPath('userData', mode.userData);
else if (automated) app.setPath('userData', fs.mkdtempSync(path.join(os.tmpdir(), 'sm-desktop-')));

if (mode.fakeMedia) app.commandLine.appendSwitch('use-fake-device-for-media-stream');

if (!app.requestSingleInstanceLock()) {
    app.quit();
} else if (scenario === 'second') {
    // A second-launch check that got the lock: single-instance has failed.
    app.exit(3);
} else {
    let mainWindow = null;
    const blocked = [];
    const rendererRequests = [];
    const externalOpened = [];
    const permissionAsks = [];
    const secondInstances = [];
    const pinDecisions = [];
    const downloads = [];

    app.on('second-instance', () => {
        secondInstances.push(Date.now());
        if (!mainWindow) return;
        if (mainWindow.isMinimized()) mainWindow.restore();
        mainWindow.show();
        mainWindow.focus();
    });
    app.on('window-all-closed', () => {
        if (!automated) app.quit();
    });

    app.whenReady()
        .then(async () => {
            // First, before anything else can use the session: no spellchecker, so no
            // dictionary download from Google's CDN (see hardening.mjs).
            disableSpellchecker(session.defaultSession);
            Menu.setApplicationMenu(null);

            // C1d part 2 (W-0027): the shell's wiring to a discovered identity, against a synthetic
            // install and TLS fixtures only. The verifier (empty pin: reject everything) and the
            // hardening (no origin: nothing allowed) are installed before the window exists; the
            // pin and origin are fixed together at the first valid identity. Interactive runs still
            // load only the fixture harness until C1d's real-smd acceptance passes.
            // The interactive app: the local smd, over its pinned listener.
            if (!automated) {
                const launched = launchTrusted({
                    args: parseArgs(process.argv),
                    openExternal: externalOpener((url) => void shell.openExternal(url)),
                });
                mainWindow = launched.win;
                mainWindow.on('closed', () => {
                    mainWindow = null;
                });
                return;
            }

            // C1d part 3: the acceptance against a real smd (SM_DESKTOP_SMD), in a temporary
            // working directory with synthetic data and free loopback ports; never the station.
            if (scenario === 'c1d') {
                const ok = await (await import('../test/scenario-c1d.mjs')).runC1d();
                app.exit(ok ? 0 : 1);
                return;
            }
            if (scenario === 'c1d-shell') {
                const s = await (await import('../test/scenario-c1d-shell.mjs')).setupC1dShell();
                const launched = launchTrusted({
                    env: s.env,
                    offscreen: true,
                    openExternal: externalOpener((url) => externalOpened.push(url)),
                    retryMs: 300,
                    onPermission: (a) => permissionAsks.push(a),
                });
                mainWindow = launched.win;
                const ok = await s.run({
                    getMain: () => mainWindow,
                    pinDecisions: launched.decisions,
                    opened: launched.opened,
                    pin: launched.pin,
                    blocked: launched.blocked,
                });
                await s.cleanup();
                app.exit(ok ? 0 : 1);
                return;
            }
            // The fixture checks start with the "daemon" down to prove the cold start.
            // The C1 proof (ADR 0103) runs a genuine and an impostor TLS harness at one address
            // and pins the genuine one's synthetic certificate; everything else uses A1's plain
            // fixture. Neither is smd.
            let c1 = null;
            if (scenario === 'c1') c1 = await (await import('../test/scenario-c1.mjs')).setupC1();
            const harness = c1
                ? c1.genuine
                : await (await import('../harness/server.mjs')).startHarness({ up: false });
            const origin = harness.origin;
            const appUrl = origin + '/';

            // The pin is installed before the session's first request (ADR 0103 point 3).
            if (c1) {
                installPin(session.defaultSession, {
                    fingerprint: c1.fingerprint,
                    onDecision: (d) => pinDecisions.push(d),
                });
            }

            hardenSession(session.defaultSession, {
                origin,
                // Only a page's own requests: the shell's probe (ses.fetch) has no web contents.
                onAllowed: (d) => {
                    if (d.webContentsId !== undefined) rendererRequests.push(d.url);
                },
                onBlocked: (url) => {
                    blocked.push(url);
                    console.error(`shell: blocked a request outside the served origin: ${url}`);
                },
                onPermission: (a) => permissionAsks.push(a),
            });

            // Automated runs save downloads to a temporary directory and record them.
            if (automated) {
                const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-desktop-dl-'));
                session.defaultSession.on('will-download', (_e, item) => {
                    const rec = { url: item.getURL(), state: 'progressing', path: null };
                    downloads.push(rec);
                    rec.path = path.join(dir, item.getFilename());
                    item.setSavePath(rec.path);
                    item.once('done', (_e2, state) => (rec.state = state));
                });
            }

            const openExternal = externalOpener((url) => {
                externalOpened.push(url);
                if (!automated) void shell.openExternal(url);
            });
            mainWindow = createMainWindow({ origin, offscreen: automated, openExternal });
            mainWindow.on('closed', () => {
                mainWindow = null;
            });
            returnToWaitingOnFailure(mainWindow, appUrl, origin);
            void loadWhenUp(mainWindow, appUrl, origin);

            if (scenario === 'a1') {
                const { runA1Scenario } = await import('../test/scenario-a1.mjs');
                const ok = await runA1Scenario({
                    harness,
                    getMain: () => mainWindow,
                    blocked,
                    rendererRequests: () => rendererRequests.filter((u) => u.startsWith(origin)),
                    externalOpened,
                    permissionAsks,
                    secondInstances,
                });
                await harness.stop();
                app.exit(ok ? 0 : 1);
            }
            if (scenario === 'c1') {
                const ok = await c1.run({
                    getMain: () => mainWindow,
                    blocked,
                    rendererRequests,
                    pinDecisions,
                    downloads,
                });
                app.exit(ok ? 0 : 1);
            }
        })
        .catch((e) => {
            // A failed start-up never leaves an automated check hanging (W-0027 C1d: a scenario
            // set-up error once did).
            console.error('shell: start-up failed:', e);
            if (automated) app.exit(1);
        });
}
