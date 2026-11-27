// W-0029 part 6 at the PRODUCTION BOUNDARY (the clean-room review of
// d7bcf9e5): the real main.ts with only the transport stubbed. The operator
// presses Continue and it FAILS. The page reads what the daemon holds, with
// both choices disabled until that is known. The event socket then drops
// while that read is out, so its answer is dropped like any other from
// before. The choices must not come back on the old question: the page says
// the session could not be read, with what went wrong, and "Try again".
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
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

const CONTINUE = 'POST /v1/operating-session/continue';
const continues = (): number =>
    daemon.sent.filter((s) => s.url === '/v1/operating-session/continue').length;

describe('main.ts: Continue fails, and the event socket drops while the page finds out what it did', () => {
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
        'the old question does not come back with its choices: the session could not be read, and Continue was sent once',
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
            daemon.routes[STATE] = [
                {
                    status: 200,
                    body: state('recovery_pending'),
                    hold: new Promise<void>((r) => (answerFinding = r)),
                },
                { status: 200, body: state('recovery_pending') },
            ];
            const before = reads();
            await fireEvent.click(screen.getByRole('button', { name: 'Continue this session' }));
            await vi.waitFor(() => expect(reads()).toBe(before + 1), WAIT);
            expect(screen.getByRole('button', { name: 'Continue this session' })).toBeDisabled();
            expect(screen.getByRole('button', { name: 'Start a new session' })).toBeDisabled();

            // The socket drops; its replacement is connecting. The read answers.
            shellStream().emit('error');
            answerFinding();
            expect(
                await screen.findByRole('heading', { name: 'The session could not be read' })
            ).toBeInTheDocument();
            expect(screen.getByRole('alert').textContent).toBe('The daemon is busy.');
            expect(screen.queryByRole('button', { name: 'Continue this session' })).toBeNull();
            expect(screen.queryByRole('button', { name: 'Start a new session' })).toBeNull();
            expect(continues()).toBe(1);
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
            expect(reads()).toBe(before + 1);

            // "Try again": the daemon holds the question, and the choices are back.
            await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            expect(screen.getByRole('button', { name: 'Continue this session' })).toBeEnabled();
            expect(screen.getByRole('button', { name: 'Start a new session' })).toBeEnabled();
            expect(continues()).toBe(1);
        },
        TEST_LIMIT
    );
});
