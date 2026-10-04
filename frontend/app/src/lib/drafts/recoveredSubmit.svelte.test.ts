/*
    Logging a recovered QSO (ADR 0085 RS18–RS25; Restore commit 4).

      LG1  Gates: archive gate, source archive and logbook UUID, ownership,
           confirmed rig values, validation, the in-flight latch, attribution
           present — but NOT the CAT link.
      LG2  The EXACT request — destination, force flag, ADIF and expected
           attribution — is stored before it is sent.
      LG3  If that store fails, nothing is sent and memory claims no attempt.
      LG4  Confirmed: the record is deleted, the form emptied, the lock
           released; the request carried the saved values and expectation.
      LG5  Confirmed but the delete fails: a terminal "logged" record with the
           QSO UUID is written instead, and the QSO is let go.
      LG6  Confirmed but both cleanup writes fail: the UUID is shown, this tab
           cannot submit again, the lock is kept, and a cleanup retry cleans
           without sending.
      LG7  A transport failure, a malformed success and an abort are AMBIGUOUS:
           the stored attempt stays, the outcome reads unknown, no retry.
      LG8  A definite refusal after an earlier unknown outcome keeps it unknown;
           with no earlier uncertainty it restores "not logged".
      LG9  A duplicate keeps the form and the record and names the existing
           QSO; "Log anyway" is only an explicit, forced, separate contact.
      LG10 attribution_changed (409) is a definite refusal, shown verbatim.
      LG11 An unknown-outcome record needs the explicit confirmation; Cancel
           sends nothing; confirming authorises one attempt.
      LG12 Gates are checked again after each wait: a switch beginning during
           the store sends nothing and withdraws the attempt.
      LG15 A Log's messages and its confirmed UUID belong to that recovered QSO:
           once it is let go, the next one restored — even the same record —
           starts clean and is not blocked as "already logged".
      LG14 Enrichment extras are read for the call with the record's own grid.
      LG13 An edit during the request is saved WITH the attempt (no late write
           erases it); after a confirmed log, edits write nothing.
      LG16 Cleanup retries are serialized, and a cleanup lets go of the form
           only while this tab still holds that QSO: a contact begun after it
           was let go (by an earlier retry) is never erased; Clear and Discard
           stand down while a cleanup runs, as they do for the request — also
           for a retry queued before the Log itself settles.
      LG17 Either cleanup outcome refreshes this tab's own Saved QSOs list (the
           change notice reaches only the other tabs).
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import { draft, clearDraft, submitState } from '../operate/qso.svelte';
import { rig } from '../operate/rig.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { _setDraftChannelForTests } from './draftChannel';
import { sampleRecord } from './savedDraft.fixture';
import { fakeDraftLocks } from './draftLock.fixture';
import { confirmRecoveredRig, recovered } from './recovered.svelte';
import { restoreSavedDraft, _resetRestoreForTests } from './restoreSession';
import {
    clearRecovered,
    discardRecovered,
    _resetRecoveredSaveForTests,
} from './recoveredSave.svelte';
import {
    logRecovered,
    recoveredLogBlock,
    recoveredSubmit,
    recoveredSubmitView,
    retryRecoveredCleanup,
    setRecoveredExtras,
    setRecoveredSender,
    setRecoveredSubmitEnv,
    UNKNOWN_CONFIRM,
    _resetRecoveredSubmitForTests,
    type RecoveredSendOptions,
} from './recoveredSubmit.svelte';
import { loadSavedDrafts, savedDrafts, _resetSavedDraftsForTests } from './savedDrafts.svelte';
import type { SubmitOutcome } from '../api/qso';
import type { SavedDraft } from './savedDraft';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

let mem = memoryDraftStore();
let locks = fakeDraftLocks();
let env = {
    switchGate: null as string | null,
    bootArchiveId: 'arch-a',
    activeLogbookUuid: 'lb-a',
    activeLogbookId: 1,
};
const sent: Array<{ adif: string; logbookId: number; opts: RecoveredSendOptions }> = [];
let answer: (adif: string) => Promise<SubmitOutcome> = () =>
    Promise.resolve({ kind: 'stored', uuid: 'qso-1' });

const UUIDish = 'qso-1';
const stored = (): SavedDraft | undefined => mem.rows.get('d-1');

async function restore(record: SavedDraft = sampleRecord()): Promise<void> {
    await mem.put(record);
    const restoreEnv = {
        locksAvailable: true,
        onPhoneCw: true,
        bootArchiveId: 'arch-a',
        activeLogbookUuid: 'lb-a',
        currentAttribution: sampleRecord().attribution,
    };
    expect((await restoreSavedDraft(record, () => restoreEnv)).ok).toBe(true);
    flushSync();
    expect(confirmRecoveredRig()).toBe(true);
}

beforeEach(async () => {
    mem = memoryDraftStore();
    _setDraftStoreForTests(mem);
    _setDraftChannelForTests({ post() {}, subscribe: () => () => {} });
    locks = fakeDraftLocks();
    vi.stubGlobal('navigator', { locks: locks.manager });
    rig.mode = 'USB';
    rig.cat = 'lost'; // CAT does not gate a recovered Log
    flushSync();
    clearDraft();
    resetToasts();
    _resetRecoveredSaveForTests();
    _resetRecoveredSubmitForTests();
    _resetSavedDraftsForTests();
    env = {
        switchGate: null,
        bootArchiveId: 'arch-a',
        activeLogbookUuid: 'lb-a',
        activeLogbookId: 1,
    };
    setRecoveredSubmitEnv(() => env);
    sent.length = 0;
    answer = () => Promise.resolve({ kind: 'stored', uuid: UUIDish });
    setRecoveredSender((adif, logbookId, opts) => {
        sent.push({ adif, logbookId, opts });
        return answer(adif);
    });
    await restore();
});
afterEach(async () => {
    submitState.busy = false;
    _resetRecoveredSubmitForTests();
    _resetRecoveredSaveForTests();
    await _resetRestoreForTests();
    clearDraft();
    _setDraftStoreForTests(null);
    _setDraftChannelForTests(null);
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

describe('gates', () => {
    it('LG1 CAT lost does not block; the other gates do', () => {
        expect(recoveredLogBlock()).toBeNull();
        recovered.confirmed = false;
        expect(recoveredLogBlock()).toMatch(/rig values/);
        recovered.confirmed = true;
        env.switchGate = 'An archive switch is in progress';
        expect(recoveredLogBlock()).toMatch(/archive switch/);
        env.switchGate = null;
        env.activeLogbookUuid = 'other';
        expect(recoveredLogBlock()).toMatch(/belongs to/);
        env.activeLogbookUuid = 'lb-a';
        draft.callsign = '';
        expect(recoveredLogBlock()).toMatch(/callsign/i);
    });

    it('LG1 a record without attribution cannot be logged', () => {
        // Restore already refuses such a record (restoreEligibility); this is
        // the Log path's own gate, independent of how the record got here.
        recovered.record!.attribution = null;
        expect(recoveredLogBlock()).toMatch(/attribution unavailable/i);
    });
});

describe('the request', () => {
    it('LG2 the exact request is stored before it is sent', async () => {
        let atSend: SavedDraft | undefined;
        answer = () => {
            atSend = structuredClone(stored()); // what the store held as it went out
            return Promise.resolve({ kind: 'stored', uuid: UUIDish });
        };
        recovered.rig!.freqHz = 14_200_000; // a correction (then confirm again)
        expect(confirmRecoveredRig()).toBe(true);
        await logRecovered({});
        expect(sent).toHaveLength(1);
        const { adif, logbookId, opts } = sent[0];
        expect(atSend?.outcome).toBe('unknown');
        expect(atSend?.attempt).toMatchObject({
            archiveId: 'arch-a',
            logbookUuid: 'lb-a',
            logbookId,
            force: opts.force,
            adif,
            expect: opts.expect,
        });
        expect(logbookId).toBe(1);
        expect(opts.force).toBe(false);
        expect(opts.expect).toEqual(sampleRecord().attribution);
        expect(adif).toContain('<FREQ:9>14.200000');
        expect(adif).toContain('<OPERATOR:6>7Q5MLV');
        expect(adif).toContain('<MY_NAME:4>Marc');
        expect(adif).toContain('<MY_GRIDSQUARE:4>KH66');
    });

    it('LG14 enrichment is read for the call, its bearing from the SAVED grid', async () => {
        const extras = vi.fn(() => ({ country: 'Japan', antAz: '42.0' }));
        setRecoveredExtras(extras);
        await logRecovered({});
        expect(extras).toHaveBeenCalledWith('G0ABC', 'KH66'); // the saved call is g0abc
        expect(sent[0].adif).toContain('<COUNTRY:5>Japan');
        expect(sent[0].adif).toContain('<ANT_AZ:4>42.0');
    });

    it('LG3 a failed store sends nothing and claims no attempt', async () => {
        vi.spyOn(mem, 'put').mockRejectedValueOnce(new Error('QuotaExceededError'));
        expect(await logRecovered({})).toBe('refused');
        expect(sent).toHaveLength(0);
        expect(recovered.record?.attempt).toBeNull();
        expect(recovered.record?.outcome).toBe('unlogged');
    });
});

describe('a confirmed log', () => {
    it('LG4 deletes the record, empties the form, releases the lock', async () => {
        expect(await logRecovered({})).toBe('stored');
        expect(stored()).toBeUndefined();
        expect(recovered.record).toBeNull();
        expect(draft.callsign).toBe('');
        expect(locks.held.size).toBe(0);
    });

    it('LG5 a failed delete writes a terminal logged record instead', async () => {
        vi.spyOn(mem, 'remove').mockRejectedValueOnce(new Error('busy'));
        expect(await logRecovered({})).toBe('stored');
        expect(stored()).toMatchObject({ state: 'logged', loggedQsoUuid: UUIDish });
        expect(recovered.record).toBeNull();
        expect(locks.held.size).toBe(0);
    });

    it('LG6 both cleanup writes failing keeps the UUID, blocks submit, retries cleanup only', async () => {
        vi.spyOn(mem, 'remove').mockRejectedValueOnce(new Error('busy'));
        const put = vi.spyOn(mem, 'put');
        let failPuts = true;
        put.mockImplementation((r) => {
            if (failPuts && r.state === 'logged') return Promise.reject(new Error('busy'));
            mem.rows.set(r.id, structuredClone(r));
            return Promise.resolve();
        });
        expect(await logRecovered({})).toBe('stored');
        expect(recoveredSubmit.confirmedUuid).toBe(UUIDish);
        expect(recoveredSubmit.cleanupError).not.toBe('');
        expect(recoveredLogBlock()).toMatch(/already logged/i);
        expect(await logRecovered({})).toBe('blocked');
        expect(locks.held.size).toBe(1);
        draft.notes = 'typed after the log';
        flushSync();
        await Promise.resolve();
        expect(stored()?.fields.notes).not.toBe('typed after the log'); // writes stopped
        failPuts = false;
        expect(await retryRecoveredCleanup()).toBe(true);
        expect(sent).toHaveLength(1); // cleanup never resubmits
        expect(recovered.record).toBeNull();
    });

    // Both cleanup writes fail once; later removes wait on `gates` until
    // `holding` is switched off.
    const held = { holding: true, removes: () => 0 };
    function failCleanupThenHoldRemoves(): Array<() => void> {
        held.holding = true;
        const gates: Array<() => void> = [];
        const remove = vi.spyOn(mem, 'remove');
        held.removes = () => remove.mock.calls.length;
        remove.mockRejectedValueOnce(new Error('busy'));
        remove.mockImplementation((id) => {
            const done = (): void => void mem.rows.delete(id);
            if (!held.holding) return Promise.resolve(done());
            return new Promise<void>((resolve) => {
                gates.push(() => {
                    done();
                    resolve();
                });
            });
        });
        vi.spyOn(mem, 'put').mockImplementation((r) => {
            if (r.state === 'logged') return Promise.reject(new Error('busy'));
            mem.rows.set(r.id, structuredClone(r));
            return Promise.resolve();
        });
        return gates;
    }

    it('LG16 overlapping cleanup retries never erase the next contact', async () => {
        const gates = failCleanupThenHoldRemoves();
        expect(await logRecovered({})).toBe('stored');
        const first = retryRecoveredCleanup();
        const second = retryRecoveredCleanup();
        await vi.waitFor(() => expect(gates.length).toBeGreaterThan(0));
        gates.shift()!();
        expect(await first).toBe(true);
        expect(recovered.record).toBeNull();
        draft.callsign = 'NEXT1'; // the operator begins another contact
        held.holding = false;
        gates.forEach((open) => open());
        await second;
        expect(draft.callsign).toBe('NEXT1');
        // One remove in the Log, one for both retries: the second waited and
        // found the QSO already let go.
        expect(held.removes()).toBe(2);
    });

    it('LG16 Clear and Discard stand down while a cleanup runs', async () => {
        const gates = failCleanupThenHoldRemoves();
        expect(await logRecovered({})).toBe('stored');
        const retry = retryRecoveredCleanup();
        await vi.waitFor(() => expect(gates.length).toBe(1));
        held.holding = false; // only the retry's remove waits
        expect(await discardRecovered('d-1')).toBe(false);
        expect(await clearRecovered()).toBe(false);
        expect(recovered.record).not.toBeNull();
        gates[0]();
        expect(await retry).toBe(true);
        expect(stored()).toBeUndefined();
        expect(recovered.record).toBeNull();
        expect(submitState.busy).toBe(false); // the latch is let go with it
    });

    it('LG16 a retry queued before the Log settles keeps Clear and Discard out', async () => {
        // The Log's cleanup fails on both writes, but only after the retry is queued.
        const remove = vi.spyOn(mem, 'remove');
        let failLogRemove: () => void = () => {};
        remove.mockImplementationOnce(
            () =>
                new Promise<void>((_, reject) => (failLogRemove = () => reject(new Error('busy'))))
        );
        let failRetryRemove: () => void = () => {};
        remove.mockImplementationOnce(
            () =>
                new Promise<void>(
                    (_, reject) => (failRetryRemove = () => reject(new Error('busy')))
                )
        );
        let logWrites = 0;
        vi.spyOn(mem, 'put').mockImplementation((r) => {
            if (r.state === 'logged' && logWrites++ === 0) return Promise.reject(new Error('busy'));
            mem.rows.set(r.id, structuredClone(r));
            return Promise.resolve();
        });
        const logging = logRecovered({});
        await vi.waitFor(() => expect(remove).toHaveBeenCalledTimes(1));
        expect(recoveredSubmitView().confirmedUuid).toBe(UUIDish); // Retry cleanup is offered
        const retry = retryRecoveredCleanup();
        failLogRemove();
        expect(await logging).toBe('stored');
        await vi.waitFor(() => expect(remove).toHaveBeenCalledTimes(2)); // the retry runs
        expect(submitState.busy).toBe(true);
        expect(await discardRecovered('d-1')).toBe(false);
        failRetryRemove();
        expect(await retry).toBe(true); // its "logged" write lands
        expect(stored()).toMatchObject({ state: 'logged', loggedQsoUuid: UUIDish });
        expect(submitState.busy).toBe(false);
    });
});

describe('the Saved QSOs list after a confirmed log', () => {
    it("LG17 a deleted record leaves this tab's list", async () => {
        await loadSavedDrafts();
        expect(savedDrafts.list.map((r) => r.id)).toEqual(['d-1']);
        expect(await logRecovered({})).toBe('stored');
        await vi.waitFor(() => expect(savedDrafts.list).toEqual([]));
    });

    it("LG17 a terminal logged record shows as logged in this tab's list", async () => {
        await loadSavedDrafts();
        vi.spyOn(mem, 'remove').mockRejectedValueOnce(new Error('busy'));
        expect(await logRecovered({})).toBe('stored');
        await vi.waitFor(() =>
            expect(savedDrafts.list).toMatchObject([{ id: 'd-1', state: 'logged' }])
        );
    });
});

describe('outcomes', () => {
    const ambiguous: Array<[string, SubmitOutcome]> = [
        ['network', { kind: 'network', message: 'reset' }],
        ['malformed success', { kind: 'server', code: 'malformed_response', message: 'no uuid' }],
        ['aborted', { kind: 'aborted', message: 'aborted' }],
    ];
    it.each(ambiguous)(
        'LG7 %s is ambiguous: the attempt stays, unknown, no retry',
        async (_name, out) => {
            answer = () => Promise.resolve(out);
            expect(await logRecovered({})).toBe('unknown');
            expect(stored()?.outcome).toBe('unknown');
            expect(stored()?.attempt?.adif).toBe(sent[0].adif);
            expect(sent).toHaveLength(1);
            expect(recovered.record).not.toBeNull();
        }
    );

    it('LG8 a definite refusal restores "not logged" only without earlier uncertainty', async () => {
        answer = () =>
            Promise.resolve({ kind: 'validation', code: 'invalid_field_value', message: 'bad' });
        expect(await logRecovered({})).toBe('refused');
        await Promise.resolve();
        expect(recovered.record?.outcome).toBe('unlogged');
        expect(recovered.record?.attempt).toBeNull();

        answer = () => Promise.resolve({ kind: 'network', message: 'reset' });
        expect(await logRecovered({})).toBe('unknown');
        answer = () =>
            Promise.resolve({ kind: 'validation', code: 'invalid_field_value', message: 'bad' });
        expect(await logRecovered({ confirm: () => true })).toBe('refused');
        expect(recovered.record?.outcome).toBe('unknown'); // the earlier uncertainty stays
    });

    it('LG9 a duplicate keeps everything; Log anyway is explicit and forced', async () => {
        answer = () => Promise.resolve({ kind: 'duplicate', uuid: 'existing-1' });
        expect(await logRecovered({})).toBe('duplicate');
        expect(recoveredSubmit.duplicateUuid).toBe('existing-1');
        expect(recovered.record).not.toBeNull();
        expect(stored()).toBeDefined();
        answer = () => Promise.resolve({ kind: 'stored', uuid: UUIDish });
        expect(await logRecovered({ force: true })).toBe('stored');
        expect(sent[1].opts.force).toBe(true);
    });

    it('LG10 attribution_changed is a definite refusal, shown verbatim', async () => {
        answer = () =>
            Promise.resolve({
                kind: 'validation',
                code: 'attribution_changed',
                message: 'the station’s attribution changed: MY_RIG "X" now',
            });
        expect(await logRecovered({})).toBe('refused');
        expect(recoveredSubmit.refusal).toMatch(/attribution changed/);
        expect(recovered.record).not.toBeNull();
    });
});

describe('a QSO let go', () => {
    it('LG15 a duplicate answer does not follow the QSO into its next restore', async () => {
        answer = () => Promise.resolve({ kind: 'duplicate', uuid: 'qso-old' });
        expect(await logRecovered({})).toBe('duplicate');
        expect(recoveredSubmitView().duplicateUuid).toBe('qso-old');
        await vi.waitFor(() => expect(stored()?.attempt).toBeNull());
        expect(await clearRecovered()).toBe(true);
        // The SAME saved record again (restore() would otherwise default to a sample).
        const again = stored();
        expect(again?.id).toBe('d-1');
        await restore(again);
        expect(recoveredSubmitView().duplicateUuid).toBe('');
        expect(recoveredSubmitView().refusal).toBe('');
        // Once this QSO has its own message, the old duplicate is still not shown.
        env.switchGate = 'An archive switch is in progress';
        expect(await logRecovered({})).toBe('blocked');
        expect(recoveredSubmitView().refusal).toMatch(/archive switch/);
        expect(recoveredSubmitView().duplicateUuid).toBe('');
    });

    it('LG15 a confirmed UUID does not block the next recovered QSO', async () => {
        vi.spyOn(mem, 'remove').mockRejectedValueOnce(new Error('remove failed'));
        answer = () => {
            vi.spyOn(mem, 'put').mockRejectedValueOnce(new Error('put failed'));
            return Promise.resolve({ kind: 'stored', uuid: UUIDish });
        };
        expect(await logRecovered({})).toBe('stored');
        expect(recoveredSubmitView().confirmedUuid).toBe(UUIDish);
        expect(await discardRecovered('d-1')).toBe(true); // the delete now works
        expect(recovered.record).toBeNull();
        await restore(sampleRecord({ id: 'd-2' }));
        expect(recoveredSubmitView().confirmedUuid).toBe('');
        expect(recoveredLogBlock()).toBeNull();
        answer = () => Promise.resolve({ kind: 'stored', uuid: 'qso-2' });
        expect(await logRecovered({})).toBe('stored');
    });
});

describe('unknown outcome and races', () => {
    it('LG11 an unknown-outcome record needs the explicit confirmation', async () => {
        await _resetRestoreForTests();
        mem = memoryDraftStore();
        _setDraftStoreForTests(mem);
        await restore(sampleRecord({ id: 'd-1', outcome: 'unknown' }));
        const confirm = vi.fn(() => false);
        expect(await logRecovered({ confirm })).toBe('cancelled');
        expect(confirm).toHaveBeenCalledWith(UNKNOWN_CONFIRM);
        expect(sent).toHaveLength(0);
        expect(await logRecovered({ confirm: () => true })).toBe('stored');
        expect(sent).toHaveLength(1);
    });

    it('LG12 a switch beginning during the store sends nothing and withdraws the attempt', async () => {
        const realPut = mem.put.bind(mem);
        vi.spyOn(mem, 'put').mockImplementationOnce(async (r) => {
            env.switchGate = 'An archive switch is in progress';
            return realPut(r);
        });
        expect(await logRecovered({})).toBe('blocked');
        expect(sent).toHaveLength(0);
        await Promise.resolve();
        expect(recovered.record?.attempt).toBeNull();
    });

    it('LG13 an edit during the request is saved with the attempt', async () => {
        let finish!: (o: SubmitOutcome) => void;
        answer = () => new Promise((r) => (finish = r));
        const logging = logRecovered({});
        await vi.waitFor(() => expect(sent).toHaveLength(1));
        draft.notes = 'typed while logging';
        flushSync();
        await vi.waitFor(() => expect(stored()?.fields.notes).toBe('typed while logging'));
        expect(stored()?.attempt).not.toBeNull(); // not erased by the later write
        finish({ kind: 'network', message: 'reset' });
        expect(await logging).toBe('unknown');
    });
});
