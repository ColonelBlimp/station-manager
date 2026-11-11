// W-0027 C1d part 3: the acceptance against a REAL smd (ADR 0103; the C1d brief's acceptance).
// Never the live station: smd is the binary named by SM_DESKTOP_SMD, run with a temporary
// SM_WORKING_DIR and a synthetic config seeding a configured workspace (setup complete, a
// placeholder station callsign) whose data_dir is a separate absolute directory (operator ruling
// Q1 (a)), a TCP browser listener with the SPA on a free loopback port, a free app port, and
// bridge, FT8 and evidence off. No real station file, port (8080/8443) or credential is used.
//
// Phases, in one Electron run:
//  1. cold start, no identity, an impostor on the app port: the waiting page says so; nothing is
//     sent there;
//  2. the impostor goes, smd starts and creates its identity: the shell pins it and the REAL SPA
//     runs its configured workspace; fetch, wss: and EventSource work;
//  3. a draft in the contact form's callsign field; smd stops and an impostor takes its port:
//     fresh attempts are refused and nothing arrives there; the draft stays;
//  4. smd restarts: the transports recover; the draft is still there;
//  5. a fresh window (a new, unused session: its own verifier, pin and origin) with the identity
//     present and an impostor on the port: the pin rejects it; once smd returns, it loads. Its
//     launch records the order: verifier and request filter before the window;
//  6. smd makes a new identity: the existing windows keep their pins and refuse it; a fresh
//     window pins the new identity and loads;
//  7. the shell never opened key.pem; the verifier was asked only about 127.0.0.1.
// A "fresh window" is a fresh session and window, not a restarted app process (codex-070).
// PASSES only if every check holds. Run: SM_DESKTOP_SMD=<smd> npm run check:c1d.
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { app, session } from 'electron';
import { startHarness } from '../harness/server.mjs';
import { launchTrusted } from '../src/launch.mjs';
import { externalOpener } from '../src/window.mjs';
import { makeIdentity } from './identity.mjs';
import { managedProcess, releaseAfter } from './smd-process.mjs';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const js = (win, code, ms = 8000) =>
    Promise.race([
        win.webContents.executeJavaScript(code, true),
        new Promise((_r, reject) =>
            setTimeout(() => reject(new Error(`page call timed out: ${code.slice(0, 60)}`)), ms)
        ),
    ]);

async function until(pred, what, timeout = 20000) {
    const end = Date.now() + timeout;
    while (Date.now() < end) {
        try {
            const v = await pred();
            if (v) return v;
        } catch {
            /* loading */
        }
        await sleep(150);
    }
    throw new Error(`timed out waiting for ${what}`);
}

const freePort = () =>
    new Promise((resolve, reject) => {
        const s = net.createServer();
        s.once('error', reject);
        s.listen(0, '127.0.0.1', () => {
            const { port } = s.address();
            s.close(() => resolve(port));
        });
    });

const answers = (port) => () =>
    new Promise((resolve) => {
        const s = net.connect(port, '127.0.0.1', () => {
            s.end();
            resolve(true);
        });
        s.on('error', () => resolve(false));
    });

// In-page transports against smd. Each resolves to what arrived, or 'refused:<reason>'.
const tryFetch = (m) =>
    `fetch('/v1/version?m=${m}', { cache: 'no-store' }).then((r) => r.json()).then((j) => 'smd:' + (typeof j.daemon), (e) => 'refused:' + e.name)`;
