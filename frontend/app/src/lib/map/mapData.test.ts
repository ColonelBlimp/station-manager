// Pins the map's data story: ADIF timestamp parsing, the coordinate
// fallback chain (decimal lat/lon → gridsquare → unplotted), and the
// windowed collection's stop conditions (edge / last page / page cap).
// The pager is injected, so no fetch mocking — pure logic under test.

import { describe, it, expect } from 'vitest';
import {
    qsoEpochMs,
    rowPoint,
    collectWindow,
    qsoLabel,
    storedDurationMin,
    type Pager,
} from './mapData.svelte';
import type { LogbookQso, QsoPageOutcome } from '../api/logbooks';

const q = (over: Partial<LogbookQso>): LogbookQso => ({ id: 1, ...over });

describe('storedDurationMin', () => {
    it('restores a persisted picker choice', () => {
        expect(storedDurationMin('15')).toBe(15);
        expect(storedDurationMin('2880')).toBe(2880);
    });

    it('falls back to the 6 h default for absent, garbled, or retired values', () => {
        expect(storedDurationMin(null)).toBe(360);
        expect(storedDurationMin('')).toBe(360);
        expect(storedDurationMin('yesterday')).toBe(360);
        expect(storedDurationMin('14400')).toBe(360); // the dropped 10-day option
    });
});

describe('qsoEpochMs', () => {
    it('parses ADIF date + HHMM as UTC', () => {
        expect(qsoEpochMs('20260716', '1234')).toBe(Date.UTC(2026, 6, 16, 12, 34, 0));
    });

    it('parses HHMMSS seconds (ADR 0041 precision)', () => {
        expect(qsoEpochMs('20260716', '123456')).toBe(Date.UTC(2026, 6, 16, 12, 34, 56));
    });

    it('treats a missing/garbled time as midnight, a garbled date as unparseable', () => {
        expect(qsoEpochMs('20260716', undefined)).toBe(Date.UTC(2026, 6, 16));
        expect(qsoEpochMs('20260716', 'xx')).toBe(Date.UTC(2026, 6, 16));
        expect(qsoEpochMs(undefined, '1234')).toBeNull();
        expect(qsoEpochMs('2026-07-16', '1234')).toBeNull();
    });
});

describe('rowPoint', () => {
    it('prefers the enrichment decimal lat/lon', () => {
        const p = rowPoint(q({ lat: '51.5', lon: '-0.1', gridsquare: 'KH45' }));
        expect(p).toEqual({ lat: 51.5, lon: -0.1 });
    });

    it('falls back to the gridsquare cell centre', () => {
        const p = rowPoint(q({ gridsquare: 'IO91' }));
        expect(p).not.toBeNull();
        expect(p!.lat).toBeGreaterThan(51);
        expect(p!.lat).toBeLessThan(52);
    });

    it('rejects import-era ADIF Location strings and still uses the grid', () => {
        const p = rowPoint(q({ lat: 'N051 30.000', lon: 'W000 06.000', gridsquare: 'IO91' }));
        expect(p).not.toBeNull();
        expect(p!.lon).toBeLessThan(0);
    });

    it('returns null with neither source (unplotted, fail-soft)', () => {
        expect(rowPoint(q({}))).toBeNull();
        expect(rowPoint(q({ gridsquare: 'not-a-grid' }))).toBeNull();
    });
});

describe('collectWindow', () => {
    const at = (date: string, time: string, id: number): LogbookQso =>
        q({ id, qso_date: date, time_on: time });

    const pagerOf = (pages: LogbookQso[][]): Pager => {
        return (after?: string): Promise<QsoPageOutcome> => {
            const i = after === undefined ? 0 : Number(after);
            return Promise.resolve({
                kind: 'ok',
                items: pages[i],
                nextCursor: i + 1 < pages.length ? String(i + 1) : null,
            });
        };
    };

    it('stops at the first row past the window edge', async () => {
        const since = Date.UTC(2026, 6, 16, 10, 0, 0);
        const pager = pagerOf([
            [at('20260716', '1200', 3), at('20260716', '1100', 2)],
            [at('20260716', '0900', 1)], // past the edge — never included
        ]);
        const out = await collectWindow(pager, since);
        expect(out.rows.map((r) => r.id)).toEqual([3, 2]);
        expect(out.capped).toBe(false);
    });

    it('drains to the last page when everything is inside the window', async () => {
        const since = Date.UTC(2026, 6, 1);
        const pager = pagerOf([[at('20260716', '1200', 2)], [at('20260715', '0900', 1)]]);
        const out = await collectWindow(pager, since);
        expect(out.rows).toHaveLength(2);
        expect(out.capped).toBe(false);
    });

    it('skips rows with unparseable timestamps rather than mis-windowing them', async () => {
        const since = Date.UTC(2026, 6, 16);
        const pager = pagerOf([[at('20260716', '1200', 2), q({ id: 99 })]]);
        const out = await collectWindow(pager, since);
        expect(out.rows.map((r) => r.id)).toEqual([2]);
    });

    it('reports capped when the page cap fires before the edge', async () => {
        // 30 identical single-row pages, all inside the window — the cap (25)
        // must fire and say so instead of silently truncating.
        const pages = Array.from({ length: 30 }, (_, i) => [at('20260716', '1200', i + 1)]);
        const out = await collectWindow(pagerOf(pages), Date.UTC(2026, 6, 1));
        expect(out.capped).toBe(true);
        expect(out.rows).toHaveLength(25);
    });

    it('propagates a pager error', async () => {
        const pager: Pager = () => Promise.resolve({ kind: 'error', message: 'boom' });
        await expect(collectWindow(pager, 0)).rejects.toThrow('boom');
    });
});

describe('qsoLabel', () => {
    it('carries call, grid, distance, bearing, band and mode', () => {
        const label = qsoLabel(
            q({ call: 'G4ABC', gridsquare: 'IO91', band: '20m', mode: 'FT8' }),
            { lat: 51.5, lon: -0.5 },
            { lat: -14.0, lon: 33.8 }
        );
        expect(label).toContain('G4ABC');
        expect(label).toContain('IO91');
        expect(label).toMatch(/\d ?km|\d,\d{3} ?km|km/);
        expect(label).toContain('°');
        expect(label).toContain('20m');
        expect(label).toContain('FT8');
    });

    it('omits distance/bearing with no origin', () => {
        const label = qsoLabel(q({ call: 'G4ABC' }), { lat: 51.5, lon: -0.5 }, null);
        expect(label).toBe('G4ABC');
    });
});
