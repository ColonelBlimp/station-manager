// W-0029 part 6, slice 3: the Session panel's New and Continue, in the real app against a
// REAL smd (criteria E, F, D3). Never the live station: smd is the binary named by
// SM_DESKTOP_SMD, run with a temporary SM_WORKING_DIR, synthetic data and free loopback
// ports, with bridge, FT8 and evidence off. Nothing is keyed and no rig is addressed.
//
// The page's own tests stub the daemon. What only this run shows is the two together: that
// what the panel sends is what smd accepts, and that what the panel then shows is what smd
// holds. Each control is pressed in the page as the operator presses it; smd is asked, by
// the scenario's own requests, what it holds; and the two are compared.
//
//  1. the app starts, smd opens its session, and a contact is logged: the panel shows it;
//  2. "New session" is pressed. smd holds another session, open and empty; the panel shows
//     that one: no row, a count of 0, a clock started again. The session closed had a
//     contact, so smd kept it;
//  3. "Continue…" is offered. Its list has one line, for the session closed, with its one
//     contact;
//  4. the line is chosen. smd holds the FIRST session again, open, in a new sitting; the
//     panel shows its contact and a clock started again. The session left had no contact,
//     so smd discarded it: it is not in the list;
//  5. a second contact is logged: smd puts it in the continued session, and the panel shows
//     both. It is still so after a reload: the page holds nothing of its own;
//  6. the default logbook is changed to a second logbook, and back again (criterion F9; ADR
//     0100 §8's acceptance, here through the panel). Each change is asked of smd by its own
//     route and takes effect at smd's restart, which smd asks for itself and the scenario
//     performs, as the service manager does on the station. After the first, smd holds a
//     session of the second logbook; after the second, a new session of the first logbook,
//     and the first session, closed by the change, is in Continue's list;
//  7. that session is chosen in the panel. smd holds it again; a third contact joins it;
//     and it stays open.
// PASSES only if every check holds. Run: SM_DESKTOP_SMD=<smd> npm run check:controls.
//
// A control, not a mode of the product: with SM_CONTROLS_CONTROL=skip-new the scenario does
// not press "New session", and the run must FAIL at step 2's wait. It shows that the run
// cannot pass on a page that did nothing.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { launchTrusted } from '../src/launch.mjs';
import { externalOpener } from '../src/window.mjs';
import { managedProcess, releaseAfter } from './smd-process.mjs';
import {
    SESSION,
    WORKSPACE,
    answers,
    freePort,
    js,
    until,
    writeConfig,
} from './scenario-recovery.mjs';

// In the page: press the first button with exactly this text, or this title.
const press = (label) =>
    `(() => { const b = [...document.querySelectorAll('button')].find((x) =>
        x.textContent.trim() === ${JSON.stringify(label)} || x.getAttribute('title') === ${JSON.stringify(label)});
        if (!b || b.disabled) return false; b.click(); return true; })()`;
// What the Session panel shows: its heading ("Session (1) Started 09:15 UTC"), the callsigns
// of its rows, what it says when it has none, the header's clock, and what it says of a
// control. '' and [] while the panel is not on the page.
const PANEL = `(() => {
    const text = (e) => (e ? e.textContent.replace(/\\s+/g, ' ').trim() : '');
    const heading = [...document.querySelectorAll('h3')].find((e) => /^Session \\(/.test(text(e)));
    const card = heading ? heading.closest('.card') : null;
    return {
        heading: text(heading),
        calls: card ? [...card.querySelectorAll('tbody tr td:nth-child(2)')].map(text) : [],
        empty: text(document.querySelector('[data-testid="session-empty"]')),
        clock: text(document.querySelector('[data-testid="session-timer"]')),
        said: text(document.querySelector('[data-testid="session-control-error"], [data-testid="session-unknown"]')),
    };
})()`;
// The lines of Continue's list, as the dialog shows them.
const LINES = `[...document.querySelectorAll('[role="dialog"] ul button')].map((b) => b.textContent.replace(/\\s+/g, ' ').trim())`;
const CHOOSE_FIRST_LINE = `(() => { const b = document.querySelector('[role="dialog"] ul button');
    if (!b || b.disabled) return false; b.click(); return true; })()`;
