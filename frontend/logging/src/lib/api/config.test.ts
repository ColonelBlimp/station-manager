import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchConfig, putConfig, type ConfigResponse } from './config';

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

// Minimal-but-valid ConfigResponse fixture. The wrapper only checks
// for "is a plain object" before casting, so every consumer-facing
// branch we care about is exercised with this shape. Field omissions
// are deliberate — the daemon's `omitempty` JSON tags mean a real
// payload routinely lacks half of these.
const MIN_CONFIG: ConfigResponse = {
    setup_complete: true,
    logging_station: { station_callsign: 'M0XYZ' },
    default_logbook: { id: 1 },
    default_rig: { id: 0 },
    station: { amp_enabled: false, amp_multiplier: 1, default_power: 100 },
    bridge: { enabled: false },
    mailer: { enabled: false },
};

describe('fetchConfig', () => {
    it('GETs /v1/config', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(MIN_CONFIG), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await fetchConfig();

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe('/v1/config');
        // safeFetch doesn't set method='GET' explicitly — fetch defaults to
        // GET when method is unset. Asserting `undefined` pins that the
        // wrapper deliberately doesn't override it (vs `putConfig` below
        // which DOES set 'PUT').
        expect(init?.method).toBeUndefined();
    });

    it('returns kind=ok with the parsed config on 200', async () => {
        mockFetchJSON(200, MIN_CONFIG);
        const out = await fetchConfig();
        expect(out).toEqual({ kind: 'ok', config: MIN_CONFIG });
    });

    it('returns kind=server malformed_response when 200 body is not an object', async () => {
        // I7 from the review — without this guard, a daemon regression
        // returning a non-object body crashes the caller at the first
        // `config.logging_station` dereference. The wrapper downgrades
        // it to a synthesised server error so the toast/error path runs
        // instead of the panel exploding.
        const response = new Response('not json', {
            status: 200,
            headers: { 'Content-Type': 'text/plain' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );

        const out = await fetchConfig();
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a non-JSON or empty body for /v1/config',
        });
    });

    it('returns kind=server malformed_response when 200 body is a JSON array', async () => {
        // Arrays parse fine as JSON but isPlainObject excludes them
        // because the daemon's `/v1/config` envelope is always `{...}`.
        mockFetchJSON(200, [1, 2, 3]);
        const out = await fetchConfig();
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a non-JSON or empty body for /v1/config',
        });
    });

    it('returns kind=validation for 400 with daemon code+message', async () => {
        mockFetchJSON(400, {
            code: 'invalid_field_value',
            message: 'my_cq_zone must be 1-40',
        });
        const out = await fetchConfig();
        expect(out).toEqual({
            kind: 'validation',
            code: 'invalid_field_value',
            message: 'my_cq_zone must be 1-40',
        });
    });

    it('returns kind=server for 5xx with daemon code+message', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'config persist failed' });
        const out = await fetchConfig();
        expect(out).toEqual({
            kind: 'server',
            code: 'db_error',
            message: 'config persist failed',
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

        const out = await fetchConfig();
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
        const out = await fetchConfig();
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
        const out = await fetchConfig(ctrl.signal);
        expect(out).toEqual({ kind: 'aborted', message: 'aborted' });
    });

    it('passes the AbortSignal through to fetch', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(MIN_CONFIG), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const ctrl = new AbortController();
        await fetchConfig(ctrl.signal);
        const [, init] = fetchSpy.mock.calls[0];
        expect(init?.signal).toBe(ctrl.signal);
    });
});

describe('putConfig', () => {
    it('PUTs /v1/config with the JSON-stringified payload and Content-Type', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(MIN_CONFIG), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const payload = {
            logging_station: { station_callsign: 'M0NEW', my_gridsquare: 'IO91' },
            station: { amp_enabled: true, amp_multiplier: 10, default_power: 50 },
        };
        await putConfig(payload);

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe('/v1/config');
        expect(init?.method).toBe('PUT');
        expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json');
        // Bit-for-bit body assertion: the wrapper does not transform
        // the payload (no key-stripping, no field renaming). Pinning
        // this keeps a future "helpful preprocessing" from silently
        // drifting the wire format.
        expect(init?.body).toBe(JSON.stringify(payload));
    });

    it('returns kind=ok with the parsed config on 200', async () => {
        mockFetchJSON(200, MIN_CONFIG);
        const out = await putConfig({});
        expect(out).toEqual({ kind: 'ok', config: MIN_CONFIG });
    });

    it('returns kind=validation for 400 with daemon code+message', async () => {
        // Most common PUT failure — daemon-side zone/callsign validation
        // rejects a malformed field (per session 46's validation work).
        mockFetchJSON(400, {
            code: 'invalid_field_value',
            message: 'station_callsign must be 3-32 characters and contain at least one digit',
        });
        const out = await putConfig({
            logging_station: { station_callsign: 'BAD' },
        });
        expect(out).toEqual({
            kind: 'validation',
            code: 'invalid_field_value',
            message: 'station_callsign must be 3-32 characters and contain at least one digit',
        });
    });

    it('returns kind=server for 5xx with daemon code+message', async () => {
        mockFetchJSON(500, { code: 'config_write_failed', message: 'could not write config.json' });
        const out = await putConfig({});
        expect(out).toEqual({
            kind: 'server',
            code: 'config_write_failed',
            message: 'could not write config.json',
        });
    });

    it('returns kind=server malformed_response when 200 body is not an object', async () => {
        // Same parseOutcome guard as fetchConfig — pinned here too
        // because a PUT round-trip is the more operator-visible path
        // (Save button click), so a regression here is louder.
        const response = new Response('null', {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );

        const out = await putConfig({});
        expect(out).toEqual({
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a non-JSON or empty body for /v1/config',
        });
    });

    it('returns kind=network when fetch throws', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const out = await putConfig({});
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
        const out = await putConfig({}, ctrl.signal);
        expect(out).toEqual({ kind: 'aborted', message: 'aborted' });
    });

    it('passes the AbortSignal through to fetch', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(MIN_CONFIG), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const ctrl = new AbortController();
        await putConfig({}, ctrl.signal);
        const [, init] = fetchSpy.mock.calls[0];
        expect(init?.signal).toBe(ctrl.signal);
    });
});
