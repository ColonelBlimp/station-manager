// W-0027 D1: the start-up trace (one stderr line per state change, for the journal and the
// production smoke check). Bounded labels and paths only; never page contents.
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { test } from 'node:test';
import {
    addressLabel,
    blockedRequestLine,
    pageLabel,
    refusedRedirectLine,
    traceLine,
    traceWindow,
} from '../src/trace.mjs';
import { waitingPageUrl } from '../src/waiting.mjs';

const origin = 'https://127.0.0.1:8443';

test('a trace line is one prefixed line; control characters in a path cannot split it', () => {
    assert.equal(
        traceLine('pinned https://127.0.0.1:8443'),
        'station-manager-desktop: pinned https://127.0.0.1:8443\n'
    );
    const l = traceLine('waiting: x /tmp/a\nforged: line\r\x1b[2J');
    assert.equal(l.split('\n').length, 2, JSON.stringify(l));
    assert.ok(l.endsWith('\n'));
    // eslint-disable-next-line no-control-regex -- the forged control characters are the point
    assert.ok(!/[\r\x1b]/.test(l), JSON.stringify(l));
});

test('page labels: the local waiting page, the app, anything else; never the URL itself', () => {
    assert.equal(pageLabel(waitingPageUrl(null), origin), 'waiting page');
    assert.equal(
        pageLabel(waitingPageUrl(null, { state: 'refused', path: '/p', reason: 'r' }), origin),
        'waiting page'
    );
    // codex-077: identity, not scheme. Unrelated, blank and non-HTML data: documents are not the
    // waiting page, nor is a waiting document the shell did not generate (here, one altered).
    for (const u of [
        'data:text/html,<h1>Wrong page</h1>',
        'data:text/html,',
        'data:application/json,%7B%7D',
        waitingPageUrl(null) + '%20',
    ]) {
        assert.equal(pageLabel(u, origin), 'other', u.slice(0, 60));
    }
    assert.equal(pageLabel(origin + '/', origin), 'app');
    assert.equal(pageLabel(origin + '/logbook', origin), 'app');
    assert.equal(pageLabel(origin + '/', null), 'other');
    assert.equal(pageLabel('https://example.com/', origin), 'other');
    assert.equal(pageLabel('about:blank', origin), 'other');
});

function fakeContents(pid = 4242) {
    const wc = new EventEmitter();
    wc.url = '';
    wc.getURL = () => wc.url;
    wc.getOSProcessId = () => pid;
    return wc;
}

test('a finished load is traced with its label and the renderer pid of that same contents', () => {
    const lines = [];
    const wc = fakeContents(4242);
    traceWindow(
        wc,
        () => origin,
        (l) => lines.push(l)
    );
    wc.url = waitingPageUrl(null);
    wc.emit('did-finish-load');
    wc.url = origin + '/';
    wc.emit('did-finish-load');
    assert.deepEqual(lines, ['loaded waiting page renderer 4242', 'loaded app renderer 4242']);
});

test('only a finished load counts: a started navigation is not traced as loaded', () => {
    const lines = [];
    const wc = fakeContents();
    traceWindow(
        wc,
        () => origin,
        (l) => lines.push(l)
    );
    wc.url = waitingPageUrl(null);
    wc.emit('did-start-loading');
    wc.emit('did-start-navigation', {}, wc.url, false, true);
    assert.deepEqual(lines, []);
});

test('a main-frame load failure and a lost renderer are traced; subframe failures are not', () => {
    const lines = [];
    const wc = fakeContents();
    traceWindow(
        wc,
        () => origin,
        (l) => lines.push(l)
    );
    wc.emit('did-fail-load', {}, -105, 'NAME_NOT_RESOLVED', origin + '/', true);
    wc.emit('did-fail-load', {}, -2, 'FAILED', origin + '/frame', false);
    // -3 is a load superseded or stopped (the shell replacing its own page), not a failure.
    wc.emit('did-fail-load', {}, -3, 'ABORTED', waitingPageUrl(null), true);
    wc.emit('render-process-gone', {}, { reason: 'crashed', exitCode: 11 });
    assert.deepEqual(lines, ['load failed app -105', 'renderer gone crashed']);
});

