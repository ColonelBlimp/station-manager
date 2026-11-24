// Closing the app (W-0027 A3, sections B to E), against a model of smd's answers held in this
// file. Nothing listens and nothing is sent: `ses.fetch` is the model, and the window, the
// box and the quit are recorders. Requests are held and released by the test, never slept on.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ADMISSION_PATH, admission } from '../src/admission.mjs';
import {
    ARCHIVE_HEADER,
    CLOSE_TIMEOUT_MS,
    closeFlow,
    isCloseRequest,
    reportExit,
    stopFt8,
    unconfirmedBox,
} from '../src/closing.mjs';

const ORIGIN = 'https://127.0.0.1:8443';
const DISARM = '/v1/ft8/tx/arm';
const SESSION = '/v1/operating-session';
const EXIT = '/v1/operating-session/exit';
const json = (status, body) => ({ status, json: async () => body });
const settled = () => new Promise((r) => setImmediate(r));

/** smd, as a close meets it. `on[path]` is a queue of one-shot behaviours for the next
 *  request to that path: a function given the request, returning a response to use in place
 *  of the model's, or a promise of one (a held request), or throwing (no answer). `lose`
 *  applies the request and then loses its answer. */
function daemon({ ft8 = true, generation = 3, archive = 'ARCH-A' } = {}) {
    const d = {
        ft8,
        generation,
        archive,
        exitRecorded: false,
        notCurrent: false, // the daemon restarted: this lifetime holds the session, not current
        calls: [],
        on: { [DISARM]: [], [SESSION]: [], [EXIT]: [] },
        lose: { [EXIT]: 0 },
        log: null,
    };
    d.ses = {
        fetch: async (url, opts = {}) => {
            const u = new URL(url);
            const method = opts.method ?? 'GET';
            assert.equal(u.origin, ORIGIN);
            assert.equal(opts.redirect, 'manual', 'a redirect must never be followed');
            assert.ok(opts.signal instanceof AbortSignal, 'every request has its limit');
            assert.ok(isCloseRequest(url), 'every request of a close carries the shell’s mark');
            const call = {
                method,
                path: u.pathname,
                body: opts.body ? JSON.parse(opts.body) : undefined,
                archive: opts.headers?.[ARCHIVE_HEADER],
            };
            d.calls.push(call);
            d.log?.push(`${method} ${u.pathname}`);
            const special = d.on[u.pathname]?.shift();
            if (special) {
                const r = await special(call, opts);
                if (r) return r;
            }
            if (u.pathname === DISARM) {
                assert.deepEqual(call.body, { armed: false });
                return d.ft8 ? json(202, {}) : json(404, { code: 'not_found' });
            }
            if (u.pathname === SESSION && method === 'GET') {
                return json(200, {
                    archive_id: d.archive,
                    generation: d.generation,
                    state: 'open',
                });
            }
            assert.equal(u.pathname, EXIT);
            let res;
            if (d.exitRecorded)
                res = json(200, { generation: d.generation, already_recorded: true });
            else if (d.notCurrent) res = json(409, { code: 'operating_session_exit_not_current' });
            else if (call.body.generation !== d.generation)
                res = json(409, { code: 'operating_session_stale', generation: d.generation });
            else {
                d.exitRecorded = true;
                d.generation++;
                res = json(200, { generation: d.generation, already_recorded: false });
            }
            if (d.lose[EXIT] > 0) {
                d.lose[EXIT]--;
                throw new Error('the answer was lost');
            }
            return res;
        },
    };
    d.sent = () => d.calls.map((c) => `${c.method} ${c.path}`);
    return d;
}

/** A request that is never answered and ends only when its limit does. The limit's own timer
 *  does not keep Node alive (AbortSignal.timeout is unreferenced), so this one does. */
const neverAnswers = (_call, opts) =>
    new Promise((_res, reject) => {
        const alive = setInterval(() => {}, 5);
        opts.signal.addEventListener('abort', () => {
            clearInterval(alive);
            reject(new Error('gave up'));
        });
    });

