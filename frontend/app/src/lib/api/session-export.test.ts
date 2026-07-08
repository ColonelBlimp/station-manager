import { describe, it, expect, afterEach, vi } from 'vitest';
import { exportSessionAdif } from './session-export';

afterEach(() => {
    vi.restoreAllMocks();
});

function stubFetch(response: Response): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(response))
    );
}

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
});
