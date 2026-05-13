import { afterEach, describe, expect, it, vi } from 'vitest';
import { isPlainObject, isShape, readJsonBody, safeFetch } from './_helpers';

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
        vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(response)));

        const out = await safeFetch('/anywhere');
        expect(out).toEqual({ ok: true, response });
    });

    it('returns kind=network when fetch rejects with a generic TypeError', async () => {
        vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))));

        const out = await safeFetch('/anywhere');
        expect(out).toEqual({ ok: false, kind: 'network', message: 'Failed to fetch' });
    });

    it('returns kind=aborted when fetch rejects with an AbortError', async () => {
        // AbortController.abort() typically throws DOMException name=AbortError.
        // Forcing the name explicitly so the test does not depend on the
        // platform polyfill's exact class hierarchy.
        const abortErr = new Error('aborted by caller');
        abortErr.name = 'AbortError';
        vi.stubGlobal('fetch', vi.fn(() => Promise.reject(abortErr)));

        const out = await safeFetch('/anywhere');
        expect(out).toEqual({ ok: false, kind: 'aborted', message: 'aborted by caller' });
    });

    it('returns kind=aborted on TimeoutError (AbortSignal.timeout)', async () => {
        const timeoutErr = new Error('timed out');
        timeoutErr.name = 'TimeoutError';
        vi.stubGlobal('fetch', vi.fn(() => Promise.reject(timeoutErr)));

        const out = await safeFetch('/anywhere');
        expect(out).toEqual({ ok: false, kind: 'aborted', message: 'timed out' });
    });

    it('returns kind=aborted when the signal was aborted even if the error name is generic', async () => {
        // Belt-and-braces: some polyfills surface a generic TypeError on
        // post-abort fetches. The signal.aborted check is the fallback
        // classifier so the caller still sees `aborted`.
        const ctrl = new AbortController();
        ctrl.abort();
        vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('generic'))));

        const out = await safeFetch('/anywhere', { signal: ctrl.signal });
        expect(out).toEqual({ ok: false, kind: 'aborted', message: 'generic' });
    });

    it('passes init through to fetch', async () => {
        const fetchSpy = vi.fn(() => Promise.resolve(new Response('', { status: 200 })));
        vi.stubGlobal('fetch', fetchSpy);

        const init = { method: 'POST', body: 'payload' };
        await safeFetch('/anywhere', init);

        expect(fetchSpy).toHaveBeenCalledWith('/anywhere', init);
    });
});
