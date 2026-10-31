// The contest dupe warning (W-0023 weekend slice; dogfood 2026-10-31, 7Q7EB
// logged twice on 20m with no warning). In contest mode, committing the
// callsign asks the daemon whether that station was already worked on this
// band in this contest occurrence. A hit is a warning, never a refusal: the
// sponsor keeps duplicates in the log (ruling A). A failed check shows nothing
// — no false alarm. Injected like the submit seam (ADR 0045).

import { draft } from './qso.svelte';
import { rig } from './rig.svelte';
import { contest, contestId, contestStart } from './contest.svelte';

type DupeCheck = (
    call: string,
    band: string,
    contestId: string,
    start: string
) => Promise<boolean | null>;
let check: DupeCheck | null = null;
let seq = 0;

/** The last answer: which call and band it is for, and whether it is a dupe. */
export const contestDupeState = $state({ call: '', band: '', occurrence: '', dupe: false });

export function setContestDupeCheck(fn: DupeCheck | null): void {
    check = fn;
}

export async function checkContestDupe(
    call: string,
    band: string,
    contestId: string,
    start: string
): Promise<void> {
    const n = ++seq;
    clearContestDupe(n);
    if (check === null || call === '' || band === '') return;
    let dupe: boolean | null;
    try {
        dupe = await check(call, band, contestId, start);
    } catch {
        dupe = null;
    }
    if (n !== seq) return; // a later question (or a Clear) has superseded this one
    Object.assign(contestDupeState, {
        call,
        band,
        occurrence: `${contestId}|${start}`,
        dupe: dupe === true,
    });
}

/** Forget the answer, and any question still out. */
export function clearContestDupe(n = ++seq): void {
    seq = n;
    Object.assign(contestDupeState, { call: '', band: '', occurrence: '', dupe: false });
}

/** Show the warning: the answer is a dupe for the call, band and contest
 *  occurrence on the form now, in contest mode (Codex review 4249d4d7: a
 *  result never outlives the contest it was checked against). */
export function contestDupeShown(): boolean {
    return (
        contest.on &&
        contestDupeState.dupe &&
        contestDupeState.occurrence === `${contestId()}|${contestStart()}` &&
        contestDupeState.call === draft.callsign.trim().toUpperCase() &&
        contestDupeState.band === rig.band
    );
}
