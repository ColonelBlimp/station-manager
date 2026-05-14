import { describe, it, expect } from 'vitest';
import { isValidRst } from './rst';

describe('isValidRst', () => {
    it('accepts a 2-digit RST', () => {
        expect(isValidRst('59')).toBeNull();
    });

    it('accepts a 3-digit RST', () => {
        expect(isValidRst('599')).toBeNull();
    });

    it('rejects a 1-digit RST', () => {
        expect(isValidRst('5')).toBe('validators.rst');
    });

    it('rejects a 4-digit RST', () => {
        expect(isValidRst('5999')).toBe('validators.rst');
    });

    it('treats empty string as not-invalid (presence is a separate concern)', () => {
        expect(isValidRst('')).toBeNull();
    });

    it('treats whitespace-only string as not-invalid', () => {
        expect(isValidRst('   ')).toBeNull();
    });

    it('rejects letters', () => {
        expect(isValidRst('5A9')).toBe('validators.rst');
    });
});
