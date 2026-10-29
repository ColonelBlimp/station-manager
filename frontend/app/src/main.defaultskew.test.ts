// W-0024 slice 4 at the PRODUCTION BOUNDARY: the station context and the
// daemon's identity are read inside one bracket. If the context's default
// disagrees with the default the daemon names, the tab has no single
// established context: it reloads, and no live submit is sent from it.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, submits, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { draft, logDraft } from './lib/operate/qso.svelte';
import { rig } from './lib/operate/rig.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const B = '01a187bc-0200-7000-8000-000000000002';

const daemon: Daemon = {
    version: {
        daemon: 'test',
        instance: 'i1',
        archive: { id: ARCHIVE, label: 'Home', ownership: 'legacy', default_logbook_uuid: B },
    },
    config: {
        setup_complete: true,
        logging_station: { station_callsign: '7Q5MLV' },
        default_logbook: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' },
        bridge: { enabled: false },
    },
    routes: {
        'POST /v1/qso': {
            status: 201,
            body: { status: 'stored', uuid: 'q1', archive_id: ARCHIVE },
        },
    },
    sent: [],
};
let reloads = 0;

describe('main.ts: a context that disagrees with the daemon’s default', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => reloads++);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it('reloads at boot and sends no live submit', async () => {
        await vi.waitFor(() => expect(reloads).toBe(1));
        rig.band = '20m';
        rig.mode = 'USB';
        rig.freq = '14.200.000';
        rig.confirmedBand = '20m';
        draft.callsign = 'VK0YY';
        draft.dateOn = '2026-10-29';
        draft.timeOn = '12:00:00';
        draft.rstSent = '59';
        draft.rstRcvd = '59';
        await logDraft();
        expect(submits(daemon)).toHaveLength(0);
    });
});
