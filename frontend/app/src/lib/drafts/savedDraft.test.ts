/*
    What a saved draft says about itself (ADR 0085; operator wording 2026-09-30).

      D1  An unlogged record reads "Unlogged QSO saved from ‘Home’ — not logged."
      D2  A record whose logging outcome is unknown NEVER says "not logged": it
          reads "QSO draft saved from ‘Home’ — logging outcome unknown. Check the
          Logbook in ‘Home’ before logging it."
      D3  Every saved value is shown; the rig values say which reading they are,
          and a missing one reads "unknown", never a default.
      D4  The copy text carries the headline and every row.
      D5  A damaged or foreign record read back from storage is not a saved draft.
      D7  A version 1 record (slice 1) is read as version 2 with its attribution
          MISSING and its outcome KEPT: an unknown outcome stays unknown (RS2).
      D8  Missing attribution reads "Original attribution unavailable"; known
          attribution is shown, a known-empty value as "(none)" (RS1).
      D9  A record logged but not removed says so with its QSO UUID (RS7).
      D10 The version 2 shape is checked: attribution null or complete; state;
          the logged UUID; the attempted submission null or complete.
      D6  A draft that could NOT be saved never claims it was: it reads "not
          saved", keeps an unknown logging outcome, has no Saved time, and
          still names its archive when known (review 2026-09-30).
*/
import { describe, expect, it } from 'vitest';
import {
    readSavedDraft,
    savedDraftHeadline,
    savedDraftLines,
    savedDraftSummary,
    savedDraftText,
} from './savedDraft';
import { sampleRecord, sampleV1Record } from './savedDraft.fixture';

