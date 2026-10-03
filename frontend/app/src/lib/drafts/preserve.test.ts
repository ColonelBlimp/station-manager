/*
    Saving the Phone / CW draft before a rebind reload (ADR 0085, rule 3 slice 1).

      P1  No unlogged work: nothing is saved (and the reload may proceed).
      P2  The saved record keeps the ORIGINAL archive and logbook identity, the
          station attribution, every entered field and the rig reading.
      P3  An unknown logging outcome — reported uncertain, or a Log request still
          in flight — is saved as outcome "unknown".
      P4  Repeated reload callbacks and Retry save reuse the SAME record (one id
          per page); overlapping calls share one write. Apart from a second
          record for the same QSO.
      P5  Missing proven source identity (archive or logbook UUID) fails the save
          with nothing written; the record is still returned for display.
      P6  A storage failure fails the save; the record is returned for display.
      P7  Two pages' drafts from the same archive are separate records.
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { draft, clearDraft, setSubmit, logDraft } from '../operate/qso.svelte';
import { rig, confirmRig, catLink } from '../operate/rig.svelte';
import { flushSync } from 'svelte';
import { noteRigDrop, _resetRigSnapshotForTests } from '../operate/rigSnapshot.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { preserveDraft, _resetPreserveForTests, type StationSource } from './preserve';

const ARCHIVE = { archiveId: 'arch-a', archiveLabel: 'Home' };
const STATION: StationSource = {
    logbookUuid: 'lb-a',
    logbookId: 1,
    logbookName: 'Home log',
    stationCallsign: '7Q5MLV',
    operator: '7Q5MLV',
    myGrid: 'KH66',
    attribution: { myRig: 'FTdx10 (home)', operator: '7Q5MLV', myName: 'Marc' },
};
let mem = memoryDraftStore();

beforeEach(() => {
    mem = memoryDraftStore();
    _setDraftStoreForTests(mem);
    _resetPreserveForTests();
    _resetRigSnapshotForTests();
    clearDraft();
    rig.freq = '14.255.000';
    rig.band = '20m';
    rig.mode = 'USB';
});
afterEach(() => {
    _setDraftStoreForTests(null);
    clearDraft();
});

function typeDraft(): void {
    draft.callsign = 'G0ABC';
    draft.dateOn = '2026-09-30';
    draft.timeOn = '12:00:00';
    draft.name = 'Bob';
}

describe('preserveDraft', () => {
    it('P1 nothing to save', async () => {
        expect(await preserveDraft(ARCHIVE, STATION)).toEqual({ kind: 'none' });
        expect(mem.rows.size).toBe(0);
    });

    it('P2 the record keeps its original identity, fields and rig reading', async () => {
        typeDraft();
        const out = await preserveDraft(ARCHIVE, STATION);
        expect(out.kind).toBe('saved');
        const [rec] = [...mem.rows.values()];
        expect(rec).toMatchObject({
            version: 3,
            rigCorrection: null,
            attribution: { myRig: 'FTdx10 (home)', operator: '7Q5MLV', myName: 'Marc' },
            state: 'draft',
            attempt: null,
            archiveId: 'arch-a',
            archiveLabel: 'Home',
            logbookUuid: 'lb-a',
            logbookName: 'Home log',
            stationCallsign: '7Q5MLV',
            operator: '7Q5MLV',
            myGrid: 'KH66',
            outcome: 'unlogged',
        });
        expect(rec.fields).toMatchObject({ callsign: 'G0ABC', timeOn: '12:00:00', name: 'Bob' });
        expect(rec.rig).toMatchObject({ freqHz: 14_255_000, band: '20m', basis: 'when-saved' });
    });

    it('P3 an uncertain log attempt saves outcome unknown', async () => {
        typeDraft();
        confirmRig();
        setSubmit(() =>
            Promise.resolve({ ok: false as const, message: 'Cannot confirm', uncertain: true })
        );
        await logDraft();
        await preserveDraft(ARCHIVE, STATION);
        expect([...mem.rows.values()][0].outcome).toBe('unknown');
    });

    it('P3 a Log request still in flight saves outcome unknown', async () => {
        typeDraft();
        confirmRig();
        let answer: (r: { ok: false; message: string }) => void = () => {};
        setSubmit(() => new Promise((r) => (answer = r)));
        const run = logDraft();
        await preserveDraft(ARCHIVE, STATION);
        expect([...mem.rows.values()][0].outcome).toBe('unknown');
        answer({ ok: false, message: 'x' });
        await run;
    });

    it('P4 repeated saves and overlapping calls reuse one record', async () => {
        typeDraft();
        const put = vi.spyOn(mem, 'put');
        const [a, b] = await Promise.all([
            preserveDraft(ARCHIVE, STATION),
            preserveDraft(ARCHIVE, STATION),
        ]);
        expect(put).toHaveBeenCalledTimes(1);
        draft.comment = 'edited';
        const c = await preserveDraft(ARCHIVE, STATION);
        expect(mem.rows.size).toBe(1);
        const ids = [a, b, c].map((o) => (o.kind === 'saved' ? o.record.id : ''));
        expect(new Set(ids).size).toBe(1);
        expect([...mem.rows.values()][0].fields.comment).toBe('edited');
    });

    it('P4 a retry after a failed write reuses the same record', async () => {
        typeDraft();
        const put = vi.spyOn(mem, 'put').mockRejectedValueOnce(new Error('QuotaExceededError'));
        const first = await preserveDraft(ARCHIVE, STATION);
        const second = await preserveDraft(ARCHIVE, STATION);
        expect(first.kind).toBe('failed');
        expect(second.kind).toBe('saved');
        expect(put).toHaveBeenCalledTimes(2);
        expect(first.kind !== 'none' && second.kind !== 'none' && first.record.id).toBe(
            second.kind !== 'none' && second.record.id
        );
    });

    it('P5 missing proven identity fails with nothing written', async () => {
        typeDraft();
        const noArchive = await preserveDraft(null, STATION);
        const noLogbook = await preserveDraft(ARCHIVE, { ...STATION, logbookUuid: '' });
        const noArchiveId = await preserveDraft({ archiveId: '', archiveLabel: '' }, STATION);
        for (const out of [noArchive, noLogbook, noArchiveId]) {
            expect(out.kind).toBe('failed');
            if (out.kind === 'failed') {
                expect(out.reason).toMatch(/could not be proven/);
                expect(out.record.fields.callsign).toBe('G0ABC');
            }
        }
        expect(mem.rows.size).toBe(0);
    });

    it('P6 a storage failure fails the save', async () => {
        typeDraft();
        vi.spyOn(mem, 'put').mockRejectedValue(new Error('QuotaExceededError'));
        const out = await preserveDraft(ARCHIVE, STATION);
        expect(out.kind).toBe('failed');
        if (out.kind === 'failed') expect(out.reason).toMatch(/QuotaExceededError/);
    });

    it('P7 another page’s draft from the same archive is a separate record', async () => {
        typeDraft();
        await preserveDraft(ARCHIVE, STATION);
        _resetPreserveForTests(); // a different page
        draft.callsign = 'M0XYZ';
        await preserveDraft(ARCHIVE, STATION);
        expect([...mem.rows.values()].map((r) => r.fields.callsign).sort()).toEqual([
            'G0ABC',
            'M0XYZ',
        ]);
    });

    // Review 2026-09-30: a replacement daemon's CW report refilled 599/599 over
    // typed 57/56, and the save paired them with the held USB reading.
    it('P8 typed reports saved with the held reading survive a reconnect mode report', async () => {
        rig.mode = 'USB';
        flushSync();
        draft.callsign = 'G0ABC';
        draft.rstSent = '57';
        draft.rstRcvd = '56';
        flushSync();
        noteRigDrop(Date.parse('2026-09-30T12:00:00Z'));
        catLink.onRigState({ mode: 'CW', vfoA: 7_074_000 });
        flushSync();
        await preserveDraft(ARCHIVE, STATION);
        const saved = [...mem.rows.values()][0];
        expect(saved.rig.mode).toBe('USB');
        expect([saved.fields.rstSent, saved.fields.rstRcvd]).toEqual(['57', '56']);
    });

    // RS1: a failed attribution read is recorded as missing, never guessed.
    it('P9 missing attribution is saved as missing', async () => {
        typeDraft();
        await preserveDraft(ARCHIVE, { ...STATION, attribution: null });
        expect([...mem.rows.values()][0].attribution).toBeNull();
    });
});
