// W-0027 D1, operator ruling Q3 (a): when the daemon may not be running, the waiting page says how
// to start it; an identity refusal keeps its own explanation and no start advice.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { waitingPageUrl } from '../src/waiting.mjs';

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