// ---------------------------------------------------------------- the requests themselves

test('the disarm: the operator’s own request, answered before anything else', async () => {
    const d = daemon();
    assert.equal(await stopFt8(d.ses, ORIGIN), 'stopped');
    assert.deepEqual(d.sent(), [`POST ${DISARM}`]);
});

test('the disarm: FT8 not served is exactly smd’s "no such route", and nothing else is', async () => {
    assert.equal(await stopFt8(daemon({ ft8: false }).ses, ORIGIN), 'not-served');
    for (const answer of [
        () => json(404, { code: 'something_else' }),
        () => json(404, null),
        () => json(503, { code: 'ft8_tx_unavailable' }),
        () => json(409, { code: 'ft8_tx_in_flight' }),
        () => json(200, {}),
        () => {
            throw new Error('no answer');
        },
    ]) {
        const d = daemon();
        d.on[DISARM].push(answer);
        assert.equal(await stopFt8(d.ses, ORIGIN), 'failed');
    }
});

test('each request waits its own limit and no longer', async () => {
    assert.equal(CLOSE_TIMEOUT_MS, 5000, 'ruled 2026-11-23: five seconds for each request');
    const d = daemon();
    d.on[DISARM].push(neverAnswers);
    assert.equal(await stopFt8(d.ses, ORIGIN, { timeoutMs: 20 }), 'failed');
    d.on[SESSION].push(neverAnswers);
    assert.deepEqual(await reportExit(d.ses, ORIGIN, { timeoutMs: 20 }), {
        state: 'unconfirmed',
        sent: false,
    });
});

test('the exit: the state is read, then the exit names the generation and the archive read', async () => {
    const d = daemon({ generation: 7, archive: 'ARCH-Z' });
    assert.deepEqual(await reportExit(d.ses, ORIGIN), { state: 'acknowledged', sent: true });
    assert.deepEqual(d.sent(), [`GET ${SESSION}`, `POST ${EXIT}`]);
    assert.deepEqual(d.calls[1].body, { generation: 7 });
    assert.equal(d.calls[1].archive, 'ARCH-Z');
});

test('the exit: one already recorded is acknowledged (a lost answer is safe to repeat)', async () => {
    const d = daemon();
    d.exitRecorded = true;
    assert.deepEqual(await reportExit(d.ses, ORIGIN), { state: 'acknowledged', sent: true });
});

test('the exit: stale is sent again at the generation returned, without reading or admitting again', async () => {
    const d = daemon({ generation: 3 });
    d.on[EXIT].push(() => {
        d.generation = 5; // the session state moved between the read and the exit
        return undefined;
    });
    assert.deepEqual(await reportExit(d.ses, ORIGIN), { state: 'acknowledged', sent: true });
    assert.deepEqual(d.sent(), [`GET ${SESSION}`, `POST ${EXIT}`, `POST ${EXIT}`]);
    assert.deepEqual(
        d.calls.slice(1).map((c) => c.body.generation),
        [3, 5]
    );
});

test('the exit: a stale answer that names no generation is not guessed at', async () => {
    const d = daemon();
    d.on[EXIT].push(() => json(409, { code: 'operating_session_stale' }));
    assert.deepEqual(await reportExit(d.ses, ORIGIN), { state: 'unconfirmed', sent: true });
    assert.equal(d.calls.length, 2);
});

test('the exit: not current and still holding the session is terminal, and not repeated', async () => {
    const d = daemon();
    d.notCurrent = true;
    assert.deepEqual(await reportExit(d.ses, ORIGIN), { state: 'terminal', sent: true });
    assert.equal(d.calls.length, 2);
});

test('the exit: a state that cannot be read sends no exit', async () => {
    for (const answer of [
        () => json(503, { code: 'operating_session_unavailable' }),
        () => json(200, { generation: 3 }), // no archive
        () => json(200, { archive_id: 'A', generation: '3' }), // not a number
        () => json(200, null),
        () => {
            throw new Error('no answer');
        },
    ]) {
        const d = daemon();
        d.on[SESSION].push(answer);
        assert.deepEqual(await reportExit(d.ses, ORIGIN), { state: 'unconfirmed', sent: false });
        assert.deepEqual(d.sent(), [`GET ${SESSION}`]);
    }
});

