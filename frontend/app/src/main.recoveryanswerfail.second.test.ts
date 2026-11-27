// W-0029 part 6 at the PRODUCTION BOUNDARY (Codex's review, codex-284): the
// real main.ts with only the transport stubbed. The operator presses Continue
// and it FAILS; the read the page sends to find out is slow. A read sent AFTER
// the failure answers with the question, so the choices come back, and the
// operator presses New. While New is out, the first answer's slow read ends.
// That must not enable the choices: a second answer is out, and pressing
// Continue would send a third.
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

describe("main.ts: a second answer is out when the failed first answer's read ends", () => {
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
        'both choices stay disabled while New is out, no third answer is sent, and New then opens the workspace',
        async () => {
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            const atOpen = reads();
            shellStream().emit('open');
            await vi.waitFor(() => expect(reads()).toBe(atOpen + 1), WAIT);
            await settle();
            expect(recovery.status).toBe('question');

            let answerFinding!: () => void;
            let answerNew!: () => void;
            daemon.routes[STATE] = [
                {
                    status: 200,
                    body: state('recovery_pending'),
                    hold: new Promise<void>((r) => (answerFinding = r)),
                },
                { status: 200, body: state('recovery_pending') },
            ];
            daemon.routes[CONTINUE] = FAILS;
            daemon.routes[NEW] = {
                status: 200,
                body: { archive_id: ARCHIVE, generation: 5, state: 'none', session: null },
                hold: new Promise<void>((r) => (answerNew = r)),
            };

            // Continue fails; the read that finds out is held.
            const before = reads();
            await fireEvent.click(button('Continue this session'));
            await vi.waitFor(() => expect(reads()).toBe(before + 1), WAIT);
            expect(button('Start a new session')).toBeDisabled();

            // A read sent after the failure answers: the question stands, the choices are back.
            changed();
            await vi.waitFor(() => expect(button('Start a new session')).toBeEnabled(), WAIT);
            expect(reads()).toBe(before + 2);

            // New is pressed, and is out.
            await fireEvent.click(button('Start a new session'));
            await vi.waitFor(() => expect(news()).toBe(1), WAIT);
            expect(button('Continue this session')).toBeDisabled();

            // The first answer's read ends.
            answerFinding();
            await settle();
            await settle();
            expect(asked()).not.toBeNull();
            expect(button('Continue this session')).toBeDisabled();
            expect(button('Start a new session')).toBeDisabled();
            await fireEvent.click(button('Continue this session'));
            await settle();
            expect(continues()).toBe(1);
            expect(news()).toBe(1);
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0); // the question still stands: no presence in FT8

            // New is applied: the workspace.
            answerNew();
            await vi.waitFor(() => expect(workspace()).not.toBeNull(), WAIT);
            expect(asked()).toBeNull();
            expect(continues()).toBe(1);
            expect(news()).toBe(1);
        },
        TEST_LIMIT
    );
});
