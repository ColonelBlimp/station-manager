import { afterEach, describe, expect, it, vi } from 'vitest';
import { sendRigTune } from './rig-tune';

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

// F-04 confirm-by-push (ADR 0078): a rig-tune POST that hits a FIRED timeout is
// AMBIGUOUS — no response arrived, and that proves only that: the request may or
// may not have reached the daemon, which may already have keyed/dropped the
// carrier — so sendRigTune must carry `timedOut` on its `network` arm for the
// seam to reconcile against the tune-state SSE, rather than flattening it into a
// bare network error the caller renders as a definite failure. Only a fired
// timeout is marked: an HTTP rejection IS a definite answer (validation/server),
// and a generic non-timeout transport failure is left unmarked (not proven to
// have committed OR failed).
describe('sendRigTune — timed-out write is ambiguous (F-04 confirm-by-push)', () => {
    it('marks a fired timeout as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        const r = await sendRigTune(true);
        expect(r.kind).toBe('network');
        if (r.kind !== 'network') return;
        expect(r.timedOut).toBe(true);
    });

    it('does NOT mark a non-timeout transport failure as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const r = await sendRigTune(true);
        expect(r.kind).toBe('network');
        if (r.kind !== 'network') return;
        expect(r.timedOut).toBeFalsy();
    });

    it('a 4xx is a definite validation rejection (the daemon answered), not network', async () => {
        mockJSON(409, { code: 'rig_identity_unverified', message: 'rig identity unverified' });
        const r = await sendRigTune(true);
        expect(r.kind).toBe('validation');
    });

    it('a 5xx is a definite server rejection, not network', async () => {
        mockJSON(503, { code: 'rig_not_connected', message: 'rig not connected' });
        const r = await sendRigTune(true);
        expect(r.kind).toBe('server');
    });
});
