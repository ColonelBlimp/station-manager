// W-0027 A3, first slice: the app's close, in the real app against a REAL smd. Never the live
// station: smd is the binary named by SM_DESKTOP_SMD, run with a temporary SM_WORKING_DIR,
// synthetic data and free loopback ports, with bridge, FT8 and evidence off. Nothing is keyed
// and no rig is addressed: FT8 is off in this smd, so the close's disarm meets "no such route".
//
// The window's own close is not wired to this flow yet (the question that goes before it is the
// second slice), so the scenario calls each launch's `closing.close()` itself. Everything the
// close is made of is the app's own: its session, admission, request filter, window and, for
// the last step, its own rule for quitting when the last window closes.
//
// One Electron run, two starts of the app (each launch makes its own app lifetime).
//  1. the first start is admitted, the workspace loads, and one contact is made a member of
//     its session (the member row is the scenario's, as in scenario-recovery.mjs);
//  2. a close whose disarm gets no answer: nothing of the exit is sent, the box is of the
//     first kind and the window is kept. While the box is up the page can read and cannot
//     write. Cancel: the window and the page's writes come back, and smd still says this
//     start holds its session;
//  3. the close again, with nothing in the way: the disarm, the read and the exit go out in
//     that order, smd acknowledges, the window goes. The archive file holds the session
//     closed as `app_closed` and the exit recorded;
//  4. the second start: the workspace, and no question about the session just closed;
//  5. its close, under the app's own quit rule, with the exit's answer lost: the exit is
//     stored and the app cannot know. The window is closed BEFORE the box, and the app is
//     still running with no window. A second start of the app and a second close add
//     nothing, and another request to quit does not end the app under the box. Try again
//     sends no second disarm: smd answers "already recorded", and the app quits by itself.
// PASSES only if every check holds. Run: SM_DESKTOP_SMD=<smd> npm run check:close.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { app, BrowserWindow } from 'electron';
import { isCloseRequest } from '../src/closing.mjs';
import { launchTrusted } from '../src/launch.mjs';
import { externalOpener } from '../src/window.mjs';
import {
    HEADING,
    LOG_ONE,
    SESSION,
    WORKSPACE,
    answers,
    freePort,
    js,
    until,
    writeConfig,
} from './scenario-recovery.mjs';
import { managedProcess, releaseAfter } from './smd-process.mjs';

// What the page can do while a close is under way: a read, and a write. The write is a disarm,
// which this smd (FT8 off) answers 404 when it is let through: harmless either way.
const PAGE_READ = `fetch('/v1/version', { cache: 'no-store' }).then((r) => r.status, () => 'refused')`;
const PAGE_WRITE = `fetch('/v1/ft8/tx/arm', { method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: '{"armed":false}' }).then((r) => r.status, () => 'refused')`;

/** The shell's own requests of a close, seen and steered in its session object (as the
 *  recovery scenario holds the shell's admission). The page's requests do not go through this.
 *  `next[path]` is a one-shot behaviour: 'no-answer' sends nothing and fails; 'lose-answer'
 *  sends the request for real and then loses its answer. */
function watchClose(win) {
    const ses = win.webContents.session;
    const send = ses.fetch.bind(ses);
    const w = { sent: [], next: {} };
    ses.fetch = async (url, opts = {}) => {
        if (!isCloseRequest(String(url))) return send(url, opts);
        const pathname = new URL(String(url)).pathname;
        w.sent.push(`${opts.method ?? 'GET'} ${pathname}`);
        const how = w.next[pathname];
        delete w.next[pathname];
        if (how === 'no-answer') throw new Error('scenario: no answer');
        const res = await send(url, opts);
        if (how === 'lose-answer') throw new Error('scenario: the answer was lost');
        return res;
    };
    return w;
}

/** The archive file, read by the scenario after smd has written it. */
function archive(data) {
    const db = new DatabaseSync(path.join(data, 'db', 'station-manager.db'), { readOnly: true });
    try {
        return {
            sessions: db
                .prepare(
                    'SELECT id, closed_at IS NOT NULL AS closed, close_reason FROM operating_session ORDER BY opened_at'
                )
                .all(),
            exits: db
                .prepare('SELECT app_lifetime FROM operating_app_exit')
                .all()
                .map((r) => r.app_lifetime),
        };
    } finally {
        db.close();
    }
}

