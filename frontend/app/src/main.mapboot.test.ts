// PRODUCTION-BOUNDARY PIN for the Map tab's stream budget (dogfood 2026-09-11/13,
// operator ruling 2026-09-14, W-0012): a tab that boots on the map route opens the
// shell's always-on /v1/events (the map rides it) and NOT /v1/rig/events, even
// with CAT enabled — the full-window map renders no header chip, Rig panel or
// alarm banner, so the rig stream would hold a browser connection for nothing.
// Imports the REAL main.ts with only fetch + EventSource stubbed, like
// main.boot.test.ts; that file pins the positive half (CAT on, an Operate route
// → the rig stream opens) — see its bridge-enabled case.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';

class FakeEventSource {
    static instances: FakeEventSource[] = [];
    listeners = new Map<string, ((ev: MessageEvent<string>) => void)[]>();
    readyState = 0;
    constructor(public url: string) {
        FakeEventSource.instances.push(this);
    }
    addEventListener(type: string, fn: (ev: MessageEvent<string>) => void): void {
        const list = this.listeners.get(type) ?? [];
        list.push(fn);
        this.listeners.set(type, list);
    }
    close(): void {
        this.readyState = 2;
    }
}

function json(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { 'content-type': 'application/json' },
    });
}
const requests: string[] = [];
function fakeFetch(input: RequestInfo | URL): Promise<Response> {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    requests.push(url);
    if (url === '/v1/config') {
        return Promise.resolve(
            json({
                setup_complete: true,
                default_logbook: { id: 1, name: 'Default', callsign: '7Q5MLV' },
                bridge: { enabled: true, driver: 'ftdx10', rig_name: 'FTdx10' },
            })
        );
    }
    if (url === '/v1/version') {
        return Promise.resolve(json({ daemon: '2.0.0-test', go: 'go1.26', env: 'release' }));
    }
    if (url === '/v1/logbook/1/count') return Promise.resolve(json({ count: 7468 }));
    return Promise.resolve(json({ error: 'not stubbed' }, 404));
}
const sourceFor = (url: string): FakeEventSource | undefined =>
    FakeEventSource.instances.find((s) => s.url === url);

describe('main.ts: a Map tab holds the log stream only', () => {
    beforeAll(async () => {
        vi.stubGlobal('EventSource', FakeEventSource);
        vi.stubGlobal('fetch', fakeFetch);
        const base = import.meta.env.BASE_URL.replace(/\/$/, '');
        window.history.replaceState({}, '', `${base}/map`);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        // Boot settles once station context has been applied (the count seed).
        await vi.waitFor(() =>
            expect(requests.filter((u) => u === '/v1/logbook/1/count')).toHaveLength(1)
        );
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it('opens /v1/events and never /v1/rig/events with CAT enabled on the map route', () => {
        expect(sourceFor('/v1/events')).toBeDefined();
        expect(sourceFor('/v1/rig/events')).toBeUndefined();
    });
});
