// Logging a recovered QSO (ADR 0085 RS18–RS25; Restore commit 4). It never goes
// through the ordinary Log assembly (live rig, today's station): the request is
// built from the recovered record, its corrected rig values and its original
// attribution, and the daemon refuses it unless it would store exactly that
// attribution (expect_*).
//
//   - Gates: archive gate, source archive and logbook UUID, ownership, confirmed
//     rig values, validation, the in-flight latch, attribution present — NOT the
//     CAT link. Checked before, and again after every wait.
//   - Before the POST the EXACT request is stored as the record's attempt with
//     outcome "unknown"; a failed store sends nothing.
//   - Confirmed: the UUID is terminal in this tab; the record is deleted, else
//     marked logged; if both fail the UUID is shown and only cleanup is retried —
//     never the submit. Cleanups run one at a time, each for the QSO it was
//     asked for, and let the form go only while this tab still holds that QSO.
//   - Ambiguous (transport failure, malformed success, abort): the attempt
//     stays, the outcome is unknown, nothing is retried.
//   - Definite refusal (validation, attribution_changed, duplicate): this
//     request stored nothing, so the earlier state returns — but an EARLIER
//     unknown outcome is never erased.

import type { SubmitAttribution } from '../api/submit-attribution';
import type { SubmitOutcome } from '../api/qso';
import { draft, draftProblems, submitState } from '../operate/qso.svelte';
import { toasts } from '../ui/toasts.svelte';
import { announceDraftsChanged } from './draftChannel';
import { draftStore } from './draftStore';
import { recovered, recoveredRigProblem } from './recovered.svelte';
import { buildRecoveredAdif, type RecoveredExtras } from './recoveredRequest';
import {
    flushRecoveredWrites,
    persistRecordChange,
    recoveredSave,
    stopRecoveredWrites,
} from './recoveredSave.svelte';
import { finishRecovered, ownsRecoveredRecord } from './restoreSession';
import { refreshSavedDraftsNow } from './savedDrafts.svelte';
import type { AttemptedSubmission, SavedDraft } from './savedDraft';

export const UNKNOWN_CONFIRM =
    'I checked the original Logbook; this contact is not already logged.';

export interface RecoveredSendOptions {
    force: boolean;
    expect: SubmitAttribution;
}
type Sender = (
    adif: string,
    logbookId: number,
    opts: RecoveredSendOptions
) => Promise<SubmitOutcome>;
export interface RecoveredSubmitEnv {
    /** archiveSwitchGate(): why archive-scoped operations are refused, or null. */
    switchGate: string | null;
    bootArchiveId: string | null;
    activeLogbookUuid: string;
    /** The active default logbook's numeric id — the ?logbook= of the request. */
    activeLogbookId: number;
}

let send: Sender | null = null;
let readEnv: () => RecoveredSubmitEnv = () => ({
    switchGate: 'Recovered logging is not wired.',
    bootArchiveId: null,
    activeLogbookUuid: '',
    activeLogbookId: 0,
});
// Enrichment for the call; the bearing is from the record's own grid, not today's.
let extrasFor: (call: string, myGrid: string) => RecoveredExtras = () => ({});

export function setRecoveredSender(fn: Sender): void {
    send = fn;
}
export function setRecoveredSubmitEnv(fn: () => RecoveredSubmitEnv): void {
    readEnv = fn;
}
export function setRecoveredExtras(fn: (call: string, myGrid: string) => RecoveredExtras): void {
    extrasFor = fn;
}

// When the attempt was recorded (a plain value, not reactive state).
function isoNow(): string {
    return new Date().toISOString();
}

export const recoveredSubmit = $state({
    busy: false,
    /** The recovered QSO (its installed record) the fields below describe; a
     *  later install — even of the same saved record — is another QSO. */
    subject: null as SavedDraft | null,
    /** The QSO UUID a confirmed Log returned: terminal in this tab. */
    confirmedUuid: '',
    cleanupError: '',
    /** The existing QSO a duplicate answer named. */
    duplicateUuid: '',
    /** A definite refusal's message. */
    refusal: '',
});

const CLEAN = { confirmedUuid: '', cleanupError: '', duplicateUuid: '', refusal: '' };

/** The Log's messages and confirmed UUID, for the QSO on the form only. */
export function recoveredSubmitView(): typeof CLEAN {
    const mine = recovered.record !== null && recoveredSubmit.subject === recovered.record;
    return mine ? recoveredSubmit : CLEAN;
}

/** Why a recovered Log cannot be sent now, or null. */
export function recoveredLogBlock(): string | null {
    return blockReason(false);
}

