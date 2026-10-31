// W-0023 weekend slice (operator, 2026-10-31): the contest timers in the shell
// header — "Last QSO" (full minutes since the last contest QSO, counted from
// the clock minute after it, as N1MM+ does; green once the gap reaches the
// contest's off-time minimum, UK/EI rule 4.4b: 60), "Rate" (contest QSOs
// completed in the last 60 minutes) and "Elapsed" (time since the first
// contest QSO; the operator: no syncing with what the contest requires).
import { describe, it, expect } from 'vitest';
import { computeTimers } from './contestTimers';

const at = (h: number, m: number, s = 0) => Date.UTC(2026, 9, 31, h, m, s);

describe('computeTimers', () => {
    it('nothing logged: no last QSO, no rate, no elapsed time', () => {
        expect(computeTimers([], at(13, 0), 60)).toEqual({
            sinceLastMin: null,
            offQualifies: false,
            ratePerHour: 0,
            elapsedMin: null,
        });
    });

    it('since last counts full minutes from the clock minute after the QSO', () => {
        const q = [at(12, 10, 30)];
        expect(computeTimers(q, at(12, 10, 59), 60).sinceLastMin).toBe(0);
        expect(computeTimers(q, at(12, 11, 59), 60).sinceLastMin).toBe(0);
        expect(computeTimers(q, at(12, 12, 0), 60).sinceLastMin).toBe(1);
        expect(computeTimers(q, at(12, 31, 0), 60).sinceLastMin).toBe(20);
    });

    it('the gap is an off period at the minimum, not a minute before', () => {
        const q = [at(12, 10, 30)];
        expect(computeTimers(q, at(13, 10, 0), 60)).toMatchObject({
            sinceLastMin: 59,
            offQualifies: false,
        });
        expect(computeTimers(q, at(13, 11, 0), 60)).toMatchObject({
            sinceLastMin: 60,
            offQualifies: true,
        });
    });

    it('rate counts the contest QSOs of the last 60 minutes', () => {
        const q = [at(12, 0), at(12, 30), at(12, 59, 59), at(13, 0, 1), at(13, 30)];
        expect(computeTimers(q, at(13, 0, 30), 60).ratePerHour).toBe(3); // after 12:00:30
        expect(computeTimers(q, at(13, 59, 0), 60).ratePerHour).toBe(3);
        expect(computeTimers(q, at(15, 0), 60).ratePerHour).toBe(0);
    });

    it('elapsed is whole minutes since the first QSO, whatever the order given', () => {
        const q = [at(13, 0), at(12, 10, 30), at(12, 40)];
        expect(computeTimers(q, at(12, 10, 59), 60).elapsedMin).toBe(0);
        expect(computeTimers(q, at(15, 35, 30), 60).elapsedMin).toBe(205);
    });
});