test('the exit: no answer, or any other answer, is not an acknowledgement', async () => {
    for (const answer of [
        () => json(503, { code: 'operating_session_unavailable' }),
        () => json(409, { code: 'archive_mismatch' }),
        () => json(400, { code: 'missing_required_field' }),
        () => json(200, null),
        () => json(200, { generation: 4 }), // not the acknowledgement's shape
        () => {
            throw new Error('no answer');
        },
    ]) {
        const d = daemon();
        d.on[EXIT].push(answer);
        assert.deepEqual(await reportExit(d.ses, ORIGIN), { state: 'unconfirmed', sent: true });
    }
});

// ---------------------------------------------------------------------------- the flow

/** A close flow over the model, with every side effect recorded in one ordered log. */
function flowOver(d, { asked = true, retired = false, answers = [], confirm, timeoutMs } = {}) {
    const log = [];
    d.log = log;
    const boxes = [];
    const f = { log, boxes, barrier: Promise.resolve(), retired, quitAllowed: [] };
    f.flow = closeFlow({
        ses: d.ses,
        origin: () => ORIGIN,
        admission: {
            everAsked: () => asked,
            isRetired: () => f.retired,
            beginClosing: () => {
                log.push('barrier');
                return f.barrier;
            },
            endClosing: () => log.push('barrier lifted'),
        },
        window: {
            hide: () => log.push('hide'),
            show: () => log.push('show'),
            destroy: () => log.push('destroy'),
        },
        pageWrites: {
            refuse: () => log.push('writes refused'),
            allow: () => log.push('writes allowed'),
        },
        ask: async (state) => {
            log.push('box');
            boxes.push(state);
            const next = answers.shift();
            return typeof next === 'function' ? next() : next;
        },
        quit: () => {
            f.quitAllowed.push(f.flow.allowsQuit());
            log.push('quit');
        },
        confirm,
        timeoutMs,
    });
    return f;
}

test('a close with nothing in the way: barrier, no page writes, hidden, disarm, read, exit, then gone', async () => {
    const d = daemon();
    const f = flowOver(d);
    assert.equal(await f.flow.close(), 'closed');
    assert.deepEqual(f.log, [
        'barrier',
        'writes refused',
        'hide',
        `POST ${DISARM}`,
        `GET ${SESSION}`,
        `POST ${EXIT}`,
        'destroy',
        'quit',
    ]);
    assert.equal(f.boxes.length, 0, 'an acknowledged close shows no box');
    assert.equal(d.exitRecorded, true);
});

test('FT8 not served: the close goes on', async () => {
    const d = daemon({ ft8: false });
    const f = flowOver(d);
    assert.equal(await f.flow.close(), 'closed');
    assert.deepEqual(d.sent(), [`POST ${DISARM}`, `GET ${SESSION}`, `POST ${EXIT}`]);
});

test('the exit is not sent until admission work already issued has settled', async () => {
    const d = daemon();
    const f = flowOver(d);
    let release;
    f.barrier = new Promise((res) => (release = res));
    const closing = f.flow.close();
    await settled();
    assert.deepEqual(d.sent(), [], 'a request of the close went out before the admission settled');
    assert.deepEqual(f.log, ['barrier', 'writes refused', 'hide']);
    release();
    assert.equal(await closing, 'closed');
});

test('a window that knows it is retired sends no disarm, and still reports its exit', async () => {
    const d = daemon();
    const f = flowOver(d, { retired: true });
    assert.equal(await f.flow.close(), 'closed');
    assert.deepEqual(d.sent(), [`GET ${SESSION}`, `POST ${EXIT}`]);
});

