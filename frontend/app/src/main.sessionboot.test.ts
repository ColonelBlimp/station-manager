// W-0029 part 6 at the PRODUCTION BOUNDARY: the page before its event stream has
// opened for the first time (Codex's review, codex-275).
//
// A page reads the session and its contacts over HTTP as soon as it has proven
// its archive, and the panel shows them. But until the event stream is open
// nothing can tell the page of a contact stored a moment later, so those rows
// are not yet known to be all of the session: export and email are held, as
// after a dropped stream. The first opening reads the state and the rows again,
// and only then are they released.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { session } from './lib/operate/session.svelte';
import { _reloadArchiveStoresForTests } from './lib/operate/archiveStores.svelte';
import { setBoundArchive } from './lib/api/archive-guard';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const S1 = '01a1f83c-da20-751f-80b7-ec9a3f1f9301';
const uuid = (n: number): string => `01a21282-f185-7377-a3eb-cf669a08891${n}`;

const member = (n: number, call: string): unknown => ({
    uuid: uuid(n),
    revision: 0,
    call,
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
});
const members = (rows: unknown[]): unknown => ({
    archive_id: ARCHIVE,
    generation: 4,
    session_id: S1,
    members: rows,
});

const STATE = 'GET /v1/operating-session';
const ROWS = 'GET /v1/operating-session/members';

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
    routes: {
        [STATE]: {
            status: 200,
            body: {
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
            },
        },
        [ROWS]: { status: 200, body: members([member(1, 'G4FIRST')]) },
    },
    sent: [],
};

const calls = (): string[] => session.qsos.map((q) => q.callsign);
const stateReads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;

describe('main.ts: before the event stream has opened for the first time', () => {
    beforeAll(async () => {
        setBoundArchive(null);
        _reloadArchiveStoresForTests();
        installDaemon(daemon);
        _setReloadForTests(() => {});
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
        // The stream's socket exists and is still connecting: no 'open' yet.
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it('shows the rows it has read, and holds export and email until the stream is open', async () => {
        await vi.waitFor(() => expect(calls()).toEqual(['G4FIRST']));
        expect(session.rows).toBe('ok');
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('stream');

        // A second contact is stored. Nothing can tell the page.
        daemon.routes[ROWS] = {
            status: 200,
            body: members([member(2, 'K2SECOND'), member(1, 'G4FIRST')]),
        };
        await Promise.resolve();
        expect(calls()).toEqual(['G4FIRST']);
        expect(session.behind).toBe(true);

        // The stream opens: the state is read once, the rows are read again,
        // and only then is the hold released.
        const before = stateReads();
        shellStream().emit('open');
        await vi.waitFor(() => expect(session.behind).toBe(false));
        expect(calls()).toEqual(['K2SECOND', 'G4FIRST']);
        expect(stateReads()).toBe(before + 1);
    });
});
