// The POSITIVE half of main.mapboot.test.ts: with CAT enabled on an Operate route
// the shell DOES open /v1/rig/events. Without this pin, the map-route test would
// also pass against a shell that never opens the rig stream at all.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';

class FakeEventSource {
    static instances: FakeEventSource[] = [];
    readyState = 0;
    constructor(public url: string) {
        FakeEventSource.instances.push(this);
    }
    addEventListener(): void {}
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

describe('main.ts: an Operate tab with CAT enabled holds the rig stream', () => {
    beforeAll(async () => {
        vi.stubGlobal('EventSource', FakeEventSource);
        vi.stubGlobal('fetch', fakeFetch);
        const base = import.meta.env.BASE_URL.replace(/\/$/, '');
        window.history.replaceState({}, '', `${base}/operate/phone`);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() =>
            expect(requests.filter((u) => u === '/v1/logbook/1/count')).toHaveLength(1)
        );
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it('opens both /v1/events and /v1/rig/events', () => {
        expect(sourceFor('/v1/events')).toBeDefined();
        expect(sourceFor('/v1/rig/events')).toBeDefined();
    });
});
