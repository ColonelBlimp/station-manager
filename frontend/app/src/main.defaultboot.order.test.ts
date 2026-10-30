// W-0024 slice 5b at the PRODUCTION BOUNDARY: the default-change status is read
// INSIDE the identity bracket, and live work stays locked until it settles. The
// response is held behind a barrier: while it is outstanding the bracket has
// not closed, entry is locked, a Log is not sent and FT8 TX is refused; once a
// readable status names this tab's default, live work opens.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, submits, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { draft, entryLock, logDraft } from './lib/operate/qso.svelte';
import { armTx } from './lib/operate/ft8.svelte';
import { rig } from './lib/operate/rig.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';

let releaseStatus: () => void = () => {};
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
            status: 200,
            body: { effective: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' } },
            hold: new Promise<void>((r) => (releaseStatus = r)),
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

describe('main.ts: the boot status read, held', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => {});
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it('live work stays locked until the status settles inside the bracket', async () => {
        await vi.waitFor(() =>
            expect(
                daemon.sent.some((s) => s.method === 'GET' && s.url === '/v1/default-logbook')
            ).toBe(true)
        );
        // Let every other boot read (identity, config, catalogue) answer, so
        // only the held status can be keeping the bracket open.
        for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 10));
        expect.soft(daemon.sent.some((s) => s.url === '/v1/config')).toBe(true);
        // Inside the bracket: it has not closed while the status is outstanding.
        expect.soft(archivesState.verifying).toBe(true);
        expect.soft(entryLock() ?? '').toMatch(GATED);
        const ft8 = await armTx(true);
        expect.soft(ft8.status).toBe('failed');
        fillDraft('ZS6BOS');
        await logDraft();
        expect.soft(submits(daemon)).toHaveLength(0);

        releaseStatus();
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
        await vi.waitFor(() => expect(entryLock()).toBeNull());
        fillDraft('VK0YY');
        await logDraft();
        expect(submits(daemon)).toHaveLength(1);
    });
});
