// W-0027 C1d part 2: the shell's own wiring against a synthetic install and TLS fixture
// harnesses. Never smd and never the station: a temporary SM_WORKING_DIR, synthetic identities
// made per run, an ephemeral loopback port, a temporary profile. (The real-smd acceptance is
// part 3's check:c1d.)
//
// It proves, in order:
//  1. no identity yet: the waiting page says so, and nothing is sent to the app address,
//     although an impostor is listening there;
//  2. an unsafe identity: the waiting page names the path and the reason, escaped, and still
//     nothing is sent;
//  3. a valid identity: the pin and origin are fixed; the impostor still at the address is
//     refused by the pin; the genuine listener then loads;
//  4. the pin is fixed for the app's lifetime: replacing the identity on disk and the listener
//     with another key is refused, the identity is never read again, and the genuine listener
//     recovers.
// PASSES only if every check holds. Run: npm run check:c1d-shell (xvfb-run -a without a display).
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { startHarness } from '../harness/server.mjs';
import { fingerprintOf } from '../src/pin.mjs';
import { makeIdentity } from './identity.mjs';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const js = (win, code, ms = 6000) =>
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

const tryFetch = (m) =>
    `fetch('/api/echo?m=${m}', { cache: 'no-store' }).then((r) => r.json()).then((j) => j.tag + ':' + j.m, (e) => 'refused:' + e.name)`;

/** The synthetic install and the two harnesses at one port, both down. The install directory's
 *  name contains markup, so the waiting page's escaping is tested on a real path. */
export async function setupC1dShell() {
    const base = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-c1d-shell-'));
    const wd = path.join(base, 'sm <b>x');
    fs.mkdirSync(wd, { mode: 0o700 });
    const G = makeIdentity('genuine');
    const I = makeIdentity('impostor');
    const genuine = await startHarness({ up: false, tls: G, tag: 'genuine' });
    const impostor = await startHarness({ up: false, tls: I, tag: 'impostor', port: genuine.port });
    fs.writeFileSync(
        path.join(wd, 'config.json'),
        JSON.stringify({ server: { app_port: genuine.port } }),
        { mode: 0o600 }
    );
    const idDir = path.join(wd, 'desktop-identity');
    const writeIdentity = (id) => {
        fs.writeFileSync(path.join(idDir, 'key.pem'), id.key, { mode: 0o600 });
        fs.writeFileSync(path.join(idDir, 'cert.pem'), id.cert, { mode: 0o644 });
    };
    return {
        env: { SM_WORKING_DIR: wd, HOME: os.homedir() },
        origin: genuine.origin,
        cleanup: async () => {
            await genuine.stop();
            await impostor.stop();
            fs.rmSync(base, { recursive: true, force: true }); // exactly the path this run made
        },
        run: (ctx) => runScenario({ ...ctx, genuine, impostor, G, I, wd, idDir, writeIdentity }),
    };
}

