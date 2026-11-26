import { describe, it, expect } from 'vitest';
import { formatDurationHms, formatSessionStart } from './time';

describe('formatDurationHms', () => {
    it('formats zero as 00:00:00', () => {
        expect(formatDurationHms(0)).toBe('00:00:00');
    });

    it('formats one second', () => {
        expect(formatDurationHms(1000)).toBe('00:00:01');
    });

    it('formats one minute', () => {
        expect(formatDurationHms(60_000)).toBe('00:01:00');
    });

    it('formats one hour', () => {
        expect(formatDurationHms(3_600_000)).toBe('01:00:00');
    });

    it('formats a typical operating-session length (2h 15m 30s)', () => {
        expect(formatDurationHms((2 * 3600 + 15 * 60 + 30) * 1000)).toBe('02:15:30');
    });

    it('handles the 24-hour boundary cleanly (does not wrap)', () => {
        expect(formatDurationHms(24 * 3600 * 1000)).toBe('24:00:00');
    });

    it('grows the hours field past 99 for very long sessions (Field Day)', () => {
        expect(formatDurationHms(100 * 3600 * 1000)).toBe('100:00:00');
    });

    it('floors fractional milliseconds to whole seconds', () => {
        expect(formatDurationHms(1_999)).toBe('00:00:01');
    });

    it('clamps negative inputs to 00:00:00', () => {
        expect(formatDurationHms(-1)).toBe('00:00:00');
    });
});

// The Session panel's "Started …" (W-0029 part 6, operator ruling 2026-11-25).
describe('formatSessionStart', () => {
    const now = new Date('2026-11-25T23:59:30Z');

    it('gives the time alone for a start today, in UTC', () => {
        expect(formatSessionStart('2026-11-25T09:15:42Z', now)).toBe('09:15 UTC');
        expect(formatSessionStart('2026-11-25T00:00:00Z', now)).toBe('00:00 UTC');
    });

    it('puts the date first when the start is not today’s', () => {
        expect(formatSessionStart('2026-11-24T23:59:59Z', now)).toBe('2026-11-24 23:59 UTC');
        expect(formatSessionStart('2026-11-20T09:15:00Z', now)).toBe('2026-11-20 09:15 UTC');
    });

    it('judges "today" in UTC, not in the window’s zone', () => {
        // 01:30 local on the 26th at +02:00 is 23:30 UTC on the 25th: today.
        expect(formatSessionStart('2026-11-26T01:30:00+02:00', now)).toBe('23:30 UTC');
    });

    it('gives nothing for a start it cannot read', () => {
        expect(formatSessionStart('not a time', now)).toBe('');
    });
});
