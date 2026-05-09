import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchQso, patchQso } from './qso-update';

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

const UUID = '01910d3a-7000-7abc-8def-0123456789ab';
// Daemon's GET response is flat — types.Qso embeds QsoDetails /
// ContactedStation as anonymous structs, so encoding/json promotes
// their fields to the top level. Test fixtures must match the wire.
const DAEMON_QSO = {
    uuid: UUID,
    call: 'M0XYZ',
    name: 'Marc',
    mode: 'USB',
    band: '20m',
};

describe('fetchQso', () => {
    it('GETs /v1/qso/{uuid}', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(DAEMON_QSO), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await fetchQso(UUID);
        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url] = fetchSpy.mock.calls[0];
        expect(url).toBe(`/v1/qso/${UUID}`);
    });

    it('returns kind=ok with the parsed QSO on 200', async () => {
        mockFetchJSON(200, DAEMON_QSO);
        const out = await fetchQso(UUID);
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') {
            expect(out.qso.call).toBe('M0XYZ');
        }
    });

    it('returns kind=not_found on 404', async () => {
        mockFetchJSON(404, { code: 'not_found', message: 'QSO not found' });
        const out = await fetchQso(UUID);
        expect(out).toEqual({ kind: 'not_found', message: 'QSO not found' });
    });

    it('returns kind=validation on 400', async () => {
        mockFetchJSON(400, { code: 'invalid_uuid', message: 'invalid UUID' });
        const out = await fetchQso(UUID);
        expect(out).toEqual({
            kind: 'validation',
            code: 'invalid_uuid',
            message: 'invalid UUID',
        });
    });

    it('returns kind=server on 500', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'database operation failed' });
        const out = await fetchQso(UUID);
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
        const out = await fetchQso(UUID);
        expect(out).toEqual({ kind: 'network', message: 'Failed to fetch' });
    });
});

describe('patchQso', () => {
    it('PATCHes /v1/qso/{uuid} with JSON body', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(new Response(JSON.stringify(DAEMON_QSO), { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        const body = { call: 'M0EDIT' };
        await patchQso(UUID, body);

        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe(`/v1/qso/${UUID}`);
        expect(init?.method).toBe('PATCH');
        expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json');
        expect(init?.body).toBe(JSON.stringify(body));
    });

    it('returns kind=ok with the merged QSO on 200', async () => {
        mockFetchJSON(200, DAEMON_QSO);
        const out = await patchQso(UUID, {});
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') {
            expect(out.qso.call).toBe('M0XYZ');
        }
    });

    it('returns kind=not_found on 404', async () => {
        mockFetchJSON(404, { code: 'not_found', message: 'QSO not found' });
        const out = await patchQso(UUID, {});
        expect(out).toEqual({ kind: 'not_found', message: 'QSO not found' });
    });

    it('returns kind=duplicate on 409', async () => {
        mockFetchJSON(409, {
            code: 'duplicate_key',
            message: 'edit would collide with another QSO in this logbook',
        });
        const out = await patchQso(UUID, {});
        expect(out).toEqual({
            kind: 'duplicate',
            message: 'edit would collide with another QSO in this logbook',
        });
    });

    it('returns kind=validation on 400', async () => {
        mockFetchJSON(400, {
            code: 'invalid_field_value',
            message: 'band "999m" is not a recognised band',
        });
        const out = await patchQso(UUID, {});
        expect(out).toEqual({
            kind: 'validation',
            code: 'invalid_field_value',
            message: 'band "999m" is not a recognised band',
        });
    });

    it('returns kind=server on 500', async () => {
        mockFetchJSON(500, { code: 'update_failed', message: 'QSO update failed' });
        const out = await patchQso(UUID, {});
        expect(out).toEqual({
            kind: 'server',
            code: 'update_failed',
            message: 'QSO update failed',
        });
    });

    it('returns kind=network when fetch throws', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const out = await patchQso(UUID, {});
        expect(out).toEqual({ kind: 'network', message: 'Failed to fetch' });
    });
});