describe('saved draft wording', () => {
    it('D1 unlogged', () => {
        expect(savedDraftHeadline(sampleRecord())).toBe(
            'Unlogged QSO saved from ‘Home’ — not logged.'
        );
        expect(savedDraftSummary(sampleRecord())).toBe('G0ABC · 2026-09-30 12:00:00 UTC');
    });

    it('D2 an unknown outcome never says "not logged"', () => {
        const h = savedDraftHeadline(sampleRecord({ outcome: 'unknown' }));
        expect(h).toBe(
            'QSO draft saved from ‘Home’ — logging outcome unknown. Check the Logbook in ‘Home’ before logging it.'
        );
        expect(savedDraftText(sampleRecord({ outcome: 'unknown' }))).not.toMatch(/not logged/);
    });

    it('D3 every value, the reading named, missing values unknown', () => {
        const rows = Object.fromEntries(savedDraftLines(sampleRecord()));
        expect(rows).toMatchObject({
            Archive: 'Home',
            Logbook: 'Home log',
            'Station callsign': '7Q5MLV',
            Callsign: 'G0ABC',
            'Time on': '12:00:00',
            'RST rcvd': '57',
            Frequency: '14.255000 MHz',
            Band: '20m',
            Mode: 'SSB / USB',
            'Rig values': 'last reading before the connection dropped, 2026-09-30 12:05:00 UTC',
            Name: 'Bob',
            Comment: 'tnx',
        });
        const missing = sampleRecord();
        missing.rig = {
            ...missing.rig,
            freqHz: null,
            band: '',
            adifMode: '',
            subMode: '',
            basis: 'when-saved',
        };
        const m = Object.fromEntries(savedDraftLines(missing));
        expect(m).toMatchObject({
            Frequency: 'unknown',
            Band: 'unknown',
            Mode: 'unknown',
            'Rig values': 'reading when saved, 2026-09-30 12:05:00 UTC',
        });
    });

    it('D4 copy text holds the headline and every row', () => {
        const t = savedDraftText(sampleRecord());
        expect(t.split('\n')[0]).toBe('Unlogged QSO saved from ‘Home’ — not logged.');
        expect(t).toContain('Frequency: 14.255000 MHz');
        expect(t).toContain('Comment: tnx');
    });

    it('D5 shape check', () => {
        expect(readSavedDraft(sampleRecord())).toEqual(sampleRecord());
        expect(readSavedDraft({ ...sampleRecord(), version: 3 })).toBeNull();
        expect(readSavedDraft({ ...sampleRecord(), outcome: 'maybe' })).toBeNull();
        expect(readSavedDraft({ ...sampleRecord(), logbookUuid: 3 })).toBeNull();
        const noField = sampleRecord();
        delete (noField.fields as Partial<typeof noField.fields>).notes;
        expect(readSavedDraft(noField)).toBeNull();
        expect(
            readSavedDraft({ ...sampleRecord(), rig: { ...sampleRecord().rig, basis: 'x' } })
        ).toBeNull();
        expect(readSavedDraft(null)).toBeNull();
    });

    it('D7 a version 1 record keeps its outcome and has no attribution', () => {
        const plain = readSavedDraft(sampleV1Record());
        expect(plain).toMatchObject({
            version: 2,
            attribution: null,
            state: 'draft',
            attempt: null,
            outcome: 'unlogged',
        });
        const unknown = readSavedDraft(sampleV1Record({ outcome: 'unknown' }));
        expect(unknown?.outcome).toBe('unknown');
        expect(savedDraftHeadline(unknown!)).not.toMatch(/not logged/);
        expect(readSavedDraft(sampleV1Record({ outcome: 'maybe' }))).toBeNull();
    });

    it('D8 attribution: missing, known, known-empty', () => {
        const missing = Object.fromEntries(savedDraftLines(sampleRecord({ attribution: null })));
        expect(missing.Attribution).toBe('Original attribution unavailable');
        const known = Object.fromEntries(savedDraftLines(sampleRecord()));
        expect(known).toMatchObject({
            Operator: '7Q5MLV',
            'Operator name': 'Marc',
            'My rig': 'FTdx10 (home)',
        });
        expect(known.Attribution).toBeUndefined();
        const empty = Object.fromEntries(
            savedDraftLines(sampleRecord({ attribution: { myRig: '', operator: '', myName: '' } }))
        );
        expect(empty).toMatchObject({
            Operator: '(none)',
            'Operator name': '(none)',
            'My rig': '(none)',
        });
    });

    it('D9 a logged record says so with its QSO UUID', () => {
        const r = sampleRecord({ state: 'logged', loggedQsoUuid: '01a0f6c4-uuid' });
        expect(savedDraftHeadline(r)).toBe(
            'Logged as QSO 01a0f6c4-uuid — this browser’s saved copy could not be removed.'
        );
        expect(savedDraftHeadline(r)).not.toMatch(/not logged|unknown/);
    });

    it('D10 version 2 shape', () => {
        expect(readSavedDraft({ ...sampleRecord(), attribution: { myRig: 'x' } })).toBeNull();
        expect(readSavedDraft({ ...sampleRecord(), state: 'done' })).toBeNull();
        expect(
            readSavedDraft({ ...sampleRecord(), state: 'logged', loggedQsoUuid: '' })
        ).toBeNull();
        expect(
            readSavedDraft({ ...sampleRecord(), attempt: { at: '2026-10-01T10:00:00Z' } })
        ).toBeNull();
        const attempt = {
            at: '2026-10-01T10:00:00.000Z',
            logbookId: 1,
            adif: '<CALL:5>G0ABC<EOR>',
            expect: { myRig: 'FTdx10 (home)', operator: '7Q5MLV', myName: 'Marc' },
        };
        expect(readSavedDraft({ ...sampleRecord(), attempt })?.attempt).toEqual(attempt);
    });

    it('D6 an unsaved draft says so, keeping the outcome', () => {
        const plain = sampleRecord();
        expect(savedDraftHeadline(plain, 'unsaved')).toBe(
            'Unlogged QSO from ‘Home’ — not saved, and not logged.'
        );
        const unknown = sampleRecord({ outcome: 'unknown' });
        expect(savedDraftHeadline(unknown, 'unsaved')).toBe(
            'QSO draft from ‘Home’ — not saved. Logging outcome unknown. Check the Logbook in ‘Home’ before logging it.'
        );
        const text = savedDraftText(unknown, 'unsaved');
        expect(text).not.toMatch(/saved from|not logged/);
        expect(text).toMatch(/logging outcome unknown/i);
        expect(Object.keys(Object.fromEntries(savedDraftLines(unknown, 'unsaved')))).not.toContain(
            'Saved'
        );
        const unproven = sampleRecord({ archiveLabel: '', outcome: 'unknown' });
        expect(savedDraftHeadline(unproven, 'unsaved')).toBe(
            'QSO draft — not saved. Logging outcome unknown. Check the Logbook before logging it.'
        );
    });
});
