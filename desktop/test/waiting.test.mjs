// W-0027 D1, operator ruling Q3 (a): when the daemon may not be running, the waiting page says how
// to start it; an identity refusal keeps its own explanation and no start advice.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { isShellProbe, probeUrl, waitingPageUrl } from '../src/waiting.mjs';

const page = (u) => decodeURIComponent(u);
const START = 'smctl start';

test('the pinned daemon does not answer: the page shows how to start it', () => {
    const p = page(waitingPageUrl('https://127.0.0.1:8443'));
    assert.ok(p.includes(START), p);
    assert.ok(p.includes('data-sm-start'), 'the guidance is not marked');
});

test('smd has not run yet (waiting): the page shows how to start it, with the reason', () => {
    const p = page(
        waitingPageUrl(null, {
            state: 'waiting',
            path: '/h/.config/station-manager/desktop-identity',
            reason: 'smd has not created its identity yet',
        })
    );
    assert.ok(p.includes(START), p);
    assert.ok(p.includes('smd has not created its identity yet'));
});

test('an identity refusal keeps its explanation and gives no start advice', () => {
    const p = page(
        waitingPageUrl(null, { state: 'refused', path: '/p', reason: 'must be mode 0700' })
    );
    assert.ok(p.includes('must be mode 0700'));
    assert.ok(!p.includes(START), 'start advice on a refusal: starting smd does not fix it');
});

// W-0027 C2b: the operating credential's two refusals are told apart from the identity's.
test('an unusable credential file is named as the credential, with no start advice', () => {
    const p = page(
        waitingPageUrl(null, {
            state: 'refused',
            subject: 'credential',
            path: '/p/desktop-credential/credential',
            reason: 'must be mode 0600 (is 0644)',
        })
    );
    assert.ok(p.includes('operating credential'), p);
    assert.ok(!p.includes('identity'), 'a credential refusal is described as an identity one');
    assert.ok(p.includes('must be mode 0600'));
    assert.ok(!p.includes(START));
});

test('the daemon did not accept the credential: restart the app, then the daemon', () => {
    const p = page(waitingPageUrl('https://127.0.0.1:8443', { state: 'unaccepted' }));
    assert.ok(p.includes('data-sm-state="unaccepted"'), p);
    assert.ok(p.includes('did not accept'), p);
    assert.ok(p.includes('smctl restart'), p);
    assert.ok(!p.includes(START + '<'), 'start advice where the daemon is running');
    assert.ok(p.includes('https://127.0.0.1:8443'));
});

// codex-112: the shell's own probe is recognised by a mark only the main process knows, never
// by who Electron says sent it.
test('the shell\u2019s probe asks /v1/version and is recognised only by its own mark', () => {
    const u = new URL(probeUrl('https://127.0.0.1:8443'));
    assert.equal(u.origin, 'https://127.0.0.1:8443');
    assert.equal(u.pathname, '/v1/version');
    assert.ok(isShellProbe(u.href));
    assert.ok(isShellProbe(u.href.replace('https:', 'wss:')), 'the mark, not the scheme');
    const mark = [...u.searchParams.values()][0];
    assert.ok(mark.length >= 32, 'the mark is too short to be unguessable');
    for (const other of [
        'https://127.0.0.1:8443/v1/version', // a page's own request for the same endpoint
        'https://127.0.0.1:8443/v1/version?' + [...u.searchParams.keys()][0] + '=guess',
        u.href.replace('/v1/version', '/v1/qso'),
        'not a url',
        '',
    ]) {
        assert.ok(!isShellProbe(other), other);
    }
});
