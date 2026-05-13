import { afterEach, describe, expect, it, vi } from 'vitest';
import { submitQso } from './qso';

const ADIF = '<call:5>K1ABC<eor>';

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

const STORED_UUID = '01910d3a-7000-7abc-8def-0123456789ab';
const DUPLICATE_UUID = '01910d3a-7001-7def-9123-0123456789cd';

describe('submitQso', () => {
    it('posts to /v1/qso with the logbook query and application/x-adif body', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(
                    new Response(JSON.stringify({ status: 'stored', uuid: STORED_UUID, id: 42 }), {
                        status: 201,
                    })
                )
        );
        vi.stubGlobal('fetch', fetchSpy);

        await submitQso(ADIF, 1);

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const call = fetchSpy.mock.calls[0];
        const url = call[0];
        const init = call[1];
        expect(url).toBe('/v1/qso?logbook=1');
        expect(init?.method).toBe('POST');
        expect((init?.headers as Record<string, string>)['Content-Type']).toBe(
            'application/x-adif'
        );
        expect(init?.body).toBe(ADIF);
    });

    it('returns kind=stored on 201', async () => {
        mockFetchJSON(201, { status: 'stored', uuid: STORED_UUID, id: 42 });
        const out = await submitQso(ADIF, 1);
        expect(out).toEqual({ kind: 'stored', uuid: STORED_UUID });
    });

    it('returns kind=duplicate on 200 with status=duplicate', async () => {
        mockFetchJSON(200, { status: 'duplicate', uuid: DUPLICATE_UUID, id: 17 });
        const out = await submitQso(ADIF, 1);
        expect(out).toEqual({ kind: 'duplicate', uuid: DUPLICATE_UUID });
    });

    it('returns kind=validation for 4xx with daemon code+message', async () => {
        mockFetchJSON(400, { code: 'invalid_adif', message: 'no QSO records found in ADIF body' });
        const out = await submitQso(ADIF, 1);
        expect(out).toEqual({
            kind: 'validation',
            code: 'invalid_adif',
            message: 'no QSO records found in ADIF body',
        });
    });

    it('returns kind=validation for 409 with duplicate_key code', async () => {
        // 409 only fires from PATCH today, but a future POST contract change
        // (e.g. force=0 explicit-conflict mode) could surface it; the SPA
        // should map any 4xx via the validation arm without special-casing.
        mockFetchJSON(409, { code: 'duplicate_key', message: 'QSO already exists' });
        const out = await submitQso(ADIF, 1);
        expect(out).toEqual({
            kind: 'validation',
            code: 'duplicate_key',
            message: 'QSO already exists',
        });
    });

    it('returns kind=server for 5xx', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'database operation failed' });
        const out = await submitQso(ADIF, 1);
        expect(out).toEqual({
            kind: 'server',
            code: 'db_error',
            message: 'database operation failed',
        });
    });

    it('returns kind=network when fetch throws', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const out = await submitQso(ADIF, 1);
        expect(out).toEqual({ kind: 'network', message: 'Failed to fetch' });
    });

    it('returns kind=aborted when AbortSignal cancels the request', async () => {
        const abortErr = new Error('aborted');
        abortErr.name = 'AbortError';
        vi.stubGlobal('fetch', vi.fn(() => Promise.reject(abortErr)));

        const ctrl = new AbortController();
        const out = await submitQso(ADIF, 1, ctrl.signal);
        expect(out).toEqual({ kind: 'aborted', message: 'aborted' });
    });

    it('passes the AbortSignal through to fetch', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(
                    new Response(JSON.stringify({ status: 'stored', uuid: STORED_UUID }), {
                        status: 201,
                    })
                )
        );
        vi.stubGlobal('fetch', fetchSpy);

        const ctrl = new AbortController();
        await submitQso(ADIF, 1, ctrl.signal);
        const [, init] = fetchSpy.mock.calls[0];
        expect(init?.signal).toBe(ctrl.signal);
    });

    it('falls back to a synthesised error envelope when the body is unparseable', async () => {
        const response = new Response('not json', {
            status: 502,
            headers: { 'Content-Type': 'text/plain' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );
        const out = await submitQso(ADIF, 1);
        expect(out).toEqual({
            kind: 'server',
            code: 'unknown_error',
            message: 'HTTP 502',
        });
    });
});
