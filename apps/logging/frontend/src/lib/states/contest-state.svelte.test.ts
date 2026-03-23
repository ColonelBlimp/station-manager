import { describe, it, expect, beforeEach } from 'vitest';

// Since contestState uses $state runes which require Svelte compilation,
// we test the pure logic functions separately and the state behavior
// by directly manipulating and checking the exported object properties.

// Import the actual state - SvelteKit's vite plugin should handle the transformation
import { contestState } from '$lib/states/contest-state.svelte';

describe('contestState', () => {
    beforeEach(() => {
        // Reset state before each test
        contestState.isIncremental = true;
        contestState.totalQsos = 0;
        contestState.currentStx = '001';
        contestState.srxInvalid = false;
    });

    describe('initial state', () => {
        it('should have correct default values after reset', () => {
            expect(contestState.isIncremental).toBe(true);
            expect(contestState.totalQsos).toBe(0);
            expect(contestState.currentStx).toBe('001');
            expect(contestState.srxInvalid).toBe(false);
        });
    });

    describe('setCurrent', () => {
        it('should set the current STX value', () => {
            contestState.setCurrent('005');
            expect(contestState.currentStx).toBe('005');
            expect(contestState.isIncremental).toBe(true);
        });

        it('should trim whitespace from STX value', () => {
            contestState.setCurrent('  010  ');
            expect(contestState.currentStx).toBe('010');
        });

        it('should disable increment when STX starts with !', () => {
            contestState.setCurrent('!100');
            expect(contestState.currentStx).toBe('100');
            expect(contestState.isIncremental).toBe(false);
        });

        it('should re-enable increment when STX does not start with !', () => {
            contestState.setCurrent('!100');
            expect(contestState.isIncremental).toBe(false);

            contestState.setCurrent('200');
            expect(contestState.currentStx).toBe('200');
            expect(contestState.isIncremental).toBe(true);
        });
    });

    describe('increment', () => {
        it('should increment the STX value when isIncremental is true', () => {
            contestState.currentStx = '001';
            contestState.isIncremental = true;

            const result = contestState.increment();

            expect(result).toBe('002');
            expect(contestState.currentStx).toBe('002');
        });

        it('should preserve zero-padding when incrementing', () => {
            contestState.currentStx = '099';
            contestState.isIncremental = true;

            const result = contestState.increment();

            expect(result).toBe('100');
            expect(contestState.currentStx).toBe('100');
        });

        it('should not increment when isIncremental is false', () => {
            contestState.currentStx = '050';
            contestState.isIncremental = false;

            const result = contestState.increment();

            expect(result).toBe('050');
            expect(contestState.currentStx).toBe('050');
        });

        it('should handle non-numeric STX gracefully', () => {
            contestState.currentStx = 'ABC';
            contestState.isIncremental = true;

            const result = contestState.increment();

            expect(result).toBe('ABC');
            expect(contestState.currentStx).toBe('ABC');
        });

        it('should expand width when incrementing past current width', () => {
            contestState.currentStx = '99';
            contestState.isIncremental = true;

            const result = contestState.increment();

            expect(result).toBe('100');
        });
    });

    describe('srxInvalid state', () => {
        it('should set srxInvalid to true', () => {
            expect(contestState.srxInvalid).toBe(false);

            contestState.setSrxInvalid(true);

            expect(contestState.srxInvalid).toBe(true);
        });

        it('should set srxInvalid to false', () => {
            contestState.srxInvalid = true;

            contestState.setSrxInvalid(false);

            expect(contestState.srxInvalid).toBe(false);
        });

        it('should reset srxInvalid via resetSrxValidation', () => {
            contestState.srxInvalid = true;

            contestState.resetSrxValidation();

            expect(contestState.srxInvalid).toBe(false);
        });

        it('should be idempotent when already valid', () => {
            contestState.srxInvalid = false;

            contestState.resetSrxValidation();

            expect(contestState.srxInvalid).toBe(false);
        });
    });

    describe('totalQsos', () => {
        it('should allow setting totalQsos', () => {
            contestState.totalQsos = 100;
            expect(contestState.totalQsos).toBe(100);
        });
    });
});