const trySocket = () => `new Promise((resolve) => {
    let ws;
    try { ws = new WebSocket('wss://' + location.host + '/v1/events/ws'); } catch (e) { resolve('refused:' + e.name); return; }
    const t = setTimeout(() => { try { ws.close(); } catch {} resolve('refused:timeout'); }, 5000);
    ws.onmessage = (ev) => { clearTimeout(t); ws.close(); try { resolve('smd:' + JSON.parse(ev.data).event); } catch { resolve('other'); } };
    ws.onerror = () => { clearTimeout(t); resolve('refused:error'); };
})`;
const tryEvents = () => `new Promise((resolve) => {
    const es = new EventSource('/v1/events');
    const t = setTimeout(() => { es.close(); resolve('refused:timeout'); }, 5000);
    es.onopen = () => { clearTimeout(t); es.close(); resolve('smd:open'); };
    es.onerror = () => { clearTimeout(t); es.close(); resolve('refused:error'); };
})`;
const transports = async (win, m) => [
    await js(win, tryFetch(m)),
    await js(win, trySocket()),
    await js(win, tryEvents()),
];

// The configured workspace: the contact form's own callsign field, and no setup form.
const WORKSPACE = `(() => { const f = document.querySelector('#lc-call');
    return !!f && !f.disabled && !f.readOnly && !document.querySelector('#setup-callsign'); })()`;
const DRAFT = `document.querySelector('#lc-call')?.value ?? '<gone>'`;

function writeConfig(wd, data, browserPort, appPort) {
    const cfg = {
        version: 6,
        data_dir: data,
        socket_path: `127.0.0.1:${browserPort}`,
        server: { protocol: 'tcp', serve_spa: true, app_port: appPort },
        datastore: { driver: 'sqlite', path: path.join(data, 'db', 'station-manager.db') },
        setup_complete: true,
        logging_station: { station_callsign: 'N0CALL' }, // a placeholder, never a real station
        bridge: { enabled: false },
        ft8: { enabled: false },
        evidence: { capture: false },
    };
    fs.writeFileSync(path.join(wd, 'config.json'), JSON.stringify(cfg), { mode: 0o600 });
}

/** One app window, launched exactly as the interactive app launches it (src/launch.mjs). */
const launch = (env, partition) =>
    launchTrusted({
        env,
        offscreen: true,
        openExternal: externalOpener(() => {}),
        partition,
        retryMs: 300,
    });

/** Launch a fresh window recording the order of its session's guards and the window's creation. */
function launchRecordingOrder(env, partition) {
    const order = [];
    const ses = session.fromPartition(partition);
    const verify = ses.setCertificateVerifyProc.bind(ses);
    ses.setCertificateVerifyProc = (...a) => {
        order.push('verifier');
        return verify(...a);
    };
    const filter = ses.webRequest.onBeforeRequest.bind(ses.webRequest);
    ses.webRequest.onBeforeRequest = (...a) => {
        order.push('filter');
        return filter(...a);
    };
    app.once('browser-window-created', () => order.push('window'));
    return { ...launch(env, partition), order };
}

