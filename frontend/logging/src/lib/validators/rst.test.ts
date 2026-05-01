import { describe, it, expect } from 'vitest';
import { isValidRst } from './rst';

describe('isValidRst', () => {
    it('accepts a 2-digit RST', () => {
        expect(isValidRst('59')).toBe(true);
    });

    it('accepts a 3-digit RST', () => {
        expect(isValidRst('599')).toBe(true);
    });

    it('rejects a 1-digit RST', () => {
        expect(isValidRst('5')).toBe(false);
    });

    it('rejects a 4-digit RST', () => {
        expect(isValidRst('5999')).toBe(false);
    });

    it('treats empty string as not-invalid (presence is a separate concern)', () => {
        expect(isValidRst('')).toBe(true);
    });

    it('treats whitespace-only string as not-invalid', () => {
        expect(isValidRst('   ')).toBe(true);
    });

    it('rejects letters', () => {
        expect(isValidRst('5A9')).toBe(false);
    });
});
