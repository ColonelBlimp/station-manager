// Save the Phone / CW draft before a rebind reload (ADR 0085, rule 3). One
// record id per page: repeated reload callbacks and Retry save update the same
// record, and overlapping calls share one write, so a QSO is never saved twice.
// Without a proven source — the archive this page booted on and its default
// logbook's UUID — nothing is written: the caller holds the reload.

import { draft, draftInProgress, logOutcomeUnknown, type QsoDraft } from '../operate/qso.svelte';
import { rigReadingForSave } from '../operate/rigSnapshot.svelte';
import { announceDraftsChanged } from './draftChannel';
import { draftStore } from './draftStore';
import { rememberPreservedForAnnouncement } from './savedDrafts.svelte';
import { flushRecoveredWrites, recoveredRecordNow } from './recoveredSave.svelte';
import type { SavedDraft } from './savedDraft';
import type { SubmitAttribution } from '../api/submit-attribution';

export interface DraftSource {
    archiveId: string;
    archiveLabel: string;
}

export interface StationSource {
    logbookUuid: string;
    logbookId: number;
    logbookName: string;
    stationCallsign: string;
    operator: string;
    myGrid: string;
    /** From GET /v1/submit-attribution inside the boot identity bracket; null
     *  when that read failed — recorded as missing, never guessed (RS1). */
    attribution: SubmitAttribution | null;
}

export type PreserveResult =
    | { kind: 'none' }
    | { kind: 'saved'; record: SavedDraft }
    | { kind: 'failed'; record: SavedDraft; reason: string };

let recordId = '';
let inFlight: Promise<PreserveResult> | null = null;

function mintId(): string {
    const c = globalThis.crypto;
    if (c && typeof c.randomUUID === 'function') return c.randomUUID();
    return `draft-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function build(archive: DraftSource | null, station: StationSource): SavedDraft {
    if (recordId === '') recordId = mintId();
    const now = new Date();
    const fields: QsoDraft = { ...draft };
    return {
        version: 3,
        id: recordId,
        savedAt: now.toISOString(),
        archiveId: archive?.archiveId ?? '',
        archiveLabel: archive?.archiveLabel ?? '',
        ...station,
        fields,
        rig: rigReadingForSave(now.getTime()),
        outcome: logOutcomeUnknown() ? 'unknown' : 'unlogged',
        state: 'draft',
        loggedQsoUuid: '',
        attempt: null,
        rigCorrection: null,
    };
}

async function run(archive: DraftSource | null, station: StationSource): Promise<PreserveResult> {
    // A recovered QSO already has its record — with its ORIGINAL archive,
    // logbook, attribution and rig reading. Flush the latest edit to it; never
    // save it again as a new record with today's values.
    const owned = recoveredRecordNow();
    if (owned !== null) {
        if (!(await flushRecoveredWrites())) {
            return {
                kind: 'failed',
                record: owned,
                reason: 'The recovered QSO’s latest edit was not saved.',
            };
        }
        const record = recoveredRecordNow() ?? owned;
        announceDraftsChanged();
        rememberPreservedForAnnouncement(record);
        return { kind: 'saved', record };
    }
    if (!draftInProgress()) return { kind: 'none' };
    const record = build(archive, station);
    if (record.archiveId === '' || record.logbookUuid === '') {
        return {
            kind: 'failed',
            record,
            reason: 'The QSO’s archive or logbook could not be proven, so it was not saved.',
        };
    }
    try {
        await draftStore().put(record);
    } catch (e) {
        const detail = e instanceof Error ? e.message : String(e);
        return { kind: 'failed', record, reason: `Browser storage did not keep it (${detail}).` };
    }
    announceDraftsChanged(); // committed: the other tabs re-read
    rememberPreservedForAnnouncement(record); // announced once after this tab's reload
    return { kind: 'saved', record };
}

/** Save the current draft, if there is one, with its original source. */
export function preserveDraft(
    archive: DraftSource | null,
    station: StationSource
): Promise<PreserveResult> {
    inFlight ??= run(archive, station).finally(() => {
        inFlight = null;
    });
    return inFlight;
}

export function _resetPreserveForTests(): void {
    recordId = '';
    inFlight = null;
}