export async function runC1d() {
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const windows = [];
    let base = null;
    let impostor = null;
    let smd = null;

    try {
        // Every resource is owned from here: released in finally whatever fails (codex-070).
        const bin = process.env.SM_DESKTOP_SMD;
        if (!bin || !fs.existsSync(bin)) {
            throw new Error('set SM_DESKTOP_SMD to an smd built with the SPA embedded');
        }
        base = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-c1d-real-'));
        const wd = path.join(base, 'wd');
        const data = path.join(base, 'data'); // data_dir elsewhere: Q1 (a) end to end
        fs.mkdirSync(wd, { mode: 0o700 });
        fs.mkdirSync(data, { mode: 0o700 });
        const browserPort = await freePort();
        impostor = await startHarness({
            up: false,
            tls: makeIdentity('impostor'),
            tag: 'impostor',
        });
        const appPort = impostor.port;
        writeConfig(wd, data, browserPort, appPort);
        smd = managedProcess(bin, ['--config', path.join(wd, 'config.json')], {
            env: { PATH: process.env.PATH, HOME: os.homedir(), SM_WORKING_DIR: wd },
            ready: answers(browserPort),
        });
        const env = { SM_WORKING_DIR: wd, HOME: os.homedir() };
        const idDir = path.join(data, 'desktop-identity');

        // 1. Cold start, no identity, an impostor on the app port.
        await impostor.start();
        const A = launch(env, null);
        windows.push(A);
        await until(
            () =>
                js(A.win, `document.querySelector('[data-sm-reason]')?.textContent ?? ''`).then(
                    (t) => t.includes('not created')
                ),
            'the waiting page saying smd has not created its identity'
        );
        await sleep(2000);
        check(
            'Cold start, no identity: the waiting page says so; nothing is sent to the app port',
            impostor.handshakes.failed + impostor.handshakes.secure === 0 &&
                impostor.requests.length === 0,
            JSON.stringify(impostor.handshakes)
        );

        // 2. smd starts and creates its identity: pinned; the real SPA runs its workspace.
        await impostor.stop();
        await smd.start();
        await until(() => A.pin.get() !== null, 'the pin to be fixed');
        check(
            'The identity is found under data_dir (not SM_WORKING_DIR) and pinned',
            fs.existsSync(path.join(idDir, 'cert.pem')) &&
                !fs.existsSync(path.join(wd, 'desktop-identity')),
            idDir
        );
        await until(() => js(A.win, WORKSPACE), "the real SPA's configured workspace");
        const title = await js(A.win, 'document.title');
        check(
            'The real SPA runs its configured workspace over the pinned listener (the contact form, no setup form; its script set the title)',
            / · Station Manager$/.test(title) &&
                A.win.webContents.getURL().startsWith(`https://127.0.0.1:${appPort}/`),
            title
        );
        const t2 = await transports(A.win, 'a');
        check(
            'fetch, wss: and EventSource work against smd',
            t2[0] === 'smd:string' && t2[1] === 'smd:connected' && t2[2] === 'smd:open',
            t2.join(' ')
        );

        // 3. A draft in the contact form's callsign field; smd replaced by an impostor.
        await js(
            A.win,
            `(() => { const i = document.querySelector('#lc-call'); i.focus();
               const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
               set.call(i, 'C1D-DRAFT'); i.dispatchEvent(new Event('input', { bubbles: true })); return i.value; })()`
        );
        const urlBefore = A.win.webContents.getURL();
        await smd.stop();
        await impostor.start();
        const failed0 = impostor.handshakes.failed;
        const t3 = await transports(A.win, 'i');
        // The SPA's own stream reconnects also reach the impostor here, so the refused handshakes
        // are evidence that it was contacted and refused, not an attribution to each transport
        // (the C1 proof's fixtures attribute each one).
        check(
            'smd replaced by an impostor: fetch, wss: and EventSource are refused; the impostor saw only refused handshakes and received no request',
            t3.every((v) => v.startsWith('refused:')) &&
                impostor.handshakes.failed > failed0 &&
                impostor.handshakes.secure === 0 &&
                impostor.requests.length === 0,
            `${t3.join(' ')}; handshakes refused ${impostor.handshakes.failed - failed0}`
        );
        check(
            '…the loaded workspace keeps its draft and is not sent to the waiting page',
            (await js(A.win, DRAFT)) === 'C1D-DRAFT' && A.win.webContents.getURL() === urlBefore,
            await js(A.win, DRAFT)
        );

        // 4. smd restarts: recovery, draft intact.
        await impostor.stop();
        await smd.start();
        const t4 = await until(async () => {
            const v = await transports(A.win, 'r');
            return v.every((x) => x.startsWith('smd:')) ? v : null;
        }, 'the transports to recover');
        check(
            'smd restarted: fetch, wss: and EventSource recover',
            Boolean(t4),
            (t4 ?? []).join(' ')
        );
        check(
            '…and the draft is still there',
            (await js(A.win, DRAFT)) === 'C1D-DRAFT',
            await js(A.win, DRAFT)
        );

        // 5. A fresh window (new session) with the identity present and an impostor on the port.
        await smd.stop();
        await impostor.start();
        const seen0 = impostor.handshakes.failed + impostor.handshakes.secure;
        const B = launchRecordingOrder(env, 'c1d-fresh-session');
        windows.push(B);
        const at = (k) => B.order.indexOf(k);
        check(
            'A fresh window installs the verifier and the request filter before the window exists',
            at('verifier') >= 0 &&
                at('filter') >= 0 &&
                at('window') > at('verifier') &&
                at('window') > at('filter'),
            B.order.join(' < ')
        );
        await until(() => B.pin.get() !== null, 'the fresh window to pin the existing identity');
        // Wait for any attempt, so a pin that accepted the impostor fails the check below.
        await until(
            () => impostor.handshakes.failed + impostor.handshakes.secure > seen0,
            'the fresh window to try the impostor'
        );
        await sleep(1500);
        check(
            'Fresh window, existing identity, impostor on the port: the pin rejects it; nothing reaches it',
            B.pin.get() === A.pin.get() &&
                impostor.handshakes.secure === 0 &&
                impostor.requests.length === 0 &&
                (await js(B.win, `!!document.querySelector('[data-sm-waiting]')`)),
            JSON.stringify(impostor.handshakes)
        );
        await impostor.stop();
        await smd.start();
        await until(() => js(B.win, WORKSPACE), 'the fresh window to load the workspace');
        check('…once smd returns, the fresh window loads the configured workspace', true);

        // 6. smd makes a new identity.
        const oldPin = A.pin.get();
        await smd.stop();
        fs.rmSync(idDir, { recursive: true });
        await smd.start();
        await until(
            () => fs.existsSync(path.join(idDir, 'cert.pem')),
            'smd to make a new identity'
        );
        const t6 = await js(A.win, tryFetch('n'));
        check(
            'A new smd identity: the existing windows keep their pins and refuse it',
            t6.startsWith('refused:') && A.pin.get() === oldPin && B.pin.get() === oldPin,
            t6
        );
        const C = launch(env, 'c1d-after-rotation');
        windows.push(C);
        await until(() => js(C.win, WORKSPACE), 'a fresh window to load with the new identity');
        check(
            '…and a fresh window pins the new identity and loads the workspace',
            C.pin.get() !== null && C.pin.get() !== oldPin,
            `${oldPin.slice(0, 11)}… -> ${C.pin.get().slice(0, 11)}…`
        );

        // 7. Key and verifier hygiene.
        const opened = windows.flatMap((w) => w.opened);
        check(
            'The shell never opened key.pem (every open through its boundary is recorded)',
            opened.length > 0 && !opened.some((p) => p.endsWith('key.pem')),
            `${opened.length} opens`
        );
        const hosts = new Set(windows.flatMap((w) => w.decisions.map((d) => d.hostname)));
        check(
            'The verifier was asked only about 127.0.0.1',
            hosts.size === 1 && hosts.has('127.0.0.1'),
            [...hosts].join(',')
        );
        const blocked = windows.flatMap((w) => w.blocked);
        check('Nothing left the app origin', blocked.length === 0, blocked.slice(0, 3).join(' '));
    } catch (e) {
        check('Scenario completed', false, e.message);
    } finally {
        for (const w of windows) if (!w.win.isDestroyed()) w.win.destroy();
        await impostor?.stop();
        // The scratch tree goes only once smd's exit is confirmed; otherwise the run fails and
        // names what it kept (codex-071).
        if (smd) {
            const r = await releaseAfter(smd, base);
            if (!r.ok)
                check(
                    'smd stopped before cleanup',
                    false,
                    `kept ${r.kept}; smd pid ${r.pid}: ${r.reason}`
                );
        } else if (base) {
            fs.rmSync(base, { recursive: true, force: true }); // exactly what this run made
        }
    }

    for (const c of checks) {
        console.log(`  ${c.ok ? 'ok  ' : 'FAIL'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`);
    }
    const ok = checks.length > 0 && checks.every((c) => c.ok);
    if (!ok && smd) console.log(`smd log (tail):\n${smd.log.split('\n').slice(-15).join('\n')}`);
    console.log(ok ? `C1d PASS (${checks.length} checks)` : 'C1d FAIL');
    return ok;
}
