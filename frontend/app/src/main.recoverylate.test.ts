// codex-228 at the PRODUCTION BOUNDARY: the real main.ts, with the session read
// that the shell's stream makes when it opens SENT before the boot has proven
// its archive and held by the daemon across that proof. It comes back after the
// proof, for the very archive proven, as an old recovery question, while the
// load's own read is still out. It was sent with nothing proven, so it draws
// nothing. The load's read then fails, and the page says the session could not
// be read: the old question is not left standing in its place.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen } from '@testing-library/svelte';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { boundArchive, setBoundArchive } from './lib/api/archive-guard';
import { recovery } from './lib/recovery.svelte';

const PROVEN = '01a163a7-0f04-7f69-85e9-255664b58d22';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';

const gate = (): { release: () => void; held: Promise<void> } => {
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    return { release, held };
};
const early = gate(); // the stream's read, sent before the proof
const fresh = gate(); // the load's read, sent at the proof
const boot = gate(); // the boot's bracket, held at its config read

const daemon: Daemon = {
    version: {
        daemon: 'test',
        instance: 'i2',
        archive: { id: PROVEN, label: 'Contest', ownership: 'managed', default_logbook_uuid: A },
    },
    config: {
        setup_complete: true,
        logging_station: { station_callsign: '7Q5MLV' },
        default_logbook: { id: 1, uuid: A, name: 'Contest', callsign: '7Q5MLV' },
        bridge: { enabled: false },
        ft8_enabled: true,
        ft8_running: true,
    },
    configHold: boot.held,
    routes: {
        'GET /v1/operating-session': [
            {
                status: 200,
                hold: early.held,
                body: {
                    archive_id: PROVEN,
                    generation: 3,
                    state: 'recovery_pending',
                    session: {
                        id: '01a1f83c-da20-751f-80b7-ec9a3f1f9302',
                        logbook_uuid: A,
                        opened_at: '2026-11-20T09:15:00Z',
                        sitting_started_at: '2026-11-20T09:15:00Z',
                        has_contacts: true,
                        contact_count: 5,
                        held_by_you: false,
                    },
                },
            },
            {
                status: 503,
                hold: fresh.held,
                body: {
                    code: 'operating_session_unavailable',
                    message: 'the operating session could not be read',
                },
            },
        ],
    },
    sent: [],
};

const workspace = (): Element | null => document.querySelector('.content-wrap');
const asked = (): HTMLElement | null =>
    screen.queryByRole('heading', { name: 'The last session is still open' });
const ft8 = (): number =>
    daemon.sent.filter((s) => s.url.startsWith('/v1/ft8')).length +
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8')).length;
const reads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;

describe('main.ts: a session read sent before the proof comes back after it', () => {
    beforeAll(async () => {
        setBoundArchive(null); // a real page has proven nothing when it loads
        installDaemon(daemon);
        _setReloadForTests(() => {});
        window.localStorage.setItem('sm-op-mode', 'ft8');
        window.history.replaceState({}, '', '/operate/ft8');
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        // The stream opens with nothing proven, and the page sends its read.
        expect(boundArchive()).toBeNull();
        shellStream().emit('open');
        await vi.waitFor(() => expect(reads()).toBe(1));
        // The boot then proves its archive and sends the load's own read.
        boot.release();
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
        await vi.waitFor(() => expect(reads()).toBe(2));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
        window.localStorage.removeItem('sm-op-mode');
    });

    it("draws nothing from the earlier read, and says the session could not be read when the load's read fails", async () => {
        expect(boundArchive()).toBe(PROVEN);
        expect(recovery.status).toBe('loading');

        // The earlier read comes back first: an old question, for the archive now proven.
        early.release();
        await new Promise((r) => setTimeout(r, 20));
        expect(recovery.status).toBe('loading');
        expect(asked()).toBeNull();
        expect(workspace()).toBeNull();
        expect(ft8()).toBe(0);

        // The load's own read fails.
        fresh.release();
        expect(
            await screen.findByRole('heading', { name: 'The session could not be read' })
        ).toBeInTheDocument();
        expect(asked()).toBeNull();
        expect(workspace()).toBeNull();
        expect(ft8()).toBe(0);
    });
});
