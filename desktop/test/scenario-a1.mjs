// W-0027 A1 acceptance, run inside Electron against the fixture harness with
// offscreen windows. PASSES only if every check holds. Run: npm run check:a1
// (under xvfb-run where there is no display).
import { spawn } from 'node:child_process';
import http from 'node:http';
import { app, BrowserWindow, Menu } from 'electron';
import { mainWindowOptions } from '../src/window.mjs';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const js = (win, code, ms = 4000) =>
    Promise.race([
        win.webContents.executeJavaScript(code, true),
        new Promise((_r, reject) =>
            setTimeout(() => reject(new Error(`page call timed out: ${code.slice(0, 60)}`)), ms)
        ),
    ]);

async function until(pred, what, timeout = 10000) {
    const end = Date.now() + timeout;
    while (Date.now() < end) {
        try {
            const v = await pred();
            if (v) return v;
        } catch {
            /* the page is loading */
        }
        await sleep(100);
    }
    throw new Error(`timed out waiting for ${what}`);
}

const onWaitingPage = (win) =>
    win.webContents.getURL().startsWith('data:') &&
    js(win, `!!document.querySelector('[data-sm-waiting]')`);
const onFixture = (win, origin) =>
    win.webContents.getURL().startsWith(origin + '/') &&
    js(win, `!!document.querySelector('[data-sm-fixture]')`);

/** A second launch with the same profile: it must exit at once (code 0) and the
 *  running instance must hear of it. */
function secondLaunch(userData) {
    return new Promise((resolve) => {
        const child = spawn(process.execPath, [app.getAppPath()], {
            env: { ...process.env, SM_DESKTOP_SCENARIO: 'second', SM_DESKTOP_USERDATA: userData },
            stdio: 'ignore',
        });
        const timer = setTimeout(() => {
            child.kill('SIGKILL');
            resolve({ code: 'timeout' });
        }, 15000);
        child.on('exit', (code) => {
            clearTimeout(timer);
            resolve({ code });
        });
    });
}

