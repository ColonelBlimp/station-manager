import { describe, it, expect } from 'vitest';
import { isValidCallsign } from './callsign';

describe('isValidCallsign', () => {
    it('accepts a standard callsign', () => {
        expect(isValidCallsign('M0ABC')).toBeNull();
    });

    it('accepts a callsign with a slashed prefix', () => {
        expect(isValidCallsign('GW/M0ABC')).toBeNull();
    });

    it('accepts a callsign with a slashed suffix', () => {
        expect(isValidCallsign('M0ABC/P')).toBeNull();
    });

    it('lowercases are normalized before matching', () => {
        expect(isValidCallsign('m0abc')).toBeNull();
    });

    it('trims surrounding whitespace', () => {
        expect(isValidCallsign('  M0ABC  ')).toBeNull();
    });

    it('treats empty string as not-invalid (presence is a separate concern)', () => {
        expect(isValidCallsign('')).toBeNull();
    });

    it('treats whitespace-only string as not-invalid', () => {
        expect(isValidCallsign('   ')).toBeNull();
    });

    it('rejects too-short input', () => {
        expect(isValidCallsign('M0')).toBe('validators.callsign');
    });

    // Length-boundary regressions. Daemon parity is 3-32; pre-2026-05-12
    // the SPA was 3-20 which silently rejected long special-event calls
    // the daemon would accept (review I11).
    it('accepts a 30-character callsign (within daemon limit)', () => {
        expect(isValidCallsign('M0AB' + 'C'.repeat(26))).toBeNull();
    });

    it('rejects a 33-character callsign (past daemon limit)', () => {
        expect(isValidCallsign('M0AB' + 'C'.repeat(29))).toBe('validators.callsign');
    });

    it('rejects pure-letter input (no digit)', () => {
        expect(isValidCallsign('ABCDE')).toBe('validators.callsign');
    });

    it('rejects pure-digit input (no letter)', () => {
        expect(isValidCallsign('12345')).toBe('validators.callsign');
    });

    it('rejects a leading slash', () => {
        expect(isValidCallsign('/M0ABC')).toBe('validators.callsign');
    });

    it('rejects a trailing slash', () => {
        expect(isValidCallsign('M0ABC/')).toBe('validators.callsign');
    });

    it('rejects consecutive slashes', () => {
        expect(isValidCallsign('M0ABC//P')).toBe('validators.callsign');
    });

    it('rejects non-alphanumeric characters', () => {
        expect(isValidCallsign('M0-ABC')).toBe('validators.callsign');
    });
});
