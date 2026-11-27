// W-0029 part 6 at the PRODUCTION BOUNDARY (the clean-room review of
// d7bcf9e5): the real main.ts with only the transport stubbed. The operator
// presses Continue and it FAILS. The page reads what the daemon holds, with
// both choices disabled until that is known. The event socket is then
// REPLACED while that read is out, so its answer is dropped. The choices stay
// disabled, and nothing can be pressed, until the read the new connection
// sent has been drawn.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
import { nextSocket } from './test/fakeSocket';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { recovery } from './lib/recovery.svelte';
import { screen, fireEvent } from '@testing-library/svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const REMNANT = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';
const STATE = 'GET /v1/operating-session';
const TEST_LIMIT = 60_000;
const WAIT = { timeout: 10_000 };

// As the daemon answers: one caller can be told "open" and "recovery pending"
// for the same session at the same generation, by which lifetime is admitted
// when it is asked (internal/api/operating_session.go, sessionResponse).
const state = (name: 'recovery_pending' | 'open', generation = 4) => ({
    archive_id: ARCHIVE,
    generation,
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
const settle = async (): Promise<void> => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
};
// The event socket fails and its replacement opens.
const reconnect = async (): Promise<void> => {
    const dropped = shellStream();
    dropped.emit('error');
    (await nextSocket(dropped, (fn) => vi.waitFor(fn, WAIT))).emit('open');
};

const CONTINUE = 'POST /v1/operating-session/continue';
const continues = (): number =>
    daemon.sent.filter((s) => s.url === '/v1/operating-session/continue').length;

describe('main.ts: Continue fails, and the event socket is replaced while the page finds out what it did', () => {
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
        "both choices stay disabled until the new connection's read is drawn, and Continue was sent once",
        async () => {
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            const atOpen = reads();
            shellStream().emit('open');
            await vi.waitFor(() => expect(reads()).toBe(atOpen + 1), WAIT);
            await settle();
            expect(recovery.status).toBe('question');

            // Continue fails. The page reads what the daemon holds; that read is held.
            daemon.routes[CONTINUE] = {
                status: 503,
                body: { code: 'unavailable', message: 'The daemon is busy.' },
            };
            let answerFinding!: () => void;
            let answerFresh!: () => void;
            daemon.routes[STATE] = [
                {
                    status: 200,
                    body: state('recovery_pending'),
                    hold: new Promise<void>((r) => (answerFinding = r)),
                },
                {
                    status: 200,
                    body: state('recovery_pending'),
                    hold: new Promise<void>((r) => (answerFresh = r)),
                },
            ];
            const before = reads();
            await fireEvent.click(screen.getByRole('button', { name: 'Continue this session' }));
            await vi.waitFor(() => expect(reads()).toBe(before + 1), WAIT);

            // The socket is replaced, and the page reads afresh; that read is held too.
            await reconnect();
            await vi.waitFor(() => expect(reads()).toBe(before + 2), WAIT);

            // The earlier read answers: it is of the connection that is gone.
            answerFinding();
            await settle();
            await settle();
            expect(asked()).not.toBeNull();
            const cont = screen.getByRole('button', { name: 'Continue this session' });
            expect(cont).toBeDisabled(); // what the answer did is still not known
            expect(screen.getByRole('button', { name: 'Start a new session' })).toBeDisabled();
            await fireEvent.click(cont);
            expect(continues()).toBe(1);

            // The new connection's read is drawn: the choices are back.
            answerFresh();
            await vi.waitFor(
                () =>
                    expect(
                        screen.getByRole('button', { name: 'Continue this session' })
                    ).toBeEnabled(),
                WAIT
            );
            expect(screen.getByRole('button', { name: 'Start a new session' })).toBeEnabled();
            expect(continues()).toBe(1);
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
        },
        TEST_LIMIT
    );
});
