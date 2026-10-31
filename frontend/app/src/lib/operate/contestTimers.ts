// Contest timers (W-0023 weekend slice; operator, 2026-10-31):
//   - "Last QSO": full minutes since the last contest QSO, counted from the
//     clock minute after it, as N1MM+ documents (n1mmwp.hamdocs.com, Info
//     window → QSO timers); an off period once it reaches the contest's
//     minimum (UK/EI rule 4.4b: 60 minutes);
//   - "Rate": contest QSOs completed in the last 60 minutes (N1MM's "last hour");
//   - "Elapsed": time since the first contest QSO — deliberately not the
//     contest's operating-time rules (the operator: no syncing with them).

export interface TimerReadout {
    /** Full minutes since the clock minute after the last QSO; null before any. */
    sinceLastMin: number | null;
    /** The current gap has reached the off-time minimum. */
    offQualifies: boolean;
    /** Contest QSOs completed in the last 60 minutes. */
    ratePerHour: number;
    /** Whole minutes since the first contest QSO; null before any. */
    elapsedMin: number | null;
}

const minuteOf = (ms: number): number => Math.floor(ms / 60_000);

/** Pure: completions are each contest QSO's completion time (epoch ms). */
export function computeTimers(
    completions: readonly number[],
    nowMs: number,
    offMin: number
): TimerReadout {
    const ratePerHour = completions.filter((t) => t > nowMs - 3_600_000 && t <= nowMs).length;
    if (completions.length === 0) {
        return { sinceLastMin: null, offQualifies: false, ratePerHour, elapsedMin: null };
    }
    const sinceLastMin = Math.max(0, minuteOf(nowMs) - (minuteOf(Math.max(...completions)) + 1));
    const elapsedMin = Math.max(0, Math.floor((nowMs - Math.min(...completions)) / 60_000));
    return { sinceLastMin, offQualifies: sinceLastMin >= offMin, ratePerHour, elapsedMin };
}
