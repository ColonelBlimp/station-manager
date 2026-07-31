import { afterEach, describe, expect, it, vi } from 'vitest';
import { daemonSkewMs, _resetDaemonClockForTests } from './daemonClock.svelte';
import { DEFAULT_TIMEOUT_MS, isPlainObject, isShape, readJsonBody, safeFetch } from './_helpers';

afterEach(() => {
    vi.restoreAllMocks();
});

describe('isPlainObject', () => {
    it('returns true for plain objects', () => {
        expect(isPlainObject({})).toBe(true);
        expect(isPlainObject({ a: 1 })).toBe(true);
        expect(isPlainObject(Object.create(null))).toBe(true);
    });

    it('returns false for null', () => {
        expect(isPlainObject(null)).toBe(false);
    });

    it('returns false for arrays', () => {
        // Arrays would pass `typeof === 'object'` so the dedicated
        // !Array.isArray check is load-bearing here.
        expect(isPlainObject([])).toBe(false);
        expect(isPlainObject([1, 2, 3])).toBe(false);
    });

    it('returns false for primitives', () => {
        expect(isPlainObject('s')).toBe(false);
        expect(isPlainObject(42)).toBe(false);
        expect(isPlainObject(true)).toBe(false);
        expect(isPlainObject(undefined)).toBe(false);
    });
});

describe('isShape', () => {
    interface Sample {
        uuid: string;
        status?: string;
    }

    it('returns true when every required key is present', () => {
        expect(isShape<Sample>({ uuid: 'x', status: 'stored' }, ['uuid'])).toBe(true);
    });

    it('returns true with an empty required-key list (object check only)', () => {
        expect(isShape<Sample>({ anything: true }, [])).toBe(true);
    });

    it('returns false when a required key is missing', () => {
        expect(isShape<Sample>({ status: 'stored' }, ['uuid'])).toBe(false);
    });

    it('returns false for non-objects regardless of required keys', () => {
        expect(isShape<Sample>(null, [])).toBe(false);
        expect(isShape<Sample>(['uuid'], ['uuid'])).toBe(false);
    });

    it('is presence-only — does NOT type-check field values', () => {
        // Intentional: a present-but-wrong-type field still narrows.
        // Per-endpoint semantic checks (e.g. uuid non-empty string)
        // belong at the call site.
        expect(isShape<Sample>({ uuid: 42 }, ['uuid'])).toBe(true);
    });
});

describe('readJsonBody', () => {
    it('returns the parsed JSON for a valid body', async () => {
        const r = new Response('{"a":1}', {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
        });
        await expect(readJsonBody(r)).resolves.toEqual({ a: 1 });
    });

    it('returns null when the body is unparseable', async () => {
        const r = new Response('not json', {
            status: 200,
            headers: { 'Content-Type': 'text/plain' },
        });
        await expect(readJsonBody(r)).resolves.toBeNull();
    });
});

