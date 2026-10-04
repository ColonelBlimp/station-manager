// A recovered QSO's edits are saved to ITS record (ADR 0085 RS16–RS17a; Restore
// commit 3): one write in flight, newer edits coalesced into the next, no timer.
// Clear / Escape wait for the latest edit to commit before letting the QSO go;
// Discard in the owning tab stops the writes, settles the one in flight and
// deletes before letting go; a rebind save flushes to the owned record rather
// than saving a new one. Corrected rig values are saved too, as rigCorrection —
// apart from the original reading, which is kept — and their confirmation is
// never saved: every Restore asks again (operator ruling 2026-10-02).

import { flushSync, untrack } from 'svelte';
import { clearDraft, draft, submitState, type QsoDraft } from '../operate/qso.svelte';
import { toasts } from '../ui/toasts.svelte';
import { announceDraftsChanged } from './draftChannel';
import { draftStore } from './draftStore';
import { recovered } from './recovered.svelte';
import { finishRecovered } from './restoreSession';
import type { SavedDraft } from './savedDraft';
import type { RigReading } from '../operate/rigSnapshot.svelte';

/** error: an edit was NOT stored. listError: every edit is stored, but this
 *  tab's Saved QSOs list could not be refreshed to show it — a different
 *  thing, never reported as an unsaved edit (review 2026-10-03). */
export const recoveredSave = $state({
    error: '',
    listError: '',
    clearing: false,
    discarding: false,
});
const LIST_STALE =
    'Saved, but the Saved QSOs list could not be refreshed to show it, so the QSO stays here — try Clear again.';

let trackedRecord: SavedDraft | null = null;
let lastSeen = '';
let revision = 0; // advanced by every edit
let committed = 0; // the newest revision known to be stored
let stopped = false; // a discard is under way: no new writes
let loop: Promise<boolean> | null = null;

// After each committed write THIS tab's Saved QSOs list is read again: the
// change notice reaches only the other tabs, and a stale local entry would show
// old values and fail a later Restore as "changed" (review 2026-10-03). Wired by
// savedDrafts.svelte.ts, which owns the list (it imports this module, not the
// reverse). Awaited, so a flush resolves only once the list is current.
let onWritten: () => Promise<boolean> = () => Promise.resolve(true);
export function setRecoveredWrittenListener(fn: () => Promise<boolean>): void {
    onWritten = fn;
}
let listFresh = true; // this tab's list shows the last committed write
let listRefresh: Promise<boolean> | null = null; // the newest refresh still running

// Started after each committed write and NOT awaited by the write loop: a write
// is complete when storage commits, so the rebind save never waits on the list
// (review 2026-10-03). Clear waits on it separately. Only the newest refresh
// decides the list's state.
let listToken: object | null = null; // names the newest refresh
// Waiters for "the newest refresh has finished" — whichever refresh is newest
// when it finishes, not the one that was newest when they began to wait: a
// Clear must not stay blocked on an obsolete read (review 2026-10-03).
let listWaiters: Array<() => void> = [];
function listSettled(): Promise<void> {
    if (listRefresh === null) return Promise.resolve();
    return new Promise<void>((resolve) => listWaiters.push(resolve));
}
function wakeListWaiters(): void {
    const waiters = listWaiters;
    listWaiters = [];
    for (const wake of waiters) wake();
}
function startListRefresh(): Promise<boolean> {
    listFresh = false;
    const token = {};
    listToken = token;
    const run = (async (): Promise<boolean> => {
        const ok = await onWritten();
        if (listToken === token) {
            listToken = null;
            listRefresh = null;
            listFresh = ok;
            // Only when every edit is stored: "Saved, but…" must not stand beside
            // a newer edit that is not (review 2026-10-03).
            recoveredSave.listError = ok || committed < revision ? '' : LIST_STALE;
            wakeListWaiters();
        }
        return ok;
    })();
    listRefresh = run;
    return run;
}

