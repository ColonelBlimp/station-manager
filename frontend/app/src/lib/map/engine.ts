/**
 * Great-circle map render engine — pure geometry, no DOM.
 *
 * The seam every map component draws through. d3-geo owns the fiddly,
 * well-solved spherical math — projection, adaptive great-circle path
 * sampling, antimeridian clipping — per the backlog render decision
 * ("QSO contacts map"): hand-rolling those is exactly the work a focused
 * MIT lib should carry. The basemap is Natural Earth 110m TopoJSON from
 * the `world-atlas` package (public domain → GPL-clean, no ODbL
 * share-alike), `import`ed so Vite bundles it into the binary via
 * `//go:embed` — never fetched, same offline-first posture as the
 * flag-icons. AVOID swapping in OSM-derived data.
 *
 * Coordinate convention: this module speaks `{ lat, lon }` decimal degrees
 * (matching `utils/bearing.ts` `DecimalCoords`); d3's internal
 * `[lon, lat]` ordering stays inside this file.
 */

import { geoNaturalEarth1, geoPath, geoInterpolate, geoGraticule10, geoCircle } from 'd3-geo';
import type { GeoPath, GeoProjection } from 'd3-geo';
import { feature } from 'topojson-client';
import type { FeatureCollection, Geometry, LineString, Polygon } from 'geojson';
import worldTopo from 'world-atlas/countries-110m.json';

export interface LatLon {
    lat: number;
    lon: number;
}

/** A ready-to-draw projection + SVG path generator fitted to a pixel box. */
export interface MapEngine {
    projection: GeoProjection;
    path: GeoPath;
    width: number;
    height: number;
}

/** The full-globe outline — drawn first as the ocean backdrop. */
export const SPHERE = { type: 'Sphere' } as const;

/**
 * Build an engine fitted to `width × height`. Natural Earth is the
 * projection the basemap data was designed around: equal-area-ish,
 * gentle on high latitudes, no interrupted lobes for arcs to fall into.
 */
export function createEngine(width: number, height: number): MapEngine {
    const projection = geoNaturalEarth1().fitSize([width, height], SPHERE);
    return { projection, path: geoPath(projection), width, height };
}

let countriesCache: FeatureCollection<Geometry> | null = null;

function topoToCountries(raw: unknown): FeatureCollection<Geometry> {
    // world-atlas ships untyped JSON; the shape is fixed by that package.
    const topo = raw as Parameters<typeof feature>[0];
    return feature(topo, topo.objects.countries) as unknown as FeatureCollection<Geometry>;
}

/**
 * Countries as GeoJSON features, extracted once from the bundled
 * TopoJSON (the topojson→geojson conversion allocates; callers re-render
 * far more often than the data changes).
 */
export function worldCountries(): FeatureCollection<Geometry> {
    if (countriesCache === null) {
        countriesCache = topoToCountries(worldTopo);
    }
    return countriesCache;
}

let countriesHiCache: FeatureCollection<Geometry> | null = null;

/**
 * The 50m (1:50M) countries — the zoomed-in level of detail. 110m coasts
 * are visibly blocky past ~3× and drop small islands outright; 50m fixes
 * both for ~750 KB. The dynamic import keeps that chunk BUNDLED (Vite
 * code-splits it into the binary — offline posture unchanged, nothing
 * fetched from the network) but out of the initial page load: the browser
 * only parses it the first time an operator actually zooms in.
 */
export async function worldCountriesHi(): Promise<FeatureCollection<Geometry>> {
    if (countriesHiCache === null) {
        const mod = await import('world-atlas/countries-50m.json');
        countriesHiCache = topoToCountries(mod.default);
    }
    return countriesHiCache;
}

/**
 * SVG path `d` for one country (or any GeoJSON object) under the
 * engine's projection. Null when the geometry projects to nothing.
 */
export function geometryPath(engine: MapEngine, geom: object): string | null {
    return engine.path(geom as Parameters<GeoPath>[0]);
}

/**
 * SVG path `d` for the short great-circle arc between two points.
 * A two-point LineString is enough: geoPath samples adaptively along
 * the great circle and clips at the antimeridian (a Pacific-crossing
 * arc renders as two segments, not a horizontal smear across the map).
 */
export function arcPath(engine: MapEngine, from: LatLon, to: LatLon, bow = 0): string | null {
    // bow 0 keeps the ORIGINAL two-point line, not a sampled approximation of
    // it: every single-QSO arc goes through here, and d3's adaptive sampling
    // draws a better great circle than any fixed step count we would pick.
    const line: LineString = {
        type: 'LineString',
        coordinates:
            bow === 0
                ? [
                      [from.lon, from.lat],
                      [to.lon, to.lat],
                  ]
                : bowedArc(from, to, bow).map((p) => [p.lon, p.lat]),
    };
    return engine.path(line);
}

