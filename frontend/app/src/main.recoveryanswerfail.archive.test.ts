// W-0029 part 6 at the PRODUCTION BOUNDARY (Codex's review, codex-285): the
// real main.ts with only the transport stubbed. The page shows archive A's
// recovery question and the operator presses Continue. Before that is
// answered the event socket opens, for the first time, on a daemon that now
// serves archive B: that is not a reconnection, so the page's reconnection
// check does not run. The opening's read of the state is slow. Continue then
// FAILS, and so does the read sent to find out what it did.
//
// The opening's read was sent before the answer failed, so its state is not
// drawn. But it names archive B, and that is positive word that the daemon
// serves another archive: it must still start the page's archive check, which
// confirms B and reloads. Otherwise the page sits on "Try again", bound to A.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { recovery } from './lib/recovery.svelte';
import { session } from './lib/operate/session.svelte';
import { screen, fireEvent } from '@testing-library/svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const OTHER = '01a1640f-815a-75e4-bae2-b9ef8d86f8c3';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const REMNANT = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';
const STATE = 'GET /v1/operating-session';
const TEST_LIMIT = 60_000;
const WAIT = { timeout: 10_000 };

// As the daemon answers: one caller can be told "open" and "recovery pending"
// for the same session at the same generation, by which lifetime is admitted
// when it is asked (internal/api/operating_session.go, sessionResponse).
const state = (name: 'recovery_pending' | 'open', generation = 4, contacts = 3) => ({
    archive_id: ARCHIVE,
    generation,
    state: name,
    session: {
        id: REMNANT,
        logbook_uuid: A,
        opened_at: '2026-11-20T09:15:00Z',
        sitting_started_at: '2026-11-20T09:15:00Z',
        has_contacts: true,
        contact_count: contacts,
        held_by_you: false,
    },
});

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
    // The load finds recovery pending: the question.
    routes: { [STATE]: { status: 200, body: state('recovery_pending') } },
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
const CONTINUE = 'POST /v1/operating-session/continue';
const continues = (): number =>
    daemon.sent.filter((s) => s.url === '/v1/operating-session/continue').length;

const news = (): number => daemon.sent.filter((s) => s.url === '/v1/operating-session/new').length;
const identityReads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/version').length;
const reload = vi.fn();
const button = (name: string): HTMLElement => screen.getByRole('button', { name });
const FAILS = { status: 503, body: { code: 'unavailable', message: 'The daemon is busy.' } };

describe('main.ts: Continue fails, and a read sent before it is answered for another archive', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(reload);
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
        'starts the archive check, which reloads; B is not drawn and no choice is enabled',
        async () => {
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            expect(recovery.status).toBe('question');

            let answerContinue!: () => void;
            let answerOpening!: () => void;
            daemon.routes[CONTINUE] = {
                ...FAILS,
                hold: new Promise<void>((r) => (answerContinue = r)),
            };
            daemon.routes[STATE] = [
                // The opening's read: the daemon serves B by then.
                {
                    status: 200,
                    body: { ...state('open', 2), archive_id: OTHER },
                    hold: new Promise<void>((r) => (answerOpening = r)),
                },
                // The read that finds out what Continue did: it fails.
                FAILS,
            ];

            // Continue is pressed, and is out.
            const before = reads();
            await fireEvent.click(button('Continue this session'));
            await vi.waitFor(() => expect(continues()).toBe(1), WAIT);

            // The daemon comes to serve B, and the socket opens for the first time.
            daemon.version = {
                daemon: 'test',
                instance: 'i2',
                archive: { id: OTHER, label: 'Away', ownership: 'legacy', default_logbook_uuid: A },
            };
            shellStream().emit('open');
            await vi.waitFor(() => expect(reads()).toBe(before + 1), WAIT);

            // Continue fails, and so does the read that finds out.
            const identityBefore = identityReads();
            answerContinue();
            expect(
                await screen.findByRole('heading', { name: 'The session could not be read' })
            ).toBeInTheDocument();
            expect(reads()).toBe(before + 2);
            expect(identityReads()).toBe(identityBefore);
            expect(reload).not.toHaveBeenCalled();

            // The opening's read answers, for B.
            answerOpening();
            await vi.waitFor(() => expect(identityReads()).toBeGreaterThan(identityBefore), WAIT);
            await vi.waitFor(() => expect(reload).toHaveBeenCalledOnce(), WAIT);

            // B's session was not drawn, and nothing more was sent.
            expect(session.sessionId).toBeNull();
            expect(workspace()).toBeNull();
            expect(screen.queryByRole('button', { name: 'Continue this session' })).toBeNull();
            expect(screen.queryByRole('button', { name: 'Start a new session' })).toBeNull();
            expect(continues()).toBe(1);
            expect(news()).toBe(0);
            expect(ft8()).toBe(0);
        },
        TEST_LIMIT
    );
});
