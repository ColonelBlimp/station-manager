import { afterEach, describe, expect, it, vi } from 'vitest';
import { restartDaemon } from './restart';

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
});
