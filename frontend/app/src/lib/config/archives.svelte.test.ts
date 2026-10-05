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
    archiveEntryLock,
    archiveSwitchGate,
    verifyAfterRigReconnect,
    archivesState,
    createArchive,
    archiveDraft,
    archiveDraftDirty,
    clearArchiveDraft,
    retireEmptyDraftKey,
    submitArchiveDraft,
    loadArchives,
    mintRequestKey,
    bootArchiveScoped,
    verifyArchiveGeneration,
    _resetArchivesForTests,
    _setBootIdentityForTests,
    _setReloadForTests,
} from './archives.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';
import { draft, clearDraft, submitState } from '../operate/qso.svelte';
import { installBrowserStorageSpies } from '../utils/browserStorageSpies.fixture';

/*
    ARCHIVES STATE — the one activation flow, run from Settings → Archives (ADR
    0087: the header only names the archive). The rules: the daemon's state is the only truth (a 202 never
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
    lastActivationCode: '',
    sizeBytes: 1,
    modifiedAt: null,
    logbooks: [],
    contentsStatus: 'current',
} as const;
const CONTEST = {
    id: 'b',
    label: 'Contest',
    ownership: 'managed',
    state: 'inactive',
    lastActivationError: '',
    lastActivationCode: '',
    sizeBytes: 1,
    modifiedAt: null,
    logbooks: [],
    contentsStatus: 'current',
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
    clearDraft();
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
        const confirm = vi.fn((_text: string) => true);
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
        expect(archivesState.switchUnresolved).toBe(true); // latched until the page actually unloads
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
        expect(archivesState.switchUnresolved).toBe(true);
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
        for (let i = 0; i < 6; i++) await Promise.resolve(); // the watch runs in the background
        expect(waitForDaemonBack).toHaveBeenCalledTimes(3);
        expect(reloads).toBe(1);
        expect(archivesState.switchUnresolved).toBe(true); // latched through the reload request
        expect(archivesState.switchDetail).toMatch(/restarted on another archive/);
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
        expect(archivesState.switchDetail).toMatch(/did not answer as a new instance/);
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

/*
    THE ACTIVATE GATE (ADR 0087; operator rulings 2026-10-05). Switching is
    offered only from Settings → Archives, and a switch reloads every window, so
    the confirmation itself is the gate:
      A1  An empty form: the prompt names the destination and warns that
          unlogged work in any other window will be lost; OK sends, Cancel sends
          nothing.
      A2  An unlogged entry — a partial one too — is NOT refused: the prompt
          names it and says OK discards this window's entry and switches.
          Cancel sends nothing and keeps the entry untouched.
      A3  The entry is discarded by the reload, never before the request: a
          definite refusal or an answer that did not accept leaves it in place
          and reloads nothing; an accepted switch reloads with no save step; an
          unproven outcome gates and keeps it.
      A4  A Log in flight refuses Activate, checked before the prompt AND again
          before the request. Once the outcome is unknown, switching is allowed
          and the prompt says the QSO may already be logged, to be checked in
          the original archive's Logbook.
      U3  From the moment the request goes out until the outcome, entry is
          LOCKED (archiveEntryLock names why); a definite refusal unlocks it;
          an uncertain outcome keeps the page gated.
      U4  A reconnect's identity check (verifying) does NOT lock entry.
*/
describe('activateArchive — the Activate gate', () => {
    const OTHER_WINDOWS = /unlogged work in any other open window .* is lost/i;

    beforeEach(() => {
        submitState.busy = false;
        submitState.uncertain = false;
    });
    afterEach(() => {
        submitState.busy = false;
        submitState.uncertain = false;
    });

    it('A1 an empty form: the prompt names the destination and warns about other windows', async () => {
        await loadArchives();
        const confirm = vi.fn((_text: string) => false);
        expect(await activateArchive('b', confirm)).toBe(false);
        const text = confirm.mock.calls[0][0];
        expect(text).toMatch(/“Contest”/);
        expect(text).toMatch(OTHER_WINDOWS);
        expect(text).not.toMatch(/discards/);
        expect(activateQsoArchive).not.toHaveBeenCalled();
    });

    it('A2 an unlogged entry is offered for discard, named; Cancel keeps it and sends nothing', async () => {
        await loadArchives();
        draft.callsign = 'ZS6BOS';
        draft.timeOn = '10:00:00';
        const confirm = vi.fn((_text: string) => false);
        expect(await activateArchive('b', confirm)).toBe(false);
        expect(confirm).toHaveBeenCalledTimes(1);
        const text = confirm.mock.calls[0][0];
        expect(text).toMatch(/“Contest”/);
        expect(text).toMatch(/ZS6BOS/);
        expect(text).toMatch(/10:00:00/);
        expect(text).toMatch(/OK discards this window’s entry and switches/);
        expect(text).toMatch(OTHER_WINDOWS);
        expect(fetchDaemonInstance).not.toHaveBeenCalled();
        expect(activateQsoArchive).not.toHaveBeenCalled();
        expect(toastsState.items).toEqual([]);
        expect(draft.callsign).toBe('ZS6BOS');
    });

    it('A2 a partial entry (no callsign yet) is named as one; OK sends the request', async () => {
        await loadArchives();
        draft.name = 'Bob';
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'refused',
            code: 'tx_busy',
            message: 'transmit is armed',
        });
        const confirm = vi.fn((_text: string) => true);
        await activateArchive('b', confirm);
        expect(confirm).toHaveBeenCalledTimes(1);
        expect(confirm.mock.calls[0][0]).toMatch(/partial/);
        expect(confirm.mock.calls[0][0]).toMatch(/OK discards this window’s entry and switches/);
        expect(activateQsoArchive).toHaveBeenCalledTimes(1);
    });

    it('A3 a definite refusal or a non-accepting answer leaves the entry and reloads nothing', async () => {
        await loadArchives();
        draft.callsign = 'ZS6BOS';
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValueOnce({
            kind: 'refused',
            code: 'tx_busy',
            message: 'transmit is armed',
        });
        expect(await activateArchive('b', () => true)).toBe(false);
        vi.mocked(activateQsoArchive).mockResolvedValueOnce({ kind: 'error', message: 'HTTP 500' });
        expect(await activateArchive('b', () => true)).toBe(false);
        expect(draft.callsign).toBe('ZS6BOS');
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(false);
    });

    it('A3 an accepted switch reloads with no save step; an unproven one gates and keeps the entry', async () => {
        const spies = installBrowserStorageSpies();
        try {
            await loadArchives();
            draft.callsign = 'ZS6BOS';
            vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
            vi.mocked(activateQsoArchive).mockResolvedValue({ kind: 'network', message: 'reset' });
            vi.mocked(waitForDaemonBack).mockResolvedValue(false);
            await activateArchive('b', () => true);
            expect(archivesState.switchUnresolved).toBe(true);
            expect(draft.callsign).toBe('ZS6BOS');
            expect(reloads).toBe(0);

            _resetArchivesForTests();
            await loadArchives();
            vi.mocked(activateQsoArchive).mockResolvedValue({
                kind: 'accepted',
                id: 'b',
                durability: 'durable',
            });
            vi.mocked(waitForDaemonBack).mockResolvedValue(true);
            expect(await activateArchive('b', () => true)).toBe(true);
            expect(reloads).toBe(1);
            expect(spies.opens).toEqual([]);
            expect(spies.deletes).toEqual([]);
            expect(spies.locks).toEqual([]);
            expect(spies.channels).toEqual([]);
        } finally {
            spies.restore();
        }
    });

    it('A4 a Log in flight refuses before the prompt', async () => {
        await loadArchives();
        draft.callsign = 'ZS6BOS';
        submitState.busy = true;
        const confirm = vi.fn((_text: string) => true);
        expect(await activateArchive('b', confirm)).toBe(false);
        expect(confirm).not.toHaveBeenCalled();
        expect(activateQsoArchive).not.toHaveBeenCalled();
        expect(hasToast('error', /being logged.*wait for it to finish/i)).toBe(true);
    });

    it('A4 a Log started while the prompt is open refuses before the request', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        const confirm = vi.fn((_text: string) => {
            submitState.busy = true;
            return true;
        });
        expect(await activateArchive('b', confirm)).toBe(false);
        expect(confirm).toHaveBeenCalledTimes(1);
        expect(fetchDaemonInstance).not.toHaveBeenCalled();
        expect(activateQsoArchive).not.toHaveBeenCalled();
        expect(hasToast('error', /being logged.*wait for it to finish/i)).toBe(true);
        expect(archivesState.activating).toBe(false);
    });

    it('A4 an unknown Log outcome is allowed, saying it may already be logged in the original archive', async () => {
        await loadArchives();
        draft.callsign = 'ZS6BOS';
        submitState.uncertain = true;
        const confirm = vi.fn((_text: string) => false);
        await activateArchive('b', confirm);
        expect(confirm).toHaveBeenCalledTimes(1);
        const text = confirm.mock.calls[0][0];
        expect(text).toMatch(/may already be logged/);
        expect(text).toMatch(/Logbook in “Home”/);
        // Uncertain, so never called unlogged (review 2026-10-05).
        expect(text).toMatch(/Phone \/ CW entry/);
        expect(text).not.toMatch(/unlogged QSO/);
        expect(text).toMatch(/OK discards this window’s entry and switches/);
    });

    it('U3 entry is locked while the request is in flight and unlocked by a definite refusal', async () => {
        await loadArchives();
        expect(archiveEntryLock()).toBeNull();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        let answer: (v: { kind: 'refused'; code: string; message: string }) => void = () => {};
        vi.mocked(activateQsoArchive).mockReturnValue(new Promise((r) => (answer = r)));
        const run = activateArchive('b', () => true);
        await Promise.resolve();
        await Promise.resolve();
        expect(archiveEntryLock()).toMatch(/archive switch is in progress/);
        answer({ kind: 'refused', code: 'tx_busy', message: 'transmit is armed' });
        await run;
        expect(archiveEntryLock()).toBeNull();
    });

    it('U3 an uncertain outcome keeps entry locked with the gate', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({ kind: 'network', message: 'reset' });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        await activateArchive('b', () => true);
        expect(archivesState.activating).toBe(false);
        expect(archiveEntryLock()).toMatch(/unresolved/);
    });

    it('U4 a reconnect identity check does not lock entry', () => {
        archivesState.verifying = true;
        expect(archiveSwitchGate()).not.toBeNull(); // submits still wait for it
        expect(archiveEntryLock()).toBeNull();
    });
});

