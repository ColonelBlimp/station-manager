// W-0024 slice 4 at the PRODUCTION BOUNDARY: the real main.ts loads the station
// context inside the identity bracket and builds the live submit from it. The
// logbook a submit names (?logbook=) and the default it asserts
// (X-SM-Default-Logbook) must come from that one context and stay together —
// through a reconnect that finds another default too. The daemon permits a
// submit naming a logbook other than the asserted default (a deliberate
// operation), so an old numeric target sent with a refreshed assertion would
// be logged where the operator never meant: the pairs below are the guard.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, pairOf, shellStream, submits, type Daemon } from './test/mainHarness';
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
        archive: { id: ARCHIVE, label: 'Home', ownership: 'legacy', default_logbook_uuid: A },
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

function fillDraft(call: string): void {
    rig.band = '20m';
    rig.mode = 'USB';
    rig.freq = '14.200.000';
    rig.confirmedBand = '20m';
    draft.callsign = call;
    draft.dateOn = '2026-10-29';
    draft.timeOn = '12:00:00';
    draft.rstSent = '59';
    draft.rstRcvd = '59';
}

describe('main.ts: the live submit pair', () => {
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

    // One sequence: the reconnect step depends on the booted context. Early
    // checks are soft so a red run reports every step.
    it('submits (1, A) from the booted context, and never a mixed pair after a reconnect finds B', async () => {
        fillDraft('VK0YY');
        await logDraft();
        expect.soft(submits(daemon).map(pairOf)).toEqual([['1', A]]);
        expect.soft(submits(daemon)[0].archive).toBe(ARCHIVE);

        // The daemon restarts with B effective (logbook 2); the shell stream reconnects.
        daemon.version = {
            daemon: 'test',
            instance: 'i2',
            archive: { id: ARCHIVE, label: 'Home', ownership: 'legacy', default_logbook_uuid: B },
        };
        daemon.config = {
            setup_complete: true,
            logging_station: { station_callsign: '7Q5MLV' },
            default_logbook: { id: 2, uuid: B, name: 'Contest', callsign: '7Q5MLV' },
            bridge: { enabled: false },
        };
        const src = shellStream();
        src.emit('open');
        src.emit('error');
        src.emit('open');
        await vi.waitFor(() => expect(reloads).toBe(1));

        // The reload is stubbed, so this tab is still the old page: a further
        // submit is held, or — if sent — is the old context's pair, whole.
        fillDraft('ZS6BOS');
        await logDraft();
        for (const p of submits(daemon).map(pairOf)) expect(p).toEqual(['1', A]);
    });
});
