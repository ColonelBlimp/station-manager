/*
    The owning tab's Saved QSOs list follows its own recovered edits (review
    2026-10-03; ADR 0086). Other tabs re-read on the change notice; THIS tab's
    list must be refreshed by the save itself — otherwise Details / Copy show
    the old values, and a later Restore from that entry is refused as changed.

      SL1 After an edit and a correction are saved and the QSO is cleared, the
          local list holds the stored values.
      SL2 The list then offers a record that restores.
      SL3 A flush resolves only once that local re-read has landed.
      SL4 A re-read superseded by a newer one does not count: Clear waits for
          the newest read, and only then lets the QSO go (review 2026-10-03).
      SL5 A failed re-read does not count: Clear retries it once; if the list
          still cannot be refreshed it keeps the QSO and says the LIST could not
          be refreshed — never that the edit is unsaved — and a later Clear
          retries again without another edit.
      SL6 A rebind save needs only the committed write: a failed list refresh
          does not hold the reload.
      SL7 Nor does a PENDING list refresh: the rebind save resolves once the
          write commits, while the list read is still outstanding.
      SL8 "Saved, but the list…" never stands beside a newer unsaved edit: a
          later write failure clears it.
      SL9 Nor can an OLDER refresh, failing late, bring it back while a newer
          edit is unsaved.
      SL10 Clear finishes once the latest edit and ITS list refresh have landed,
          even while an obsolete earlier read is still pending.
      SL11 The same for Clear's own retry: a newer edit's refresh that lands
          releases Clear while the retry read is still pending.
      SL12 A list read made for another reason (visibility, another tab's
          notice) that lands after the refresh began also releases Clear.
*/
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import { draft, clearDraft } from '../operate/qso.svelte';
import { rig } from '../operate/rig.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { _setDraftChannelForTests } from './draftChannel';
import { fakeDraftLocks } from './draftLock.fixture';
import { sampleRecord } from './savedDraft.fixture';
import { correctRecoveredRig, recovered } from './recovered.svelte';
import { restoreSavedDraft, _resetRestoreForTests } from './restoreSession';
import {
    clearRecovered,
    flushRecoveredEdits,
    recoveredSave,
    _resetRecoveredSaveForTests,
} from './recoveredSave.svelte';
import {
    loadSavedDrafts,
    savedDrafts,
    watchSavedDrafts,
    _resetSavedDraftsForTests,
} from './savedDrafts.svelte';
import { preserveDraft, _resetPreserveForTests } from './preserve';

let mem = memoryDraftStore();
let stop: () => void = () => {};
const env = {
    locksAvailable: true,
    onPhoneCw: true,
    bootArchiveId: 'arch-a',
    activeLogbookUuid: 'lb-a',
    currentAttribution: sampleRecord().attribution,
};

beforeEach(async () => {
    mem = memoryDraftStore();
    await mem.put(sampleRecord());
    _setDraftStoreForTests(mem);
    // Posting is not delivery: no notice comes back to this tab.
    _setDraftChannelForTests({ post() {}, subscribe: () => () => {} });
    vi.stubGlobal('navigator', { locks: fakeDraftLocks().manager });
    rig.mode = 'USB';
    flushSync();
    clearDraft();
    _resetSavedDraftsForTests();
    _resetRecoveredSaveForTests();
    stop = watchSavedDrafts();
    await vi.waitFor(() => expect(savedDrafts.list).toHaveLength(1));
    expect((await restoreSavedDraft(savedDrafts.list[0], () => env)).ok).toBe(true);
    flushSync();
});
afterEach(async () => {
    stop();
    _resetRecoveredSaveForTests();
    await _resetRestoreForTests();
    _setDraftStoreForTests(null);
    _setDraftChannelForTests(null);
    _resetSavedDraftsForTests();
    vi.unstubAllGlobals();
});

it('SL1 the local list holds what was stored', async () => {
    correctRecoveredRig({ freqHz: 14_200_000 });
    draft.notes = 'latest edit';
    expect(await flushRecoveredEdits()).toBe(true);
    expect(await clearRecovered()).toBe(true);
    expect(mem.rows.get('d-1')?.rigCorrection?.freqHz).toBe(14_200_000);
    expect(savedDrafts.list[0].rigCorrection?.freqHz).toBe(14_200_000);
    expect(savedDrafts.list[0].fields.notes).toBe('latest edit');
});

it('SL2 the list then offers a record that restores', async () => {
    correctRecoveredRig({ freqHz: 14_200_000 });
    expect(await clearRecovered()).toBe(true);
    expect(await restoreSavedDraft(savedDrafts.list[0], () => env)).toEqual({ ok: true });
});

