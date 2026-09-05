import { afterEach, describe, expect, it, vi } from 'vitest';
import { armFt8Tx } from './ft8tx';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockJSON(status: number, body: unknown) {
    vi.stubGlobal(
        'fetch',
        vi.fn((_url: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

// F-04 confirm-by-push (ADR 0078): an FT8 arm POST that hits a FIRED timeout is
// AMBIGUOUS — no response arrived, and that proves only that: the request may or
// may not have reached the daemon, which may already have armed (or disarmed) —
// so armFt8Tx must carry `timedOut` on its network arm for the seam to reconcile
// against the ft8-tx SSE, never flattening it into a definite failure. An HTTP
// status is a definite answer (the daemon refused); a non-timeout transport
// failure is left unmarked (proven neither committed nor failed).
describe('armFt8Tx — timed-out write is ambiguous (F-04 confirm-by-push)', () => {
    it('marks a fired timeout as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        const r = await armFt8Tx(true);
        expect(r.kind).toBe('network');
        if (r.kind !== 'network') return;
        expect(r.timedOut).toBe(true);
    });

    it('does NOT mark a non-timeout transport failure as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const r = await armFt8Tx(true);
        expect(r.kind).toBe('network');
        if (r.kind !== 'network') return;
        expect(r.timedOut).toBeFalsy();
    });

    it('a 4xx is a definite validation rejection, not network', async () => {
        mockJSON(409, { code: 'ft8_rung_not_skippable', message: 'not skippable' });
        const r = await armFt8Tx(true);
        expect(r.kind).toBe('validation');
    });

    it('a 5xx is a definite server rejection (the daemon refused the arm), not network', async () => {
        mockJSON(503, { code: 'rig_not_ready', message: 'rig not ready' });
        const r = await armFt8Tx(true);
        expect(r.kind).toBe('server');
    });
});
