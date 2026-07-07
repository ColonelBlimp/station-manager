import { describe, it, expect } from 'vitest';
import { isValidRst, isValidSignalReport } from './rst';

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
