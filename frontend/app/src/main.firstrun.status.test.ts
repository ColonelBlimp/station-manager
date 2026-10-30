// W-0024 slice 5b at the PRODUCTION BOUNDARY, first run with the routes served:
// before setup the daemon has no default, and its status names an effective
// logbook without a UUID (internal/archive effectiveDefault), which the client
// reads as unreadable — live work is locked. Setup succeeds and the status is
// read again: while that read is outstanding, and when it fails, live work stays
// locked with the recovery controls shown; a valid status then releases logging.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { installDaemon, submits, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { setBoundDefault } from './lib/api/archive-guard';
import { saveSetup } from './lib/setup.svelte';
import { draft, entryLock, logDraft } from './lib/operate/qso.svelte';
import { rig } from './lib/operate/rig.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const STATUS = 'GET /v1/default-logbook';

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
        // No default yet: the served answer names no UUID.
        [STATUS]: { status: 200, body: { effective: { id: 1 } } },
        'PUT /v1/config': { status: 200, body: {} },
        'POST /v1/qso': {
            status: 201,
            body: { status: 'stored', uuid: 'q1', archive_id: ARCHIVE },
        },
    },
    sent: [],
};
const statusReads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/default-logbook').length;

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

describe('main.ts: first run with the default-change routes served', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => {});
        setBoundDefault(null);
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    // One sequence. Early checks are soft so a red run reports every step.
    it('unreadable before setup → setup → re-read pending, then failed, then valid releases logging', async () => {
        expect.soft(statusReads()).toBe(1);
        expect.soft(entryLock() ?? '').toMatch(GATED);

        // Setup seeds the default; the re-read is held.
        let release: () => void = () => {};
        daemon.routes[STATUS] = {
            status: 500,
            body: { code: 'default_change_status_failed', message: 'x' },
            hold: new Promise<void>((r) => (release = r)),
        };
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
        await vi.waitFor(() => expect(statusReads()).toBe(2));

        // While the re-read is outstanding: still locked, nothing sent.
        expect.soft(entryLock() ?? '').toMatch(GATED);
        fillDraft('ZS6BOS');
        await logDraft();
        expect.soft(submits(daemon)).toHaveLength(0);

        // It fails: still locked, and the recovery controls are shown.
        release();
        const check = await screen.findByRole('button', { name: /Check again/ });
        expect.soft(screen.getByRole('button', { name: /Reload/ })).toBeTruthy();
        expect.soft(entryLock() ?? '').toMatch(GATED);

        // A valid status releases logging.
        daemon.routes[STATUS] = {
            status: 200,
            body: { effective: { id: 1, uuid: A, name: 'Default', callsign: '7Q5MLV' } },
        };
        await fireEvent.click(check);
        await vi.waitFor(() => expect(entryLock()).toBeNull());
        fillDraft('VK0YY');
        await logDraft();
        expect(submits(daemon)).toHaveLength(1);
    });
});