// The values a correction is judged by: the operator's corrected frequency,
// band, mode and submode (the literal and the provenance are display only).
const rigKey = (r: RigReading | null): string =>
    r === null ? 'none' : JSON.stringify([r.freqHz, r.band, r.adifMode, r.subMode]);

const canonical = (f: QsoDraft, rig: RigReading | null): string =>
    JSON.stringify([
        Object.keys(f)
            .sort()
            .map((k) => [k, f[k as keyof QsoDraft]]),
        rigKey(rig),
    ]);

/** The correction to store: null when the values are the original reading's. */
function correctionOf(record: SavedDraft, rig: RigReading | null): RigReading | null {
    if (rig === null || rigKey(rig) === rigKey(record.rig)) return null;
    return { ...rig };
}

function noteFields(record: SavedDraft, snapshot: QsoDraft, rig: RigReading | null): void {
    // Each installation is a new edit session, even when another tab changed
    // the same UUID since this tab last released it.
    if (record !== trackedRecord) {
        trackedRecord = record;
        listFresh = true;
        listRefresh = null;
        listToken = null;
        wakeListWaiters();
        recoveredSave.listError = '';
        lastSeen = canonical(record.fields, record.rigCorrection ?? record.rig);
        revision = committed = 0;
        stopped = false;
        recoveredSave.error = '';
    }
    const now = canonical(snapshot, rig);
    if (now === lastSeen) return;
    lastSeen = now;
    revision++;
    // "Saved, but the list…" must not stand beside an edit that is not yet saved
    // (review 2026-10-03); the refresh after this edit's write sets it again.
    recoveredSave.listError = '';
    void pump();
}

$effect.root(() => {
    $effect(() => {
        const r = recovered.record;
        if (r === null) return;
        const snapshot: QsoDraft = { ...draft }; // tracks every field
        const rig = recovered.rig === null ? null : { ...recovered.rig }; // and each correction
        untrack(() => noteFields(r, snapshot, rig));
    });
});

async function writeLoop(): Promise<boolean> {
    while (committed < revision) {
        if (stopped || recovered.record === null) return false;
        const target = revision;
        const fields: QsoDraft = { ...draft };
        const base = $state.snapshot(recovered.record);
        const rigCorrection = correctionOf(base, $state.snapshot(recovered.rig));
        const record: SavedDraft = { ...base, fields, rigCorrection };
        try {
            await draftStore().put(record);
        } catch (e) {
            recoveredSave.error = `The latest edit was not saved (${e instanceof Error ? e.message : String(e)}).`;
            return false;
        }
        committed = Math.max(committed, target);
        if (recovered.record !== null) {
            recovered.record.fields = fields;
            recovered.record.rigCorrection = rigCorrection;
        }
        recoveredSave.error = '';
        announceDraftsChanged();
        // The write is committed whatever the list does: a failed refresh is
        // recorded as a stale list, not as an unsaved edit.
        void startListRefresh();
    }
    return true;
}

function pump(): Promise<boolean> {
    if (stopped) return Promise.resolve(false);
    loop ??= writeLoop().finally(() => {
        loop = null;
    });
    return loop;
}

/** Wait until every edit made so far — including one made while waiting — is
 *  stored AND this tab's list shows it; a stale list is refreshed again here,
 *  without another edit. False when a write failed (error) or the list could
 *  not be refreshed (listError). Clear waits on this. */
export async function flushRecoveredEdits(): Promise<boolean> {
    let retried = false;
    for (;;) {
        if (!(await flushRecoveredWrites())) return false;
        if (listRefresh !== null) {
            await listSettled(); // the newest refresh decides; then check again
            continue;
        }
        if (!listFresh) {
            if (retried) return false; // still stale: the QSO stays (listError)
            retried = true;
            void startListRefresh(); // retry without another edit; the signal decides
            continue;
        }
        flushSync();
        if (committed >= revision && loop === null && listRefresh === null) return true;
    }
}

/** Wait until every edit made so far is stored — the list aside. A rebind
 *  save waits on this: the record in storage is what survives the reload. */
