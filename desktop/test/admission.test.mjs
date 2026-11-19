// The app lifetime and its admission (W-0027 C4a), against a model of smd's rule held in this
// file. Nothing listens and nothing is sent: `ses.fetch` is the model.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    ADMISSION_PATH,
    MARKER_HEADER,
    admission,
    admit,
    isAdmissionRequest,
    newLifetime,
    watchDaemon,
} from '../src/admission.mjs';

const ORIGIN = 'https://127.0.0.1:8443';
const json = (status, body) => ({ status, json: async () => body });

/** smd's admission rule (internal/api/app_admission.go), for one caller lifetime. `before`
 *  hooks run just before a request is answered, to place another app's admission between this
 *  one's read and its admit. */
function model({ instance = 'proc-1' } = {}) {
    const m = {
        instance,
        generation: 0,
        current: null,
        retired: new Set(),
        calls: [],
        before: { GET: [], POST: [] },
        loseNextAnswer: false,
        replaceWith(lifetime) {
            if (m.current) m.retired.add(m.current);
            m.current = lifetime;
            m.generation++;
        },
        ses(lifetime) {
            return {
                fetch: async (url, opts = {}) => {
                    const method = opts.method ?? 'GET';
                    assert.equal(url, ORIGIN + ADMISSION_PATH);
                    assert.equal(opts.redirect, 'manual', 'a redirect must never be followed');
                    m.calls.push(method);
                    m.before[method].shift()?.();
                    if (method === 'GET') {
                        return json(200, {
                            daemon_instance: m.instance,
                            generation: m.generation,
                            current: lifetime === m.current,
                        });
                    }
                    const req = JSON.parse(opts.body);
                    let res;
                    if (lifetime === m.current) res = json(200, { current: true });
                    else if (m.retired.has(lifetime))
                        res = json(409, { code: 'app_lifetime_retired' });
                    else if (req.daemon_instance !== m.instance || req.generation !== m.generation)
                        res = json(409, { code: 'app_admission_stale' });
                    else {
                        m.replaceWith(lifetime);
                        res = json(200, { current: true, generation: m.generation });
                    }
                    if (m.loseNextAnswer) {
                        m.loseNextAnswer = false;
                        throw new Error('the answer was lost');
                    }
                    return res;
                },
            };
        },
    };
    return m;
}

test('a lifetime is 16 random bytes in base64url, and no two are alike', () => {
    const a = newLifetime();
    assert.match(a, /^[A-Za-z0-9_-]{21}[AQgw]$/);
    assert.notEqual(a, newLifetime());
});

test('the first admission reads, then admits against what it read', async () => {
    const m = model();
    assert.deepEqual(await admit(m.ses('A'), ORIGIN), {
        state: 'admitted',
        instance: 'proc-1',
        generation: 1,
    });
    assert.deepEqual(m.calls, ['GET', 'POST']);
    assert.equal(m.current, 'A');
});

test('a lifetime that is already current admits nothing again', async () => {
    const m = model();
    m.replaceWith('A');
    assert.deepEqual(await admit(m.ses('A'), ORIGIN), {
        state: 'admitted',
        instance: 'proc-1',
        generation: 1,
    });
    assert.deepEqual(m.calls, ['GET']);
    assert.equal(m.generation, 1);
});

// smd has no state in which the app is not admitted (W-0027 C2e). A reply that claims one, as
// a daemon older than that wrote with its check off, is not believed: the app admits anyway.
test('a reply that says no admission is required is admitted to all the same', async () => {
    const m = model();
    const ses = m.ses('A');
    const claiming = {
        fetch: async (url, opts = {}) => {
            const res = await ses.fetch(url, opts);
            if ((opts.method ?? 'GET') !== 'GET') return res;
            return json(res.status, { ...(await res.json()), required: false });
        },
    };
    assert.deepEqual(await admit(claiming, ORIGIN), {
        state: 'admitted',
        instance: 'proc-1',
        generation: 1,
    });
    assert.deepEqual(m.calls, ['GET', 'POST']);
    assert.equal(m.current, 'A');
});

test('a stale answer is read again and admitted again, and then stands', async () => {
    const m = model();
    // A dead app's admission lands between this app's read and its admit.
    m.before.POST.push(() => m.replaceWith('dead'));
    // The generation it stands at is the one after its own admit, as smd reports it.
    assert.deepEqual(await admit(m.ses('B'), ORIGIN), {
        state: 'admitted',
        instance: 'proc-1',
        generation: 2,
    });
    assert.equal(m.generation, 2);
    assert.deepEqual(m.calls, ['GET', 'POST', 'GET', 'POST']);
    assert.equal(m.current, 'B');
    assert.ok(m.retired.has('dead'));
});

