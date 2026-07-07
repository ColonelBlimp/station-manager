import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchContactHistory, type ContactHistory } from './contact-history';

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

function makeRow(overrides: Partial<ContactHistory> = {}): ContactHistory {
    return {
        id: 1,
        uuid: '00000000-0000-7000-8000-000000000001',
        band: '20m',
        freq: '14.250000',
        mode: 'SSB',
        qso_date: '20260508',
        time_on: '1430',
        name: 'Test',
        country: 'England',
        call: 'M0XYZ',
        rst_sent: '59',
        rst_rcvd: '59',
        notes: '',
        ...overrides,
    };
}

describe('fetchContactHistory', () => {
    it('GETs /v1/contact-history with the URL-encoded callsign', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify({ items: [] }), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        // "M0XYZ/P" exercises encodeURIComponent — the "/" is the proof
        // the encoder is firing; a bare callsign would let a missing
        // encoder pass silently.
        await fetchContactHistory('M0XYZ/P');

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe('/v1/contact-history?call=M0XYZ%2FP');
        expect(init?.method).toBe('GET');
    });

    it('returns kind=ok with the parsed items on 200', async () => {
        const rows = [makeRow(), makeRow({ id: 2, uuid: 'b', band: '40m' })];
        mockFetchJSON(200, { items: rows });
        const out = await fetchContactHistory('M0XYZ');
        expect(out).toEqual({ kind: 'ok', items: rows });
    });

    it('returns kind=ok with items=[] for the empty "never worked them" case', async () => {
        // 200 + items=[] is the daemon's contract for "no prior contacts" —
        // it is NOT a 404. The SPA renders this as the WorkedPanel
        // empty-state hint, not as an error toast.
        mockFetchJSON(200, { items: [] });
        const out = await fetchContactHistory('M0XYZ');
        expect(out).toEqual({ kind: 'ok', items: [] });
    });

    it('returns kind=server malformed_response when a 200 body is not a JSON object (M4)', async () => {
        // A 200 whose body is not an object is a daemon regression, NOT an
        // empty history. Surfacing it as a controlled server outcome (rather
        // than the prior false-empty `items:[]`) keeps a daemon/proxy fault
        // distinguishable from a genuine "never worked them" (review
        // 2026-06-04 M4). Logging is still never blocked — runLookup ignores
        // any non-ok contact-history outcome, so the panel stays empty.
        const response = new Response('not json', {
            status: 200,
            headers: { 'Content-Type': 'text/plain' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );

        const out = await fetchContactHistory('M0XYZ');
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a 200 without an items array for /v1/contact-history',
        });
    });

    it('returns kind=server malformed_response when 200 body has no items field (M4)', async () => {
        mockFetchJSON(200, { other: 'shape' });
        const out = await fetchContactHistory('M0XYZ');
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a 200 without an items array for /v1/contact-history',
        });
    });

    it('returns kind=server malformed_response when 200 body has items but not an array (M4)', async () => {
        mockFetchJSON(200, { items: 'oops' });
        const out = await fetchContactHistory('M0XYZ');
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a 200 without an items array for /v1/contact-history',
        });
    });

    it('returns kind=validation for 400 missing_required_param', async () => {
        mockFetchJSON(400, {
            code: 'missing_required_param',
            message: 'call query parameter is required',
        });
        const out = await fetchContactHistory('');
        expect(out).toEqual({
            kind: 'validation',
            code: 'missing_required_param',
            message: 'call query parameter is required',
        });
    });

    it('returns kind=validation for 400 invalid_field_value', async () => {
        mockFetchJSON(400, {
            code: 'invalid_field_value',
            message: 'call must be 3-32 characters and contain at least one digit',
        });
        const out = await fetchContactHistory('AB');
        expect(out).toEqual({
            kind: 'validation',
            code: 'invalid_field_value',
            message: 'call must be 3-32 characters and contain at least one digit',
        });
    });

    it('returns kind=validation for 404 logbook_not_found', async () => {
        // The SPA wrapper currently never sends ?logbook=, but the
        // daemon can still return this status for a direct caller (curl,
        // future logbook-filter panel). 404 < 500 routes through the
        // validation arm — it's an operator/input fault, not a daemon
        // fault.
        mockFetchJSON(404, {
            code: 'logbook_not_found',
            message: 'logbook does not exist',
        });
        const out = await fetchContactHistory('M0XYZ');
        expect(out).toEqual({
            kind: 'validation',
            code: 'logbook_not_found',
            message: 'logbook does not exist',
        });
    });

    it('returns kind=server for 5xx with daemon code+message', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'database operation failed' });
        const out = await fetchContactHistory('M0XYZ');
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

        const out = await fetchContactHistory('M0XYZ');
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
        const out = await fetchContactHistory('M0XYZ');
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
        const out = await fetchContactHistory('M0XYZ', ctrl.signal);
        expect(out).toEqual({ kind: 'aborted', message: 'aborted' });
    });

    it('passes the AbortSignal through to fetch', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify({ items: [] }), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const ctrl = new AbortController();
        await fetchContactHistory('M0XYZ', ctrl.signal);
        const [, init] = fetchSpy.mock.calls[0];
        // safeFetch composes the caller signal with a timeout, so fetch receives
        // a new combined signal — the operator's cancel still propagates through it.
        expect(init?.signal).toBeInstanceOf(AbortSignal);
        expect(init?.signal).not.toBe(ctrl.signal);
        ctrl.abort();
        expect(init?.signal?.aborted).toBe(true);
    });
});
