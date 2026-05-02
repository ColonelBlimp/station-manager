import { describe, it, expect } from 'vitest';
import { formatUtcDate, formatUtcTime, formatDurationHms } from './time';

describe('formatUtcDate', () => {
    it('formats a typical date', () => {
        const d = new Date('2026-05-02T15:30:00Z');
        expect(formatUtcDate(d)).toBe('2026-05-02');
    });

    it('pads single-digit months with a leading zero', () => {
        const d = new Date('2026-03-15T12:00:00Z');
        expect(formatUtcDate(d)).toBe('2026-03-15');
    });

    it('pads single-digit days with a leading zero', () => {
        const d = new Date('2026-12-05T00:00:00Z');
        expect(formatUtcDate(d)).toBe('2026-12-05');
    });

    it('handles January 1 (year boundary)', () => {
        const d = new Date('2026-01-01T00:00:00Z');
        expect(formatUtcDate(d)).toBe('2026-01-01');
    });

    it('handles December 31 (year boundary)', () => {
        const d = new Date('2026-12-31T23:59:59Z');
        expect(formatUtcDate(d)).toBe('2026-12-31');
    });

    it('handles leap-year February 29', () => {
        const d = new Date('2024-02-29T12:00:00Z');
        expect(formatUtcDate(d)).toBe('2024-02-29');
    });

    it('uses UTC components, not local-timezone components', () => {
        // 23:30 UTC on May 2, which is May 3 local time in zones east of UTC.
        // The formatter MUST report May 2 — the input's UTC date.
        const d = new Date('2026-05-02T23:30:00Z');
        expect(formatUtcDate(d)).toBe('2026-05-02');
    });

    it('is consistent across timezones for an instant near a UTC date boundary', () => {
        // 00:30 UTC on May 2 — May 1 local time in zones west of UTC.
        const d = new Date('2026-05-02T00:30:00Z');
        expect(formatUtcDate(d)).toBe('2026-05-02');
    });
});

describe('formatUtcTime', () => {
    it('formats a typical time', () => {
        const d = new Date('2026-05-02T15:30:00Z');
        expect(formatUtcTime(d)).toBe('15:30');
    });

    it('pads single-digit hours with a leading zero', () => {
        const d = new Date('2026-05-02T07:45:00Z');
        expect(formatUtcTime(d)).toBe('07:45');
    });

    it('pads single-digit minutes with a leading zero', () => {
        const d = new Date('2026-05-02T15:05:00Z');
        expect(formatUtcTime(d)).toBe('15:05');
    });

    it('handles midnight (00:00)', () => {
        const d = new Date('2026-05-02T00:00:00Z');
        expect(formatUtcTime(d)).toBe('00:00');
    });

    it('handles noon (12:00)', () => {
        const d = new Date('2026-05-02T12:00:00Z');
        expect(formatUtcTime(d)).toBe('12:00');
    });

    it('handles end-of-day (23:59)', () => {
        const d = new Date('2026-05-02T23:59:00Z');
        expect(formatUtcTime(d)).toBe('23:59');
    });

    it('uses UTC components, not local-timezone components', () => {
        // Midnight UTC — varying local hours depending on TZ. UTC stays 00:00.
        const d = new Date('2026-05-02T00:00:00Z');
        expect(formatUtcTime(d)).toBe('00:00');
    });

    it('drops seconds (HH:MM only, not HH:MM:SS)', () => {
        const d = new Date('2026-05-02T15:30:45Z');
        expect(formatUtcTime(d)).toBe('15:30');
    });
});

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

    it('formats a typical operating-session length', () => {
        // 2h 15m 30s
        const ms = (2 * 3600 + 15 * 60 + 30) * 1000;
        expect(formatDurationHms(ms)).toBe('02:15:30');
    });

    it('handles 24-hour boundary cleanly (does not wrap)', () => {
        expect(formatDurationHms(24 * 3600 * 1000)).toBe('24:00:00');
    });

    it('grows the hours field past 99 for very long sessions (Field Day)', () => {
        // 100h 0m 0s
        expect(formatDurationHms(100 * 3600 * 1000)).toBe('100:00:00');
    });

    it('floors fractional milliseconds to whole seconds', () => {
        // 1.999s → 1s, not rounded up
        expect(formatDurationHms(1_999)).toBe('00:00:01');
    });

    it('clamps negative inputs to 00:00:00', () => {
        expect(formatDurationHms(-1)).toBe('00:00:00');
        expect(formatDurationHms(-60_000)).toBe('00:00:00');
    });

    it('zero-pads single-digit hours, minutes, and seconds', () => {
        // 5h 7m 9s
        const ms = (5 * 3600 + 7 * 60 + 9) * 1000;
        expect(formatDurationHms(ms)).toBe('05:07:09');
    });
});
