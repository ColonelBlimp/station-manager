// The question before a close (W-0027 A3, section A), against a model of smd held in this file.
// Nothing listens and nothing is sent: `ses.fetch` is the model, the page's read and the
// question are given by the test, and requests are held and released, never slept on.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { test } from 'node:test';
import {
    CLOSE_TIMEOUT_MS,
    PAGE_REASONS_EXPRESSION,
    SETTINGS_SECTIONS,
    closeConfirmation,
    closeFlow,
    closeQuestion,
    isCloseRequest,
    pageReasons,
    readFt8State,
    readPageReasons,
} from '../src/closing.mjs';

const ORIGIN = 'https://127.0.0.1:8443';
const STATE = '/v1/ft8/state';
const json = (status, body) => ({ status, json: async () => body });
const served = (wouldStop) => ({
    served: true,
    would_stop: wouldStop,
    tx_armed: wouldStop,
    tx_in_flight: false,
    run_active: false,
    auto_work_armed: false,
});
const NOTHING = { entry: '', unsaved: [], saving: [] };

/** smd, as a close meets it: FT8's state as `state` answers it (a response, or a function
 *  given the request's options), and the rest of a close answered as acknowledged. */
function daemon(state = json(200, served(false))) {
    const d = { calls: [], state };
    d.ses = {
        fetch: async (url, opts = {}) => {
            const u = new URL(url);
            assert.equal(u.origin, ORIGIN);
            assert.equal(opts.redirect, 'manual', 'a redirect must never be followed');
            assert.ok(opts.signal instanceof AbortSignal, 'every request has its limit');
            assert.ok(isCloseRequest(url), 'every request of a close carries the shell’s mark');
            d.calls.push(`${opts.method ?? 'GET'} ${u.pathname}`);
            if (u.pathname === STATE)
                return typeof d.state === 'function' ? d.state(opts) : d.state;
            if (u.pathname === '/v1/ft8/tx/arm') return json(202, {});
            if (u.pathname === '/v1/operating-session')
                return json(200, { archive_id: 'ARCH-A', generation: 3, state: 'open' });
            return json(200, { generation: 4, already_recorded: false });
        },
    };
    return d;
}

/** A request that is never answered and ends only when its limit does (the limit's own timer
 *  does not keep Node alive, so this one does). */
const neverAnswers = (opts) =>
    new Promise((_res, reject) => {
        const alive = setInterval(() => {}, 5);
        opts.signal.addEventListener('abort', () => {
            clearInterval(alive);
            reject(new Error('gave up'));
        });
    });

// ------------------------------------------------------------------ FT8's state, from smd

test('FT8’s state is smd’s own word: active, idle, or not served', async () => {
    const read = (state) => readFt8State(daemon(state).ses, ORIGIN);
    assert.equal(await read(json(200, served(true))), 'active');
    assert.equal(await read(json(200, served(false))), 'idle');
    assert.equal(await read(json(200, { served: false })), 'not-served');
});

test('FT8’s state: it is one read, marked as the shell’s own', async () => {
    const d = daemon();
    await readFt8State(d.ses, ORIGIN);
    assert.deepEqual(d.calls, [`GET ${STATE}`]);
});

test('FT8’s state: anything that is not a valid answer is "not known", never idle and never "not served"', async () => {
    for (const answer of [
        json(404, { code: 'not_found' }), // a daemon without the route
        json(200, null),
        json(200, []),
        json(200, 'idle'),
        json(200, {}),
        json(200, { served: true }), // would_stop missing
        json(200, { served: true, would_stop: 'no' }),
        json(200, { served: 'false' }),
        json(200, { served: false, would_stop: false }), // "not served" says nothing else
        json(200, { would_stop: false }),
        json(403, { code: 'desktop_app_required' }),
        json(500, served(false)),
        () => {
            throw new Error('no answer');
        },
    ]) {
        assert.equal(await readFt8State(daemon(answer).ses, ORIGIN), 'unknown');
    }
});

