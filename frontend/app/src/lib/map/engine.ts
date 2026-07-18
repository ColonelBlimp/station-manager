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
export function arcPath(engine: MapEngine, from: LatLon, to: LatLon): string | null {
    const line: LineString = {
        type: 'LineString',
        coordinates: [
            [from.lon, from.lat],
            [to.lon, to.lat],
        ],
    };
    return engine.path(line);
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
