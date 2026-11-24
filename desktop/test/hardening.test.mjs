// The request filter (ADR 0102; W-0027 A1), and its switch for a close (W-0027 A3, B5): from
// the moment a close is confirmed the page starts nothing new. Electron is not loaded: the
// session is a recorder that keeps the listener it was given.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { hardenSession } from '../src/hardening.mjs';

const ORIGIN = 'https://127.0.0.1:8443';
const MARK = `${ORIGIN}/v1/ft8/tx/arm?shell=1`;

function session() {
    const s = { listener: null };
    s.ses = {
        setPermissionRequestHandler() {},
        setPermissionCheckHandler() {},
        setDevicePermissionHandler() {},
        setSpellCheckerEnabled() {},
        setSpellCheckerLanguages() {},
        webRequest: { onBeforeRequest: (fn) => (s.listener = fn) },
    };
    s.verdict = (url, method = 'GET') => {
        let out;
        s.listener({ url, method, webContentsId: 1 }, (r) => (out = r));
        return out.cancel === true ? 'cancelled' : 'allowed';
    };
    return s;
}

function hardened() {
    const s = session();
    s.refused = [];
    s.blocked = [];
    s.writes = hardenSession(s.ses, {
        origin: () => ORIGIN,
        onBlocked: (url) => s.blocked.push(url),
        onWriteRefused: (d) => s.refused.push(`${d.method} ${d.url}`),
        isShellRequest: (url) => url === MARK,
    });
    return s;
}

test('ordinarily the page reads and writes on the served origin, and nowhere else', () => {
    const s = hardened();
    assert.equal(s.verdict(`${ORIGIN}/v1/qsos`, 'POST'), 'allowed');
    assert.equal(s.verdict(`${ORIGIN}/v1/qsos`), 'allowed');
    assert.equal(s.verdict('https://example.org/x'), 'cancelled');
    assert.deepEqual(s.refused, []);
});

test('closing: the page’s writes are refused, each one, and its reads are not', () => {
    const s = hardened();
    s.writes.refuse();
    for (const method of ['POST', 'PUT', 'PATCH', 'DELETE']) {
        assert.equal(s.verdict(`${ORIGIN}/v1/ft8/tx/arm`, method), 'cancelled', method);
    }
    assert.equal(s.refused.length, 4, 'each refused write is reported');
    assert.equal(s.verdict(`${ORIGIN}/v1/operating-session`, 'GET'), 'allowed');
    assert.equal(s.verdict(`${ORIGIN}/`, 'HEAD'), 'allowed');
    assert.equal(s.verdict('wss://127.0.0.1:8443/v1/events/ws', 'GET'), 'allowed');
    assert.equal(s.verdict('data:text/html,x'), 'allowed');
    assert.deepEqual(s.blocked, [], 'a refused write is not an off-origin request');
});

test('closing: the shell’s own write passes, by its mark and not by who is said to have sent it', () => {
    const s = hardened();
    s.writes.refuse();
    assert.equal(s.verdict(MARK, 'POST'), 'allowed');
    // The same address without the mark, as a page would send it:
    assert.equal(s.verdict(`${ORIGIN}/v1/ft8/tx/arm`, 'POST'), 'cancelled');
    // And a request that names no window at all (a worker's) is still the page's:
    let out;
    s.listener({ url: `${ORIGIN}/v1/qsos`, method: 'POST' }, (r) => (out = r));
    assert.equal(out.cancel, true);
});

test('closing: the origin rule is untouched; the mark opens nothing off the origin', () => {
    const s = hardened();
    s.writes.refuse();
    assert.equal(s.verdict('https://example.org/v1/ft8/tx/arm?shell=1', 'POST'), 'cancelled');
    assert.equal(s.verdict('https://example.org/x'), 'cancelled');
    assert.equal(s.blocked.length, 2);
});

test('a close cancelled: the page writes again', () => {
    const s = hardened();
    s.writes.refuse();
    assert.equal(s.verdict(`${ORIGIN}/v1/qsos`, 'POST'), 'cancelled');
    s.writes.allow();
    assert.equal(s.verdict(`${ORIGIN}/v1/qsos`, 'POST'), 'allowed');
});
