// The completion times of the chosen contest occurrence's contacts, for the
// header's contest timers (W-0023 weekend slice). Loaded from the daemon when
// the contest is chosen (so a reload or a second tab is right), and each
// contact logged here is added at once. Injected like the submit seam
// (ADR 0045).

type Loader = (contestId: string, start: string) => Promise<number[] | null>;
let loader: Loader | null = null;
let seq = 0;

export const contestHistory = $state({ key: '', completions: [] as number[], failed: false });

export function setContestHistoryLoader(fn: Loader | null): void {
    loader = fn;
}

export async function loadContestHistory(contestId: string, start: string): Promise<void> {
    const n = ++seq;
    Object.assign(contestHistory, { key: `${contestId}|${start}`, completions: [], failed: false });
    if (loader === null) return;
    let got: number[] | null;
    try {
        got = await loader(contestId, start);
    } catch {
        got = null;
    }
    if (n !== seq) return; // a later choice has superseded this load
    if (got === null) contestHistory.failed = true;
    else contestHistory.completions = got;
}

/** A contact of this occurrence was logged here: count it now. */
export function noteContestQso(contestId: string, start: string, completionMs: number): void {
    if (contestHistory.key !== `${contestId}|${start}`) return;
    contestHistory.completions = [...contestHistory.completions, completionMs];
}
