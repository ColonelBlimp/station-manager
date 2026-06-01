import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchVersion, type VersionResponse } from './version';

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

const FULL_VERSION: VersionResponse = {
    daemon: '2.0.0-alpha.1-2-g3db10475',
    go: 'go1.24.0',
    schema: { version: 1, dirty: false },
};

describe('fetchVersion', () => {
    it('GETs /v1/version', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(FULL_VERSION), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await fetchVersion();

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe('/v1/version');
        // No method override — fetch defaults to GET.
        expect(init?.method).toBeUndefined();
    });

    it('returns kind=ok with the parsed payload on 200', async () => {
        mockFetchJSON(200, FULL_VERSION);
        const out = await fetchVersion();
        expect(out).toEqual({ kind: 'ok', version: FULL_VERSION });
    });

    it('returns kind=ok with schema absent (omitempty path)', async () => {
        // The daemon omits `schema` when the migration-level query
        // fails; the wrapper passes the object through untouched and
        // the panel renders "unavailable".
        const noSchema = { daemon: 'dev', go: 'go1.24.0' };
        mockFetchJSON(200, noSchema);
        const out = await fetchVersion();
        expect(out).toEqual({ kind: 'ok', version: noSchema });
    });

    it('returns kind=server malformed_response when 200 body is not an object', async () => {
        const response = new Response('not json', {
            status: 200,
            headers: { 'Content-Type': 'text/plain' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );

        const out = await fetchVersion();
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a non-JSON or empty body for /v1/version',
        });
    });

    it('returns kind=server malformed_response when 200 body is a JSON array', async () => {
        mockFetchJSON(200, [1, 2, 3]);
        const out = await fetchVersion();
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a non-JSON or empty body for /v1/version',
        });
    });

    it('returns kind=network when fetch throws', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const out = await fetchVersion();
        expect(out).toEqual({ kind: 'network', message: 'Failed to fetch' });
    });

    it('returns kind=aborted when AbortSignal cancels the request', async () => {
        const abortErr = new Error('aborted');
        abortErr.name = 'AbortError';
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(abortErr))
        );

        const ctrl = new AbortController();
        const out = await fetchVersion(ctrl.signal);
        expect(out).toEqual({ kind: 'aborted', message: 'aborted' });
    });

    it('passes the AbortSignal through to fetch', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(FULL_VERSION), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const ctrl = new AbortController();
        await fetchVersion(ctrl.signal);
        const [, init] = fetchSpy.mock.calls[0];
        expect(init?.signal).toBe(ctrl.signal);
    });
});