test('the waiting-page memory is bounded; the latest documents are always recognised', () => {
    const urls = [];
    for (let i = 0; i < 100; i++)
        urls.push(waitingPageUrl(null, { state: 'waiting', path: `/p${i}`, reason: 'r' }));
    assert.equal(pageLabel(urls.at(-1), origin), 'waiting page');
    assert.equal(pageLabel(urls[0], origin), 'other', 'the memory is unbounded');
});

// The two diagnostics for what the shell refused (a request outside the origin, a redirect
// off it) name an address by its scheme, host and port, and by nothing else of it. A
// query, a path, user-info or a fragment can carry data, and the daemon choosing a redirect
// does not make its address free of it (review, 2026-11-19).
const SECRETS = [
    'src-query-TOKEN',
    'dst-USER',
    'dst-PASSWORD',
    'dst-query-TOKEN',
    'dst-FRAGMENT',
    'dst-PATH',
    'src-PATH',
];

test('an address label is the scheme, host and port only', () => {
    assert.equal(addressLabel('https://127.0.0.1:9999/a/b?x=1#f'), 'https://127.0.0.1:9999');
    assert.equal(addressLabel('https://user:pw@example.invalid/p?q=1'), 'https://example.invalid');
    assert.equal(addressLabel('http://127.0.0.1:8443/x'), 'http://127.0.0.1:8443');
    assert.equal(addressLabel('wss://127.0.0.1:8443/v1/events/ws?t=1'), 'wss://127.0.0.1:8443');
    // Relative to the address it came with, as a redirect is.
    assert.equal(addressLabel('/app/?k=v', origin + '/redirect?to=x'), origin);
    assert.equal(addressLabel('//example.invalid/x?k=v', origin + '/'), 'https://example.invalid');
    // No host to name: the scheme alone. Not an address: a fixed label.
    assert.equal(addressLabel('data:text/html,<p>dst-PATH</p>'), 'data:');
    assert.equal(addressLabel('mailto:dst-USER@example.invalid'), 'mailto:');
    assert.equal(addressLabel('https://[not-an-address/x?dst-query-TOKEN'), '[not an address]');
    assert.equal(addressLabel('', undefined), '[not an address]');
    assert.equal(addressLabel(undefined), '[not an address]');
    // Bounded: a host of any length cannot make a line of any length.
    assert.ok(addressLabel(`https://${'a'.repeat(500)}.invalid/`).length <= 120);
});

test('the blocked-request line names the origin asked for, and none of the address’s data', () => {
    const line = blockedRequestLine(
        'https://dst-USER:dst-PASSWORD@127.0.0.1:9999/dst-PATH?t=dst-query-TOKEN#dst-FRAGMENT'
    );
    assert.equal(
        line,
        'shell: blocked a request outside the served origin: https://127.0.0.1:9999'
    );
    for (const secret of SECRETS) assert.ok(!line.includes(secret), `${secret} is in: ${line}`);
});

test('the refused-redirect line names two origins, and none of either address’s data', () => {
    const from = `${origin}/src-PATH?to=https%3A%2F%2F127.0.0.1%3A9999%2Fdst-PATH%3Ft%3Ddst-query-TOKEN&s=src-query-TOKEN`;
    const to =
        'https://dst-USER:dst-PASSWORD@127.0.0.1:9999/dst-PATH?t=dst-query-TOKEN#dst-FRAGMENT';
    const line = refusedRedirectLine(from, to);
    assert.equal(
        line,
        `shell: refused a redirect off the served origin: ${origin} -> https://127.0.0.1:9999`
    );
    for (const secret of SECRETS) assert.ok(!line.includes(secret), `${secret} is in: ${line}`);

    // A target that is not one address has a fixed label, whatever it held.
    assert.equal(
        refusedRedirectLine(from, 'https://[not-an-address/dst-PATH?dst-query-TOKEN'),
        `shell: refused a redirect off the served origin: ${origin} -> [not an address]`
    );
    assert.equal(
        refusedRedirectLine(from, ''),
        `shell: refused a redirect off the served origin: ${origin} -> [no single address]`
    );
    // A relative target is on the address it came from.
    assert.equal(
        refusedRedirectLine('http://127.0.0.1:8443/x', '/y?dst-query-TOKEN'),
        'shell: refused a redirect off the served origin: http://127.0.0.1:8443 -> http://127.0.0.1:8443'
    );
    // One line, whatever the addresses hold.
    const forged = refusedRedirectLine(from, 'https://127.0.0.1:9999/a\nshell: forged');
    assert.equal(forged.split('\n').length, 1, JSON.stringify(forged));
});
