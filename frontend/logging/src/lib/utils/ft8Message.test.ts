import { describe, expect, it } from 'vitest';
import { parseCqCall } from './ft8Message';

describe('parseCqCall', () => {
    it('parses a plain CQ with grid', () => {
        expect(parseCqCall('CQ K1ABC FN42')).toBe('K1ABC');
    });

    it('parses CQ with no grid', () => {
        expect(parseCqCall('CQ G3XYZ')).toBe('G3XYZ');
    });

    it('skips the DX modifier', () => {
        expect(parseCqCall('CQ DX K1ABC FN42')).toBe('K1ABC');
    });

    it('skips a directional region modifier', () => {
        expect(parseCqCall('CQ EU G3XYZ IO91')).toBe('G3XYZ');
    });

    it('skips a contest/activity tag', () => {
        expect(parseCqCall('CQ POTA W2ABC')).toBe('W2ABC');
        expect(parseCqCall('CQ TEST G3XYZ IO91')).toBe('G3XYZ');
    });

    it('skips a directed-CQ numeric token', () => {
        expect(parseCqCall('CQ 030 K1ABC FN42')).toBe('K1ABC');
    });

    it('keeps a compound callsign intact', () => {
        expect(parseCqCall('CQ G3XYZ/P IO91')).toBe('G3XYZ/P');
    });

    it('is whitespace- and case-tolerant', () => {
        expect(parseCqCall('  cq   k1abc   fn42 ')).toBe('K1ABC');
    });

    it('returns null for non-CQ messages', () => {
        expect(parseCqCall('K1ABC W2XYZ -10')).toBeNull();
        expect(parseCqCall('W2XYZ K1ABC RR73')).toBeNull();
        expect(parseCqCall('')).toBeNull();
    });

    it('returns null for a CQ with no recognisable callsign', () => {
        expect(parseCqCall('CQ DX')).toBeNull();
    });
});