/*
    EVERY AUTOMATIC REBIND RELOADS AT ONCE (ADR 0087: no save step).
      R2  The switch watch, the unproven-boot watch and a boot bracket that
          straddles a change each reload directly — a held entry is not saved
          and does not hold the reload.
      V10 (Codex review ebe244dc P2, retained): a rig reconnect whose identity
          cannot be read is retried, then FAILS CLOSED — gated as a log-stream
          reconnect is. Another archive reloads. During boot (no proven
          baseline yet) it does nothing.
*/
describe('automatic rebind reloads', () => {
    it('R2 every automatic reload path reloads directly, a held entry included', async () => {
        draft.callsign = 'ZS6BOS';
        // The switch watch: the wait expires, then the new instance is seen.
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValueOnce(false).mockResolvedValueOnce(true);
        await activateArchive('b', () => true);
        for (let i = 0; i < 10; i++) await Promise.resolve();
        expect(reloads).toBe(1);

        // The unproven-boot watch: the daemon answers again.
        _resetArchivesForTests();
        vi.mocked(fetchDaemonIdentity).mockResolvedValue(null);
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        await bootArchiveScoped(() => Promise.resolve(0));
        for (let i = 0; i < 10; i++) await Promise.resolve();
        expect(reloads).toBe(2);

        // The boot bracket straddling a change.
        _resetArchivesForTests();
        vi.mocked(fetchDaemonIdentity)
            .mockResolvedValueOnce({ instance: 'i1', archiveId: 'a' })
            .mockResolvedValueOnce({ instance: 'i2', archiveId: 'b' });
        await bootArchiveScoped(() => Promise.resolve(0));
        expect(reloads).toBe(3);
        expect(draft.callsign).toBe('ZS6BOS'); // never saved, only lost to the real reload
    });

    it('V10 a rig reconnect with an unreadable identity is retried, then gates', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue(null);
        await verifyAfterRigReconnect();
        expect(fetchDaemonIdentity).toHaveBeenCalledTimes(3);
        expect(archivesState.switchUnresolved).toBe(true);
        expect(archivesState.switchDetail).toMatch(/rig connection came back/);
        expect(reloads).toBe(0);
    });

    it('V10 a rig reconnect to another archive reloads', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        draft.callsign = 'ZS6BOS';
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        await verifyAfterRigReconnect();
        expect(reloads).toBe(1);
        expect(archivesState.switchUnresolved).toBe(true);
    });

    it('V10 a rig reconnect to the same archive changes nothing, a restart included', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'a' });
        await verifyAfterRigReconnect();
        expect(archivesState.switchUnresolved).toBe(false);
        expect(reloads).toBe(0);
    });

    it('V10 before the boot bracket proves a baseline it does nothing', async () => {
        vi.mocked(fetchDaemonIdentity).mockResolvedValue(null);
        await verifyAfterRigReconnect();
        expect(fetchDaemonIdentity).not.toHaveBeenCalled();
        expect(archivesState.switchUnresolved).toBe(false);
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

describe('the gate while a verification is pending, and latching before a reload', () => {
    it('operations are refused while the reconnect identity check is in flight', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        let release: (v: { instance: string; archiveId: string }) => void = () => {};
        vi.mocked(fetchDaemonIdentity).mockReturnValue(new Promise((r) => (release = r)));
        const pending = verifyArchiveGeneration();
        await Promise.resolve();
        expect(archivesState.verifying).toBe(true);
        expect(archiveSwitchGate()).toMatch(/confirming which archive/);
        release({ instance: 'i1', archiveId: 'a' });
        await pending;
        expect(archivesState.verifying).toBe(false);
        expect(archiveSwitchGate()).toBeNull();
    });

    it('an older check settling cannot reopen admission while a newer one is still pending', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        const releases: ((v: { instance: string; archiveId: string }) => void)[] = [];
        vi.mocked(fetchDaemonIdentity).mockImplementation(
            () => new Promise((r) => releases.push(r))
        );
        const first = verifyArchiveGeneration();
        const second = verifyArchiveGeneration();
        await Promise.resolve();
        expect(releases).toHaveLength(2);
        releases[0]({ instance: 'i1', archiveId: 'a' }); // the older check: same archive
        await first;
        expect(archiveSwitchGate()).not.toBeNull(); // the newer check is still reading
        releases[1]({ instance: 'i2', archiveId: 'b' }); // the newer check: the replacement daemon
        await second;
        expect(reloads).toBe(1);
    });

    it('operations are refused for the whole boot bracket, including while the shell opens inside it', async () => {
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i1', archiveId: 'a' });
        let gateInsideReads: string | null = 'unset';
        await bootArchiveScoped(() => {
            gateInsideReads = archiveSwitchGate();
            return Promise.resolve();
        });
        expect(gateInsideReads).toMatch(/confirming which archive/);
        expect(archiveSwitchGate()).toBeNull();
    });

    it('a proven switch latches the gate BEFORE requesting the reload, so a cancelled unload stays gated', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        let latchedAtReload = false;
        _setReloadForTests(() => {
            latchedAtReload = archivesState.switchUnresolved;
        });
        await activateArchive('b', () => true);
        expect(latchedAtReload).toBe(true);
        expect(archivesState.switchUnresolved).toBe(true); // the page survived the (cancelled) unload: still gated
        expect(archiveSwitchGate()).not.toBeNull();
    });

    it('a changed archive on reconnect latches before its reload too', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        let latchedAtReload = false;
        _setReloadForTests(() => {
            latchedAtReload = archivesState.switchUnresolved;
        });
        await verifyArchiveGeneration();
        expect(latchedAtReload).toBe(true);
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

// The New archive form's draft lives in the store so the Settings leave guard
// can see and discard it (operator ruling 2026-09-29, the Logbooks rules). A
// success clears only the draft it submitted; a newer one typed while the
// request was on the wire stays, with a fresh request key of its own.
describe('the New archive draft', () => {
    const typed = (label: string) => {
        archiveDraft.label = label;
        archiveDraft.logbookName = 'Contest';
        archiveDraft.logbookCallsign = 'g4abc';
    };
    const created = () =>
        vi.mocked(createQsoArchive).mockResolvedValue({
            kind: 'ok',
            archive: CONTEST,
            reused: false,
        });

    it('an untouched form is not an edit; any typed field is', () => {
        clearArchiveDraft();
        expect(archiveDraftDirty()).toBe(false);
        archiveDraft.requestKey = 'k'; // a key alone is not work at stake
        expect(archiveDraftDirty()).toBe(false);
        for (const field of ['label', 'logbookName', 'logbookCallsign'] as const) {
            clearArchiveDraft();
            archiveDraft[field] = 'x';
            expect(archiveDraftDirty()).toBe(true);
        }
        clearArchiveDraft();
        archiveDraft.label = '   ';
        expect(archiveDraftDirty()).toBe(false);
    });

    it('submits the trimmed draft with one request key, uppercased callsign, and clears it', async () => {
        clearArchiveDraft();
        typed(' Contest ');
        created();
        expect(await submitArchiveDraft()).toBe(true);
        const sent = vi.mocked(createQsoArchive).mock.calls[0][0];
        expect(sent).toMatchObject({
            label: 'Contest',
            logbookName: 'Contest',
            logbookCallsign: 'G4ABC',
        });
        expect(sent.requestKey).not.toBe('');
        expect(archiveDraftDirty()).toBe(false);
        expect(archiveDraft.requestKey).toBe('');
    });

    it('a refusal keeps the draft and its key for the retry', async () => {
        clearArchiveDraft();
        typed('Contest');
        vi.mocked(createQsoArchive).mockResolvedValue({
            kind: 'refused',
            code: 'invalid_field_value',
            message: 'no',
        });
        expect(await submitArchiveDraft()).toBe(false);
        const key = archiveDraft.requestKey;
        expect(key).not.toBe('');
        expect(archiveDraft.label).toBe('Contest');
        await submitArchiveDraft();
        expect(vi.mocked(createQsoArchive).mock.calls[1][0].requestKey).toBe(key);
    });

    it('keeps a newer draft typed while the create was on the wire, with a new key', async () => {
        clearArchiveDraft();
        typed('First');
        let answer: (v: unknown) => void = () => {};
        vi.mocked(createQsoArchive).mockImplementation(
            () => new Promise((r) => (answer = r)) as never
        );
        const pending = submitArchiveDraft();
        const firstKey = archiveDraft.requestKey;
        archiveDraft.label = 'Second';
        answer({ kind: 'ok', archive: CONTEST, reused: false });
        expect(await pending).toBe(true);
        expect(archiveDraft.label).toBe('Second');
        expect(archiveDraftDirty()).toBe(true);
        // The created archive's key must not be reused for a different one.
        expect(archiveDraft.requestKey).not.toBe(firstKey);
    });

    it('an emptied form retires its key; a kept draft keeps it for the retry', () => {
        typed('Contest');
        archiveDraft.requestKey = 'k';
        retireEmptyDraftKey();
        expect(archiveDraft.requestKey).toBe('k');
        archiveDraft.label = '';
        archiveDraft.logbookName = ' ';
        archiveDraft.logbookCallsign = '';
        retireEmptyDraftKey();
        expect(archiveDraft.requestKey).toBe('');
    });

    it('a discard clears the fields and the key', () => {
        typed('Contest');
        archiveDraft.requestKey = 'k';
        clearArchiveDraft();
        expect(archiveDraft).toEqual({
            label: '',
            logbookName: '',
            logbookCallsign: '',
            requestKey: '',
        });
    });
});
