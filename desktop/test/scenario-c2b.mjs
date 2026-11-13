// W-0027 C2b: the operating credential the shell presents (ADR 0103 point 7), against a
// synthetic install and TLS fixture harnesses. Never smd and never the station: a temporary
// SM_WORKING_DIR, a synthetic identity and credentials made per run, ephemeral loopback ports,
// temporary profiles.
//
// It proves, in order:
//  1. no credential yet: the shell waits and sends nothing, although the genuine listener is up;
//  2. an unsafe credential: the waiting page names the path and the reason, never the value,
//     and still nothing is sent;
//  3. a valid one: the pin, origin and credential are fixed; the probe, the page, an asset, a
//     fetch, an event stream and a WebSocket each carry the credential exactly once;
//  4. a header a page supplies is replaced, and the page's own request never holds the value;
//  5. it goes nowhere else: another port (with the same certificate), a redirect off the
//     origin, a plain-HTTP downgrade, an impostor at the address; and a reconnect carries it;
//  6. the daemon replaces its credential: a loaded workspace is told once, is not navigated,
//     keeps what was typed, and the shell reads nothing again;
//  7. a window started before the new credential is on disk waits, refused, and says so; it
//     never adopts the new file; a window started afterwards presents the new credential.
//  8. the refusal first shows where there is no window or no status (a shared worker's request,
//     a WebSocket upgrade): the workspace is still told; and a 403 of another kind is asked
//     about once and told to no one.
// PASSES only if every check holds. Run: npm run check:c2b (xvfb-run -a without a display).
import { randomBytes } from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { startHarness } from '../harness/server.mjs';
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

const newCredential = () => randomBytes(32).toString('base64url');

/** The synthetic install with a valid identity and no credential; the genuine harness up at the
 *  app port, an impostor for the same port (down), and a second listener on another port that
 *  serves the SAME certificate, so only the origin rule keeps the credential from it. */
export async function setupC2b() {
    const base = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-c2b-'));
    const wd = path.join(base, 'sm');
    fs.mkdirSync(wd, { mode: 0o700 });
    const G = makeIdentity('genuine');
    const I = makeIdentity('impostor');
    // Both reserve the one port while down; the genuine one is then brought up.
    const genuine = await startHarness({ up: false, tls: G, tag: 'genuine' });
    const impostor = await startHarness({ up: false, tls: I, tag: 'impostor', port: genuine.port });
    await genuine.start();
    const other = await startHarness({ up: true, tls: G, tag: 'other-port' });
    fs.writeFileSync(
        path.join(wd, 'config.json'),
        JSON.stringify({ server: { app_port: genuine.port } }),
        { mode: 0o600 }
    );
    const idDir = path.join(wd, 'desktop-identity');
    fs.mkdirSync(idDir, { mode: 0o700 });
    fs.writeFileSync(path.join(idDir, 'key.pem'), G.key, { mode: 0o600 });
    fs.writeFileSync(path.join(idDir, 'cert.pem'), G.cert, { mode: 0o644 });
    const credDir = path.join(wd, 'desktop-credential');
    const credFile = path.join(credDir, 'credential');
    return {
        env: { SM_WORKING_DIR: wd, HOME: os.homedir() },
        cleanup: async () => {
            await genuine.stop();
            await impostor.stop();
            await other.stop();
            fs.rmSync(base, { recursive: true, force: true }); // exactly the path this run made
        },
        run: (ctx) => runScenario({ ...ctx, genuine, impostor, other, credDir, credFile }),
    };
}

