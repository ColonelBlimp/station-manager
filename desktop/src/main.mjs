// Station Manager desktop shell (ADR 0102; W-0027 A1). Development only: not released.
//
// A1 loads ONLY the built-in fixture harness. The shell executes no daemon-served UI
// until C1's authentication is implemented and validated (ADR 0102, Requirements), so
// there is deliberately no way to point it at smd yet.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { app, Menu, session, shell } from 'electron';
import { startHarness } from '../harness/server.mjs';
import { hardenSession } from './hardening.mjs';
import { createMainWindow, externalOpener } from './window.mjs';
import { loadWhenUp, returnToWaitingOnFailure } from './waiting.mjs';

const scenario = process.env.SM_DESKTOP_SCENARIO ?? '';
const automated = scenario !== '';

// Automated runs use a throwaway profile unless told to share one (the second-launch
// check); interactive runs use the default profile.
if (process.env.SM_DESKTOP_USERDATA) app.setPath('userData', process.env.SM_DESKTOP_USERDATA);
else if (automated) app.setPath('userData', fs.mkdtempSync(path.join(os.tmpdir(), 'sm-desktop-')));

// Automated runs give Chromium a fake capture device, so a refused microphone request
// is the permission policy's decision, never a missing device (codex-038).
if (automated) app.commandLine.appendSwitch('use-fake-device-for-media-stream');

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

    app.whenReady().then(async () => {
        Menu.setApplicationMenu(null);
        // Automated runs start with the "daemon" down to prove the cold start.
        // SM_DESKTOP_HARNESS_DELAY_MS shows the waiting page by hand.
        const delay = Number(process.env.SM_DESKTOP_HARNESS_DELAY_MS || 0);
        const harness = await startHarness({ up: !automated && delay === 0 });
        if (!automated && delay > 0) setTimeout(() => void harness.start(), delay);
        const origin = harness.origin;
        const appUrl = origin + '/';

        hardenSession(session.defaultSession, {
            origin,
            onAllowed: (d) => rendererRequests.push(d.url),
            onBlocked: (url) => {
                blocked.push(url);
                console.error(`shell: blocked a request outside the served origin: ${url}`);
            },
            onPermission: (a) => permissionAsks.push(a),
        });

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
    });
}