test('a retirement learnt while the barrier waited counts: no disarm', async () => {
    const d = daemon();
    const f = flowOver(d);
    let release;
    f.barrier = new Promise((res) => (release = res));
    const closing = f.flow.close();
    await settled();
    f.retired = true; // the admission issued before the close answered "retired"
    release();
    await closing;
    assert.deepEqual(d.sent(), [`GET ${SESSION}`, `POST ${EXIT}`]);
});

test('a lifetime smd was never asked to admit has nothing to stop or report: it just closes', async () => {
    const d = daemon();
    const f = flowOver(d, { asked: false });
    assert.equal(await f.flow.close(), 'closed');
    assert.deepEqual(d.sent(), []);
    // The barrier is entered all the same (codex-239): a start-up probe still out when the
    // window goes must not become this lifetime's first admission.
    assert.deepEqual(f.log, ['barrier', 'destroy', 'quit']);
    assert.ok(!f.log.includes('barrier lifted'));
});

test('first kind, the disarm fails: no exit is reported, the window is kept, and the box says so', async () => {
    const d = daemon();
    d.on[DISARM].push(() => json(503, { code: 'ft8_tx_unavailable' }));
    const f = flowOver(d, { answers: ['cancel'] });
    assert.equal(await f.flow.close(), 'cancelled');
    assert.deepEqual(d.sent(), [`POST ${DISARM}`], 'an exit was reported as if FT8 had stopped');
    assert.deepEqual(f.boxes, [{ kind: 'first', disarmFailed: true, terminal: false }]);
    assert.deepEqual(f.log, [
        'barrier',
        'writes refused',
        'hide',
        `POST ${DISARM}`,
        'box',
        'writes allowed',
        'barrier lifted',
        'show',
    ]);
    assert.equal(d.exitRecorded, false);
});

test('first kind, the state cannot be read: Cancel gives the window and the admission back', async () => {
    const d = daemon();
    d.on[SESSION].push(() => json(503, { code: 'operating_session_unavailable' }));
    const f = flowOver(d, { answers: ['cancel'] });
    assert.equal(await f.flow.close(), 'cancelled');
    assert.deepEqual(f.boxes, [{ kind: 'first', disarmFailed: false, terminal: false }]);
    assert.ok(!f.log.includes('destroy') && !f.log.includes('quit'));
    assert.deepEqual(f.log.slice(-3), ['writes allowed', 'barrier lifted', 'show']);
});

test('first kind, Try again: the close runs again from the start, under the same barrier', async () => {
    const d = daemon();
    d.on[DISARM].push(() => {
        throw new Error('no answer');
    });
    const f = flowOver(d, { answers: ['retry'] });
    assert.equal(await f.flow.close(), 'closed');
    assert.deepEqual(d.sent(), [
        `POST ${DISARM}`,
        `POST ${DISARM}`,
        `GET ${SESSION}`,
        `POST ${EXIT}`,
    ]);
    assert.equal(f.log.filter((l) => l === 'barrier').length, 1);
    assert.ok(!f.log.includes('barrier lifted'));
});

test('first kind, Close anyway: the app goes and nothing is claimed', async () => {
    const d = daemon();
    d.on[DISARM].push(() => {
        throw new Error('no answer');
    });
    const f = flowOver(d, { answers: ['close'] });
    assert.equal(await f.flow.close(), 'closed-unconfirmed');
    assert.deepEqual(f.log.slice(-2), ['destroy', 'quit']);
    assert.equal(d.exitRecorded, false);
    assert.ok(!f.log.includes('barrier lifted'));
});

test('after a cancelled close the next close starts afresh', async () => {
    const d = daemon();
    d.on[DISARM].push(() => {
        throw new Error('no answer');
    });
    const f = flowOver(d, { answers: ['cancel'] });
    assert.equal(await f.flow.close(), 'cancelled');
    assert.equal(f.flow.holdsApp(), false);
    assert.equal(await f.flow.close(), 'closed');
    assert.equal(f.log.filter((l) => l === 'barrier').length, 2);
});

