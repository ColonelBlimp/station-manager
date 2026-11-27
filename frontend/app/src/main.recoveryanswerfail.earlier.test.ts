// W-0029 part 6 at the PRODUCTION BOUNDARY (Codex's review, codex-284): the
// real main.ts with only the transport stubbed. A read of the state is out,
// sent for an event, when the operator presses Continue and it FAILS. The page
// reads what the daemon holds. The EARLIER read then answers first, with the
// same question. It was sent before the answer failed, so it cannot say what
// the answer did: both choices stay disabled, and pressing sends nothing,
// until the read sent after the failure has been drawn.
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
const settle = async (): Promise<void> => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
};
const CONTINUE = 'POST /v1/operating-session/continue';
const continues = (): number =>
    daemon.sent.filter((s) => s.url === '/v1/operating-session/continue').length;

const NEW = 'POST /v1/operating-session/new';
const news = (): number => daemon.sent.filter((s) => s.url === '/v1/operating-session/new').length;
const changed = (): void =>
    shellStream().emit(
        'operating_session.changed',
        JSON.stringify({ archive_id: ARCHIVE, generation: 4 })
    );
const button = (name: string): HTMLElement => screen.getByRole('button', { name });
const FAILS = { status: 503, body: { code: 'unavailable', message: 'The daemon is busy.' } };

describe('main.ts: Continue fails while a read sent before it is still out', () => {
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
        'the earlier read enables neither choice and sends no second answer; the read sent after the failure does',
        async () => {
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            const atOpen = reads();
            shellStream().emit('open');
            await vi.waitFor(() => expect(reads()).toBe(atOpen + 1), WAIT);
            await settle();
            expect(recovery.status).toBe('question');

            let answerEarlier!: () => void;
            let answerFinding!: () => void;
            daemon.routes[STATE] = [
                {
                    status: 200,
                    body: state('recovery_pending', 4, 7),
                    hold: new Promise<void>((r) => (answerEarlier = r)),
                },
                {
                    status: 200,
                    body: state('recovery_pending', 4, 5),
                    hold: new Promise<void>((r) => (answerFinding = r)),
                },
                { status: 200, body: state('recovery_pending', 4, 5) },
            ];
            daemon.routes[CONTINUE] = FAILS;
            daemon.routes[NEW] = FAILS;

            // A read is sent for an event, and held.
            const before = reads();
            changed();
            await vi.waitFor(() => expect(reads()).toBe(before + 1), WAIT);

            // Continue fails; the page reads what the daemon holds, and that is held too.
            await fireEvent.click(button('Continue this session'));
            await vi.waitFor(() => expect(reads()).toBe(before + 2), WAIT);
            expect(continues()).toBe(1);

            // The read from before the failure answers.
            answerEarlier();
            await settle();
            await settle();
            expect(asked()).not.toBeNull();
            expect(button('Continue this session')).toBeDisabled();
            expect(button('Start a new session')).toBeDisabled();
            await fireEvent.click(button('Start a new session'));
            await settle();
            expect(news()).toBe(0);
            expect(continues()).toBe(1);
            expect(document.body.textContent).not.toContain('7 contacts'); // it was not drawn

            // The read sent after the failure is drawn: the choices are back.
            answerFinding();
            await vi.waitFor(() => expect(button('Continue this session')).toBeEnabled(), WAIT);
            expect(button('Start a new session')).toBeEnabled();
            expect(document.body.textContent?.replace(/\s+/g, ' ')).toContain('has 5 contacts.');
            expect(screen.getByRole('alert')).toHaveTextContent('The daemon is busy.');
            expect(reads()).toBe(before + 2);
            expect(news()).toBe(0);
            expect(continues()).toBe(1);
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
        },
        TEST_LIMIT
    );
});
