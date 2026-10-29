// W-0024 slice 4 at the PRODUCTION BOUNDARY, first run: the tab boots before a
// default exists (no pair, nothing bound); setup seeds it and the re-fetched
// context establishes the pair WITHOUT a reload. The live submit and an FT8
// start then assert it, and a reconnect to the same daemon changes nothing.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, pairOf, shellStream, submits, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { saveSetup } from './lib/setup.svelte';
import { draft, logDraft } from './lib/operate/qso.svelte';
import { rig } from './lib/operate/rig.svelte';
import { startFt8Cq } from './lib/api/ft8qso';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';

const daemon: Daemon = {
    version: {
        daemon: 'test',
        instance: 'i1',
        archive: { id: ARCHIVE, label: 'Home', ownership: 'legacy' },
    },
    config: {
        setup_complete: false,
        default_logbook: { id: 1 },
        bridge: { enabled: false },
    },
    routes: {
        'PUT /v1/config': { status: 200, body: {} },
        'POST /v1/qso': {
            status: 201,
            body: { status: 'stored', uuid: 'q1', archive_id: ARCHIVE },
        },
        'POST /v1/ft8/cq/start': { status: 202, body: {} },
    },
    sent: [],
};
let reloads = 0;

describe('main.ts: first-run setup establishes the live context', () => {
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

    // Early checks are soft so a red run reports every step.
    it('setup → the pair (1, A) for a submit, A for FT8, and a same-daemon reconnect keeps it', async () => {
        // The daemon seeds the default when setup is saved (same process).
        daemon.config = {
            setup_complete: true,
            logging_station: { station_callsign: '7Q5MLV' },
            default_logbook: { id: 1, uuid: A, name: 'Default', callsign: '7Q5MLV' },
            bridge: { enabled: false },
        };
        daemon.version = {
            daemon: 'test',
            instance: 'i1',
            archive: { id: ARCHIVE, label: 'Home', ownership: 'legacy', default_logbook_uuid: A },
        };
        expect((await saveSetup('7Q5MLV')).ok).toBe(true);

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
        expect.soft(submits(daemon).map(pairOf)).toEqual([['1', A]]);

        // Logging re-checks the identity in the background; FT8 waits for it.
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
        await startFt8Cq(1500, 14.074);
        const cq = daemon.sent.filter((s) => s.url === '/v1/ft8/cq/start');
        expect.soft(cq.map((s) => s.defaultLogbook)).toEqual([A]);

        const src = shellStream();
        src.emit('open');
        src.emit('error');
        src.emit('open');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
        await Promise.resolve();
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(false);
    });
});
