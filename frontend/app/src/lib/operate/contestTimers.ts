// Contest timers (W-0023 weekend slice; operator, 2026-10-31), counted the
// way N1MM+ documents the contest scorers do (n1mmwp.hamdocs.com, Entry
// window → Off Times; Info window → QSO timers):
//   - off time is consecutive FULL clock minutes with no QSO, from the clock
//     minute after a QSO to the clock minute before the next;
//   - a gap is off time only once it reaches the contest's minimum (UK/EI
//     12-hour category, rule 4.4b: 60 minutes); a shorter gap is operating time;
//   - the minutes before the first QSO are off time;
//   - operating time is every other elapsed full minute of the contest.

export interface TimerReadout {
    /** Full minutes since the clock minute after the last QSO; null before any. */
    sinceLastMin: number | null;
    /** The current gap has reached the off-time minimum. */
    offQualifies: boolean;
    /** Operating time so far, in minutes. */
    opMin: number;
}

const minuteOf = (ms: number): number => Math.floor(ms / 60_000);

/** Pure: completions are each contest QSO's completion time (epoch ms). */
export function computeTimers(
    completions: readonly number[],
    startMs: number,
    endMs: number,
    nowMs: number,
    offMin: number
): TimerReadout {
    if (completions.length === 0) return { sinceLastMin: null, offQualifies: false, opMin: 0 };
    const now = minuteOf(nowMs);
    const last = Math.max(...completions.map(minuteOf));
    const sinceLastMin = Math.max(0, now - (last + 1));

    // Elapsed full minutes of the contest: [first, stop).
    const first = minuteOf(startMs);
    const stop = Math.min(now, minuteOf(endMs));
    const qsoMinutes = [...new Set(completions.map(minuteOf))]
        .filter((m) => m >= first && m < stop)
        .sort((a, b) => a - b);
    let opMin = 0;
    if (qsoMinutes.length > 0) {
        // From the first QSO minute (the lead-in is off) to the end of what has
        // elapsed: each QSO minute counts, and each gap counts unless it qualifies.
        let prev = qsoMinutes[0];
        opMin = 1;
        const gapCounts = (gap: number): number => (gap >= offMin ? 0 : gap);
        for (const m of qsoMinutes.slice(1)) {
            opMin += gapCounts(m - prev - 1) + 1;
            prev = m;
        }
        opMin += gapCounts(stop - prev - 1);
    }
    return { sinceLastMin, offQualifies: sinceLastMin >= offMin, opMin };
}