it('SL3 a flush waits for the local re-read', async () => {
    let finishList!: (rows: unknown[]) => void;
    const realList = mem.list.bind(mem);
    vi.spyOn(mem, 'list').mockImplementationOnce(
        () => new Promise((resolve) => (finishList = (rows) => resolve(rows)))
    );
    draft.notes = 'held read';
    let done = false;
    const flushing = flushRecoveredEdits().then((ok) => {
        done = true;
        return ok;
    });
    await vi.waitFor(() => expect(finishList).toBeTypeOf('function'));
    await Promise.resolve();
    expect(done).toBe(false); // the list is not current yet
    finishList(await realList());
    expect(await flushing).toBe(true);
    expect(savedDrafts.list[0].fields.notes).toBe('held read');
});

it('SL4 a superseded re-read makes Clear wait for the newest read', async () => {
    let finishOwn!: (rows: unknown[]) => void;
    let finishNewer!: (rows: unknown[]) => void;
    const realList = mem.list.bind(mem);
    vi.spyOn(mem, 'list')
        .mockImplementationOnce(() => new Promise((resolve) => (finishOwn = resolve)))
        .mockImplementationOnce(() => new Promise((resolve) => (finishNewer = resolve)));
    correctRecoveredRig({ freqHz: 14_200_000 });
    let clearDone = false;
    const clearing = clearRecovered().then((ok) => {
        clearDone = true;
        return ok;
    });
    await vi.waitFor(() => expect(finishOwn).toBeTypeOf('function'));
    const laterRead = loadSavedDrafts(); // a visibility or other-tab re-read
    finishOwn(await realList());
    for (let i = 0; i < 30; i++) await Promise.resolve();
    flushSync();
    expect(clearDone).toBe(false);
    expect(recovered.record).not.toBeNull();
    finishNewer(await realList());
    await laterRead;
    expect(await clearing).toBe(true);
    expect(savedDrafts.list[0].rigCorrection?.freqHz).toBe(14_200_000);
    expect(recovered.record).toBeNull();
});

it('SL5 a failed re-read keeps the QSO, says the list is stale, and Clear retries', async () => {
    // Two reads fail: the write's own refresh, and Clear's retry of it.
    vi.spyOn(mem, 'list')
        .mockRejectedValueOnce(new Error('first failed read'))
        .mockRejectedValueOnce(new Error('second failed read'));
    correctRecoveredRig({ freqHz: 14_200_000 });
    expect(await clearRecovered()).toBe(false);
    expect(mem.rows.get('d-1')?.rigCorrection?.freqHz).toBe(14_200_000); // committed
    expect(recovered.record).not.toBeNull();
    expect(recoveredSave.error).toBe(''); // the edit is NOT reported unsaved
    expect(recoveredSave.listError).toMatch(/Saved QSOs list could not be refreshed/);
    // No further edit: the next Clear refreshes the list and lets the QSO go.
    expect(await clearRecovered()).toBe(true);
    expect(savedDrafts.list[0].rigCorrection?.freqHz).toBe(14_200_000);
    expect(recoveredSave.listError).toBe('');
});

it('SL6 a failed list refresh does not hold a rebind save', async () => {
    _resetPreserveForTests();
    vi.spyOn(mem, 'list').mockRejectedValue(new Error('reads failing'));
    draft.notes = 'typed before the switch';
    const out = await preserveDraft(
        { archiveId: 'arch-a', archiveLabel: 'Home' },
        {
            logbookUuid: 'lb-a',
            logbookId: 1,
            logbookName: 'Home log',
            stationCallsign: '7Q5MLV',
            operator: '7Q5MLV',
            myGrid: 'KH66',
            attribution: null,
        }
    );
    expect(out.kind).toBe('saved');
    expect(mem.rows.get('d-1')?.fields.notes).toBe('typed before the switch');
});

it('SL7 a pending list refresh does not hold a rebind save', async () => {
    _resetPreserveForTests();
    let finishRead!: (rows: unknown[]) => void;
    const realList = mem.list.bind(mem);
    vi.spyOn(mem, 'list').mockImplementationOnce(
        () => new Promise((resolve) => (finishRead = resolve))
    );
    draft.notes = 'committed before reload';
    let saveDone = false;
    const saving = preserveDraft(
        { archiveId: 'arch-a', archiveLabel: 'Home' },
        {
            logbookUuid: 'lb-a',
            logbookId: 1,
            logbookName: 'Home log',
            stationCallsign: '7Q5MLV',
            operator: '7Q5MLV',
            myGrid: 'KH66',
            attribution: null,
        }
    ).then((out) => {
        saveDone = true;
        return out;
    });
    await vi.waitFor(() => expect(finishRead).toBeTypeOf('function'));
    expect(mem.rows.get('d-1')?.fields.notes).toBe('committed before reload');
    for (let i = 0; i < 30; i++) await Promise.resolve();
    const savedBeforeReadSettled = saveDone;
    finishRead(await realList());
    expect((await saving).kind).toBe('saved');
    expect(savedBeforeReadSettled).toBe(true);
});

