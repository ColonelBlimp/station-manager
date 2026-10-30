import { afterEach, describe, expect, it, vi } from 'vitest';
import { ARCHIVE_HEADER, setBoundArchive } from './archive-guard';
import { TEST_ARCHIVE } from '../../test/archive';
import { fetchDefaultChangeStatus, requestDefaultChange } from './default-change';

// W-0024 slice 5a: the Set as default wire. Unsupported (404) is not unreadable;
// an answer that cannot prove what happened is an unknown outcome.

const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const B = '01a187bc-0200-7000-8000-000000000002';
const REQ = '01a187bc-0300-7000-8000-000000000003';

type Sent = { url: string; method: string; body: string | null; archive: string | null };
let sent: Sent[];

function stub(status: number, body: unknown, raw = false): void {
    sent = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            sent.push({
                url: input,
                method: (init?.method ?? 'GET').toUpperCase(),
                body: typeof init?.body === 'string' ? init.body : null,
                archive: new Headers(init?.headers).get(ARCHIVE_HEADER),
            });
            return Promise.resolve(
                new Response(raw ? String(body) : JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
}

function stubFailure(err: Error): void {
    sent = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            sent.push({ url: input, method: init?.method ?? 'GET', body: null, archive: null });
            return Promise.reject(err);
        })
    );
}

afterEach(() => {
    setBoundArchive(TEST_ARCHIVE);
    vi.unstubAllGlobals();
});