test('FT8’s state: a read that is not answered within its limit is "not known"', async () => {
    assert.equal(
        await readFt8State(daemon(neverAnswers).ses, ORIGIN, { timeoutMs: 20 }),
        'unknown'
    );
});

// ------------------------------------------------------------------ the page's own reasons

test('the page’s reasons: only the values the shell knows', () => {
    assert.deepEqual(pageReasons(['', '', '']), NOTHING);
    assert.deepEqual(pageReasons(['draft', '', '']), { ...NOTHING, entry: 'draft' });
    assert.deepEqual(pageReasons(['unknown', '', '']), { ...NOTHING, entry: 'unknown' });
    assert.deepEqual(pageReasons(['', 'Station|FT8 / FT4', 'General']), {
        entry: '',
        unsaved: ['Station', 'FT8 / FT4'],
        saving: ['General'],
    });
});

test('the page’s reasons: anything else is "not reported", each attribute by itself', () => {
    // The waiting page, or any page that publishes nothing.
    assert.deepEqual(pageReasons([null, null, null]), NOTHING);
    for (const raw of [undefined, null, 'draft', {}, [], ['draft'], ['draft', '', '', '']]) {
        assert.deepEqual(pageReasons(raw), NOTHING, JSON.stringify(raw));
    }
    // A value that is not known silences only its own line.
    assert.deepEqual(pageReasons(['discard everything', 'Station', '']), {
        ...NOTHING,
        unsaved: ['Station'],
    });
    assert.deepEqual(pageReasons(['draft', 'Station|Click Close', 'General']), {
        entry: 'draft',
        unsaved: [],
        saving: ['General'],
    });
    for (const sections of [7, 'station', 'Station|', '|', 'Station|Station', ' Station']) {
        assert.deepEqual(pageReasons(['', sections, '']).unsaved, [], String(sections));
        assert.deepEqual(pageReasons(['', '', sections]).saving, [], String(sections));
    }
});

test('the page’s reasons: a section is named once, as being saved', () => {
    assert.deepEqual(pageReasons(['', 'Station|General', 'General']), {
        entry: '',
        unsaved: ['Station'],
        saving: ['General'],
    });
});

test('the sections the shell knows are the page’s own, every one', () => {
    // The page names a section by its tab (frontend/app/src/lib/config/unsaved.ts). A tab
    // added there and not here would be silently "not reported" at a close.
    const src = fs.readFileSync(
        new URL('../../frontend/app/src/lib/config/unsaved.ts', import.meta.url),
        'utf8'
    );
    const labels = [...src.matchAll(/^\s+label: '([^']+)',$/gm)].map((m) => m[1]);
    assert.ok(labels.length >= 9, 'the page’s sections were not found');
    assert.deepEqual([...SETTINGS_SECTIONS], labels);
    for (const label of labels) assert.ok(!label.includes('|'), label);
});

test('the page is read with one fixed expression, and only attributes of the close’s own', () => {
    assert.equal(typeof PAGE_REASONS_EXPRESSION, 'string');
    const names = [...PAGE_REASONS_EXPRESSION.matchAll(/'(data-[a-z-]+)'/g)].map((m) => m[1]);
    assert.deepEqual(names, [
        'data-sm-close-entry',
        'data-sm-close-unsaved',
        'data-sm-close-saving',
    ]);
});

test('the page’s read: what the page publishes, validated', async () => {
    assert.deepEqual(await readPageReasons(async () => ['draft', 'Rigs', '']), {
        entry: 'draft',
        unsaved: ['Rigs'],
        saving: [],
    });
});

test('the page’s read: a page that cannot be read reports nothing, and is not waited for beyond the limit', async () => {
    const gone = async () => {
        throw new Error('the renderer has gone');
    };
    assert.deepEqual(await readPageReasons(gone), NOTHING);
    assert.deepEqual(
        await readPageReasons(() => new Promise(() => {}), { timeoutMs: 20 }),
        NOTHING
    );
    assert.deepEqual(
        await readPageReasons(() => {
            throw new Error('no page');
        }),
        NOTHING
    );
});

