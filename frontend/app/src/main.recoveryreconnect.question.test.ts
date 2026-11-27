// W-0029 part 6 at the PRODUCTION BOUNDARY (Codex's review, codex-281): the
// real main.ts with only the transport stubbed. The page was last in FT8 and
// is reopened on /operate/ft8, the view that claims FT8 and opens its stream.
//
// A read of the state answered "open" before the event socket dropped must not
// open the workspace after its replacement has opened: the daemon may say
// "recovery pending" to this connection, and the read that says so is still
// out. Nothing sent before an opening is drawn after it.
//
// Here, from the question: an earlier "open" does not dismiss it. The fresh
// read says open: the workspace, and only then FT8. (From a withheld
// workspace, with a fresh "recovery pending": main.recoveryreconnect.test.ts.)
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

describe('main.ts: a state answer from before the event socket was replaced', () => {
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
        'with the question showing: an earlier "open" does not dismiss it; a fresh "open" shows the workspace, and FT8 only then',
        async () => {
            await vi.waitFor(() => expect(asked()).not.toBeNull(), WAIT);
            // The event socket opens, and its read finds the same: the question.
            const atOpen = reads();
            shellStream().emit('open');
            await vi.waitFor(() => expect(reads()).toBe(atOpen + 1), WAIT);
            await settle();
            expect(recovery.status).toBe('question');
            let answerOld!: () => void;
            let answerNew!: () => void;
            daemon.routes[STATE] = [
                {
                    status: 200,
                    body: state('open'),
                    hold: new Promise<void>((r) => (answerOld = r)),
                },
                {
                    status: 200,
                    body: state('open'),
                    hold: new Promise<void>((r) => (answerNew = r)),
                },
            ];
            const before = reads();
            shellStream().emit(
                'operating_session.changed',
                JSON.stringify({ archive_id: ARCHIVE, generation: 4 })
            );
            await vi.waitFor(() => expect(reads()).toBe(before + 1), WAIT); // served "open", held

            await reconnect();
            await vi.waitFor(() => expect(reads()).toBe(before + 2), WAIT); // the fresh read, held

            answerOld();
            await settle();
            await settle();
            expect(asked()).not.toBeNull(); // the question stands
            expect(workspace()).toBeNull();
            expect(ft8()).toBe(0);

            answerNew();
            await vi.waitFor(() => expect(workspace()).not.toBeNull(), WAIT);
            expect(asked()).toBeNull();
            await vi.waitFor(() => expect(ft8()).toBeGreaterThan(0), WAIT);
        },
        TEST_LIMIT
    );
});
