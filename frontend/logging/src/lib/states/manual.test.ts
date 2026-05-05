import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { flushSync } from 'svelte';
import { manualState } from './manual.svelte';

/**
 * manualState persistence — ADR 0011.
 *
 * Verifies the localStorage hydration + mirror pattern. Note that the
 * module-level instance is created once on first import; tests can't
 * directly observe the hydration path against fresh defaults without
 * isolating module loads. We test the live mirror behaviour (writes
 * land in localStorage) and the hydration parsers via the helper
 * functions' observable effects on the next field write.
 */

describe('manualState persistence (ADR 0011)', () => {
    beforeEach(() => {
        try { localStorage.clear(); } catch { /* noop */ }
    });

    afterEach(() => {
        try { localStorage.clear(); } catch { /* noop */ }
    });

    describe('mirror writes to localStorage', () => {
        it('saves vfoA on write', () => {
            manualState.vfoA = 21_200_000;
            flushSync();
            expect(localStorage.getItem('sm.manual.vfoA')).toBe('21200000');
        });

        it('saves vfoB on write', () => {
            manualState.vfoB = 7_100_000;
            flushSync();
            expect(localStorage.getItem('sm.manual.vfoB')).toBe('7100000');
        });

        it('saves mode on write', () => {
            manualState.mode = 'CW';
            flushSync();
            expect(localStorage.getItem('sm.manual.mode')).toBe('CW');
        });

        it('saves subMode on write', () => {
            manualState.subMode = 'CW-N';
            flushSync();
            expect(localStorage.getItem('sm.manual.subMode')).toBe('CW-N');
        });

        it('saves selectedVfo on write', () => {
            manualState.selectedVfo = 'B';
            flushSync();
            expect(localStorage.getItem('sm.manual.selectedVfo')).toBe('B');
        });

        it('uses the sm.manual.* key namespace', () => {
            manualState.vfoA = 14_300_000;
            flushSync();
            const keys = Object.keys(localStorage).filter(k => k.startsWith('sm.manual.'));
            expect(keys).toContain('sm.manual.vfoA');
        });
    });

    describe('storage failure handling', () => {
        it('does not throw when localStorage.setItem fails', () => {
            const setItemSpy = vi.spyOn(Storage.prototype, 'setItem')
                .mockImplementation(() => { throw new Error('quota exceeded'); });
            try {
                expect(() => {
                    manualState.vfoA = 21_300_000;
                    flushSync();
                }).not.toThrow();
                // In-memory state still updates even when storage fails.
                expect(manualState.vfoA).toBe(21_300_000);
            } finally {
                setItemSpy.mockRestore();
            }
        });
    });
});