export async function runA1Scenario(ctx) {
    const { harness, getMain, blocked, externalOpened, permissionAsks, secondInstances } = ctx;
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const origin = harness.origin;
    const elsewhereHits = [];
    const other = http.createServer((req, res) => {
        elsewhereHits.push(req.url);
        res.end('elsewhere');
    });
    await new Promise((r) => other.listen(0, '127.0.0.1', r));
    const elsewhere = `http://127.0.0.1:${other.address().port}`;

    try {
        // 1. Cold start with the origin down: the shell's own waiting page, which
        //    asks the origin for nothing (it is local to the shell).
        const main = getMain();
        await until(() => onWaitingPage(main), 'the waiting page');
        const before = harness.requests.length;
        await sleep(1500);
        check(
            'Cold start with the daemon down shows the local waiting page',
            await onWaitingPage(main),
            main.webContents.getURL().slice(0, 40)
        );
        check(
            'The waiting page loads nothing from the daemon origin (renderer requests while waiting: 0)',
            ctx.rendererRequests().length === 0,
            JSON.stringify(ctx.rendererRequests())
        );
        check(
            'Only the shell probes the origin while it is down',
            harness.requests.length === before,
            `${harness.requests.length - before} served while down`
        );

        // 2. The origin comes up: the window loads it without anyone acting.
        await harness.start();
        await until(() => onFixture(main, origin), 'the fixture page after the daemon came up');
        check('When the daemon answers, the window loads it by itself', true);

        // 3. The renderer is sandboxed, context-isolated and has no Node.
        const prefs = main.webContents.getLastWebPreferences();
        check(
            'Renderer preferences: sandbox on, context isolation on, Node integration off, no webview',
            prefs.sandbox === true &&
                prefs.contextIsolation === true &&
                prefs.nodeIntegration === false &&
                prefs.webviewTag === false,
            JSON.stringify({
                sandbox: prefs.sandbox,
                contextIsolation: prefs.contextIsolation,
                nodeIntegration: prefs.nodeIntegration,
                webviewTag: prefs.webviewTag,
            })
        );
        const nodeGlobals = await js(
            main,
            `[typeof require, typeof process, typeof module, typeof Buffer].join(',')`
        );
        check(
            'The page sees no Node globals (require, process, module, Buffer)',
            nodeGlobals === 'undefined,undefined,undefined,undefined',
            nodeGlobals
        );

        // 4. Requests and navigation stay on the served origin. "Elsewhere" is a real
        //    listening server, so a request the shell failed to cancel would reach it.
        const blockedBefore = blocked.length;
        await js(
            main,
            `fetch('${elsewhere}/x', { mode: 'no-cors' }).then(() => 'answered', () => 'refused')`
        );
        await sleep(300);
        check(
            'A request to another origin is cancelled by the shell (it never arrives)',
            elsewhereHits.length === 0 &&
                blocked.slice(blockedBefore).some((u) => u.startsWith(elsewhere)),
            `arrived ${elsewhereHits.length}; blocked ${JSON.stringify(blocked.slice(blockedBefore))}`
        );
        const sameOrigin = await js(main, `fetch('/').then((r) => r.status, () => 'refused')`);
        check('A request to the served origin is allowed', sameOrigin === 200, String(sameOrigin));

        const windowsBefore = BrowserWindow.getAllWindows().length;
        await js(main, `location.href = '${elsewhere}/away'; true`);
        await sleep(800);
        check(
            'Navigating the window to another origin is refused; the window stays on its page',
            (await onFixture(main, origin)) &&
                BrowserWindow.getAllWindows().length === windowsBefore,
            main.webContents.getURL()
        );
        check(
            'An http (not https) external address is not handed to the browser',
            !externalOpened.some((u) => u.startsWith(elsewhere)),
            JSON.stringify(externalOpened)
        );

        await js(main, `window.open('https://example.org/qrz', '_blank'); true`);
        await js(main, `window.open('/other', '_blank'); true`);
        await js(main, `window.open('file:///etc/passwd', '_blank'); true`);
        await sleep(800);
        check(
            'window.open creates no window (windows belong to the shell)',
            BrowserWindow.getAllWindows().length === windowsBefore,
            `${windowsBefore} -> ${BrowserWindow.getAllWindows().length}`
        );
        check(
            'Only the https external address goes to the default browser',
            externalOpened.length === 1 && externalOpened[0] === 'https://example.org/qrz',
            JSON.stringify(externalOpened)
        );

        // 5. Electron permissions are denied by default, and the handlers decided it.
        const asksBefore = permissionAsks.length;
        const perms = await js(
            main,
            `(async () => {
                const notify = await Notification.requestPermission();
                const geo = (await navigator.permissions.query({ name: 'geolocation' })).state;
                const mic = await navigator.mediaDevices
                    .getUserMedia({ audio: true })
                    .then(() => 'granted', (e) => 'refused:' + e.name);
                return { notify, geo, mic };
            })()`,
            8000
        );
        // The runs have a fake capture device, so NotAllowedError is the policy's refusal
        // and NotFoundError (no device) does not pass.
        const asked = permissionAsks.slice(asksBefore);
        const saw = (kind, permission) =>
            asked.some((a) => a.kind === kind && a.permission === permission);
        check(
            'Notifications, geolocation and the microphone are refused (microphone: NotAllowedError)',
            perms.notify === 'denied' &&
                perms.geo === 'denied' &&
                perms.mic === 'refused:NotAllowedError',
            JSON.stringify(perms)
        );
        check(
            "…each by the shell's handlers (notifications and media requests, geolocation check)",
            saw('request', 'notifications') &&
                saw('request', 'media') &&
                saw('check', 'geolocation'),
            JSON.stringify(asked)
        );

        // 6. The main window's minimum size is 1024x640.
        const [minW, minH] = main.getMinimumSize();
        main.setSize(800, 500);
        await sleep(300);
        const [w, h] = main.getSize();
        check(
            'Main window minimum size is 1024x640 and a smaller size is refused',
            minW === 1024 && minH === 640 && w >= 1024 && h >= 640,
            `minimum ${minW}x${minH}; after setSize(800,500): ${w}x${h}`
        );

        // 6b. The window carries Station Manager's icon (configuration; the title bar
        //     itself is a desktop check).
        const icon = mainWindowOptions({ offscreen: true }).icon;
        const iconSize = icon && !icon.isEmpty() ? icon.getSize() : null;
        check(
            "The main window is given Station Manager's icon (256x256, from logo.svg)",
            iconSize?.width === 256 && iconSize?.height === 256,
            JSON.stringify(iconSize)
        );

        // 7. No application menu (no menu reload, no developer tools).
        check('No application menu', Menu.getApplicationMenu() === null);

        // 8. Daemon loss: a failed load returns to the waiting page, and the window
        //    recovers by itself when the daemon is back.
        await harness.stop();
        main.loadURL(origin + '/');
        await until(() => onWaitingPage(main), 'the waiting page after a failed load');
        check('A failed load after daemon loss shows the waiting page', true);

        // 8b. A replacement listener at the daemon's address that redirects elsewhere: the
        //     shell's availability probe refuses the redirect, loads nothing, and keeps
        //     waiting (codex-038).
        const port = Number(new URL(origin).port);
        const imposter = http.createServer((_req, res) => {
            res.writeHead(302, { location: `${elsewhere}/redirect-target` });
            res.end();
        });
        await new Promise((r) => imposter.listen(port, '127.0.0.1', r));
        const hitsBefore = elsewhereHits.length;
        await sleep(3000);
        check(
            "A redirecting replacement at the daemon's address: the probe's redirect reaches nothing",
            elsewhereHits.length === hitsBefore,
            JSON.stringify(elsewhereHits.slice(hitsBefore))
        );
        check(
            '…and the window keeps waiting (no application load)',
            await onWaitingPage(main),
            main.webContents.getURL().slice(0, 40)
        );
        imposter.closeAllConnections();
        await new Promise((r) => imposter.close(r));

        // 8c. A replacement that answers the probe but redirects the window's own load
        //     elsewhere: nothing arrives there, the window never rests on the other address
        //     or an error page, it keeps returning to the waiting page, and it retries at the
        //     waiting pace rather than in a tight loop.
        let windowLoads = 0;
        const splitter = http.createServer((req, res) => {
            // The window's own page load is a document navigation; the shell's probe (ses.fetch,
            // the same Chromium stack and user agent since C1) is not.
            if (req.headers['sec-fetch-dest'] === 'document') {
                windowLoads += 1;
                res.writeHead(302, { location: `${elsewhere}/window-target` });
                res.end();
                return;
            }
            res.writeHead(200, { 'content-type': 'text/plain' });
            res.end('probe answered');
        });
        await new Promise((r) => splitter.listen(port, '127.0.0.1', r));
        const hitsBeforeSplit = elsewhereHits.length;
        main.loadURL(origin + '/');
        const seen = [];
        for (let i = 0; i < 30; i++) {
            await sleep(100);
            seen.push(main.webContents.getURL());
        }
        const rested = seen.filter((u) => u.startsWith(elsewhere) || u.startsWith('chrome-error:'));
        check(
            "A replacement redirecting only the window's load: nothing arrives elsewhere",
            elsewhereHits.length === hitsBeforeSplit,
            JSON.stringify(elsewhereHits.slice(hitsBeforeSplit))
        );
        await sleep(1000);
        check(
            '…the window returns to the waiting page and never shows the other address or an error page',
            (await onWaitingPage(main)) &&
                seen.some((u) => u.startsWith('data:')) &&
                rested.length === 0,
            `final ${main.webContents.getURL().slice(0, 30)}; elsewhere/error samples ${rested.length}/30`
        );
        check(
            '…and retries at the waiting pace, not in a tight loop',
            windowLoads >= 1 && windowLoads <= 8,
            `${windowLoads} window loads in about 4 s`
        );
        splitter.closeAllConnections();
        await new Promise((r) => splitter.close(r));
        await harness.start();
        await until(() => onFixture(main, origin), 'recovery after the daemon returned');
        check('…and the window recovers when the daemon returns', true);

        // 9. Single instance: a second launch exits and the running one hears of it.
        const heardBefore = secondInstances.length;
        const second = await secondLaunch(app.getPath('userData'));
        await until(
            () => secondInstances.length > heardBefore,
            'the second-instance event',
            5000
        ).catch(() => {});
        check(
            'A second launch exits at once and the running instance is told',
            second.code === 0 && secondInstances.length === heardBefore + 1,
            `second exited ${second.code}; events ${secondInstances.length - heardBefore}`
        );
    } catch (err) {
        check('scenario completed', false, String(err?.message ?? err));
    }

    other.close();
    check(
        'No unexpected blocked requests',
        blocked.every((u) => u.startsWith(elsewhere)),
        JSON.stringify(blocked)
    );

    const pass = checks.every((c) => c.ok);
    console.log(`A1 ${pass ? 'PASS' : 'FAIL'}`);
    for (const c of checks)
        console.log(`  ${c.ok ? 'ok  ' : 'FAIL'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`);
    return pass;
}
