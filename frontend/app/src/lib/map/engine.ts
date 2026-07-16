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

import { geoNaturalEarth1, geoPath, geoInterpolate, geoGraticule10 } from 'd3-geo';
import type { GeoPath, GeoProjection } from 'd3-geo';
import { feature } from 'topojson-client';
import type { FeatureCollection, Geometry, LineString } from 'geojson';
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

/**
 * Countries as GeoJSON features, extracted once from the bundled
 * TopoJSON (the topojson→geojson conversion allocates; callers re-render
 * far more often than the data changes).
 */
export function worldCountries(): FeatureCollection<Geometry> {
    if (countriesCache === null) {
        // world-atlas ships untyped JSON; the shape is fixed by that package.
        const topo = worldTopo as unknown as Parameters<typeof feature>[0];
        countriesCache = feature(
            topo,
            topo.objects.countries
        ) as unknown as FeatureCollection<Geometry>;
    }
    return countriesCache;
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
