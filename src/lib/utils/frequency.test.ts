import { describe, it, expect } from 'vitest';
import {
    formatCatKHzToDottedMHz,
    parseCatKHzToMHz,
    frequencyToBandFromCat,
    parseDottedKHzToMHz,
    formatDottedKHzToDottedMHz,
    dottedKHzToShortMHz,
    rawKHzStringToDottedMHz,
} from './frequency';

describe('frequency utilities', () => {
    describe('parseCatKHzToMHz', () => {
        it('parses valid 9-digit CAT kHz string to MHz', () => {
            // Example CAT strings interpreted as kHz with fixed width
            expect(parseCatKHzToMHz('007101000')).toBeCloseTo(7.101, 6);
            expect(parseCatKHzToMHz('014320000')).toBeCloseTo(14.32, 6);
        });

        it('returns null for invalid inputs', () => {
            expect(parseCatKHzToMHz('')).toBeNull();
            expect(parseCatKHzToMHz('abc')).toBeNull();
            expect(parseCatKHzToMHz('12345')).toBeNull();
        });
    });

    describe('formatCatKHzToDottedMHz', () => {
        it('formats valid CAT kHz strings to dotted MHz', () => {
            expect(formatCatKHzToDottedMHz('007101000')).toBe('7.101.000');
            expect(formatCatKHzToDottedMHz('014320000')).toBe('14.320.000');
        });

        it('returns empty string for invalid input', () => {
            expect(formatCatKHzToDottedMHz('')).toBe('');
            expect(formatCatKHzToDottedMHz('abc')).toBe('');
            expect(formatCatKHzToDottedMHz('12345')).toBe('');
        });
    });

    describe('frequencyToBandFromCat', () => {
        it('maps CAT kHz strings to correct bands', () => {
            // 7.101 MHz -> 40m
            expect(frequencyToBandFromCat('007101000')).toBe('40m');
            // 14.320 MHz -> 20m
            expect(frequencyToBandFromCat('014320000')).toBe('20m');
            // Zero / invalid -> no band
            expect(frequencyToBandFromCat('000000000')).toBe('');
        });
    });

    describe('parseDottedKHzToMHz', () => {
        it('parses dotted kHz strings into MHz', () => {
            expect(parseDottedKHzToMHz('7.101.000')).toBeCloseTo(7.101, 6);
            expect(parseDottedKHzToMHz('14.320.000')).toBeCloseTo(14.32, 6);
        });

        it('returns null for invalid dotted kHz inputs', () => {
            expect(parseDottedKHzToMHz('')).toBeNull();
            expect(parseDottedKHzToMHz('abc')).toBeNull();
            expect(parseDottedKHzToMHz('7..101.000')).toBeNull();
        });
    });

    describe('formatDottedKHzToDottedMHz', () => {
        it('normalises dotted kHz strings to dotted MHz', () => {
            expect(formatDottedKHzToDottedMHz('7.101.000')).toBe('7.101.000');
            expect(formatDottedKHzToDottedMHz('14.320.000')).toBe('14.320.000');
        });

        it('returns empty string for invalid dotted kHz input', () => {
            expect(formatDottedKHzToDottedMHz('')).toBe('');
            expect(formatDottedKHzToDottedMHz('abc')).toBe('');
        });
    });

    describe('dottedKHzToShortMHz', () => {
        it('converts dotted kHz string to short MHz string', () => {
            expect(dottedKHzToShortMHz('14.320.000')).toBe('14.320');
            expect(dottedKHzToShortMHz('7.101.000')).toBe('7.101');
        });

        it('returns empty string for invalid inputs', () => {
            expect(dottedKHzToShortMHz('')).toBe('');
            expect(dottedKHzToShortMHz('abc')).toBe('');
        });
    });

    describe('rawKHzStringToDottedMHz', () => {
        it('formats plain kHz strings into dotted MHz', () => {
            expect(rawKHzStringToDottedMHz('14320')).toBe('14.320');
            expect(rawKHzStringToDottedMHz('7101')).toBe('7.101');
        });

        it('trims whitespace and rejects invalid input', () => {
            expect(rawKHzStringToDottedMHz(' 14320 ')).toBe('14.320');
            expect(rawKHzStringToDottedMHz('abc')).toBe('');
            expect(rawKHzStringToDottedMHz('')).toBe('');
            expect(rawKHzStringToDottedMHz('0000')).toBe('');
        });
    });
});
