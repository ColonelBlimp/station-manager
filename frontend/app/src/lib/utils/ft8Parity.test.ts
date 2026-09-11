import { describe, expect, it } from 'vitest';
import { slotClock, slotMsFor, slotParity } from './ft8Parity';

// W-0019 slice 4 (ADR 0080): the slot clock follows the active profile —
// 15 s for FT8, 7.5 s for FT4 — for parity and for the countdown pill.
describe('profile slot clock', () => {
    it("slotMsFor names each profile's period", () => {
        expect(slotMsFor('FT8')).toBe(15_000);
        expect(slotMsFor('FT4')).toBe(7_500);
        expect(slotMsFor('')).toBe(15_000);
    });

    it('slotParity on the FT4 lattice: :00.0 even, :07.5 odd, :15.0 even', () => {
        expect(slotParity('2026-09-12T14:30:00.000Z', 7_500)).toBe('even');
        expect(slotParity('2026-09-12T14:30:07.500Z', 7_500)).toBe('odd');
        expect(slotParity('2026-09-12T14:30:15.000Z', 7_500)).toBe('even');
        // The FT8 lattice is unchanged (and the default).
        expect(slotParity('2026-09-12T14:30:15Z')).toBe('odd');
        expect(slotParity('2026-09-12T14:30:15Z', 15_000)).toBe('odd');
    });

    it("slotClock counts down within the profile's period", () => {
        const t = Date.UTC(2026, 8, 12, 14, 30, 9); // 14:30:09
        expect(slotClock(t, 15_000)).toEqual({ secsLeft: 6, parity: 'even' }); // :00–:15 is even on FT8
        expect(slotClock(t, 7_500)).toEqual({ secsLeft: 6, parity: 'odd' }); // :07.5–:15.0 is odd on FT4
        expect(slotClock(Date.UTC(2026, 8, 12, 14, 30, 3), 7_500)).toEqual({
            secsLeft: 5,
            parity: 'even',
        });
        expect(slotClock(Date.UTC(2026, 8, 12, 14, 30, 22, 400), 7_500).secsLeft).toBe(1);
    });
});