export async function flushRecoveredWrites(): Promise<boolean> {
    for (;;) {
        flushSync(); // an edit typed just now reaches the tracker first
        if (!(await pump())) return false;
        flushSync();
        if (committed >= revision && loop === null) return true;
    }
}

/** Clear / Escape on a recovered QSO: keep its edits, then let it go. */
export async function clearRecovered(): Promise<boolean> {
    if (recovered.record === null) {
        clearDraft();
        return true;
    }
    if (submitState.busy || recoveredSave.clearing || recoveredSave.discarding) return false;
    recoveredSave.clearing = true;
    try {
        if (!(await flushRecoveredEdits())) {
            // A stale list says so itself; never call a stored edit unsaved.
            if (recoveredSave.error === '' && recoveredSave.listError === '') {
                recoveredSave.error = 'The latest edit could not be saved, so the QSO stays here.';
            }
            return false;
        }
        await finishRecovered(); // detaches synchronously: no edit lands in between
        return true;
    } finally {
        recoveredSave.clearing = false;
    }
}

/** The card's Clear and Escape. */
export function requestClearDraft(): void {
    if (recovered.record !== null) void clearRecovered();
    else clearDraft();
}

/** Discard of the record this tab has restored (operator ruling 3): no new
 *  writes, the one in flight settled, the delete awaited — then let it go. A
 *  failure keeps the record, the form and the reservation. */
export async function discardRecovered(id: string): Promise<boolean> {
    if (submitState.busy || recoveredSave.clearing || recoveredSave.discarding) {
        toasts.error('The saved QSO cannot be discarded while it is being logged or cleared.');
        return false;
    }
    recoveredSave.discarding = true;
    stopped = true;
    try {
        await loop;
        try {
            await draftStore().remove(id);
        } catch (e) {
            stopped = false;
            // A failed Discard keeps the form, including edits typed during
            // the delete. Resume those saves without requiring another edit.
            void pump();
            const detail = e instanceof Error ? e.message : String(e);
            toasts.error(`The saved QSO could not be discarded (${detail}); it is still kept.`);
            return false;
        }
        await finishRecovered();
        announceDraftsChanged();
        return true;
    } finally {
        recoveredSave.discarding = false;
    }
}

/** Change the owned record's attempt / outcome and store it through the same
 *  write loop as the edits (Restore commit 4): every write after this one
 *  carries the change, so a late autosave cannot erase it. True once stored;
 *  on a failed write the change is undone in memory, so nothing claims what
 *  storage does not hold. */
export async function persistRecordChange(
    change: Partial<Pick<SavedDraft, 'attempt' | 'outcome'>>
): Promise<boolean> {
    const r = recovered.record;
    if (r === null || stopped) return false;
    const before = { attempt: r.attempt, outcome: r.outcome };
    Object.assign(r, change);
    revision++;
    if (await flushRecoveredWrites()) return true;
    if (recovered.record === r) Object.assign(r, before);
    return false;
}

/** Stop writing the owned record — for good, once a Log is confirmed: a later
 *  autosave would put a draft back. Settles the write in flight first. */
export async function stopRecoveredWrites(): Promise<void> {
    stopped = true;
    await loop;
}

/** The owned record as it now stands, for a rebind save. */
export function recoveredRecordNow(): SavedDraft | null {
    if (recovered.record === null) return null;
    const base = $state.snapshot(recovered.record);
    const rigCorrection = correctionOf(base, $state.snapshot(recovered.rig));
    return { ...base, fields: { ...draft }, rigCorrection };
}

export function _resetRecoveredSaveForTests(): void {
    trackedRecord = null;
    lastSeen = '';
    revision = committed = 0;
    stopped = false;
    loop = null;
    recoveredSave.error = '';
    recoveredSave.listError = '';
    listFresh = true;
    listRefresh = null;
    listToken = null;
    wakeListWaiters();
    recoveredSave.clearing = false;
    recoveredSave.discarding = false;
}
