// Pins the palette contract: normalisation (ADIF band tokens arrive in
// mixed case), the override layer (config `map.band_colors` rides on top),
// and the never-invisible fallback for bands the palette doesn't know.

import { describe, it, expect } from 'vitest';
import {
    bandColor,
    bandRank,
    normalizeBand,
    DEFAULT_BAND_COLORS,
    FALLBACK_BAND_COLOR,
} from './bandColors';

describe('normalizeBand', () => {
    it('lowercases and trims ADIF band tokens', () => {
        expect(normalizeBand('20M ')).toBe('20m');
        expect(normalizeBand(' 70CM')).toBe('70cm');
        expect(normalizeBand(undefined)).toBe('');
    });
});

describe('bandColor', () => {
    it('resolves known bands case-insensitively', () => {
        expect(bandColor('20m')).toBe(DEFAULT_BAND_COLORS['20m']);
        expect(bandColor('20M')).toBe(DEFAULT_BAND_COLORS['20m']);
    });

    it('falls back to gray for unknown or missing bands', () => {
        expect(bandColor('13cm')).toBe(FALLBACK_BAND_COLOR);
        expect(bandColor(undefined)).toBe(FALLBACK_BAND_COLOR);
        expect(bandColor('')).toBe(FALLBACK_BAND_COLOR);
    });

    it('lets an operator override win over the default', () => {
        expect(bandColor('20m', { '20m': '#000000' })).toBe('#000000');
        // An override for a band outside the default palette works too.
        expect(bandColor('13cm', { '13cm': '#123456' })).toBe('#123456');
        // Overrides for OTHER bands don't leak.
        expect(bandColor('40m', { '20m': '#000000' })).toBe(DEFAULT_BAND_COLORS['40m']);
    });

    it('gives every default palette colour a distinct value', () => {
        const values = Object.values(DEFAULT_BAND_COLORS);
        expect(new Set(values).size).toBe(values.length);
    });
});

describe('bandRank', () => {
    it('orders by wavelength with unknown bands last', () => {
        expect(bandRank('160m')).toBeLessThan(bandRank('20m'));
        expect(bandRank('20m')).toBeLessThan(bandRank('70cm'));
        expect(bandRank('13cm')).toBeGreaterThan(bandRank('70cm'));
        expect(bandRank('')).toBeGreaterThan(bandRank('70cm'));
    });
});
