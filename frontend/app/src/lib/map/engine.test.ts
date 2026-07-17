// Pins the spherical math the map stands on: projection orientation,
// great-circle arc properties (poleward bow, endpoint fidelity), and the
// antimeridian split — the classic map bug where a Pacific-crossing arc
// renders as a horizontal smear. Pure geometry, no component render.

import { describe, it, expect } from 'vitest';
import {
    createEngine,
    worldCountries,
    arcPath,
    sampleArc,
    project,
    geometryPath,
    graticulePath,
    nightCap,
    subsolarPoint,
    SPHERE,
    TWILIGHT_RADII,
} from './engine';

const W = 960;
const H = 500;

// Fixed reference points (decimal degrees).
const LONDON = { lat: 51.5, lon: -0.1 };
const NEW_YORK = { lat: 40.7, lon: -74.0 };
const TOKYO = { lat: 35.7, lon: 139.7 };
const SAN_FRANCISCO = { lat: 37.8, lon: -122.4 };
const LILONGWE = { lat: -14.0, lon: 33.8 }; // 7Q — the operator's QTH region

describe('createEngine / project', () => {
    const engine = createEngine(W, H);

    it('fits the sphere inside the pixel box', () => {
        const [[x0, y0], [x1, y1]] = engine.path.bounds(SPHERE);
        expect(x0).toBeGreaterThanOrEqual(-1);
        expect(y0).toBeGreaterThanOrEqual(-1);
        expect(x1).toBeLessThanOrEqual(W + 1);
        expect(y1).toBeLessThanOrEqual(H + 1);
    });

    it('projects (0,0) to the viewport centre', () => {
        const p = project(engine, { lat: 0, lon: 0 });
        expect(p).not.toBeNull();
        const [x, y] = p!;
        expect(x).toBeCloseTo(W / 2, 0);
        expect(y).toBeCloseTo(H / 2, 0);
    });

    it('orients the axes: east is right, north is up', () => {
        const [cx, cy] = project(engine, { lat: 0, lon: 0 })!;
        const [ex] = project(engine, { lat: 0, lon: 90 })!;
        const [, ny] = project(engine, { lat: 45, lon: 0 })!;
        expect(ex).toBeGreaterThan(cx);
        expect(ny).toBeLessThan(cy); // SVG y grows downward
    });
});

describe('sampleArc', () => {
    it('returns steps+1 points with exact endpoints', () => {
        const pts = sampleArc(LILONGWE, LONDON, 32);
        expect(pts).toHaveLength(33);
        expect(pts[0].lat).toBeCloseTo(LILONGWE.lat, 6);
        expect(pts[0].lon).toBeCloseTo(LILONGWE.lon, 6);
        expect(pts[32].lat).toBeCloseTo(LONDON.lat, 6);
        expect(pts[32].lon).toBeCloseTo(LONDON.lon, 6);
    });

    it('bows poleward: the London→New York midpoint lies north of both endpoints', () => {
        // The defining great-circle property a naive lat/lon lerp gets wrong.
        const pts = sampleArc(LONDON, NEW_YORK, 2);
        expect(pts[1].lat).toBeGreaterThan(LONDON.lat);
        expect(pts[1].lat).toBeGreaterThan(NEW_YORK.lat);
    });

    it('crosses the antimeridian on a Tokyo→San Francisco arc', () => {
        const pts = sampleArc(TOKYO, SAN_FRANCISCO, 64);
        // The short path heads east across the Pacific: longitudes run
        // toward +180 then flip sign — never the long way west via Europe.
        const nearFlip = pts.some((p) => Math.abs(p.lon) > 170);
        expect(nearFlip).toBe(true);
    });
});

