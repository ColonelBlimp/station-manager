// dxccFlag: numeric DXCC → flag emoji for stored-QSO surfaces (no ccode on
// rows). Pins the split-entity conventions and the degrade-to-blank posture.

import { describe, it, expect } from 'vitest';
import { dxccFlag } from './dxccFlag';

describe('dxccFlag', () => {
    it('maps common entities', () => {
        expect(dxccFlag('288')).toBe('🇺🇦'); // Ukraine
        expect(dxccFlag('339')).toBe('🇯🇵'); // Japan
        expect(dxccFlag('291')).toBe('🇺🇸'); // United States
        expect(dxccFlag('440')).toBe('🇲🇼'); // Malawi — home!
    });

    it('collapses DXCC splits to their ISO parent', () => {
        expect(dxccFlag('223')).toBe('🇬🇧'); // England
        expect(dxccFlag('279')).toBe('🇬🇧'); // Scotland
        expect(dxccFlag('54')).toBe('🇷🇺'); // European Russia
        expect(dxccFlag('15')).toBe('🇷🇺'); // Asiatic Russia
        expect(dxccFlag('225')).toBe('🇮🇹'); // Sardinia
        expect(dxccFlag('110')).toBe('🇺🇸'); // Hawaii
    });

    it('degrades to blank — unmapped, empty, junk', () => {
        expect(dxccFlag('9999')).toBe('');
        expect(dxccFlag('')).toBe('');
        expect(dxccFlag(undefined)).toBe('');
        expect(dxccFlag(null)).toBe('');
    });
});
