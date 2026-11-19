// W-0027 C4a: the app lifetime and its admission, against a synthetic install and a TLS fixture
// harness that answers as smd does with its operating-client check on. Never smd and never the
// station: a temporary SM_WORKING_DIR, a synthetic identity and credential made per run, an
// ephemeral loopback port, temporary profiles.
//
// It proves, in order:
//  1. admission comes first: the read and the admit are answered before the page is asked for,
//     and the harness then names this app's lifetime as the current one;
//  2. every kind of request carries the lifetime exactly once, with the credential: the page, an
//     asset, a fetch, an event stream and a WebSocket; a header a page supplies is replaced;
//  3. a new daemon process is admitted to again, in a loaded workspace that is not navigated,
//     when its marker arrives on a fetch, on a stream's reconnection and on a WebSocket's
//     handshake, each tried on its own (the main process reads response headers as they
//     arrive, the WebSocket's included: codex-127);
//  4. a late response from the earlier process makes the shell ask and changes nothing;
//  5. a renderer reload, and a renderer crash that is reloaded, keep the lifetime;
//  6. a refusal of some other kind, with nothing changed, asks nothing and warns of nothing;
//     a second start has a lifetime of its own and replaces the first, in the same daemon
//     process; the first, still loaded, learns it from its next response, is told once, is
//     not navigated, and never takes the station back; a start whose lifetime the daemon has
//     already replaced shows the retired page, loads nothing and asks no more;
//  7. an admission that another one overtakes is read again and admitted again;
//  8. a read that fails keeps the window waiting, and it loads once the read answers.
// PASSES only if every check holds. Run: npm run check:c4a (xvfb-run -a without a display).
import { randomBytes } from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { startHarness } from '../harness/server.mjs';
import { makeIdentity } from './identity.mjs';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const js = (win, code, ms = 8000) =>
    Promise.race([
        win.webContents.executeJavaScript(code, true),
        new Promise((_r, reject) =>
            setTimeout(() => reject(new Error(`page call timed out: ${code.slice(0, 60)}`)), ms)
        ),
    ]);

