// PRODUCTION-BOUNDARY PIN for a saved draft's attribution (ADR 0085; operator
// rulings 2026-10-02 and 2026-10-05). Imports the REAL main.ts with transport,
// browser storage and reload seams, so removing a wiring line turns it red.
//
//   M1  Boot reads GET /v1/submit-attribution for the operator this page sends
//       (logging_station.operator), inside the boot identity bracket.
//   M2  A config.updated on /v1/events — another client's write — re-reads it.
//   M3  A drop then a proven reopen of /v1/events re-reads it, although the
//       earlier read succeeded.
//   M4  A drop followed by another window's archive switch saves the last read
//       attribution with the original source, eligible when that source returns.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { _setDraftStoreForTests, memoryDraftStore } from './lib/drafts/draftStore';
import { _setReloadForTests, bootArchiveId } from './lib/config/archives.svelte';
import { currentAttribution, attributionSettled } from './lib/drafts/attributionSource.svelte';
import { draft } from './lib/operate/qso.svelte';
import { restoreEligibility } from './lib/drafts/restore';

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
const mem = memoryDraftStore();
const reload = vi.fn();
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
                bridge: { enabled: false },
            })
        );
    }
    if (url === '/v1/version') {
        return Promise.resolve(json({ daemon: '2.0.0-test', ...identity }));
    }
    if (url.startsWith('/v1/submit-attribution')) {
        return Promise.resolve(
            json({
                my_rig: identity.archive.id === 'arch-a' ? 'FTdx10' : 'OTHER ARCHIVE RIG',
                operator: 'G0XYZ',
                my_name: 'Guest',
            })
        );
    }
    if (url === '/v1/logbook/1/count') return Promise.resolve(json({ count: 1 }));
    if (url === '/v1/qso-archives') return Promise.resolve(json({ archives: [] }));
    return Promise.resolve(json({ error: 'not stubbed' }, 404));
}
const attributionReads = (): string[] =>
    requests.filter((u) => u.startsWith('/v1/submit-attribution'));
const sourceFor = (url: string): FakeEventSource | undefined =>
    FakeEventSource.instances.find((s) => s.url === url);

describe('main.ts: a saved draft’s attribution', () => {
    beforeAll(async () => {
        vi.stubGlobal('EventSource', FakeEventSource);
        vi.stubGlobal('fetch', fakeFetch);
        _setDraftStoreForTests(mem);
        _setReloadForTests(reload);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(attributionReads().length).toBeGreaterThan(0));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
        _setDraftStoreForTests(null);
    });

    // One sequence: each step builds on the read count the previous one set.
    it('boot, config.updated and a proven reconnect each read it, for the sent operator', async () => {
        // M1: read for the operator the page sends, after the context named it.
        expect(attributionReads()).toEqual(['/v1/submit-attribution?operator=G0XYZ']);
        const configRead = requests.indexOf('/v1/config');
        expect(configRead).toBeGreaterThanOrEqual(0);
        expect(requests.indexOf(attributionReads()[0])).toBeGreaterThan(configRead);

        const src = sourceFor('/v1/events');
        expect(src).toBeDefined();
        src!.emit('open');

        // M2: another client's write.
        src!.emit('config.updated', '{}');
        await vi.waitFor(() => expect(attributionReads()).toHaveLength(2));

        // M3: a drop and a reopen, proven by the identity check, reads again.
        // Keep an actual draft across this restart: no archive change, no save.
        draft.callsign = '7Q7CT';
        draft.dateOn = '2026-10-05';
        draft.timeOn = '10:00:00';
        src!.emit('error');
        identity = { ...identity, instance: 'i2' };
        src!.emit('open');
        await vi.waitFor(() => expect(attributionReads()).toHaveLength(3));
        expect(new Set(attributionReads())).toEqual(
            new Set(['/v1/submit-attribution?operator=G0XYZ'])
        );
    });

    it('M4 the disconnect then archive-switch save keeps attribution and offers Restore on return', async () => {
        await attributionSettled();
        const original = currentAttribution('G0XYZ');
        expect(original).toEqual({ myRig: 'FTdx10', operator: 'G0XYZ', myName: 'Guest' });
        expect(bootArchiveId()).toBe('arch-a');
        expect(mem.rows.size).toBe(0); // the same-archive restart did not save
        expect(reload).not.toHaveBeenCalled();

        const src = sourceFor('/v1/events')!;
        src.emit('error');
        identity = { instance: 'i3', archive: { id: 'arch-b', label: 'Away' } };
        const reads = attributionReads().length;
        src.emit('open');
        await vi.waitFor(() => expect(reload).toHaveBeenCalledOnce());
        expect(mem.rows.size).toBe(1);
        const [saved] = [...mem.rows.values()];
        expect(saved).toMatchObject({
            archiveId: 'arch-a',
            logbookUuid: 'lb-a',
            operator: 'G0XYZ',
            fields: { callsign: '7Q7CT', dateOn: '2026-10-05', timeOn: '10:00:00' },
        });
        // This is the S2.5 regression: old code reaches here with null.
        expect(saved.attribution).toEqual(original);
        expect(attributionReads()).toHaveLength(reads); // never borrow the new archive's value
        expect(currentAttribution('G0XYZ')).toBeNull(); // held value is not current
        expect(
            restoreEligibility(saved, {
                locksAvailable: true,
                onPhoneCw: true,
                bootArchiveId: 'arch-a',
                activeLogbookUuid: 'lb-a',
                currentAttribution: original,
            })
        ).toEqual({ kind: 'eligible' });
    });
});
