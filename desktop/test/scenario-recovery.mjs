// W-0029 part 4: the recovery question, in the real app against a REAL smd (ADR 0100 §6; the
// brief names this harness for "the question before the workspace", which only Electron shows).
// Never the live station: smd is the binary named by SM_DESKTOP_SMD, run with a temporary
// SM_WORKING_DIR, synthetic data and free loopback ports, with bridge, FT8 and evidence off.
//
// One Electron run, two starts of the app. Each launch makes its own app lifetime
// (src/launch.mjs), so the second launch is to smd what a restart of the app is.
//  1. the first start is admitted and smd opens its session; the workspace loads;
//  2. a contact is logged through smd's own route and made a member of that session. Nothing
//     in smd writes a member row yet (W-0029 part 5), so the scenario writes that one row
//     into the archive file itself; everything else is smd's;
//  3. the first window goes without a confirmed exit, as after a crash or a power cut;
//  4. the second start: what loads is the question, naming the session and its one contact,
//     and nothing of the workspace;
//  5. Continue: the workspace loads, and smd says this start now holds that same session;
//  6. smd is stopped and started again under the running app. The page's stream reconnects
//     and it reads the session again, possibly before the shell has admitted its lifetime to
//     the new process: it must not be asked about the session it holds itself (the
//     clean-room review of 2c362c3d). The scenario waits until it has SEEN the page consume a
//     state answered by the restarted smd (the process's own marker tells the two apart), and
//     fails if that never happens.
// PASSES only if every check holds. Run: SM_DESKTOP_SMD=<smd> npm run check:recovery.
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { launchTrusted } from '../src/launch.mjs';
import { externalOpener } from '../src/window.mjs';
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

