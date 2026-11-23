// codex-226, finding 1, at the PRODUCTION BOUNDARY: the real main.ts, with a
// session read made before the boot has proven its archive (the shell's stream
// opens first, and that makes the page read) answered for one archive ("open":
// nothing to ask), and the boot's bracket then proving ANOTHER, whose session
// needs its answer.
// That earlier answer must not stand as permission to show this archive's
// workspace: from the proof until this archive's own state is read, nothing of
// the workspace is shown and nothing of FT8 is asked for, though FT8 is the
// view the page would restore. Then the question.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen } from '@testing-library/svelte';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { boundArchive, setBoundArchive } from './lib/api/archive-guard';
import { recovery } from './lib/recovery.svelte';

const EARLIER = '01a163a7-0f04-7f69-85e9-255664b58d11';
const PROVEN = '01a163a7-0f04-7f69-85e9-255664b58d22';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';

const session = (id: string, heldByYou: boolean, contacts: number) => ({
    id,
    logbook_uuid: A,
    opened_at: '2026-11-20T09:15:00Z',
    sitting_started_at: '2026-11-20T09:15:00Z',
    has_contacts: contacts > 0,
    contact_count: contacts,
    held_by_you: heldByYou,
});

let release!: () => void;
const held = new Promise<void>((r) => (release = r));
// The boot's bracket is held at its config read until the early session read has
// been answered, so that read is certainly made with no archive proven.
let letBootGoOn!: () => void;
const bootHeld = new Promise<void>((r) => (letBootGoOn = r));

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
    configHold: bootHeld,
    routes: {
        'GET /v1/operating-session': [
            // The early read, answered while the daemon still served the earlier archive.
            {
                status: 200,
                body: {
                    archive_id: EARLIER,
                    generation: 9,
                    state: 'open',
                    session: session('01a1f83c-da20-751f-80b7-ec9a3f1f9301', true, 0),
                },
            },
            // The proven archive's own state: held by the test, then the question.
            {
                status: 200,
                hold: held,
                body: {
                    archive_id: PROVEN,
                    generation: 3,
                    state: 'recovery_pending',
                    session: session('01a1f83c-da20-751f-80b7-ec9a3f1f9302', false, 5),
                },
            },
        ],
    },
    sent: [],
};

const workspace = (): Element | null => document.querySelector('.content-wrap');
const ft8 = (): number =>
    daemon.sent.filter((s) => s.url.startsWith('/v1/ft8')).length +
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8')).length;
const reads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;

describe('main.ts: the early session read was for another archive than the one the boot proves', () => {
    beforeAll(async () => {
        setBoundArchive(null); // a real page has proven nothing when it loads
        installDaemon(daemon);
        _setReloadForTests(() => {});
        window.localStorage.setItem('sm-op-mode', 'ft8');
        window.history.replaceState({}, '', '/operate/ft8');
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        // Nothing is proven yet. The shell's stream opens, and the page reads:
        // the answer is the earlier archive's.
        expect(boundArchive()).toBeNull();
        shellStream().emit('open');
        await vi.waitFor(() => expect(reads()).toBe(1));
        await Promise.resolve();
        expect(recovery.status).toBe('loading');
        letBootGoOn();
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
        window.localStorage.removeItem('sm-op-mode');
    });

    it("withholds the workspace until the proven archive's own state is read, then asks", async () => {
        // The bracket has succeeded and the page is bound to the proven archive;
        // its own session read is out and unanswered.
        expect(boundArchive()).toBe(PROVEN);
        await vi.waitFor(() => expect(reads()).toBe(2));
        expect(recovery.status).toBe('loading');
        expect(workspace()).toBeNull();
        expect(
            screen.queryByRole('heading', { name: 'The last session is still open' })
        ).toBeNull();
        expect(ft8()).toBe(0);

        release();
        expect(
            await screen.findByRole('heading', { name: 'The last session is still open' })
        ).toBeInTheDocument();
        expect(document.body.textContent?.replace(/\s+/g, ' ')).toContain(
            'has 5 contacts and was never closed.'
        );
        expect(workspace()).toBeNull();
        expect(ft8()).toBe(0);
    });
});
