// Session export/email accounting (W-0021 "alpha.4 hold: fix design", revision 3,
// slice 5; Finding #6). What the operator is told must be what the daemon
// confirmed for exactly the selection that was sent: no success claim from
// absent, malformed or inconsistent metadata, and every shortfall named.

import { describe, expect, it } from 'vitest';
import { emailReport, exportReport, snapshotSelection } from './sessionAccounting';

const rows = (...uuids: (string | undefined)[]) => uuids.map((uuid) => ({ uuid }));

describe('snapshotSelection', () => {
    it('counts distinct UUIDs, rows without one, and collapsed repeats', () => {
        const sel = snapshotSelection(rows('a', 'b', 'a', undefined, '', 'c', 'b'));
        expect(sel.uuids).toEqual(['a', 'b', 'c']);
        expect(sel.noId).toBe(2);
        expect(sel.collapsed).toBe(2);
    });

    it('is a copy: later changes to the rows do not reach it', () => {
        const live = rows('a');
        const sel = snapshotSelection(live);
        live.push({ uuid: 'b' });
        expect(sel.uuids).toEqual(['a']);
    });
});

describe('exportReport', () => {
    const counts = (requested: number, exported: number, omitted: number) => ({
        requested,
        exported,
        omitted,
    });

    it('a complete export is an info toast with the confirmed count', () => {
        const sel = snapshotSelection(rows('a', 'b', 'c', 'd'));
        expect(exportReport(sel, counts(4, 4, 0))).toEqual({
            level: 'info',
            text: 'Exported 4 QSOs to ADIF',
        });
    });

    it('names every part of a short export against the whole selection', () => {
        const sel = snapshotSelection(rows('a', 'b', 'c', 'd', undefined));
        expect(exportReport(sel, counts(4, 3, 1))).toEqual({
            level: 'warn',
            text: 'Exported 3 of 5 selected QSOs. 1 could not be found in this archive. 1 has no QSO ID.',
        });
    });

    it('B2-10: one record in the file from four rows is never "Exported 4"', () => {
        const sel = snapshotSelection(rows('a', 'b', 'c', 'd'));
        expect(exportReport(sel, counts(4, 1, 3))).toEqual({
            level: 'warn',
            text: 'Exported 1 of 4 selected QSOs. 3 could not be found in this archive.',
        });
    });

    it('plural no-ID rows', () => {
        const sel = snapshotSelection(rows('a', undefined, undefined));
        expect(exportReport(sel, counts(1, 1, 0)).text).toBe(
            'Exported 1 of 3 selected QSOs. 2 have no QSO ID.'
        );
    });

    it('discloses collapsed duplicates, never as missing or delivered', () => {
        const sel = snapshotSelection(rows('a', 'a'));
        expect(exportReport(sel, counts(1, 1, 0))).toEqual({
            level: 'info',
            text: 'Exported 1 QSO to ADIF. Duplicate selections were counted once.',
        });
    });

    it('a duplicated UUID that is itself omitted', () => {
        const sel = snapshotSelection(rows('a', 'b', 'b'));
        expect(exportReport(sel, counts(2, 1, 1)).text).toBe(
            'Exported 1 of 2 selected QSOs. 1 could not be found in this archive. Duplicate selections were counted once.'
        );
    });

    const UNCONFIRMED =
        'The file was saved, but the daemon did not confirm how many QSOs it holds — check it before relying on it.';

    it.each([
        ['absent or malformed headers', null],
        // Self-consistent, but for three QSOs when two were sent.
        ['requested is not the distinct UUIDs sent', counts(3, 2, 1)],
        ['exported + omitted is not requested', counts(2, 1, 0)],
        ['nothing exported', counts(2, 0, 2)],
        ['a count over the selection', counts(2, 3, -1)],
    ])('%s: never a success claim', (_name, c) => {
        const sel = snapshotSelection(rows('a', 'b'));
        expect(exportReport(sel, c)).toEqual({ level: 'warn', text: UNCONFIRMED });
    });

    it('an unconfirmed export still states what the tab knows', () => {
        const sel = snapshotSelection(rows('a', 'a', undefined));
        expect(exportReport(sel, null).text).toBe(
            `${UNCONFIRMED} 1 has no QSO ID. Duplicate selections were counted once.`
        );
    });
});