test('a retired lifetime ends the attempt', async () => {
    const m = model();
    m.replaceWith('A');
    m.replaceWith('B');
    assert.deepEqual(await admit(m.ses('A'), ORIGIN), { state: 'retired', instance: 'proc-1' });
    assert.deepEqual(m.calls, ['GET', 'POST']);
    assert.equal(m.current, 'B', 'the current app was disturbed');
});

// codex-126, finding 1, as the whole loop: the app is admitted but never hears so; another
// app then replaces it; when it asks again it is retired, and it never asks after that.
test('a lost success followed by a replacement ends as retired, with no further attempt', async () => {
    const m = model();
    let retiredTold = 0;
    const a = admission({
        ses: m.ses('A'),
        origin: () => ORIGIN,
        onRetired: () => retiredTold++,
    });
    m.loseNextAnswer = true;
    assert.equal((await a.admitNow()).state, 'failed');
    assert.equal(m.current, 'A', 'the model: A was admitted, only the answer was lost');

    m.replaceWith('B');
    assert.equal((await a.admitNow()).state, 'retired');
    assert.equal(m.current, 'B', 'a retired lifetime took the station back');
    assert.ok(a.isRetired());

    const before = m.calls.length;
    assert.equal((await a.admitNow()).state, 'retired');
    a.sawMarker('proc-2');
    await Promise.resolve();
    assert.equal(m.calls.length, before, 'a retired lifetime asked again');
    assert.equal(retiredTold, 0, 'retired at start is said by the waiting page, not a dialog');
});

test('anything else is a failure, and nothing is admitted', async () => {
    const answers = [
        () => json(500, {}),
        () => json(403, { code: 'desktop_app_required' }),
        () => json(404, {}),
        () => ({ status: 200, json: async () => Promise.reject(new Error('not JSON')) }),
        () => json(200, { generation: 0, current: false }), // no marker
        () => json(200, { daemon_instance: 'p', generation: 'x', current: false }),
        () => {
            throw new Error('nothing answers');
        },
    ];
    for (const answer of answers) {
        const calls = [];
        const ses = {
            fetch: async (_url, opts = {}) => {
                calls.push(opts.method ?? 'GET');
                return answer();
            },
        };
        assert.deepEqual(await admit(ses, ORIGIN), { state: 'failed' });
        assert.deepEqual(calls, ['GET'], 'an admit was sent after a read that did not answer');
    }
    // A refused admit that is neither stale nor retired.
    const m = model();
    const ses = m.ses('A');
    const real = ses.fetch;
    ses.fetch = async (url, opts = {}) =>
        (opts.method ?? 'GET') === 'POST' ? json(409, { code: 'something_else' }) : real(url, opts);
    assert.deepEqual(await admit(ses, ORIGIN), { state: 'failed' });
});

test('admissions in progress are joined, not repeated', async () => {
    let n = 0;
    let release;
    const gate = new Promise((r) => (release = r));
    const a = admission({
        ses: {},
        origin: () => ORIGIN,
        admitFn: async () => {
            n++;
            await gate;
            return { state: 'admitted', instance: 'proc-1' };
        },
    });
    const all = [a.admitNow(), a.admitNow(), a.admitNow()];
    release();
    for (const r of await Promise.all(all)) assert.equal(r.state, 'admitted');
    assert.equal(n, 1);
    await a.admitNow();
    assert.equal(n, 2, 'a later admission is its own');
});

test('a different marker makes the shell ask; the same one, or none, does not', async () => {
    const asked = [];
    let next = { state: 'admitted', instance: 'proc-1' };
    const a = admission({
        ses: {},
        origin: () => ORIGIN,
        admitFn: async () => {
            asked.push(next.instance);
            return next;
        },
    });
    a.sawMarker('proc-9');
    await Promise.resolve();
    assert.deepEqual(asked, [], 'a marker before the first admission started one');

    await a.admitNow();
    asked.length = 0;
    for (const same of ['proc-1', '', undefined, null, 7]) a.sawMarker(same);
    await Promise.resolve();
    assert.deepEqual(asked, []);

    // The daemon was restarted: its responses carry a new marker, and the read confirms it.
    next = { state: 'admitted', instance: 'proc-2' };
    a.sawMarker('proc-2');
    await new Promise((r) => setImmediate(r));
    assert.deepEqual(asked, ['proc-2']);

    // A late response from the old process: the shell asks, the read says nothing changed,
    // and it goes on with the process it is admitted to.
    asked.length = 0;
    a.sawMarker('proc-1');
    await new Promise((r) => setImmediate(r));
    assert.deepEqual(asked, ['proc-2'], 'one question, answered by the current process');
    a.sawMarker('proc-2');
    await new Promise((r) => setImmediate(r));
    assert.deepEqual(asked, ['proc-2'], 'the current marker asked again');
});

