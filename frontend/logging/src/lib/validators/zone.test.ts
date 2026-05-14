import { describe, it, expect } from 'vitest';
import { isValidCqZone, isValidItuZone, isValidDxcc } from './zone';

/*
    Mirror the daemon's `TestHandlePutConfig_LoggingStationZoneValidation`
    table (handler_config_test.go) — the SPA validator must agree with
    the daemon, otherwise operators see "looks fine here, rejected on
    save" or the inverse. Daemon parity tests live alongside, so a
    future contract change is caught on both sides.
*/

describe('isValidCqZone (1-40)', () => {
    it('accepts the lower bound', () => {
        expect(isValidCqZone('1')).toBeNull();
    });

    it('accepts the upper bound', () => {
        expect(isValidCqZone('40')).toBeNull();
    });

    it('accepts an interior value', () => {
        expect(isValidCqZone('14')).toBeNull();
    });

    it('rejects 0 (below CQ Zone range)', () => {
        expect(isValidCqZone('0')).toBe('validators.cq_zone');
    });

    it('rejects 41 (above range)', () => {
        expect(isValidCqZone('41')).toBe('validators.cq_zone');
    });

    it('treats empty as valid (operator clearing the field)', () => {
        expect(isValidCqZone('')).toBeNull();
    });

    it('treats whitespace-only as valid (trimmed to empty)', () => {
        expect(isValidCqZone('   ')).toBeNull();
    });

    it('trims surrounding whitespace before validating', () => {
        expect(isValidCqZone('  14  ')).toBeNull();
    });

    it('rejects non-numeric input', () => {
        expect(isValidCqZone('14x')).toBe('validators.cq_zone');
    });

    it('rejects fractional input', () => {
        expect(isValidCqZone('14.5')).toBe('validators.cq_zone');
    });

    it('rejects negative input', () => {
        expect(isValidCqZone('-5')).toBe('validators.cq_zone');
    });
});

describe('isValidItuZone (1-90)', () => {
    it('accepts the lower bound', () => {
        expect(isValidItuZone('1')).toBeNull();
    });

    it('accepts the upper bound', () => {
        expect(isValidItuZone('90')).toBeNull();
    });

    it('accepts an interior value', () => {
        expect(isValidItuZone('27')).toBeNull();
    });

    it('rejects 0 (below ITU Zone range)', () => {
        expect(isValidItuZone('0')).toBe('validators.itu_zone');
    });

    it('rejects 91 (above range)', () => {
        expect(isValidItuZone('91')).toBe('validators.itu_zone');
    });

    it('treats empty as valid', () => {
        expect(isValidItuZone('')).toBeNull();
    });

    it('rejects non-numeric input', () => {
        expect(isValidItuZone('27x')).toBe('validators.itu_zone');
    });
});

describe('isValidDxcc (0-522)', () => {
    it('accepts 0 (None / maritime per ARRL)', () => {
        expect(isValidDxcc('0')).toBeNull();
    });

    it('accepts the upper bound', () => {
        expect(isValidDxcc('522')).toBeNull();
    });

    it('accepts an interior value', () => {
        expect(isValidDxcc('291')).toBeNull();
    });

    it('rejects 523 (above current ARRL max)', () => {
        expect(isValidDxcc('523')).toBe('validators.dxcc');
    });

    it('treats empty as valid', () => {
        expect(isValidDxcc('')).toBeNull();
    });

    it('rejects non-numeric input', () => {
        expect(isValidDxcc('291x')).toBe('validators.dxcc');
    });

    it('rejects fractional input', () => {
        expect(isValidDxcc('291.5')).toBe('validators.dxcc');
    });

    it('rejects negative input', () => {
        expect(isValidDxcc('-1')).toBe('validators.dxcc');
    });
});
