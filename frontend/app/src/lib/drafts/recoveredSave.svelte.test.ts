/*
    Restore commit 3 (ADR 0085 RS16–RS17a): a recovered QSO's edits are saved,
    Clear / Escape keep them, and the owning tab may Discard it.

      RS16a An edit is saved to the SAME record (its id), never a new one.
      RS16b One write in flight; edits made meanwhile are coalesced into the
            next write — no timer; the stored record ends with the latest edit.
      RS16c A failed save is shown and retried by the next edit.
      RS16d Installing the record writes nothing (no edit has happened).
      RS16e Restoring the same UUID after another owner edited it writes nothing.
      RS17a Clear waits for the latest edit to commit, then empties the form and
            releases the lock; the stored record holds the final edit.
      RS17b An edit made DURING Clear's final save is saved before the release.
      RS17c A failed final save keeps the form and the lock, and says so.
      RS17d Clear is refused while a submission is in flight.
      DA1   Discard in the owning tab settles the write in flight, deletes, and
            only then empties the form and releases; a late write cannot
            recreate the record.
      DA2   A failed delete keeps the record, the form and the lock.
      DA2b  Edits typed during a failed delete resume saving without a new edit.
      DA3   Discard is refused while a submission is in flight.
      CP1   A rig correction is saved as the record's rigCorrection; the original
            reading is kept unchanged.
      CP2   A correction alone is an edit; correcting back to the original
            saves "no correction".
      CP3   Restoring a record with a saved correction shows the corrected
            values, UNCONFIRMED — the confirmation is never saved.
      PR1   A rebind save with a recovered QSO on the form flushes to the owned
            record and creates no new one.
      PR2   If that flush fails the rebind save fails (the reload is held), and
            still no new record exists.
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import { draft, clearDraft, submitState } from '../operate/qso.svelte';
import { rig } from '../operate/rig.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { sampleRecord } from './savedDraft.fixture';
import { fakeDraftLocks } from './draftLock.fixture';
import { recovered, confirmRecoveredRig, correctRecoveredRig } from './recovered.svelte';
import { restoreSavedDraft, _resetRestoreForTests } from './restoreSession';
import type { RestoreEnv } from './restore';
import { discardSavedDraft, savedDrafts } from './savedDrafts.svelte';
import {
    clearRecovered,
    recoveredSave,
    requestClearDraft,
    _resetRecoveredSaveForTests,
} from './recoveredSave.svelte';
import { preserveDraft, _resetPreserveForTests } from './preserve';
import { _setDraftChannelForTests } from './draftChannel';
import type { SavedDraft } from './savedDraft';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

const env: RestoreEnv = {
    locksAvailable: true,
    onPhoneCw: true,
    bootArchiveId: 'arch-a',
    activeLogbookUuid: 'lb-a',
    currentAttribution: sampleRecord().attribution,
};
let mem = memoryDraftStore();
let locks = fakeDraftLocks();
let putsAfterSeed = 0;

/** A put the test releases by hand, to hold a write in flight. */
function holdPuts() {
    const real = mem.put.bind(mem);
    const waiting: Array<() => void> = [];
    const spy = vi.spyOn(mem, 'put').mockImplementation(async (r: SavedDraft) => {
        await new Promise<void>((go) => waiting.push(go));
        return real(r);
    });
    return { spy, release: () => waiting.shift()?.(), pending: () => waiting.length };
}

async function settle(): Promise<void> {
    for (let i = 0; i < 20; i++) await Promise.resolve();
    flushSync();
}

