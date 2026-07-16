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

import { isNonstandardCall, isCqType4 } from './ft8Message';

describe('isNonstandardCall (ADR 0048 type-4 detection)', () => {
    it('true for prefix-compound and non-/P suffixes', () => {
        expect(isNonstandardCall('PJ4/NA2AA')).toBe(true); // prefix-compound
        expect(isNonstandardCall('K1ABC/D')).toBe(true); // /D suffix
        expect(isNonstandardCall('K1ABC/4')).toBe(true); // digit suffix
        expect(isNonstandardCall('W1AW/MM')).toBe(true); // /MM
    });
    it('false for a plain call and the standard /P variant', () => {
        expect(isNonstandardCall('7Q5MLV')).toBe(false); // plain
        expect(isNonstandardCall('NA2AA/P')).toBe(false); // /P packs standard
    });
    it('false for /R — encodes but go-ft8 cannot decode it, so it is not answerable', () => {
        expect(isNonstandardCall('NA2AA/R')).toBe(false);
    });
});

describe('isCqType4', () => {
    it('true for a nonstandard-call CQ', () => {
        expect(isCqType4('CQ PJ4/NA2AA')).toBe(true);
        expect(isCqType4('CQ K1ABC/D')).toBe(true);
        expect(isCqType4('CQ DX PJ4/NA2AA')).toBe(true);
    });
    it('false for a standard CQ, a /P CQ, and a CQ FD', () => {
        expect(isCqType4('CQ K1ABC FN42')).toBe(false);
        expect(isCqType4('CQ NA2AA/P FN42')).toBe(false);
        expect(isCqType4('CQ FD PJ4/NA2AA')).toBe(false); // FD is its own path
    });
    it('false for a non-CQ line', () => {
        expect(isCqType4('K1ABC PJ4/NA2AA RR73')).toBe(false);
    });
});
