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
      D6  A draft that could NOT be saved never claims it was: it reads "not
          saved", keeps an unknown logging outcome, has no Saved time, and
          still names its archive when known (review 2026-09-30).
*/
import { describe, expect, it } from 'vitest';
import {
    isSavedDraft,
    savedDraftHeadline,
    savedDraftLines,
    savedDraftSummary,
    savedDraftText,
} from './savedDraft';
import { sampleRecord } from './savedDraft.fixture';

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
        expect(isSavedDraft(sampleRecord())).toBe(true);
        expect(isSavedDraft({ ...sampleRecord(), version: 2 })).toBe(false);
        expect(isSavedDraft({ ...sampleRecord(), outcome: 'maybe' })).toBe(false);
        expect(isSavedDraft({ ...sampleRecord(), logbookUuid: 3 })).toBe(false);
        const noField = sampleRecord();
        delete (noField.fields as Partial<typeof noField.fields>).notes;
        expect(isSavedDraft(noField)).toBe(false);
        expect(
            isSavedDraft({ ...sampleRecord(), rig: { ...sampleRecord().rig, basis: 'x' } })
        ).toBe(false);
        expect(isSavedDraft(null)).toBe(false);
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
