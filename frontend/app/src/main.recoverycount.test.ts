// W-0029 part 6 at the PRODUCTION BOUNDARY (the operator's ruling, 2026-11-27,
// on codex-280's last point): the real main.ts with only the transport
// stubbed. The recovery question is showing with one contact. That contact is
// deleted, and the read the page then owes for the count FAILS. The question
// says what went wrong on the line it already has for that, with the read's
// own message; the count shown is the last one received; Continue and New
// stay available; and the read is not repeated. The next read of the state
// that is drawn brings the count and clears the line.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { screen } from '@testing-library/svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const REMNANT = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';
const STATE = 'GET /v1/operating-session';
const TEST_LIMIT = 60_000;
const WAIT = { timeout: 10_000 };

// As the daemon answers: `has_contacts` stays true when the last contact is
// deleted (a deleted contact's membership is kept).
const state = (contacts: number) => ({
    archive_id: ARCHIVE,
    generation: 4,
    state: 'recovery_pending',
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
    routes: { [STATE]: { status: 200, body: state(1) } },
    sent: [],
};

const reads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;
const ft8 = (): number =>
    daemon.sent.filter((s) => s.url.startsWith('/v1/ft8')).length +
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8')).length;
const workspace = (): Element | null => document.querySelector('.content-wrap');
const text = (): string => document.body.textContent?.replace(/\s+/g, ' ') ?? '';
const asked = (): HTMLElement | null =>
    screen.queryByRole('heading', { name: 'The last session is still open' });
const settle = async (): Promise<void> => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
};

describe("main.ts: the read owed for the recovery question's count fails", () => {
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
        "the question says so with the read's own message, keeps both choices, and the next read clears it",
        async () => {
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            const atOpen = reads();
            shellStream().emit('open');
            await vi.waitFor(() => expect(reads()).toBe(atOpen + 1), WAIT);
            await settle();
            expect(text()).toContain('has 1 contact.');
            expect(screen.queryByRole('alert')).toBeNull();

            // The contact is deleted, and the daemon cannot answer the read for it.
            daemon.routes[STATE] = {
                status: 503,
                body: { code: 'unavailable', message: 'The daemon is busy.' },
            };
            const before = reads();
            shellStream().emit(
                'qso.deleted',
                JSON.stringify({ qso_uuid: '01a1f83c-da20-751f-80b7-ec9a3f1f9601', logbook_id: 1 })
            );
            await vi.waitFor(() => expect(reads()).toBe(before + 1), WAIT);
            const alert = await screen.findByRole('alert');
            expect(alert.textContent).toBe('The daemon is busy.');
            expect(asked()).not.toBeNull(); // still the question
            expect(text()).toContain('has 1 contact.'); // the last count received
            expect(screen.getByRole('button', { name: 'Continue this session' })).toBeEnabled();
            expect(screen.getByRole('button', { name: 'Start a new session' })).toBeEnabled();
            expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
            await settle();
            expect(reads()).toBe(before + 1); // not repeated
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);
            expect(daemon.sent.filter((s) => s.method !== 'GET')).toEqual([]);

            // The next read of the state that is drawn: the count, and no alert.
            daemon.routes[STATE] = { status: 200, body: state(0) };
            shellStream().emit(
                'operating_session.changed',
                JSON.stringify({ archive_id: ARCHIVE, generation: 4 })
            );
            await vi.waitFor(() => expect(text()).toContain('has 0 contacts.'), WAIT);
            expect(screen.queryByRole('alert')).toBeNull();
            expect(asked()).not.toBeNull();
        },
        TEST_LIMIT
    );
});
