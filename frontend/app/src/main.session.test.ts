// W-0029 part 6 at the PRODUCTION BOUNDARY (criteria A, B and D): the real
// main.ts on a daemon that holds an operating session. What the page shows of
// the Session is what the daemon ANSWERS, and nothing else.
//
// The nearest confusable outcome is a window that still draws rows of its own.
// So the daemon here holds a contact this window never logged, the tab's storage
// holds a row an earlier build left there, and the page must show the first and
// never the second. Every later change reaches the page as an event that
// carries no row: the row it shows afterwards is whatever the daemon then
// answers, which the test changes in between.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen } from '@testing-library/svelte';
import { installDaemon, shellStream, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { session } from './lib/operate/session.svelte';
import { _reloadArchiveStoresForTests } from './lib/operate/archiveStores.svelte';
import { setBoundArchive } from './lib/api/archive-guard';
import { nextSocket } from './test/fakeSocket';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const S1 = '01a1f83c-da20-751f-80b7-ec9a3f1f9301';
const S2 = '01a1f83c-da20-751f-80b7-ec9a3f1f9302';
const uuid = (n: number): string => `01a21282-f185-7377-a3eb-cf669a08891${n}`;

const state = (id: string, generation: number, sittingStartedAt: string): unknown => ({
    archive_id: ARCHIVE,
    generation,
    state: 'open',
    session: {
        id,
        logbook_uuid: A,
        opened_at: '2026-11-20T09:15:00Z',
        sitting_started_at: sittingStartedAt,
        has_contacts: true,
        contact_count: 1,
        held_by_you: true,
    },
});
const member = (n: number, call: string, over: Record<string, unknown> = {}): unknown => ({
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
    ...over,
});
const members = (id: string | null, generation: number, rows: unknown[]): unknown => ({
    archive_id: ARCHIVE,
    generation,
    session_id: id,
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
        [STATE]: { status: 200, body: state(S1, 4, '2026-11-25T09:15:00Z') },
        // A contact the daemon holds and this window never logged.
        [ROWS]: { status: 200, body: members(S1, 4, [member(1, 'G4DAEMON')]) },
    },
    sent: [],
};

const calls = (): string[] => session.qsos.map((q) => q.callsign);
const rowReads = (): number =>
    daemon.sent.filter((s) => s.url === '/v1/operating-session/members').length;
const stateReads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;
const event = (name: string, payload: Record<string, unknown>): void =>
    shellStream().emit(name, JSON.stringify(payload));

describe('main.ts: the Session is the daemon’s', () => {
    beforeAll(async () => {
        // A real page has proven nothing when it loads: the boot binds the archive.
        setBoundArchive(null);
        _reloadArchiveStoresForTests();
        installDaemon(daemon);
        _setReloadForTests(() => {});
        // What an earlier build left in this tab: a list for this archive, and
        // the clock's own start.
        window.sessionStorage.setItem(
            `sm.session.qsos.${ARCHIVE}`,
            JSON.stringify({ qsos: [{ id: 1, uuid: 'old', callsign: 'W1WINDOW', timeOn: '' }] })
        );
        window.sessionStorage.setItem('sm.session.startedAt', '1');
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
        shellStream().emit('open');
    });
    afterAll(() => {
        vi.unstubAllGlobals();
        window.sessionStorage.clear();
    });

    it('A1, B8: shows the daemon’s rows, and nothing an earlier build left in the tab', async () => {
        await vi.waitFor(() => expect(calls()).toEqual(['G4DAEMON']));
        expect(session.sessionId).toBe(S1);
        expect(window.sessionStorage.getItem(`sm.session.qsos.${ARCHIVE}`)).toBeNull();
        expect(window.sessionStorage.getItem('sm.session.startedAt')).toBeNull();
        expect(document.body.textContent).not.toContain('W1WINDOW');
    });

    it('D1: the header’s clock counts the daemon’s sitting', async () => {
        const clock = await screen.findByTestId('session-timer');
        // The sitting began on the daemon long before this window loaded.
        await vi.waitFor(() => expect(clock.textContent).toMatch(/^\d{2,}:\d{2}:\d{2}$/));
        const hours = Number(clock.textContent.split(':')[0]);
        expect(hours).toBeGreaterThan(0);
    });

    it('A3: a contact stored elsewhere appears when the daemon says it is a member', async () => {
        daemon.routes[ROWS] = {
            status: 200,
            body: members(S1, 4, [member(2, 'K2FT8'), member(1, 'G4DAEMON')]),
        };
        const before = rowReads();
        event('qso.stored', { qso_uuid: uuid(2), logbook_id: 1, operating_session_id: S1 });
        await vi.waitFor(() => expect(calls()).toEqual(['K2FT8', 'G4DAEMON']));
        expect(rowReads()).toBe(before + 1);
    });

    it('A2: a stored contact that joined no session asks nothing and adds nothing', async () => {
        const before = rowReads();
        event('qso.stored', { qso_uuid: uuid(9), logbook_id: 2 }); // an import, another logbook
        await Promise.resolve();
        await Promise.resolve();
        expect(rowReads()).toBe(before);
        expect(calls()).toEqual(['K2FT8', 'G4DAEMON']);
    });

    it('A4: an edit elsewhere shows, and a delete elsewhere takes the row away', async () => {
        daemon.routes[ROWS] = {
            status: 200,
            body: members(S1, 4, [
                member(2, 'K2FT8', { name: 'Edited elsewhere', emailed: true }),
                member(1, 'G4DAEMON'),
            ]),
        };
        event('qso.updated', { qso_uuid: uuid(2), logbook_id: 1 });
        await vi.waitFor(() => expect(session.qsos[0].name).toBe('Edited elsewhere'));
        expect(session.qsos[0].emailed).toBe(true); // an email stamp arrives the same way

        daemon.routes[ROWS] = { status: 200, body: members(S1, 4, [member(1, 'G4DAEMON')]) };
        event('qso.deleted', { qso_uuid: uuid(2), logbook_id: 1 });
        await vi.waitFor(() => expect(calls()).toEqual(['G4DAEMON']));
    });

    it('B6: when the stream reopens the state and the rows are read again', async () => {
        daemon.routes[ROWS] = {
            status: 200,
            body: members(S1, 4, [member(3, 'JA3DOWN'), member(1, 'G4DAEMON')]),
        };
        const rowsBefore = rowReads();
        const stateBefore = stateReads();
        const dropped = shellStream();
        dropped.emit('error');
        (await nextSocket(dropped, (fn) => vi.waitFor(fn))).emit('open');
        await vi.waitFor(() => expect(calls()).toEqual(['JA3DOWN', 'G4DAEMON']));
        expect(rowReads()).toBeGreaterThan(rowsBefore);
        expect(stateReads()).toBeGreaterThan(stateBefore);
    });

    it('B5, D3: after New the panel is the new session’s, and the clock starts again', async () => {
        const newSitting = new Date(Date.now() - 5000).toISOString();
        daemon.routes[STATE] = { status: 200, body: state(S2, 5, newSitting) };
        daemon.routes[ROWS] = { status: 200, body: members(S2, 5, []) };
        event('operating_session.changed', { archive_id: ARCHIVE, generation: 5 });
        await vi.waitFor(() => expect(session.sessionId).toBe(S2));
        expect(calls()).toEqual([]); // no row of the session before, even while its rows are read
        await vi.waitFor(() => expect(session.rows).toBe('ok'));
        expect(calls()).toEqual([]);
        const clock = screen.getByTestId('session-timer');
        await vi.waitFor(() => expect(clock.textContent).toMatch(/^00:00:\d{2}$/));
    });

    it('the contact that opens a session is found by reading the state', async () => {
        // Its qso.stored names a session the page does not show yet.
        const S3 = '01a1f83c-da20-751f-80b7-ec9a3f1f9303';
        daemon.routes[STATE] = { status: 200, body: state(S3, 6, new Date().toISOString()) };
        daemon.routes[ROWS] = { status: 200, body: members(S3, 6, [member(4, 'ZS4FIRST')]) };
        const before = stateReads();
        event('qso.stored', { qso_uuid: uuid(4), logbook_id: 1, operating_session_id: S3 });
        await vi.waitFor(() => expect(calls()).toEqual(['ZS4FIRST']));
        expect(stateReads()).toBe(before + 1);
        expect(session.sessionId).toBe(S3);
    });

    // Codex's review of the correction (codex-274), through the real wiring.
    it('is behind from operating_session.changed until the state and the rows agree', async () => {
        const S4 = '01a1f83c-da20-751f-80b7-ec9a3f1f9304';
        await vi.waitFor(() => expect(session.behind).toBe(false));
        let answerState: () => void = () => {};
        daemon.routes[STATE] = {
            status: 200,
            body: state(S4, 7, new Date().toISOString()),
            hold: new Promise<void>((r) => (answerState = r)),
        };
        daemon.routes[ROWS] = { status: 200, body: members(S4, 7, [member(5, 'VK5NEW')]) };

        event('operating_session.changed', { archive_id: ARCHIVE, generation: 7 });

        expect(session.behind).toBe(true); // at once: the rows on screen may be another session's
        expect(calls()).toEqual(['ZS4FIRST']); // they stay on screen, and are not offered for export
        answerState();
        await vi.waitFor(() => expect(session.behind).toBe(false));
        expect(session.sessionId).toBe(S4);
        expect(calls()).toEqual(['VK5NEW']);
    });

    it('is behind from the loss of the event stream until the read after it is back', async () => {
        const dropped = shellStream();
        dropped.emit('error');
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('stream');

        (await nextSocket(dropped, (fn) => vi.waitFor(fn))).emit('open');
        await vi.waitFor(() => expect(session.behind).toBe(false));
    });

    it('rows that name another archive start the page’s archive check', async () => {
        const OTHER = '01a1640f-815a-75e4-bae2-b9ef8d86f8c3';
        const identityReads = (): number =>
            daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/version').length;
        const before = identityReads();
        const current = session.sessionId;
        daemon.routes[ROWS] = {
            status: 200,
            body: { archive_id: OTHER, generation: 7, session_id: current, members: [] },
        };
        event('qso.updated', { qso_uuid: uuid(5), logbook_id: 1 });
        await vi.waitFor(() => expect(identityReads()).toBeGreaterThan(before));
        expect(calls()).toEqual(['VK5NEW']); // nothing of the other archive was drawn
    });
});
