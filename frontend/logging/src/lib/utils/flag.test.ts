import { describe, expect, it } from 'vitest';
import { ccodeToFlag } from './flag';

describe('ccodeToFlag', () => {
    it('maps a valid alpha-2 code to its flag emoji', () => {
        expect(ccodeToFlag('GB')).toBe('🇬🇧');
        expect(ccodeToFlag('US')).toBe('🇺🇸');
        expect(ccodeToFlag('MW')).toBe('🇲🇼'); // Malawi — the dogfood enrichment country
    });

    it('is case-insensitive and trims surrounding whitespace', () => {
        expect(ccodeToFlag('gb')).toBe('🇬🇧');
        expect(ccodeToFlag('  Us  ')).toBe('🇺🇸');
    });

    it('returns empty string for anything that is not exactly two letters', () => {
        // empty / nullish
        expect(ccodeToFlag('')).toBe('');
        expect(ccodeToFlag(null)).toBe('');
        expect(ccodeToFlag(undefined)).toBe('');
        // a DXCC number (the shape a non-hamnut source might supply) is not alpha-2
        expect(ccodeToFlag('291')).toBe('');
        // wrong length / non-letters
        expect(ccodeToFlag('G')).toBe('');
        expect(ccodeToFlag('GBR')).toBe('');
        expect(ccodeToFlag('G1')).toBe('');
    });
});