test('second kind, the exit’s answer is lost: the window is closed BEFORE the box, and there is no way back', async () => {
    const d = daemon();
    d.lose[EXIT] = 1;
    const f = flowOver(d, { answers: ['cancel'] }); // even a box that says cancel
    assert.equal(await f.flow.close(), 'closed-unconfirmed');
    assert.deepEqual(f.boxes, [{ kind: 'second', disarmFailed: false, terminal: false }]);
    assert.ok(
        f.log.indexOf('destroy') < f.log.indexOf('box'),
        'the box was shown over a live window'
    );
    for (const never of ['show', 'writes allowed', 'barrier lifted']) {
        assert.ok(!f.log.includes(never), `a second-kind close did "${never}"`);
    }
    assert.equal(f.log.filter((l) => l === 'destroy').length, 1);
    assert.equal(f.log.at(-1), 'quit');
});

test('second kind, Try again: the exit already recorded is acknowledged and the app goes', async () => {
    const d = daemon();
    d.lose[EXIT] = 1;
    const f = flowOver(d, { answers: ['retry'] });
    assert.equal(await f.flow.close(), 'closed');
    // No second disarm: see the next test.
    assert.deepEqual(d.sent(), [
        `POST ${DISARM}`,
        `GET ${SESSION}`,
        `POST ${EXIT}`,
        `GET ${SESSION}`,
        `POST ${EXIT}`,
    ]);
    assert.equal(f.log.filter((l) => l === 'destroy').length, 1);
    assert.equal(f.boxes.length, 1);
});

// The clean-room review of ff653c6c (P1). Once an exit was sent, smd may have stored it:
// this lifetime may be retired, and cannot know. A disarm needs no lifetime, so one sent then
// would stop whoever operates now. So Try again asks only for the acknowledgement. Nothing is
// lost: this close's own disarm was answered before its exit went out, the page has written
// nothing since, and an acknowledged exit stops FT8 for its own lifetime anyway.
test('once an exit was sent, Try again sends no disarm: it may be another app’s FT8 by now', async () => {
    const d = daemon();
    d.on[EXIT].push(neverAnswers); // the first exit: given up on, and it may still be stored
    const f = flowOver(d, { answers: ['retry'], timeoutMs: 20 });
    assert.equal(await f.flow.close(), 'closed');
    assert.deepEqual(d.sent(), [
        `POST ${DISARM}`,
        `GET ${SESSION}`,
        `POST ${EXIT}`,
        `GET ${SESSION}`,
        `POST ${EXIT}`,
    ]);
    assert.deepEqual(f.boxes, [{ kind: 'second', disarmFailed: false, terminal: false }]);
});

test('once an exit was sent the close stays of the second kind, though a later attempt sends no exit of its own', async () => {
    const d = daemon();
    d.lose[EXIT] = 1; // the first exit is stored, and its answer lost
    d.on[SESSION].push(undefined, () => json(503, { code: 'operating_session_unavailable' }));
    const f = flowOver(d, { answers: ['retry', 'cancel'] });
    assert.equal(await f.flow.close(), 'closed-unconfirmed');
    assert.deepEqual(
        f.boxes.map((b) => b.kind),
        ['second', 'second'],
        'a retry that sent no exit made the close of the first kind again'
    );
    assert.ok(!f.log.includes('show') && !f.log.includes('barrier lifted'));
});

test('the terminal answer: second kind, no Try again, and trying again is not done even if asked', async () => {
    const d = daemon();
    d.notCurrent = true;
    const f = flowOver(d, { answers: ['retry'] });
    assert.equal(await f.flow.close(), 'closed-unconfirmed');
    assert.deepEqual(f.boxes, [{ kind: 'second', disarmFailed: false, terminal: true }]);
    assert.equal(d.calls.length, 3, 'a terminal answer was repeated');
    assert.ok(f.log.indexOf('destroy') < f.log.indexOf('box'));
});