function writeConfig(wd, data, browserPort, appPort) {
    const cfg = {
        version: 7,
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

const launch = (env, partition) =>
    launchTrusted({
        env,
        offscreen: true,
        openExternal: externalOpener(() => {}),
        partition,
        retryMs: 300,
    });

// In the page: what the page shows, and what smd says of the session to this start.
const WORKSPACE = `(() => { const f = document.querySelector('#lc-call');
    return !!f && !f.disabled && !f.readOnly && !!document.querySelector('.content-wrap'); })()`;
const HEADING = `document.querySelector('h1')?.textContent?.trim() ?? ''`;
const TEXT = `document.body.textContent.replace(/\\s+/g, ' ')`;
// The scenario's own query. It goes round the observer below, once that is installed.
const SESSION = `(window.__smRealFetch ?? fetch)('/v1/operating-session', { cache: 'no-store' }).then((r) => r.json())`;
// A passive observer of the page's own reads of the session (codex-228, codex-229). It wraps
// the page's fetch and, for an answered read of the session, the response's json(): the page
// reads the body through that (lib/api/_helpers.ts, readJsonBody), so a read is recorded when
// the page has CONSUMED its state, not when a response merely became available. Each record
// carries the daemon instance that answered it (smd's own marker on the response), so a read
// answered by the process that was stopped cannot be taken for one from the process started
// after it. It sends nothing. The scenario's own queries go through the fetch saved here
// (window.__smRealFetch) and are never recorded.
//
// `hold` is a control (SM_RECOVERY_CONTROL=hold-consumption): the body of a read answered by
// any instance but `before` is never given to the page. The page then has not consumed its
// state, and the scenario must not get past its wait.
const WATCH_SESSION_READS = (hold) => `(() => {
    if (window.__smSession !== undefined) return true;
    const real = window.fetch.bind(window);
    window.__smRealFetch = real;
    window.__smSession = { consumedFrom: [], before: null, hold: ${hold ? 'true' : 'false'} };
    window.fetch = async (...args) => {
        const response = await real(...args);
        const url = typeof args[0] === 'string' ? args[0] : (args[0]?.url ?? '');
        if (new URL(url, location.href).pathname !== '/v1/operating-session' || !response.ok) {
            return response;
        }
        const instance = response.headers.get('x-sm-daemon-instance') ?? '';
        const json = response.json.bind(response);
        response.json = async () => {
            const watch = window.__smSession;
            if (watch.hold && watch.before !== null && instance !== watch.before) {
                return new Promise(() => {}); // the control: the page never gets its state
            }
            const body = await json();
            watch.consumedFrom.push(instance);
            return body;
        };
        return response;
    };
    return true;
})()`;
// The daemon instance answering now, asked by the scenario itself.
const INSTANCE = `(window.__smRealFetch ?? fetch)('/v1/operating-session', { cache: 'no-store' })
    .then((r) => r.headers.get('x-sm-daemon-instance') ?? '')`;
const NOTE_INSTANCE = (instance) =>
    `(() => { window.__smSession.before = ${JSON.stringify(instance)}; return true; })()`;
// Has the page consumed a session state answered by an instance other than the one noted?
const CONSUMED_FROM_ANOTHER = `window.__smSession.consumedFrom.some(
    (i) => i !== '' && i !== window.__smSession.before)`;
// Once its body is consumed the page decodes and draws within the same turn of its loop; two
// turns later it has.
const SETTLED = `new Promise((r) => setTimeout(() => setTimeout(() => r(true), 50), 50))`;
const press = (label) =>
    `(() => { const b = [...document.querySelectorAll('button')].find((x) => x.textContent.trim() === ${JSON.stringify(label)});
        if (!b) return false; b.click(); return true; })()`;
// One contact, logged by smd's own route as the page's submit would name it.
const LOG_ONE = `(async () => {
    const version = await (await fetch('/v1/version', { cache: 'no-store' })).json();
    const config = await (await fetch('/v1/config', { cache: 'no-store' })).json();
    const adif = '<CALL:5>DL9UW<BAND:3>20m<MODE:3>SSB<FREQ:6>14.210<QSO_DATE:8>20261029<TIME_ON:4>1200<TIME_OFF:4>1200<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:6>N0CALL<EOR>';
    const r = await fetch('/v1/qso?logbook=' + config.default_logbook.id, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/x-adif',
            'X-SM-Archive-Id': version.archive.id,
            'X-SM-Default-Logbook': version.archive.default_logbook_uuid,
        },
        body: adif,
    });
    return r.status;
})()`;

export async function runRecovery() {
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const windows = [];
    let base = null;
    let smd = null;
    // A control, not a mode of the product: see WATCH_SESSION_READS. With it set the run
    // must FAIL, at phase 6's wait.
    const holdConsumption = process.env.SM_RECOVERY_CONTROL === 'hold-consumption';

    try {
        const bin = process.env.SM_DESKTOP_SMD;
        if (!bin || !fs.existsSync(bin)) {
            throw new Error('set SM_DESKTOP_SMD to an smd built with the SPA embedded');
        }
        base = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-recovery-real-'));
        const wd = path.join(base, 'wd');
        const data = path.join(base, 'data');
        fs.mkdirSync(wd, { mode: 0o700 });
        fs.mkdirSync(data, { mode: 0o700 });
        const browserPort = await freePort();
        const appPort = await freePort();
        writeConfig(wd, data, browserPort, appPort);
        smd = managedProcess(bin, ['--config', path.join(wd, 'config.json')], {
            env: { PATH: process.env.PATH, HOME: os.homedir(), SM_WORKING_DIR: wd },
            ready: answers(browserPort),
        });
        const env = { SM_WORKING_DIR: wd, HOME: os.homedir() };
        await smd.start();

        // 1. The first start: admitted, its session opened, the workspace shown.
        const first = launch(env, null);
        windows.push(first);
        await until(() => js(first.win, WORKSPACE), "the first start's workspace");
        const opened = await js(first.win, SESSION);
        check(
            'The first start is given a session of its own and shown the workspace, with no question',
            opened.state === 'open' && opened.session?.held_by_you === true,
            JSON.stringify(opened)
        );
        const sessionId = opened.session?.id ?? '';

        // 2. One contact, a member of that session.
        const logged = await js(first.win, LOG_ONE);
        const db = new DatabaseSync(path.join(data, 'db', 'station-manager.db'));
        try {
            db.exec('PRAGMA busy_timeout = 5000');
            db.prepare(
                `INSERT INTO operating_session_member (qso_id, session_id)
                 SELECT (SELECT MAX(id) FROM qso), id FROM operating_session WHERE closed_at IS NULL`
            ).run();
        } finally {
            db.close();
        }
        const withContact = await js(first.win, SESSION);
        check(
            "The session has one contact (the member row is the scenario's; the contact and the count are smd's)",
            logged === 201 &&
                withContact.session?.id === sessionId &&
                withContact.session?.has_contacts === true &&
                withContact.session?.contact_count === 1,
            `logged ${logged}; ${JSON.stringify(withContact.session)}`
        );

        // 3. The first window goes with no confirmed exit: smd is told nothing.
        first.win.destroy();

        // 4. The second start: the question, and nothing of the workspace.
        const second = launch(env, 'recovery-second-start');
        windows.push(second);
        await until(
            () => js(second.win, HEADING).then((h) => h === 'The last session is still open'),
            'the recovery question'
        );
        const text = await js(second.win, TEXT);
        const shown = {
            workspace: await js(second.win, `!!document.querySelector('.content-wrap')`),
            form: await js(second.win, `!!document.querySelector('#lc-call')`),
            nav: await js(second.win, `!!document.querySelector('nav')`),
            buttons: await js(
                second.win,
                `[...document.querySelectorAll('button')].map((b) => b.textContent.trim())`
            ),
            title: await js(second.win, 'document.title'),
        };
        check(
            'A second start meets that session: the question is what loads, naming it and its one contact',
            /The session that started \d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC has 1 contact and was never closed\./.test(
                text
            ),
            text.slice(0, 220)
        );
        check(
            '…and nothing of the workspace is shown: no contact form, no rail, only Continue and New',
            !shown.workspace &&
                !shown.form &&
                !shown.nav &&
                JSON.stringify(shown.buttons) ===
                    JSON.stringify(['Continue this session', 'Start a new session']) &&
                // A development build of smd puts its own marker before the view's name.
                /(^|· )Session · Station Manager$/.test(shown.title),
            JSON.stringify(shown)
        );
        const asked = await js(second.win, SESSION);
        check(
            'smd names the state recovery pending, and says this start does not hold the session',
            asked.state === 'recovery_pending' &&
                asked.session?.id === sessionId &&
                asked.session?.held_by_you === false &&
                second.lifetime !== first.lifetime,
            JSON.stringify(asked)
        );

        // 5. Continue: the workspace, and smd says this start holds that same session.
        const pressed = await js(second.win, press('Continue this session'));
        await until(() => js(second.win, WORKSPACE), 'the workspace after Continue');
        const continued = await js(second.win, SESSION);
        check(
            'After Continue the workspace is shown, and smd says this start holds the same session with its contact',
            pressed === true &&
                continued.state === 'open' &&
                continued.session?.id === sessionId &&
                continued.session?.held_by_you === true &&
                continued.session?.contact_count === 1 &&
                continued.generation === asked.generation + 1,
            JSON.stringify(continued)
        );
        check(
            '…and the question is gone',
            (await js(second.win, HEADING)) !== 'The last session is still open'
        );

        // 6. smd restarts under the running app, which holds a session with a contact.
        // The scenario asks smd nothing after the restart until the page has consumed a state
        // from the new process: a request of its own would make the shell admit early and
        // hide the interval this phase is about. What it waits for is seen, not timed.
        await js(second.win, WATCH_SESSION_READS(holdConsumption));
        const instanceBefore = await js(second.win, INSTANCE);
        await js(second.win, NOTE_INSTANCE(instanceBefore));
        await smd.stop();
        await smd.start();
        await until(
            () => js(second.win, CONSUMED_FROM_ANOTHER),
            'the page to consume its own read of the session from the restarted smd',
            30000
        );
        await js(second.win, SETTLED); // what that state drew
        const afterRestart = {
            heading: await js(second.win, HEADING),
            workspace: await js(second.win, WORKSPACE),
            session: await js(second.win, SESSION),
        };
        const instanceAfter = await js(second.win, INSTANCE);
        check(
            'After smd restarts under the running app the workspace stays: the app is not asked about the session it holds',
            instanceBefore !== '' &&
                instanceAfter !== '' &&
                instanceAfter !== instanceBefore &&
                afterRestart.heading !== 'The last session is still open' &&
                afterRestart.workspace === true &&
                afterRestart.session.session?.id === sessionId &&
                afterRestart.session.generation === continued.generation,
            JSON.stringify({ heading: afterRestart.heading, workspace: afterRestart.workspace })
        );
    } catch (e) {
        check('Scenario completed', false, e.message);
    } finally {
        for (const w of windows) if (!w.win.isDestroyed()) w.win.destroy();
        // The scratch tree goes only once smd's exit is confirmed; otherwise the run fails and
        // names what it kept.
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
    console.log(ok ? `Recovery PASS (${checks.length} checks)` : 'Recovery FAIL');
    return ok;
}
