// F-03c (ADR 0077): the single-record edit boundary. A GET/PATCH response must be a plain object
// (an array is rejected — typeof [] === 'object' would otherwise pass) whose uuid EQUALS the
// requested uuid, or it is an error and nothing is written to the store. safeFetch/readJsonBody
// are the real thing over a mocked fetch.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchQso, patchQso } from './qso-patch';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockFetchJSON(status: number, body: unknown): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

const UUID = '00000000-0000-7000-8000-000000000001';

describe('fetchQso — response is a plain object with the requested uuid (F-03c)', () => {
    it('rejects an array body (never writes it as a QSO)', async () => {
        mockFetchJSON(200, [{ uuid: UUID }]);
        const out = await fetchQso(UUID);
        expect(out.kind).toBe('error');
    });

    it('rejects a response whose uuid differs from the requested uuid', async () => {
        mockFetchJSON(200, { uuid: 'a-different-uuid', call: 'K1ABC' });
        const out = await fetchQso(UUID);
        expect(out.kind).toBe('error');
    });

    it('rejects a response with no uuid', async () => {
        mockFetchJSON(200, { call: 'K1ABC' });
        const out = await fetchQso(UUID);
        expect(out.kind).toBe('error');
    });

    it('accepts a response whose uuid equals the requested uuid', async () => {
        mockFetchJSON(200, { uuid: UUID, call: 'K1ABC' });
        const out = await fetchQso(UUID);
        expect(out).toEqual({ kind: 'ok', qso: { uuid: UUID, call: 'K1ABC' } });
    });
});

describe('patchQso — response is a plain object with the requested uuid (F-03c)', () => {
    it('rejects an array body', async () => {
        mockFetchJSON(200, [{ uuid: UUID }]);
        const out = await patchQso(UUID, { comment: 'x' });
        expect(out.kind).toBe('error');
    });

    it('rejects a response whose uuid differs from the requested uuid', async () => {
        mockFetchJSON(200, { uuid: 'wrong', call: 'K1ABC' });
        const out = await patchQso(UUID, { comment: 'x' });
        expect(out.kind).toBe('error');
    });

    it('accepts a response whose uuid equals the requested uuid', async () => {
        mockFetchJSON(200, { uuid: UUID, comment: 'x' });
        const out = await patchQso(UUID, { comment: 'x' });
        expect(out).toEqual({ kind: 'ok', qso: { uuid: UUID, comment: 'x' } });
    });
});
