import { describe, it, expect } from 'vitest';
import { isValidCallsign } from './callsign';

describe('isValidCallsign', () => {
    it('accepts a standard callsign', () => {
        expect(isValidCallsign('M0ABC')).toBe(true);
    });

    it('accepts a callsign with a slashed prefix', () => {
        expect(isValidCallsign('GW/M0ABC')).toBe(true);
    });

    it('accepts a callsign with a slashed suffix', () => {
        expect(isValidCallsign('M0ABC/P')).toBe(true);
    });

    it('lowercases are normalized before matching', () => {
        expect(isValidCallsign('m0abc')).toBe(true);
    });

    it('trims surrounding whitespace', () => {
        expect(isValidCallsign('  M0ABC  ')).toBe(true);
    });

    it('treats empty string as not-invalid (presence is a separate concern)', () => {
        expect(isValidCallsign('')).toBe(true);
    });

    it('treats whitespace-only string as not-invalid', () => {
        expect(isValidCallsign('   ')).toBe(true);
    });

    it('rejects too-short input', () => {
        expect(isValidCallsign('M0')).toBe(false);
    });

    it('rejects pure-letter input (no digit)', () => {
        expect(isValidCallsign('ABCDE')).toBe(false);
    });

    it('rejects pure-digit input (no letter)', () => {
        expect(isValidCallsign('12345')).toBe(false);
    });

    it('rejects a leading slash', () => {
        expect(isValidCallsign('/M0ABC')).toBe(false);
    });

    it('rejects a trailing slash', () => {
        expect(isValidCallsign('M0ABC/')).toBe(false);
    });

    it('rejects consecutive slashes', () => {
        expect(isValidCallsign('M0ABC//P')).toBe(false);
    });

    it('rejects non-alphanumeric characters', () => {
        expect(isValidCallsign('M0-ABC')).toBe(false);
    });
});
