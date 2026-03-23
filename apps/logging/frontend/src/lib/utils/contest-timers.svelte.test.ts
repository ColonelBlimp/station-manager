import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { ContestTimersClass } from './contest-timers.svelte';

describe('ContestTimersClass', () => {
    let timers: ContestTimersClass;

    beforeEach(() => {
        vi.useFakeTimers();
        timers = new ContestTimersClass();
    });

    afterEach(() => {
        timers.stop(); // Ensure cleanup
        vi.useRealTimers();
    });

    describe('initial state', () => {
        it('should have correct default values', () => {
            expect(timers.sinceStartTimerID).toBeNull();
            expect(timers.sinceLastQsoTimerID).toBeNull();
            expect(timers.elapsedSinceStart).toBe(0);
            expect(timers.elapsedSinceLastQso).toBe(0);
            expect(timers.isRunning).toBe(false);
        });

        it('should have formatted values initialized to 00:00:00', () => {
            expect(timers.formattedSinceStart).toBe('00:00:00');
            expect(timers.formattedSinceLastQso).toBe('00:00:00');
        });
    });

    describe('derived formatted values', () => {
        it('should update formattedSinceStart when elapsedSinceStart changes', () => {
            timers.start();
            expect(timers.formattedSinceStart).toBe('00:00:00');

            vi.advanceTimersByTime(1000);
            expect(timers.formattedSinceStart).toBe('00:00:01');

            vi.advanceTimersByTime(59000); // 60 seconds total
            expect(timers.formattedSinceStart).toBe('00:01:00');

            vi.advanceTimersByTime(3540000); // 60 minutes total
            expect(timers.formattedSinceStart).toBe('01:00:00');
        });

        it('should update formattedSinceLastQso when elapsedSinceLastQso changes', () => {
            timers.reset();
            expect(timers.formattedSinceLastQso).toBe('00:00:00');

            vi.advanceTimersByTime(1000);
            expect(timers.formattedSinceLastQso).toBe('00:00:01');

            vi.advanceTimersByTime(59000); // 60 seconds total
            expect(timers.formattedSinceLastQso).toBe('00:01:00');
        });

        it('should reset formatted values when stop() is called', () => {
            timers.start();
            timers.reset();
            vi.advanceTimersByTime(5000);

            expect(timers.formattedSinceStart).toBe('00:00:05');
            expect(timers.formattedSinceLastQso).toBe('00:00:05');

            timers.stop();

            expect(timers.formattedSinceStart).toBe('00:00:00');
            expect(timers.formattedSinceLastQso).toBe('00:00:00');
        });

        it('should reset formattedSinceLastQso when reset() is called', () => {
            timers.reset();
            vi.advanceTimersByTime(5000);
            expect(timers.formattedSinceLastQso).toBe('00:00:05');

            timers.reset();
            expect(timers.formattedSinceLastQso).toBe('00:00:00');
        });
    });

    describe('start()', () => {
        it('should set isRunning to true', () => {
            expect(timers.isRunning).toBe(false);

            timers.start();

            expect(timers.isRunning).toBe(true);
        });

        it('should set sinceStartTimerID to a non-null value', () => {
            expect(timers.sinceStartTimerID).toBeNull();

            timers.start();

            expect(timers.sinceStartTimerID).not.toBeNull();
        });

        it('should increment elapsedSinceStart every second', () => {
            timers.start();

            expect(timers.elapsedSinceStart).toBe(0);

            vi.advanceTimersByTime(1000);
            expect(timers.elapsedSinceStart).toBe(1);

            vi.advanceTimersByTime(1000);
            expect(timers.elapsedSinceStart).toBe(2);

            vi.advanceTimersByTime(3000);
            expect(timers.elapsedSinceStart).toBe(5);
        });

        it('should not start a second timer if already running', () => {
            timers.start();
            const firstTimerID = timers.sinceStartTimerID;

            timers.start();

            expect(timers.sinceStartTimerID).toBe(firstTimerID);
        });
    });

    describe('stop()', () => {
        it('should set isRunning to false', () => {
            timers.start();
            expect(timers.isRunning).toBe(true);

            timers.stop();

            expect(timers.isRunning).toBe(false);
        });

        it('should reset sinceStartTimerID to null', () => {
            timers.start();
            expect(timers.sinceStartTimerID).not.toBeNull();

            timers.stop();

            expect(timers.sinceStartTimerID).toBeNull();
        });

        it('should reset all elapsed counters to 0', () => {
            timers.start();
            vi.advanceTimersByTime(5000);
            expect(timers.elapsedSinceStart).toBe(5);

            timers.stop();

            expect(timers.elapsedSinceStart).toBe(0);
            expect(timers.elapsedSinceLastQso).toBe(0);
        });

        it('should stop the timer from incrementing', () => {
            timers.start();
            vi.advanceTimersByTime(2000);
            expect(timers.elapsedSinceStart).toBe(2);

            timers.stop();
            vi.advanceTimersByTime(5000);

            expect(timers.elapsedSinceStart).toBe(0);
        });

        it('should clear the lastQso timer as well', () => {
            timers.start();
            timers.reset(); // Start the lastQso timer
            vi.advanceTimersByTime(3000);
            expect(timers.elapsedSinceLastQso).toBe(3);

            timers.stop();

            expect(timers.sinceLastQsoTimerID).toBeNull();
            expect(timers.elapsedSinceLastQso).toBe(0);
        });
    });

    describe('reset()', () => {
        it('should reset elapsedSinceLastQso to 0', () => {
            timers.reset();
            vi.advanceTimersByTime(5000);
            expect(timers.elapsedSinceLastQso).toBe(5);

            timers.reset();

            expect(timers.elapsedSinceLastQso).toBe(0);
        });

        it('should start a new lastQso timer', () => {
            timers.reset();
            const firstTimerID = timers.sinceLastQsoTimerID;
            expect(firstTimerID).not.toBeNull();

            vi.advanceTimersByTime(2000);
            timers.reset();

            // New timer should be created
            expect(timers.sinceLastQsoTimerID).not.toBeNull();
            expect(timers.elapsedSinceLastQso).toBe(0);
        });

        it('should increment elapsedSinceLastQso after reset', () => {
            timers.reset();
            expect(timers.elapsedSinceLastQso).toBe(0);

            vi.advanceTimersByTime(1000);
            expect(timers.elapsedSinceLastQso).toBe(1);

            vi.advanceTimersByTime(2000);
            expect(timers.elapsedSinceLastQso).toBe(3);
        });

        it('should clear previous timer even if elapsedSinceLastQso is 0', () => {
            timers.reset();
            const firstTimerID = timers.sinceLastQsoTimerID;
            expect(firstTimerID).not.toBeNull();

            // Reset immediately without advancing time
            timers.reset();
            const secondTimerID = timers.sinceLastQsoTimerID;

            // A new timer should be created with a different ID
            expect(secondTimerID).not.toBeNull();
            expect(secondTimerID).not.toBe(firstTimerID);

            // The old timer should not cause double increments
            vi.advanceTimersByTime(1000);
            expect(timers.elapsedSinceLastQso).toBe(1); // Should be 1, not 2
        });
    });

    describe('integration: start and stop cycle', () => {
        it('should properly manage state through multiple start/stop cycles', () => {
            // First cycle
            timers.start();
            expect(timers.isRunning).toBe(true);
            vi.advanceTimersByTime(3000);
            expect(timers.elapsedSinceStart).toBe(3);

            timers.stop();
            expect(timers.isRunning).toBe(false);
            expect(timers.elapsedSinceStart).toBe(0);

            // Second cycle
            timers.start();
            expect(timers.isRunning).toBe(true);
            vi.advanceTimersByTime(2000);
            expect(timers.elapsedSinceStart).toBe(2);

            timers.stop();
            expect(timers.isRunning).toBe(false);
        });
    });
});
