import { describe, it, expect } from 'vitest';
import {
    calculateBearing,
    gridToDecimal,
    haversineKm,
    pathInfo,
} from './bearing';

describe('gridToDecimal', () => {
    it('returns null for empty input', () => {
        expect(gridToDecimal('')).toBeNull();
    });

    it('returns null for invalid grid', () => {
        expect(gridToDecimal('ZZ99')).toBeNull();
    });

    it('case-folds input', () => {
        const a = gridToDecimal('JN58td');
        const b = gridToDecimal('JN58TD');
        expect(a).not.toBeNull();
        expect(b).not.toBeNull();
        expect(a!.lat).toBeCloseTo(b!.lat, 9);
        expect(a!.lon).toBeCloseTo(b!.lon, 9);
    });

    // v1 reference vectors from station-manager-v1/internal/maidenhead/bearing_test.go
    it('matches v1 vectors', () => {
        const munich = gridToDecimal('JN58td');
        expect(munich).not.toBeNull();
        expect(munich!.lat).toBeCloseTo(48.1458333, 4);
        expect(munich!.lon).toBeCloseTo(11.625, 4);

        const aa00aa = gridToDecimal('AA00aa');
        expect(aa00aa).not.toBeNull();
        expect(aa00aa!.lat).toBeCloseTo(-89.9791667, 4);
        expect(aa00aa!.lon).toBeCloseTo(-179.9583333, 4);

        const jj00aa = gridToDecimal('JJ00aa');
        expect(jj00aa).not.toBeNull();
        expect(jj00aa!.lat).toBeCloseTo(0.0208333, 4);
        expect(jj00aa!.lon).toBeCloseTo(0.0416667, 4);
    });

    it('handles 4-char input', () => {
        // IO91 corner is (-2, 51); centre is (-1, 51.5).
        const io91 = gridToDecimal('IO91');
        expect(io91).not.toBeNull();
        expect(io91!.lat).toBeCloseTo(51.5, 9);
        expect(io91!.lon).toBeCloseTo(-1.0, 9);
    });

    it('handles 8-char input', () => {
        const io91vl42 = gridToDecimal('IO91vl42');
        expect(io91vl42).not.toBeNull();
        // 6-char IO91vl centre is well-defined; 8-char shifts within
        // the subsquare. Just check we got finite numbers in range.
        expect(io91vl42!.lat).toBeGreaterThan(-90);
        expect(io91vl42!.lat).toBeLessThan(90);
        expect(io91vl42!.lon).toBeGreaterThan(-180);
        expect(io91vl42!.lon).toBeLessThan(180);
    });
});

describe('calculateBearing', () => {
    it('returns 0 for due north', () => {
        const b = calculateBearing(0, 0, 10, 0);
        expect(b).toBeGreaterThanOrEqual(-1);
        expect(b).toBeLessThanOrEqual(1);
    });

    it('returns 90 for due east', () => {
        const b = calculateBearing(0, 0, 0, 10);
        expect(b).toBeGreaterThanOrEqual(89);
        expect(b).toBeLessThanOrEqual(91);
    });

    it('returns 180 for due south', () => {
        const b = calculateBearing(10, 0, 0, 0);
        expect(b).toBeGreaterThanOrEqual(179);
        expect(b).toBeLessThanOrEqual(181);
    });

    it('returns 270 for due west', () => {
        const b = calculateBearing(0, 10, 0, 0);
        expect(b).toBeGreaterThanOrEqual(269);
        expect(b).toBeLessThanOrEqual(271);
    });

    // London → New York, ~288.3° per v1's spherical model.
    it('matches v1 London→NY reference (~288.3°)', () => {
        const b = calculateBearing(51.5074, -0.1278, 40.7128, -74.006);
        expect(Math.abs(b - 288.3)).toBeLessThanOrEqual(1.0);
    });

    it('rounds to 0.1°', () => {
        // Result has at most one digit past the decimal point.
        const b = calculateBearing(51.5, -1, 48.15, 11.625);
        expect(b * 10).toBeCloseTo(Math.round(b * 10), 9);
    });
});

describe('haversineKm', () => {
    it('returns 0 for identical points', () => {
        expect(haversineKm(51.5, -1, 51.5, -1)).toBe(0);
    });

    it('matches a known great-circle distance (London→Paris ~344 km)', () => {
        const d = haversineKm(51.5074, -0.1278, 48.8566, 2.3522);
        // Math.ceil rounding; allow ±2 km for endpoint precision.
        expect(d).toBeGreaterThanOrEqual(342);
        expect(d).toBeLessThanOrEqual(346);
    });
});

describe('pathInfo', () => {
    it('returns null for invalid local grid', () => {
        expect(pathInfo('ZZ99', 'JN58td')).toBeNull();
    });

    it('returns null for invalid remote grid', () => {
        expect(pathInfo('JN58td', 'ZZ99')).toBeNull();
    });

    it('returns null for empty input', () => {
        expect(pathInfo('', 'JN58td')).toBeNull();
        expect(pathInfo('JN58td', '')).toBeNull();
    });

    it('short + long bearings are 180° apart', () => {
        const p = pathInfo('JN58td', 'FN31pr');
        expect(p).not.toBeNull();
        // Either spb + 180 ≡ lpb (mod 360) or lpb + 180 ≡ spb (mod 360).
        const a = ((p!.shortPathBearing + 180) % 360);
        const b = ((p!.longPathBearing + 180) % 360);
        const diffA = Math.abs(a - p!.longPathBearing);
        const diffB = Math.abs(b - p!.shortPathBearing);
        expect(Math.min(diffA, diffB)).toBeLessThanOrEqual(0.2);
    });

    it('short + long km approx Earth circumference', () => {
        const p = pathInfo('JN58td', 'FN31pr');
        expect(p).not.toBeNull();
        const sum = p!.shortPathDistanceKm + p!.longPathDistanceKm;
        const earthCircumference = Math.ceil(2 * Math.PI * 6371);
        // v1 tolerance was 2 km; ceil-rounding can drift a bit more
        // when both halves are independently ceiled — allow 3 km.
        expect(Math.abs(sum - earthCircumference)).toBeLessThanOrEqual(3);
    });

    it('miles match km via 0.621371 ceil-rounded', () => {
        const p = pathInfo('JN58td', 'FN31pr');
        expect(p).not.toBeNull();
        expect(p!.shortPathDistanceMiles).toBe(
            Math.ceil(p!.shortPathDistanceKm * 0.621371),
        );
        expect(p!.longPathDistanceMiles).toBe(
            Math.ceil(p!.longPathDistanceKm * 0.621371),
        );
    });

    it('produces sensible bearings for JN58td→FN31pr (Munich→New England)', () => {
        const p = pathInfo('JN58td', 'FN31pr');
        expect(p).not.toBeNull();
        // Roughly westward (NY is west of Munich).
        expect(p!.shortPathBearing).toBeGreaterThan(180);
        expect(p!.shortPathBearing).toBeLessThan(360);
    });
});
