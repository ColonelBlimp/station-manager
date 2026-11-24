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
import { app, dialog, Menu, session, shell } from 'electron';
import { unconfirmedBox } from './closing.mjs';
import { disableSpellchecker, hardenSession } from './hardening.mjs';
import { parseArgs } from './daemon.mjs';
import { launchTrusted } from './launch.mjs';
import { installPin } from './pin.mjs';
import { blockedRequestLine } from './trace.mjs';
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

    // The close of the app's window (W-0027 A3), held here once a window is launched.
    let closing = null;

    app.on('second-instance', () => {
        secondInstances.push(Date.now());
        // While a close is under way a second start adds nothing: the window is hidden, or
        // gone, and the box is what the operator answers. Showing the window again would
        // put a page in front of the operator that can no longer write (the clean-room
        // review of ff653c6c).
        if (!mainWindow || closing?.holdsApp()) return;
        if (mainWindow.isMinimized()) mainWindow.restore();
        mainWindow.show();
        mainWindow.focus();
    });
    // The close of the app's window (W-0027 A3). While one is under way the app outlives its
    // window: a close smd did not confirm, once an exit report was sent, closes the window
    // BEFORE its box, and the box must still be there to answer. The close itself quits the
    // app when it ends. An automated check keeps the app alive between its windows, except
    // where it says it wants the app's own rule (quitRule.asInstalled).
    const quitRule = { asInstalled: !automated };
    app.on('window-all-closed', () => {
        if (quitRule.asInstalled && !closing?.holdsApp()) app.quit();
    });
    // And no other request to quit ends the app under a close's box: only the close's own
    // final quit goes through while one is under way (codex-239). What the operating system
    // does to the process is not asked here and cannot be (a kill, the end of the session).
    app.on('before-quit', (e) => {
        if (closing && !closing.allowsQuit()) e.preventDefault();
    });
    // The box for a close smd did not confirm. It has no parent: the window is hidden, or
    // already closed.
    const askUnconfirmed = async (state) => {
        const box = unconfirmedBox(state);
        const { response } = await dialog.showMessageBox({
            type: 'warning',
            title: 'Station Manager',
            message: box.message,
            detail: box.detail,
            buttons: box.buttons,
            cancelId: box.cancelId,
            defaultId: 0,
            noLink: true,
        });
        return box.choices[response] ?? box.choices[box.cancelId];
    };

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
                    // W-0027 C2b: said once, in a box of the shell's own. The page is left as
                    // it is, so nothing the operator has typed is lost.
                    onCredentialRefused: () => {
                        if (!mainWindow) return;
                        void dialog.showMessageBox(mainWindow, {
                            type: 'warning',
                            title: 'Station Manager',
                            message: 'The daemon no longer accepts this app',
                            detail:
                                'Its operating credential has changed since Station Manager was opened, so the daemon is refusing this window.\n\n' +
                                'What you have typed is still here. To reconnect, close Station Manager and open it again.',
                            buttons: ['OK'],
                        });
                    },
                    // W-0027 C4a: the daemon has admitted a later start of the app. Said
                    // once; this window can still read and log, and can no longer operate.
                    onRetired: () => {
                        if (!mainWindow) return;
                        void dialog.showMessageBox(mainWindow, {
                            type: 'warning',
                            title: 'Station Manager',
                            message: 'Another Station Manager has taken over',
                            detail:
                                'The daemon is being operated from a later start of Station Manager, so this window can no longer transmit.\n\n' +
                                'What you have typed is still here. Close this window when you have finished with it.',
                            buttons: ['OK'],
                        });
                    },
                    askUnconfirmed,
                    quit: () => app.quit(),
                });
                mainWindow = launched.win;
                closing = launched.closing; // held, not yet run by anything (launch.mjs)
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
            // W-0029 part 4: the recovery question, against a real smd (SM_DESKTOP_SMD), in a
            // temporary working directory with synthetic data and free ports; never the station.
            if (scenario === 'recovery') {
                const ok = await (await import('../test/scenario-recovery.mjs')).runRecovery();
                app.exit(ok ? 0 : 1);
                return;
            }
            // W-0027 A3: the app's close, against a real smd (SM_DESKTOP_SMD), in a temporary
            // working directory with synthetic data and free ports; never the station. It
            // hands each launch to the handlers above as the app's own, and for its last step
            // asks for the app's own quit rule, so that what is checked is those handlers and
            // not copies of them.
            if (scenario === 'close') {
                const { runClose } = await import('../test/scenario-close.mjs');
                const ok = await runClose({
                    // The launch the handlers above are to treat as the app's own: its
                    // window and its close, as the interactive path sets them.
                    adopt: (launched) => {
                        mainWindow = launched.win;
                        closing = launched.closing;
                        launched.win.on('closed', () => {
                            if (mainWindow === launched.win) mainWindow = null;
                        });
                    },
                    useInstalledQuitRule: () => {
                        quitRule.asInstalled = true;
                    },
                });
                app.exit(ok ? 0 : 1);
                return;
            }
            // C2b (W-0027): the operating credential, against a synthetic install and TLS
            // fixtures only. The scenario launches further windows itself, each on a fresh
            // session, to prove what a restart adopts.
            if (scenario === 'c2b') {
                const s = await (await import('../test/scenario-c2b.mjs')).setupC2b();
                const launch = (partition) => {
                    const refusals = [];
                    const sent = [];
                    const launched = launchTrusted({
                        env: s.env,
                        offscreen: true,
                        partition,
                        openExternal: externalOpener((url) => externalOpened.push(url)),
                        retryMs: 300,
                        onCredentialRefused: () => refusals.push(Date.now()),
                        onCredentialRequest: (r) => sent.push(r),
                    });
                    return { ...launched, refusals, sent };
                };
                const first = launch(undefined);
                mainWindow = first.win;
                const ok = await s.run({ first, launch });
                await s.cleanup();
                app.exit(ok ? 0 : 1);
                return;
            }
            // C4a (W-0027): the app lifetime and its admission, against a synthetic install
            // and a TLS fixture that answers as smd does with its check on.
            if (scenario === 'c4a') {
                const s = await (await import('../test/scenario-c4a.mjs')).setupC4a();
                const launch = (partition) => {
                    const retired = [];
                    const launched = launchTrusted({
                        env: s.env,
                        offscreen: true,
                        partition,
                        openExternal: externalOpener((url) => externalOpened.push(url)),
                        retryMs: 300,
                        onRetired: () => retired.push(Date.now()),
                    });
                    return { ...launched, retired };
                };
                const first = launch(undefined);
                mainWindow = first.win;
                const ok = await s.run({ first, launch });
                await s.cleanup();
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
                    console.error(blockedRequestLine(url));
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