// ------------------------------------------------------------------------------ the words

test('no reason, no question', () => {
    for (const ft8 of ['idle', 'not-served', 'none']) {
        assert.equal(closeQuestion({ ft8, ...NOTHING }), null);
    }
});

test('the question: the ruled words, each line only when it is true, and two answers', () => {
    const q = closeQuestion({ ft8: 'active', ...NOTHING });
    assert.equal(q.message, 'Close Station Manager?');
    assert.equal(q.detail, 'FT8 will be stopped and transmit disarmed.');
    assert.deepEqual(q.buttons, ['Close', 'Cancel']);
    assert.deepEqual(q.choices, ['close', 'cancel']);
    assert.equal(q.cancelId, 1, 'dismissing the question is Cancel');

    const line = (reasons) => closeQuestion({ ft8: 'idle', ...NOTHING, ...reasons }).detail;
    assert.equal(
        closeQuestion({ ft8: 'unknown', ...NOTHING }).detail,
        "FT8's state could not be read. If it is running, it will be stopped."
    );
    assert.equal(line({ entry: 'draft' }), 'The contact you are entering will be discarded.');
    assert.equal(
        line({ entry: 'unknown' }),
        'A contact was sent to be logged and its outcome is not known. It may already be logged.'
    );
    assert.equal(
        line({ unsaved: ['Station'] }),
        'Unsaved changes in Settings (Station) will be lost.'
    );
    assert.equal(
        line({ unsaved: ['Station', 'Email', 'General'] }),
        'Unsaved changes in Settings (Station, Email and General) will be lost.'
    );
    assert.equal(
        line({ saving: ['Rigs', 'Email'] }),
        'Settings (Rigs and Email) is still being saved. Closing does not wait: the save may not complete, and anything changed since it began will be lost.'
    );
});

test('the question: every reason at once, each named once, FT8 first', () => {
    const q = closeQuestion({
        ft8: 'active',
        entry: 'unknown',
        unsaved: ['Station'],
        saving: ['General'],
    });
    assert.deepEqual(q.detail.split('\n\n'), [
        'FT8 will be stopped and transmit disarmed.',
        'A contact was sent to be logged and its outcome is not known. It may already be logged.',
        'Unsaved changes in Settings (Station) will be lost.',
        'Settings (General) is still being saved. Closing does not wait: the save may not complete, and anything changed since it began will be lost.',
    ]);
});

test('the question never says an unknown log outcome is lost, and never promises a save', () => {
    const q = closeQuestion({ ft8: 'idle', ...NOTHING, entry: 'unknown', saving: ['General'] });
    assert.doesNotMatch(q.detail, /discard|unlogged|not logged|will be saved|is saved/i);
});

// ------------------------------------------------------------------- gathering, and asking

/** A confirmation over the model, with what it read and asked recorded. */
function confirmationOver(d, { asked = true, retired = false, page = ['', '', ''], answer } = {}) {
    const c = { questions: [], pageReads: 0 };
    c.confirm = closeConfirmation({
        ses: d.ses,
        origin: () => ORIGIN,
        admission: { everAsked: () => asked, isRetired: () => retired },
        readPage: async () => {
            c.pageReads++;
            if (page instanceof Error) throw page;
            return page;
        },
        askQuestion: async (q) => {
            c.questions.push(q);
            return typeof answer === 'function' ? answer() : answer;
        },
    });
    return c;
}

test('nothing in the way: the close is not asked about, and FT8’s state was read to know it', async () => {
    const d = daemon(json(200, served(false)));
    const c = confirmationOver(d);
    assert.equal(await c.confirm(), true);
    assert.deepEqual(c.questions, []);
    assert.deepEqual(d.calls, [`GET ${STATE}`]);
    assert.equal(c.pageReads, 1);
});

test('FT8 not served is nothing in the way', async () => {
    const c = confirmationOver(daemon(json(200, { served: false })));
    assert.equal(await c.confirm(), true);
    assert.deepEqual(c.questions, []);
});

