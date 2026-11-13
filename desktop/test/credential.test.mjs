// W-0027 C2b: the operating credential the shell presents (ADR 0103 point 7). The rule for
// which request carries it is a pure function, tested here; the Electron scenario
// (npm run check:c2b) proves the wiring against TLS fixtures.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    CREDENTIAL_HEADER,
    credentialHeaders,
    onceCredential,
    watchRefusal,
} from '../src/credential.mjs';

const ORIGIN = 'https://127.0.0.1:8443';
const VALUE = 'v'.repeat(43);
const sent = (url, { origin = ORIGIN, value = VALUE, headers = {} } = {}) =>
    credentialHeaders(headers, url, origin, value);

test('the credential goes to the pinned origin and its wss partner, exactly', () => {
    for (const url of [
        ORIGIN + '/',
        ORIGIN + '/v1/version',
        ORIGIN + '/manual/index.html',
        ORIGIN + '/v1/events?x=1',
        'wss://127.0.0.1:8443/v1/events/ws',
    ]) {
        assert.deepEqual(sent(url), { [CREDENTIAL_HEADER]: VALUE }, url);
    }
});

test('it goes nowhere else', () => {
    for (const url of [
        'http://127.0.0.1:8443/', // a plain-HTTP downgrade of the origin
        'ws://127.0.0.1:8443/v1/events/ws',
        'https://127.0.0.1:8444/', // another port
        'https://127.0.0.1/', // the default port
        'https://localhost:8443/', // another name for the same address
        'https://[::1]:8443/',
        'https://127.0.0.1.example:8443/',
        'https://127.0.0.1:8443@evil.example/',
        'https://evil.example/?https://127.0.0.1:8443/',
        'data:text/html,x',
        'blob:https://127.0.0.1:8443/1',
        'file:///etc/passwd',
        'not a url',
    ]) {
        assert.deepEqual(sent(url), {}, url);
    }
});

test('nothing is sent before the origin and the credential are fixed, or to a plain-HTTP origin', () => {
    assert.deepEqual(sent(ORIGIN + '/', { origin: null }), {});
    assert.deepEqual(sent(ORIGIN + '/', { value: null }), {});
    assert.deepEqual(sent(ORIGIN + '/', { value: '' }), {});
    assert.deepEqual(sent('http://127.0.0.1:8080/', { origin: 'http://127.0.0.1:8080' }), {});
    assert.deepEqual(sent('ws://127.0.0.1:8080/ws', { origin: 'http://127.0.0.1:8080' }), {});
});

test('a header of that name already on the request is replaced, in any letter case', () => {
    const headers = {
        Accept: '*/*',
        'x-sm-operating-credential': 'from-the-page',
        'X-SM-OPERATING-CREDENTIAL': 'again',
        'X-Sm-Operating-Credential': 'and-again',
    };
    assert.deepEqual(sent(ORIGIN + '/v1/qso', { headers }), {
        Accept: '*/*',
        [CREDENTIAL_HEADER]: VALUE,
    });
    // Off the origin it is removed and not replaced: a page cannot make the shell carry one.
    assert.deepEqual(sent('https://127.0.0.1:9/', { headers }), { Accept: '*/*' });
    // The request's own headers are not modified in place.
    assert.equal(headers['x-sm-operating-credential'], 'from-the-page');
});

test('the credential is fixed once and never printed', () => {
    const c = onceCredential();
    assert.equal(c.get(), null);
    assert.throws(() => c.set(''), /empty/);
    assert.throws(() => c.set(null), /empty/);
    c.set(VALUE);
    assert.equal(c.get(), VALUE);
    assert.throws(() => c.set('another'), /already/);
    assert.equal(c.get(), VALUE);
    assert.ok(!JSON.stringify({ c }).includes(VALUE));
    assert.ok(!String(c).includes(VALUE));
});

// --- a refusal in a loaded workspace ------------------------------------------------------

/** The shell's own probe, as the watcher is told to recognise it. */
const PROBE = ORIGIN + '/v1/version?sm-probe=nonce';

/** A session whose listeners the test fires, and a confirm the test answers. */
function refusalFixture({ origin = ORIGIN } = {}) {
    let listener = null;
    let errorListener = null;
    const ses = {
        webRequest: {
            onCompleted: (fn) => (listener = fn),
            onErrorOccurred: (fn) => (errorListener = fn),
        },
    };
    const asked = [];
    const told = [];
    let answer = () => true;
    watchRefusal(ses, {
        origin: () => origin,
        isProbe: (url) => url === PROBE,
        confirm: (s, o) => {
            asked.push(o);
            return new Promise((resolve) => (answer = resolve));
        },
        onRefused: () => told.push(1),
    });
    const settle = () => new Promise((r) => setImmediate(r));
    return {
        complete: (details) => listener({ webContentsId: 7, statusCode: 403, ...details }),
        // A request that ended without a response the shell can read, as a refused WebSocket
        // upgrade does: no status code at all (Electron 44, measured in codex-112).
        fail: (details) =>
            errorListener({
                webContentsId: 7,
                resourceType: 'webSocket',
                error: 'net::ERR_FAILED',
                ...details,
            }),
        answer: async (v) => {
            answer(v);
            await settle();
        },
        asked,
        told,
    };
}

