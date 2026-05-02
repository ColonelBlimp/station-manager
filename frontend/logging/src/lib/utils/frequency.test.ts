import { describe, it, expect } from 'vitest';
import { frequencyToBand } from './frequency';

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

    it('returns 60m for the widened range (covers regional variations)', () => {
        expect(frequencyToBand(5_330_000)).toBe('60m');
        expect(frequencyToBand(5_403_000)).toBe('60m');
    });

    it('returns 2m', () => {
        expect(frequencyToBand(146_000_000)).toBe('2m');
    });

    it('returns 70cm', () => {
        expect(frequencyToBand(435_000_000)).toBe('70cm');
    });

    it('returns empty string between bands', () => {
        // 5 MHz is between 80m (3.5–4.0) and 60m (5.25–5.45)
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
