// F-04a: the upload-enqueue write boundary. Enqueue is a multi-uuid batch the worker drains in the
// background, and the SPA has no per-entry proof API, so a timed-out enqueue cannot be reconciled —
// it must report outcome-unknown (never inferred failure). It must also use the write-class timeout,
// not the read default. safeFetch is mocked so the FetchOutcome and the timeout opts are both
// directly assertable; readJsonBody/isPlainObject/daemonErrorMessage stay real.

import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('./_helpers', async (importOriginal) => {
    const actual = await importOriginal<typeof import('./_helpers')>();
    return { ...actual, safeFetch: vi.fn() };
});

import { enqueueUploads } from './uploads';
import { safeFetch, WRITE_TIMEOUT_MS, type FetchOutcome } from './_helpers';

const mockedSafeFetch = vi.mocked(safeFetch);

afterEach(() => {
    mockedSafeFetch.mockReset();
});

const okResponse = (body: unknown): FetchOutcome => ({
    ok: true,
    response: new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
    }),
});

describe('enqueueUploads — F-04a', () => {
    it('uses the write-class timeout, not the read default', async () => {
        mockedSafeFetch.mockResolvedValue(okResponse({ enqueued: 1, skipped_uploaded: 0 }));
        await enqueueUploads('qrz', ['u-1']);
        expect(mockedSafeFetch).toHaveBeenCalledWith(
            expect.any(String),
            expect.objectContaining({ method: 'POST' }),
            { timeoutMs: WRITE_TIMEOUT_MS }
        );
    });

    it('reports outcome-unknown on a timed-out enqueue, never inferred failure', async () => {
        mockedSafeFetch.mockResolvedValue({
            ok: false,
            kind: 'network',
            message: 'request timed out after 30000 ms',
            timedOut: true,
        });
        const out = await enqueueUploads('qrz', ['u-1', 'u-2']);
        expect(out.kind).toBe('error');
        if (out.kind !== 'error') return;
        expect(out.timedOut).toBe(true);
        expect(out.message).toContain('the outcome is unknown');
        expect(out.message).toContain('Check its upload status');
    });

    it('leaves a non-timeout network failure unchanged', async () => {
        mockedSafeFetch.mockResolvedValue({
            ok: false,
            kind: 'network',
            message: 'connection refused',
        });
        const out = await enqueueUploads('qrz', ['u-1']);
        expect(out).toEqual({ kind: 'error', message: 'connection refused' });
    });

    it('returns the summary on success', async () => {
        mockedSafeFetch.mockResolvedValue(okResponse({ enqueued: 2, skipped_uploaded: 1 }));
        const out = await enqueueUploads('qrz', ['u-1', 'u-2', 'u-3']);
        expect(out).toEqual({
            kind: 'ok',
            result: {
                enqueued: 2,
                skipped_uploaded: 1,
                skipped_deleted: undefined,
                not_found: undefined,
                skipped_no_history: undefined,
            },
        });
    });
});