const settled = () => new Promise((r) => setImmediate(r));

// codex-129, finding 1: the shell is admitted to P1. P2's marker starts an admission; before
// it settles the workspace's stream reconnects to P3 and its marker arrives. Joining the work
// in progress must not lose that: when P2's attempt settles, late or failed, P3 is asked about.
for (const outcome of ['succeeds late', 'fails']) {
    test(`a marker seen while an admission is in progress is asked about when it settles (it ${outcome})`, async () => {
        const asked = [];
        let settleP2;
        const a = admission({
            ses: {},
            origin: () => ORIGIN,
            admitFn: () => {
                asked.push(asked.length + 1);
                if (asked.length === 1)
                    return Promise.resolve({ state: 'admitted', instance: 'P1', generation: 1 });
                if (asked.length === 2) return new Promise((res) => (settleP2 = res));
                return Promise.resolve({ state: 'admitted', instance: 'P3', generation: 1 });
            },
        });
        await a.admitNow();
        a.sawMarker('P2', '0');
        await settled();
        assert.equal(asked.length, 2, 'the P2 marker did not start an admission');
        a.sawMarker('P3', '0'); // while P2's admission is in progress
        await settled();
        assert.equal(asked.length, 2, 'a second admission was started beside the first');
        settleP2(
            outcome === 'fails'
                ? { state: 'failed' }
                : { state: 'admitted', instance: 'P2', generation: 1 }
        );
        await settled();
        await settled();
        assert.equal(asked.length, 3, 'the P3 marker seen meanwhile was never asked about');
        // And it ends there: P3 is the process admitted to, and its marker asks nothing more.
        a.sawMarker('P3', '1');
        await settled();
        assert.equal(asked.length, 3);
    });
}

// codex-130: the order in which responses arrive is not the order of the daemon's processes.
// A late response from an older process, arriving after the newer marker was seen, must not
// cancel the need to ask about the newer one. Only smd's own answer settles it.
for (const [name, late, outcome] of [
    [
        'P2 succeeds late, after a delayed response from P2 itself',
        ['P2', '1'],
        { state: 'admitted', instance: 'P2', generation: 1 },
    ],
    ['P2 fails, after a delayed response from P1', ['P1', '1'], { state: 'failed' }],
]) {
    test(`a newer process seen during an admission is still asked about when an older response follows it (${name})`, async () => {
        const asked = [];
        let settleP2;
        const a = admission({
            ses: {},
            origin: () => ORIGIN,
            admitFn: () => {
                asked.push(1);
                if (asked.length === 1)
                    return Promise.resolve({ state: 'admitted', instance: 'P1', generation: 1 });
                if (asked.length === 2) return new Promise((res) => (settleP2 = res));
                return Promise.resolve({ state: 'admitted', instance: 'P3', generation: 1 });
            },
        });
        await a.admitNow();
        a.sawMarker('P2', '0');
        await settled();
        a.sawMarker('P3', '0'); // the newer process, seen while P2's admission is in progress
        a.sawMarker(...late); // then a response that matches what the admission settles at
        settleP2(outcome);
        await settled();
        await settled();
        assert.equal(asked.length, 3, 'the newer process seen meanwhile was never asked about');
    });
}

test('what was seen meanwhile is asked about only if the settled admission does not already answer it', async () => {
    const run = async (meanwhileGeneration) => {
        const asked = [];
        let settleP2;
        const a = admission({
            ses: {},
            origin: () => ORIGIN,
            admitFn: () => {
                asked.push(1);
                if (asked.length === 2) return new Promise((res) => (settleP2 = res));
                return Promise.resolve({
                    state: 'admitted',
                    instance: asked.length === 1 ? 'P1' : 'P2',
                    generation: 1,
                });
            },
        });
        await a.admitNow();
        a.sawMarker('P2', '0');
        await settled();
        a.sawMarker('P2', meanwhileGeneration); // the same new process, seen again meanwhile
        settleP2({ state: 'admitted', instance: 'P2', generation: 1 });
        await settled();
        await settled();
        a.sawMarker('P2', '1'); // and afterwards, what it is admitted at
        await settled();
        return asked.length;
    };
    assert.equal(await run('1'), 2, 'the process and generation admitted at asked again');
    // Seen before the admit landed, the generation was still the old one. That differs from
    // what the shell is now admitted at, so it asks once more; the read says nothing has
    // changed, and it ends there.
    assert.equal(await run('0'), 3);
});

