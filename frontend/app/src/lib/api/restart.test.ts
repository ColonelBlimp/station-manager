import { afterEach, describe, expect, it, vi } from 'vitest';
import { restartDaemon } from './restart';
import { WRITE_TIMEOUT_MS } from './_helpers';

afterEach(() => vi.restoreAllMocks());

function mockFetch(status: number, body: unknown): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(body === null ? null : JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

// safeFetch classifies a fired request timeout (name === 'TimeoutError', raised by
// AbortSignal.timeout) as network + timedOut; a plain rejection is a definite,
// non-timeout transport failure that carries no timedOut marker.
function mockFetchReject(err: Error): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.reject(err))
    );
}

const timeoutError = (): Error =>
    Object.assign(new Error('request timed out'), { name: 'TimeoutError' });

describe('restartDaemon', () => {
    it('202 → accepted', async () => {
        mockFetch(202, null);
        expect(await restartDaemon()).toEqual({ kind: 'accepted' });
    });

    it('409 tx_active → tx_active', async () => {
        mockFetch(409, { code: 'tx_active', message: 'stop transmitting' });
        expect(await restartDaemon()).toEqual({ kind: 'tx_active' });
    });

    it('503 restart_unavailable → unavailable', async () => {
        mockFetch(503, { code: 'restart_unavailable' });
        expect(await restartDaemon()).toEqual({ kind: 'unavailable' });
    });

    it('503 server_busy → error (retryable), NOT unavailable (codex 088bdb84 P3)', async () => {
        mockFetch(503, { code: 'server_busy', message: 'busy' });
        const out = await restartDaemon();
        expect(out.kind).toBe('error');
    });

    it('500 → error', async () => {
        mockFetch(500, { message: 'boom' });
        expect((await restartDaemon()).kind).toBe('error');
    });

    // F-04b (ADR 0078): a restart POST that TIMED OUT is ambiguous — the daemon
    // replies 202 then exits, so the response can be lost while it is already
    // respawning. Mark that case timedOut so the caller confirms via the
    // new-instance signal instead of declaring "Restart failed".
    it('a fired timeout → error carrying timedOut: true', async () => {
        mockFetchReject(timeoutError());
        const out = await restartDaemon();
        expect(out.kind).toBe('error');
        if (out.kind !== 'error') return;
        expect(out.timedOut).toBe(true);
    });

    // A definite (non-timeout) transport failure must NOT be marked ambiguous.
    it('a non-timeout network failure → error with no timedOut marker', async () => {
        mockFetchReject(new TypeError('connection refused'));
        const out = await restartDaemon();
        expect(out.kind).toBe('error');
        if (out.kind !== 'error') return;
        expect(out.timedOut).toBeUndefined();
    });

    // A restart is a state-mutating write, so it must outlast the read default —
    // give the daemon the write-class window before giving up (F-04b).
    it('POSTs with the write-class timeout, not the read default', async () => {
        const spy = vi.spyOn(AbortSignal, 'timeout');
        mockFetch(202, null);
        await restartDaemon();
        expect(spy).toHaveBeenCalledWith(WRITE_TIMEOUT_MS);
    });
});