describe('emailReport', () => {
    const TO = 'qsl@example.com';
    const lists = (sent: string[], omitted: string[], emailed: string[]) => ({
        sent,
        omitted,
        emailed,
    });

    it('a complete send is an info toast and marks every stamped row', () => {
        const sel = snapshotSelection(rows('a', 'b'));
        expect(emailReport(sel, TO, lists(['a', 'b'], [], ['a', 'b']))).toEqual({
            level: 'info',
            text: `Sent 2 QSOs to ${TO}`,
            mark: ['a', 'b'],
        });
    });

    it('a partial send names what could not be found', () => {
        const sel = snapshotSelection(rows('a', 'b', 'c', 'd'));
        expect(emailReport(sel, TO, lists(['a', 'b', 'c'], ['d'], ['a', 'b', 'c']))).toEqual({
            level: 'warn',
            text: `Sent 3 of 4 QSOs to ${TO}; 1 could not be found in this archive.`,
            mark: ['a', 'b', 'c'],
        });
    });

    it('rows without a QSO ID count in the denominator and are named', () => {
        const sel = snapshotSelection(rows('a', undefined));
        expect(emailReport(sel, TO, lists(['a'], [], ['a'])).text).toBe(
            `Sent 1 of 2 QSOs to ${TO}; 1 has no QSO ID.`
        );
    });

    it('partial stamping: only the stamped rows are marked, and the rest is said', () => {
        const sel = snapshotSelection(rows('a', 'b', 'c'));
        expect(emailReport(sel, TO, lists(['a', 'b', 'c'], [], ['a', 'b']))).toEqual({
            level: 'warn',
            text: `Sent 3 QSOs to ${TO}, but only 2 were recorded as emailed; the rest may be offered again.`,
            mark: ['a', 'b'],
        });
    });

    it('one stamped row reads "was"', () => {
        const sel = snapshotSelection(rows('a', 'b', 'c'));
        expect(emailReport(sel, TO, lists(['a', 'b'], ['c'], ['a'])).text).toBe(
            `Sent 2 of 3 QSOs to ${TO}; 1 could not be found in this archive, but only 1 was recorded as emailed; the rest may be offered again.`
        );
    });

    it('a stamp failure after the send marks nothing and says so', () => {
        const sel = snapshotSelection(rows('a', 'b'));
        expect(emailReport(sel, TO, lists(['a', 'b'], [], []))).toEqual({
            level: 'warn',
            text: `Sent 2 QSOs to ${TO}, but none were recorded as emailed.`,
            mark: [],
        });
    });

    it('discloses collapsed duplicates', () => {
        const sel = snapshotSelection(rows('a', 'a'));
        expect(emailReport(sel, TO, lists(['a'], [], ['a']))).toEqual({
            level: 'info',
            text: `Sent 1 QSO to ${TO}. Duplicate selections were counted once.`,
            mark: ['a'],
        });
    });

    const UNCONFIRMED = `Email sent to ${TO}, but the daemon did not confirm which QSOs it contained. No Session rows were marked as emailed.`;

    it.each([
        ['absent or malformed lists', null],
        ['a repeated UUID that hides a lost one', lists(['a', 'a'], ['b'], ['a'])],
        ['a repeated sent UUID with full coverage', lists(['a', 'a', 'b'], ['c'], ['a'])],
        ['a repeated omitted UUID', lists(['a', 'b'], ['c', 'c'], ['a'])],
        ['sent and omitted do not cover the selection', lists(['a'], ['b'], ['a'])],
        // Lengths sum to the selection, but b is both and c is neither.
        ['sent and omitted overlap', lists(['a', 'b'], ['b'], ['a'])],
        ['a UUID that was never selected', lists(['a', 'b', 'x'], [], ['a'])],
        ['emailed outside sent', lists(['a', 'b'], ['c'], ['a', 'c'])],
        ['a repeated emailed UUID', lists(['a', 'b', 'c'], [], ['a', 'a'])],
        ['nothing sent', lists([], ['a', 'b', 'c'], [])],
    ])('%s: rejected before anything is marked', (_name, l) => {
        const sel = snapshotSelection(rows('a', 'b', 'c'));
        expect(emailReport(sel, TO, l)).toEqual({ level: 'warn', text: UNCONFIRMED, mark: [] });
    });

    it('an unconfirmed send still states what the tab knows, and marks nothing', () => {
        const sel = snapshotSelection(rows('a', 'a', undefined));
        expect(emailReport(sel, TO, null)).toEqual({
            level: 'warn',
            text: `${UNCONFIRMED} 1 has no QSO ID. Duplicate selections were counted once.`,
            mark: [],
        });
    });
});
