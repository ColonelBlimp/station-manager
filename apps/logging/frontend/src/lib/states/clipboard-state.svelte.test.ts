import { describe, it, expect, beforeEach } from 'vitest';
import { clipboardState } from './clipboard-state.svelte';
import { DEFAULT_CLIPBOARD_MAX_LENGTH } from '$lib/constants/timers';

describe('clipboardState', () => {
    // Note: We're testing the singleton, so we need to reset it between tests
    beforeEach(() => {
        // Reset the state by clearing the list and resetting maxLength
        clipboardState.list = [];
        clipboardState.maxLength = DEFAULT_CLIPBOARD_MAX_LENGTH;
    });

    describe('initial state', () => {
        it('should have an empty list', () => {
            expect(clipboardState.list).toEqual([]);
        });

        it('should have default maxLength', () => {
            expect(clipboardState.maxLength).toBe(DEFAULT_CLIPBOARD_MAX_LENGTH);
        });
    });

    describe('add()', () => {
        it('should add an item to the list', () => {
            clipboardState.add('test-callsign');
            expect(clipboardState.list).toContain('test-callsign');
        });

        it('should add items to the beginning of the list (most recent first)', () => {
            clipboardState.add('first');
            clipboardState.add('second');
            clipboardState.add('third');

            expect(clipboardState.list[0]).toBe('third');
            expect(clipboardState.list[1]).toBe('second');
            expect(clipboardState.list[2]).toBe('first');
        });

        it('should trim whitespace from items', () => {
            clipboardState.add('  trimmed  ');
            expect(clipboardState.list[0]).toBe('trimmed');
        });

        it('should not add empty strings', () => {
            clipboardState.add('');
            expect(clipboardState.list).toEqual([]);
        });

        it('should not add whitespace-only strings', () => {
            clipboardState.add('   ');
            expect(clipboardState.list).toEqual([]);
        });

        it('should remove duplicates and move to front', () => {
            clipboardState.add('first');
            clipboardState.add('second');
            clipboardState.add('first'); // Add duplicate

            expect(clipboardState.list).toEqual(['first', 'second']);
            expect(clipboardState.list.length).toBe(2);
        });

        it('should enforce maxLength by removing oldest items', () => {
            clipboardState.maxLength = 3;

            clipboardState.add('one');
            clipboardState.add('two');
            clipboardState.add('three');
            clipboardState.add('four'); // Should push 'one' out

            expect(clipboardState.list.length).toBe(3);
            expect(clipboardState.list).toEqual(['four', 'three', 'two']);
            expect(clipboardState.list).not.toContain('one');
        });

        it('should handle maxLength of 1', () => {
            clipboardState.maxLength = 1;

            clipboardState.add('first');
            clipboardState.add('second');

            expect(clipboardState.list).toEqual(['second']);
        });

        it('should handle maxLength of 0', () => {
            clipboardState.maxLength = 0;

            clipboardState.add('item');

            expect(clipboardState.list).toEqual([]);
        });
    });

    describe('setMaxLength()', () => {
        it('should set maxLength to the provided value', () => {
            clipboardState.setMaxLength(5);
            expect(clipboardState.maxLength).toBe(5);
        });

        it('should truncate list if new maxLength is smaller than current list', () => {
            clipboardState.add('one');
            clipboardState.add('two');
            clipboardState.add('three');
            clipboardState.add('four');

            clipboardState.setMaxLength(2);

            expect(clipboardState.list.length).toBe(2);
            expect(clipboardState.list).toEqual(['four', 'three']); // Most recent kept
        });

        it('should floor decimal values', () => {
            clipboardState.setMaxLength(3.7);
            expect(clipboardState.maxLength).toBe(3);
        });

        it('should handle negative values by setting to 0', () => {
            clipboardState.setMaxLength(-5);
            expect(clipboardState.maxLength).toBe(0);
        });

        it('should handle NaN by setting to 0', () => {
            clipboardState.setMaxLength(NaN);
            expect(clipboardState.maxLength).toBe(0);
        });

        it('should handle Infinity by setting to 0', () => {
            clipboardState.setMaxLength(Infinity);
            expect(clipboardState.maxLength).toBe(0);
        });

        it('should not truncate list if new maxLength is larger than current list', () => {
            clipboardState.add('one');
            clipboardState.add('two');

            clipboardState.setMaxLength(10);

            expect(clipboardState.list.length).toBe(2);
            expect(clipboardState.list).toEqual(['two', 'one']);
        });
    });

    describe('integration', () => {
        it('should maintain correct order through multiple operations', () => {
            clipboardState.setMaxLength(5);

            clipboardState.add('A');
            clipboardState.add('B');
            clipboardState.add('C');
            clipboardState.add('A'); // Move A to front
            clipboardState.add('D');
            clipboardState.add('E');

            expect(clipboardState.list).toEqual(['E', 'D', 'A', 'C', 'B']);
        });
    });
});
