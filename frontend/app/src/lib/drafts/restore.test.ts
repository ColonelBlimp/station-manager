/*
    Whether a saved QSO may be restored here (ADR 0085 RS3–RS7; operator rulings
    2026-10-01). Pure: the panel will show the reason; no Restore control is
    wired until the whole recovery path exists (commit 4).

      E1  A logged record is never restorable (RS7).
      E2  No Web Locks: "read and copy only" (RS3).
      E3  Missing attribution: "Original attribution unavailable" (RS5).
      E4  Another archive or another logbook UUID names the source and never
          switches (RS4) — a colliding numeric logbook id does not pass.
      E5  MY_RIG must equal today's server stamp (hard refusal, both shown);
          today's stamp unreadable refuses too (RS5).
      E6  Saved OPERATOR / MY_NAME differing from today's do NOT refuse: Restore
          supplies them explicitly (RS5).
      E7  On any page other than Phone / CW: "Go to Phone / CW" (RS6).
      E8  Otherwise: eligible.
*/
import { describe, expect, it } from 'vitest';
import { restoreEligibility, type RestoreEnv } from './restore';
import { sampleRecord } from './savedDraft.fixture';

const ENV: RestoreEnv = {
    locksAvailable: true,
    onPhoneCw: true,
    bootArchiveId: 'arch-a',
    activeLogbookUuid: 'lb-a',
    currentAttribution: { myRig: 'FTdx10 (home)', operator: '7Q5MLV', myName: 'Marc' },
};

describe('restoreEligibility', () => {
    it('E1 a logged record is never restorable', () => {
        expect(
            restoreEligibility(sampleRecord({ state: 'logged', loggedQsoUuid: 'q' }), ENV).kind
        ).toBe('logged');
    });
    it('E2 no Web Locks', () => {
        const e = restoreEligibility(sampleRecord(), { ...ENV, locksAvailable: false });
        expect(e).toMatchObject({ kind: 'unavailable' });
        expect(e.kind === 'unavailable' && e.reason).toMatch(/read and copy only/);
    });
    it('E3 missing attribution', () => {
        const e = restoreEligibility(sampleRecord({ attribution: null }), ENV);
        expect(e.kind === 'unavailable' && e.reason).toBe('Original attribution unavailable.');
    });
    it('E4 another archive or logbook names the source', () => {
        const other = restoreEligibility(sampleRecord(), { ...ENV, bootArchiveId: 'arch-b' });
        expect(other.kind === 'unavailable' && other.reason).toMatch(/belongs to ‘Home’/);
        const logbook = restoreEligibility(sampleRecord({ logbookId: 1 }), {
            ...ENV,
            activeLogbookUuid: 'lb-other', // numeric id 1 in another archive
        });
        expect(logbook.kind).toBe('unavailable');
        const unproven = restoreEligibility(sampleRecord(), { ...ENV, bootArchiveId: null });
        expect(unproven.kind).toBe('unavailable');
    });
    it('E5 MY_RIG must match today’s stamp', () => {
        const e = restoreEligibility(sampleRecord(), {
            ...ENV,
            currentAttribution: { myRig: 'Yaesu FTdx10', operator: '7Q5MLV', myName: 'Marc' },
        });
        expect(e).toMatchObject({
            kind: 'my-rig-changed',
            saved: 'FTdx10 (home)',
            current: 'Yaesu FTdx10',
        });
        const unread = restoreEligibility(sampleRecord(), { ...ENV, currentAttribution: null });
        expect(unread.kind).toBe('unavailable');
    });
    it('E6 a different operator or name today does not refuse', () => {
        const e = restoreEligibility(sampleRecord(), {
            ...ENV,
            currentAttribution: { myRig: 'FTdx10 (home)', operator: 'G0XYZ', myName: 'Guest' },
        });
        expect(e.kind).toBe('eligible');
    });
    it('E7 other pages offer Go to Phone / CW', () => {
        expect(restoreEligibility(sampleRecord(), { ...ENV, onPhoneCw: false }).kind).toBe(
            'go-to-phone-cw'
        );
    });
    it('E8 eligible', () => {
        expect(restoreEligibility(sampleRecord(), ENV).kind).toBe('eligible');
    });
});
