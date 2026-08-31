// F-03c (ADR 0077): the single-record edit boundary. A GET/PATCH response must be a plain object
// (an array is rejected — typeof [] === 'object' would otherwise pass) whose uuid EQUALS the
// requested uuid, or it is an error and nothing is written to the store. safeFetch/readJsonBody
// are the real thing over a mocked fetch.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchQso, patchQso } from './qso-patch';

afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
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

const jsonRes = (status: number, body: unknown): Response =>
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

// safeFetch classifies a fired write timeout (name === 'TimeoutError') as network+timedOut; a plain
// rejection is a definite (non-timeout) transport failure. Dispatching on method lets one mock drive
// both the timed-out PATCH and the reconciliation re-read (GET /v1/qso/{uuid}).
const timeoutError = (): Promise<never> =>
    Promise.reject(Object.assign(new Error('request timed out'), { name: 'TimeoutError' }));

function mockByMethod(handlers: {
    PATCH?: () => Promise<Response>;
    GET?: () => Promise<Response>;
}): ReturnType<typeof vi.fn> {
    const fn = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        const method = init?.method ?? 'GET';
        if (method === 'PATCH' && handlers.PATCH) return handlers.PATCH();
        if (method === 'GET' && handlers.GET) return handlers.GET();
        return Promise.reject(new Error(`unexpected ${method} call`));
    });
    vi.stubGlobal('fetch', fn);
    return fn;
}

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

// F-04a: a PATCH that times out is AMBIGUOUS (it reached the daemon and the response was lost, so it
// MAY have committed). patchQso reconciles by re-reading the QSO and comparing the fields the
// operator attempted to change: a full match proves success; a mismatch or an unconfirmable re-read
// stays outcome-unknown (never a false success and never a definite failure).
describe('patchQso — commit-then-timeout reconciliation (F-04a)', () => {
    it('confirms success when the re-read proves the timed-out PATCH committed', async () => {
        mockByMethod({
            PATCH: timeoutError,
            GET: () =>
                Promise.resolve(jsonRes(200, { uuid: UUID, comment: 'hello', call: 'K1ABC' })),
        });
        const out = await patchQso(UUID, { comment: 'hello' });
        expect(out).toEqual({ kind: 'ok', qso: { uuid: UUID, comment: 'hello', call: 'K1ABC' } });
    });

    it('reports outcome-unknown when the re-read does not match the attempted change', async () => {
        mockByMethod({
            PATCH: timeoutError,
            GET: () => Promise.resolve(jsonRes(200, { uuid: UUID, comment: 'OLD VALUE' })),
        });
        const out = await patchQso(UUID, { comment: 'new value' });
        expect(out.kind).toBe('error');
        if (out.kind !== 'error') return;
        expect(out.timedOut).toBe(true);
        expect(out.message).toContain('the outcome is unknown');
        expect(out.message).toContain('Reload this QSO');
    });

    it('reports outcome-unknown when the re-read itself cannot confirm', async () => {
        mockByMethod({
            PATCH: timeoutError,
            GET: () => Promise.reject(new Error('network down')),
        });
        const out = await patchQso(UUID, { comment: 'x' });
        expect(out.kind).toBe('error');
        if (out.kind !== 'error') return;
        expect(out.timedOut).toBe(true);
        expect(out.message).toContain('the outcome is unknown');
    });

    it('leaves a non-timeout network failure unchanged — no reconciliation, not outcome-unknown', async () => {
        const getSpy = vi.fn(() => Promise.resolve(jsonRes(200, { uuid: UUID })));
        mockByMethod({
            PATCH: () => Promise.reject(new Error('connection refused')),
            GET: getSpy,
        });
        const out = await patchQso(UUID, { comment: 'x' });
        expect(out.kind).toBe('error');
        if (out.kind !== 'error') return;
        expect(out.timedOut).toBeUndefined();
        expect(out.message).toBe('Cannot reach the daemon.');
        // A definite (non-timeout) transport failure does not trigger a re-read.
        expect(getSpy).not.toHaveBeenCalled();
    });
});
