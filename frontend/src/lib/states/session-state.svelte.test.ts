import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { sessionState, sessionTimeState, cleanupSessionState } from './session-state.svelte';

describe('sessionState', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        // Reset state before each test
        cleanupSessionState();
        sessionTimeState.elapsed = 0;
        sessionState.operator = '';
        sessionState.total = 0;
        sessionState.list = [];
    });

    afterEach(() => {
        cleanupSessionState();
        vi.useRealTimers();
    });

    describe('initial state', () => {
        it('should have correct default values', () => {
            expect(sessionState.operator).toBe('');
            expect(sessionState.total).toBe(0);
            expect(sessionState.list).toEqual([]);
            expect(sessionTimeState.elapsed).toBe(0);
        });
    });

    describe('start()', () => {
        it('should reset elapsed time to 0', () => {
            sessionTimeState.elapsed = 100;

            sessionState.start();

            expect(sessionTimeState.elapsed).toBe(0);
        });

        it('should increment elapsed time every second', () => {
            sessionState.start();

            expect(sessionTimeState.elapsed).toBe(0);

            vi.advanceTimersByTime(1000);
            expect(sessionTimeState.elapsed).toBe(1);

            vi.advanceTimersByTime(1000);
            expect(sessionTimeState.elapsed).toBe(2);

            vi.advanceTimersByTime(3000);
            expect(sessionTimeState.elapsed).toBe(5);
        });

        it('should cleanup existing timer before starting new one', () => {
            sessionState.start();
            vi.advanceTimersByTime(5000);
            expect(sessionTimeState.elapsed).toBe(5);

            // Start again - should reset
            sessionState.start();
            expect(sessionTimeState.elapsed).toBe(0);

            vi.advanceTimersByTime(2000);
            // Should be 2, not 7 (no double-counting from old timer)
            expect(sessionTimeState.elapsed).toBe(2);
        });
    });

    describe('stop()', () => {
        it('should stop the timer from incrementing', () => {
            sessionState.start();
            vi.advanceTimersByTime(3000);
            expect(sessionTimeState.elapsed).toBe(3);

            sessionState.stop();
            vi.advanceTimersByTime(5000);

            // Should still be 3, timer stopped
            expect(sessionTimeState.elapsed).toBe(3);
        });
    });

    describe('reset()', () => {
        it('should reset elapsed time to 0', () => {
            sessionState.start();
            vi.advanceTimersByTime(5000);
            expect(sessionTimeState.elapsed).toBe(5);

            sessionState.reset();

            expect(sessionTimeState.elapsed).toBe(0);
        });

        it('should clear the list and total', () => {
            sessionState.list = [{} as never, {} as never];
            sessionState.total = 2;

            sessionState.reset();

            expect(sessionState.list).toEqual([]);
            expect(sessionState.total).toBe(0);
        });

        it('should stop the timer', () => {
            sessionState.start();
            vi.advanceTimersByTime(2000);

            sessionState.reset();
            vi.advanceTimersByTime(5000);

            expect(sessionTimeState.elapsed).toBe(0);
        });
    });

    describe('update()', () => {
        it('should update list and total', () => {
            const mockList = [{} as never, {} as never, {} as never];

            sessionState.update(mockList);

            expect(sessionState.list).toStrictEqual(mockList);
            expect(sessionState.total).toBe(3);
        });

        it('should handle empty list', () => {
            sessionState.list = [{} as never];
            sessionState.total = 1;

            sessionState.update([]);

            expect(sessionState.list).toEqual([]);
            expect(sessionState.total).toBe(0);
        });
    });

    describe('cleanupSessionState()', () => {
        it('should clear the timer interval', () => {
            sessionState.start();
            vi.advanceTimersByTime(2000);
            expect(sessionTimeState.elapsed).toBe(2);

            cleanupSessionState();
            vi.advanceTimersByTime(3000);

            // Timer should be stopped
            expect(sessionTimeState.elapsed).toBe(2);
        });

        it('should be safe to call multiple times', () => {
            sessionState.start();

            cleanupSessionState();
            cleanupSessionState();
            cleanupSessionState();

            // Should not throw
            expect(true).toBe(true);
        });
    });
});
