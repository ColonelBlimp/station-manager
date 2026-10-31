// W-0023 weekend slice (operator, 2026-10-31): contest timers in the shell
// header, counted the way N1MM+ documents the contest scorers do
// (n1mmwp.hamdocs.com, Entry window → Off Times; Info window → QSO timers):
//   - Off time is consecutive FULL clock minutes with no QSO, from the clock
//     minute after a QSO to the clock minute before the next.
//   - A gap qualifies as off time only when it reaches the contest's minimum
//     (UK/EI 12-hour, rule 4.4b: 60 minutes); a shorter gap is operating time.
//   - The minutes before the first QSO are off time.
//   - Operating time is every other elapsed minute of the contest.
import { describe, it, expect } from 'vitest';
import { computeTimers } from './contestTimers';

const START = Date.UTC(2026, 9, 31, 12, 0);
const END = START + 24 * 3_600_000;
const at = (h: number, m: number, s = 0) => Date.UTC(2026, 9, 31, h, m, s);

describe('computeTimers', () => {
    it('nothing logged: no last-QSO time, no operating time', () => {
        expect(computeTimers([], START, END, at(13, 0), 60)).toEqual({
            sinceLastMin: null,
            offQualifies: false,
            opMin: 0,
        });
    });

    it('since last counts full minutes from the clock minute after the QSO', () => {
        const q = [at(12, 10, 30)];
        expect(computeTimers(q, START, END, at(12, 10, 59), 60).sinceLastMin).toBe(0);
        expect(computeTimers(q, START, END, at(12, 11, 59), 60).sinceLastMin).toBe(0);
        expect(computeTimers(q, START, END, at(12, 12, 0), 60).sinceLastMin).toBe(1);
        expect(computeTimers(q, START, END, at(12, 31, 0), 60).sinceLastMin).toBe(20);
    });

    it('the gap qualifies as off time at the minimum, not a minute before', () => {
        const q = [at(12, 10, 30)];
        expect(computeTimers(q, START, END, at(13, 10, 0), 60)).toMatchObject({
            sinceLastMin: 59,
            offQualifies: false,
        });
        expect(computeTimers(q, START, END, at(13, 11, 0), 60)).toMatchObject({
            sinceLastMin: 60,
            offQualifies: true,
        });
    });

    it('operating time: the lead-in is off; short gaps count; qualifying gaps do not', () => {
        // First QSO 12:10 (12:00–12:09 lead-in: off). QSOs every minute to
        // 12:19, a 30-minute gap (counts), QSO 12:50, then a 90-minute gap
        // (off), QSO 14:21, now 14:30.
        const q = [];
        for (let m = 10; m <= 19; m++) q.push(at(12, m, 5));
        q.push(at(12, 50, 5), at(14, 21, 5));
        const r = computeTimers(q, START, END, at(14, 30), 60);
        // 12:10–12:50 = 41 min (10 QSO minutes, 30 idle, 1); 14:21–14:29 = 9 min
        // (the current gap of 8 has not qualified).
        expect(r.opMin).toBe(41 + 9);
    });

    it('an ongoing gap leaves operating time once it qualifies', () => {
        const q = [at(12, 0, 30)];
        // 12:00 is the QSO minute; at 13:00 the gap 12:01–12:59 (59 min) still counts.
        expect(computeTimers(q, START, END, at(13, 0), 60).opMin).toBe(60);
        // At 13:01 the gap reached 60 and becomes off time.
        expect(computeTimers(q, START, END, at(13, 1), 60).opMin).toBe(1);
    });

    it('QSOs before the start count for since-last but not operating time; nothing past the end', () => {
        const before = [at(6, 56)];
        expect(computeTimers(before, START, END, at(7, 30), 60)).toMatchObject({
            sinceLastMin: 33,
            opMin: 0,
        });
        const q = [at(12, 0, 30)];
        expect(computeTimers(q, START, END, END + 3_600_000, 60).opMin).toBe(1);
    });
});
