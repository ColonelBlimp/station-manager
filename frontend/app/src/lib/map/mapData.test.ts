// Pins the map's data story: ADIF timestamp parsing, the coordinate
// fallback chain (decimal lat/lon → gridsquare → unplotted), and the
// windowed collection's stop conditions (edge / last page / page cap).
// The pager is injected, so no fetch mocking — pure logic under test.

import { describe, it, expect, vi } from 'vitest';
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

/**
 * rowPoint — coordinate precedence.
 *
 * ACCEPTANCE CRITERION (operator-approved 2026-07-30):
 *
 *   When a station's coordinates contradict its own Maidenhead grid, the map
 *   draws the arc to the GRID, and I can tell that apart from a station with no
 *   grid at all — which stays unplotted rather than drawn somewhere plausible.
 *
 * WHY. Two rows of the newest 500 in the dogfood log drew arcs to the South
 * Pole: UA3DPM (grid KO95, lat -89.979167 lon 0.041667) and R9LAU (grid MO27,
 * lat -89.979167 lon -179.958333). Both coordinate pairs are QRZ's own, passed
 * through verbatim by internal/lookup/qrz (it derives nothing), and both
 * contradict the grid QRZ returned alongside them. Storage merges gridsquare and
 * lat/lon independently, so the two can disagree, and this function trusted the
 * coordinates.
 *
 * THE TOLERANCE IS THE GRID CELL ITSELF, so no threshold is invented: a 4-char
 * locator declares a ~2°x1° cell and a 6-char one ~5'x2.5', and coordinates
 * legitimately sit anywhere inside. "Outside the cell" is therefore a property of
 * the data, not a number someone chose. Operator's call, 2026-07-30, along with
 * map-layer-only (stored values feed ADIF export and QRZ/ClubLog uploads, so
 * they are left alone) and silent fallback.
 *
 * The rejection is NOT a denylist of bad coordinates. R9LAU's pair decodes to
 * AA00aa's centre and UA3DPM's to JA00aa's — a rule naming the first would have
 * needed widening for the second.
 */
describe('rowPoint', () => {
    it('prefers the enrichment decimal lat/lon when it agrees with the grid', () => {
        // Grid IO91 CONTAINS 51.5/-0.1. The earlier version of this test paired
        // London coordinates with KH45 (Malawi) to prove precedence, which pinned
        // the defect above as intended behaviour — a contradictory fixture cannot
        // demonstrate a precedence rule that is only valid when they agree.
        const p = rowPoint(q({ lat: '51.5', lon: '-0.1', gridsquare: 'IO91' }));
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

    // The two real rows that drew to the South Pole. Asserted as "lands inside
    // the declared grid", which is what the operator sees, rather than as an
    // exact centre — the rule is about the cell, not about one coordinate pair.
    it.each([
        { call: 'UA3DPM', lat: '-89.979167', lon: '0.041667', grid: 'KO95', latRange: [55, 56], lonRange: [38, 40] },
        { call: 'R9LAU', lat: '-89.979167', lon: '-179.958333', grid: 'MO27', latRange: [57, 58], lonRange: [64, 66] },
    ])('draws $call to its grid, not to the coordinates that contradict it', (c) => {
        const p = rowPoint(q({ lat: c.lat, lon: c.lon, gridsquare: c.grid }));
        expect(p).not.toBeNull();
        expect(p!.lat).toBeGreaterThan(c.latRange[0]);
        expect(p!.lat).toBeLessThan(c.latRange[1]);
        expect(p!.lon).toBeGreaterThan(c.lonRange[0]);
        expect(p!.lon).toBeLessThan(c.lonRange[1]);
    });

    // THE NEAREST CONFUSABLE STATE, and the reason the rule is "contradicts its
    // grid" and not "looks implausible": with no grid there is nothing to
    // contradict, so the coordinates are all we have and must be used as given.
    // Silently relocating or dropping such a station would be a worse bug than
    // the one being fixed, because nothing would reveal it.
    it('keeps coordinates when there is no grid to contradict them', () => {
        const p = rowPoint(q({ lat: '-89.979167', lon: '0.041667' }));
        expect(p).toEqual({ lat: -89.979167, lon: 0.041667 });
    });

    it('keeps coordinates when the grid is unusable', () => {
        const p = rowPoint(q({ lat: '51.5', lon: '-0.1', gridsquare: 'not-a-grid' }));
        expect(p).toEqual({ lat: 51.5, lon: -0.1 });
    });

    // A 4-char cell is ~2°x1°, so coordinates far from its CENTRE are still
    // inside it and must be kept. Without this an implementation could compare
    // against the centre with a small radius and pass every rule above while
    // throwing away the precision that makes lat/lon worth preferring.
    it('keeps coordinates that are far from a 4-char cell centre but inside it', () => {
        const p = rowPoint(q({ lat: '51.95', lon: '-0.05', gridsquare: 'IO91' }));
        expect(p).toEqual({ lat: 51.95, lon: -0.05 });
    });

    // ...and the cell shrinks with the locator's precision: the same point is
    // inside 4-char IO91 but far outside 6-char IO91wm, so a station declaring
    // subsquare precision gets it applied.
    it('applies the tighter cell of a 6-char locator', () => {
        const inside = rowPoint(q({ gridsquare: 'IO91wm' }));
        expect(inside).not.toBeNull();
        const p = rowPoint(q({ lat: '51.95', lon: '-0.05', gridsquare: 'IO91wm' }));
        expect(p).toEqual(inside);
    });

    // No invented margin means the boundary belongs to the cell. IO91 spans
    // lat [51,52) lon [-2,0), so its south-west corner counts as agreeing.
    it('accepts coordinates exactly on the cell boundary', () => {
        const p = rowPoint(q({ lat: '51', lon: '-2', gridsquare: 'IO91' }));
        expect(p).toEqual({ lat: 51, lon: -2 });
    });

    // The fallback is silent on the map but must leave a trace, and rowPoint runs
    // per row on every windowed refresh — so the dedup is what makes the warning
    // usable rather than a console flood. Both halves matter: once per row, and
    // still once for a DIFFERENT row (a cache keyed too broadly would hide the
    // second station entirely).
    describe('conflict warning', () => {
        it('warns once per conflicting row, and separately for another row', () => {
            const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
            try {
                const a = q({ call: 'UA3DPM', lat: '-89.979167', lon: '0.041667', gridsquare: 'KO95' });
                const b = q({ call: 'R9LAU', lat: '-89.979167', lon: '-179.958333', gridsquare: 'MO27' });
                rowPoint(a);
                rowPoint(a);
                rowPoint(a);
                expect(warn).toHaveBeenCalledTimes(1);
                expect(String(warn.mock.calls[0][0])).toContain('UA3DPM');
                rowPoint(b);
                expect(warn).toHaveBeenCalledTimes(2);
                expect(String(warn.mock.calls[1][0])).toContain('R9LAU');
            } finally {
                warn.mockRestore();
            }
        });

        it('stays quiet when coordinates agree with the grid', () => {
            const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
            try {
                rowPoint(q({ call: 'G0ABC', lat: '51.5', lon: '-0.1', gridsquare: 'IO91' }));
                rowPoint(q({ call: 'G0DEF', gridsquare: 'IO91' }));
                rowPoint(q({ call: 'G0GHI', lat: '-89.979167', lon: '0.041667' }));
                expect(warn).not.toHaveBeenCalled();
            } finally {
                warn.mockRestore();
            }
        });
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
