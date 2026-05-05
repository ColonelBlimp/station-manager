import { describe, it, expect } from 'vitest';
import { isValidMaidenhead } from './maidenhead';

describe('isValidMaidenhead', () => {
    it('accepts a 4-char field+square', () => {
        expect(isValidMaidenhead('IO91')).toBe(true);
    });

    it('accepts a 6-char subsquare', () => {
        expect(isValidMaidenhead('IO91vl')).toBe(true);
    });

    it('accepts an 8-char extended subsquare', () => {
        expect(isValidMaidenhead('IO91vl42')).toBe(true);
    });

    it('case-folds before matching (lowercase field)', () => {
        expect(isValidMaidenhead('io91vl')).toBe(true);
    });

    it('case-folds before matching (uppercase subsquare)', () => {
        expect(isValidMaidenhead('IO91VL')).toBe(true);
    });

    it('trims surrounding whitespace', () => {
        expect(isValidMaidenhead('  IO91vl  ')).toBe(true);
    });

    it('treats empty string as not-invalid (presence is a separate concern)', () => {
        expect(isValidMaidenhead('')).toBe(true);
    });

    it('treats whitespace-only string as not-invalid', () => {
        expect(isValidMaidenhead('   ')).toBe(true);
    });

    it('rejects 5-char input (between square and subsquare)', () => {
        expect(isValidMaidenhead('IO91v')).toBe(false);
    });

    it('rejects 7-char input (between subsquare and extended)', () => {
        expect(isValidMaidenhead('IO91vl4')).toBe(false);
    });

    it('rejects field chars beyond R', () => {
        expect(isValidMaidenhead('SS91')).toBe(false);
    });

    it('rejects subsquare chars beyond X', () => {
        expect(isValidMaidenhead('IO91yy')).toBe(false);
    });

    it('rejects digits in the field position', () => {
        expect(isValidMaidenhead('1O91vl')).toBe(false);
    });

    it('rejects letters in the square position', () => {
        expect(isValidMaidenhead('IOAAvl')).toBe(false);
    });

    it('rejects non-alphanumeric characters', () => {
        expect(isValidMaidenhead('IO-91')).toBe(false);
    });
});
