import { describe, it, expect, afterEach, vi } from 'vitest';
import { qsoDraft } from './qsoDraft.svelte';

// The visible time fields stay HH:MM; submitTimeOn/Off reconcile them with the
// captured seconds so the stored/exported TIME_ON/TIME_OFF carry HH:MM:SS (the
// QSL manager's OQRS matches on the full timestamp).
describe('qsoDraft submit-time seconds reconciliation', () => {
    it('submitTimeOn keeps the captured seconds when the minute is unchanged', () => {
        qsoDraft.timeOn = '14:23';
        qsoDraft.timeOnFull = '14:23:47';
        expect(qsoDraft.submitTimeOn()).toBe('14:23:47');
    });

    it('submitTimeOn uses :00 when the operator hand-edited the minute', () => {
        qsoDraft.timeOn = '14:25';
        qsoDraft.timeOnFull = '14:23:47'; // stale capture, different minute
        expect(qsoDraft.submitTimeOn()).toBe('14:25:00');
    });

    it('submitTimeOn returns a blank/partial visible value unchanged', () => {
        qsoDraft.timeOn = '';
        qsoDraft.timeOnFull = '14:23:47';
        expect(qsoDraft.submitTimeOn()).toBe('');
    });

    describe('submitTimeOff captures the current second at submit', () => {
        afterEach(() => vi.useRealTimers());

        it('emits the current seconds when the visible minute is still now', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-07-01T14:23:47Z'));
            qsoDraft.timeOff = '14:23';
            expect(qsoDraft.submitTimeOff()).toBe('14:23:47');
        });

        it('emits :00 when the visible minute differs from now (hand-edited)', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-07-01T14:23:47Z'));
            qsoDraft.timeOff = '14:20';
            expect(qsoDraft.submitTimeOff()).toBe('14:20:00');
        });
    });
});
