import { describe, it, expect } from 'vitest';
import { isValidRs, isValidRst, isValidSignalReport } from './rst';

describe('isValidRst', () => {
    it('accepts a 2-digit RST', () => {
        expect(isValidRst('59')).toBeNull();
    });

    it('accepts a 3-digit RST', () => {
        expect(isValidRst('599')).toBeNull();
    });

    it('rejects a 1-digit RST', () => {
        expect(isValidRst('5')).toBe('validators.rst');
    });

    it('rejects a 4-digit RST', () => {
        expect(isValidRst('5999')).toBe('validators.rst');
    });

    it('treats empty string as not-invalid (presence is a separate concern)', () => {
        expect(isValidRst('')).toBeNull();
    });

    it('treats whitespace-only string as not-invalid', () => {
        expect(isValidRst('   ')).toBeNull();
    });

    it('rejects letters', () => {
        expect(isValidRst('5A9')).toBe('validators.rst');
    });

    // The scale, not just the shape (operator catch 2026-07-08).
    it('rejects readability above 5', () => {
        expect(isValidRst('77')).toBe('validators.rst');
        expect(isValidRst('69')).toBe('validators.rst');
    });

    it('rejects zero in any position', () => {
        expect(isValidRst('000')).toBe('validators.rst');
        expect(isValidRst('50')).toBe('validators.rst');
        expect(isValidRst('09')).toBe('validators.rst');
        expect(isValidRst('590')).toBe('validators.rst');
    });

    it('accepts honest weak-signal reports', () => {
        expect(isValidRst('31')).toBeNull();
        expect(isValidRst('44')).toBeNull();
        expect(isValidRst('339')).toBeNull();
    });
});

describe('isValidRs (voice / non-CW digital — no tone digit)', () => {
    it('accepts two-digit RS on the scale', () => {
        expect(isValidRs('59')).toBeNull();
        expect(isValidRs('31')).toBeNull();
    });

    it('rejects a three-digit report — tone is CW-only', () => {
        expect(isValidRs('599')).toBe('validators.rs');
    });

    it('rejects off-scale values and zeros', () => {
        expect(isValidRs('77')).toBe('validators.rs');
        expect(isValidRs('00')).toBe('validators.rs');
        expect(isValidRs('50')).toBe('validators.rs');
    });

    it('treats empty as not-invalid (presence is a separate concern)', () => {
        expect(isValidRs('')).toBeNull();
    });
});

describe('isValidSignalReport', () => {
    it('accepts a negative dB report', () => {
        expect(isValidSignalReport('-12')).toBeNull();
    });

    it('accepts a positive dB report with a sign', () => {
        expect(isValidSignalReport('+04')).toBeNull();
    });

    it('accepts an unsigned single digit', () => {
        expect(isValidSignalReport('0')).toBeNull();
    });

    it('accepts an unsigned two-digit value', () => {
        expect(isValidSignalReport('24')).toBeNull();
    });

    it('rejects three digits', () => {
        expect(isValidSignalReport('123')).toBe('validators.signalReport');
    });

    it('rejects a bare sign', () => {
        expect(isValidSignalReport('-')).toBe('validators.signalReport');
    });

    it('rejects letters', () => {
        expect(isValidSignalReport('-1a')).toBe('validators.signalReport');
    });

    it('treats empty string as not-invalid (presence is a separate concern)', () => {
        expect(isValidSignalReport('')).toBeNull();
    });
});
