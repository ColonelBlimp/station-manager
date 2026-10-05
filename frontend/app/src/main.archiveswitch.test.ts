// PRODUCTION-BOUNDARY PIN for ADR 0087. Imports the REAL main.ts with transport,
// browser-storage and reload seams, so re-wiring the removed recovery turns it red.
//
//   R1  Another archive served after a reconnect reloads this window at once —
//       an unlogged Phone / CW entry is not saved, and the reload is not held.
//   O1  Orphaned saved-QSO records stay untouched: from boot through that
//       rebind, nothing opens or deletes an IndexedDB database, takes a Web
//       Lock or opens a BroadcastChannel, and no attribution is read.
//   C3  The Phone / CW reports refill when the mode's default changes (C3a);
//       after a rig-stream drop they refill AT ONCE — no reading from before
//       the drop is held for a save any more (C3b; ADR 0085 held it).
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { draft } from './lib/operate/qso.svelte';
import { rig } from './lib/operate/rig.svelte';
import { flushSync } from 'svelte';
import {
    installBrowserStorageSpies,
    type BrowserStorageSpies,
} from './lib/utils/browserStorageSpies.fixture';

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
        if (type === 'error') this.readyState = 0;
        for (const fn of this.listeners.get(type) ?? []) fn({ data } as MessageEvent<string>);
    }
}

const requests: string[] = [];
let identity = { instance: 'i1', archive: { id: 'arch-a', label: 'Home' } };
const reload = vi.fn();
let spies: BrowserStorageSpies;
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
                logging_station: { station_callsign: '7Q5MLV', operator: 'G0XYZ' },
                default_logbook: { id: 1, name: 'Default', callsign: '7Q5MLV', uuid: 'lb-a' },
                bridge: { enabled: true },
            })
        );
    }
    if (url === '/v1/version') {
        return Promise.resolve(json({ daemon: '2.0.0-test', ...identity }));
    }
    if (url === '/v1/logbook/1/count') return Promise.resolve(json({ count: 1 }));
    if (url === '/v1/qso-archives') return Promise.resolve(json({ archives: [] }));
    return Promise.resolve(json({ error: 'not stubbed' }, 404));
}
const sourceFor = (url: string): FakeEventSource | undefined =>
    FakeEventSource.instances.find((s) => s.url === url);

describe('main.ts after ADR 0087', () => {
    beforeAll(async () => {
        spies = installBrowserStorageSpies();
        vi.stubGlobal('EventSource', FakeEventSource);
        vi.stubGlobal('fetch', fakeFetch);
        _setReloadForTests(reload);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() =>
            expect(requests.filter((u) => u === '/v1/version').length).toBeGreaterThanOrEqual(2)
        );
    });
    afterAll(() => {
        spies.restore();
    });

    it('C3a a mode change refills both reports', () => {
        rig.mode = 'USB';
        flushSync();
        rig.mode = 'CW';
        flushSync();
        expect([draft.rstSent, draft.rstRcvd]).toEqual(['599', '599']);
        rig.mode = 'USB';
        flushSync();
        expect([draft.rstSent, draft.rstRcvd]).toEqual(['59', '59']);
    });

    it('C3b after a rig-stream drop a changed mode default refills at once', async () => {
        await vi.waitFor(() => expect(sourceFor('/v1/rig/events')).toBeDefined());
        rig.mode = 'USB';
        flushSync();
        sourceFor('/v1/rig/events')!.emit('error');
        rig.mode = 'CW';
        flushSync();
        expect([draft.rstSent, draft.rstRcvd]).toEqual(['599', '599']);
        sourceFor('/v1/rig/events')!.emit('open');
    });

    it('R1/O1 a foreign rebind reloads at once, saving nothing and touching no browser storage', async () => {
        await new Promise((r) => setTimeout(r, 50)); // let boot settle
        draft.callsign = '7Q7CT';
        draft.comment = 'lost by design';
        const src = sourceFor('/v1/events')!;
        src.emit('error');
        identity = { instance: 'i2', archive: { id: 'arch-b', label: 'Away' } };
        src.emit('open');
        await vi.waitFor(() => expect(reload).toHaveBeenCalledOnce());
        expect(archivesState.switchUnresolved).toBe(true); // latched before the reload
        expect(spies.opens).toEqual([]);
        expect(spies.deletes).toEqual([]);
        expect(spies.locks).toEqual([]);
        expect(spies.channels).toEqual([]);
        expect(requests.filter((u) => u.includes('submit-attribution'))).toEqual([]);
    });
});
