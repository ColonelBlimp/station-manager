// PRODUCTION-BOUNDARY PIN for the FT8/FT4 switch (fresh-install ruling
// 2026-09-26). Imports the REAL main.ts with only the transport stubbed, on a
// daemon NOT serving FT8 (ft8_running false) although the switch was just saved
// on (ft8_enabled true, awaiting a restart), arriving by a bookmark to
// /operate/ft8 with FT8 as the stored last mode. Found on the fresh install: the
// FT view opened onto 'no such API route' beside live-looking Call CQ / Enable TX
// controls. The boot must land on Phone / CW with the note, never mount the FT
// view, and never request an FT8 route.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';

class FakeEventSource {
    static instances: FakeEventSource[] = [];
    constructor(public url: string) {
        FakeEventSource.instances.push(this);
    }
    addEventListener(): void {}
    close(): void {}
}

const requests: string[] = [];
function json(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { 'content-type': 'application/json' },
    });
}
function fakeFetch(input: RequestInfo | URL): Promise<Response> {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    requests.push(url);
    if (url === '/v1/config') {
        return Promise.resolve(
            json({
                setup_complete: true,
                default_logbook: { id: 1, name: 'Default', callsign: '7Q5MLV' },
                bridge: { enabled: false },
                ft8_enabled: true,
                ft8_running: false,
            })
        );
    }
    if (url === '/v1/version') {
        return Promise.resolve(json({ daemon: '2.0.0-test', go: 'go1.26', env: 'release' }));
    }
    if (url === '/v1/logbook/1/count') return Promise.resolve(json({ count: 0 }));
    return Promise.resolve(json({ error: 'not stubbed' }, 404));
}

describe('main.ts: FT8/FT4 turned off at boot', () => {
    beforeAll(async () => {
        vi.stubGlobal('EventSource', FakeEventSource);
        vi.stubGlobal('fetch', fakeFetch);
        window.localStorage.setItem('sm-op-mode', 'ft8');
        window.history.replaceState({}, '', '/operate/ft8');
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        // Boot settled: the station context resolved and seeded the header count.
        await vi.waitFor(() => expect(requests).toContain('/v1/logbook/1/count'));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
        window.localStorage.removeItem('sm-op-mode');
    });

    it('lands on Phone / CW with the note, and never touches an FT8 route', async () => {
        const { router } = await import('./lib/router.svelte');
        await vi.waitFor(() =>
            expect(document.body.textContent).toContain('FT8 and FT4 are turned off')
        );
        expect(router.view).toBe('operate');
        expect(router.mode).toBe('phone');
        expect(window.location.pathname).toBe('/operate/phone');
        expect(window.localStorage.getItem('sm-op-mode')).toBe('phone');
        expect(document.body.textContent).not.toMatch(/API route/i);
        // The sidebar offers no FT links.
        const nav = document.querySelector('nav') ?? document.body;
        expect(nav.textContent).not.toMatch(/\bFT8\b|\bFT4\b/);
        // Nothing FT8-shaped was requested or subscribed.
        expect(requests.filter((u) => u.startsWith('/v1/ft8'))).toEqual([]);
        expect(FakeEventSource.instances.filter((s) => s.url.startsWith('/v1/ft8'))).toEqual([]);
    });
});