/**
 * Spread between adjacent arcs that share a destination, as a fraction of the
 * arc's OWN angular length.
 *
 * Proportional rather than absolute so the fan reads the same on a 500 km hop
 * and a 15 000 km path — a fixed angular offset is invisible on one and absurd
 * on the other. EYEBALL THIS: it is a visual judgement, not a derived value.
 */
export const FAN_SPREAD = 0.05;

/**
 * Sample a great circle from `from` to `to`, bowed to one side.
 *
 * Exists because several QSOs with the same station resolve to the same point,
 * so their arcs were identical and painted exactly on top of one another — the
 * map drew five arcs for six contacts and the hidden one was the older band.
 *
 * The displacement is perpendicular to the great-circle PLANE and scaled by
 * sin(πt), so it vanishes at both ends: the arc bows, it never moves. A
 * positive bow goes to one side of the true path and a negative bow to the
 * other, which is what lets a pair straddle it rather than both leaning the
 * same way (the latter separates each from the true path but not from each
 * other — the bug restated).
 *
 * `steps` is fixed rather than adaptive: these paths are always short-lived
 * render output, and the alternative — asking d3 to adaptively resample a
 * curve that is no longer a geodesic — is not something d3-geo offers.
 */
export function bowedArc(from: LatLon, to: LatLon, bow: number, steps = 48): LatLon[] {
    const a = toVec(from);
    const b = toVec(to);
    // Normal to the plane the two points define. Its LENGTH is sin(angle), so
    // it goes to zero for identical or antipodal endpoints — there is then no
    // unique great circle and so no side to bow toward. Fall through straight.
    const n: Vec3 = [
        a[1] * b[2] - a[2] * b[1],
        a[2] * b[0] - a[0] * b[2],
        a[0] * b[1] - a[1] * b[0],
    ];
    const nLen = Math.hypot(n[0], n[1], n[2]);
    const interp = geoInterpolate([from.lon, from.lat], [to.lon, to.lat]);
    const sample = (t: number): LatLon => {
        const [lon, lat] = interp(t);
        return { lat, lon };
    };
    if (bow === 0 || nLen === 0) {
        return Array.from({ length: steps + 1 }, (_, i) => sample(i / steps));
    }
    // atan2(sin, cos) rather than acos: stable for the near-antipodal arcs a
    // 7Q station actually works.
    const angle = Math.atan2(nLen, a[0] * b[0] + a[1] * b[1] + a[2] * b[2]);
    const amplitude = bow * angle;
    const un: Vec3 = [n[0] / nLen, n[1] / nLen, n[2] / nLen];

    const out: LatLon[] = [];
    for (let i = 0; i <= steps; i++) {
        // Endpoints are copied, not computed: "ends ON the station" is the
        // difference between a bowed arc and a moved one, and it should not
        // rest on floating-point luck at t=0 and t=1.
        if (i === 0) {
            out.push({ ...from });
            continue;
        }
        if (i === steps) {
            out.push({ ...to });
            continue;
        }
        const t = i / steps;
        const p = toVec(sample(t));
        const w = amplitude * Math.sin(Math.PI * t);
        const v: Vec3 = [p[0] + un[0] * w, p[1] + un[1] * w, p[2] + un[2] * w];
        const len = Math.hypot(v[0], v[1], v[2]);
        out.push(toLatLon([v[0] / len, v[1] / len, v[2] / len]));
    }
    return out;
}

/**
 * Bow for each arc, given its destination key in RENDER ORDER.
 *
 * A destination with one contact gets 0 — the overwhelmingly common case must
 * draw exactly what it drew before. Duplicates are spread symmetrically about
 * the true path, so with three the middle one is still the geodesic.
 *
 * "Same destination" is EXACT equality of the key the caller builds from the
 * resolved point. No proximity tolerance: arcs to merely nearby points are
 * already distinguishable, so there is nothing for a threshold to decide, and
 * inventing one would start bending arcs that were never superimposed.
 */
