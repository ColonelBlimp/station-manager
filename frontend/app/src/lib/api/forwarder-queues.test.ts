import { describe, it, expect, afterEach, vi } from 'vitest';
import { fetchForwarderQueues, clearForwarderQueue } from './forwarder-queues';

const urlOf = (input: RequestInfo | URL): string =>
    typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

function stubJson(status: number, body: unknown): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

afterEach(() => {
    vi.unstubAllGlobals();
});

describe('fetchForwarderQueues', () => {
    it('returns the per-forwarder counts on a 200 envelope', async () => {
        stubJson(200, {
            forwarders: [
                { name: 'qrz', clearable: 3, in_flight: 1 },
                { name: 'clublog', clearable: 0, in_flight: 0 },
            ],
        });
        const out = await fetchForwarderQueues();
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') {
            expect(out.forwarders).toHaveLength(2);
            expect(out.forwarders[0]).toEqual({ name: 'qrz', clearable: 3, in_flight: 1 });
        }
    });

    it('maps a transport failure to an error', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new Error('connection refused')))
        );
        expect((await fetchForwarderQueues()).kind).toBe('error');
    });

    it('maps a non-2xx status to an error', async () => {
        stubJson(500, { code: 'queue_counts_failed' });
        expect((await fetchForwarderQueues()).kind).toBe('error');
    });

    it('rejects a non-envelope body', async () => {
        stubJson(200, [{ name: 'qrz' }]); // bare array, not {forwarders:[]}
        expect((await fetchForwarderQueues()).kind).toBe('error');
    });
});

describe('clearForwarderQueue', () => {
    it('returns the discarded count on 200', async () => {
        stubJson(200, { discarded: 4 });
        const out = await clearForwarderQueue('qrz');
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') {
            expect(out.discarded).toBe(4);
        }
    });

    it('POSTs to the name-scoped path, URL-encoding the exact name', async () => {
        let url: string | undefined;
        let init: RequestInit | undefined;
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL, i?: RequestInit) => {
                url = urlOf(input);
                init = i;
                return Promise.resolve(
                    new Response(JSON.stringify({ discarded: 0 }), { status: 200 })
                );
            })
        );
        await clearForwarderQueue(' qrz '); // surrounding whitespace must round-trip
        expect(url).toBe('/v1/forwarder/%20qrz%20/queue/clear');
        expect(init?.method).toBe('POST');
    });

    it('surfaces the daemon error MESSAGE on a 404', async () => {
        stubJson(404, { code: 'unknown_forwarder', message: 'no such forwarder' });
        const out = await clearForwarderQueue('nope');
        expect(out.kind).toBe('error');
        if (out.kind === 'error') {
            expect(out.message).toBe('no such forwarder');
        }
    });

    it('swallows a transport failure into an error result', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new Error('connection refused')))
        );
        expect((await clearForwarderQueue('qrz')).kind).toBe('error');
    });
});
