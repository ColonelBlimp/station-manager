// Pins the zoom/pan transform math and the endpoint hit-grouping — the
// pure layer under WorldMap's wheel/drag/tooltip interaction.

import { describe, it, expect } from 'vitest';
import {
    IDENTITY,
    MIN_ZOOM,
    MAX_ZOOM,
    clampTransform,
    zoomAt,
    panBy,
    toContent,
    endpointsNear,
} from './zoom';

const W = 960;
const H = 500;

describe('zoomAt', () => {
    it('keeps the point under the cursor fixed while scaling', () => {
        const t = zoomAt(IDENTITY, 300, 200, 2, W, H);
        expect(t.k).toBe(2);
        // The content point that was at viewBox (300,200) is still there.
        expect(toContent(t, 300, 200)).toEqual([300, 200]);
    });

    it('composes: two zooms about the same point still pin that point', () => {
        const t1 = zoomAt(IDENTITY, 640, 100, 2, W, H);
        const t2 = zoomAt(t1, 640, 100, 3, W, H);
        expect(t2.k).toBe(6);
        const [cx, cy] = toContent(t2, 640, 100);
        expect(cx).toBeCloseTo(640, 6);
        expect(cy).toBeCloseTo(100, 6);
    });

    it('clamps to MAX_ZOOM and MIN_ZOOM', () => {
        const max = zoomAt(IDENTITY, 0, 0, 1e6, W, H);
        expect(max.k).toBe(MAX_ZOOM);
        const min = zoomAt(max, 0, 0, 1e-6, W, H);
        expect(min).toEqual(IDENTITY);
        expect(min.k).toBe(MIN_ZOOM);
    });

    it('returns the same transform when already at the bound (no-op)', () => {
        const max = zoomAt(IDENTITY, 100, 100, 1e6, W, H);
        expect(zoomAt(max, 500, 300, 4, W, H)).toBe(max);
    });

    it('zooming fully out snaps exactly to IDENTITY from any pan', () => {
        const zoomed = panBy(zoomAt(IDENTITY, 480, 250, 4, W, H), -50, -30, W, H);
        expect(zoomAt(zoomed, 111, 222, 1 / 16, W, H)).toEqual(IDENTITY);
    });
});

describe('clampTransform / panBy', () => {
    it('never lets the content edge leave the viewBox', () => {
        const t = zoomAt(IDENTITY, 0, 0, 2, W, H); // top-left pinned: x = y = 0
        expect(panBy(t, 500, 500, W, H)).toEqual({ k: 2, x: 0, y: 0 });
        const far = panBy(t, -1e6, -1e6, W, H);
        expect(far).toEqual({ k: 2, x: W * (1 - 2), y: H * (1 - 2) });
    });

    it('pans freely inside the bounds', () => {
        const t = zoomAt(IDENTITY, 480, 250, 4, W, H);
        const moved = panBy(t, -10, 7, W, H);
        expect(moved.x).toBeCloseTo(t.x - 10);
        expect(moved.y).toBeCloseTo(t.y + 7);
        expect(moved.k).toBe(4);
    });

    it('is inert at k = 1 (nothing to pan)', () => {
        expect(panBy(IDENTITY, 100, 100, W, H)).toBe(IDENTITY);
        expect(clampTransform({ k: 1, x: 40, y: -9 }, W, H)).toEqual(IDENTITY);
    });
});

describe('endpointsNear', () => {
    const ends = [
        { x: 100, y: 100 }, // 0: stacked pair …
        { x: 101, y: 100 }, // 1: … with 0
        { x: 300, y: 300 }, // 2: alone
    ];

    it('returns the whole stack at the nearest endpoint', () => {
        expect(endpointsNear(ends, 99, 99, 10, 5)).toEqual([0, 1]);
    });

    it('returns only the isolated endpoint when nearest to it', () => {
        expect(endpointsNear(ends, 302, 301, 10, 5)).toEqual([2]);
    });

    it('returns empty beyond the hit radius or with no endpoints', () => {
        expect(endpointsNear(ends, 500, 90, 10, 5)).toEqual([]);
        expect(endpointsNear([], 100, 100, 10, 5)).toEqual([]);
    });

    it('nearest wins when two groups are in hit range', () => {
        // Equidistant-ish probe closer to the singleton.
        expect(endpointsNear(ends, 290, 290, 200, 5)).toEqual([2]);
    });
});
