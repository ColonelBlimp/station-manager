// W-0024 slice 5b at the PRODUCTION BOUNDARY (operator ruling 2026-10-30): a
// status that cannot be read at boot gates live work. The daemon's seals stop
// writes and transmission, but not an entry typed and then discarded by an
// impending reload. The shell and Settings still load — the app is not inert —
// and the banner's recovery control reopens live work once a readable status
// proves admission open. (A 404 imposes no gate: every other main.ts test boots
// with the route unstubbed, which answers 404, and logs freely.)
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { installDaemon, submits, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { draft, entryLock, logDraft } from './lib/operate/qso.svelte';
import { armTx } from './lib/operate/ft8.svelte';
import { rig } from './lib/operate/rig.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';

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
        'GET /v1/default-logbook': {
            status: 500,
            body: { code: 'default_change_status_failed', message: 'config unreadable' },
        },
        'POST /v1/qso': {
            status: 201,
            body: { status: 'stored', uuid: 'q1', archive_id: ARCHIVE },
        },
    },
    sent: [],
};

function fillDraft(call: string): void {
    rig.band = '20m';
    rig.mode = 'USB';
    rig.freq = '14.200.000';
    rig.confirmedBand = '20m';
    draft.callsign = call;
    draft.dateOn = '2026-10-30';
    draft.timeOn = '12:00:00';
    draft.rstSent = '59';
    draft.rstRcvd = '59';
}

const GATED = /default logbook/i;

describe('main.ts: an unreadable default-change status at boot', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => {});
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    // One sequence: gated at boot, then reopened by the recovery control.
    it('locks entry, Log and FT8 admission; the shell stays usable; Check again reopens', async () => {
        expect
            .soft(daemon.sent.some((s) => s.method === 'GET' && s.url === '/v1/default-logbook'))
            .toBe(true);
        expect.soft(entryLock() ?? '').toMatch(GATED);
        const ft8 = await armTx(true);
        expect.soft('message' in ft8 ? ft8.message : '').toMatch(GATED);
        fillDraft('ZS6BOS');
        await logDraft();
        expect.soft(submits(daemon)).toHaveLength(0);
        expect.soft(archivesState.switchUnresolved).toBe(false);
        expect.soft(document.querySelector('[inert]')).toBeNull();

        daemon.routes['GET /v1/default-logbook'] = {
            status: 200,
            body: { effective: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' } },
        };
        const check = await screen.findByRole('button', { name: /Check again/ });
        await fireEvent.click(check);
        await vi.waitFor(() => expect(entryLock()).toBeNull());
        fillDraft('VK0YY');
        await logDraft();
        expect(submits(daemon)).toHaveLength(1);
    });
});