export function fanBows(destKeys: string[]): number[] {
    const total = new Map<string, number>();
    for (const k of destKeys) total.set(k, (total.get(k) ?? 0) + 1);
    // Counted per key rather than by walking runs: the render order is by TIME,
    // so two contacts with one station are usually NOT adjacent.
    const seen = new Map<string, number>();
    return destKeys.map((k) => {
        const n = total.get(k) ?? 1;
        if (n === 1) return 0;
        const i = seen.get(k) ?? 0;
        seen.set(k, i + 1);
        return FAN_SPREAD * (i - (n - 1) / 2);
    });
}

type Vec3 = [number, number, number];

function toVec(p: LatLon): Vec3 {
    const la = p.lat * RAD;
    const lo = p.lon * RAD;
    const c = Math.cos(la);
    return [c * Math.cos(lo), c * Math.sin(lo), Math.sin(la)];
}

function toLatLon(v: Vec3): LatLon {
    return { lat: Math.asin(v[2]) / RAD, lon: Math.atan2(v[1], v[0]) / RAD };
}

/**
 * Project a point to pixel coordinates (markers, tooltips). Null when
 * the projection rejects the point.
 */
export function project(engine: MapEngine, p: LatLon): [number, number] | null {
    return engine.projection([p.lon, p.lat]) ?? null;
}

/**
 * Sample `steps + 1` points along the short great-circle arc, endpoints
 * included. Not used for drawing (arcPath's adaptive sampling is better)
 * — this is the seam for arc-length-proportional work: animated draws,
 * midpoint labels, and the unit tests that pin the spherical math.
 */
export function sampleArc(from: LatLon, to: LatLon, steps = 64): LatLon[] {
    const interpolate = geoInterpolate([from.lon, from.lat], [to.lon, to.lat]);
    const points: LatLon[] = [];
    for (let i = 0; i <= steps; i++) {
        const [lon, lat] = interpolate(i / steps);
        points.push({ lat, lon });
    }
    return points;
}

/** SVG path `d` for a 10° graticule — the subtle lat/lon grid. */
export function graticulePath(engine: MapEngine): string | null {
    return engine.path(geoGraticule10());
}

const RAD = Math.PI / 180;
const DEG = 180 / Math.PI;

/** Normalise degrees of longitude to [-180, 180). */
function wrapLon(deg: number): number {
    return ((((deg + 180) % 360) + 360) % 360) - 180;
}

/**
 * Subsolar point (sun at the zenith) at `at` — the anchor for the
 * grey-line overlay. Standard low-precision solar ephemeris (the NOAA /
 * Astronomical Almanac truncation): mean longitude + equation-of-centre
 * → ecliptic longitude → declination (the latitude) and right ascension,
 * minus Greenwich mean sidereal time (the longitude). Good to ~0.01°;
 * the terminator itself is a refraction-fuzzed band half a degree wide,
 * so higher-order terms are noise here.
 */
export function subsolarPoint(at: Date): LatLon {
    // Fractional days since the J2000.0 epoch (2000-01-01 12:00 UTC).
    const n = at.getTime() / 86_400_000 - 10_957.5;
    const meanLon = 280.46 + 0.9856474 * n;
    const meanAnom = (357.528 + 0.9856003 * n) * RAD;
    const eclipticLon =
        (meanLon + 1.915 * Math.sin(meanAnom) + 0.02 * Math.sin(2 * meanAnom)) * RAD;
    const obliquity = (23.439 - 0.0000004 * n) * RAD;
    const declination = Math.asin(Math.sin(obliquity) * Math.sin(eclipticLon)) * DEG;
    const rightAscension =
        Math.atan2(Math.cos(obliquity) * Math.sin(eclipticLon), Math.cos(eclipticLon)) * DEG;
    const gmst = 280.46061837 + 360.98564736629 * n;
    return { lat: declination, lon: wrapLon(rightAscension - gmst) };
}

/**
 * Twilight shading rings, outermost first: sun below the horizon (0°),
 * below civil twilight (−6°), below nautical twilight (−12°). Sun
 * altitude −x° ⇔ 90−x° from the ANTIsolar point, so these are cap radii
 * for nightCap. Stacked translucent fills grade dusk → dark night; the
 * band the first and last ring bound IS the grey line.
 */
export const TWILIGHT_RADII = [90, 84, 78] as const;

/**
 * The spherical cap of radius `radius` degrees around the antisolar
 * point, as a GeoJSON polygon ready for geometryPath. d3's geoCircle
 * handles the polar/antimeridian wrapping that makes hand-rolled
 * terminator polygons notoriously fiddly.
 */
export function nightCap(at: Date, radius: number): Polygon {
    const sun = subsolarPoint(at);
    return geoCircle()
        .center([wrapLon(sun.lon + 180), -sun.lat])
        .radius(radius)();
}
