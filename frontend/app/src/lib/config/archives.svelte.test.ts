import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../api/qso-archives', () => ({
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
    fetchDaemonIdentity: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));

import {
    activateQsoArchive,
    createQsoArchive,
    fetchDaemonIdentity,
    fetchQsoArchives,
} from '../api/qso-archives';
import { fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import {
    activateArchive,
    activeArchive,
    archiveSwitchGate,
    archivesState,
    createArchive,
    loadArchives,
    mintRequestKey,
    bootArchiveScoped,
    verifyArchiveGeneration,
    _resetArchivesForTests,
    _setBootIdentityForTests,
    _setReloadForTests,
} from './archives.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';

/*
    ARCHIVES STATE — the one activation flow both the Settings tab and the header
    selector run. The rules: the daemon's state is the only truth (a 202 never
    makes the list say "active"); a refusal shows the daemon's reason; the
    restart is awaited by the new-instance signal; an ambiguous timeout is never
    "failed".
*/

const HOME = {
    id: 'a',
    label: 'Home',
    ownership: 'legacy',
    state: 'active',
    lastActivationError: '',
    sizeBytes: 1,
    modifiedAt: null,
} as const;
const CONTEST = {
    id: 'b',
    label: 'Contest',
    ownership: 'managed',
    state: 'inactive',
    lastActivationError: '',
    sizeBytes: 1,
    modifiedAt: null,
} as const;

const hasToast = (level: string, re: RegExp): boolean =>
    toastsState.items.some((t) => t.level === level && re.test(t.message));

beforeEach(() => {
    vi.mocked(fetchQsoArchives).mockReset();
    vi.mocked(createQsoArchive).mockReset();
    vi.mocked(activateQsoArchive).mockReset();
    vi.mocked(fetchDaemonIdentity).mockReset();
    vi.mocked(fetchDaemonInstance).mockReset();
    vi.mocked(waitForDaemonBack).mockReset();
    _resetArchivesForTests();
    resetToasts();
    reloads = 0;
    _setReloadForTests(() => reloads++);
    vi.mocked(fetchQsoArchives).mockResolvedValue({ kind: 'ok', archives: [HOME, CONTEST] });
});
let reloads = 0;
afterEach(() => vi.restoreAllMocks());

describe('loadArchives', () => {
    it('holds the daemon list and names the active archive', async () => {
        await loadArchives();
        expect(archivesState.loaded).toBe(true);
        expect(activeArchive()?.label).toBe('Home');
    });
    it('a failed re-read retains the list but marks it STALE, with the error; a fresh read clears it', async () => {
        expect(await loadArchives()).toBe(true);
        vi.mocked(fetchQsoArchives).mockResolvedValue({ kind: 'error', message: 'down' });
        expect(await loadArchives()).toBe(false);
        expect(archivesState.list.map((a) => a.id)).toEqual(['a', 'b']);
        expect(archivesState.error).toBe('down');
        expect(archivesState.stale).toBe(true);
        vi.mocked(fetchQsoArchives).mockResolvedValue({ kind: 'ok', archives: [HOME] });
        expect(await loadArchives()).toBe(true);
        expect(archivesState.stale).toBe(false);
        expect(archivesState.error).toBe('');
    });
    it('a first read that fails is not stale (nothing was retained)', async () => {
        vi.mocked(fetchQsoArchives).mockResolvedValue({ kind: 'error', message: 'down' });
        await loadArchives();
        expect(archivesState.stale).toBe(false);
        expect(archivesState.loaded).toBe(false);
    });
});

describe('activateArchive', () => {
    it('a declined confirmation sends nothing', async () => {
        await loadArchives();
        expect(await activateArchive('b', () => false)).toBe(false);
        expect(activateQsoArchive).not.toHaveBeenCalled();
    });

    it('the active archive is never re-activated', async () => {
        await loadArchives();
        const confirm = vi.fn(() => true);
        expect(await activateArchive('a', confirm)).toBe(false);
        expect(confirm).not.toHaveBeenCalled();
        expect(activateQsoArchive).not.toHaveBeenCalled();
    });

    it('accepted: waits for the NEW daemon instance, then RELOADS the page to rebind every archive-scoped store', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        expect(await activateArchive('b', () => true)).toBe(true);
        expect(waitForDaemonBack).toHaveBeenCalledWith('inst-1');
        expect(reloads).toBe(1);
        expect(hasToast('info', /Daemon restarted\. Reloading/)).toBe(true);
        expect(archivesState.switchUnresolved).toBe(false);
        expect(archivesState.activating).toBe(false);
    });

    it('a NEW instance answering reloads at once, even when the catalogue cannot be read from it', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        vi.mocked(fetchQsoArchives).mockResolvedValue({ kind: 'error', message: 'down' });
        await activateArchive('b', () => true);
        expect(reloads).toBe(1);
        expect(archivesState.switchUnresolved).toBe(false);
    });

    it('no baseline instance: the generation cannot be proven → the app is GATED, no reload, no watch', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        await activateArchive('b', () => true);
        expect(waitForDaemonBack).not.toHaveBeenCalled();
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(true);
        expect(archivesState.switchDetail).toMatch(/could not be read before the switch/);
        expect(archiveSwitchGate()).toMatch(/reload the page/);
        expect(hasToast('warn', /outcome is unknown/)).toBe(true);
        expect(hasToast('error', /./)).toBe(false);
        // Gated: another activation is refused without a request.
        expect(await activateArchive('b', () => true)).toBe(false);
        expect(activateQsoArchive).toHaveBeenCalledTimes(1);
    });

    it('the wait expires: the app is GATED and keeps watching; the new instance, when seen, reloads', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        vi.mocked(waitForDaemonBack)
            .mockResolvedValueOnce(false) // the activation's own wait
            .mockResolvedValueOnce(false) // watch round 1
            .mockResolvedValueOnce(true); // watch round 2: the new instance
        await activateArchive('b', () => true);
        expect(archivesState.switchUnresolved).toBe(true);
        expect(archivesState.switchDetail).toMatch(/did not answer as a new instance/);
        for (let i = 0; i < 6; i++) await Promise.resolve(); // the watch runs in the background
        expect(waitForDaemonBack).toHaveBeenCalledTimes(3);
        expect(reloads).toBe(1);
    });

    it('the watch gives up after its rounds and leaves the gate up', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        await activateArchive('b', () => true);
        for (let i = 0; i < 30; i++) await Promise.resolve();
        expect(waitForDaemonBack).toHaveBeenCalledTimes(11); // 1 + WATCH_ROUNDS
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(true);
    });

    it('a non-timeout transport failure is an UNCONFIRMED write: reconciled like a timeout, gated when unproven', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'network',
            message: 'connection reset',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        expect(await activateArchive('b', () => true)).toBe(false);
        expect(waitForDaemonBack).toHaveBeenCalledWith('inst-1');
        expect(archivesState.switchUnresolved).toBe(true);
        expect(hasToast('error', /./)).toBe(false);
    });

    it('an uncoded daemon answer is a definite non-acceptance: an error, no gate', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({ kind: 'error', message: 'HTTP 500' });
        expect(await activateArchive('b', () => true)).toBe(false);
        expect(waitForDaemonBack).not.toHaveBeenCalled();
        expect(archivesState.switchUnresolved).toBe(false);
        expect(hasToast('error', /HTTP 500/)).toBe(true);
    });

    it('the gate names an in-flight activation too', async () => {
        await loadArchives();
        expect(archiveSwitchGate()).toBeNull();
        archivesState.activating = true;
        expect(archiveSwitchGate()).toMatch(/in progress/);
    });

    it('accepted but the daemon never comes back: the outcome is unknown, never "failed", and the app is gated', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'uncertain',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        await activateArchive('b', () => true);
        expect(hasToast('info', /durability unconfirmed/)).toBe(true);
        expect(hasToast('warn', /outcome is unknown/)).toBe(true);
        expect(hasToast('error', /./)).toBe(false);
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(true);
    });

    it('refused: shows the daemon reason and reloads the list', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'refused',
            code: 'tx_busy',
            message: 'FT8 is busy: ft8: transmit is armed',
        });
        expect(await activateArchive('b', () => true)).toBe(false);
        expect(hasToast('error', /transmit is armed/)).toBe(true);
        expect(reloads).toBe(0);
        expect(waitForDaemonBack).not.toHaveBeenCalled();
        expect(fetchQsoArchives).toHaveBeenCalledTimes(2);
        expect(activeArchive()?.label).toBe('Home');
    });

    it('a timed-out request reconciles by the new-instance signal; a new instance proves a late switch and reloads', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'network',
            message: 'timed out',
            timedOut: true,
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        await activateArchive('b', () => true);
        expect(waitForDaemonBack).toHaveBeenCalledWith('inst-1');
        expect(hasToast('warn', /outcome is unknown/)).toBe(true);
        expect(hasToast('error', /./)).toBe(false);
        expect(reloads).toBe(1);
    });

    it('a second activation while one is in flight is ignored', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        let release: (v: {
            kind: 'accepted';
            id: string;
            durability: 'durable';
        }) => void = () => {};
        vi.mocked(activateQsoArchive).mockReturnValue(new Promise((r) => (release = r)));
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        const first = activateArchive('b', () => true);
        await Promise.resolve();
        await Promise.resolve();
        expect(archivesState.activating).toBe(true);
        expect(await activateArchive('b', () => true)).toBe(false);
        release({ kind: 'accepted', id: 'b', durability: 'durable' });
        await first;
        expect(activateQsoArchive).toHaveBeenCalledTimes(1);
    });
});

