// parseSender is the directed-call seam: the sender of any plain decode line is
// callable without waiting for their CQ (a DX running a pile-up can go many
// minutes between CQs). These pin what counts as a callable sender — and what
// must not (CQ lines route to parseCq; hashed senders have no known callsign).

import { describe, it, expect } from 'vitest';
import { parseSender } from './ft8Message';

describe('parseSender', () => {
    it('extracts the sender from a report line (no grid)', () => {
        expect(parseSender('K1ABC T22TT -05')).toEqual({ call: 'T22TT', grid: '' });
    });

    it('extracts the sender and their grid from a grid line', () => {
        expect(parseSender('K1ABC T22TT RI91')).toEqual({ call: 'T22TT', grid: 'RI91' });
    });

    it('treats RR73 as a roger, never the grid (Maidenhead collision)', () => {
        expect(parseSender('K1ABC T22TT RR73')).toEqual({ call: 'T22TT', grid: '' });
    });

    it('accepts a two-token line (73 exchanges truncate)', () => {
        expect(parseSender('K1ABC T22TT')).toEqual({ call: 'T22TT', grid: '' });
    });

    it('accepts a hashed ADDRESSEE — the sender is still callable', () => {
        expect(parseSender('<KH1/KH7Z> T22TT RR73')).toEqual({ call: 'T22TT', grid: '' });
    });

    it('rejects a hashed SENDER — the real callsign is unknown', () => {
        expect(parseSender('K1ABC <T22TT> R-07')).toBeNull();
    });

    it('rejects CQ lines (they route to parseCq)', () => {
        expect(parseSender('CQ T22TT RI91')).toBeNull();
        expect(parseSender('CQ DX T22TT RI91')).toBeNull();
    });

    it('rejects lines whose sender token is not a callsign', () => {
        expect(parseSender('K1ABC 73')).toBeNull(); // no letter in sender slot
        expect(parseSender('TNX 73')).toBeNull(); // addressee not a call either
    });

    it('is case-insensitive', () => {
        expect(parseSender('k1abc t22tt ri91')).toEqual({ call: 'T22TT', grid: 'RI91' });
    });
});