describe('safeFetch', () => {
    it('returns ok=true with the Response on a successful fetch', async () => {
        const response = new Response('hi', { status: 200 });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );

        const out = await safeFetch('/anywhere');
        expect(out).toEqual({ ok: true, response });
    });

    it('returns kind=network when fetch rejects with a generic TypeError', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );

        const out = await safeFetch('/anywhere');
        expect(out).toEqual({ ok: false, kind: 'network', message: 'Failed to fetch' });
    });

    it('returns kind=aborted when fetch rejects with an AbortError', async () => {
        // AbortController.abort() typically throws DOMException name=AbortError.
        // Forcing the name explicitly so the test does not depend on the
        // platform polyfill's exact class hierarchy.
        const abortErr = new Error('aborted by caller');
        abortErr.name = 'AbortError';
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(abortErr))
        );

        const out = await safeFetch('/anywhere');
        expect(out).toEqual({ ok: false, kind: 'aborted', message: 'aborted by caller' });
    });

    it('returns kind=network on a fired timeout (TimeoutError), not aborted', async () => {
        // A timeout is a FAILURE, not an operator cancel — it must surface as
        // retriable 'network' so a caller that silently drops 'aborted' can't
        // swallow a hung request (the blank-boot / stuck-latch bug).
        const timeoutErr = new Error('timed out');
        timeoutErr.name = 'TimeoutError';
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(timeoutErr))
        );

        const out = await safeFetch('/anywhere');
        expect(out).toEqual({
            ok: false,
            kind: 'network',
            message: `request timed out after ${DEFAULT_TIMEOUT_MS} ms`,
            // The ambiguity marker: a timed-out WRITE may have committed
            // daemon-side, so callers report "unknown", not "failed".
            timedOut: true,
        });
    });

    it('returns kind=aborted when the signal was aborted even if the error name is generic', async () => {
        // Belt-and-braces: some polyfills surface a generic TypeError on
        // post-abort fetches. The signal.aborted check is the fallback
        // classifier so the caller still sees `aborted`.
        const ctrl = new AbortController();
        ctrl.abort();
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('generic')))
        );

        const out = await safeFetch('/anywhere', { signal: ctrl.signal });
        expect(out).toEqual({ ok: false, kind: 'aborted', message: 'generic' });
    });

    it('passes init through to fetch, with a timeout signal injected', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response('', { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await safeFetch('/anywhere', { method: 'POST', body: 'payload' });

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe('/anywhere');
        expect(init).toMatchObject({ method: 'POST', body: 'payload' });
        // The default timeout is applied by injecting an AbortSignal.
        expect(init?.signal).toBeInstanceOf(AbortSignal);
    });

    it('injects a timeout AbortSignal when the caller passes none', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response('', { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await safeFetch('/anywhere');

        const [, init] = fetchSpy.mock.calls[0];
        expect(init?.signal).toBeInstanceOf(AbortSignal);
    });

    it('composes the caller signal with the timeout so operator-cancel still fires', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response('', { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const ctrl = new AbortController();
        await safeFetch('/anywhere', { signal: ctrl.signal });

        const [, init] = fetchSpy.mock.calls[0];
        expect(init?.signal).toBeInstanceOf(AbortSignal);
        // AbortSignal.any returns a NEW combined signal (not the caller's own),
        // but the caller's abort still propagates through it.
        expect(init?.signal).not.toBe(ctrl.signal);
        ctrl.abort();
        expect(init?.signal?.aborted).toBe(true);
    });

    it('opts out of the timeout when timeoutMs <= 0 (no signal injected)', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response('', { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await safeFetch('/anywhere', undefined, { timeoutMs: 0 });

        // No caller signal + timeout opted out → init passed through untouched.
        const [, init] = fetchSpy.mock.calls[0];
        expect(init).toBeUndefined();
    });
});

/*
    DAEMON CLOCK CALIBRATION — the one place it happens.

    FT8 decode staleness compares daemon-produced slot times against "now", so the
    SPA needs the DAEMON's clock, not the browser's (codex 9d7a3f46 P1). It is
    sampled from the HTTP `Date` header here, in safeFetch, because this is the
    single chokepoint every daemon request in this directory passes through — and
    because `Date` is stamped at send time, so unlike an SSE frame it cannot arrive
    replayed from a cache (codex 0d85428e P2).

    Without these rules the mechanism is only ever exercised by tests that call
    noteDaemonDate by hand, and nothing pins that a real response calibrates it.
*/
describe('safeFetch daemon-clock calibration', () => {
    beforeEach(() => _resetDaemonClockForTests());
    afterEach(() => _resetDaemonClockForTests());

    it('records the skew from a response Date header', async () => {
        const daemonNow = new Date(Date.now() - 5 * 60_000);
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.resolve(
                    new Response('', { status: 200, headers: { date: daemonNow.toUTCString() } })
                )
            )
        );

        await safeFetch('/v1/anything');

        // Browser-now minus daemon-now ~= 5 minutes. Second-granularity header, so
        // allow a couple of seconds either side rather than asserting an exact ms.
        expect(daemonSkewMs()).toBeGreaterThan(5 * 60_000 - 2_000);
        expect(daemonSkewMs()).toBeLessThan(5 * 60_000 + 2_000);
    });

    it('keeps the last good skew when the header is missing or unparseable', async () => {
        const daemonNow = new Date(Date.now() - 5 * 60_000);
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.resolve(
                    new Response('', { status: 200, headers: { date: daemonNow.toUTCString() } })
                )
            )
        );
        await safeFetch('/v1/anything');
        const calibrated = daemonSkewMs();

        // A response with no Date, then one with a broken Date. Neither says
        // anything about the clock, so guessing would be worse than the last answer.
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(new Response('', { status: 200 })))
        );
        await safeFetch('/v1/anything');
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.resolve(new Response('', { status: 200, headers: { date: 'nonsense' } }))
            )
        );
        await safeFetch('/v1/anything');

        expect(daemonSkewMs()).toBe(calibrated);
    });
});