describe('verifyArchiveGeneration — every tab rebinds on reconnect', () => {
    it('the same archive after a reconnect changes nothing', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'a' });
        await verifyArchiveGeneration();
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(false);
    });

    it('another archive served after a reconnect reloads this tab', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        await verifyArchiveGeneration();
        expect(reloads).toBe(1);
        expect(hasToast('info', /serves another archive/)).toBe(true);
    });

    it('an unreadable identity after retries gates the tab (fail closed)', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue(null);
        await verifyArchiveGeneration();
        expect(fetchDaemonIdentity).toHaveBeenCalledTimes(3);
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(true);
        expect(archivesState.switchDetail).toMatch(/identity could not be read/);
        expect(archiveSwitchGate()).not.toBeNull();
    });

    it('a transient read failure is retried before gating', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity)
            .mockResolvedValueOnce(null)
            .mockResolvedValueOnce({ instance: 'i2', archiveId: 'a' });
        await verifyArchiveGeneration();
        expect(archivesState.switchUnresolved).toBe(false);
        expect(reloads).toBe(0);
    });

    it('a reconnect while the gate is latched does nothing extra (the watch owns the rebind)', async () => {
        vi.mocked(fetchDaemonIdentity).mockResolvedValue(null);
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        await bootArchiveScoped(() => Promise.resolve());
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        await verifyArchiveGeneration();
        expect(archivesState.switchUnresolved).toBe(true);
    });
});