it('SL8 a later write failure clears "Saved, but the list…"', async () => {
    vi.spyOn(mem, 'list').mockRejectedValueOnce(new Error('list unavailable'));
    draft.notes = 'first stored edit';
    flushSync();
    await vi.waitFor(() => expect(recoveredSave.listError).toMatch(/^Saved, but/));
    vi.spyOn(mem, 'put').mockRejectedValueOnce(new Error('QuotaExceededError'));
    draft.notes = 'second unsaved edit';
    flushSync();
    await vi.waitFor(() => expect(recoveredSave.error).toMatch(/QuotaExceededError/));
    expect(mem.rows.get('d-1')?.fields.notes).toBe('first stored edit');
    expect(recoveredSave.listError).toBe('');
});

it('SL9 an older refresh failing late cannot restore "Saved, but…"', async () => {
    let failOldRead!: (error: Error) => void;
    vi.spyOn(mem, 'list').mockImplementationOnce(
        () => new Promise((_resolve, reject) => (failOldRead = reject))
    );
    draft.notes = 'first stored edit';
    flushSync();
    await vi.waitFor(() => expect(failOldRead).toBeTypeOf('function'));
    vi.spyOn(mem, 'put').mockRejectedValueOnce(new Error('QuotaExceededError'));
    draft.notes = 'second unsaved edit';
    flushSync();
    await vi.waitFor(() => expect(recoveredSave.error).toMatch(/QuotaExceededError/));
    failOldRead(new Error('older list read failed late'));
    for (let i = 0; i < 30; i++) await Promise.resolve();
    flushSync();
    expect(savedDrafts.error).toBe('older list read failed late');
    expect(recoveredSave.error).toMatch(/QuotaExceededError/);
    expect(recoveredSave.listError).toBe('');
});

it('SL10 Clear does not wait on an obsolete read', async () => {
    let finishOldRead!: (rows: unknown[]) => void;
    const realList = mem.list.bind(mem);
    vi.spyOn(mem, 'list').mockImplementationOnce(
        () => new Promise((resolve) => (finishOldRead = resolve))
    );
    draft.notes = 'first edit';
    let clearDone = false;
    const clearing = clearRecovered().then((ok) => {
        clearDone = true;
        return ok;
    });
    await vi.waitFor(() => expect(finishOldRead).toBeTypeOf('function'));
    for (let i = 0; i < 30; i++) await Promise.resolve();
    draft.notes = 'latest edit';
    flushSync();
    await vi.waitFor(() => expect(savedDrafts.list[0].fields.notes).toBe('latest edit'));
    for (let i = 0; i < 30; i++) await Promise.resolve();
    const finishedBeforeOldRead = clearDone;
    finishOldRead(await realList());
    expect(await clearing).toBe(true);
    expect(finishedBeforeOldRead).toBe(true);
});

it('SL11 a pending retry read does not hold Clear once a newer refresh lands', async () => {
    let finishRetry!: (rows: unknown[]) => void;
    const realList = mem.list.bind(mem);
    vi.spyOn(mem, 'list')
        .mockRejectedValueOnce(new Error('initial refresh failed'))
        .mockImplementationOnce(() => new Promise((resolve) => (finishRetry = resolve)));
    draft.notes = 'first edit';
    let clearDone = false;
    const clearing = clearRecovered().then((ok) => {
        clearDone = true;
        return ok;
    });
    await vi.waitFor(() => expect(finishRetry).toBeTypeOf('function'));
    draft.notes = 'latest edit';
    flushSync();
    await vi.waitFor(() => expect(savedDrafts.list[0].fields.notes).toBe('latest edit'));
    for (let i = 0; i < 30; i++) await Promise.resolve();
    const finishedBeforeObsoleteRetry = clearDone;
    finishRetry(await realList());
    expect(await clearing).toBe(true);
    expect(finishedBeforeObsoleteRetry).toBe(true);
});

it('SL12 a newer read for another reason releases Clear', async () => {
    let finishOldRead!: (rows: unknown[]) => void;
    const realList = mem.list.bind(mem);
    vi.spyOn(mem, 'list').mockImplementationOnce(
        () => new Promise((resolve) => (finishOldRead = resolve))
    );
    draft.notes = 'stored edit';
    let clearDone = false;
    const clearing = clearRecovered().then((ok) => {
        clearDone = true;
        return ok;
    });
    await vi.waitFor(() => expect(finishOldRead).toBeTypeOf('function'));
    for (let i = 0; i < 30; i++) await Promise.resolve();
    expect(await loadSavedDrafts()).toBe('applied'); // a visibility / channel read
    for (let i = 0; i < 30; i++) await Promise.resolve();
    expect(clearDone).toBe(true); // released before the old read answers
    finishOldRead(await realList());
    expect(await clearing).toBe(true);
});