test('a page\u2019s 403 on the pinned origin makes the shell ask smd itself, and tell once', async () => {
    const f = refusalFixture();
    f.complete({ url: ORIGIN + '/v1/qso' });
    assert.deepEqual(f.asked, [ORIGIN], 'the shell did not ask smd');
    assert.equal(f.told.length, 0, 'told before smd confirmed');
    f.complete({ url: ORIGIN + '/v1/events' }); // more refusals while the question is out
    f.complete({ url: ORIGIN + '/v1/config' });
    assert.equal(f.asked.length, 1, 'a second question was started while one was out');
    await f.answer(true);
    assert.equal(f.told.length, 1);
    f.complete({ url: ORIGIN + '/v1/qso' }); // once told, it is not said again or asked again
    await f.answer(true);
    assert.equal(f.told.length, 1, 'told twice');
    assert.equal(f.asked.length, 1, 'asked again after telling');
});

test('smd says it is not the credential: nothing is told, and a later refusal asks again', async () => {
    const f = refusalFixture();
    f.complete({ url: ORIGIN + '/v1/qso' });
    await f.answer(false); // a 403 of another kind (an archive guard, an origin refusal)
    assert.equal(f.told.length, 0);
    f.complete({ url: ORIGIN + '/v1/qso' });
    assert.equal(f.asked.length, 2, 'the shell stopped asking after one other 403');
    await f.answer(true);
    assert.equal(f.told.length, 1);
});

test('nothing but a page\u2019s 403 on the pinned origin prompts the question', async () => {
    const f = refusalFixture();
    f.complete({ url: ORIGIN + '/v1/qso', statusCode: 200 });
    f.complete({ url: ORIGIN + '/v1/qso', statusCode: 401 });
    f.complete({ url: ORIGIN + '/v1/qso', statusCode: 500 });
    f.complete({ url: 'https://127.0.0.1:9/v1/qso' }); // another port
    f.complete({ url: 'http://127.0.0.1:8443/v1/qso' }); // a downgrade
    f.complete({ url: PROBE, webContentsId: undefined }); // the shell's own probe
    f.complete({ url: PROBE }); // recognised by what it is, not by who is said to have sent it
    f.fail({ url: PROBE }); // and were it to fail as a WebSocket does, it still would not
    f.fail({ url: ORIGIN + '/v1/qso', resourceType: 'xhr' }); // a fetch that failed: the daemon is down
    f.fail({ url: 'wss://127.0.0.1:9/v1/events/ws' }); // a WebSocket to another port
    f.fail({ url: 'ws://127.0.0.1:8443/v1/events/ws' }); // a downgrade
    assert.deepEqual(f.asked, []);
    const unpinned = refusalFixture({ origin: null });
    unpinned.complete({ url: ORIGIN + '/v1/qso' });
    assert.deepEqual(unpinned.asked, [], 'asked before any origin was pinned');
});

test('a failed question tells nothing and does not stop the next one', async () => {
    let listener = null;
    const ses = { webRequest: { onCompleted: (fn) => (listener = fn), onErrorOccurred: () => {} } };
    const told = [];
    let calls = 0;
    watchRefusal(ses, {
        origin: () => ORIGIN,
        isProbe: () => false,
        confirm: () => {
            calls += 1;
            return calls === 1 ? Promise.reject(new Error('probe failed')) : Promise.resolve(true);
        },
        onRefused: () => told.push(1),
    });
    const fire = () => listener({ webContentsId: 7, statusCode: 403, url: ORIGIN + '/v1/qso' });
    fire();
    await new Promise((r) => setImmediate(r));
    assert.equal(told.length, 0);
    fire();
    await new Promise((r) => setImmediate(r));
    assert.equal(told.length, 1, 'a failed question left the shell unable to ask again');
});

// codex-112: two paths where the first refused request carries no window, or no status.
test('a request without a window (a shared worker) prompts the question like any other', async () => {
    const f = refusalFixture();
    f.complete({ url: ORIGIN + '/v1/qso', webContentsId: undefined });
    assert.deepEqual(f.asked, [ORIGIN], 'a 403 without a web contents was taken for the probe');
    await f.answer(true);
    assert.equal(f.told.length, 1);
});

test('a WebSocket that fails on the pinned origin prompts the question, and smd decides the cause', async () => {
    const refusedOne = refusalFixture();
    refusedOne.fail({ url: 'wss://127.0.0.1:8443/v1/events/ws' });
    assert.deepEqual(
        refusedOne.asked,
        [ORIGIN],
        'a failed WebSocket upgrade did not prompt the question'
    );
    await refusedOne.answer(true);
    assert.equal(refusedOne.told.length, 1);

    const daemonDown = refusalFixture();
    daemonDown.fail({
        url: 'wss://127.0.0.1:8443/v1/events/ws',
        error: 'net::ERR_CONNECTION_REFUSED',
    });
    assert.equal(daemonDown.asked.length, 1);
    await daemonDown.answer(false); // smd does not say it refused the credential: nothing is told
    assert.equal(daemonDown.told.length, 0);
});

test('the shell\u2019s own question never prompts another, whatever its answer', async () => {
    const f = refusalFixture();
    f.complete({ url: ORIGIN + '/v1/qso' });
    await f.answer(false); // the question is over; its own completion arrives only now
    f.complete({ url: PROBE, webContentsId: undefined });
    f.complete({ url: PROBE });
    assert.equal(f.asked.length, 1, 'the question prompted itself');
});