describe('bootArchiveScoped — the identity is bracketed around the archive-scoped reads', () => {
    it('two agreeing reads record the identity the stores were built against', async () => {
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i1', archiveId: 'a' });
        const reads = vi.fn(() => Promise.resolve('ctx'));
        expect(await bootArchiveScoped(reads)).toBe('ctx');
        expect(reads).toHaveBeenCalledTimes(1);
        expect(fetchDaemonIdentity).toHaveBeenCalledTimes(2);
        // Recorded: a later reconnect on the same archive changes nothing.
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i9', archiveId: 'a' });
        await verifyArchiveGeneration();
        expect(reloads).toBe(0);
    });

    it('a boot that straddles a switch (identity differs across the reads) reloads at once', async () => {
        vi.mocked(fetchDaemonIdentity)
            .mockResolvedValueOnce({ instance: 'i1', archiveId: 'a' })
            .mockResolvedValueOnce({ instance: 'i2', archiveId: 'b' });
        await bootArchiveScoped(() => Promise.resolve());
        expect(reloads).toBe(1);
        expect(hasToast('info', /changed while the page was loading/)).toBe(true);
    });

    it('a plain restart during the boot (instance differs, same archive) also reloads: nothing is proven', async () => {
        vi.mocked(fetchDaemonIdentity)
            .mockResolvedValueOnce({ instance: 'i1', archiveId: 'a' })
            .mockResolvedValueOnce({ instance: 'i2', archiveId: 'a' });
        await bootArchiveScoped(() => Promise.resolve());
        expect(reloads).toBe(1);
    });

    it('an unreadable identity on either side LATCHES the gate at once (no reconnect needed), then reloads when a daemon answers', async () => {
        vi.mocked(fetchDaemonIdentity)
            .mockResolvedValueOnce({ instance: 'i1', archiveId: 'a' })
            .mockResolvedValue(null); // the trailing read fails, retries included
        vi.mocked(waitForDaemonBack).mockResolvedValueOnce(false).mockResolvedValueOnce(true);
        await bootArchiveScoped(() => Promise.resolve());
        expect(archivesState.switchUnresolved).toBe(true);
        expect(archivesState.switchDetail).toMatch(/while the page was loading/);
        expect(archiveSwitchGate()).not.toBeNull();
        expect(fetchDaemonIdentity).toHaveBeenCalledTimes(1 + 3);
        for (let i = 0; i < 8; i++) await Promise.resolve(); // the watch runs in the background
        expect(waitForDaemonBack).toHaveBeenCalledWith('');
        expect(reloads).toBe(1);
    });

    it('both identity reads failing (daemon down at boot) latches the gate too, and no daemon means no reload', async () => {
        vi.mocked(fetchDaemonIdentity).mockResolvedValue(null);
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        await bootArchiveScoped(() => Promise.resolve());
        expect(archivesState.switchUnresolved).toBe(true);
        for (let i = 0; i < 30; i++) await Promise.resolve();
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(true);
    });
});

