/**
 * Bearing and distance math for the country panel's short/long path
 * display. Ported from v1's `internal/maidenhead/bearing.go` (which
 * was battle-tested across many real QSOs); the algorithm and rounding
 * conventions are preserved verbatim. Differences from v1:
 *
 *   - 4 / 6 / 8-char grid squares are accepted (v1 was 6-char only).
 *   - Returns `null` for invalid input rather than throwing.
 *   - `pathInfo(...)` is the single bundled entry point; per-axis
 *     helpers (bearing-only, distance-only) are not exported until a
 *     consumer needs them — keeping the API tight.
 */
import { isValidMaidenhead } from '../validators/maidenhead';

const EARTH_RADIUS_KM = 6371.0;
const KM_TO_MILES = 0.621371;

const FIELD_WIDTH = 20.0;
const FIELD_HEIGHT = 10.0;
const SQUARE_WIDTH = 2.0;
const SQUARE_HEIGHT = 1.0;
const SUBSQUARE_WIDTH = 5.0 / 60.0;
const SUBSQUARE_HEIGHT = 2.5 / 60.0;
const EXTENDED_WIDTH = 5.0 / 600.0;
const EXTENDED_HEIGHT = 2.5 / 600.0;

const toRadians = (deg: number): number => (deg * Math.PI) / 180;
const toDegrees = (rad: number): number => (rad * 180) / Math.PI;

export interface DecimalCoords {
    /** Latitude in decimal degrees, centre of the locator's cell. */
    lat: number;
    /** Longitude in decimal degrees, centre of the locator's cell. */
    lon: number;
}

export interface PathInfo {
    /** Short-path bearing in degrees, 0-360, rounded to 0.1°. */
    shortPathBearing: number;
    /** Long-path bearing — short + 180 mod 360, rounded to 0.1°. */
    longPathBearing: number;
    /** Short-path great-circle distance, km (Math.ceil rounded). */
    shortPathDistanceKm: number;
    /** Short-path distance, miles (Math.ceil rounded). */
    shortPathDistanceMiles: number;
    /** Long-path distance, km — Earth circumference minus short. */
    longPathDistanceKm: number;
    /** Long-path distance, miles. */
    longPathDistanceMiles: number;
}

/**
 * Convert a Maidenhead locator (4 / 6 / 8 chars) to the decimal
 * lat/lon at the cell's centre. Returns `null` for empty or invalid
 * input.
 */
export function gridToDecimal(grid: string): DecimalCoords | null {
    const trimmed = grid.trim().toUpperCase();
    if (trimmed === '' || isValidMaidenhead(trimmed) !== null) {
        return null;
    }

    let lon = -180.0 + FIELD_WIDTH * (trimmed.charCodeAt(0) - 'A'.charCodeAt(0));
    let lat = -90.0 + FIELD_HEIGHT * (trimmed.charCodeAt(1) - 'A'.charCodeAt(0));
    lon += SQUARE_WIDTH * (trimmed.charCodeAt(2) - '0'.charCodeAt(0));
    lat += SQUARE_HEIGHT * (trimmed.charCodeAt(3) - '0'.charCodeAt(0));

    let cellLon = SQUARE_WIDTH;
    let cellLat = SQUARE_HEIGHT;

    if (trimmed.length >= 6) {
        lon += SUBSQUARE_WIDTH * (trimmed.charCodeAt(4) - 'A'.charCodeAt(0));
        lat += SUBSQUARE_HEIGHT * (trimmed.charCodeAt(5) - 'A'.charCodeAt(0));
        cellLon = SUBSQUARE_WIDTH;
        cellLat = SUBSQUARE_HEIGHT;
    }
    if (trimmed.length === 8) {
        lon += EXTENDED_WIDTH * (trimmed.charCodeAt(6) - '0'.charCodeAt(0));
        lat += EXTENDED_HEIGHT * (trimmed.charCodeAt(7) - '0'.charCodeAt(0));
        cellLon = EXTENDED_WIDTH;
        cellLat = EXTENDED_HEIGHT;
    }

    lon += cellLon / 2.0;
    lat += cellLat / 2.0;

    return { lat, lon };
}

/**
 * Initial great-circle bearing from (lat1, lon1) to (lat2, lon2),
 * normalised to [0, 360) and rounded to 0.1°.
 */
export function calculateBearing(lat1: number, lon1: number, lat2: number, lon2: number): number {
    const lat1Rad = toRadians(lat1);
    const lon1Rad = toRadians(lon1);
    const lat2Rad = toRadians(lat2);
    const lon2Rad = toRadians(lon2);

    const dLon = lon2Rad - lon1Rad;
    const y = Math.sin(dLon) * Math.cos(lat2Rad);
    const x =
        Math.cos(lat1Rad) * Math.sin(lat2Rad) -
        Math.sin(lat1Rad) * Math.cos(lat2Rad) * Math.cos(dLon);
    let bearing = toDegrees(Math.atan2(y, x));
    if (bearing < 0) bearing += 360;
    return Math.round(bearing * 10) / 10;
}

/**
 * Great-circle distance in km between two decimal-degree points,
 * using the haversine formula and Earth radius 6371 km. The result is
 * Math.ceil-rounded to match v1's behaviour.
 */
export function haversineKm(lat1: number, lon1: number, lat2: number, lon2: number): number {
    const lat1Rad = toRadians(lat1);
    const lat2Rad = toRadians(lat2);
    const dLat = toRadians(lat2 - lat1);
    const dLon = toRadians(lon2 - lon1);

    const a =
        Math.sin(dLat / 2) * Math.sin(dLat / 2) +
        Math.cos(lat1Rad) * Math.cos(lat2Rad) * Math.sin(dLon / 2) * Math.sin(dLon / 2);
    const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
    return Math.ceil(EARTH_RADIUS_KM * c);
}

/**
 * Short-path and long-path bearings + distances between two locators.
 * Returns `null` if either locator is invalid.
 */
export function pathInfo(localGrid: string, remoteGrid: string): PathInfo | null {
    const local = gridToDecimal(localGrid);
    const remote = gridToDecimal(remoteGrid);
    if (local === null || remote === null) {
        return null;
    }

    const shortPathBearing = calculateBearing(local.lat, local.lon, remote.lat, remote.lon);
    const longPathBearing = Math.round(((shortPathBearing + 180) % 360) * 10) / 10;

    const shortPathDistanceKm = haversineKm(local.lat, local.lon, remote.lat, remote.lon);
    const earthCircumferenceKm = 2 * Math.PI * EARTH_RADIUS_KM;
    const longPathDistanceKm = Math.ceil(earthCircumferenceKm - shortPathDistanceKm);

    return {
        shortPathBearing,
        longPathBearing,
        shortPathDistanceKm,
        shortPathDistanceMiles: Math.ceil(shortPathDistanceKm * KM_TO_MILES),
        longPathDistanceKm,
        longPathDistanceMiles: Math.ceil(longPathDistanceKm * KM_TO_MILES),
    };
}
