// W-0027 D1: the start-up trace (one stderr line per state change, for the journal and the
// production smoke check). Bounded labels and paths only; never page contents.
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { test } from 'node:test';
import { pageLabel, traceLine, traceWindow } from '../src/trace.mjs';
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
