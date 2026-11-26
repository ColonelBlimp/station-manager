// W-0029 part 6 at the PRODUCTION BOUNDARY: a session state answered for another
// archive than the one the boot proved (Codex's review, codex-275).
//
// The boot's identity reads prove archive A. Before the page's first read of
// the session's state is answered, the daemon comes to serve archive B, and
// the event stream then opens on it for the first time: that is not a
// reconnection, so the page's reconnection check does not run. The state the
// page gets names B. It must not be drawn, and it is positive word that the
// archive has changed: the page's own archive check has to start, or the page
// sits on a blank workspace for ever.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { session } from './lib/operate/session.svelte';
import { recovery } from './lib/recovery.svelte';
import { _reloadArchiveStoresForTests } from './lib/operate/archiveStores.svelte';
import { setBoundArchive } from './lib/api/archive-guard';

const HOME = '01a163a7-0f04-7f69-85e9-255664b58d11';
const OTHER = '01a1640f-815a-75e4-bae2-b9ef8d86f8c3';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const S1 = '01a1f83c-da20-751f-80b7-ec9a3f1f9301';

let answerState: () => void = () => {};
const daemon: Daemon = {
    version: {
        daemon: 'test',
        instance: 'i1',
        archive: { id: HOME, label: 'Home', ownership: 'legacy', default_logbook_uuid: A },
    },
    config: {
        setup_complete: true,
        logging_station: { station_callsign: '7Q5MLV' },
        default_logbook: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' },
        bridge: { enabled: false },
        ft8_enabled: false,
    },
    routes: {
        // Every read of the state is answered by the daemon as it is by then:
        // serving the OTHER archive. The first is held until the test lets it go.
        'GET /v1/operating-session': [
            {
                status: 200,
                body: {
                    archive_id: OTHER,
                    generation: 2,
                    state: 'open',
                    session: {
                        id: S1,
                        logbook_uuid: A,
                        opened_at: '2026-11-25T09:15:00Z',
                        sitting_started_at: '2026-11-25T09:15:00Z',
                        has_contacts: false,
                        contact_count: 0,
                        held_by_you: true,
                    },
                },
                hold: new Promise<void>((r) => (answerState = r)),
            },
            {
                status: 200,
                body: {
                    archive_id: OTHER,
                    generation: 2,
                    state: 'open',
                    session: {
                        id: S1,
                        logbook_uuid: A,
                        opened_at: '2026-11-25T09:15:00Z',
                        sitting_started_at: '2026-11-25T09:15:00Z',
                        has_contacts: false,
                        contact_count: 0,
                        held_by_you: true,
                    },
                },
            },
        ],
    },
    sent: [],
};

const identityReads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/version').length;
const rowReads = (): number =>
    daemon.sent.filter((s) => s.url === '/v1/operating-session/members').length;

describe('main.ts: the session state is answered for another archive than the one proven', () => {
    beforeAll(async () => {
        setBoundArchive(null);
        _reloadArchiveStoresForTests();
        installDaemon(daemon);
        _setReloadForTests(() => {});
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it('draws nothing of it and starts the archive check, with no contacts ever asked for', async () => {
        const before = identityReads();
        shellStream().emit('open'); // the first opening, on the daemon now serving OTHER
        answerState();

        await vi.waitFor(() => expect(identityReads()).toBeGreaterThan(before));
        expect(recovery.status).toBe('loading'); // nothing of the other archive was drawn
        expect(session.sessionId).toBeNull();
        expect(rowReads()).toBe(0);
    });
});
