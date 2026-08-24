import { describe, it, expect, afterEach, vi } from 'vitest';
import { recordExportFailed } from './notifications';

afterEach(() => {
    vi.unstubAllGlobals();
});

const urlOf = (input: RequestInfo | URL): string =>
    typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

describe('recordExportFailed', () => {
    it('posts only the allowlisted typed fields — never a message or code', async () => {
        let url: string | undefined;
        let init: RequestInit | undefined;
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL, i?: RequestInit) => {
                url = urlOf(input);
                init = i;
                return Promise.resolve(new Response(null, { status: 204 }));
            })
        );

        await recordExportFailed(42, 'server');

        expect(url).toBe('/v1/notifications');
        expect(init?.method).toBe('POST');
        const body = JSON.parse(init?.body as string) as Record<string, unknown>;
        // Exactly the three allowlisted keys, nothing else (no message/code/reason).
        expect(body).toEqual({ kind: 'export.adif_failed', count: 42, outcome: 'server' });
        expect(Object.keys(body).sort()).toEqual(['count', 'kind', 'outcome']);
    });

    it('swallows a transport failure so the caller is never disturbed', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new Error('connection refused')))
        );
        // Must resolve, not reject — a post failure cannot suppress the caller's toast.
        await expect(recordExportFailed(1, 'network')).resolves.toBeUndefined();
    });
});
