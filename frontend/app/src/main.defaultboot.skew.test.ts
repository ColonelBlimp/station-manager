// W-0024 slice 5b at the PRODUCTION BOUNDARY: the default-change status can
// arrive BEFORE the station context. When the two name different defaults the
// tab is stale — it reloads and live work stays locked; it never adopts the
// status's default, and never logs to the context's either.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, submits, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { setBoundDefault } from './lib/api/archive-guard';
import { draft, entryLock, logDraft } from './lib/operate/qso.svelte';
import { rig } from './lib/operate/rig.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const B = '01a187bc-0200-7000-8000-000000000002';

let releaseConfig: () => void = () => {};
const daemon: Daemon = {
    version: {
        daemon: 'test',
        instance: 'i1',
        archive: { id: ARCHIVE, label: 'Home', ownership: 'legacy', default_logbook_uuid: A },
    },
    config: {
        setup_complete: true,
        logging_station: { station_callsign: '7Q5MLV' },
        default_logbook: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' },
        bridge: { enabled: false },
    },
    configHold: new Promise<void>((r) => (releaseConfig = r)),
    routes: {
        'GET /v1/default-logbook': {
            status: 200,
            body: { effective: { id: 2, uuid: B, name: 'Contest', callsign: '7Q5MLV' } },
        },
        'POST /v1/qso': {
            status: 201,
            body: { status: 'stored', uuid: 'q1', archive_id: ARCHIVE },
        },
    },
    sent: [],
};
let reloads = 0;

describe('main.ts: the status arrives before the station context, naming another default', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => reloads++);
        // A fresh page has no established default until its context is read
        // (test/setup.ts binds one globally for other tests).
        setBoundDefault(null);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it('reloads, keeps live work locked, and sends no Log', async () => {
        await vi.waitFor(() =>
            expect(
                daemon.sent.some((s) => s.method === 'GET' && s.url === '/v1/default-logbook')
            ).toBe(true)
        );
        expect.soft(reloads).toBe(0);
        releaseConfig();
        await vi.waitFor(() => expect(reloads).toBe(1));
        // Locked by the reload's latch, whose detail names the other default.
        expect.soft(entryLock()).not.toBeNull();
        expect.soft(archivesState.switchDetail).toMatch(/another default logbook/);
        rig.band = '20m';
        rig.mode = 'USB';
        rig.freq = '14.200.000';
        rig.confirmedBand = '20m';
        draft.callsign = 'ZS6BOS';
        draft.dateOn = '2026-10-30';
        draft.timeOn = '12:00:00';
        draft.rstSent = '59';
        draft.rstRcvd = '59';
        await logDraft();
        expect(submits(daemon)).toHaveLength(0);
    });
});