async function until(pred, what, timeout = 12000) {
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

const LIFETIME = /^[A-Za-z0-9_-]{21}[AQgw]$/;
const ADMISSION = '/v1/app/lifetime';

/** A synthetic install with a valid identity and credential, and the genuine harness up at the
 *  app port, requiring that credential and an admission, as smd does with its check on. */
export async function setupC4a() {
    const base = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-c4a-'));
    const wd = path.join(base, 'sm');
    fs.mkdirSync(wd, { mode: 0o700 });
    const G = makeIdentity('genuine');
    const genuine = await startHarness({ up: true, tls: G, tag: 'genuine' });
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
    fs.mkdirSync(credDir, { mode: 0o700 });
    const credential = randomBytes(32).toString('base64url');
    fs.writeFileSync(path.join(credDir, 'credential'), credential, { mode: 0o600 });
    genuine.state.requireCredential = credential;
    return {
        env: { SM_WORKING_DIR: wd, HOME: os.homedir() },
        cleanup: async () => {
            await genuine.stop();
            fs.rmSync(base, { recursive: true, force: true }); // exactly the path this run made
        },
        run: (ctx) => runScenario({ ...ctx, genuine }),
    };
}

async function runScenario({ first, launch, genuine }) {
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const adm = genuine.state.admission;
    const tag = (win) => js(win, `document.querySelector('[data-sm-fixture]')?.dataset.tag ?? ''`);
    const waitingState = (win) =>
        js(win, `document.querySelector('[data-sm-waiting]')?.dataset.smState ?? ''`);
    const pathOf = (r) => new URL(r.url, 'http://x').pathname;
    const admissionsOf = (lifetime) =>
        genuine.requests
            .filter((r) => pathOf(r) === ADMISSION && r.lifetime[0] === lifetime)
            .map((r) => r.method);
    /** A new daemon process, as a restart makes one: a new marker, and nothing admitted. */
    let processes = 1;
    const newProcess = () => {
        processes += 1;
        adm.instance = `fixture-genuine-${processes}`;
        adm.generation = 0;
        adm.current = null;
        adm.retired.clear();
    };
    const fetchEcho = (win, m, init = '{ cache: "no-store" }') =>
        js(
            win,
            `fetch('/api/echo?m=${m}', ${init}).then((r) => r.ok ? r.json().then((j) => j.m) : 'status:' + r.status, (e) => 'refused:' + e.name)`
        );
    const socket = (win, m) =>
        js(
            win,
            `new Promise((res) => { const w = new WebSocket('wss://' + location.host + '/ws?m=${m}');
               w.onmessage = (ev) => { w.close(); res(ev.data); }; w.onerror = () => res('refused'); })`
        );

    try {
        const main = first.win;
        const L = first.lifetime;

        // 1. Admission comes first.
        await until(async () => (await tag(main)) === 'genuine', 'the application to load');
        const order = genuine.requests
            .filter((r) => r.cred.length === 1)
            .map((r) => r.method + ' ' + pathOf(r));
        const page = order.indexOf('GET /');
        const read = order.indexOf('GET ' + ADMISSION);
        const admit = order.indexOf('POST ' + ADMISSION);
        check(
            'The read and the admit are answered before the page is asked for',
            read >= 0 && admit > read && page > admit,
            order.slice(0, 8).join(', ')
        );
        check(
            "smd then names this app's lifetime as the current one, at generation 1",
            LIFETIME.test(L) && adm.current === L && adm.generation === 1,
            `generation ${adm.generation}`
        );

        // 2. Every kind of request carries it exactly once, with the credential.
        await js(
            main,
            `new Promise((res, rej) => { const s = document.createElement('script');
               s.src = '/asset.js?m=a1'; s.onload = () => res(window.__smAsset); s.onerror = () => rej(new Error('asset'));
               document.head.appendChild(s); })`
        );
        await fetchEcho(main, 'f1');
        await js(
            main,
            `new Promise((res, rej) => { const e = new EventSource('/events?m=s1'); window.__smStream = e;
               e.onmessage = () => res(true); e.onerror = () => rej(new Error('stream')); })`
        );
        await socket(main, 'w1');
        await fetchEcho(
            main,
            'f2',
            `{ cache: "no-store", headers: { 'X-SM-App-Lifetime': 'page-supplied', 'x-sm-app-lifetime': 'twice' } }`
        );
        const kinds = {
            'the page': genuine.requests.filter((r) => r.url === '/'),
            'an asset': genuine.requests.filter((r) => r.url.startsWith('/asset.js')),
            'a fetch': genuine.requests.filter((r) => r.url.includes('m=f1')),
            'an event stream': genuine.requests.filter((r) => r.url.includes('m=s1')),
            'a WebSocket': genuine.upgrades.filter((r) => r.url.includes('m=w1')),
            'a fetch whose page supplied a lifetime': genuine.requests.filter((r) =>
                r.url.includes('m=f2')
            ),
        };
        for (const [kind, seen] of Object.entries(kinds)) {
            check(
                `${kind} carries the app's lifetime, once, with the credential`,
                seen.length > 0 &&
                    seen.every(
                        (r) => r.lifetime.length === 1 && r.lifetime[0] === L && r.cred.length === 1
                    ),
                JSON.stringify(seen.map((r) => r.lifetime))
            );
        }

        // 3. A new daemon process, learnt from each carrier on its own, with no navigation.
        await js(main, `window.__smKept = 'typed'; window.__smStream.close(); true`);
        const reAdmittedBy = async (name, trigger) => {
            newProcess();
            const before = admissionsOf(L).length;
            await trigger();
            await until(() => adm.current === L, `re-admission after ${name}`);
            check(
                `A new daemon process is admitted to again when its marker arrives on ${name}`,
                adm.current === L && adm.generation === 1 && admissionsOf(L).length >= before + 2,
                `${adm.instance}; admissions ${admissionsOf(L).slice(before).join(',')}`
            );
        };
        await reAdmittedBy('a fetch', () => fetchEcho(main, 'r1'));
        await reAdmittedBy('a WebSocket handshake', () => socket(main, 'r2'));
        // A stream that reconnects by itself: opened under one process, cut, and answered by the next.
        await js(
            main,
            `new Promise((res, rej) => { const e = new EventSource('/events?m=r3'); window.__smStream = e;
               e.onmessage = () => res(true); e.onerror = () => rej(new Error('stream')); })`
        );
        const streams = () => genuine.requests.filter((r) => r.url.includes('m=r3'));
        await reAdmittedBy("a stream's own reconnection", async () => {
            await genuine.stop(); // every connection is cut; the port is kept
            await genuine.start();
            await until(() => streams().length >= 2, 'the stream to reconnect', 20000);
        });
        check(
            'The reconnection carried the lifetime too',
            streams().every((r) => r.lifetime.length === 1 && r.lifetime[0] === L),
            `${streams().length} stream requests`
        );
        check(
            'The workspace was not navigated: what was there is still there',
            (await js(main, `window.__smKept`)) === 'typed' && (await tag(main)) === 'genuine'
        );

        // 4. A late response from the earlier process: asked about, and nothing changes.
        const readsBefore = admissionsOf(L).filter((m) => m === 'GET').length;
        const postsBefore = admissionsOf(L).filter((m) => m === 'POST').length;
        adm.oneOffMarker = 'an-earlier-process';
        await fetchEcho(main, 'late');
        await until(
            () => admissionsOf(L).filter((m) => m === 'GET').length > readsBefore,
            'the shell to ask after a late marker'
        );
        await sleep(400);
        check(
            'A late response from an earlier process makes the shell ask, and admits nothing again',
            admissionsOf(L).filter((m) => m === 'POST').length === postsBefore &&
                adm.current === L &&
                adm.generation === 1,
            `reads ${admissionsOf(L).filter((m) => m === 'GET').length - readsBefore}, admits 0`
        );
        // And the admission's own responses, which carry the marker too, start nothing.
        const settled = admissionsOf(L).length;
        await sleep(600);
        check(
            "The admission's own responses start no further asking",
            admissionsOf(L).length === settled
        );

        // 5. A renderer reload, and a crash that is reloaded, keep the lifetime.
        const pagesBefore = genuine.requests.filter((r) => r.url === '/').length;
        main.webContents.reload();
        await until(
            async () =>
                genuine.requests.filter((r) => r.url === '/').length > pagesBefore &&
                (await tag(main)) === 'genuine' &&
                (await js(main, `window.__smKept`)) === undefined,
            'the reloaded page'
        );
        const afterReload = genuine.requests.filter((r) => r.url === '/').at(-1);
        check(
            'A renderer reload keeps the lifetime, and replaces nothing',
            afterReload.lifetime[0] === L && adm.current === L && adm.generation === 1
        );
        const gone = new Promise((r) => main.webContents.once('render-process-gone', r));
        main.webContents.forcefullyCrashRenderer();
        await gone;
        const pagesCrashed = genuine.requests.filter((r) => r.url === '/').length;
        main.webContents.reload();
        await until(
            async () =>
                genuine.requests.filter((r) => r.url === '/').length > pagesCrashed &&
                (await tag(main)) === 'genuine',
            'the page after a renderer crash',
            20000
        );
        const afterCrash = genuine.requests.filter((r) => r.url === '/').at(-1);
        const afterCrashFetch = await fetchEcho(main, 'c1');
        check(
            'A renderer crash that is reloaded keeps the lifetime, on the page and on what it then asks',
            afterCrash.lifetime[0] === L &&
                afterCrashFetch === 'c1' &&
                genuine.requests
                    .filter((r) => r.url.includes('m=c1'))
                    .every((r) => r.lifetime[0] === L) &&
                adm.current === L
        );

        // 6. A refusal of another kind changes nothing. Then a second start replaces the
        // first, and the first learns of it.
        const quiet = admissionsOf(L).length;
        const conflict = await js(
            main,
            `fetch('/api/conflict', { cache: 'no-store' }).then((r) => r.status)`
        );
        await sleep(500);
        check(
            'A refusal of some other kind, with nothing changed, asks nothing and warns of nothing',
            conflict === 409 && admissionsOf(L).length === quiet && first.retired.length === 0
        );
        await js(main, `window.__smKept = 'typed again'; true`);
        const second = launch('c4a-second');
        await until(async () => (await tag(second.win)) === 'genuine', 'the second start to load');
        check(
            'A second start has a lifetime of its own, and its admission replaces the first',
            LIFETIME.test(second.lifetime) &&
                second.lifetime !== L &&
                adm.current === second.lifetime &&
                adm.retired.has(L) &&
                adm.generation === 2
        );
        // The first window is still loaded. Its next response comes from the same daemon
        // process, with the same marker and a later generation (codex-129, finding 2).
        const before6 = admissionsOf(L).length;
        await fetchEcho(main, 'replaced');
        await until(() => first.retired.length === 1, 'the replaced window to be told');
        await fetchEcho(main, 'replaced-again');
        await sleep(600);
        check(
            'An app replaced within the same daemon process learns it from its next response and is told once',
            first.retired.length === 1 && first.admission.isRetired(),
            `asked ${admissionsOf(L).slice(before6).join(',')}`
        );
        check(
            '...it is not navigated, asks no more, and does not take the station back',
            (await js(main, `window.__smKept`)) === 'typed again' &&
                (await tag(main)) === 'genuine' &&
                admissionsOf(L).slice(before6).join(',') === 'GET,POST' &&
                adm.current === second.lifetime &&
                adm.generation === 2
        );
        const third = launch('c4a-third');
        adm.retired.add(third.lifetime); // the daemon has already replaced this one
        await until(async () => (await waitingState(third.win)) === 'retired', 'the retired page');
        const thirdAsked = admissionsOf(third.lifetime).length;
        await sleep(1200); // longer than the waiting loop's pause, were it still asking
        check(
            'A start whose lifetime has been replaced shows the retired page, loads nothing and asks no more',
            (await tag(third.win).catch(() => '')) === '' &&
                admissionsOf(third.lifetime).length === thirdAsked &&
                admissionsOf(third.lifetime).join(',') === 'GET,POST' &&
                adm.current === second.lifetime &&
                third.retired.length === 0,
            `asked ${admissionsOf(third.lifetime).join(',')}`
        );

        // 7. An admission that another one overtakes is read again and admitted again.
        let overtaken = false;
        adm.holdAdmit = async () => {
            if (overtaken) return;
            overtaken = true;
            genuine.replaceLifetime('an-app-that-died'); // lands between the read and the admit
        };
        const fourth = launch('c4a-fourth');
        await until(
            async () => (await tag(fourth.win)) === 'genuine',
            'the overtaken start to load'
        );
        adm.holdAdmit = null;
        check(
            'A stale admission is read again and admitted again, and then stands',
            admissionsOf(fourth.lifetime).join(',') === 'GET,POST,GET,POST' &&
                adm.current === fourth.lifetime &&
                adm.retired.has('an-app-that-died'),
            admissionsOf(fourth.lifetime).join(',')
        );

        // 8. A read that fails keeps the window waiting; it loads once the read answers.
        adm.failReads = 3;
        const fifth = launch('c4a-fifth');
        await until(() => adm.failReads < 3, 'a failed read');
        check(
            'While the read fails the window waits and the application is not loaded',
            (await waitingState(fifth.win).catch(() => '')) !== '' &&
                (await tag(fifth.win).catch(() => '')) === ''
        );
        await until(
            async () => (await tag(fifth.win)) === 'genuine',
            'the fifth start to load',
            20000
        );
        check(
            'Once the read answers the app is admitted and loads',
            adm.current === fifth.lifetime && adm.failReads === 0
        );
        check(
            'No window was told it was retired while it was the one starting',
            [second, fourth, fifth].every((l) => l.retired.length === 0)
        );
    } catch (e) {
        check('Scenario completed', false, e.message);
    }

    for (const c of checks) {
        console.log(`  ${c.ok ? 'ok  ' : 'FAIL'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`);
    }
    const ok = checks.every((c) => c.ok);
    console.log(ok ? `C4a PASS (${checks.length} checks)` : 'C4a FAIL');
    return ok;
}