test('something in the way: asked once, Close goes on and Cancel does not', async () => {
    for (const [answer, goes] of [
        ['close', true],
        ['cancel', false],
        [undefined, false], // a box that gave no answer is not a Close
    ]) {
        const c = confirmationOver(daemon(json(200, served(true))), { answer });
        assert.equal(await c.confirm(), goes);
        assert.equal(c.questions.length, 1);
    }
});

test('the reasons are gathered from both: the page’s lines beside smd’s word', async () => {
    const c = confirmationOver(daemon(json(200, served(true))), {
        page: ['draft', 'Station', ''],
        answer: 'close',
    });
    await c.confirm();
    assert.deepEqual(c.questions[0].detail.split('\n\n'), [
        'FT8 will be stopped and transmit disarmed.',
        'The contact you are entering will be discarded.',
        'Unsaved changes in Settings (Station) will be lost.',
    ]);
});

test('a page that cannot be read does not silence smd’s word (codex-236)', async () => {
    const c = confirmationOver(daemon(json(200, served(true))), {
        page: new Error('the renderer has gone'),
        answer: 'cancel',
    });
    assert.equal(await c.confirm(), false);
    assert.equal(c.questions[0].detail, 'FT8 will be stopped and transmit disarmed.');
});

test('FT8’s state not known asks, with the qualified line', async () => {
    const c = confirmationOver(daemon(json(404, { code: 'not_found' })), { answer: 'close' });
    assert.equal(await c.confirm(), true);
    assert.equal(
        c.questions[0].detail,
        "FT8's state could not be read. If it is running, it will be stopped."
    );
});

test('a window that knows it is retired reads no FT8 state and shows no FT8 line', async () => {
    // It sends no disarm, so it must not say this close stops FT8: it may be another app's.
    const d = daemon(json(200, served(true)));
    const quiet = confirmationOver(d, { retired: true });
    assert.equal(await quiet.confirm(), true);
    assert.deepEqual(quiet.questions, []);
    assert.deepEqual(d.calls, []);

    const withDraft = confirmationOver(d, {
        retired: true,
        page: ['draft', '', ''],
        answer: 'close',
    });
    await withDraft.confirm();
    assert.equal(withDraft.questions[0].detail, 'The contact you are entering will be discarded.');
    assert.deepEqual(d.calls, []);
});

test('a lifetime smd was never asked to admit reads no FT8 state either: its close sends no disarm', async () => {
    const d = daemon(json(200, served(true)));
    const c = confirmationOver(d, { asked: false });
    assert.equal(await c.confirm(), true);
    assert.deepEqual(c.questions, []);
    assert.deepEqual(d.calls, []);
});

test('the page is read last: what is typed while smd’s answer is awaited is asked about', async () => {
    // The window still takes input while FT8's state is read, for up to the limit. A page
    // read before that answer would miss a contact typed meanwhile, and the close would
    // then destroy the window without a word (the clean-room review of 88177082).
    let releaseState;
    const d = daemon(() => new Promise((r) => (releaseState = () => r(json(200, served(false))))));
    const c = { questions: [], pageReads: 0 };
    let typed = false;
    const confirm = closeConfirmation({
        ses: d.ses,
        origin: () => ORIGIN,
        admission: { everAsked: () => true, isRetired: () => false },
        readPage: async () => {
            c.pageReads++;
            return [typed ? 'draft' : '', '', ''];
        },
        askQuestion: async (q) => {
            c.questions.push(q);
            return 'cancel';
        },
    });
    const done = confirm();
    await new Promise((r) => setImmediate(r));
    assert.equal(c.pageReads, 0, 'the page was read before smd had answered');
    typed = true; // the operator types a callsign while the read is out
    releaseState();
    assert.equal(await done, false);
    assert.equal(c.pageReads, 1);
    assert.equal(c.questions[0].detail, 'The contact you are entering will be discarded.');
});

