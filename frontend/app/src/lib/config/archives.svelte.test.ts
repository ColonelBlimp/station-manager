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
    discardAndReload,
    reloadNow,
    retireSnapshotIfSameArchive,
    retrySave,
    setDraftPreserver,
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
import { draft, clearDraft } from '../operate/qso.svelte';
import { rig } from '../operate/rig.svelte';
import {
    noteRigDrop,
    rigReadingForSave,
    _resetRigSnapshotForTests,
} from '../operate/rigSnapshot.svelte';
import type { PreserveResult } from '../drafts/preserve';
import { sampleRecord } from '../drafts/savedDraft.fixture';

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
    setDraftPreserver(null);
    _resetRigSnapshotForTests();
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
    UNLOGGED WORK (ADR 0085; operator rulings 2026-09-30). The switch reloads the
    page and the Phone / CW draft lives only in memory, so:
      U1  Any unlogged work — a partial entry counts — refuses the switch before
          the confirmation is asked, and nothing is sent. Apart from a switch
          that reloads the page over the QSO being typed.
      U2  A QSO started while the confirmation is open is caught by a second
          check after it, before the request. Apart from a check made only
          before a prompt the operator may sit on.
      U3  From the moment the request goes out until the outcome, entry is
          LOCKED (archiveEntryLock names why); a definite refusal unlocks it;
          an uncertain outcome keeps the page gated (the lock stays, under the
          existing overlay). Apart from a form that accepts typing the reload
          is about to discard.
      U4  A reconnect's identity check (verifying) does NOT lock entry: it runs
          on every stream reconnect and the draft survives it.
