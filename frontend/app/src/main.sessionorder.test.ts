// W-0029 part 6 at the PRODUCTION BOUNDARY: a start in which the event stream
// opens before the archive is proven (the clean-room review of 9b8c6260).
//
// The state read made at that opening was sent with no archive proven, so the
// recovery gate refuses its answer. The load's own read, made at the proof,
// draws the session, and its contacts are read. If the first read's answer
// comes last, every read the page needs has in fact answered; export and email
// must end up free, not held as if a read had failed.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { session } from './lib/operate/session.svelte';
import { _reloadArchiveStoresForTests } from './lib/operate/archiveStores.svelte';
import { boundArchive, setBoundArchive } from './lib/api/archive-guard';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const S1 = '01a1f83c-da20-751f-80b7-ec9a3f1f9301';

const state = {
    archive_id: ARCHIVE,
    generation: 4,
    state: 'open',
    session: {
        id: S1,
        logbook_uuid: A,
        opened_at: '2026-11-25T09:15:00Z',
        sitting_started_at: '2026-11-25T09:15:00Z',
        has_contacts: true,
        contact_count: 1,
        held_by_you: true,
    },
};

let proveArchive: () => void = () => {};
let answerFirstState: () => void = () => {};
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
        ft8_enabled: false,
    },
    // The boot's bracket of identity reads closes only once the config is in:
    // holding it holds the proof.
    configHold: new Promise<void>((r) => (proveArchive = r)),
    routes: {
        'GET /v1/operating-session': [
            // The read made when the stream opens, before the proof: answered last.
            { status: 200, body: state, hold: new Promise<void>((r) => (answerFirstState = r)) },
            { status: 200, body: state },
        ],
        'GET /v1/operating-session/members': {
            status: 200,
            body: {
                archive_id: ARCHIVE,
                generation: 4,
                session_id: S1,
                members: [
                    {
                        uuid: '01a21282-f185-7377-a3eb-cf669a088911',
                        revision: 0,
                        call: 'G4FIRST',
                        qso_date: '20261125',
                        time_on: '143000',
                        band: '20m',
                        mode: 'SSB',
                        rst_sent: '59',
                        rst_rcvd: '59',
                        name: '',
                        country: '',
                        comment: '',
                        emailed: false,
                    },
                ],
            },
        },
    },
    sent: [],
};

// Generous: these wait on the page's own work, under whatever load the suite
// runs. The test's own limit is raised with them (below): a wait longer than
// the test may run is no wait at all.
const WAIT = { timeout: 10_000 };
const TEST_LIMIT = 60_000;
const stateReads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;

describe('main.ts: the event stream opens before the archive is proven', () => {
    beforeAll(() => {
        setBoundArchive(null);
        _reloadArchiveStoresForTests();
        installDaemon(daemon);
        _setReloadForTests(() => {});
        document.body.innerHTML = '<div id="app"></div>';
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it(
        'ends with export and email free, once every read has answered',
        async () => {
            // The page's module has run when this resolves: its streams are asked
            // for, and its boot is waiting on the config, so no archive is proven.
            // (Not a timed wait: loading the page takes as long as it takes.)
            await import('./main');
            expect(boundArchive()).toBeNull();
            shellStream().emit('open');
            await vi.waitFor(() => expect(stateReads()).toBe(1), WAIT);

            // The proof: the load's own read draws the session, and its contacts are read.
            proveArchive();
            await vi.waitFor(() => expect(archivesState.verifying).toBe(false), WAIT);
            await vi.waitFor(
                () => expect(session.qsos.map((q) => q.callsign)).toEqual(['G4FIRST']),
                WAIT
            );
            expect(session.behind).toBe(true); // the opening's own state check is still out

            // The first read is answered last, and is refused: it was sent before the proof.
            answerFirstState();

            await vi.waitFor(() => expect(session.behind).toBe(false), WAIT);
            expect(session.hold).toBe('');
        },
        TEST_LIMIT
    );
});
