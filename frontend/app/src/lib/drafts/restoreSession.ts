// Restoring a saved QSO onto the Phone / CW form (ADR 0085 RS3–RS11): the
// reservation, the re-read, and the environment the panel's Restore reads.
import {
    clearDraft,
    draftInProgress,
    entryLock,
    installRecoveredDraft,
    submitState,
} from '../operate/qso.svelte';
import { listSavedDrafts } from './draftStore';
import { draftLocksAvailable, reserveSavedDraft, type DraftReservation } from './draftLock';
import { recovered } from './recovered.svelte';
import { restoreEligibility, type RestoreEnv } from './restore';
import type { SavedDraft } from './savedDraft';

type RestoreResult = { ok: true } | { ok: false; reason: string };
let pending = false;
let reservation: DraftReservation | null = null;

function refusal(record: SavedDraft, env: RestoreEnv): string | null {
    if (recovered.record !== null) return 'A recovered QSO is already on the form.';
    if (draftInProgress() || submitState.busy)
        return 'Clear the current QSO before restoring a saved one.';
    const locked = entryLock();
    if (locked !== null) return locked;
    const eligibility = restoreEligibility(record, {
        ...env,
        locksAvailable: draftLocksAvailable(),
    });
    switch (eligibility.kind) {
        case 'eligible':
            return null;
        case 'unavailable':
            return eligibility.reason;
        case 'logged':
            return 'This saved QSO has already been logged.';
        case 'go-to-phone-cw':
            return 'Go to Phone / CW to restore this QSO.';
        case 'my-rig-changed':
            return `My rig differs: saved ‘${eligibility.saved}’; current ‘${eligibility.current}’.`;
    }
}

/** The environment is read again after each await; a panel snapshot cannot
 *  authorise filling a form or choosing a destination. */
export async function restoreSavedDraft(
    record: SavedDraft,
    readEnv: () => RestoreEnv
): Promise<RestoreResult> {
    if (pending) return { ok: false, reason: 'A Restore is already pending in this tab.' };
    pending = true;
    let claim: DraftReservation | null = null;
    try {
        const before = refusal(record, readEnv());
        if (before !== null) return { ok: false, reason: before };
        // Freeze what was offered: neither a refreshed panel nor caller-owned
        // data may change the candidate across the asynchronous claim/read.
        const offered = JSON.stringify(record);
        claim = await reserveSavedDraft(record.id);
        const afterClaim = refusal(record, readEnv());
        if (afterClaim !== null) return { ok: false, reason: afterClaim };
        const current = (await listSavedDrafts()).find((r) => r.id === record.id);
        if (current === undefined || JSON.stringify(current) !== offered) {
            return {
                ok: false,
                reason: 'The saved QSO changed or is no longer available. Read the saved list again.',
            };
        }
        const afterRead = refusal(current, readEnv());
        if (afterRead !== null) return { ok: false, reason: afterRead };
        installRecoveredDraft(current);
        reservation = claim;
        claim = null; // ownership now lives with this tab, not the panel
        return { ok: true };
    } catch (e) {
        return { ok: false, reason: e instanceof Error ? e.message : String(e) };
    } finally {
        await claim?.release();
        pending = false;
    }
}

// The page's Restore environment, wired by main.ts (it holds the station
// context); null leaves the panel offering no Restore.
let pageEnv: (() => RestoreEnv) | null = null;

export function setRestoreEnv(fn: (() => RestoreEnv) | null): void {
    pageEnv = fn;
}

/** The page's Restore environment now, or null when Restore is not wired. */
export function readRestoreEnv(): RestoreEnv | null {
    return pageEnv === null ? null : { ...pageEnv(), locksAvailable: draftLocksAvailable() };
}

/** Restore from the panel, against the page's environment. */
export function restoreFromPanel(record: SavedDraft): Promise<RestoreResult> {
    const env = pageEnv;
    if (env === null) return Promise.resolve({ ok: false, reason: 'Restore is not available.' });
    return restoreSavedDraft(record, env);
}

/** This tab holds the reservation for the recovered record `id`. */
export function ownsRecoveredRecord(id: string): boolean {
    return reservation !== null && recovered.record?.id === id;
}

/** Let go of the recovered QSO: detach it and empty the form synchronously —
 *  nothing typed after this lands in the recovered record — then release the
 *  reservation. Callers settle the record's storage first (commit 3). */
export async function finishRecovered(): Promise<void> {
    const claim = reservation;
    reservation = null;
    recovered.record = null;
    recovered.rig = null;
    recovered.confirmed = false;
    clearDraft();
    await claim?.release();
}

export async function _resetRestoreForTests(): Promise<void> {
    await reservation?.release();
    reservation = null;
    pending = false;
    recovered.record = null;
    recovered.rig = null;
    recovered.confirmed = false;
    clearDraft();
}