// ----------------------------------------------------------------- within the close itself

/** The whole close over the model, with its side effects in one ordered log. */
function closeOver(d, { answer, page = ['', '', ''] } = {}) {
    const log = [];
    const f = { log, questions: [] };
    const admission = {
        everAsked: () => true,
        isRetired: () => false,
        beginClosing: () => {
            log.push('barrier');
            return Promise.resolve();
        },
        endClosing: () => log.push('barrier lifted'),
    };
    f.flow = closeFlow({
        ses: d.ses,
        origin: () => ORIGIN,
        admission,
        window: {
            hide: () => log.push('hide'),
            show: () => log.push('show'),
            destroy: () => log.push('destroy'),
        },
        pageWrites: {
            refuse: () => log.push('writes refused'),
            allow: () => log.push('writes allowed'),
        },
        ask: async () => {
            log.push('box');
            return 'close';
        },
        quit: () => log.push('quit'),
        confirm: closeConfirmation({
            ses: d.ses,
            origin: () => ORIGIN,
            admission,
            readPage: async () => page,
            askQuestion: async (q) => {
                log.push('question');
                f.questions.push(q);
                return typeof answer === 'function' ? answer() : answer;
            },
        }),
    });
    return f;
}

test('Cancel changes nothing at all: no barrier, no disarm, no exit, the window as it was', async () => {
    const d = daemon(json(200, served(true)));
    const f = closeOver(d, { answer: 'cancel' });
    assert.equal(await f.flow.close(), 'cancelled');
    assert.deepEqual(f.log, ['question']);
    assert.deepEqual(d.calls, [`GET ${STATE}`], 'only the read, which changes nothing');
    assert.equal(f.flow.holdsApp(), false);
});

test('Close: the question first, then the close in its order', async () => {
    const d = daemon(json(200, served(true)));
    const f = closeOver(d, { answer: 'close' });
    assert.equal(await f.flow.close(), 'closed');
    assert.deepEqual(f.log, ['question', 'barrier', 'writes refused', 'hide', 'destroy', 'quit']);
    assert.deepEqual(d.calls, [
        `GET ${STATE}`,
        'POST /v1/ft8/tx/arm',
        'GET /v1/operating-session',
        'POST /v1/operating-session/exit',
    ]);
});

test('the disarm is sent whatever the read said: an idle answer is one observation', async () => {
    const d = daemon(json(200, served(false)));
    const f = closeOver(d);
    assert.equal(await f.flow.close(), 'closed');
    assert.deepEqual(f.questions, []);
    assert.ok(d.calls.includes('POST /v1/ft8/tx/arm'));
});

test('one close at a time: a second close while the question shows asks nothing more', async () => {
    let answerIt;
    const d = daemon(json(200, served(true)));
    const f = closeOver(d, { answer: () => new Promise((r) => (answerIt = r)) });
    const first = f.flow.close();
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));
    const second = f.flow.close();
    assert.equal(f.questions.length, 1);
    assert.equal(f.flow.holdsApp(), true, 'the question is part of the close');
    assert.equal(f.flow.allowsQuit(), false, 'no other quit ends the app under the question');
    answerIt('cancel');
    assert.equal(await first, 'cancelled');
    assert.equal(await second, 'cancelled');
    assert.equal(f.questions.length, 1);
    assert.deepEqual(d.calls, [`GET ${STATE}`]);
});

test('after a Cancel the next close asks afresh, from what is true then', async () => {
    const d = daemon(json(200, served(true)));
    const answers = ['cancel'];
    const f = closeOver(d, { answer: () => answers.shift() });
    assert.equal(await f.flow.close(), 'cancelled');
    d.state = json(200, served(false)); // the run ended meanwhile
    assert.equal(await f.flow.close(), 'closed');
    assert.equal(f.questions.length, 1, 'the second close had nothing to ask');
});

test('the read of FT8’s state waits the ruled limit and no other', () => {
    assert.equal(CLOSE_TIMEOUT_MS, 5000);
});