async function runScenario(ctx) {
    const { first, launch, genuine, impostor, other, credDir, credFile } = ctx;
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const main = first.win;
    const waitingPage = (win) =>
        js(
            win,
            `(() => { const w = document.querySelector('[data-sm-waiting]');
              return w ? { state: w.dataset.smState, text: document.body.innerText,
                           reason: document.querySelector('[data-sm-reason]')?.textContent ?? '',
                           path: document.querySelector('[data-sm-path]')?.textContent ?? '' } : null; })()`
        );
    const tag = (win) => js(win, `document.querySelector('[data-sm-fixture]')?.dataset.tag ?? ''`);
    const echo = (win, m, init = '{ cache: "no-store" }') =>
        js(
            win,
            `fetch('/api/echo?m=${m}', ${init}).then((r) => r.ok ? r.json().then((j) => j.tag + ':' + j.m) : 'status:' + r.status, (e) => 'refused:' + e.name)`
        );
    // A fetch made by a blob-backed SharedWorker: Electron reports it with no window at all.
    const workerFetch = (win, m) =>
        js(
            win,
            `new Promise((res, rej) => {
               const src = 'onconnect = (e) => { const p = e.ports[0]; fetch(' + JSON.stringify(location.origin + '/api/echo?m=${m}') +
                 ', { cache: "no-store" }).then((r) => p.postMessage("status:" + r.status), (err) => p.postMessage("refused:" + err.name)); };';
               const w = new SharedWorker(URL.createObjectURL(new Blob([src], { type: 'text/javascript' })));
               w.onerror = () => rej(new Error('shared worker'));
               w.port.onmessage = (ev) => res(ev.data);
               w.port.start(); })`
        );
    // A WebSocket attempt: what arrived, or 'refused' when the upgrade failed.
    const socket = (win, m) =>
        js(
            win,
            `new Promise((res) => { const w = new WebSocket('wss://' + location.host + '/ws?m=${m}');
               w.onmessage = (ev) => { w.close(); res(ev.data); }; w.onerror = () => res('refused'); })`
        );
    const probes = (h) => h.requests.filter((r) => r.url.startsWith('/v1/version')).length;
    const sentTo = (h) => h.handshakes.secure + h.handshakes.failed + h.requests.length;
    const reqFor = (h, part) => h.requests.filter((r) => r.url.includes(part));
    const V1 = newCredential();
    const V2 = newCredential();

    try {
        // 1. No credential yet, with the genuine listener up and the identity valid.
        const w1 = await until(async () => {
            const p = await waitingPage(main);
            return p?.reason.includes('operating credential') ? p : null;
        }, 'the waiting page saying smd has not created its credential');
        await sleep(2000); // several discovery rounds
        check(
            'No credential: the shell waits and says what for',
            w1.state === 'waiting' && w1.path.endsWith('desktop-credential'),
            JSON.stringify(w1)
        );
        check(
            '…and nothing is sent to the listener, nothing pinned, no credential held',
            sentTo(genuine) === 0 && first.pin.get() === null && first.cred.get() === null,
            JSON.stringify({ handshakes: genuine.handshakes, requests: genuine.requests.length })
        );

        // 2. An unsafe credential: readable by others.
        fs.mkdirSync(credDir, { mode: 0o700 });
        fs.writeFileSync(credFile, V1, { mode: 0o644 });
        fs.chmodSync(credFile, 0o644);
        const w2 = await until(async () => {
            const p = await waitingPage(main);
            return p?.state === 'refused' ? p : null;
        }, 'the waiting page refusing the credential');
        check(
            'Unsafe credential: the waiting page names the path and the reason, as the credential',
            w2.reason.includes('0600') &&
                w2.path.endsWith(path.join('desktop-credential', 'credential')) &&
                w2.text.includes('operating credential'),
            JSON.stringify({ reason: w2.reason, path: w2.path })
        );
        check('…never the value', !w2.text.includes(V1));
        check(
            '…and still nothing is sent, pinned or held',
            sentTo(genuine) === 0 && first.pin.get() === null && first.cred.get() === null
        );

        // 3. Made safe: fixed together, and every kind of request carries it exactly once.
        fs.chmodSync(credFile, 0o600);
        await until(async () => (await tag(main)) === 'genuine', 'the genuine page to load');
        const openedAtFix = first.opened.length;
        check(
            'Valid credential: the pin, the origin and the credential are fixed',
            first.pin.get() !== null &&
                first.cred.get() === V1 &&
                first.target.origin === genuine.origin
        );
        await js(
            main,
            `new Promise((res, rej) => { const s = document.createElement('script');
               s.src = '/asset.js?m=a1'; s.onload = () => res(window.__smAsset); s.onerror = () => rej(new Error('asset'));
               document.head.appendChild(s); })`
        );
        const fetched = await echo(main, 'f1');
        const sse = await js(
            main,
            `new Promise((res, rej) => { const e = new EventSource('/events?m=s1');
               e.onmessage = (m) => { e.close(); res(m.data); }; e.onerror = () => { e.close(); rej(new Error('sse')); }; })`
        );
        const ws = await js(
            main,
            `new Promise((res, rej) => { const w = new WebSocket('wss://' + location.host + '/ws?m=w1');
               w.onmessage = (m) => { w.close(); res(m.data); }; w.onerror = () => rej(new Error('ws')); })`
        );
        check(
            'A fetch, an event stream and a WebSocket are served',
            fetched === 'genuine:f1' && sse === 'genuine:s1' && ws === 'genuine:w1',
            `${fetched} ${sse} ${ws}`
        );
        const kinds = {
            'the probe': reqFor(genuine, '/v1/version'),
            'the page': genuine.requests.filter((r) => r.url === '/'),
            'an asset': reqFor(genuine, '/asset.js'),
            'a fetch': reqFor(genuine, 'm=f1'),
            'an event stream': reqFor(genuine, '/events'),
            'a WebSocket upgrade': genuine.upgrades.filter((u) => u.url.includes('m=w1')),
        };
        for (const [kind, seen] of Object.entries(kinds)) {
            check(
                `…${kind} carries the credential exactly once`,
                seen.length > 0 && seen.every((r) => r.cred.length === 1 && r.cred[0] === V1),
                `${seen.length} request(s); header counts ${seen.map((r) => r.cred.length).join(',')}`
            );
        }
        const all = [...genuine.requests, ...genuine.upgrades];
        check(
            '…and so does every other request the listener received',
            all.every((r) => r.cred.length === 1 && r.cred[0] === V1),
            all
                .filter((r) => r.cred.length !== 1)
                .map((r) => r.url)
                .join(' ')
        );

        const viaWorker = await workerFetch(main, 'sw1');
        const sw1 = reqFor(genuine, 'm=sw1');
        check(
            '…a shared worker’s request, which names no window, carries it too',
            viaWorker === 'status:200' &&
                sw1.length === 1 &&
                sw1[0].cred.length === 1 &&
                sw1[0].cred[0] === V1,
            `${viaWorker}; ${sw1.length} request(s)`
        );

        // 4. A page cannot supply the header, and never holds the value.
        const before4 = genuine.requests.length;
        await echo(
            main,
            'p1',
            `{ cache: 'no-store', headers: { 'x-sm-operating-credential': 'from-the-page', 'X-SM-Operating-Credential': 'again' } }`
        );
        const p1 = genuine.requests.slice(before4).filter((r) => r.url.includes('m=p1'));
        check(
            'A header the page supplies is replaced: exactly one value arrives, the shell’s',
            p1.length === 1 && p1[0].cred.length === 1 && p1[0].cred[0] === V1,
            JSON.stringify(p1.map((r) => r.cred.map((c) => (c === V1 ? 'the credential' : c))))
        );
        const pageView = await js(
            main,
            `(() => { const r = new Request('/api/echo?m=p2');
               return { has: r.headers.has('x-sm-operating-credential'),
                        html: document.documentElement.outerHTML.length,
                        seen: document.documentElement.outerHTML.includes(${JSON.stringify(V1)}) ||
                              JSON.stringify(performance.getEntries()).includes(${JSON.stringify(V1)}) }; })()`
        );
        check(
            'The page’s own request, document and timing entries do not hold the value',
            pageView.has === false && pageView.seen === false,
            JSON.stringify(pageView)
        );

        // 5. Nowhere else.
        const elsewhere = await js(
            main,
            `fetch(${JSON.stringify(other.origin + '/api/echo?m=o1')}).then((r) => 'status:' + r.status, (e) => 'refused:' + e.name)`
        );
        const viaRedirect = await js(
            main,
            `fetch('/redirect?to=' + encodeURIComponent(${JSON.stringify(other.origin + '/api/echo?m=o2')})).then((r) => 'status:' + r.status, (e) => 'refused:' + e.name)`
        );
        const downgrade = await js(
            main,
            `fetch(${JSON.stringify(genuine.origin.replace('https:', 'http:') + '/api/echo?m=d1')}).then((r) => 'status:' + r.status, (e) => 'refused:' + e.name)`
        );
        await sleep(500);
        check(
            'Another port serving the same certificate receives nothing, directly or by redirect',
            elsewhere.startsWith('refused:') &&
                viaRedirect.startsWith('refused:') &&
                sentTo(other) === 0,
            `${elsewhere} ${viaRedirect}; other listener saw ${sentTo(other)}`
        );
        const marked = first.sent.filter((r) => r.sent).map((r) => r.url);
        const wssPartner = genuine.origin.replace('https:', 'wss:') + '/';
        check(
            '…the shell added the credential only to requests for the pinned origin or its wss partner',
            marked.length > 0 &&
                marked.every(
                    (u) => u.startsWith(genuine.origin + '/') || u.startsWith(wssPartner)
                ) &&
                first.sent.some((r) => !r.sent), // it saw other requests (the local pages) and left them
            `${marked.length} of ${first.sent.length} requests marked`
        );
        check(
            'A plain-HTTP downgrade of the origin is refused',
            downgrade.startsWith('refused:'),
            downgrade
        );

        await genuine.stop();
        await impostor.start();
        const failedBefore = impostor.handshakes.failed;
        const toImpostor = await echo(main, 'i1');
        check(
            'An impostor at the address is refused at TLS: no request, so no credential, reaches it',
            toImpostor.startsWith('refused:') &&
                impostor.handshakes.failed > failedBefore &&
                impostor.handshakes.secure === 0 &&
                impostor.requests.length === 0,
            `${toImpostor}; ${JSON.stringify(impostor.handshakes)}`
        );
        await impostor.stop();
        await genuine.start();
        const back = await until(async () => {
            const v = await echo(main, 'b1');
            return v.startsWith('genuine') ? v : null;
        }, 'the genuine listener to recover');
        const b1 = reqFor(genuine, 'm=b1');
        check(
            'A reconnect after the listener returns carries the credential',
            back === 'genuine:b1' &&
                b1.length > 0 &&
                b1.every((r) => r.cred.length === 1 && r.cred[0] === V1)
        );

        // 5b. A 403 of another kind: the shell asks smd once, tells nothing, and its own question
        // (which is refused the same way) does not prompt another (codex-112).
        const probesBefore = probes(genuine);
        genuine.state.forbid = 'cross_origin';
        const other403 = await echo(main, 'x1');
        await sleep(2500);
        const asked = probes(genuine) - probesBefore;
        genuine.state.forbid = null;
        check(
            'Another kind of 403: the shell asks smd exactly once and tells the operator nothing',
            other403 === 'status:403' && asked === 1 && first.refusals.length === 0,
            `${other403}; asked ${asked} time(s); told ${first.refusals.length}`
        );

        // 6. The daemon replaces its credential under a loaded workspace.
        await js(main, `(window.__draft = 'kept', true)`);
        const urlBefore = main.webContents.getURL();
        genuine.state.requireCredential = V2;
        const refusedFetch = await echo(main, 'r1');
        await until(() => first.refusals.length > 0, 'the shell to report the refusal');
        for (const m of ['r2', 'r3', 'r4']) await echo(main, m);
        await sleep(1500);
        check(
            'Credential replaced: the page’s request is refused and the shell says so, once',
            refusedFetch === 'status:403' && first.refusals.length === 1,
            `${refusedFetch}; told ${first.refusals.length} time(s)`
        );
        check(
            '…the workspace is not navigated and keeps what was typed',
            main.webContents.getURL() === urlBefore &&
                (await tag(main)) === 'genuine' &&
                (await js(main, `window.__draft`)) === 'kept'
        );
        check(
            '…and the shell reads no file again and holds the same credential',
            first.opened.length === openedAtFix && first.cred.get() === V1,
            `opened ${openedAtFix} -> ${first.opened.length}`
        );

        // 7. A window started while the old credential is still on disk: refused, and it says so.
        const second = launch('c2b-second');
        const w7 = await until(async () => {
            const p = await waitingPage(second.win);
            return p?.state === 'unaccepted' ? p : null;
        }, 'the second window to show that the daemon did not accept it');
        check(
            'A window with the old credential waits, and says the daemon did not accept it',
            w7.text.includes('did not accept') &&
                w7.text.includes('smctl restart') &&
                !w7.text.includes(V1),
            w7.text.replace(/\s+/g, ' ').slice(0, 160)
        );
        check(
            '…with no separate report: the waiting page is the explanation',
            second.refusals.length === 0
        );

        // smd's new credential reaches the disk; only a newly started window adopts it.
        fs.writeFileSync(credFile, V2, { mode: 0o600 });
        const third = launch('c2b-third');
        await until(async () => (await tag(third.win)) === 'genuine', 'the third window to load');
        const t1 = await echo(third.win, 't1');
        const seenT1 = reqFor(genuine, 'm=t1');
        check(
            'A window started after the replacement presents the new credential and is served',
            t1 === 'genuine:t1' &&
                third.cred.get() === V2 &&
                seenT1.every((r) => r.cred.length === 1 && r.cred[0] === V2),
            t1
        );
        await sleep(1200); // the second window has probed again since the file changed
        const stillWaiting = await waitingPage(second.win);
        check(
            '…while the running ones keep the credential they started with',
            second.cred.get() === V1 &&
                first.cred.get() === V1 &&
                stillWaiting?.state === 'unaccepted',
            JSON.stringify(stillWaiting?.state)
        );
        // 8. codex-112: the refusal first shows on a path with no window or no status. Two more
        // windows, loaded with the current credential; then smd replaces it again, and in each
        // window the one request made is of one such kind.
        const V3 = newCredential();
        const viaShared = launch('c2b-shared-worker');
        const viaSocket = launch('c2b-websocket');
        await until(
            async () => (await tag(viaShared.win)) === 'genuine',
            'the worker window to load'
        );
        await until(
            async () => (await tag(viaSocket.win)) === 'genuine',
            'the socket window to load'
        );
        const served = [await workerFetch(viaShared.win, 'sw2'), await socket(viaSocket.win, 'k2')];
        genuine.state.requireCredential = V3;
        await sleep(300);
        const quiet = [viaShared, viaSocket, third].map((l) => l.refusals.length).join(',');

        const sharedRefused = await workerFetch(viaShared.win, 'sw3');
        await until(
            () => viaShared.refusals.length > 0,
            'the shell to report a shared worker’s refusal'
        );
        check(
            'A shared worker’s request is the only one refused: the workspace is still told, once',
            served[0] === 'status:200' &&
                sharedRefused === 'status:403' &&
                viaShared.refusals.length === 1,
            `${served[0]} then ${sharedRefused}; told ${viaShared.refusals.length}`
        );

        const upgradesBefore = genuine.upgrades.length;
        const socketRefused = await socket(viaSocket.win, 'k3');
        await until(() => viaSocket.refusals.length > 0, 'the shell to report a refused WebSocket');
        const k3 = genuine.upgrades.slice(upgradesBefore);
        check(
            'A WebSocket upgrade is the only request refused: the workspace is still told, once',
            served[1] === 'genuine:k2' &&
                socketRefused === 'refused' &&
                k3.length === 1 &&
                viaSocket.refusals.length === 1,
            `${served[1]} then ${socketRefused}; ${k3.length} upgrade(s); told ${viaSocket.refusals.length}`
        );
        await sleep(1500);
        check(
            '…neither window is navigated or told again, and a window that asked for nothing is told nothing',
            (await tag(viaShared.win)) === 'genuine' &&
                (await tag(viaSocket.win)) === 'genuine' &&
                viaShared.refusals.length === 1 &&
                viaSocket.refusals.length === 1 &&
                quiet === '0,0,0' &&
                third.refusals.length === 0,
            `before any request ${quiet}; third window told ${third.refusals.length}`
        );

        let again = 'accepted';
        try {
            first.cred.set(V2);
        } catch {
            again = 'refused';
        }
        check('A second credential is refused', again === 'refused');
        const leaked = [first, second, third].flatMap((l) => l.blocked).join(' ');
        check(
            'No blocked address carries a credential',
            !leaked.includes(V1) && !leaked.includes(V2)
        );
    } catch (e) {
        check('Scenario completed', false, e.message);
    }

    for (const c of checks) {
        console.log(`  ${c.ok ? 'ok  ' : 'FAIL'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`);
    }
    const ok = checks.every((c) => c.ok);
    console.log(ok ? `C2b PASS (${checks.length} checks)` : 'C2b FAIL');
    return ok;
}
