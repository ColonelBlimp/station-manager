import { describe, it, expect } from 'vitest';
import { formatDurationHms } from './time';

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
