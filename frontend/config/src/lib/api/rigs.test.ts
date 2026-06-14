import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchRigs, type RigsResponse } from './rigs';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockFetchJSON(status: number, body: unknown): void {
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

const MIN_RIGS: RigsResponse = {
    default_rig_id: 2,
    rigs: [{ id: 2, model: 'yaesu-ftdx10', port: '/dev/ttyUSB0' }],
    catalogue: [
        {
            id: 'yaesu-ftdx10',
            name: 'Yaesu FTdx10',
            manufacturer: 'Yaesu',
            model: 'FTdx10',
            family: 'ftdx',
            ft8_mode: 'DATA-U',
            rig_modes: ['LSB', 'USB', 'DATA-U'],
            serial: {
                baud_rate: 38400,
                data_bits: 8,
                stop_bits: 1,
                parity: 'none',
                line_delimiter: ';',
                read_timeout_ms: 1000,
            },
        },
    ],
};

describe('fetchRigs', () => {
    it('GETs /v1/rigs and returns kind=ok with the parsed body', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(MIN_RIGS), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const out = await fetchRigs();

        expect(fetchSpy.mock.calls[0][0]).toBe('/v1/rigs');
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') {
            expect(out.rigs.default_rig_id).toBe(2);
            expect(out.rigs.catalogue[0].ft8_mode).toBe('DATA-U');
        }
    });

    it('returns kind=server/malformed_response on a 200 missing required blocks', async () => {
        mockFetchJSON(200, { default_rig_id: 2 }); // no rigs / catalogue
        const out = await fetchRigs();
        expect(out.kind).toBe('server');
        if (out.kind === 'server') {
            expect(out.code).toBe('malformed_response');
        }
    });

    it('returns kind=server on a 200 with a non-object body', async () => {
        mockFetchJSON(200, 'not an object');
        const out = await fetchRigs();
        expect(out.kind).toBe('server');
    });

    it('returns kind=network when fetch throws', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const out = await fetchRigs();
        expect(out.kind).toBe('network');
    });

    it('returns kind=aborted when the caller cancels', async () => {
        const controller = new AbortController();
        controller.abort();
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new DOMException('aborted', 'AbortError')))
        );
        const out = await fetchRigs(controller.signal);
        expect(out.kind).toBe('aborted');
    });
});