test('one close at a time: a second close while the box is up starts nothing and shows no second box', async () => {
    const d = daemon();
    d.lose[EXIT] = 1;
    let answer;
    const f = flowOver(d, { answers: [() => new Promise((res) => (answer = res))] });
    const first = f.flow.close();
    for (let i = 0; i < 200 && f.boxes.length === 0; i++) await settled();
    assert.equal(f.boxes.length, 1, 'the box never came up');
    assert.equal(f.flow.holdsApp(), true, 'the app must outlive its window while the box is up');
    const sentBefore = d.calls.length;
    const second = f.flow.close();
    await settled();
    assert.equal(f.boxes.length, 1);
    assert.equal(d.calls.length, sentBefore);
    answer('close');
    assert.equal(await first, 'closed-unconfirmed');
    assert.equal(await second, 'closed-unconfirmed');
    assert.equal(f.log.filter((l) => l === 'quit').length, 1);
});

test('a close that is not confirmed changes nothing at all', async () => {
    const d = daemon();
    const f = flowOver(d, { confirm: async () => false });
    assert.equal(await f.flow.close(), 'cancelled');
    assert.deepEqual(f.log, []);
    assert.equal(f.flow.holdsApp(), false);
});

// --------------------------------------------- the app's quit, while a close holds the app

test('another request to quit while a close is under way is not the close’s own: only that one is allowed', async () => {
    const d = daemon();
    d.lose[EXIT] = 1;
    let answer;
    const f = flowOver(d, { answers: [() => new Promise((res) => (answer = res))] });
    assert.equal(
        f.flow.allowsQuit(),
        true,
        'with no close under way the app quits as it always did'
    );
    const closing = f.flow.close();
    for (let i = 0; i < 200 && f.boxes.length === 0; i++) await settled();
    assert.equal(f.boxes.length, 1, 'the box never came up');
    assert.equal(f.flow.allowsQuit(), false, 'a quit under the second-kind box would skip the box');
    answer('close');
    await closing;
    assert.deepEqual(f.quitAllowed, [true], 'the close’s own final quit was refused');
    assert.equal(f.flow.allowsQuit(), true);
});

test('…and so under the first-kind box, and again after a cancelled close', async () => {
    const d = daemon();
    d.on[DISARM].push(() => {
        throw new Error('no answer');
    });
    let answer;
    const f = flowOver(d, { answers: [() => new Promise((res) => (answer = res))] });
    const closing = f.flow.close();
    for (let i = 0; i < 200 && f.boxes.length === 0; i++) await settled();
    assert.equal(f.flow.allowsQuit(), false);
    answer('cancel');
    assert.equal(await closing, 'cancelled');
    assert.equal(f.flow.allowsQuit(), true);
});

// --------------------------------- the never-asked close and a start-up probe still out

/** A window as the waiting loop and the close see it. */
function fakeWindow(ses) {
    const win = {
        destroyed: false,
        loads: [],
        isDestroyed: () => win.destroyed,
        loadURL: async (url) => void win.loads.push(url),
        webContents: { getURL: () => 'data:text/html,waiting', session: ses },
    };
    return win;
}

test('closed before smd was ever asked: the barrier stands, and nothing asks afterwards', async () => {
    let admitCalls = 0;
    const a = admission({
        ses: {},
        origin: () => ORIGIN,
        admitFn: async () => {
            admitCalls++;
            return { state: 'admitted', instance: 'P1', generation: 1 };
        },
    });
    const d = daemon();
    const f = flowOver(d, {});
    const flow = closeFlow({
        ses: d.ses,
        origin: () => ORIGIN,
        admission: a,
        window: { hide() {}, show() {}, destroy() {} },
        pageWrites: { refuse() {}, allow() {} },
        ask: async () => assert.fail('a close with nothing to report showed a box'),
        quit() {},
    });
    void f;
    assert.equal(await flow.close(), 'closed');
    assert.equal(a.isClosing(), true, 'the close went by without the barrier');
    assert.deepEqual(await a.admitNow(), { state: 'failed' });
    assert.equal(admitCalls, 0, 'an admission was launched after the app had closed');
    assert.equal(a.everAsked(), false);
});

