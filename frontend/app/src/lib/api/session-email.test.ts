// F-04b (ADR 0078): a session-email send is the worst write to double-fire — a
// real message in a real inbox. A FIRED timeout is the ambiguous case the caller
// must steer with the shared "outcome unknown" lead, so the outcome must carry
// timedOut. A generic (non-timeout) network failure keeps its existing cautious
// wording and carries NO timedOut marker. safeFetch/readJsonBody are the real
// thing over a mocked fetch.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { sendSessionEmail } from './session-email';
import { EMAIL_TIMEOUT_MS } from './_helpers';

afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});

const timeoutError = (): Error =>
    Object.assign(new Error('request timed out'), { name: 'TimeoutError' });

const REQ = { to: 'qsl@example.com', uuids: ['u-1'] };

describe('sendSessionEmail — F-04b timed-out ambiguity', () => {
    it('carries timedOut on a fired timeout', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(timeoutError()))
        );
        const out = await sendSessionEmail(REQ);
        expect(out.kind).toBe('network');
        if (out.kind !== 'network') return;
        expect(out.timedOut).toBe(true);
    });

    it('leaves a generic (non-timeout) network failure without a timedOut marker', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('connection refused')))
        );
        const out = await sendSessionEmail(REQ);
        expect(out.kind).toBe('network');
        if (out.kind !== 'network') return;
        expect(out.timedOut).toBeUndefined();
    });

    // Email keeps its own long window (it must outlast the daemon's SMTP ceiling);
    // F-04b does not change that.
    it('still POSTs with the email timeout, not the write default', async () => {
        const spy = vi.spyOn(AbortSignal, 'timeout');
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.resolve(
                    new Response(JSON.stringify({ status: 'sent', emailed: ['u-1'], date: '' }), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                )
            )
        );
        await sendSessionEmail(REQ);
        expect(spy).toHaveBeenCalledWith(EMAIL_TIMEOUT_MS);
    });
});

// Slice 5 (W-0021 revision 3): the dialog validates the partition itself; this
// layer only refuses to invent lists. Each of sent, omitted and emailed must be
// an array of strings, or `lists` is null. `emailed` keeps its lenient meaning
// for the Logbook's caller.
describe('sendSessionEmail — sent/omitted/emailed lists', () => {
    const reply = (body: unknown) =>
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.resolve(
                    new Response(JSON.stringify(body), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                )
            )
        );

    it('returns the three lists as sent', async () => {
        reply({ status: 'sent', sent: ['a', 'a'], omitted: ['b'], emailed: ['a'], date: '' });
        const out = await sendSessionEmail(REQ);
        expect(out.kind === 'sent' && out.lists).toEqual({
            sent: ['a', 'a'],
            omitted: ['b'],
            emailed: ['a'],
        });
    });

    it.each([
        ['no sent', { omitted: [], emailed: ['a'] }],
        ['no omitted', { sent: ['a'], emailed: ['a'] }],
        ['null emailed', { sent: ['a'], omitted: [], emailed: null }],
        ['a non-string entry', { sent: ['a', 7], omitted: [], emailed: ['a'] }],
        ['a string, not a list', { sent: 'a', omitted: [], emailed: ['a'] }],
    ])('%s gives no lists', async (_name, body) => {
        reply({ status: 'sent', date: '', ...body });
        const out = await sendSessionEmail(REQ);
        expect(out.kind).toBe('sent');
        expect(out.kind === 'sent' && out.lists).toBeNull();
    });
});