export async function runClose({ adopt, useInstalledQuitRule }) {
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const windows = [];
    let base = null;
    let smd = null;

    try {
        const bin = process.env.SM_DESKTOP_SMD;
        if (!bin || !fs.existsSync(bin)) {
            throw new Error('set SM_DESKTOP_SMD to an smd built with the SPA embedded');
        }
        base = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-close-real-'));
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

        // Each launch gets a box and a quit of the scenario's own. The box is answered from a
        // script, and what was true while it was up is looked at inside it.
        const launch = (partition) => {
            const l = { boxes: [], answers: [], quits: 0, quit: null };
            Object.assign(
                l,
                launchTrusted({
                    env,
                    offscreen: true,
                    openExternal: externalOpener(() => {}),
                    partition,
                    retryMs: 300,
                    askUnconfirmed: async (state) => {
                        l.boxes.push(state);
                        return l.answers.shift()(state);
                    },
                    quit: () => {
                        l.quits++;
                        l.quit?.();
                    },
                })
            );
            windows.push(l);
            return l;
        };

        // 1. The first start, and one contact in its session.
        const first = launch(null);
        adopt(first); // the app's own window and close, to the main process's handlers
        await until(() => js(first.win, WORKSPACE), "the first start's workspace");
        const opened = await js(first.win, SESSION);
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
        const sessionId = opened.session?.id ?? '';
        check(
            'The first start holds a session with one contact',
            opened.state === 'open' && opened.session?.held_by_you === true && logged === 201,
            `logged ${logged}; ${JSON.stringify(opened)}`
        );

        // 2. A close whose disarm gets no answer: first kind, and Cancel.
        const watch1 = watchClose(first.win);
        watch1.next['/v1/ft8/tx/arm'] = 'no-answer';
        const during = {};
        first.answers.push(async () => {
            during.destroyed = first.win.isDestroyed();
            during.visible = first.win.isVisible();
            during.closing = first.admission.isClosing();
            during.read = await js(first.win, PAGE_READ);
            during.write = await js(first.win, PAGE_WRITE);
            app.emit('second-instance'); // a second start of the app, under this box
            await new Promise((r) => setTimeout(r, 200));
            during.visibleAfterSecondStart = first.win.isVisible();
            return 'cancel';
        });
        const cancelled = await first.closing.close();
        check(
            'A disarm that gets no answer: nothing of the exit is sent, and the box is of the first kind',
            cancelled === 'cancelled' &&
                JSON.stringify(watch1.sent) === JSON.stringify(['POST /v1/ft8/tx/arm']) &&
                JSON.stringify(first.boxes) ===
                    JSON.stringify([{ kind: 'first', disarmFailed: true, terminal: false }]),
            JSON.stringify({ cancelled, sent: watch1.sent, boxes: first.boxes })
        );
        check(
            'While that box is up the window is kept and hidden (a second start of the app does not show it), the app is admitted nowhere new, and the page reads but cannot write',
            during.destroyed === false &&
                during.visible === false &&
                during.closing === true &&
                during.read === 200 &&
                during.write === 'refused' &&
                during.visibleAfterSecondStart === false,
            JSON.stringify(during)
        );
        const after = {
            visible: first.win.isVisible(),
            closing: first.admission.isClosing(),
            write: await js(first.win, PAGE_WRITE),
            session: await js(first.win, SESSION),
            file: archive(data),
            quits: first.quits,
        };
        check(
            'Cancel gives back the window, the page’s writes and the admission; smd still says this start holds its session, and no exit is recorded',
            after.visible === true &&
                after.closing === false &&
                after.write === 404 &&
                after.session.state === 'open' &&
                after.session.session?.id === sessionId &&
                after.session.session?.held_by_you === true &&
                after.file.exits.length === 0 &&
                after.quits === 0,
            JSON.stringify({ ...after, session: after.session.state, file: after.file })
        );

        // 3. The close again, with nothing in the way.
        watch1.sent.length = 0;
        const closed = await first.closing.close();
        const file3 = archive(data);
        check(
            'A close with nothing in the way sends the disarm, the read and the exit, in that order, and smd acknowledges',
            closed === 'closed' &&
                JSON.stringify(watch1.sent) ===
                    JSON.stringify([
                        'POST /v1/ft8/tx/arm',
                        'GET /v1/operating-session',
                        'POST /v1/operating-session/exit',
                    ]) &&
                first.boxes.length === 1,
            JSON.stringify({ closed, sent: watch1.sent, boxes: first.boxes.length })
        );
        check(
            '…the window is gone, the app is told to quit once, and the archive file holds the session closed as app_closed and the exit recorded',
            first.win.isDestroyed() &&
                first.quits === 1 &&
                file3.sessions.length === 1 &&
                file3.sessions[0].id === sessionId &&
                file3.sessions[0].closed === 1 &&
                file3.sessions[0].close_reason === 'app_closed' &&
                JSON.stringify(file3.exits) === JSON.stringify([first.lifetime]),
            JSON.stringify({
                quits: first.quits,
                sessions: file3.sessions,
                exits: file3.exits.length,
            })
        );

        // 4. The second start: no question about the session that was closed.
        const second = launch('close-second-start');
        adopt(second);
        await until(() => js(second.win, WORKSPACE), "the second start's workspace");
        const reopened = await js(second.win, SESSION);
        check(
            'The next start is shown the workspace and is not asked about the session just closed',
            (await js(second.win, HEADING)) !== 'The last session is still open' &&
                reopened.state === 'open' &&
                reopened.session?.held_by_you === true &&
                reopened.session?.id !== sessionId,
            JSON.stringify(reopened)
        );

        // 5. Its close, under the app's own quit rule, with the exit's answer lost.
        const events = [];
        const note = (name) => () => events.push(name);
        app.on('window-all-closed', note('window-all-closed'));
        app.on('before-quit', note('before-quit'));
        // Every quit is held from here on: the scenario still has its verdict to give, and
        // a run that ended at the app's quit would exit 0 with nothing said. (It did, when
        // the quit rule was broken on purpose: the app quit under the box and the second
        // quit, at the acknowledgement, was let through.) The verdict's own exit is
        // app.exit(), which does not ask.
        let quitSeen;
        const quitting = new Promise((resolve) => (quitSeen = resolve));
        app.on('will-quit', (e) => {
            e.preventDefault();
            events.push('will-quit');
            quitSeen();
        });
        useInstalledQuitRule();
        second.quit = () => app.quit();
        const watch2 = watchClose(second.win);
        watch2.next['/v1/operating-session/exit'] = 'lose-answer';
        const box = {};
        second.answers.push(async () => {
            await until(
                () => events.includes('window-all-closed'),
                'the last window to close',
                5000
            );
            await new Promise((r) => setTimeout(r, 300)); // had the app quit, it would have by now
            box.windows = BrowserWindow.getAllWindows().length;
            box.events = [...events];
            box.exits = archive(data).exits;
            app.emit('second-instance'); // a second start of the app while the box is up
            const again = second.closing.close(); // a second close
            app.quit(); // and another request to quit: the app's own, not the close's
            await new Promise((r) => setTimeout(r, 300));
            box.windowsAfter = BrowserWindow.getAllWindows().length;
            box.boxesAfter = second.boxes.length;
            box.sentAfter = watch2.sent.length;
            box.eventsAfter = [...events];
            box.again = again;
            return 'retry';
        });
        const finished = await second.closing.close();
        check(
            'An exit whose answer is lost: the window is closed before the box, which is of the second kind',
            JSON.stringify(second.boxes) ===
                JSON.stringify([{ kind: 'second', disarmFailed: false, terminal: false }]) &&
                box.windows === 0,
            JSON.stringify({ boxes: second.boxes, windows: box.windows })
        );
        check(
            '…the app is still running with no window (its own quit rule did not end it), and smd had stored the exit',
            JSON.stringify(box.events) === JSON.stringify(['window-all-closed']) &&
                box.exits.includes(second.lifetime),
            JSON.stringify({ events: box.events, stored: box.exits.includes(second.lifetime) })
        );
        check(
            '…a second start of the app and a second close add nothing: no window, no second box, no request',
            box.windowsAfter === 0 &&
                box.boxesAfter === 1 &&
                box.sentAfter === 3 &&
                (await box.again) === 'closed',
            JSON.stringify({
                windows: box.windowsAfter,
                boxes: box.boxesAfter,
                sent: box.sentAfter,
            })
        );
        check(
            '…and another request to quit, made while the box is up, does not end the app under it',
            JSON.stringify(box.eventsAfter) ===
                JSON.stringify(['window-all-closed', 'before-quit']),
            JSON.stringify(box.eventsAfter)
        );
        const quit = await Promise.race([
            quitting.then(() => true),
            new Promise((r) => setTimeout(() => r(false), 10000)),
        ]);
        check(
            'Try again: smd answers that the exit is already recorded, and the app then quits by itself',
            finished === 'closed' &&
                quit === true &&
                JSON.stringify(watch2.sent) ===
                    JSON.stringify([
                        'POST /v1/ft8/tx/arm',
                        'GET /v1/operating-session',
                        'POST /v1/operating-session/exit',
                        // Try again: no second disarm once an exit was sent.
                        'GET /v1/operating-session',
                        'POST /v1/operating-session/exit',
                    ]) &&
                // The refused quit asked first; the close's own is the one that went on.
                JSON.stringify(events) ===
                    JSON.stringify([
                        'window-all-closed',
                        'before-quit',
                        'before-quit',
                        'will-quit',
                    ]) &&
                second.quits === 1,
            JSON.stringify({
                finished,
                quit,
                sent: watch2.sent.length,
                events,
                quits: second.quits,
            })
        );
        const file5 = archive(data);
        check(
            'The archive file holds both exits and no open session',
            file5.exits.length === 2 &&
                file5.exits.includes(first.lifetime) &&
                file5.exits.includes(second.lifetime) &&
                file5.sessions.every((s) => s.closed === 1),
            JSON.stringify({ exits: file5.exits.length, sessions: file5.sessions })
        );
    } catch (e) {
        check('Scenario completed', false, e.message);
    } finally {
        for (const w of windows) if (w.win && !w.win.isDestroyed()) w.win.destroy();
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
    console.log(ok ? `Close PASS (${checks.length} checks)` : 'Close FAIL');
    return ok;
}
