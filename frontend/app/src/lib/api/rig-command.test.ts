import { afterEach, describe, expect, it, vi } from 'vitest';
import { sendRigCommand } from './rig-command';

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

// F-04 confirm-by-push (ADR 0078): a rig-command POST that hits a FIRED timeout is
// AMBIGUOUS — no response arrived, and that proves only that: the request may or
// may not have reached the daemon, and the rig may already have moved — so
// sendRigCommand must carry `timedOut` on its network arm for the seam to
// reconcile against the rig-state SSE (watched ops) or resolve to unknown
// (contract-only ops), never a definite failure. An HTTP status is a definite
// answer; a non-timeout transport failure is unmarked.
describe('sendRigCommand — timed-out write is ambiguous (F-04 confirm-by-push)', () => {
    it('marks a fired timeout as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        const r = await sendRigCommand('set_mode', 'USB');
        expect(r.kind).toBe('network');
        if (r.kind !== 'network') return;
        expect(r.timedOut).toBe(true);
    });

    it('does NOT mark a non-timeout transport failure as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const r = await sendRigCommand('set_mode', 'USB');
        expect(r.kind).toBe('network');
        if (r.kind !== 'network') return;
        expect(r.timedOut).toBeFalsy();
    });

    it('a 4xx is a definite validation rejection, not network', async () => {
        mockJSON(400, { code: 'rig_invalid_value', message: 'bad value' });
        const r = await sendRigCommand('set_mode', 'NOPE');
        expect(r.kind).toBe('validation');
    });

    it('a 5xx is a definite server rejection, not network', async () => {
        mockJSON(503, { code: 'rig_not_connected', message: 'rig not connected' });
        const r = await sendRigCommand('set_mode', 'USB');
        expect(r.kind).toBe('server');
    });
});