// `inFlight`: the caller is the Log holding the latch, rechecking after a wait.
function blockReason(inFlight: boolean): string | null {
    const r = recovered.record;
    if (r === null) return 'No recovered QSO is on the form.';
    const confirmed = recoveredSubmitView().confirmedUuid;
    if (confirmed !== '') return `Already logged as QSO ${confirmed}.`;
    if (!inFlight && (recoveredSubmit.busy || submitState.busy)) return 'Logging is in progress.';
    if (recoveredSave.clearing || recoveredSave.discarding) return 'The QSO is being cleared.';
    if (r.attribution === null) return 'Original attribution unavailable.';
    const env = readEnv();
    if (env.switchGate !== null) return env.switchGate;
    if (env.bootArchiveId !== r.archiveId || env.activeLogbookUuid !== r.logbookUuid) {
        return `It belongs to ‘${r.archiveLabel}’ (logbook ‘${r.logbookName}’), not the archive and logbook in use here.`;
    }
    if (!ownsRecoveredRecord(r.id)) return 'This tab no longer holds the recovered QSO.';
    if (!recovered.confirmed || recoveredRigProblem() !== null) {
        return 'Confirm or correct the recovered rig values first.';
    }
    if (draft.callsign.trim() === '' || draft.dateOn === '' || draft.timeOn === '') {
        return 'A callsign, date and time on are needed.';
    }
    if (Object.values(draftProblems()).some(Boolean)) return 'Correct the highlighted fields.';
    return null;
}

export type RecoveredLogResult =
    'stored' | 'duplicate' | 'refused' | 'unknown' | 'blocked' | 'cancelled';

const AMBIGUOUS = new Set(['network', 'server', 'aborted']);

/** Log the recovered QSO. `force` is the explicit "separate contact" choice
 *  after a duplicate; `confirm` answers UNKNOWN_CONFIRM for a record whose
 *  logging outcome is unknown (window.confirm in the app). */
export async function logRecovered(opts: {
    force?: boolean;
    confirm?: (text: string) => boolean;
}): Promise<RecoveredLogResult> {
    const block = recoveredLogBlock();
    if (block !== null) {
        if (recovered.record !== null) adopt(recovered.record);
        recoveredSubmit.refusal = block;
        return 'blocked';
    }
    const record = recovered.record as SavedDraft;
    adopt(record);
    const confirm = opts.confirm ?? ((t: string) => window.confirm(t));
    // One explicit confirmation authorises ONE attempt; the earlier uncertainty stays recorded.
    if (record.outcome === 'unknown' && !confirm(UNKNOWN_CONFIRM)) return 'cancelled';

    recoveredSubmit.busy = true;
    submitState.busy = true; // Clear / Discard stand down while the request is out
    recoveredSubmit.refusal = '';
    recoveredSubmit.duplicateUuid = '';
    try {
        if (!(await flushRecoveredWrites())) {
            recoveredSubmit.refusal = 'The latest edit is not saved, so nothing was sent.';
            return 'refused';
        }
        const after = blockReason(true);
        if (after !== null) {
            recoveredSubmit.refusal = after;
            return 'blocked';
        }
        const env = readEnv();
        const call = draft.callsign.trim().toUpperCase();
        const rig = $state.snapshot(recovered.rig)!;
        const attempt: AttemptedSubmission = {
            at: isoNow(),
            archiveId: record.archiveId,
            logbookUuid: record.logbookUuid,
            logbookId: env.activeLogbookId,
            force: opts.force === true,
            adif: buildRecoveredAdif(record, { ...draft }, rig, extrasFor(call, record.myGrid)),
            expect: { ...record.attribution! },
        };
        const before = { attempt: record.attempt, outcome: record.outcome };
        if (!(await persistRecordChange({ attempt, outcome: 'unknown' }))) {
            recoveredSubmit.refusal = 'The attempt could not be recorded, so nothing was sent.';
            return 'refused';
        }
        const late = blockReason(true);
        if (late !== null) {
            // Nothing was sent: withdraw the attempt (best effort; an earlier
            // unknown outcome is restored as it was).
            void persistRecordChange(before);
            recoveredSubmit.refusal = late;
            return 'blocked';
        }
        if (send === null) {
            void persistRecordChange(before);
            recoveredSubmit.refusal = 'Recovered logging is not wired.';
            return 'blocked';
        }
        const out = await send(attempt.adif, attempt.logbookId, {
            force: attempt.force,
            expect: attempt.expect,
        });
        return await settle(out, before, record);
    } finally {
        recoveredSubmit.busy = false;
        // A Retry cleanup queued meanwhile still holds the latch (review 2026-10-04).
        submitState.busy = cleanups > 0;
    }
}