describe('arcPath', () => {
    const engine = createEngine(W, H);

    it('renders a same-hemisphere arc as one continuous subpath', () => {
        const d = arcPath(engine, LILONGWE, LONDON);
        expect(d).not.toBeNull();
        expect(d!.match(/M/g)).toHaveLength(1);
    });

    it('splits a Pacific-crossing arc at the antimeridian instead of smearing', () => {
        const d = arcPath(engine, TOKYO, SAN_FRANCISCO);
        expect(d).not.toBeNull();
        // Two move commands = two clipped segments, one per side of ±180°.
        expect(d!.match(/M/g)!.length).toBeGreaterThanOrEqual(2);
        // And no segment spans the map: a smear would step nearly the full
        // projected width between consecutive points in one subpath.
        for (const sub of d!.split('M').filter((s) => s.length > 0)) {
            const xs = sub
                .split(/[LZ,]/)
                .map((n) => parseFloat(n))
                .filter((n, i) => Number.isFinite(n) && i % 2 === 0);
            for (let i = 1; i < xs.length; i++) {
                expect(Math.abs(xs[i] - xs[i - 1])).toBeLessThan(W / 2);
            }
        }
    });
});

describe('worldCountries / geometryPath / graticulePath', () => {
    const engine = createEngine(W, H);

    it('extracts the Natural Earth country set', () => {
        const fc = worldCountries();
        expect(fc.type).toBe('FeatureCollection');
        expect(fc.features.length).toBeGreaterThan(150);
        expect(fc.features.every((f) => f.geometry !== null)).toBe(true);
    });

    it('returns the same (cached) collection on repeat calls', () => {
        expect(worldCountries()).toBe(worldCountries());
    });

    it('projects every country to a drawable path', () => {
        const fc = worldCountries();
        const drawable = fc.features.filter((f) => geometryPath(engine, f) !== null);
        expect(drawable.length).toBe(fc.features.length);
    });

    it('renders a graticule', () => {
        const d = graticulePath(engine);
        expect(d).not.toBeNull();
        expect(d!.length).toBeGreaterThan(100);
    });
});

describe('subsolarPoint / nightCap (grey line)', () => {
    it('matches the almanac at the J2000 epoch', () => {
        // 2000-01-01 12:00 UTC: declination −23.04°, and the ~−3 min
        // equation of time puts the sun just east of Greenwich at noon.
        const p = subsolarPoint(new Date(Date.UTC(2000, 0, 1, 12)));
        expect(Math.abs(p.lat - -23.04)).toBeLessThan(0.1);
        expect(Math.abs(p.lon - 0.8)).toBeLessThan(0.5);
    });

    it('tracks declination through equinox and solstices', () => {
        // Equinox instant (2026-03-20 14:46 UTC): sun over the equator.
        const equinox = subsolarPoint(new Date(Date.UTC(2026, 2, 20, 14, 46)));
        expect(Math.abs(equinox.lat)).toBeLessThan(0.5);
        // Solstices pin the extremes at ±obliquity (23.44°).
        const june = subsolarPoint(new Date(Date.UTC(2026, 5, 21, 12)));
        const december = subsolarPoint(new Date(Date.UTC(2026, 11, 21, 12)));
        expect(Math.abs(june.lat - 23.44)).toBeLessThan(0.1);
        expect(Math.abs(december.lat - -23.44)).toBeLessThan(0.1);
    });

    it('puts noon near Greenwich and midnight near the antimeridian', () => {
        expect(Math.abs(subsolarPoint(new Date(Date.UTC(2026, 6, 17, 12))).lon)).toBeLessThan(5);
        expect(Math.abs(subsolarPoint(new Date(Date.UTC(2026, 6, 17, 0))).lon)).toBeGreaterThan(
            175
        );
    });

    it('projects a drawable cap at every twilight radius', () => {
        const engine = createEngine(W, H);
        const at = new Date(Date.UTC(2026, 6, 17, 12));
        for (const r of TWILIGHT_RADII) {
            const d = geometryPath(engine, nightCap(at, r));
            expect(d).not.toBeNull();
            expect(d!.length).toBeGreaterThan(50);
        }
    });

    it('centres the night hemisphere opposite the sun', () => {
        // Equinox: the terminator runs pole to pole, so the 90° cap's ring
        // must reach both polar regions — the classic sanity check.
        const ring = nightCap(new Date(Date.UTC(2026, 2, 20, 14, 46)), 90).coordinates[0];
        const lats = ring.map((c) => c[1]);
        expect(Math.max(...lats)).toBeGreaterThan(85);
        expect(Math.min(...lats)).toBeLessThan(-85);
    });
});