async function runScenario(ctx) {
    const { getMain, pinDecisions, opened, pin, blocked } = ctx;
    const { genuine, impostor, G, I, idDir, writeIdentity } = ctx;
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const main = getMain();
    const page = () =>
        js(
            main,
            `(() => { const w = document.querySelector('[data-sm-waiting]');
              return w ? { state: w.dataset.smState,
                           reason: document.querySelector('[data-sm-reason]')?.textContent ?? '',
                           path: document.querySelector('[data-sm-path]')?.textContent ?? '',
                           injected: !!document.querySelector('.box b') } : null; })()`
        );
    const tag = () => js(main, `document.querySelector('[data-sm-fixture]')?.dataset.tag ?? ''`);

    try {
        // 1. No identity yet, with an impostor listening at the app address.
        await impostor.start();
        const w1 = await until(async () => {
            const p = await page();
            return p?.reason.includes('not created') ? p : null;
        }, 'the waiting page saying smd has not created its identity');
        await sleep(2500); // several discovery rounds
        check(
            'No identity: the waiting page says smd has not created its identity yet',
            w1.state === 'waiting' && w1.path.endsWith('desktop-identity'),
            JSON.stringify(w1)
        );
        check(
            '…and nothing is sent to the app address (no handshake, no request), no pin asked',
            impostor.handshakes.failed + impostor.handshakes.secure === 0 &&
                impostor.requests.length === 0 &&
                pinDecisions.length === 0,
            JSON.stringify({ handshakes: impostor.handshakes, pin: pinDecisions.length })
        );

        // 2. An unsafe identity: directory 0755.
        fs.mkdirSync(idDir, { mode: 0o755 });
        fs.chmodSync(idDir, 0o755);
        writeIdentity(G);
        const w2 = await until(async () => {
            const p = await page();
            return p?.state === 'refused' ? p : null;
        }, 'the waiting page refusing the identity');
        check(
            'Unsafe identity: the waiting page names the path and the reason',
            w2.reason.includes('0700') && w2.path.endsWith('desktop-identity'),
            JSON.stringify(w2)
        );
        check(
            '…shown as text: the path’s markup is not rendered',
            w2.path.includes('<b>x') && !w2.injected,
            JSON.stringify(w2)
        );
        check(
            '…and still nothing is sent to the app address, and no pin is fixed',
            impostor.handshakes.failed + impostor.handshakes.secure === 0 &&
                impostor.requests.length === 0 &&
                pin.get() === null,
            JSON.stringify(impostor.handshakes)
        );

        // 3. Made safe: the pin and origin are fixed; the impostor still there is refused.
        fs.chmodSync(idDir, 0o700);
        await until(() => pin.get() !== null, 'the pin to be fixed');
        const openedAtPin = opened.length;
        check(
            'Valid identity: the pin is fixed to its certificate',
            pin.get() === fingerprintOf(G.cert),
            pin.get()
        );
        // Wait for any handshake, so an accepted impostor fails the check below, not this wait.
        await until(
            () => impostor.handshakes.failed + impostor.handshakes.secure > 0,
            'the probe to try the impostor'
        );
        await sleep(1500);
        check(
            '…the impostor still at the address is refused by the pin; nothing reaches it',
            impostor.handshakes.secure === 0 && impostor.requests.length === 0,
            JSON.stringify(impostor.handshakes)
        );
        const w3 = await page();
        check(
            '…and the waiting page no longer shows the repaired refusal (codex-068)',
            w3 && w3.state === 'waiting' && w3.reason === '',
            JSON.stringify(w3)
        );
        await impostor.stop();
        // The genuine listener answers / but not yet /v1/version: the probe must keep waiting.
        genuine.state.versionReady = false;
        await genuine.start();
        // Wait for any request, so a probe of / fails the check below rather than this wait.
        await until(() => genuine.requests.length > 0, 'the probe to reach the genuine listener');
        await sleep(2000);
        check(
            'The probe asks /v1/version: with / answering but /v1/version not ready, the shell keeps waiting',
            genuine.requests.every((r) => r.url === '/v1/version') &&
                (await tag()) === '' &&
                (await js(main, `!!document.querySelector('[data-sm-waiting]')`)),
            `requests: ${[...new Set(genuine.requests.map((r) => r.url))].join(' ')}`
        );
        genuine.state.versionReady = true;
        await until(async () => (await tag()) === 'genuine', 'the genuine page to load');
        check(
            '…the genuine listener loads',
            (await js(main, `fetch('/api/echo?m=g1').then((r) => r.json()).then((j) => j.tag)`)) ===
                'genuine'
        );

        // 4. The pin is fixed for the app's lifetime.
        await genuine.stop();
        writeIdentity(I); // smd made a new identity on disk
        await impostor.start(); // and serves it
        const failedBefore = impostor.handshakes.failed;
        const replaced = await js(main, tryFetch('r1'));
        check(
            'Pin fixed: a new identity on disk and at the address is refused until relaunch',
            replaced.startsWith('refused:') &&
                impostor.handshakes.failed > failedBefore && // attempted, and refused at TLS
                impostor.handshakes.secure === 0 &&
                impostor.requests.length === 0,
            `${replaced}; handshakes refused ${impostor.handshakes.failed - failedBefore}`
        );
        check(
            '…the identity is never read again after the pin',
            opened.length === openedAtPin && pin.get() === fingerprintOf(G.cert),
            `opened ${openedAtPin} -> ${opened.length}`
        );
        await impostor.stop();
        await genuine.start();
        const back = await until(async () => {
            const v = await js(main, tryFetch('b1'));
            return v.startsWith('genuine') ? v : null;
        }, 'the genuine listener to recover');
        check('…and the original identity recovers', back === 'genuine:b1', back);

        let second = 'accepted';
        try {
            pin.set(fingerprintOf(I.cert));
        } catch {
            second = 'refused';
        }
        check('A second pin is refused', second === 'refused');
        check(
            'The verifier was asked only about 127.0.0.1',
            pinDecisions.every((d) => d.hostname === '127.0.0.1'),
            [...new Set(pinDecisions.map((d) => d.hostname))].join(',')
        );
        check('Nothing left the app origin', blocked.length === 0, blocked.slice(0, 3).join(' '));
    } catch (e) {
        check('Scenario completed', false, e.message);
    }

    for (const c of checks) {
        console.log(`  ${c.ok ? 'ok  ' : 'FAIL'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`);
    }
    const ok = checks.every((c) => c.ok);
    console.log(ok ? `C1d-shell PASS (${checks.length} checks)` : 'C1d-shell FAIL');
    return ok;
}
