import { describe, it, expect, afterEach, vi } from 'vitest';
import { recordExportFailed, fetchNotifications } from './notifications';

function stubJson(status: number, body: unknown): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

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

describe('fetchNotifications', () => {
    it('returns the items on a 200 envelope', async () => {
        stubJson(200, {
            items: [
                {
                    id: 2,
                    category: 'notification',
                    kind: 'forward.failed',
                    severity: 'warn',
                    occurred_at: '2026-08-24T06:00:00Z',
                    build: 'v-test',
                    detail: { qso_id: 7, forwarder: 'qrz', action: 'insert', attempts: 2 },
                },
            ],
        });
        const out = await fetchNotifications(50);
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') {
            expect(out.items).toHaveLength(1);
            expect(out.items[0].kind).toBe('forward.failed');
        }
    });

    it('maps a transport failure to an error outcome', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new Error('connection refused')))
        );
        const out = await fetchNotifications();
        expect(out.kind).toBe('error');
    });

    it('maps a daemon error status to an error outcome', async () => {
        stubJson(500, { code: 'db_error', message: 'boom' });
        const out = await fetchNotifications();
        expect(out.kind).toBe('error');
    });

    it('rejects an unexpected (non-envelope) body', async () => {
        stubJson(200, [{ id: 1 }]); // bare array, not {items:[]}
        const out = await fetchNotifications();
        expect(out.kind).toBe('error');
    });
});
