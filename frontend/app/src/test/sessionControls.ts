// Fixtures for the tests that drive the Session panel's controls through the
// REAL main.ts (W-0029 part 6, slice 3): the daemon's answers as it gives them
// on the wire, and counters over what the page sent.
import { expect, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { installDaemon, shellStream, type Daemon, type Route, type Sent } from './mainHarness';
import { nextSocket } from './fakeSocket';
import { _setReloadForTests, archivesState } from '../lib/config/archives.svelte';
import { _reloadArchiveStoresForTests } from '../lib/operate/archiveStores.svelte';
import { setBoundArchive } from '../lib/api/archive-guard';

export const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
export const OTHER_ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58dff';
export const LOGBOOK = '01a187bc-0173-7649-8ff3-71db8170c309';
/** The session open when the page loads. */
export const S_OPEN = '01a1f83c-da20-751f-80b7-ec9a3f1f9301';
/** A closed session, in Continue's list. */
export const S_CLOSED = '01a1f83c-da20-751f-80b7-ec9a3f1f9302';
/** A session that did not exist when the page loaded (New opens it). */
export const S_NEW = '01a1f83c-da20-751f-80b7-ec9a3f1f9303';

export const STATE = 'GET /v1/operating-session';
export const ROWS = 'GET /v1/operating-session/members';
export const CLOSED = 'GET /v1/operating-session/closed';
export const CONTINUE = 'POST /v1/operating-session/continue';
export const NEW = 'POST /v1/operating-session/new';

export const uuid = (n: number): string =>
    `01a21282-f185-7377-a3eb-cf669a0889${String(n).padStart(2, '0')}`;

export const sessionState = (
    id: string | null,
    generation: number,
    over: Record<string, unknown> = {}
): Record<string, unknown> => ({
    archive_id: ARCHIVE,
    generation,
    state: id === null ? 'none' : 'open',
    session:
        id === null
            ? null
            : {
                  id,
                  logbook_uuid: LOGBOOK,
                  opened_at: '2026-11-20T09:15:00Z',
                  sitting_started_at: '2026-11-25T09:15:00Z',
                  has_contacts: true,
                  contact_count: 1,
                  held_by_you: true,
              },
    ...over,
});

/** The question: the session left open by an earlier start. */
export const pendingState = (id: string, generation: number): Record<string, unknown> => {
    const s = sessionState(id, generation, { state: 'recovery_pending' });
    (s.session as Record<string, unknown>).held_by_you = false;
    return s;
};

export const member = (n: number, call: string): unknown => ({
    uuid: uuid(n),
    revision: 0,
    call,
    qso_date: '20261125',
    time_on: '143000',
    band: '20m',
    mode: 'FT8',
    rst_sent: '-07',
    rst_rcvd: '-10',
    name: '',
    country: '',
    comment: '',
    emailed: false,
});

export const members = (id: string | null, generation: number, rows: unknown[]): unknown => ({
    archive_id: ARCHIVE,
    generation,
    session_id: id,
    members: rows,
});

export const closedLine = (id: string, over: Record<string, unknown> = {}): unknown => ({
    id,
    logbook_uuid: LOGBOOK,
    opened_at: '2026-11-20T09:15:00Z',
    latest_sitting_started_at: '2026-11-20T09:15:00Z',
    latest_sitting_ended_at: '2026-11-20T11:42:00Z',
    an_end_unconfirmed: false,
    contact_count: 37,
    ...over,
});

export const closedList = (
    generation: number,
    sessions: unknown[],
    over: Record<string, unknown> = {}
): unknown => ({
    archive_id: ARCHIVE,
    generation,
    logbook_uuid: LOGBOOK,
    sessions,
    more: false,
    ...over,
});

export const ok = (body: unknown, hold?: Promise<void>): Route => ({ status: 200, body, hold });

/** A daemon serving ARCHIVE, set up, with the given answers. */
export function daemonWith(routes: Daemon['routes'], ft8 = false): Daemon {
    return {
        version: {
            daemon: 'test',
            instance: 'i1',
            archive: {
                id: ARCHIVE,
                label: 'Home',
                ownership: 'legacy',
                default_logbook_uuid: LOGBOOK,
            },
        },
        config: {
            setup_complete: true,
            logging_station: { station_callsign: '7Q5MLV' },
            default_logbook: { id: 1, uuid: LOGBOOK, name: 'Home', callsign: '7Q5MLV' },
            bridge: { enabled: false },
            ft8_enabled: ft8,
            ft8_running: ft8,
        },
        routes,
        sent: [],
    };
}

const path = (s: Sent): string => s.url.split('?')[0];

/** The requests the page sent to one path, in order. */
export const sentTo = (d: Daemon, method: string, url: string): Sent[] =>
    d.sent.filter((s) => s.method === method && path(s) === url);

export const stateReads = (d: Daemon): number => sentTo(d, 'GET', '/v1/operating-session').length;
export const rowReads = (d: Daemon): number =>
    sentTo(d, 'GET', '/v1/operating-session/members').length;
/** The queries of the reads of Continue's list, in order ("limit=20", …). */
export const listReads = (d: Daemon): string[] =>
    sentTo(d, 'GET', '/v1/operating-session/closed').map((s) => s.url.split('?')[1] ?? '');
export const continues = (d: Daemon): Sent[] => sentTo(d, 'POST', '/v1/operating-session/continue');
export const news = (d: Daemon): Sent[] => sentTo(d, 'POST', '/v1/operating-session/new');

export function deferred(): { promise: Promise<void>; release: () => void } {
    let release!: () => void;
    const promise = new Promise<void>((r) => (release = r));
    return { promise, release };
}

export const WAIT = { timeout: 10_000 };
export const TEST_LIMIT = 60_000;

/**
 * Load the real page on this daemon and open its event stream. A real page has
 * proven nothing when it loads: the boot binds the archive.
 */
export async function boot(d: Daemon, view: 'phone' | 'ft8' = 'phone'): Promise<void> {
    setBoundArchive(null);
    _reloadArchiveStoresForTests();
    installDaemon(d);
    _setReloadForTests(() => {});
    if (view === 'ft8') {
        window.localStorage.setItem('sm-op-mode', 'ft8');
        window.history.replaceState({}, '', '/operate/ft8');
    }
    document.body.innerHTML = '<div id="app"></div>';
    await import('../main');
    await vi.waitFor(() => expect(archivesState.verifying).toBe(false), WAIT);
    shellStream().emit('open');
}

/** The Session panel is a tile the operator shows from the rail. */
export async function showPanel(): Promise<void> {
    await fireEvent.click(await screen.findByTitle('Session — show', {}, WAIT));
}

/** Press "Continue…" and give the list's lines once they are read. */
export async function openList(): Promise<HTMLElement[]> {
    await fireEvent.click(await screen.findByRole('button', { name: 'Continue…' }, WAIT));
    return screen.findAllByRole('button', { name: /UTC ·/ }, WAIT);
}

/** An event on the shell's stream, as the daemon sends it. */
export const event = (name: string, payload: Record<string, unknown> = {}): void =>
    shellStream().emit(name, JSON.stringify(payload));

/** The shell's event stream drops, and its replacement opens. */
export async function reconnect(): Promise<void> {
    const dropped = shellStream();
    dropped.emit('error');
    (await nextSocket(dropped, (fn) => vi.waitFor(fn))).emit('open');
}
