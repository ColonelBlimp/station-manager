import { describe, it, expect } from 'vitest';
import { isValidFrequency, parseFrequency } from './frequency';

describe('parseFrequency', () => {
    describe('display format MHz.kHz.Hz', () => {
        it('parses a typical 20m frequency', () => {
            expect(parseFrequency('14.250.000')).toBe(14_250_000);
        });

        it('parses a 40m frequency with non-zero Hz', () => {
            expect(parseFrequency('7.000.500')).toBe(7_000_500);
        });

        it('parses a 2m frequency', () => {
            expect(parseFrequency('144.300.000')).toBe(144_300_000);
        });

        it('rejects kHz segment greater than 999', () => {
            // The regex constrains segments to 1-3 digits, so "1000" can't match
            // — but "999" is the max valid; anything "looks like 1000" hits regex first.
            expect(parseFrequency('14.9999.000')).toBeNull();
        });
    });

    describe('decimal MHz', () => {
        it('parses bare MHz integer', () => {
            expect(parseFrequency('14')).toBe(14_000_000);
        });

        it('parses MHz with two-digit fraction', () => {
            expect(parseFrequency('14.25')).toBe(14_250_000);
        });

        it('parses MHz with three-digit fraction (kHz precision)', () => {
            expect(parseFrequency('14.250')).toBe(14_250_000);
        });

        it('parses MHz with six-digit fraction (Hz precision)', () => {
            expect(parseFrequency('14.123456')).toBe(14_123_456);
        });

        it('rejects more than six fractional digits', () => {
            expect(parseFrequency('14.1234567')).toBeNull();
        });
    });

    describe('empty / whitespace', () => {
        it('returns null for empty string', () => {
            expect(parseFrequency('')).toBeNull();
        });

        it('returns null for whitespace-only', () => {
            expect(parseFrequency('   ')).toBeNull();
        });

        it('trims surrounding whitespace before parsing', () => {
            expect(parseFrequency('  14.250.000  ')).toBe(14_250_000);
        });
    });

    describe('malformed input', () => {
        it('rejects leading period', () => {
            expect(parseFrequency('.250')).toBeNull();
        });

        it('rejects trailing period', () => {
            expect(parseFrequency('14.')).toBeNull();
        });

        it('rejects four-segment forms', () => {
            expect(parseFrequency('14.250.000.500')).toBeNull();
        });

        it('rejects non-digit characters', () => {
            expect(parseFrequency('14.250.0a0')).toBeNull();
        });

        it('rejects negative numbers', () => {
            expect(parseFrequency('-14.250')).toBeNull();
        });

        it('rejects scientific notation', () => {
            expect(parseFrequency('1.4e7')).toBeNull();
        });
    });

    describe('range', () => {
        it('rejects below 100 kHz', () => {
            expect(parseFrequency('0.05')).toBeNull(); // 50 kHz
        });

        it('accepts at 100 kHz', () => {
            expect(parseFrequency('0.1')).toBe(100_000);
        });

        it('accepts at 30 GHz', () => {
            expect(parseFrequency('30000')).toBe(30_000_000_000);
        });

        it('rejects above 30 GHz', () => {
            expect(parseFrequency('30001')).toBeNull();
        });
    });
});

describe('isValidFrequency', () => {
    it('returns null for empty string (presence is form-level)', () => {
        expect(isValidFrequency('')).toBeNull();
    });

    it('returns null for whitespace-only', () => {
        expect(isValidFrequency('   ')).toBeNull();
    });

    it('returns null for a valid display-format frequency', () => {
        expect(isValidFrequency('14.250.000')).toBeNull();
    });

    it('returns null for a valid decimal MHz', () => {
        expect(isValidFrequency('14.250')).toBeNull();
    });

    it('returns the i18n key for malformed input', () => {
        expect(isValidFrequency('14.')).toBe('validators.frequency');
    });

    it('returns the i18n key for out-of-range', () => {
        expect(isValidFrequency('99999')).toBe('validators.frequency');
    });
});