describe('createArchive', () => {
    it('created: reports, reloads, resolves true', async () => {
        vi.mocked(createQsoArchive).mockResolvedValue({
            kind: 'ok',
            archive: CONTEST,
            reused: false,
        });
        expect(
            await createArchive({
                requestKey: 'k',
                label: 'Contest',
                logbookName: 'Contest',
                logbookCallsign: 'G4ABC',
            })
        ).toBe(true);
        expect(hasToast('info', /Created archive “Contest”/)).toBe(true);
        expect(archivesState.loaded).toBe(true);
    });
    it('refused: the daemon message, resolves false, form kept', async () => {
        vi.mocked(createQsoArchive).mockResolvedValue({
            kind: 'refused',
            code: 'invalid_field_value',
            message: 'label must be at most 80 characters',
        });
        expect(
            await createArchive({
                requestKey: 'k',
                label: 'x',
                logbookName: 'x',
                logbookCallsign: 'G4ABC',
            })
        ).toBe(false);
        expect(hasToast('error', /at most 80/)).toBe(true);
    });
    it('a timed-out create is unknown and refreshes the list', async () => {
        vi.mocked(createQsoArchive).mockResolvedValue({
            kind: 'network',
            message: 'timed out',
            timedOut: true,
        });
        expect(
            await createArchive({
                requestKey: 'k',
                label: 'x',
                logbookName: 'x',
                logbookCallsign: 'G4ABC',
            })
        ).toBe(false);
        expect(hasToast('warn', /outcome is unknown/)).toBe(true);
        expect(fetchQsoArchives).toHaveBeenCalled();
    });
    it('mints a distinct request key per attempt', () => {
        expect(mintRequestKey()).not.toBe(mintRequestKey());
    });
});
