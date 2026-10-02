// RS9 rechecks both asynchronous boundaries; RS10 ownership outlives views.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import { draft, clearDraft, setEntryGate, submitState } from '../operate/qso.svelte';
import { rig } from '../operate/rig.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { sampleRecord } from './savedDraft.fixture';
import { fakeDraftLocks } from './draftLock.fixture';
import { recovered } from './recovered.svelte';
import { restoreSavedDraft, _resetRestoreForTests } from './restoreSession';
import type { RestoreEnv } from './restore';
import { discardSavedDraft, loadSavedDrafts, savedDrafts } from './savedDrafts.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';

const makeEnv = (): RestoreEnv => ({
    locksAvailable: true,
    onPhoneCw: true,
    bootArchiveId: 'arch-a',
    activeLogbookUuid: 'lb-a',
    currentAttribution: sampleRecord().attribution,
});
let mem = memoryDraftStore();
let locks = fakeDraftLocks();
let env = makeEnv();
beforeEach(async () => {
    mem = memoryDraftStore();
    await mem.put(sampleRecord());
    _setDraftStoreForTests(mem);
    locks = fakeDraftLocks();
    vi.stubGlobal('navigator', { locks: locks.manager });
    env = makeEnv();
    rig.mode = 'USB';
    flushSync();
    clearDraft();
    resetToasts();
});
afterEach(async () => {
    await _resetRestoreForTests();
    clearDraft();
    setEntryGate(null);
    submitState.busy = false;
    _setDraftStoreForTests(null);
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

describe('Restore reservation and recheck', () => {
    it('C1 a failed read releases ownership and keeps the form', async () => {
        const before = { ...draft };
        vi.spyOn(mem, 'list').mockRejectedValueOnce(new Error('storage unavailable'));
        const result = await restoreSavedDraft(sampleRecord(), () => env);
        expect(result.ok).toBe(false);
        if (!result.ok) expect(result.reason).toContain('storage unavailable');
        expect(draft).toEqual(before);
        expect(recovered.record).toBeNull();
        expect(locks.held.size).toBe(0);
    });
    it('C2 fills every saved field only while holding the UUID lock', async () => {
        const read = vi.spyOn(mem, 'list').mockImplementation(() => {
            expect(locks.held.size).toBe(1);
            return Promise.resolve([sampleRecord()]);
        });
        expect((await restoreSavedDraft(sampleRecord(), () => env)).ok).toBe(true);
        flushSync();
        expect(read).toHaveBeenCalledOnce();
        expect(draft).toEqual(sampleRecord().fields);
        expect(recovered.record?.id).toBe('d-1');
        expect(locks.held.size).toBe(1);
    });
    it.each(['typed', 'archive', 'logbook', 'rig', 'unread', 'page', 'entry', 'submit'])(
        'C3 %s changing during the storage read refuses without filling',
        async (change) => {
            let finish!: (rows: unknown[]) => void;
            let entered!: () => void;
            const reading = new Promise<void>((resolve) => {
                entered = resolve;
            });
            vi.spyOn(mem, 'list').mockImplementationOnce(() => {
                entered();
                return new Promise((resolve) => {
                    finish = resolve;
                });
            });
            const attempt = restoreSavedDraft(sampleRecord(), () => env);
            await reading;
            if (change === 'typed') draft.notes = 'Entered while waiting';
            if (change === 'archive') env.bootArchiveId = 'arch-b';
            if (change === 'logbook') env.activeLogbookUuid = 'other';
            if (change === 'rig')
                env.currentAttribution = { ...env.currentAttribution!, myRig: 'Other rig' };
            if (change === 'unread') env.currentAttribution = null;
            if (change === 'page') env.onPhoneCw = false;
            if (change === 'entry') setEntryGate(() => 'Archive switching');
            if (change === 'submit') submitState.busy = true;
            const before = { ...draft };
            finish([sampleRecord()]);
            expect((await attempt).ok).toBe(false);
            expect(draft).toEqual(before);
            expect(recovered.record).toBeNull();
            expect(locks.held.size).toBe(0);
        }
    );
    it('C4 entry made before the lock callback is preserved', async () => {
        const attempt = restoreSavedDraft(sampleRecord(), () => env);
        draft.callsign = 'ZS6BOS';
        expect((await attempt).ok).toBe(false);
        expect(draft.callsign).toBe('ZS6BOS');
        expect(locks.held.size).toBe(0);
    });
    it.each(['removed', 'damaged', 'changed', 'logged'])(
        'C5 a %s record releases and refuses',
        async (change) => {
            const current = sampleRecord();
            if (change === 'changed') current.fields.notes = 'Newer edit';
            if (change === 'logged') {
                current.state = 'logged';
                current.loggedQsoUuid = 'qso-1';
            }
            vi.spyOn(mem, 'list').mockResolvedValueOnce(
                change === 'removed' ? [] : change === 'damaged' ? [{ id: 'd-1' }] : [current]
            );
            expect((await restoreSavedDraft(sampleRecord(), () => env)).ok).toBe(false);
            expect(draft.callsign).toBe('');
            expect(locks.held.size).toBe(0);
        }
    );
    it('C6 another Restore in this tab cannot replace an existing or pending recovery', async () => {
        const first = restoreSavedDraft(sampleRecord(), () => env);
        expect((await restoreSavedDraft(sampleRecord(), () => env)).ok).toBe(false);
        expect((await first).ok).toBe(true);
        expect((await restoreSavedDraft(sampleRecord(), () => env)).ok).toBe(false);
        expect(locks.request).toHaveBeenCalledOnce();
    });
    it('C7 a foreign Discard refuses while held; releases only after deletion commits', async () => {
        const { reserveSavedDraft } = await import('./draftLock');
        const owner = await reserveSavedDraft('d-1');
        await loadSavedDrafts();
        expect(await discardSavedDraft('d-1')).toBe(false);
        expect(mem.rows.has('d-1')).toBe(true);
        expect(savedDrafts.list).toHaveLength(1);
        expect(JSON.stringify(toastsState.items)).toContain('In use in another tab');
        await owner.release();
        let finish!: () => void;
        let entered!: () => void;
        const deleting = new Promise<void>((resolve) => {
            entered = resolve;
        });
        const remove = mem.remove.bind(mem);
        vi.spyOn(mem, 'remove').mockImplementationOnce(async (id) => {
            entered();
            await new Promise<void>((resolve) => {
                finish = resolve;
            });
            await remove(id);
        });
        const discard = discardSavedDraft('d-1');
        await deleting;
        expect(locks.held.size).toBe(1);
        await expect(reserveSavedDraft('d-1')).rejects.toThrow('In use in another tab');
        finish();
        expect(await discard).toBe(true);
        expect(mem.rows.has('d-1')).toBe(false);
        expect(locks.held.size).toBe(0);
    });
    it('C9 an occupied UUID and a denied API keep the form and explain the refusal', async () => {
        const { reserveSavedDraft } = await import('./draftLock');
        const owner = await reserveSavedDraft('d-1');
        const before = { ...draft };
        expect(await restoreSavedDraft(sampleRecord(), () => env)).toEqual({
            ok: false,
            reason: 'In use in another tab.',
        });
        expect(draft).toEqual(before);
        await owner.release();
        locks.request.mockRejectedValueOnce(new Error('Access denied'));
        expect(await restoreSavedDraft(sampleRecord(), () => env)).toEqual({
            ok: false,
            reason: 'Access denied',
        });
        expect(draft).toEqual(before);
        vi.stubGlobal('navigator', {});
        const unavailable = await restoreSavedDraft(sampleRecord(), () => env);
        expect(unavailable.ok).toBe(false);
        if (!unavailable.ok) expect(unavailable.reason).toContain('read and copy only');
    });
    it('C8 a failed delete releases the lock and a missing API preserves legacy Discard', async () => {
        vi.spyOn(mem, 'remove').mockRejectedValueOnce(new Error('disk full'));
        expect(await discardSavedDraft('d-1')).toBe(false);
        expect(locks.held.size).toBe(0);
        expect(mem.rows.has('d-1')).toBe(true);
        vi.stubGlobal('navigator', {});
        expect(await discardSavedDraft('d-1')).toBe(true);
    });
});
