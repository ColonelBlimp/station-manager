/*
    Saved QSOs stay in step across this browser's tabs (operator finding and
    ruling 2026-09-30: a QSO discarded in one tab stayed listed in another).

      S1  A committed discard is announced; other tabs re-read the list and the
          entry leaves them. A failed discard announces nothing.
      S2  A committed save is announced; a failed save announces nothing.
      S3  An announcement from another tab re-reads this tab's list.
      S4  A tab becoming visible re-reads its list (a missed announcement).
      S5  An older read cannot resurrect a discarded entry: a read that began
          before the discard and ends after it is dropped.
      S6  Overlapping reads: only the newest read's result is applied.
      S7  A failed re-read keeps the list shown and says so.
      S8  Only a committed save leaves the one-time announcement for the tab's
          reload (ADR 0086); a failed save leaves none.
    These announcements are change notices only — they do not give any tab
    ownership of a record (Restore's exclusive ownership is separate).
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { draft, clearDraft } from '../operate/qso.svelte';
import { _resetRigSnapshotForTests } from '../operate/rigSnapshot.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { _setDraftChannelForTests, type DraftChannel } from './draftChannel';
import {
    consumePreservedAnnouncement,
    discardSavedDraft,
    loadSavedDrafts,
    savedDrafts,
    watchSavedDrafts,
    _resetSavedDraftsForTests,
} from './savedDrafts.svelte';
import { preserveDraft, _resetPreserveForTests } from './preserve';
import { sampleRecord } from './savedDraft.fixture';

/** A fake channel: counts this tab's announcements; `deliver` plays one
 *  arriving from another tab. */
function fakeChannel(): DraftChannel & { posts: number; deliver: () => void } {
    const listeners = new Set<() => void>();
    return {
        posts: 0,
        post() {
            this.posts++;
        },
        subscribe(fn) {
            listeners.add(fn);
            return () => listeners.delete(fn);
        },
        deliver() {
            for (const fn of listeners) fn();
        },
    };
}

let mem = memoryDraftStore();
let chan = fakeChannel();
let stop: () => void = () => {};

beforeEach(() => {
    mem = memoryDraftStore();
    chan = fakeChannel();
    _setDraftStoreForTests(mem);
    _setDraftChannelForTests(chan);
    _resetSavedDraftsForTests();
    _resetPreserveForTests();
    _resetRigSnapshotForTests();
    clearDraft();
});
afterEach(() => {
    stop();
    stop = () => {};
    _setDraftStoreForTests(null);
    _setDraftChannelForTests(null);
    vi.restoreAllMocks();
    clearDraft();
});

const OTHER = () => sampleRecord({ id: 'd-2', savedAt: '2026-09-30T12:20:00.000Z' });

describe('saved QSOs across tabs', () => {
    it('S1 a committed discard is announced; a failed one is not', async () => {
        await mem.put(sampleRecord());
        await mem.put(OTHER());
        await loadSavedDrafts();
        expect(await discardSavedDraft('d-1')).toBe(true);
        expect(chan.posts).toBe(1);
        vi.spyOn(mem, 'remove').mockRejectedValueOnce(new Error('busy'));
        expect(await discardSavedDraft('d-2')).toBe(false);
        expect(chan.posts).toBe(1);
    });

    it('S2 a committed save is announced; a failed one is not', async () => {
        const station = {
            logbookUuid: 'lb-a',
            logbookId: 1,
            logbookName: 'Home log',
            stationCallsign: '7Q5MLV',
            operator: '7Q5MLV',
            myGrid: 'KH66',
            attribution: null,
        };
        draft.callsign = 'G0ABC';
        vi.spyOn(mem, 'put').mockRejectedValueOnce(new Error('QuotaExceededError'));
        expect((await preserveDraft({ archiveId: 'a', archiveLabel: 'Home' }, station)).kind).toBe(
            'failed'
        );
        expect(chan.posts).toBe(0);
        expect((await preserveDraft({ archiveId: 'a', archiveLabel: 'Home' }, station)).kind).toBe(
            'saved'
        );
        expect(chan.posts).toBe(1);
    });

    it('S3 another tab’s announcement re-reads the list', async () => {
        await mem.put(sampleRecord());
        stop = watchSavedDrafts();
        await vi.waitFor(() => expect(savedDrafts.list.map((r) => r.id)).toEqual(['d-1']));
        await mem.remove('d-1'); // discarded in another tab…
        await mem.put(OTHER()); // …and another saved there
        chan.deliver();
        await vi.waitFor(() => expect(savedDrafts.list.map((r) => r.id)).toEqual(['d-2']));
    });

    it('S4 becoming visible re-reads the list', async () => {
        stop = watchSavedDrafts();
        await vi.waitFor(() => expect(savedDrafts.list).toEqual([]));
        await mem.put(sampleRecord());
        Object.defineProperty(document, 'visibilityState', {
            value: 'visible',
            configurable: true,
        });
        document.dispatchEvent(new Event('visibilitychange'));
        await vi.waitFor(() => expect(savedDrafts.list.map((r) => r.id)).toEqual(['d-1']));
    });

    it('S5 an older read cannot resurrect a discarded entry', async () => {
        await mem.put(sampleRecord());
        await loadSavedDrafts();
        let finishOld: (rows: unknown[]) => void = () => {};
        const stale = [sampleRecord()];
        vi.spyOn(mem, 'list').mockImplementationOnce(() => new Promise((r) => (finishOld = r)));
        const older = loadSavedDrafts(); // e.g. a visibility re-read, still waiting
        expect(await discardSavedDraft('d-1')).toBe(true);
        finishOld(stale); // it answers with the record as it was before the discard
        await older;
        await vi.waitFor(() => expect(savedDrafts.list).toEqual([]));
    });

    it('S6 only the newest of overlapping reads is applied', async () => {
        let finishFirst: (rows: unknown[]) => void = () => {};
        vi.spyOn(mem, 'list')
            .mockImplementationOnce(() => new Promise((r) => (finishFirst = r)))
            .mockResolvedValueOnce([OTHER()]);
        const first = loadSavedDrafts();
        await loadSavedDrafts();
        finishFirst([sampleRecord()]);
        await first;
        expect(savedDrafts.list.map((r) => r.id)).toEqual(['d-2']);
    });

    it('S7 a failed re-read keeps the list and says so', async () => {
        await mem.put(sampleRecord());
        await loadSavedDrafts();
        vi.spyOn(mem, 'list').mockRejectedValueOnce(new Error('the storage read failed'));
        await loadSavedDrafts();
        expect(savedDrafts.list.map((r) => r.id)).toEqual(['d-1']);
        expect(savedDrafts.error).toMatch(/storage read failed/);
    });

    it('S8 only a committed save leaves the one-time announcement', async () => {
        const station = {
            logbookUuid: 'lb-a',
            logbookId: 1,
            logbookName: 'Home log',
            stationCallsign: '7Q5MLV',
            operator: '7Q5MLV',
            myGrid: 'KH66',
            attribution: null,
        };
        sessionStorage.clear();
        draft.callsign = 'G0ABC';
        vi.spyOn(mem, 'put').mockRejectedValueOnce(new Error('QuotaExceededError'));
        await preserveDraft({ archiveId: 'a', archiveLabel: 'Home' }, station);
        expect(consumePreservedAnnouncement()).toBeNull();
        await preserveDraft({ archiveId: 'a', archiveLabel: 'Home' }, station);
        expect(consumePreservedAnnouncement()?.message).toMatch(/Not logged — QSO from ‘Home’/);
        expect(consumePreservedAnnouncement()).toBeNull(); // once
    });
});