const effective = { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' };

function held(over: Record<string, unknown> = {}): Record<string, unknown> {
    return {
        request_id: REQ,
        logbook_uuid: B,
        state: 'pending_restart',
        unconfirmed: true,
        ...over,
    };
}

// The daemon's states (internal/archive/default_change.go).
const STATES = [
    'pending_restart',
    'pending_unclear',
    'applied_cleanup_pending',
    'withdrawal_unconfirmed',
    'restart_failed',
    'cancelled',
    'withdrawn',
    'not_applied_abandoned',
    'effective',
];

describe('fetchDefaultChangeStatus', () => {
    it('reads the effective default with no change in play', async () => {
        stub(200, { effective });
        expect(await fetchDefaultChangeStatus()).toEqual({
            kind: 'ok',
            status: { effective, change: null },
        });
        expect(sent).toEqual([
            expect.objectContaining({ url: '/v1/default-logbook', method: 'GET' }),
        ]);
    });

    it('reads a change in play', async () => {
        stub(200, {
            effective,
            change: {
                request_id: REQ,
                logbook_uuid: B,
                state: 'pending_unclear',
                failure_code: 'default_change_pending_unclear',
                unconfirmed: true,
            },
        });
        expect(await fetchDefaultChangeStatus()).toEqual({
            kind: 'ok',
            status: {
                effective,
                change: {
                    requestId: REQ,
                    logbookUuid: B,
                    state: 'pending_unclear',
                    removed: '',
                    failureCode: 'default_change_pending_unclear',
                    unconfirmed: true,
                },
            },
        });
    });

    it.each(STATES)('the daemon’s state %s is readable', async (state) => {
        stub(200, { effective, change: held({ state, unconfirmed: false }) });
        const out = await fetchDefaultChangeStatus();
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') expect(out.status.change?.state).toBe(state);
    });

    it('a 404 (routes not served) is unsupported', async () => {
        stub(404, { code: 'not_found', message: 'no route' });
        expect(await fetchDefaultChangeStatus()).toEqual({ kind: 'unsupported' });
    });

    it.each([
        ['a server error', 500, { code: 'default_change_status_failed', message: 'x' }, false],
        ['a 503', 503, { code: 'default_change_unavailable', message: 'x' }, false],
        ['malformed JSON', 200, '{not json', true],
        ['no effective default', 200, { change: null }, false],
        ['a change without a request id', 200, { effective, change: { state: 'x' } }, false],
        [
            'a malformed effective uuid',
            200,
            { effective: { ...effective, uuid: 'not-a-uuid' } },
            false,
        ],
        [
            'a change without unconfirmed',
            200,
            { effective, change: held({ unconfirmed: undefined }) },
            false,
        ],
        [
            'a non-boolean unconfirmed',
            200,
            { effective, change: held({ unconfirmed: 'true' }) },
            false,
        ],
        ['an unknown state', 200, { effective, change: held({ state: 'pending_soon' }) }, false],
    ] as [string, number, unknown, boolean][])(
        '%s is unreadable — never idle, never unsupported',
        async (_name, status, body, raw) => {
            stub(status, body, raw);
            expect((await fetchDefaultChangeStatus()).kind).toBe('unreadable');
        }
    );

    it('no answer is a network outcome, distinct from unsupported', async () => {
        stubFailure(new TypeError('Failed to fetch'));
        expect((await fetchDefaultChangeStatus()).kind).toBe('network');
    });
});

describe('requestDefaultChange', () => {
    it('sends the target and the believed default, archive-guarded', async () => {
        stub(202, { request_id: REQ, durability: 'durable' });
        expect(await requestDefaultChange(B, A)).toEqual({
            kind: 'accepted',
            requestId: REQ,
            durability: 'durable',
        });
        expect(sent).toHaveLength(1);
        expect(sent[0]).toMatchObject({
            url: '/v1/default-logbook',
            method: 'POST',
            archive: TEST_ARCHIVE,
        });
        expect(JSON.parse(sent[0].body ?? '')).toEqual({
            logbook_uuid: B,
            believed_default_uuid: A,
        });
    });

    it('an uncertain durability is reported', async () => {
        stub(202, { request_id: REQ, durability: 'uncertain' });
        expect(await requestDefaultChange(B, A)).toMatchObject({
            kind: 'accepted',
            durability: 'uncertain',
        });
    });

    it('a 202 without a request id cannot prove what was accepted: unknown', async () => {
        stub(202, { durability: 'durable' });
        expect((await requestDefaultChange(B, A)).kind).toBe('unknown');
    });

    it.each([
        [409, 'default_stale'],
        [409, 'tx_busy'],
        [404, 'default_target_unavailable'],
        [503, 'restart_unavailable'],
        [500, 'default_change_persist_failed'],
    ])('a %i %s is a refusal carrying the daemon’s code and words', async (status, code) => {
        stub(status, { code, message: `words for ${code}` });
        expect(await requestDefaultChange(B, A)).toEqual({
            kind: 'refused',
            code,
            message: `words for ${code}`,
        });
    });

    it.each([
        ['restart_failed'],
        ['restart_failed_withdrawal_unconfirmed'],
        ['default_change_pending_unclear'],
    ])('a 500 %s is a refusal: the daemon answered what happened', async (code) => {
        stub(500, { code, message: 'm' });
        expect(await requestDefaultChange(B, A)).toMatchObject({ kind: 'refused', code });
    });

    it('a timeout is unknown, sent once', async () => {
        stubFailure(new DOMException('timed out', 'TimeoutError'));
        const out = await requestDefaultChange(B, A);
        expect(out.kind).toBe('unknown');
        expect(sent).toHaveLength(1);
    });

    it('a dropped connection is unknown too (the restart drops it)', async () => {
        stubFailure(new TypeError('Failed to fetch'));
        expect((await requestDefaultChange(B, A)).kind).toBe('unknown');
    });

    it('an answer without a coded body is unknown, not a refusal', async () => {
        stub(502, '<html>bad gateway</html>', true);
        expect((await requestDefaultChange(B, A)).kind).toBe('unknown');
    });

    it('is not sent before the archive is proven', async () => {
        stub(202, { request_id: REQ, durability: 'durable' });
        setBoundArchive(null);
        const out = await requestDefaultChange(B, A);
        expect(sent).toHaveLength(0);
        expect(out).toMatchObject({ kind: 'refused', code: 'archive_unproven' });
    });
});
