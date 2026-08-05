// Pins the spherical math the map stands on: projection orientation,
// great-circle arc properties (poleward bow, endpoint fidelity), and the
// antimeridian split — the classic map bug where a Pacific-crossing arc
// renders as a horizontal smear. Pure geometry, no component render.

import { describe, it, expect } from 'vitest';
import {
    createEngine,
    worldCountries,
    arcPath,
    bowedArc,
    fanBows,
    sampleArc,
    project,
    geometryPath,
    graticulePath,
    nightCap,
    subsolarPoint,
    SPHERE,
    TWILIGHT_RADII,
    type LatLon,
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

/*
    FANNED ARCS — several QSOs with the SAME station (dogfood 2026-08-05).

    Two contacts with one station resolve to the same point, so their arcs
    were byte-identical and painted exactly on top of each other: six QSOs
    drew five visible arcs, and the hidden one was the OLDER band (paint
    order puts the newest on top). The legend counted both; the picture did
    not.

    ACCEPTANCE CRITERIA (operator-observable, before any mechanism):

        M1  When I work the same station twice, the map draws two
            DISTINGUISHABLE arcs — the arcs I can see match the plotted
            count.
        M2  When a destination has only one QSO, its arc is unchanged. I
            can tell "duplicates fan out" apart from "everything is now
            slightly bent".
        M3  A fanned arc still starts at my QTH and ends ON the station —
            I can tell a BOWED arc apart from a MOVED one.

    The rules below pin the geometry; the DOM half (that two same-point
    QSOs actually render two different paths) is in MapView.svelte.test.ts,
    because only there is "what the operator sees" observable.

    NO TOLERANCE IS INVENTED for "the same destination": it is exact
    equality of the resolved point. Arcs to merely NEARBY points already
    draw distinguishably, so there is nothing for a threshold to decide.
*/
describe('bowed arcs (fanning duplicate destinations)', () => {
    const engine = createEngine(W, H);

    it('M2: a zero bow is the plain two-point geodesic, unchanged', () => {
        // Compared against the LITERAL two-point line rather than against
        // arcPath's own default argument. The obvious spelling —
        // arcPath(a,b,0) === arcPath(a,b) — compares the function to itself
        // and passes no matter what it does; its reversion proof caught that.
        //
        // Byte-identical matters here: every single-QSO arc goes through this,
        // and d3's ADAPTIVE sampling of a two-point line draws a better great
        // circle than the fixed step count bowedArc has to use.
        const straight = engine.path({
            type: 'LineString',
            coordinates: [
                [LILONGWE.lon, LILONGWE.lat],
                [LONDON.lon, LONDON.lat],
            ],
        });
        expect(arcPath(engine, LILONGWE, LONDON, 0)).toBe(straight);
        expect(arcPath(engine, LILONGWE, LONDON)).toBe(straight);
    });

    it('M1: a non-zero bow draws a different path', () => {
        const straight = arcPath(engine, LILONGWE, LONDON);
        const bowed = arcPath(engine, LILONGWE, LONDON, 0.05);
        expect(bowed).not.toBeNull();
        expect(bowed).not.toBe(straight);
    });

    it('M1: opposite bows go to opposite sides of the great circle', () => {
        // Same magnitude, mirrored — otherwise a "fan" could put both arcs on
        // the same side, which separates them from the straight path but not
        // from EACH OTHER, and that is the bug.
        const mid = (bow: number): LatLon => bowedArc(LILONGWE, LONDON, bow, 8)[4];
        const left = mid(0.05);
        const right = mid(-0.05);
        const centre = mid(0);
        expect(left.lat).not.toBeCloseTo(right.lat, 3);
        // The straight arc's midpoint sits BETWEEN them.
        expect(Math.sign(left.lat - centre.lat)).toBe(-Math.sign(right.lat - centre.lat));
    });

    it('M3: the endpoints are exact for any bow', () => {
        for (const bow of [-0.2, -0.05, 0, 0.05, 0.2]) {
            const pts = bowedArc(LILONGWE, TOKYO, bow, 16);
            expect(pts[0].lat).toBeCloseTo(LILONGWE.lat, 9);
            expect(pts[0].lon).toBeCloseTo(LILONGWE.lon, 9);
            expect(pts.at(-1)!.lat).toBeCloseTo(TOKYO.lat, 9);
            expect(pts.at(-1)!.lon).toBeCloseTo(TOKYO.lon, 9);
        }
    });

    it('M3: a degenerate pair (same point both ends) does not explode', () => {
        // No great-circle plane exists, so there is no direction to bow in.
        const pts = bowedArc(LONDON, LONDON, 0.05, 8);
        expect(pts.every((p) => Number.isFinite(p.lat) && Number.isFinite(p.lon))).toBe(true);
        expect(arcPath(engine, LONDON, LONDON, 0.05)).not.toBeUndefined();
    });
});

describe('fanBows', () => {
    it('leaves distinct destinations straight', () => {
        expect(fanBows(['a', 'b', 'c'])).toEqual([0, 0, 0]);
    });

    it('M1: splits a duplicated destination symmetrically about the true path', () => {
        const [x, y] = fanBows(['a', 'a']);
        expect(x).not.toBe(0);
        expect(x).toBe(-y); // mirrored, so the straight path stays the axis
    });

    it('keeps the middle arc straight when three share a destination', () => {
        const [x, y, z] = fanBows(['a', 'a', 'a']);
        expect(y).toBe(0);
        expect(x).toBe(-z);
        expect(Math.abs(x)).toBeGreaterThan(0);
    });

    it('fans each destination independently, in render order', () => {
        // Interleaved on purpose: a per-destination counter that walked the
        // list assuming duplicates were adjacent would pass a grouped fixture.
        expect(fanBows(['a', 'b', 'a'])).toEqual([
            ...fanBows(['a', 'a']).slice(0, 1),
            0,
            fanBows(['a', 'a'])[1],
        ]);
    });
});
