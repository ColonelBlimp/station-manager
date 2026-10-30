// W-0024 slice 5a at the PRODUCTION BOUNDARY: while a Set as default request
// is on the wire, and while its restart is awaited, the real main.ts wiring
// gates live work — a Phone / CW submit is not sent, entry is locked and an
// FT8 TX start is refused. A state flag nobody wires would leave all three
// open, so each is driven through the composition root.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';

vi.mock('./lib/api/restart', () => ({
    fetchDaemonInstance: vi.fn(() => Promise.resolve('i1')),
    waitForDaemonBack: vi.fn(),
    restartDaemon: vi.fn(),
}));

import { installDaemon, submits, type Daemon } from './test/mainHarness';
import { waitForDaemonBack } from './lib/api/restart';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { logbooksState } from './lib/config/logbooks.svelte';
import { loadDefaultChange, setAsDefault } from './lib/config/defaultChange.svelte';
import { draft, entryLock, logDraft } from './lib/operate/qso.svelte';
import { armTx } from './lib/operate/ft8.svelte';
import { rig } from './lib/operate/rig.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const C = '01a187bc-0300-7000-8000-000000000003';
const REQ = '01a187bc-0400-7000-8000-000000000004';

let answerPost: () => void = () => {};
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
        },
        'POST /v1/default-logbook': {
            status: 202,
            body: { request_id: REQ, durability: 'durable' },
            hold: new Promise<void>((r) => (answerPost = r)),
        },
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

const GATED = /default logbook/i;

async function expectLiveWorkGated(call: string): Promise<void> {
    expect.soft(entryLock() ?? '').toMatch(GATED);
    const ft8 = await armTx(true);
    expect.soft(ft8.status).toBe('failed');
    expect.soft('message' in ft8 ? ft8.message : '').toMatch(GATED);
    const before = submits(daemon).length;
    fillDraft(call);
    await logDraft();
    expect.soft(submits(daemon)).toHaveLength(before);
}

describe('main.ts: live work is gated while Set as default is pending', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => reloads++);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
        logbooksState.rows = [
            { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV', description: '', count: 3 },
            { id: 3, uuid: C, name: 'Contest', callsign: '7Q5MLV', description: '', count: 0 },
        ];
        logbooksState.defaultId = 1;
        logbooksState.stationCallsign = '7Q5MLV';
        logbooksState.loaded = true;
        await loadDefaultChange();
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    // One sequence: request on the wire, then the restart awaited, then the
    // new instance. Soft checks so a red run reports every step.
    it('refuses submit, entry and FT8 TX through the request and the restart wait', async () => {
        let back: (v: boolean) => void = () => {};
        vi.mocked(waitForDaemonBack).mockReturnValue(new Promise((r) => (back = r)));

        const run = setAsDefault(3, () => true);
        await vi.waitFor(() =>
            expect(
                daemon.sent.some((s) => s.method === 'POST' && s.url === '/v1/default-logbook')
            ).toBe(true)
        );
        await expectLiveWorkGated('ZS6BOS');

        answerPost();
        await vi.waitFor(() => expect(waitForDaemonBack).toHaveBeenCalledWith('i1'));
        await expectLiveWorkGated('VK0YY');

        back(true);
        expect(await run).toBe(true);
        expect(reloads).toBe(1);
        expect(submits(daemon)).toHaveLength(0);
    });
});
