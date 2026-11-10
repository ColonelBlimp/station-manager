// W-0027 C1 proof (ADR 0103): the shell's TLS pin, against a genuine and an impostor
// harness taking turns at one loopback address. Fixture-only: synthetic identities made
// per run, an ephemeral port, a temporary profile; never smd or the station.
//
// Every transport is first shown to WORK against the genuine listener, then shown to be
// refused by the impostor with fresh, uniquely marked attempts that are counted at the
// impostor, so a cached page or an attempt never made cannot pass as a rejection.
// PASSES only if every check holds. Run: npm run check:c1 (xvfb-run -a without a display).
import https from 'node:https';
import fs from 'node:fs';
import { BrowserWindow } from 'electron';
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

// In-page transport attempts. Each resolves to what arrived, or 'refused:<reason>'.
const tryFetch = (m) =>
    `fetch('/api/echo?m=${m}', { cache: 'no-store' }).then((r) => r.json()).then((j) => j.tag + ':' + j.m, (e) => 'refused:' + e.name)`;
const trySocket = (m) => `new Promise((resolve) => {
    let ws;
    try { ws = new WebSocket('wss://' + location.host + '/ws?m=${m}'); } catch (e) { resolve('refused:' + e.name); return; }
    const t = setTimeout(() => { try { ws.close(); } catch {} resolve('refused:timeout'); }, 4000);
    ws.onmessage = (ev) => { clearTimeout(t); ws.close(); resolve(String(ev.data)); };
    ws.onerror = () => { clearTimeout(t); resolve('refused:error'); };
})`;
const tryEvents = (m) => `new Promise((resolve) => {
    const es = new EventSource('/events?m=${m}');
    const t = setTimeout(() => { es.close(); resolve('refused:timeout'); }, 4000);
    es.onmessage = (ev) => { clearTimeout(t); es.close(); resolve(String(ev.data)); };
    es.onerror = () => { clearTimeout(t); es.close(); resolve('refused:error'); };
})`;

/** Build the two synthetic identities and the two harnesses at one port, both down. */
export async function setupC1() {
    const G = makeIdentity('genuine');
    const I = makeIdentity('impostor');
    const genuine = await startHarness({ up: false, tls: G, tag: 'genuine' });
    const impostor = await startHarness({ up: false, tls: I, tag: 'impostor', port: genuine.port });
    const fingerprint = fingerprintOf(G.cert);
    return {
        genuine,
        impostor,
        fingerprint,
        run: (ctx) => runC1Scenario({ ...ctx, genuine, impostor, fingerprint, G, I }),
    };
}

async function download(win, downloads, url) {
    const before = downloads.length;
    win.webContents.downloadURL(url);
    const rec = await until(() => downloads[before], 'the download to start', 6000).catch(
        () => null
    );
    if (!rec) return { state: 'never started' };
    await until(() => rec.state !== 'progressing', 'the download to finish', 6000).catch(() => {});
    const body =
        rec.path && fs.existsSync(rec.path) ? fs.readFileSync(rec.path, 'utf8').trim() : '';
    return { state: rec.state, body };
}

