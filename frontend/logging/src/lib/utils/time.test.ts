import { describe, it, expect } from 'vitest';
import { formatUtcDate, formatUtcTime } from './time';

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
