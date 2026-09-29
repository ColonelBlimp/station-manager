// Settings → Logbooks, first slice (W-0021 dossier, operator ruling 2026-09-29):
// the SPA's create / rename / delete over the daemon's existing POST, PATCH and
// DELETE /v1/logbook. Each outcome keeps the daemon's refusal code so the page
// can name it, and marks a write whose response never came (timedOut) so the
// page reports an unknown outcome instead of a failure. safeFetch/readJsonBody
// are the real thing over a mocked fetch.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { createLogbook, deleteLogbook, renameLogbook } from './logbooks';

afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});

function mockFetch(status: number, body?: unknown): ReturnType<typeof vi.fn> {
    const fn = vi.fn(() =>
        Promise.resolve(
            body === undefined
                ? new Response(null, { status })
                : new Response(JSON.stringify(body), {
                      status,
                      headers: { 'Content-Type': 'application/json' },
                  })
        )
    );
    vi.stubGlobal('fetch', fn);
    return fn;
}

function mockTimeout(): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' })))
    );
}

const sent = (fn: ReturnType<typeof vi.fn>): { url: string; init: RequestInit } => {
    const [url, init] = fn.mock.calls[0] as [string, RequestInit];
    return { url, init };
};

describe('createLogbook', () => {
    it('POSTs the name and callsign and returns the new id', async () => {
        const fn = mockFetch(201, { id: 7 });
        const out = await createLogbook({ name: 'Portable', callsign: '7Q5MLV/P' });
        expect(out).toEqual({ kind: 'ok', id: 7 });
        const { url, init } = sent(fn);
        expect(url).toBe('/v1/logbook');
        expect(init.method).toBe('POST');
        expect(JSON.parse(init.body as string)).toEqual({ name: 'Portable', callsign: '7Q5MLV/P' });
    });

    it('keeps the daemon’s refusal code and message', async () => {
        mockFetch(409, {
            code: 'duplicate_name',
            message: 'a logbook with that name already exists',
        });
        const out = await createLogbook({ name: 'Home', callsign: '7Q5MLV' });
        expect(out).toMatchObject({ kind: 'error', code: 'duplicate_name' });
        if (out.kind === 'error') expect(out.message).toContain('already exists');
    });

    it('marks a timed-out create, which may have landed', async () => {
        mockTimeout();
        const out = await createLogbook({ name: 'Portable', callsign: '7Q5MLV' });
        expect(out).toMatchObject({ kind: 'error', timedOut: true });
    });

    it('refuses a 201 without a numeric id', async () => {
        mockFetch(201, { ok: true });
        const out = await createLogbook({ name: 'Portable', callsign: '7Q5MLV' });
        expect(out).toMatchObject({ kind: 'error' });
        if (out.kind === 'error') expect(out.timedOut).not.toBe(true);
    });
});

describe('renameLogbook', () => {
    it('PATCHes only the name', async () => {
        const fn = mockFetch(200, { id: 3, name: 'Contest', callsign: '7Q5MLV' });
        const out = await renameLogbook(3, 'Contest');
        expect(out).toEqual({ kind: 'ok' });
        const { url, init } = sent(fn);
        expect(url).toBe('/v1/logbook/3');
        expect(init.method).toBe('PATCH');
        expect(JSON.parse(init.body as string)).toEqual({ name: 'Contest' });
    });

    it('keeps a refusal code and marks a timeout', async () => {
        mockFetch(404, { code: 'not_found', message: 'logbook not found' });
        expect(await renameLogbook(3, 'Contest')).toMatchObject({
            kind: 'error',
            code: 'not_found',
        });
        mockTimeout();
        expect(await renameLogbook(3, 'Contest')).toMatchObject({ kind: 'error', timedOut: true });
    });
});

describe('deleteLogbook', () => {
    it('DELETEs the logbook; 204 is success', async () => {
        const fn = mockFetch(204);
        expect(await deleteLogbook(4)).toEqual({ kind: 'ok' });
        const { url, init } = sent(fn);
        expect(url).toBe('/v1/logbook/4');
        expect(init.method).toBe('DELETE');
    });

    it('keeps the has_qsos and default_logbook refusals, and marks a timeout', async () => {
        mockFetch(409, { code: 'has_qsos', message: 'logbook holds QSOs' });
        expect(await deleteLogbook(4)).toMatchObject({ kind: 'error', code: 'has_qsos' });
        mockFetch(409, { code: 'default_logbook', message: 'the default logbook' });
        expect(await deleteLogbook(1)).toMatchObject({ kind: 'error', code: 'default_logbook' });
        mockTimeout();
        expect(await deleteLogbook(4)).toMatchObject({ kind: 'error', timedOut: true });
    });
});
