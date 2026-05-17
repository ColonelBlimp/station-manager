import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchLogbookCount } from './logbook';

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

describe('fetchLogbookCount', () => {
    it('GETs /v1/logbook/{id}/count', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(
                    new Response(JSON.stringify({ logbook_id: 42, count: 0 }), { status: 200 })
                )
        );
        vi.stubGlobal('fetch', fetchSpy);

        await fetchLogbookCount(42);

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe('/v1/logbook/42/count');
        expect(init?.method).toBe('GET');
    });

    it('returns kind=ok with the parsed count on 200', async () => {
        mockFetchJSON(200, { logbook_id: 1, count: 4233 });
        const out = await fetchLogbookCount(1);
        expect(out).toEqual({ kind: 'ok', count: 4233 });
    });

    it('returns kind=ok with count=0 for an empty logbook', async () => {
        mockFetchJSON(200, { logbook_id: 1, count: 0 });
        const out = await fetchLogbookCount(1);
        expect(out).toEqual({ kind: 'ok', count: 0 });
    });

    it('downgrades a 200 with a non-numeric count to malformed_response', async () => {
        mockFetchJSON(200, { logbook_id: 1, count: 'lots' });
        const out = await fetchLogbookCount(1);
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned logbook count without a numeric count field',
        });
    });

    it('returns kind=not_found on 404', async () => {
        mockFetchJSON(404, { code: 'logbook_not_found', message: 'logbook does not exist' });
        const out = await fetchLogbookCount(99999);
        expect(out).toEqual({ kind: 'not_found', message: 'logbook does not exist' });
    });

    it('returns kind=validation on 400', async () => {
        mockFetchJSON(400, { code: 'invalid_id', message: 'bad id' });
        const out = await fetchLogbookCount(0);
        expect(out).toEqual({ kind: 'validation', code: 'invalid_id', message: 'bad id' });
    });

    it('returns kind=server on 500', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'database operation failed' });
        const out = await fetchLogbookCount(1);
        expect(out).toEqual({
            kind: 'server',
            code: 'db_error',
            message: 'database operation failed',
        });
    });

    it('returns kind=network when fetch throws', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new Error('boom')))
        );
        const out = await fetchLogbookCount(1);
        expect(out.kind).toBe('network');
    });
});
