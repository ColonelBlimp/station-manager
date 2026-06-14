import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchContestDupe } from './contest-dupe';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockFetchJSON(status: number, body: unknown): void {
    const response = new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
    vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(response))
    );
}

describe('fetchContestDupe', () => {
    it('GETs /v1/contest-dupe with URL-encoded params including mode', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify({ duplicate: false }), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await fetchContestDupe({ logbook: 1, call: 'G3XYZ/P', band: '20m', mode: 'FT8' });

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        // "/" in the compound call proves the encoder is firing.
        expect(url).toBe('/v1/contest-dupe?logbook=1&call=G3XYZ%2FP&band=20m&mode=FT8');
        expect(init?.method).toBe('GET');
    });

    it('omits mode from the query when not supplied', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify({ duplicate: false }), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await fetchContestDupe({ logbook: 2, call: 'K1ABC', band: '40m' });

        const [url] = fetchSpy.mock.calls[0];
        expect(url).toBe('/v1/contest-dupe?logbook=2&call=K1ABC&band=40m');
    });

    it('returns kind=ok with duplicate=true on a 200 hit', async () => {
        mockFetchJSON(200, { duplicate: true });
        const out = await fetchContestDupe({ logbook: 1, call: 'K1ABC', band: '20m', mode: 'FT8' });
        expect(out).toEqual({ kind: 'ok', duplicate: true });
    });

    it('returns kind=ok with duplicate=false on a 200 miss', async () => {
        mockFetchJSON(200, { duplicate: false });
        const out = await fetchContestDupe({ logbook: 1, call: 'K1ABC', band: '20m', mode: 'FT8' });
        expect(out).toEqual({ kind: 'ok', duplicate: false });
    });

    it('flags a 200 without a boolean duplicate as malformed, not a miss', async () => {
        mockFetchJSON(200, { dupe: true }); // wrong key
        const out = await fetchContestDupe({ logbook: 1, call: 'K1ABC', band: '20m' });
        expect(out.kind).toBe('server');
        if (out.kind === 'server') expect(out.code).toBe('malformed_response');
    });

    it('maps a 400 to a validation outcome', async () => {
        mockFetchJSON(400, {
            code: 'invalid_field_value',
            message: 'band is not a recognised band',
        });
        const out = await fetchContestDupe({ logbook: 1, call: 'K1ABC', band: 'bogus' });
        expect(out.kind).toBe('validation');
        if (out.kind === 'validation') expect(out.code).toBe('invalid_field_value');
    });

    it('maps a 404 (no such logbook) to a validation outcome', async () => {
        mockFetchJSON(404, { code: 'logbook_not_found', message: 'logbook does not exist' });
        const out = await fetchContestDupe({ logbook: 99, call: 'K1ABC', band: '20m' });
        expect(out.kind).toBe('validation');
        if (out.kind === 'validation') expect(out.code).toBe('logbook_not_found');
    });

    it('maps a 500 to a server outcome', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'database operation failed' });
        const out = await fetchContestDupe({ logbook: 1, call: 'K1ABC', band: '20m' });
        expect(out.kind).toBe('server');
        if (out.kind === 'server') expect(out.code).toBe('db_error');
    });
});
