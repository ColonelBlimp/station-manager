import { describe, it, expect, beforeEach } from 'vitest';
import { flushSync } from 'svelte';
import { callsignStack } from './callsignStack.svelte';

/**
 * callsignStack — pile-up scratchpad.
 *
 * Verifies the contract StackingDrawer + QsoPanel rely on: LIFO order
 * with newest-at-top, normalised push, dedupe, three pop affordances
 * (top / bottom / index), derived count.
 */

describe('callsignStack', () => {
    beforeEach(() => {
        callsignStack.clear();
        flushSync();
    });

    describe('push', () => {
        it('adds the call to the top (index 0) so newest is on top', () => {
            callsignStack.push('M0XYZ');
            callsignStack.push('G0ABC');
            flushSync();
            expect(callsignStack.items).toEqual(['G0ABC', 'M0XYZ']);
        });

        it('normalises to trim+upper before storing', () => {
            callsignStack.push('  m0xyz  ');
            flushSync();
            expect(callsignStack.items).toEqual(['M0XYZ']);
        });

        it('is a no-op on empty or whitespace-only input', () => {
            callsignStack.push('');
            callsignStack.push('   ');
            flushSync();
            expect(callsignStack.items).toEqual([]);
        });

        it('dedupes by normalised value (same call mixed-case is rejected)', () => {
            callsignStack.push('M0XYZ');
            callsignStack.push('m0xyz');
            callsignStack.push(' M0XYZ ');
            flushSync();
            expect(callsignStack.items).toEqual(['M0XYZ']);
        });
    });

    describe('popTop', () => {
        it('returns and removes the newest entry', () => {
            callsignStack.push('M0XYZ');
            callsignStack.push('G0ABC');
            flushSync();

            const popped = callsignStack.popTop();
            flushSync();

            expect(popped).toBe('G0ABC');
            expect(callsignStack.items).toEqual(['M0XYZ']);
        });

        it('returns undefined when the stack is empty', () => {
            expect(callsignStack.popTop()).toBeUndefined();
        });
    });

    describe('popBottom', () => {
        it('returns and removes the oldest entry', () => {
            callsignStack.push('M0XYZ');
            callsignStack.push('G0ABC');
            callsignStack.push('F5DEF');
            flushSync();

            const popped = callsignStack.popBottom();
            flushSync();

            expect(popped).toBe('M0XYZ');
            expect(callsignStack.items).toEqual(['F5DEF', 'G0ABC']);
        });

        it('returns undefined when the stack is empty', () => {
            expect(callsignStack.popBottom()).toBeUndefined();
        });
    });

    describe('popAt', () => {
        it('removes the entry at the given index', () => {
            callsignStack.push('M0XYZ');
            callsignStack.push('G0ABC');
            callsignStack.push('F5DEF');
            flushSync();

            const popped = callsignStack.popAt(1);
            flushSync();

            expect(popped).toBe('G0ABC');
            expect(callsignStack.items).toEqual(['F5DEF', 'M0XYZ']);
        });

        it('returns undefined for a negative index', () => {
            callsignStack.push('M0XYZ');
            flushSync();
            expect(callsignStack.popAt(-1)).toBeUndefined();
            expect(callsignStack.items).toEqual(['M0XYZ']);
        });

        it('returns undefined for an out-of-range index', () => {
            callsignStack.push('M0XYZ');
            flushSync();
            expect(callsignStack.popAt(5)).toBeUndefined();
            expect(callsignStack.items).toEqual(['M0XYZ']);
        });
    });

    describe('count', () => {
        it('derives from items.length', () => {
            flushSync();
            expect(callsignStack.count).toBe(0);

            callsignStack.push('M0XYZ');
            callsignStack.push('G0ABC');
            flushSync();
            expect(callsignStack.count).toBe(2);

            callsignStack.popTop();
            flushSync();
            expect(callsignStack.count).toBe(1);
        });
    });

    describe('clear', () => {
        it('empties the stack', () => {
            callsignStack.push('M0XYZ');
            callsignStack.push('G0ABC');
            flushSync();

            callsignStack.clear();
            flushSync();

            expect(callsignStack.items).toEqual([]);
            expect(callsignStack.count).toBe(0);
        });
    });
});