test('the whole of it: a start-up probe answered after that close asks smd nothing (codex-239)', async () => {
    // The real admission, the real close and the real waiting loop, over one session whose
    // version probe is held. Nothing is sent anywhere: the session is this recorder.
    const sent = [];
    let answerProbe;
    const ses = {
        fetch: (url, opts = {}) => {
            const path = new URL(url).pathname;
            sent.push(`${opts.method ?? 'GET'} ${path}`);
            if (path === '/v1/version') return new Promise((res) => (answerProbe = res));
            return Promise.resolve(
                json(200, { daemon_instance: 'P1', generation: 0, current: false })
            );
        },
    };
    const win = fakeWindow(ses);
    const a = admission({ ses, origin: () => ORIGIN });
    let boxes = 0;
    const flow = closeFlow({
        ses,
        origin: () => ORIGIN,
        admission: a,
        window: { hide() {}, show() {}, destroy: () => (win.destroyed = true) },
        pageWrites: { refuse() {}, allow() {} },
        ask: async () => {
            boxes++;
            return 'close';
        },
        quit() {},
    });
    const { loadWhenUp } = await import('../src/waiting.mjs');
    const loop = loadWhenUp(win, `${ORIGIN}/`, ORIGIN, { admit: a.admitNow });
    await settled();
    assert.deepEqual(sent, ['GET /v1/version'], 'fixture: the probe is out and held');

    assert.equal(await flow.close(), 'closed'); // smd was never asked: the app just goes
    answerProbe(json(200, {})); // and now smd answers the probe: it is up
    await loop;
    await settled();

    assert.deepEqual(sent, ['GET /v1/version'], 'something was asked of smd after the app closed');
    assert.ok(!sent.some((s) => s.endsWith(ADMISSION_PATH)), 'an admission was asked for');
    assert.equal(boxes, 0);
    assert.equal(a.everAsked(), false);
    assert.deepEqual(win.loads, []);
});

// ----------------------------------------------------------------------------- the box

test('the box: the ruled words, and only the choices each kind has', () => {
    const TITLE = 'The daemon did not confirm the close';
    const NOT_CONFIRMED =
        'Station Manager could not confirm with the daemon that it is closing, so the daemon may treat this sitting as interrupted.';
    const FT8 =
        'Station Manager could not confirm that FT8 was stopped. The daemon stops it by itself shortly after this window goes.';

    const first = unconfirmedBox({ kind: 'first', disarmFailed: false, terminal: false });
    assert.equal(first.message, TITLE);
    assert.ok(first.detail.startsWith(NOT_CONFIRMED));
    assert.deepEqual(first.buttons, ['Try again', 'Close anyway', 'Cancel']);
    assert.deepEqual(first.choices, ['retry', 'close', 'cancel']);
    assert.equal(first.choices[first.cancelId], 'cancel', 'dismissing the box cancels the close');

    const ft8 = unconfirmedBox({ kind: 'first', disarmFailed: true, terminal: false });
    assert.ok(ft8.detail.startsWith(FT8));
    assert.match(ft8.detail, /may still take effect/, 'E1: a stop already sent may still arrive');

    const second = unconfirmedBox({ kind: 'second', disarmFailed: false, terminal: false });
    assert.equal(second.detail, NOT_CONFIRMED);
    assert.deepEqual(second.buttons, ['Try again', 'Close anyway']);
    assert.deepEqual(second.choices, ['retry', 'close']);
    assert.equal(second.choices[second.cancelId], 'close', 'there is no way back to cancel to');

    const terminal = unconfirmedBox({ kind: 'second', disarmFailed: false, terminal: true });
    assert.deepEqual(terminal.buttons, ['Close']);
    assert.deepEqual(terminal.choices, ['close']);

    for (const box of [first, ft8, second, terminal]) {
        assert.doesNotMatch(
            box.detail,
            /could not tell/,
            'never "could not tell": it may have been told'
        );
    }
});

test('the mark: only this process’s own close requests carry it', () => {
    assert.equal(isCloseRequest(`${ORIGIN}${DISARM}`), false);
    assert.equal(isCloseRequest(`${ORIGIN}${DISARM}?sm-close=0000`), false);
    assert.equal(isCloseRequest('not a url'), false);
});