*/
describe('activateArchive — unlogged Phone / CW work', () => {
    const REFUSAL = /You have an unlogged QSO on Phone \/ CW — log or clear it, then switch\./;

    it('U1 a partial entry refuses the switch before the confirmation; nothing is sent', async () => {
        await loadArchives();
        draft.name = 'Bob'; // no callsign yet: still unlogged work
        const confirm = vi.fn(() => true);
        expect(await activateArchive('b', confirm)).toBe(false);
        expect(confirm).not.toHaveBeenCalled();
        expect(fetchDaemonInstance).not.toHaveBeenCalled();
        expect(activateQsoArchive).not.toHaveBeenCalled();
        expect(hasToast('error', REFUSAL)).toBe(true);
        expect(draft.name).toBe('Bob');
    });

    it('U2 a QSO started while the confirmation is open is refused after it', async () => {
        await loadArchives();
        // Answers ready, so a missing check would reach the request.
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'refused',
            code: 'tx_busy',
            message: 'transmit is armed',
        });
        const confirm = vi.fn(() => {
            draft.callsign = 'ZS6BOS';
            return true;
        });
        expect(await activateArchive('b', confirm)).toBe(false);
        expect(confirm).toHaveBeenCalledTimes(1);
        expect(fetchDaemonInstance).not.toHaveBeenCalled();
        expect(activateQsoArchive).not.toHaveBeenCalled();
        expect(hasToast('error', REFUSAL)).toBe(true);
        expect(archivesState.activating).toBe(false);
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
    REBIND RELOADS PRESERVE UNLOGGED WORK (ADR 0085, rule 3 slice 1).
      V1  Every rebind reload first saves the draft through the injected
          preserver, and reloads only once that save has completed.
      V2  A failed save HOLDS the reload: the gate stays latched and the
          failure (reason + the record, for display) is kept.
      V3  Reload now, and a later automatic rebind, go through the same save;
          while it still fails nothing reloads.
      V4  Retry save that succeeds reloads and clears the failure.
      V5  Discard and reload reloads without saving.
      V6  The source handed to the preserver is the archive this page booted
          on; an unproven boot hands none (the preserver then holds).
      V7  A verified same-archive recovery retires the held rig reading; a
          rig reconnect to the SAME daemon instance does too, a different one
          does not.
*/
describe('rebind reloads preserve unlogged work', () => {
    const FAILED: PreserveResult = {
        kind: 'failed',
        record: sampleRecord(),
        reason: 'Browser storage did not keep it (QuotaExceededError).',
    };
    const SAVED: PreserveResult = { kind: 'saved', record: sampleRecord() };

    it('V1 the reload waits for the save', async () => {
        await loadArchives();
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        let finish: (r: PreserveResult) => void = () => {};
        const preserver = vi.fn(() => new Promise<PreserveResult>((r) => (finish = r)));
        setDraftPreserver(preserver);
        const run = verifyArchiveGeneration();
        for (let i = 0; i < 10; i++) await Promise.resolve();
        expect(preserver).toHaveBeenCalledTimes(1);
        expect(reloads).toBe(0);
        finish(SAVED);
        await run;
        expect(reloads).toBe(1);
    });

    it('V2 a failed save holds the reload, gated, with the failure kept', async () => {
        await loadArchives();
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        setDraftPreserver(() => Promise.resolve(FAILED));
        await verifyArchiveGeneration();
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(true);
        expect(archivesState.saveFailed?.reason).toMatch(/QuotaExceededError/);
        expect(archivesState.saveFailed?.record.fields.callsign).toBe('g0abc');
    });

    it('V3 Reload now and a later automatic rebind still hold while saving fails', async () => {
        await loadArchives();
        vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        const preserver = vi.fn(() => Promise.resolve(FAILED));
        setDraftPreserver(preserver);
        await activateArchive('b', () => true); // automatic: the new instance is seen
        expect(reloads).toBe(0);
        reloadNow();
        for (let i = 0; i < 10; i++) await Promise.resolve();
        expect(preserver).toHaveBeenCalledTimes(2);
        expect(reloads).toBe(0);
        expect(archivesState.saveFailed).not.toBeNull();
    });

    it('V4 a successful Retry save reloads and clears the failure', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        const preserver = vi
            .fn<() => Promise<PreserveResult>>()
            .mockResolvedValueOnce(FAILED)
            .mockResolvedValueOnce(SAVED);
        setDraftPreserver(preserver);
        await verifyArchiveGeneration();
        expect(reloads).toBe(0);
        await retrySave();
        expect(reloads).toBe(1);
        expect(archivesState.saveFailed).toBeNull();
    });

    it('V5 Discard and reload reloads without saving', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        const preserver = vi.fn(() => Promise.resolve(FAILED));
        setDraftPreserver(preserver);
        await verifyArchiveGeneration();
        discardAndReload();
        expect(preserver).toHaveBeenCalledTimes(1);
        expect(reloads).toBe(1);
    });

    it('V6 the source is the booted archive; an unproven page hands none', async () => {
        await loadArchives();
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        const preserver = vi.fn((_src: unknown) => Promise.resolve(SAVED));
        setDraftPreserver(preserver);
        await verifyArchiveGeneration();
        expect(preserver).toHaveBeenLastCalledWith({ archiveId: 'a', archiveLabel: 'Home' });

        _resetArchivesForTests();
        setDraftPreserver(preserver);
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i3', archiveId: 'b' });
        await verifyArchiveGeneration(); // no proven baseline: rebind
        expect(preserver).toHaveBeenLastCalledWith(null);
    });

    it('V8 every automatic reload path saves first and holds on failure', async () => {
        const preserver = vi.fn(() => Promise.resolve(FAILED));
        setDraftPreserver(preserver);

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
        expect(preserver).toHaveBeenCalledTimes(1);

        // The unproven-boot watch: the daemon answers again.
        _resetArchivesForTests();
        setDraftPreserver(preserver);
        vi.mocked(fetchDaemonIdentity).mockResolvedValue(null);
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        await bootArchiveScoped(() => Promise.resolve(0));
        for (let i = 0; i < 10; i++) await Promise.resolve();
        expect(preserver).toHaveBeenCalledTimes(2);

        // The boot bracket straddling a change.
        _resetArchivesForTests();
        setDraftPreserver(preserver);
        vi.mocked(fetchDaemonIdentity)
            .mockResolvedValueOnce({ instance: 'i1', archiveId: 'a' })
            .mockResolvedValueOnce({ instance: 'i2', archiveId: 'b' });
        await bootArchiveScoped(() => Promise.resolve(0));
        expect(preserver).toHaveBeenCalledTimes(3);

        expect(reloads).toBe(0);
        expect(archivesState.saveFailed).not.toBeNull();
    });

    // Review 2026-09-30: an identity answer that started before a later loss
    // replaced the held 14.255 MHz reading with the reconnect's 7.074 MHz.
    for (const [label, check] of [
        ['rig reconnect', retireSnapshotIfSameArchive],
        ['archive reconnect', verifyArchiveGeneration],
    ] as const) {
        it(`V9 ${label}: a late same-archive answer cannot retire a newer loss's reading`, async () => {
            _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
            rig.freq = '14.255.000';
            rig.band = '20m';
            rig.mode = 'USB';
            let reply: (v: { instance: string; archiveId: string }) => void = () => {};
            vi.mocked(fetchDaemonIdentity).mockReturnValue(new Promise((r) => (reply = r)));
            const pending = check();
            noteRigDrop(Date.parse('2026-09-30T12:00:00Z'));
            rig.freq = '7.074.000';
            rig.band = '40m';
            rig.mode = 'CW';
            reply({ instance: 'i1', archiveId: 'a' });
            await pending;
            expect(rigReadingForSave().freqHz).toBe(14_255_000);
        });
    }

    it('V7 a verified same-archive recovery retires the held rig reading', async () => {
        rig.freq = '14.255.000';
        noteRigDrop(Date.parse('2026-09-30T12:00:00Z'));
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'a' });
        await verifyArchiveGeneration();
        expect(rigReadingForSave().basis).toBe('when-saved');
    });

    it('V7 a rig reconnect to the same archive retires it (a restart included); another archive keeps it', async () => {
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'a' });
        noteRigDrop(Date.parse('2026-09-30T12:00:00Z'));
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'b' });
        await retireSnapshotIfSameArchive();
        expect(rigReadingForSave().basis).toBe('before-drop');
        vi.mocked(fetchDaemonIdentity).mockResolvedValue(null);
        await retireSnapshotIfSameArchive();
        expect(rigReadingForSave().basis).toBe('before-drop');
        vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i2', archiveId: 'a' });
        await retireSnapshotIfSameArchive();
        expect(rigReadingForSave().basis).toBe('when-saved');
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
