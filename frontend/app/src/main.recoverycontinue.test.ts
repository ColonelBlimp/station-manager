// W-0029 part 6 at the PRODUCTION BOUNDARY (Codex's review, codex-282, second
// finding): the real main.ts with only the transport stubbed, reopened on its
// FT8 view. The operator presses Continue once; the event socket is replaced
// and the page reads afresh; Continue is then refused, with recovery pending
// at the next generation. The refusal was sent on the old connection, so its
// state is not drawn, and what it did is not known. The reconnect's read,
// already out, then answers "open" for that generation: it may have been
// served before the refusal, and must not open the workspace. Nothing is
// shown until a read sent after the refusal arrived has answered. That read
// fails: the page says the session could not be read, FT8 still not asked.
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

describe('main.ts: Continue is refused after the event socket was replaced', () => {
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
        'the reconnect read that was already out does not open the workspace; a failed deciding read says so; one Continue',
        async () => {
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            const atOpen = reads();
            shellStream().emit('open');
            await vi.waitFor(() => expect(reads()).toBe(atOpen + 1), WAIT);
            await settle();
            expect(recovery.status).toBe('question');

            // Continue, pressed once at generation 4 and held at the daemon.
            let answerContinue!: () => void;
            daemon.routes[CONTINUE] = {
                status: 409,
                body: {
                    code: 'operating_session_stale',
                    message: 'changed',
                    operating_session: state('recovery_pending', 5),
                },
                hold: new Promise<void>((r) => (answerContinue = r)),
            };
            await fireEvent.click(screen.getByRole('button', { name: 'Continue this session' }));
            expect(continues()).toBe(1);

            // The socket is replaced, and the page reads afresh; that read is held.
            let answerReconnect!: () => void;
            let answerDeciding!: () => void;
            daemon.routes[STATE] = [
                {
                    status: 200,
                    body: state('open', 5),
                    hold: new Promise<void>((r) => (answerReconnect = r)),
                },
                {
                    status: 503,
                    body: { code: 'unavailable', message: 'the session could not be read' },
                    hold: new Promise<void>((r) => (answerDeciding = r)),
                },
                { status: 200, body: state('recovery_pending', 5) },
            ];
            const before = reads();
            await reconnect();
            await vi.waitFor(() => expect(reads()).toBe(before + 1), WAIT);

            // Continue is refused: recovery pending at generation 5.
            answerContinue();
            await vi.waitFor(() => expect(recovery.status).toBe('loading'), WAIT);
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
            expect(reads()).toBe(before + 1); // nothing is asked while the reconnect read is out

            // The reconnect read answers "open" at 5. It was out when the refusal arrived.
            answerReconnect();
            await vi.waitFor(() => expect(reads()).toBe(before + 2), WAIT); // the read that decides
            await settle();
            expect(recovery.status).toBe('loading');
            expect(workspace()).toBeNull();
            expect(asked()).toBeNull();
            expect(ft8()).toBe(0); // no claim, no FT8 stream

            // The deciding read fails.
            answerDeciding();
            expect(
                await screen.findByRole('heading', { name: 'The session could not be read' })
            ).toBeInTheDocument();
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
            expect(reads()).toBe(before + 2); // not repeated

            // "Try again": recovery is pending at 5, and the question is asked.
            await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            expect(recovery.generation).toBe(5);
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
            expect(continues()).toBe(1); // the answer was sent once
        },
        TEST_LIMIT
    );
});
