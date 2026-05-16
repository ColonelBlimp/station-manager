import { afterEach, describe, expect, it, vi } from 'vitest';
import { enrichCallsign, type EnrichmentResult } from './enrichment';

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

const FULL_RESULT: EnrichmentResult = {
    callsign: 'M0XYZ',
    country: {
        name: 'England',
        prefix: 'M',
        dxcc_prefix: 'G',
        cq_zone: '14',
        itu_zone: '27',
        continent: 'EU',
    },
    station: {
        call: 'M0XYZ',
        name: 'Marc',
        gridsquare: 'IO91',
        qth: 'London',
    },
    country_source: 'hamnut',
    station_source: 'qrzlookupservice',
};

describe('enrichCallsign', () => {
    it('GETs /v1/enrich/callsign with the URL-encoded callsign', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(FULL_RESULT), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        // Portable suffix "/P" exercises the encodeURIComponent path —
        // a bare M0XYZ would not reveal a missing encoder.
        await enrichCallsign('M0XYZ/P');

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe('/v1/enrich/callsign?call=M0XYZ%2FP');
        expect(init?.method).toBe('GET');
    });

    it('returns kind=ok with the parsed result on 200', async () => {
        mockFetchJSON(200, FULL_RESULT);
        const out = await enrichCallsign('M0XYZ');
        expect(out).toEqual({ kind: 'ok', result: FULL_RESULT });
    });

    it('returns kind=ok with source=none / no country / no station (always-200 contract)', async () => {
        // Per ADR 0017 #12 the daemon always returns 200 even when every
        // provider is down; the SPA must surface this as a normal ok arm
        // with country_source/station_source = "none" rather than a
        // special "all providers down" failure branch.
        const empty: EnrichmentResult = {
            callsign: 'M0XYZ',
            country_source: 'none',
            station_source: 'none',
        };
        mockFetchJSON(200, empty);
        const out = await enrichCallsign('M0XYZ');
        expect(out).toEqual({ kind: 'ok', result: empty });
    });

    it('returns kind=server with unparseable_response when 200 body is not an object', async () => {
        // Daemon contract says the 200 body is a JSON object. A string,
        // array, or unparseable body indicates a daemon/proxy fault, not
        // a client mistake, so it gets surfaced as `server` rather than
        // `validation`.
        const response = new Response('not json', {
            status: 200,
            headers: { 'Content-Type': 'text/plain' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );

        const out = await enrichCallsign('M0XYZ');
        expect(out).toEqual({
            kind: 'server',
            code: 'unparseable_response',
            message: 'enrichment response was not valid JSON',
        });
    });

    it('returns kind=validation for 400 with daemon code+message', async () => {
        mockFetchJSON(400, { code: 'invalid_callsign', message: 'callsign param is required' });
        const out = await enrichCallsign('');
        expect(out).toEqual({
            kind: 'validation',
            code: 'invalid_callsign',
            message: 'callsign param is required',
        });
    });

    it('returns kind=server for 5xx with daemon code+message', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'database operation failed' });
        const out = await enrichCallsign('M0XYZ');
        expect(out).toEqual({
            kind: 'server',
            code: 'db_error',
            message: 'database operation failed',
        });
    });

    it('falls back to synthesised envelope when the error body is unparseable', async () => {
        const response = new Response('upstream gateway error', {
            status: 502,
            headers: { 'Content-Type': 'text/plain' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );

        const out = await enrichCallsign('M0XYZ');
        expect(out).toEqual({
            kind: 'server',
            code: 'unknown_error',
            message: 'HTTP 502',
        });
    });

    it('returns kind=network when fetch throws', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const out = await enrichCallsign('M0XYZ');
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
        const out = await enrichCallsign('M0XYZ', ctrl.signal);
        expect(out).toEqual({ kind: 'aborted', message: 'aborted' });
    });

    it('passes the AbortSignal through to fetch', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(FULL_RESULT), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const ctrl = new AbortController();
        await enrichCallsign('M0XYZ', ctrl.signal);
        const [, init] = fetchSpy.mock.calls[0];
        expect(init?.signal).toBe(ctrl.signal);
    });
});
