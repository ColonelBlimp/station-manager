import { describe, it, expect } from 'vitest';
import { formatFrequency, frequencyToBand } from './frequency';

describe('frequencyToBand', () => {
    it('returns 20m for 14.250 MHz', () => {
        expect(frequencyToBand(14_250_000)).toBe('20m');
    });

    it('returns 40m for 7.100 MHz', () => {
        expect(frequencyToBand(7_100_000)).toBe('40m');
    });

    it('returns 80m for 3.750 MHz', () => {
        expect(frequencyToBand(3_750_000)).toBe('80m');
    });

    it('returns 160m at the lower edge', () => {
        expect(frequencyToBand(1_800_000)).toBe('160m');
    });

    it('returns 10m at the upper edge', () => {
        expect(frequencyToBand(29_700_000)).toBe('10m');
    });

    it('returns 60m across the ADIF range (5.06–5.45 MHz)', () => {
        expect(frequencyToBand(5_060_000)).toBe('60m'); // ADIF low edge (below old 5.25 floor)
        expect(frequencyToBand(5_100_000)).toBe('60m');
        expect(frequencyToBand(5_330_000)).toBe('60m');
        expect(frequencyToBand(5_403_000)).toBe('60m');
    });

    it('returns 4m across the ADIF range (70–71 MHz)', () => {
        expect(frequencyToBand(70_600_000)).toBe('4m'); // above the old 70.5 ceiling
        expect(frequencyToBand(71_000_000)).toBe('4m');
        expect(frequencyToBand(71_100_000)).toBe('');
    });

    it('returns 2m', () => {
        expect(frequencyToBand(146_000_000)).toBe('2m');
    });

    it('returns 70cm', () => {
        expect(frequencyToBand(435_000_000)).toBe('70cm');
    });

    it('returns empty string between bands', () => {
        // 5 MHz is between 80m (3.5–4.0) and 60m (5.06–5.45)
        expect(frequencyToBand(5_000_000)).toBe('');
    });

    it('returns empty string below 160m', () => {
        expect(frequencyToBand(500_000)).toBe('');
    });

    it('returns empty string above 23cm', () => {
        expect(frequencyToBand(2_000_000_000)).toBe('');
    });

    it('returns empty string for 0', () => {
        expect(frequencyToBand(0)).toBe('');
    });
});

describe('formatFrequency', () => {
    it('formats the doc-comment example', () => {
        // The function's own doc-comment shows 14_250_000 → "14.250.000".
        // Pin it so the doc-comment can't silently drift from the code.
        expect(formatFrequency(14_250_000)).toBe('14.250.000');
    });

    it('pads sub-MHz parts with leading zeros to 3 digits', () => {
        // 5 Hz exercises both pad sites: kHz=0 → "000", Hz=5 → "005".
        // Without padStart the output would be "0.0.5" — not a parseable
        // dot-grouped frequency.
        expect(formatFrequency(5)).toBe('0.000.005');
    });

    it('zero renders as 0.000.000', () => {
        expect(formatFrequency(0)).toBe('0.000.000');
    });

    it('formats common HF watering-hole frequencies', () => {
        expect(formatFrequency(7_074_000)).toBe('7.074.000'); // 40m FT8
        expect(formatFrequency(14_074_000)).toBe('14.074.000'); // 20m FT8
        expect(formatFrequency(50_313_000)).toBe('50.313.000'); // 6m FT8
    });

    it('handles the 1 MHz boundary cleanly', () => {
        expect(formatFrequency(1_000_000)).toBe('1.000.000');
        // 999_999 sits just under the boundary — exercises mhz=0 with a
        // fully-padded kHz/Hz pair on the same value.
        expect(formatFrequency(999_999)).toBe('0.999.999');
    });

    it('pads kHz tens when only the lower digits are populated', () => {
        // 14_050_000: kHz part is 50, must render "050" so the operator
        // can scan the dot-grouped triples by position.
        expect(formatFrequency(14_050_000)).toBe('14.050.000');
    });

    it('pads Hz tens when only the lower digits are populated', () => {
        // 14_250_007: Hz part is 7, must render "007".
        expect(formatFrequency(14_250_007)).toBe('14.250.007');
    });

    it('handles a microwave frequency (MHz exceeds 3 digits)', () => {
        // 23cm: 1_296_000_000 → "1296.000.000". The MHz field is NOT
        // padded or truncated; only kHz and Hz get padStart(3). Pinning
        // this guards against a future "always pad MHz to 4 digits"
        // refactor that would break HF rendering.
        expect(formatFrequency(1_296_000_000)).toBe('1296.000.000');
    });

    it('preserves sub-kHz precision', () => {
        // 1_800_001 → 1.800.001 (160m bottom edge + 1 Hz). Rig CAT data
        // is integer-Hz; this is the smallest representable change.
        expect(formatFrequency(1_800_001)).toBe('1.800.001');
    });

    it('never produces fewer than three dot-separated groups', () => {
        // Structural pin: the output is always "<mhz>.<kHz padded>.<Hz padded>".
        // A consumer parser (e.g. SessionPanel) relies on the three-group
        // shape regardless of magnitude — including zero.
        for (const hz of [0, 1, 1_000, 1_000_000, 1_000_000_000]) {
            expect(formatFrequency(hz).split('.')).toHaveLength(3);
        }
    });

    it('coerces a negative value to "0.000.000" (defensive — never produced by CAT)', () => {
        // N4 guard. Pre-fix, hz=-1 produced "-1.-01.-01" — would break
        // every dot-group consumer downstream. The clamp keeps the
        // three-group shape stable even on nonsensical input.
        expect(formatFrequency(-1)).toBe('0.000.000');
        expect(formatFrequency(-14_250_000)).toBe('0.000.000');
    });

    it('floors a fractional Hz value before formatting', () => {
        // CAT delivers integer Hz; a fractional value (e.g. via accidental
        // math elsewhere) used to leak NaN into the padded segments.
        // Now floored to the nearest non-negative integer.
        expect(formatFrequency(14_250_000.7)).toBe('14.250.000');
        expect(formatFrequency(14_250_000.999)).toBe('14.250.000');
    });
});