// smd's list of closed sessions, asked by the scenario itself.
const CLOSED = `fetch('/v1/operating-session/closed?limit=20', { cache: 'no-store' }).then((r) => r.json())`;
// One contact, logged by smd's own route as the page's submit would name it. The page's
// form is not used: the panel can then only know of the contact from smd.
const log = (call, time) => `(async () => {
    const version = await (await fetch('/v1/version', { cache: 'no-store' })).json();
    const config = await (await fetch('/v1/config', { cache: 'no-store' })).json();
    const call = ${JSON.stringify(call)};
    const adif = '<CALL:' + call.length + '>' + call + '<BAND:3>20m<MODE:3>SSB<FREQ:6>14.210<QSO_DATE:8>20261029<TIME_ON:4>${time}<TIME_OFF:4>${time}<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:6>N0CALL<EOR>';
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
// The line of Continue's list that ends with this count, chosen as the operator chooses it.
const chooseLineWith = (count) => `(() => {
    const b = [...document.querySelectorAll('[role="dialog"] ul button')].find((x) =>
        x.textContent.replace(/\\s+/g, ' ').trim().endsWith(${JSON.stringify(count)}));
    if (!b || b.disabled) return false; b.click(); return true; })()`;
// A second logbook for the same station, made by smd's own route.
const MAKE_LOGBOOK = `(async () => {
    const version = await (await fetch('/v1/version', { cache: 'no-store' })).json();
    const r = await fetch('/v1/logbook', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-SM-Archive-Id': version.archive.id },
        body: JSON.stringify({ name: 'Second', callsign: 'N0CALL' }),
    });
    const all = await (await fetch('/v1/logbook', { cache: 'no-store' })).json();
    return { status: r.status, logbooks: all };
})()`;
// What smd says its default logbook is, and of a change of it.
const DEFAULT = `fetch('/v1/default-logbook', { cache: 'no-store' }).then((r) => r.json())`;
// A change of the default, asked of smd by its own route. smd then asks for its restart.
const changeDefault = (to, from) => `(async () => {
    const version = await (await fetch('/v1/version', { cache: 'no-store' })).json();
    const r = await fetch('/v1/default-logbook', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-SM-Archive-Id': version.archive.id },
        body: JSON.stringify({ logbook_uuid: ${JSON.stringify(to)}, believed_default_uuid: ${JSON.stringify(from)} }),
    });
    return { status: r.status, body: await r.json().catch(() => null) };
})()`;
// The record a change leaves is cleared before another change is accepted: what the page's
// own banner does for the operator.
const clearChangeRecord = (requestId) => `(async () => {
    const version = await (await fetch('/v1/version', { cache: 'no-store' })).json();
    const r = await fetch('/v1/default-logbook/retry-cleanup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-SM-Archive-Id': version.archive.id },
        body: JSON.stringify({ request_id: ${JSON.stringify(requestId)} }),
    });
    return r.status;
})()`;
// The header's clock in seconds; -1 while it shows no time.
const seconds = (clock) => {
    const m = /^(\d+):(\d{2}):(\d{2})$/.exec(clock);
    return m ? Number(m[1]) * 3600 + Number(m[2]) * 60 + Number(m[3]) : -1;
};
// "The clock started again" can only be seen if it had run for a while: a sitting a second
// old looks the same before a control and after it. So the scenario lets the clock reach
// this many seconds before each control, and requires fewer after it.
const RUN_FOR = 4;
const clockAtLeast = (win, n) =>
    until(
        () => js(win, PANEL).then((p) => (seconds(p.clock) >= n ? seconds(p.clock) : null)),
        `the clock to reach ${n} s`,
        15000
    );

export async function runControls() {
    const checks = [];
    const check = (name, ok, detail = '') => checks.push({ name, ok: Boolean(ok), detail });
    const windows = [];
    let base = null;
    let smd = null;
    const skipNew = process.env.SM_CONTROLS_CONTROL === 'skip-new';

    try {
        const bin = process.env.SM_DESKTOP_SMD;
        if (!bin || !fs.existsSync(bin)) {
            throw new Error('set SM_DESKTOP_SMD to an smd built with the SPA embedded');
        }
        base = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-controls-real-'));
        const wd = path.join(base, 'wd');
        const data = path.join(base, 'data');
        fs.mkdirSync(wd, { mode: 0o700 });
        fs.mkdirSync(data, { mode: 0o700 });
        const browserPort = await freePort();
        const appPort = await freePort();
        writeConfig(wd, data, browserPort, appPort);
        // SM_SELF_RESTART=1 is the respawn contract: smd may ask for its own restart (a
        // change of the default logbook needs one), and whoever runs it starts it again.
        // Here that is this scenario (restartAsAsked, below).
        smd = managedProcess(bin, ['--config', path.join(wd, 'config.json')], {
            env: {
                PATH: process.env.PATH,
                HOME: os.homedir(),
                SM_WORKING_DIR: wd,
                SM_SELF_RESTART: '1',
            },
            ready: answers(browserPort),
        });
        // smd has asked for its restart and is going down by itself: wait until it no longer
        // answers, collect its exit, and start it again, as the service manager does.
        const restartAsAsked = async () => {
            await until(
                async () => !(await answers(browserPort)()),
                'smd to stop answering after asking for its restart',
                30000
            );
            const exit = await smd.stop();
            await smd.start();
            return exit;
        };
        await smd.start();

        // 1. The app starts; a contact is logged; the panel shows it.
        const app = launchTrusted({
            env: { SM_WORKING_DIR: wd, HOME: os.homedir() },
            offscreen: true,
            openExternal: externalOpener(() => {}),
            partition: null,
            retryMs: 300,
        });
        windows.push(app);
        const win = app.win;
        await until(() => js(win, WORKSPACE), 'the workspace');
        const first = await js(win, SESSION);
        const firstId = first.session?.id ?? '';
        const logged = await js(win, log('DL9UW', '1200'));
        await until(() => js(win, press('Session — show')), 'the Session tile');
        let panel = await until(
            () => js(win, PANEL).then((p) => (p.calls.includes('DL9UW') ? p : null)),
            'the first contact in the panel',
            10000
        );
        check(
            'A contact logged in the session smd opened for this start is shown by the panel',
            first.state === 'open' &&
                firstId !== '' &&
                logged === 201 &&
                panel.heading.startsWith('Session (1)') &&
                panel.calls.length === 1,
            `logged ${logged}; ${JSON.stringify(panel)}`
        );

        // 2. New.
        const ranBeforeNew = await clockAtLeast(win, RUN_FOR);
        const pressedNew = skipNew
            ? true
            : await until(() => js(win, press('New session')), 'New to be offered', 10000);
        const afterNew = await until(
            () => js(win, SESSION).then((s) => (s.session && s.session.id !== firstId ? s : null)),
            'smd to hold another session after New'
        );
        const secondId = afterNew.session?.id ?? '';
        panel = await until(
            // The new session's rows have been read (and are none), not only its count shown.
            () =>
                js(win, PANEL).then((p) =>
                    p.heading.startsWith('Session (0)') &&
                    p.empty === 'No QSOs logged this session.'
                        ? p
                        : null
                ),
            'the panel to show the new session, read and empty',
            10000
        );
        check(
            'New: smd holds another session, open and empty, at a later generation, and the panel shows that one with a clock started again',
            pressedNew === true &&
                afterNew.state === 'open' &&
                afterNew.session.held_by_you === true &&
                afterNew.session.contact_count === 0 &&
                afterNew.generation > first.generation &&
                panel.calls.length === 0 &&
                panel.empty === 'No QSOs logged this session.' &&
                seconds(panel.clock) >= 0 &&
                seconds(panel.clock) < ranBeforeNew &&
                panel.said === '',
            `clock before ${ranBeforeNew} s; ${JSON.stringify(afterNew)}; ${JSON.stringify(panel)}`
        );
        let closed = await js(win, CLOSED);
        check(
            'New: the session closed had a contact, so smd kept it, with its one contact',
            closed.sessions?.length === 1 &&
                closed.sessions[0].id === firstId &&
                closed.sessions[0].contact_count === 1 &&
                closed.generation === afterNew.generation,
            JSON.stringify(closed)
        );

        // 3. Continue is offered, and its list is smd's.
        // Found and pressed in one call to the page: between two calls the button can go
        // (it is offered only while there is something to continue), and a press that
        // found nothing would leave the scenario waiting for a dialog it never opened.
        await until(() => js(win, press('Continue…')), 'Continue to be offered and pressed', 10000);
        const lines = await until(
            () => js(win, LINES).then((l) => (l.length > 0 ? l : null)),
            "the lines of Continue's list",
            10000
        );
        check(
            "Continue's list has one line, for the session closed, with its count",
            lines.length === 1 && / UTC · 1 contact$/.test(lines[0]),
            JSON.stringify(lines)
        );

        // 4. The line is chosen.
        const ranBeforeContinue = await clockAtLeast(win, RUN_FOR);
        // A line is shown before it can be chosen (while the list is being read again, or
        // is of another generation than the state the page shows): the scenario presses it
        // as the operator would, once it can be pressed (the clean-room review of e94ffc3a).
        const chose = await until(
            () => js(win, CHOOSE_FIRST_LINE),
            'the line to be one that can be chosen',
            15000
        );
        const continued = await until(
            () =>
                js(win, SESSION).then((s) =>
                    s.session?.id === firstId && s.generation > afterNew.generation ? s : null
                ),
            'smd to hold the first session again'
        );
        panel = await until(
            () => js(win, PANEL).then((p) => (p.calls.includes('DL9UW') ? p : null)),
            "the continued session's contact in the panel",
            10000
        );
        check(
            'Continue: smd holds the first session again, open, in a new sitting, and the panel shows its contact with a clock started again',
            chose === true &&
                continued.state === 'open' &&
                continued.session.held_by_you === true &&
                continued.session.contact_count === 1 &&
                continued.session.opened_at === first.session.opened_at &&
                continued.session.sitting_started_at !== first.session.sitting_started_at &&
                panel.heading.startsWith('Session (1)') &&
                panel.calls.length === 1 &&
                seconds(panel.clock) >= 0 &&
                seconds(panel.clock) < ranBeforeContinue &&
                panel.said === '',
            `clock before ${ranBeforeContinue} s; ${JSON.stringify(continued)}; ${JSON.stringify(panel)}`
        );
        closed = await js(win, CLOSED);
        check(
            'Continue: the session left had no contact, so smd discarded it, and the one continued is not in the list',
            Array.isArray(closed.sessions) &&
                closed.sessions.every((s) => s.id !== secondId && s.id !== firstId),
            JSON.stringify(closed)
        );

        // 5. The next contact joins the continued session; and after a reload.
        const loggedAgain = await js(win, log('PA3XYZ', '1215'));
        panel = await until(
            () => js(win, PANEL).then((p) => (p.calls.includes('PA3XYZ') ? p : null)),
            'the second contact in the panel',
            10000
        );
        const withTwo = await js(win, SESSION);
        check(
            'The next contact joins the continued session, and the panel shows both',
            loggedAgain === 201 &&
                withTwo.session?.id === firstId &&
                withTwo.session.contact_count === 2 &&
                panel.heading.startsWith('Session (2)') &&
                panel.calls.includes('DL9UW') &&
                panel.calls.length === 2,
            `logged ${loggedAgain}; ${JSON.stringify(withTwo.session)}; ${JSON.stringify(panel)}`
        );
        const loaded = new Promise((resolve) => win.webContents.once('did-finish-load', resolve));
        win.webContents.reload();
        await loaded;
        await until(() => js(win, WORKSPACE), 'the workspace after a reload');
        await until(
            () => js(win, press('Session — show')),
            'the Session tile after a reload'
        ).catch(
            () => false // the tile's place is remembered: it may already be showing
        );
        panel = await until(
            () => js(win, PANEL).then((p) => (p.calls.length === 2 ? p : null)),
            "the continued session's contacts after a reload",
            10000
        );
        check(
            'After a reload the panel still shows the continued session and both its contacts',
            panel.heading.startsWith('Session (2)') &&
                panel.calls.includes('DL9UW') &&
                panel.calls.includes('PA3XYZ'),
            JSON.stringify(panel)
        );

        // 6. The default logbook goes to a second logbook, and back.
        const made = await js(win, MAKE_LOGBOOK);
        const firstLogbook = first.session?.logbook_uuid ?? '';
        const secondLogbook =
            (Array.isArray(made.logbooks) ? made.logbooks : (made.logbooks?.logbooks ?? []))
                .map((l) => l.uuid)
                .find((u) => u && u !== firstLogbook) ?? '';
        const moves = [];
        for (const [to, from] of [
            [secondLogbook, firstLogbook],
            [firstLogbook, secondLogbook],
        ]) {
            const asked = await js(win, changeDefault(to, from));
            const exit = asked.status === 202 ? await restartAsAsked() : null;
            await until(() => js(win, WORKSPACE), 'the workspace after the default changed', 40000);
            const now = await until(
                () => js(win, DEFAULT).then((d) => (d.effective?.uuid === to ? d : null)),
                'smd to serve the new default'
            );
            let cleared = 0;
            if (now.change?.unconfirmed === true) {
                cleared = await js(win, clearChangeRecord(now.change.request_id));
            }
            const session = await until(
                () => js(win, SESSION).then((x) => (x.state === 'open' && x.session ? x : null)),
                'a session of the new default'
            );
            moves.push({ asked, exit, cleared, change: now.change ?? null, session });
        }
        const [toSecond, backToFirst] = moves;
        closed = await js(win, CLOSED);
        check(
            'The default logbook is changed to a second logbook and back, each at a restart smd asked for; smd then holds a new session of the first logbook, and the first session is closed and kept',
            secondLogbook !== '' &&
                toSecond.asked.status === 202 &&
                backToFirst.asked.status === 202 &&
                toSecond.exit?.code === 3 &&
                backToFirst.exit?.code === 3 &&
                toSecond.session.session.logbook_uuid === secondLogbook &&
                backToFirst.session.session.logbook_uuid === firstLogbook &&
                backToFirst.session.session.id !== firstId &&
                backToFirst.session.session.held_by_you === true &&
                closed.logbook_uuid === firstLogbook &&
                closed.sessions?.some((x) => x.id === firstId && x.contact_count === 2),
            JSON.stringify({ made: made.status, secondLogbook, moves, closed })
        );

        // 7. The first session is continued through the panel, takes the next contact, and
        //    stays open.
        await until(() => js(win, press('Session — show')), 'the Session tile', 5000).catch(
            () => false // it may already be showing
        );
        await until(
            () => js(win, press('Continue…')),
            'Continue to be offered again and pressed',
            15000
        );
        await until(
            () => js(win, LINES).then((l) => (l.length > 0 ? l : null)),
            "the lines of Continue's list after the default came back",
            10000
        );
        // After two restarts the page has reconnected and read again; the line may be
        // shown a moment before it can be chosen.
        const choseFirst = await until(
            () => js(win, chooseLineWith('· 2 contacts')),
            "the first session's line to be one that can be chosen",
            20000
        );
        const again = await until(
            () => js(win, SESSION).then((x) => (x.session?.id === firstId ? x : null)),
            'smd to hold the first session again, after the default went away and came back'
        );
        const loggedThird = await js(win, log('G4AAA', '1230'));
        panel = await until(
            () => js(win, PANEL).then((p) => (p.calls.includes('G4AAA') ? p : null)),
            'the third contact in the panel',
            10000
        );
        const withThree = await js(win, SESSION);
        check(
            'After the default went to another logbook and came back, the first session is continued through the panel, takes the next contact, and stays open (F9)',
            choseFirst === true &&
                again.state === 'open' &&
                loggedThird === 201 &&
                withThree.state === 'open' &&
                withThree.session?.id === firstId &&
                withThree.session.held_by_you === true &&
                withThree.session.contact_count === 3 &&
                panel.heading.startsWith('Session (3)') &&
                panel.calls.length === 3 &&
                panel.said === '',
            `logged ${loggedThird}; ${JSON.stringify(withThree)}; ${JSON.stringify(panel)}`
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
    console.log(ok ? `Controls PASS (${checks.length} checks)` : 'Controls FAIL');
    return ok;
}
