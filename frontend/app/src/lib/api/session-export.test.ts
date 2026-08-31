import { describe, it, expect, afterEach, vi } from 'vitest';
import { exportSessionAdif } from './session-export';
import { WRITE_TIMEOUT_MS } from './_helpers';

afterEach(() => {
    vi.restoreAllMocks();
});

function stubFetch(response: Response): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(response))
    );
}

// safeFetch classifies a fired request timeout (name === 'TimeoutError') as
// network + timedOut; a plain rejection is a definite non-timeout failure.
const timeoutError = (): Error =>
    Object.assign(new Error('request timed out'), { name: 'TimeoutError' });

describe('exportSessionAdif', () => {
    it('returns the ADIF body + the daemon filename on 200', async () => {
        stubFetch(
            new Response('<EOH>\n<CALL:5>M0XYZ\n<EOR>', {
                status: 200,
                headers: {
                    'Content-Type': 'application/x-adif',
                    'Content-Disposition': 'attachment; filename="session-20260708-043012.adi"',
                },
            })
        );
        const out = await exportSessionAdif(['a', 'b']);
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') {
            expect(out.filename).toBe('session-20260708-043012.adi');
            expect(out.body).toContain('<EOR>');
        }
    });

    it('falls back to a generic filename when the header is absent', async () => {
        stubFetch(new Response('<EOH>', { status: 200 }));
        const out = await exportSessionAdif(['a']);
        expect(out.kind === 'ok' && out.filename).toBe('session.adi');
    });

    it('maps a 400 no_qsos to its own kind', async () => {
        stubFetch(
            new Response(JSON.stringify({ code: 'no_qsos', message: 'none found' }), {
                status: 400,
                headers: { 'Content-Type': 'application/json' },
            })
        );
        const out = await exportSessionAdif(['gone']);
        expect(out.kind).toBe('no_qsos');
    });

    it('is fail-soft on a network error', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('down')))
        );
        const out = await exportSessionAdif(['a']);
        expect(out.kind).toBe('network');
    });

    // F-04b (ADR 0078): export archives a best-effort backup server-side, so a
    // timed-out export is ambiguous about that backup — carry timedOut so the
    // dialog says "outcome unknown; export again", never "Export failed".
    it('carries timedOut on a fired timeout', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(timeoutError()))
        );
        const out = await exportSessionAdif(['a']);
        expect(out.kind).toBe('network');
        if (out.kind !== 'network') return;
        expect(out.timedOut).toBe(true);
    });

    it('leaves a generic (non-timeout) network failure without a timedOut marker', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('down')))
        );
        const out = await exportSessionAdif(['a']);
        expect(out.kind).toBe('network');
        if (out.kind !== 'network') return;
        expect(out.timedOut).toBeUndefined();
    });

    // Export is a state-mutating write (it archives a backup), so it must use the
    // write-class timeout, not the read default (F-04b).
    it('POSTs with the write-class timeout, not the read default', async () => {
        const spy = vi.spyOn(AbortSignal, 'timeout');
        stubFetch(new Response('<EOH>', { status: 200 }));
        await exportSessionAdif(['a']);
        expect(spy).toHaveBeenCalledWith(WRITE_TIMEOUT_MS);
    });
});
