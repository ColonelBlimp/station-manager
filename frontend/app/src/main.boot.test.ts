// PRODUCTION-BOUNDARY PIN for the header "(n)" logbook count (alpha.2 dogfood
// Finding #16, W-0012). This imports the REAL main.ts — the composition root —
// with only the transport stubbed (fetch + EventSource), and asserts at the
// request level, so removing the one wiring line in main.ts turns it red.
//
// Rule under test: the count is fetched once at boot (the seed) and then again
// exactly once per SSE reconnection transition (error → reopen) of the shell's
// always-on /v1/events stream — never on the stream's boot open, never again on a
// reopen with no new error. The stream is opened with CAT OFF (bridge disabled),
// because Finding #16's originating case is a fresh install with no rig.
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
    emit(type: string, data?: string): void {
        if (type === 'open') this.readyState = 1;
        for (const fn of this.listeners.get(type) ?? []) fn({ data } as MessageEvent<string>);
    }
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
            })
        );
    }
    if (url === '/v1/version') {
        return Promise.resolve(json({ daemon: '2.0.0-test', go: 'go1.26', env: 'release' }));
    }
    if (url === '/v1/logbook/1/count') return Promise.resolve(json({ count: 7468 }));
    return Promise.resolve(json({ error: 'not stubbed' }, 404));
}
const countRequests = (): number => requests.filter((u) => u === '/v1/logbook/1/count').length;
const sourceFor = (url: string): FakeEventSource | undefined =>
    FakeEventSource.instances.find((s) => s.url === url);

describe('main.ts: header count refresh on the shell stream reconnection', () => {
    beforeAll(async () => {
        vi.stubGlobal('EventSource', FakeEventSource);
        vi.stubGlobal('fetch', fakeFetch);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        // Boot settles: /v1/config → station context → the count seed.
        await vi.waitFor(() => expect(countRequests()).toBe(1));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    // One sequence, deliberately: each step's expectation depends on the request
    // count the previous step established, so splitting them would make the later
    // cases order-dependent on the earlier ones (review 2026-09-07).
    it('boot seeds once; the shell stream reconnection refreshes exactly once per transition', async () => {
        // The always-on stream is open with CAT off, and no rig stream was opened.
        const src = sourceFor('/v1/events');
        expect(src).toBeDefined();
        expect(sourceFor('/v1/rig/events')).toBeUndefined();

        // The stream's boot open is not a reconnection: no request beyond the seed.
        src!.emit('open');
        await Promise.resolve();
        expect(countRequests()).toBe(1);

        // One drop then a reopen is one transition: exactly one more request.
        src!.emit('error');
        src!.emit('open');
        await vi.waitFor(() => expect(countRequests()).toBe(2));

        // A further reopen with no new error is not a transition: nothing.
        src!.emit('open');
        await Promise.resolve();
        expect(countRequests()).toBe(2);
    });
});
