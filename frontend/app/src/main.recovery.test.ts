// W-0029 part 4 at the PRODUCTION BOUNDARY (criteria E1, E5, E8): the real
// main.ts with only the transport stubbed, on a daemon whose archive holds a
// session with contacts left open by an earlier start. The page was last in
// FT8 and is reopened on /operate/ft8, the view that would open an FT8 event
// stream. What loads is the question: nothing of the workspace, no FT8 stream
// and no FT8 request. After Continue the workspace is shown, from the state the
// answer returned.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { recovery } from './lib/recovery.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const REMNANT = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';

const session = (heldByYou: boolean) => ({
    id: REMNANT,
    logbook_uuid: A,
    opened_at: '2026-11-20T09:15:00Z',
    sitting_started_at: '2026-11-20T09:15:00Z',
    has_contacts: true,
    contact_count: 12,
    held_by_you: heldByYou,
});
const pending = {
    archive_id: ARCHIVE,
    generation: 4,
    state: 'recovery_pending',
    session: session(false),
};
const continued = { archive_id: ARCHIVE, generation: 5, state: 'open', session: session(true) };

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
        'GET /v1/operating-session': { status: 200, body: pending },
        'POST /v1/operating-session/continue': { status: 200, body: continued },
    },
    sent: [],
};

const reads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;
const ft8Requests = (): string[] =>
    daemon.sent.map((s) => s.url).filter((u) => u.startsWith('/v1/ft8'));
const ft8Streams = (): FakeEventSource[] =>
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8'));
const workspace = (): Element | null => document.querySelector('.content-wrap');

describe('main.ts: a start that meets a session left open with contacts', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => {});
        window.localStorage.setItem('sm-op-mode', 'ft8');
        window.history.replaceState({}, '', '/operate/ft8');
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
        window.localStorage.removeItem('sm-op-mode');
    });

    // One sequence, as an operator would meet it.
    it('asks before the workspace, opens no FT8 stream, and shows the workspace after Continue', async () => {
        // The question is what loads, in the ruled words, with the count the daemon gave.
        expect(
            await screen.findByRole('heading', { name: 'The last session is still open' })
        ).toBeInTheDocument();
        expect(document.body.textContent?.replace(/\s+/g, ' ')).toContain(
            'The session that started 2026-11-20 09:15 UTC has 12 contacts and was never closed.'
        );
        expect(workspace()).toBeNull();
        expect(document.querySelector('nav')).toBeNull();
        expect(document.title).toBe('Session · Station Manager');
        // The view the page would restore is FT8: it is not mounted, so nothing
        // of FT8 was asked for and this app has no presence in it.
        expect(ft8Requests()).toEqual([]);
        expect(ft8Streams()).toEqual([]);

        // E8: the stream opening makes the page read again, and so does the event.
        const before = reads();
        shellStream().emit('open');
        await vi.waitFor(() => expect(reads()).toBe(before + 1));
        shellStream().emit(
            'operating_session.changed',
            JSON.stringify({ archive_id: ARCHIVE, generation: 4 })
        );
        await vi.waitFor(() => expect(reads()).toBe(before + 2));
        expect(recovery.status).toBe('question');
        expect(workspace()).toBeNull();

        // Continue: sent once, for the archive the page proved.
        await fireEvent.click(screen.getByRole('button', { name: 'Continue this session' }));
        const sent = daemon.sent.filter((s) => s.url === '/v1/operating-session/continue');
        expect(sent).toHaveLength(1);
        expect(sent[0]).toMatchObject({ method: 'POST', archive: ARCHIVE });

        // The workspace, from the state the answer returned: no further read was needed.
        const readsAtAnswer = reads();
        await vi.waitFor(() => expect(workspace()).not.toBeNull());
        expect(
            screen.queryByRole('heading', { name: 'The last session is still open' })
        ).toBeNull();
        expect(reads()).toBe(readsAtAnswer);
        // Only now is the FT8 view mounted.
        await vi.waitFor(() =>
            expect(ft8Requests().length + ft8Streams().length).toBeGreaterThan(0)
        );

        // A read from before the answer, arriving now, cannot put the question back.
        daemon.routes['GET /v1/operating-session'] = { status: 200, body: pending };
        shellStream().emit(
            'operating_session.changed',
            JSON.stringify({ archive_id: ARCHIVE, generation: 4 })
        );
        await vi.waitFor(() => expect(reads()).toBe(readsAtAnswer + 1));
        await Promise.resolve();
        expect(recovery.status).toBe('clear');
        expect(workspace()).not.toBeNull();
    });
});