async function settle(
    out: SubmitOutcome,
    before: Pick<SavedDraft, 'attempt' | 'outcome'>,
    record: SavedDraft
): Promise<RecoveredLogResult> {
    if (out.kind === 'stored') {
        recoveredSubmit.confirmedUuid = out.uuid;
        submitState.uncertain = false;
        toasts.info(`Logged as QSO ${out.uuid}.`);
        await cleanupConfirmed(record, out.uuid);
        return 'stored';
    }
    if (AMBIGUOUS.has(out.kind)) {
        // The stored attempt stands: the QSO may be in the log. Never retried.
        submitState.uncertain = true;
        recoveredSubmit.refusal = `The logging outcome is unknown (${'message' in out ? out.message : out.kind}). Check the Logbook before logging it again.`;
        return 'unknown';
    }
    // Definite: this request stored nothing. Restore what was there before it —
    // an earlier unknown outcome (and its attempt) included, never erased.
    void persistRecordChange(before);
    if (out.kind === 'duplicate') {
        recoveredSubmit.duplicateUuid = out.uuid;
        return 'duplicate';
    }
    recoveredSubmit.refusal = 'message' in out ? out.message : 'Refused.';
    return 'refused';
}

// One cleanup at a time (review 2026-10-04): overlapping retries each let go of
// the form, and a later one would empty the contact begun after the first.
// While any runs, the Log's latch stays held, so Clear and Discard stand down
// as they do for the request: a Discard during the "logged" write would have
// it bring the discarded record back.
let cleanupChain: Promise<unknown> = Promise.resolve();
let cleanups = 0;

function cleanupConfirmed(record: SavedDraft, uuid: string): Promise<boolean> {
    cleanups++;
    submitState.busy = true;
    const run = cleanupChain
        .then(() => cleanUp(record, uuid))
        .finally(() => {
            if (--cleanups === 0 && !recoveredSubmit.busy) submitState.busy = false;
        });
    cleanupChain = run.catch(() => undefined);
    return run;
}

// The form still holds this QSO under this tab's reservation. Once it was let
// go (an earlier cleanup) the form may hold the next contact.
const stillHeld = (record: SavedDraft): boolean =>
    recovered.record === record && ownsRecoveredRecord(record.id);

async function cleanUp(record: SavedDraft, uuid: string): Promise<boolean> {
    if (!stillHeld(record)) return true; // already let go: nothing left to clean here
    await stopRecoveredWrites(); // a later autosave would put a draft back
    try {
        await draftStore().remove(record.id);
    } catch {
        try {
            await draftStore().put({
                ...$state.snapshot(record),
                fields: { ...draft },
                state: 'logged',
                loggedQsoUuid: uuid,
            });
        } catch (e) {
            recoveredSubmit.cleanupError = `Logged, but this browser’s saved copy could not be removed (${e instanceof Error ? e.message : String(e)}).`;
            return false;
        }
    }
    recoveredSubmit.cleanupError = '';
    recoveredSubmit.confirmedUuid = '';
    // Defensive: the latch keeps every other let-go path out while this runs.
    if (stillHeld(record)) await finishRecovered();
    announceDraftsChanged();
    // This tab's own list too — the notice reaches only the others (review
    // 2026-10-04). Not awaited: the cleanup is complete once storage commits.
    void refreshSavedDraftsNow();
    return true;
}

// Make the messages this QSO's, dropping any left by a QSO let go before it.
function adopt(record: SavedDraft): void {
    if (recoveredSubmit.subject === record) return;
    recoveredSubmit.subject = record;
    recoveredSubmit.confirmedUuid = '';
    recoveredSubmit.cleanupError = '';
    recoveredSubmit.duplicateUuid = '';
    recoveredSubmit.refusal = '';
}

/** Retry only the cleanup of a confirmed Log — never the submit. */
export function retryRecoveredCleanup(): Promise<boolean> {
    const uuid = recoveredSubmitView().confirmedUuid;
    if (recovered.record === null || uuid === '') return Promise.resolve(false);
    return cleanupConfirmed(recovered.record, uuid);
}

export function _resetRecoveredSubmitForTests(): void {
    send = null;
    extrasFor = () => ({});
    cleanupChain = Promise.resolve();
    cleanups = 0;
    recoveredSubmit.busy = false;
    recoveredSubmit.subject = null;
    recoveredSubmit.confirmedUuid = '';
    recoveredSubmit.cleanupError = '';
    recoveredSubmit.duplicateUuid = '';
    recoveredSubmit.refusal = '';
}
