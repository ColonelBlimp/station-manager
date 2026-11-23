// W-0029 part 4 at the PRODUCTION BOUNDARY (criterion E2; operator ruling
// 2026-11-23): the real main.ts on a daemon that cannot give the session's
// state. The page does not show the workspace as if there were nothing to ask:
// it says the session could not be read and offers to try again. It opens no
// FT8 stream, though FT8 is the view it would restore. A retry that succeeds
// shows the workspace.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { installDaemon, FakeEventSource, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';

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
        'GET /v1/operating-session': {
            status: 503,
            body: {
                code: 'operating_session_unavailable',
                message: 'the operating session could not be read',
            },
        },
    },
    sent: [],
};

const workspace = (): Element | null => document.querySelector('.content-wrap');
const ft8 = (): number =>
    daemon.sent.filter((s) => s.url.startsWith('/v1/ft8')).length +
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8')).length;

describe('main.ts: the session cannot be read at the load', () => {
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

    it('says so instead of showing the workspace, and Try again shows what the next read calls for', async () => {
        expect(
            await screen.findByRole('heading', { name: 'The session could not be read' })
        ).toBeInTheDocument();
        expect(screen.getByRole('alert')).toHaveTextContent(
            'the operating session could not be read'
        );
        expect(workspace()).toBeNull();
        expect(
            screen.queryByRole('heading', { name: 'The last session is still open' })
        ).toBeNull();
        expect(ft8()).toBe(0);

        // Still failing: the same screen.
        await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
        await vi.waitFor(() =>
            expect(screen.getByRole('button', { name: 'Try again' })).not.toBeDisabled()
        );
        expect(workspace()).toBeNull();
        expect(ft8()).toBe(0);

        // The daemon can read it again: an ordinary start, so the workspace.
        delete daemon.routes['GET /v1/operating-session'];
        await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
        await vi.waitFor(() => expect(workspace()).not.toBeNull());
        expect(screen.queryByRole('heading', { name: 'The session could not be read' })).toBeNull();
    });
});