async function runC1Scenario(ctx) {
    const { getMain, blocked, rendererRequests, pinDecisions, downloads } = ctx;
    const { genuine, impostor, fingerprint, G, I } = ctx;
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const origin = genuine.origin;
    const main = getMain();
    const onWaiting = () =>
        main.webContents.getURL().startsWith('data:') &&
        js(main, `!!document.querySelector('[data-sm-waiting]')`);
    const tag = () => js(main, `document.querySelector('[data-sm-fixture]')?.dataset.tag ?? ''`);

    try {
        // 0. The copied-certificate case (the public pinned certificate with another key)
        //    cannot be served: the TLS library refuses the mismatched pair at set-up. It is
        //    therefore recorded as a fixture-setup rejection, not a handshake result; the
        //    guarantee relied on is TLS key possession (RFC 8446 §4.4.3).
        let copied = 'served';
        try {
            https.createServer({ key: I.key, cert: G.cert });
        } catch (e) {
            copied = `refused at set-up: ${e.code ?? e.message}`;
        }
        check(
            'Copied certificate without its key cannot be served (fixture-setup rejection, not a handshake)',
            copied.startsWith('refused at set-up'),
            copied
        );

        // 1. Cold start with an impostor at the address: the probe tries it and is
        //    refused by the pin; the window stays on the local waiting page.
        await until(onWaiting, 'the waiting page');
        await impostor.start();
        await sleep(3500);
        check(
            'Cold start: the impostor is tried (TLS handshakes refused) but never counts as up',
            impostor.handshakes.failed > 0 &&
                impostor.handshakes.secure === 0 &&
                (await onWaiting()),
            `handshakes refused ${impostor.handshakes.failed}, completed ${impostor.handshakes.secure}`
        );
        check(
            '…no request or upgrade ever reaches the impostor',
            impostor.requests.length === 0 && impostor.upgrades.length === 0,
            JSON.stringify({ requests: impostor.requests, upgrades: impostor.upgrades })
        );
        check(
            '…and the pin rejected it',
            pinDecisions.some((d) => !d.ok && d.hostname === '127.0.0.1') &&
                !pinDecisions.some((d) => d.ok),
            JSON.stringify(pinDecisions.slice(0, 3))
        );

        // 2. The genuine listener comes up: the window loads it, and its script runs.
        await impostor.stop();
        await genuine.start();
        const loaded = await until(
            async () => (await tag()) === 'genuine',
            'the genuine page',
            10000
        )
            .then(() => true)
            .catch(() => false);
        if (!loaded) {
            // Only when a check has already failed (the window took the impostor's page): reload
            // once so the remaining transports are still exercised and reported.
            check(
                'Genuine listener: the window moves to the genuine page by itself',
                false,
                main.webContents.getURL()
            );
            main.loadURL(origin + '/').catch(() => {});
            await until(
                async () => (await tag()) === 'genuine',
                'the genuine page after a reload',
                10000
            );
        }
        check(
            'Genuine listener: the page loads and its script runs',
            (await js(main, `window.__smFixtureScript`)) === 'genuine',
            main.webContents.getURL()
        );

        // 3. Every transport works against the genuine listener (success before rejection).
        const g = {
            fetch: await js(main, tryFetch('g1')),
            socket: await js(main, trySocket('g2')),
            events: await js(main, tryEvents('g3')),
            download: await download(main, downloads, `${origin}/download?m=g4`),
        };
        check(
            'Genuine: fetch, wss:, EventSource and a download all succeed',
            g.fetch === 'genuine:g1' &&
                g.socket === 'genuine:g2' &&
                g.events === 'genuine:g3' &&
                g.download.state === 'completed' &&
                g.download.body === 'genuine:g4',
            JSON.stringify(g)
        );

        // 4. A loaded workspace with a draft; the daemon is replaced by an impostor at the
        //    same address (the genuine connections are closed), and fresh, uniquely marked
        //    attempts are made on every transport.
        await js(
            main,
            `(() => { const i = document.createElement('input'); i.id = 'draft'; i.value = 'DRAFT-7Q5'; document.body.append(i); window.__panel = 'open'; return true; })()`
        );
        await genuine.stop();
        await impostor.start();
        // Each attempt is made on its own and must show its OWN evidence of having been tried
        // (a TLS handshake the impostor saw refused), so a skipped or still-pending operation
        // cannot pass as a rejection (codex-050).
        const attempt = async (run) => {
            const before = impostor.handshakes.failed;
            const result = await run();
            await sleep(300);
            return { result, tried: impostor.handshakes.failed - before };
        };
        const i = {
            fetch: await attempt(() => js(main, tryFetch('i1'))),
            socket: await attempt(() => js(main, trySocket('i2'))),
            events: await attempt(() => js(main, tryEvents('i3'))),
            download: await attempt(() => download(main, downloads, `${origin}/download?m=i4`)),
        };
        check(
            'Impostor after a prior success: fetch, wss: and EventSource are refused, each really attempted',
            ['fetch', 'socket', 'events'].every(
                (k) => String(i[k].result).startsWith('refused') && i[k].tried > 0
            ),
            JSON.stringify(i)
        );
        check(
            '…and the download: started, really attempted (handshake refused), never completed',
            i.download.result.state !== 'never started' &&
                i.download.result.state !== 'completed' &&
                i.download.tried > 0,
            JSON.stringify(i.download)
        );
        check(
            '…no handshake with the impostor ever completed',
            impostor.handshakes.secure === 0,
            JSON.stringify(impostor.handshakes)
        );
        check(
            '…and nothing arrived there (no request, no upgrade)',
            impostor.requests.length === 0 && impostor.upgrades.length === 0,
            JSON.stringify({ requests: impostor.requests, upgrades: impostor.upgrades })
        );
        check(
            'The loaded workspace is kept: same page, draft and panel marker intact (no waiting page)',
            (await tag()) === 'genuine' &&
                (await js(main, `document.getElementById('draft')?.value`)) === 'DRAFT-7Q5' &&
                (await js(main, `window.__panel`)) === 'open' &&
                main.webContents.getURL().startsWith(origin),
            main.webContents.getURL()
        );

        // 5. The genuine listener returns: fresh attempts succeed again, with the same pin and
        //    the draft still there.
        await impostor.stop();
        await genuine.start();
        const r = {
            fetch: await js(main, tryFetch('r1')),
            socket: await js(main, trySocket('r2')),
            events: await js(main, tryEvents('r3')),
            download: await download(main, downloads, `${origin}/download?m=r4`),
        };
        check(
            'Genuine restored: fetch, wss:, EventSource and a download recover; the draft is still there',
            r.fetch === 'genuine:r1' &&
                r.socket === 'genuine:r2' &&
                r.events === 'genuine:r3' &&
                r.download.state === 'completed' &&
                r.download.body === 'genuine:r4' &&
                (await js(main, `document.getElementById('draft')?.value`)) === 'DRAFT-7Q5',
            JSON.stringify(r)
        );
        const impostorPrint = fingerprintOf(I.cert);
        check(
            "The pin accepted only the genuine certificate, rejected the impostor's, and never changed",
            pinDecisions.some((d) => d.ok) &&
                pinDecisions.every((d) => !d.ok || d.presented === fingerprint) &&
                pinDecisions.some((d) => !d.ok && d.presented === impostorPrint) &&
                fingerprintOf(G.cert) === fingerprint,
            `${pinDecisions.filter((d) => d.ok).length} accepted, ${pinDecisions.filter((d) => !d.ok).length} rejected`
        );

        // 6. Confinement: no downgrade to http:/ws:, no other port.
        const port = genuine.port;
        const blockedBefore = blocked.length;
        const down = await js(
            main,
            `fetch('http://127.0.0.1:${port}/api/echo?m=d1').then(() => 'answered', () => 'refused')`
        );
        const downWs = await js(
            main,
            `new Promise((resolve) => { const ws = new WebSocket('ws://127.0.0.1:${port}/ws?m=d2'); ws.onopen = () => resolve('opened'); ws.onerror = () => resolve('refused'); setTimeout(() => resolve('refused:timeout'), 3000); })`
        );
        const other = await js(
            main,
            `fetch('https://127.0.0.1:${port === 65535 ? port - 1 : port + 1}/api/echo').then(() => 'answered', () => 'refused')`
        );
        const newly = blocked.slice(blockedBefore);
        check(
            'No downgrade or other port: http:, ws: and another port are cancelled by the shell',
            down === 'refused' &&
                downWs.startsWith('refused') &&
                other === 'refused' &&
                newly.some((u) => u.startsWith(`http://127.0.0.1:${port}/`)) &&
                newly.some((u) => u.startsWith(`ws://127.0.0.1:${port}/`)),
            JSON.stringify({ down, downWs, other, blocked: newly })
        );
        check(
            // Bounded: this sees only certificate verifications the network service asked for
            // (results are cached), not every connection; it caught the spellchecker download.
            'The certificate verifier was asked only about 127.0.0.1 (no spellchecker download seen)',
            pinDecisions.every((d) => d.hostname === '127.0.0.1'),
            JSON.stringify([...new Set(pinDecisions.map((d) => d.hostname))])
        );
        check(
            'No window other than the main one was created',
            BrowserWindow.getAllWindows().length === 1
        );
        const own = (u) =>
            u.startsWith(origin + '/') ||
            u.startsWith(origin.replace('https:', 'wss:') + '/') ||
            u.startsWith('data:') ||
            u.startsWith('blob:');
        check(
            'The page made requests only to its own origin (or the local waiting page)',
            rendererRequests.every(own),
            JSON.stringify(rendererRequests.filter((u) => !own(u)).slice(0, 5))
        );
    } catch (err) {
        check('scenario completed', false, String(err?.message ?? err));
    }

    await genuine.stop();
    await impostor.stop();
    const pass = checks.every((c) => c.ok);
    console.log(`C1 ${pass ? 'PASS' : 'FAIL'}`);
    for (const c of checks)
        console.log(`  ${c.ok ? 'ok  ' : 'FAIL'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`);
    return pass;
}
