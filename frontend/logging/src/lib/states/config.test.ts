import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { configState } from './config.svelte';

afterEach(() => {
    vi.restoreAllMocks();
});

beforeEach(() => {
    // Each test owns its own id + count; reset to a known baseline so
    // tests don't leak state through the module-singleton configState.
    configState.defaultLogbook.id = 0;
    configState.defaultLogbook.qsoCount = 0;
});

function mockFetchJSON(status: number, body: unknown): void {
    const response = new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
    vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(response))
    );
}

describe('configState.refreshLogbookCount', () => {
    it('updates qsoCount on a successful 200', async () => {
        configState.defaultLogbook.id = 7;
        mockFetchJSON(200, { logbook_id: 7, count: 4233 });

        await configState.refreshLogbookCount();

        expect(configState.defaultLogbook.qsoCount).toBe(4233);
    });

    it('skips the fetch entirely when no default logbook is set (id=0)', async () => {
        // id=0 is the pre-setup baseline — there is no logbook to count
        // against, so the wrapper must not even make the request (a 404
        // for id=0 would otherwise toast or churn).
        const fetchSpy = vi.fn(() =>
            Promise.resolve(new Response('{}', { status: 200 }))
        );
        vi.stubGlobal('fetch', fetchSpy);

        await configState.refreshLogbookCount();

        expect(fetchSpy).not.toHaveBeenCalled();
        expect(configState.defaultLogbook.qsoCount).toBe(0);
    });

    it('leaves the previous count visible when the fetch fails', async () => {
        // Seed a known-good count first, then a network failure on the
        // refresh — the stale-but-true previous value beats blanking to
        // zero (which would falsely advertise an empty logbook).
        configState.defaultLogbook.id = 7;
        configState.defaultLogbook.qsoCount = 100;

        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new Error('boom')))
        );

        await configState.refreshLogbookCount();

        expect(configState.defaultLogbook.qsoCount).toBe(100);
    });

    it('leaves the previous count visible on 404', async () => {
        configState.defaultLogbook.id = 7;
        configState.defaultLogbook.qsoCount = 50;
        mockFetchJSON(404, { code: 'logbook_not_found', message: 'gone' });

        await configState.refreshLogbookCount();

        expect(configState.defaultLogbook.qsoCount).toBe(50);
    });
});
