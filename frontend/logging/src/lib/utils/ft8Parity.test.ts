import { describe, it, expect } from 'vitest';
import { slotParity } from './ft8Parity';

describe('slotParity', () => {
    // The daemon convention: (unix / 15) % 2 == 0 → even. On the UTC clock that is
    // :00/:30 = even, :15/:45 = odd. A whole minute (unix multiple of 60) divides by
    // 15 to a multiple of 4 → even, so :00 is always even regardless of the minute.
    it('maps :00 and :30 to even', () => {
        expect(slotParity('2026-06-27T10:00:00Z')).toBe('even');
        expect(slotParity('2026-06-27T10:00:30Z')).toBe('even');
        expect(slotParity('2026-06-27T10:01:00Z')).toBe('even');
        expect(slotParity('2026-06-27T10:01:30Z')).toBe('even');
    });

    it('maps :15 and :45 to odd', () => {
        expect(slotParity('2026-06-27T10:00:15Z')).toBe('odd');
        expect(slotParity('2026-06-27T10:00:45Z')).toBe('odd');
        expect(slotParity('2026-06-27T10:01:15Z')).toBe('odd');
        expect(slotParity('2026-06-27T10:01:45Z')).toBe('odd');
    });

    it('agrees with a non-UTC offset that resolves to the same instant', () => {
        // 12:00:15+02:00 == 10:00:15Z → odd. Parity is about the absolute instant,
        // not the wall-clock zone (matches the daemon, which works in Unix seconds).
        expect(slotParity('2026-06-27T12:00:15+02:00')).toBe('odd');
        expect(slotParity('2026-06-27T12:00:00+02:00')).toBe('even');
    });

    it('returns "" for missing or unparseable input', () => {
        expect(slotParity('')).toBe('');
        expect(slotParity(undefined)).toBe('');
        expect(slotParity('not-a-date')).toBe('');
    });
});