// codex-129, finding 2: another start replaces this one in the SAME daemon process. The
// marker does not change; the admission generation does, and it rides on every response.
test('replaced within the same daemon process: the changed generation makes the shell ask, and it is told once', async () => {
    let told = 0;
    const asked = [];
    let next = { state: 'admitted', instance: 'P1', generation: 1 };
    const a = admission({
        ses: {},
        origin: () => ORIGIN,
        admitFn: async () => {
            asked.push(1);
            return next;
        },
        onRetired: () => told++,
    });
    await a.admitNow();
    asked.length = 0;

    // Any response while nothing has changed, a refusal of some other kind included, asks
    // nothing and warns of nothing.
    a.sawMarker('P1', '1');
    a.sawMarker('P1', undefined);
    await settled();
    assert.deepEqual([asked.length, told], [0, 0], 'an unrelated response started an admission');

    next = { state: 'retired', instance: 'P1' };
    a.sawMarker('P1', '2'); // the same process, a later generation
    await settled();
    assert.equal(asked.length, 1, 'a changed generation did not make the shell ask');
    assert.ok(a.isRetired());
    assert.equal(told, 1);
    a.sawMarker('P1', '3');
    await settled();
    assert.deepEqual([asked.length, told], [1, 1], 'a retired lifetime asked or was told again');
});

test('retired, learnt in a loaded workspace, is said once', async () => {
    let told = 0;
    let next = { state: 'admitted', instance: 'proc-1' };
    const a = admission({
        ses: {},
        origin: () => ORIGIN,
        admitFn: async () => next,
        onRetired: () => told++,
    });
    await a.admitNow();
    next = { state: 'retired', instance: 'proc-2' };
    a.sawMarker('proc-2');
    await new Promise((r) => setImmediate(r));
    a.sawMarker('proc-3');
    await new Promise((r) => setImmediate(r));
    assert.equal(told, 1);
    assert.ok(a.isRetired());
});

test('the marker is read from responses of the pinned origin only, and changes none', () => {
    let listener;
    const ses = { webRequest: { onHeadersReceived: (fn) => (listener = fn) } };
    const seen = [];
    let origin = null;
    watchDaemon(ses, { origin: () => origin, onMarker: (m) => seen.push(m) });
    const deliver = (url, responseHeaders) => {
        let answer;
        listener({ url, responseHeaders }, (a) => (answer = a));
        assert.deepEqual(answer, {}, 'the response was held or changed');
    };

    deliver(ORIGIN + '/v1/version', { [MARKER_HEADER]: ['p1'] });
    assert.deepEqual(seen, [], 'a marker counted before any origin was pinned');

    origin = ORIGIN;
    deliver(ORIGIN + '/v1/version', { 'X-Sm-Daemon-Instance': ['p1'] });
    deliver('wss://127.0.0.1:8443/v1/events/ws', { [MARKER_HEADER]: 'p2' });
    deliver(ORIGIN + '/v1/ft8/events', { [MARKER_HEADER]: ['p3'], other: ['x'] });
    assert.deepEqual(seen, ['p1', 'p2', 'p3']);
    // The admission generation is handed on with the marker it came with.
    const pairs = [];
    watchDaemon(ses, { origin: () => origin, onMarker: (m, g) => pairs.push([m, g]) });
    deliver(ORIGIN + '/v1/version', {
        [MARKER_HEADER]: ['p1'],
        'X-Sm-Admission-Generation': ['4'],
    });
    deliver(ORIGIN + '/v1/version', { [MARKER_HEADER]: ['p1'] });
    assert.deepEqual(pairs, [
        ['p1', '4'],
        ['p1', undefined],
    ]);
    watchDaemon(ses, { origin: () => origin, onMarker: (m) => seen.push(m) });

    seen.length = 0;
    deliver('https://127.0.0.1:9999/v1/version', { [MARKER_HEADER]: ['other-port'] });
    deliver('http://127.0.0.1:8443/v1/version', { [MARKER_HEADER]: ['downgrade'] });
    deliver('https://example.invalid/', { [MARKER_HEADER]: ['elsewhere'] });
    deliver(ORIGIN + ADMISSION_PATH, { [MARKER_HEADER]: ['own'] });
    deliver(ORIGIN + '/v1/version', {});
    deliver(ORIGIN + '/v1/version', undefined);
    assert.deepEqual(seen, [], 'a marker was taken from the wrong response');
});

test('the admission path is recognised, whatever its query', () => {
    assert.ok(isAdmissionRequest(ORIGIN + ADMISSION_PATH));
    assert.ok(isAdmissionRequest(ORIGIN + ADMISSION_PATH + '?x=1'));
    assert.ok(!isAdmissionRequest(ORIGIN + '/v1/version'));
    assert.ok(!isAdmissionRequest('not a url'));
});
