import { describe, it, expect } from 'vitest';
import { signalProximity, offsetFromFraction, clampOffset } from './ft8Spectrum';
import type { Ft8Band } from '../api/ft8-sse';

const band = (low: number, high: number): Ft8Band => ({ low_hz: low, high_hz: high });

describe('signalProximity', () => {
    it('is clear with no signals', () => {
        expect(signalProximity(1500, 50, [], 50)).toEqual({ kind: 'clear', gapHz: null });
    });

    it('is sharing when the footprint overlaps a signal', () => {
        // footprint [1500,1550] overlaps [1530,1580]
        expect(signalProximity(1500, 50, [band(1530, 1580)], 50)).toEqual({
            kind: 'sharing',
            gapHz: 0,
        });
    });

    it('is near when a signal sits within the margin but does not overlap', () => {
        // footprint [1500,1550]; signal [1585,1635] → gap 35 ≤ margin 50
        expect(signalProximity(1500, 50, [band(1585, 1635)], 50)).toEqual({
            kind: 'near',
            gapHz: 35,
        });
    });

    it('is clear when the nearest signal is beyond the margin', () => {
        // footprint [1500,1550]; signal [1800,1850] → gap 250 > margin 50
        expect(signalProximity(1500, 50, [band(1800, 1850)], 50)).toEqual({
            kind: 'clear',
            gapHz: 250,
        });
    });

    it('reports the nearest of several signals (below and above)', () => {
        // footprint [1500,1550]; below [1400,1480] gap 20; above [1700,1750] gap 150
        expect(signalProximity(1500, 50, [band(1400, 1480), band(1700, 1750)], 50)).toEqual({
            kind: 'near',
            gapHz: 20,
        });
    });
});

describe('offsetFromFraction / clampOffset', () => {
    it('maps a fraction across the passband', () => {
        expect(offsetFromFraction(0, 200, 3000, 50)).toBe(200);
        expect(offsetFromFraction(0.5, 200, 3000, 50)).toBe(1600);
    });

    it('clamps so the footprint stays inside the passband', () => {
        // frac 1.0 would put the offset at 3000, but the 50 Hz footprint must fit → 2950
        expect(offsetFromFraction(1, 200, 3000, 50)).toBe(2950);
        expect(clampOffset(5000, 200, 3000, 50)).toBe(2950);
        expect(clampOffset(-100, 200, 3000, 50)).toBe(200);
    });
});
