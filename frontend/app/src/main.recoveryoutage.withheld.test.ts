// W-0029 part 6 at the PRODUCTION BOUNDARY (Codex's review, codex-282, first
// finding): the real main.ts with only the transport stubbed, reopened on its
// FT8 view. Nothing has been shown yet: the load's reads of the state are out,
// served "open". The event socket drops, and those answers arrive while its
// replacement is still CONNECTING. They must not open the workspace. With no
// read left out, the page says the session could not be read (and offers "Try
// again") rather than stay blank; the replacement's opening then reads afresh.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
import { nextSocket } from './test/fakeSocket';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { recovery } from './lib/recovery.svelte';
import { screen } from '@testing-library/svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const REMNANT = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';
const STATE = 'GET /v1/operating-session';
const TEST_LIMIT = 60_000;
const WAIT = { timeout: 10_000 };

// As the daemon answers: one caller can be told "open" and "recovery pending"
// for the same session at the same generation, by which lifetime is admitted
// when it is asked (internal/api/operating_session.go, sessionResponse).
const state = (name: 'recovery_pending' | 'open') => ({
    archive_id: ARCHIVE,
    generation: 4,
    state: name,
    session: {
        id: REMNANT,
        logbook_uuid: A,
        opened_at: '2026-11-20T09:15:00Z',
        sitting_started_at: '2026-11-20T09:15:00Z',
        has_contacts: true,
        contact_count: 3,
        held_by_you: false,
    },
});

let answerEarlier!: () => void;
let answerFresh!: () => void;
const earlier = new Promise<void>((r) => (answerEarlier = r));
const fresh = new Promise<void>((r) => (answerFresh = r));

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
        ft8_enabled: true,
        ft8_running: true,
    },
    routes: {
        [STATE]: [
            // The load's read and the first opening's read: served "open", slow to return.
            { status: 200, body: state('open'), hold: earlier },
            { status: 200, body: state('open'), hold: earlier },
            // The read the replacement socket's opening sends.
            { status: 200, body: state('recovery_pending'), hold: fresh },
        ],
    },
    sent: [],
};

const reads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;
const ft8 = (): number =>
    daemon.sent.filter((s) => s.url.startsWith('/v1/ft8')).length +
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8')).length;
const workspace = (): Element | null => document.querySelector('.content-wrap');
const asked = (): HTMLElement | null =>
    screen.queryByRole('heading', { name: 'The last session is still open' });

describe("main.ts: the load's state answers arrive after the event socket dropped", () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => {});
        window.localStorage.setItem('sm-op-mode', 'ft8');
        window.history.replaceState({}, '', '/operate/ft8');
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false), WAIT);
    }, TEST_LIMIT);
    afterAll(() => {
        vi.unstubAllGlobals();
        window.localStorage.removeItem('sm-op-mode');
    });

    it(
        "with the workspace withheld: no workspace and no FT8 during the outage; the reopening's read asks the question",
        async () => {
            await vi.waitFor(() => expect(reads()).toBe(1), WAIT); // the load's read, held
            shellStream().emit('open');
            await vi.waitFor(() => expect(reads()).toBe(2), WAIT); // the first opening's, held
            expect(recovery.status).toBe('loading');

            // The socket drops. Its replacement is connecting, not open.
            const dropped = shellStream();
            dropped.emit('error');
            answerEarlier();
            expect(
                await screen.findByRole('heading', { name: 'The session could not be read' })
            ).toBeInTheDocument();
            expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
            expect(workspace()).toBeNull();
            expect(asked()).toBeNull();
            expect(ft8()).toBe(0); // no claim, no FT8 stream
            expect(daemon.sent.filter((s) => s.method !== 'GET')).toEqual([]);
            expect(reads()).toBe(2);

            // The replacement opens: the fresh read, and then the question.
            (await nextSocket(dropped, (fn) => vi.waitFor(fn, WAIT))).emit('open');
            await vi.waitFor(() => expect(reads()).toBe(3), WAIT);
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
            answerFresh();
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
            expect(reads()).toBe(3);
        },
        TEST_LIMIT
    );
});
