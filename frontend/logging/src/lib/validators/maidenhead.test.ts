import { describe, it, expect } from 'vitest';
import { isValidMaidenhead } from './maidenhead';

describe('isValidMaidenhead', () => {
    it('accepts a 4-char field+square', () => {
        expect(isValidMaidenhead('IO91')).toBeNull();
    });

    it('accepts a 6-char subsquare', () => {
        expect(isValidMaidenhead('IO91vl')).toBeNull();
    });

    it('accepts an 8-char extended subsquare', () => {
        expect(isValidMaidenhead('IO91vl42')).toBeNull();
    });

    it('case-folds before matching (lowercase field)', () => {
        expect(isValidMaidenhead('io91vl')).toBeNull();
    });

    it('case-folds before matching (uppercase subsquare)', () => {
        expect(isValidMaidenhead('IO91VL')).toBeNull();
    });

    it('trims surrounding whitespace', () => {
        expect(isValidMaidenhead('  IO91vl  ')).toBeNull();
    });

    it('treats empty string as not-invalid (presence is a separate concern)', () => {
        expect(isValidMaidenhead('')).toBeNull();
    });

    it('treats whitespace-only string as not-invalid', () => {
        expect(isValidMaidenhead('   ')).toBeNull();
    });

    it('rejects 5-char input (between square and subsquare)', () => {
        expect(isValidMaidenhead('IO91v')).toBe('validators.maidenhead');
    });

    it('rejects 7-char input (between subsquare and extended)', () => {
        expect(isValidMaidenhead('IO91vl4')).toBe('validators.maidenhead');
    });

    it('rejects field chars beyond R', () => {
        expect(isValidMaidenhead('SS91')).toBe('validators.maidenhead');
    });

    it('rejects subsquare chars beyond X', () => {
        expect(isValidMaidenhead('IO91yy')).toBe('validators.maidenhead');
    });

    it('rejects digits in the field position', () => {
        expect(isValidMaidenhead('1O91vl')).toBe('validators.maidenhead');
    });

    it('rejects letters in the square position', () => {
        expect(isValidMaidenhead('IOAAvl')).toBe('validators.maidenhead');
    });

    it('rejects non-alphanumeric characters', () => {
        expect(isValidMaidenhead('IO-91')).toBe('validators.maidenhead');
    });
});