beforeEach(async () => {
    mem = memoryDraftStore();
    await mem.put(sampleRecord());
    // Count every write after the seed — a plain wrapper, so holdPuts and the
    // per-test spies still reach the real put underneath.
    putsAfterSeed = 0;
    const realPut = mem.put.bind(mem);
    mem.put = (r: SavedDraft) => {
        putsAfterSeed++;
        return realPut(r);
    };
    _setDraftStoreForTests(mem);
    _setDraftChannelForTests({ post() {}, subscribe: () => () => {} });
    locks = fakeDraftLocks();
    vi.stubGlobal('navigator', { locks: locks.manager });
    rig.mode = 'USB';
    flushSync();
    clearDraft();
    resetToasts();
    _resetRecoveredSaveForTests();
    _resetPreserveForTests();
    expect((await restoreSavedDraft(sampleRecord(), () => env)).ok).toBe(true);
    await settle();
});
afterEach(async () => {
    submitState.busy = false;
    _resetRecoveredSaveForTests();
    await _resetRestoreForTests();
    clearDraft();
    _setDraftStoreForTests(null);
    _setDraftChannelForTests(null);
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

const stored = (): SavedDraft => mem.rows.get('d-1')!;

describe('recovered edits are saved', () => {
    it('RS16d installing writes nothing', () => {
        expect(putsAfterSeed).toBe(0);
        expect(mem.rows.size).toBe(1);
        expect(stored().fields.notes).toBe('');
        expect(recoveredSave.error).toBe('');
    });

    it('RS16a an edit is saved to the same record', async () => {
        draft.notes = 'worked on 20m';
        await settle();
        expect(mem.rows.size).toBe(1);
        expect(stored().fields.notes).toBe('worked on 20m');
        expect(stored().attribution).toEqual(sampleRecord().attribution); // original kept
    });

    it('RS16e restoring the same UUID after another owner edited it writes nothing', async () => {
        expect(await clearRecovered()).toBe(true);
        const changed = structuredClone(stored());
        changed.fields.notes = 'Saved by another tab';
        await mem.put(changed);
        const put = vi.spyOn(mem, 'put');
        expect((await restoreSavedDraft(changed, () => env)).ok).toBe(true);
        await settle();
        expect(draft.notes).toBe('Saved by another tab');
        expect(put).not.toHaveBeenCalled();
    });

    it('RS16b one write in flight; later edits coalesce into the next', async () => {
        const puts = holdPuts();
        draft.notes = 'one';
        await settle();
        draft.notes = 'two';
        await settle();
        draft.notes = 'three';
        await settle();
        expect(puts.spy).toHaveBeenCalledTimes(1); // the first is still in flight
        puts.release();
        await settle();
        expect(puts.spy).toHaveBeenCalledTimes(2); // one more, with the latest
        puts.release();
        await settle();
        expect(stored().fields.notes).toBe('three');
    });

    it('RS16c a failed save is shown and retried by the next edit', async () => {
        vi.spyOn(mem, 'put').mockRejectedValueOnce(new Error('QuotaExceededError'));
        draft.notes = 'first';
        await settle();
        expect(recoveredSave.error).toMatch(/QuotaExceededError/);
        draft.notes = 'second';
        await settle();
        expect(recoveredSave.error).toBe('');
        expect(stored().fields.notes).toBe('second');
    });
});

describe('Clear and Escape on a recovered QSO', () => {
    it('RS17a waits for the latest edit, then empties and releases', async () => {
        const puts = holdPuts();
        draft.notes = 'final';
        await settle();
        const clearing = clearRecovered();
        await settle();
        expect(recovered.record).not.toBeNull(); // still waiting on the write
        expect(locks.held.size).toBe(1);
        puts.release();
        expect(await clearing).toBe(true);
        expect(stored().fields.notes).toBe('final');
        expect(recovered.record).toBeNull();
        expect(draft.callsign).toBe('');
        expect(locks.held.size).toBe(0);
    });

    it('RS17b an edit made during the final save is saved before the release', async () => {
        const puts = holdPuts();
        draft.notes = 'before';
        await settle();
        const clearing = clearRecovered();
        await settle();
        draft.notes = 'during'; // typed while Clear waits
        await settle();
        puts.release();
        await settle();
        puts.release();
        expect(await clearing).toBe(true);
        expect(stored().fields.notes).toBe('during');
        expect(locks.held.size).toBe(0);
    });

    it('RS17c a failed final save keeps the form and the lock', async () => {
        vi.spyOn(mem, 'put').mockRejectedValue(new Error('storage busy'));
        draft.notes = 'unsaved';
        await settle();
        expect(await clearRecovered()).toBe(false);
        expect(recovered.record).not.toBeNull();
        expect(draft.notes).toBe('unsaved');
        expect(locks.held.size).toBe(1);
        expect(recoveredSave.error).toMatch(/storage busy/);
    });

    it('RS17d Clear is refused while a submission is in flight', async () => {
        submitState.busy = true;
        expect(await clearRecovered()).toBe(false);
        expect(recovered.record).not.toBeNull();
        expect(locks.held.size).toBe(1);
    });

    it('requestClearDraft clears an ordinary form at once', async () => {
        expect(await clearRecovered()).toBe(true);
        draft.callsign = 'G0ABC';
        requestClearDraft();
        expect(draft.callsign).toBe('');
    });
});

describe('Discard in the owning tab', () => {
    it('DA1 settles the write in flight, deletes, then empties and releases', async () => {
        const puts = holdPuts();
        draft.notes = 'late';
        await settle();
        savedDrafts.list = [stored()];
        const discarding = discardSavedDraft('d-1');
        await settle();
        expect(mem.rows.has('d-1')).toBe(true); // the write is still in flight
        puts.release();
        expect(await discarding).toBe(true);
        await settle();
        expect(mem.rows.has('d-1')).toBe(false); // the late write did not recreate it
        expect(recovered.record).toBeNull();
        expect(draft.callsign).toBe('');
        expect(locks.held.size).toBe(0);
        draft.notes = 'after';
        await settle();
        expect(mem.rows.has('d-1')).toBe(false);
    });

    it('DA1b an edit typed while the delete is in flight starts no write', async () => {
        let finishRemove!: () => void;
        const realRemove = mem.remove.bind(mem);
        vi.spyOn(mem, 'remove').mockImplementation(async (id: string) => {
            await new Promise<void>((go) => (finishRemove = go));
            return realRemove(id);
        });
        const put = vi.spyOn(mem, 'put');
        const discarding = discardSavedDraft('d-1');
        await settle();
        draft.notes = 'typed during the delete';
        await settle();
        expect(put).not.toHaveBeenCalled();
        finishRemove();
        expect(await discarding).toBe(true);
        await settle();
        expect(mem.rows.has('d-1')).toBe(false);
    });

    it('DA2 a failed delete keeps the record, the form and the lock', async () => {
        vi.spyOn(mem, 'remove').mockRejectedValueOnce(new Error('storage busy'));
        expect(await discardSavedDraft('d-1')).toBe(false);
        expect(mem.rows.has('d-1')).toBe(true);
        expect(recovered.record).not.toBeNull();
        expect(locks.held.size).toBe(1);
        draft.notes = 'still saving';
        await settle();
        expect(stored().fields.notes).toBe('still saving');
    });

    it('DA2b an edit during a failed Discard resumes saving without another keystroke', async () => {
        let failDelete!: (error: Error) => void;
        let entered!: () => void;
        const deleting = new Promise<void>((resolve) => {
            entered = resolve;
        });
        vi.spyOn(mem, 'remove').mockImplementationOnce(() => {
            entered();
            return new Promise<void>((_resolve, reject) => {
                failDelete = reject;
            });
        });
        const discarding = discardSavedDraft('d-1');
        await deleting;
        draft.notes = 'Typed while Discard was waiting';
        await settle();
        failDelete(new Error('Delete failed'));
        expect(await discarding).toBe(false);
        await settle();
        expect(recovered.record).not.toBeNull();
        expect(locks.held.size).toBe(1);
        expect(recoveredSave.error).toBe('');
        expect(stored().fields.notes).toBe('Typed while Discard was waiting');
    });

    it('DA3 Discard is refused while a submission is in flight', async () => {
        submitState.busy = true;
        expect(await discardSavedDraft('d-1')).toBe(false);
        expect(mem.rows.has('d-1')).toBe(true);
        expect(recovered.record).not.toBeNull();
    });
});

describe('a rebind save with a recovered QSO on the form', () => {
    const station = {
        logbookUuid: 'lb-a',
        logbookId: 1,
        logbookName: 'Home log',
        stationCallsign: '7Q5MLV',
        operator: '7Q5MLV',
        myGrid: 'KH66',
        attribution: { myRig: 'TODAY', operator: 'TODAY', myName: 'TODAY' },
    };

    it('PR1 flushes to the owned record and creates no new one', async () => {
        draft.notes = 'typed before the switch';
        const out = await preserveDraft({ archiveId: 'arch-a', archiveLabel: 'Home' }, station);
        expect(out.kind).toBe('saved');
        expect(mem.rows.size).toBe(1);
        expect(stored().fields.notes).toBe('typed before the switch');
        expect(stored().attribution).toEqual(sampleRecord().attribution); // not today's
    });

    it('PR2 a failed flush fails the save, and still no new record', async () => {
        vi.spyOn(mem, 'put').mockRejectedValue(new Error('storage busy'));
        draft.notes = 'typed before the switch';
        const out = await preserveDraft({ archiveId: 'arch-a', archiveLabel: 'Home' }, station);
        expect(out.kind).toBe('failed');
        expect(mem.rows.size).toBe(1);
    });
});

describe('rig corrections are saved', () => {
    it('CP1 a correction is saved apart from the original reading', async () => {
        correctRecoveredRig({ freqHz: 14_200_000 });
        await settle();
        expect(stored().rigCorrection?.freqHz).toBe(14_200_000);
        expect(stored().rig).toEqual(sampleRecord().rig);
        expect(stored().fields).toEqual(sampleRecord().fields);
    });

    it('CP2 correcting back to the original saves no correction', async () => {
        correctRecoveredRig({ freqHz: 14_200_000 });
        await settle();
        correctRecoveredRig({ freqHz: 14_255_000 });
        await settle();
        expect(stored().rigCorrection).toBeNull();
    });

    it('CP3 a saved correction comes back unconfirmed on the next Restore', async () => {
        correctRecoveredRig({ freqHz: 14_200_000 });
        expect(confirmRecoveredRig()).toBe(true);
        await settle();
        expect(await clearRecovered()).toBe(true);
        expect(JSON.stringify(stored())).not.toMatch(/confirm/i);
        const again = await restoreSavedDraft(stored(), () => env);
        expect(again.ok).toBe(true);
        expect(recovered.rig?.freqHz).toBe(14_200_000);
        expect(recovered.record?.rig.freqHz).toBe(14_255_000); // provenance kept
        expect(recovered.confirmed).toBe(false);
    });
});
