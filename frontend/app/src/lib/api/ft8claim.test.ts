import { afterEach, describe, expect, it, vi } from 'vitest';
import { claimFt8Profile } from './ft8claim';

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

describe('claimFt8Profile — POST /v1/ft8/claim (ADR 0080)', () => {
    it('200 → ok with the active profile the daemon reports', async () => {
        mockJSON(200, { mode: 'FT4' });
        expect(await claimFt8Profile('ft4')).toEqual({ kind: 'ok', mode: 'FT4' });
        const init = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit;
        expect(init.body).toBe('{"mode":"ft4"}');
    });

    it('409 → refused with the code and the retry hint', async () => {
        mockJSON(409, {
            code: 'ft8_session_active',
            message: 'winding down',
            retry_after_ms: 4200,
        });
        expect(await claimFt8Profile('ft4')).toEqual({
            kind: 'refused',
            code: 'ft8_session_active',
            message: 'winding down',
            retryAfterMs: 4200,
        });
    });

    it('409 without a hint → retryAfterMs 0', async () => {
        mockJSON(409, { code: 'ft8_profile_busy', message: 'busy' });
        const r = await claimFt8Profile('ft8');
        expect(r.kind === 'refused' && r.retryAfterMs).toBe(0);
    });

    it('400 → validation, 5xx → server', async () => {
        mockJSON(400, { code: 'ft8_profile_unknown', message: 'mode must be ft8 or ft4' });
        expect((await claimFt8Profile('ft4')).kind).toBe('validation');
        mockJSON(503, { code: 'internal_error', message: 'x' });
        expect((await claimFt8Profile('ft4')).kind).toBe('server');
    });
});

describe('claimFt8Profile — a grant must name the profile asked for', () => {
    it('200 naming the other profile is malformed, not a grant', async () => {
        mockJSON(200, { mode: 'FT8' });
        const r = await claimFt8Profile('ft4');
        expect(r.kind).toBe('validation');
        expect(r.kind === 'validation' && r.code).toBe('ft8_claim_malformed');
    });

    it('200 naming no profile is malformed too', async () => {
        mockJSON(200, {});
        const r = await claimFt8Profile('ft4');
        expect(r.kind === 'validation' && r.code).toBe('ft8_claim_malformed');
    });

    it('a case-insensitive matching name is a grant, normalised upper-case', async () => {
        mockJSON(200, { mode: 'ft4' });
        expect(await claimFt8Profile('ft4')).toEqual({ kind: 'ok', mode: 'FT4' });
    });
});
