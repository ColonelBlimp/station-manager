// A Phone / CW draft saved across an archive switch (ADR 0085). It keeps its
// ORIGINAL archive and logbook identity — UUIDs, since local integer ids
// collide across archives — and the rig reading it would have been logged
// with. It is never logged automatically; its outcome says whether an earlier
// log attempt may already have stored it.

import type { QsoDraft } from '../operate/qso.svelte';
import type { RigReading } from '../operate/rigSnapshot.svelte';
import type { SubmitAttribution } from '../api/submit-attribution';

/** The exact submission a recovered Log sent or was about to send (ADR 0085
 *  RS19): persisted before the request, kept through later edits. */
export interface AttemptedSubmission {
    at: string;
    /** The destination exactly as sent: the numeric id in ?logbook=, and the
     *  archive and logbook UUIDs it stood for. */
    archiveId: string;
    logbookUuid: string;
    logbookId: number;
    force: boolean;
    adif: string;
    expect: SubmitAttribution;
}

/** Version 3. A version 1 record (ADR 0085 slice 1) is read as this with its
 *  attribution MISSING and its outcome kept (RS2); a version 2 record with no
 *  rig correction. */
export interface SavedDraft {
    version: 3;
    id: string;
    savedAt: string;
    archiveId: string;
    archiveLabel: string;
    logbookUuid: string;
    logbookId: number;
    logbookName: string;
    stationCallsign: string;
    operator: string;
    myGrid: string;
    fields: QsoDraft;
    rig: RigReading;
    /** unknown: a log attempt's outcome is unknown — the QSO may be logged. */
    outcome: 'unlogged' | 'unknown';
    /** What the original submit would have been stored with; null = MISSING
     *  (a failed read, or a record saved before attribution was recorded). */
    attribution: SubmitAttribution | null;
    /** logged: a recovered Log stored it (loggedQsoUuid) but this browser copy
     *  could not be removed. */
    state: 'draft' | 'logged';
    loggedQsoUuid: string;
    attempt: AttemptedSubmission | null;
    /** The operator's corrected rig values (Restore), kept apart from `rig`, the
     *  original reading; null when nothing was corrected. A correction is never
     *  confirmed in storage: every Restore asks again. */
    rigCorrection: RigReading | null;
}

const FIELD_KEYS: Array<keyof QsoDraft> = [
    'callsign',
    'rstSent',
    'rstRcvd',
    'name',
    'qth',
    'gridsquare',
    'dateOn',
    'timeOn',
    'dateOff',
    'timeOff',
    'comment',
    'rig',
    'notes',
    'rxPwr',
];

const isObj = (v: unknown): v is Record<string, unknown> =>
    typeof v === 'object' && v !== null && !Array.isArray(v);
const isStr = (v: unknown): v is string => typeof v === 'string';

function isAttribution(v: unknown): v is SubmitAttribution {
    return isObj(v) && isStr(v.myRig) && isStr(v.operator) && isStr(v.myName);
}

function isAttempt(v: unknown): v is AttemptedSubmission {
    return (
        isObj(v) &&
        isStr(v.at) &&
        isStr(v.archiveId) &&
        isStr(v.logbookUuid) &&
        typeof v.logbookId === 'number' &&
        typeof v.force === 'boolean' &&
        isStr(v.adif) &&
        isAttribution(v.expect)
    );
}

/** The fields every version shares. */
function isCommon(v: Record<string, unknown>): boolean {
    const strs = [
        v.id,
        v.savedAt,
        v.archiveId,
        v.archiveLabel,
        v.logbookUuid,
        v.logbookName,
        v.stationCallsign,
        v.operator,
        v.myGrid,
    ];
    if (!strs.every(isStr) || typeof v.logbookId !== 'number') return false;
    if (v.outcome !== 'unlogged' && v.outcome !== 'unknown') return false;
    const f = v.fields;
    if (!isObj(f) || !FIELD_KEYS.every((k) => isStr(f[k]))) return false;
    return isRigReading(v.rig);
}

function isRigReading(r: unknown): r is RigReading {
    return (
        isObj(r) &&
        (r.freqHz === null || typeof r.freqHz === 'number') &&
        [r.band, r.mode, r.adifMode, r.subMode, r.capturedAt].every(isStr) &&
        (r.basis === 'before-drop' || r.basis === 'when-saved')
    );
}

/** A record read back from browser storage, as version 3 — or null for an
 *  unknown or damaged one, which is not shown as a saved QSO. A version 1
 *  record keeps its outcome (an unknown one stays unknown) and gets no
 *  attribution: none was recorded, and none is invented (RS1–RS2). */
export function readSavedDraft(v: unknown): SavedDraft | null {
    if (!isObj(v) || !isCommon(v)) return null;
    if (v.version === 1) {
        return {
            ...(v as unknown as Omit<
                SavedDraft,
                'version' | 'attribution' | 'state' | 'loggedQsoUuid' | 'attempt' | 'rigCorrection'
            >),
            version: 3,
            attribution: null,
            state: 'draft',
            loggedQsoUuid: '',
            attempt: null,
            rigCorrection: null,
        };
    }
    if (v.version === 2) {
        const upgraded = readSavedDraft({ ...v, version: 3, rigCorrection: null });
        return upgraded;
    }
    if (v.version !== 3) return null;
    if (v.rigCorrection !== null && !isRigReading(v.rigCorrection)) return null;
    if (v.attribution !== null && !isAttribution(v.attribution)) return null;
    if (v.attempt !== null && !isAttempt(v.attempt)) return null;
    if (v.state === 'draft') {
        if (v.loggedQsoUuid !== '') return null;
    } else if (v.state !== 'logged' || !isStr(v.loggedQsoUuid) || v.loggedQsoUuid === '') {
        return null;
    }
    return v as unknown as SavedDraft;
}

/** An ISO instant as `YYYY-MM-DD HH:MM:SS UTC` (the iso string when unparseable). */
export function formatUtc(iso: string): string {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return `${d.toISOString().slice(0, 10)} ${d.toISOString().slice(11, 19)} UTC`;
}

/** saved: the record is in browser storage; unsaved: the save failed and the
 *  record exists only on the held page (review 2026-09-30: never claim a save
 *  that did not happen). */
export type SaveState = 'saved' | 'unsaved';

/** The one-line statement of what the record is (operator wording 2026-09-30).
 *  An unknown logging outcome is never paired with "not logged". */
export function savedDraftHeadline(r: SavedDraft, state: SaveState = 'saved'): string {
    const label = r.archiveLabel;
    const from = `‘${label}’`;
    const unknown = r.outcome === 'unknown';
    if (r.state === 'logged') {
        return `Logged as QSO ${r.loggedQsoUuid} — this browser’s saved copy could not be removed.`;
    }
    if (state === 'saved') {
        return unknown
            ? `QSO draft saved from ${from} — logging outcome unknown. Check the Logbook in ${from} before logging it.`
            : `Unlogged QSO saved from ${from} — not logged.`;
    }
    const origin = label === '' ? '' : ` from ${from}`;
    const logbook = label === '' ? 'the Logbook' : `the Logbook in ${from}`;
    return unknown
        ? `QSO draft${origin} — not saved. Logging outcome unknown. Check ${logbook} before logging it.`
        : `Unlogged QSO${origin} — not saved, and not logged.`;
}

/** Callsign and QSO time, for telling several saved records apart. */
export function savedDraftSummary(r: SavedDraft): string {
    const call = r.fields.callsign.trim().toUpperCase() || '(no callsign)';
    const when = [r.fields.dateOn, r.fields.timeOn].filter((s) => s !== '').join(' ');
    return when === '' ? call : `${call} · ${when} UTC`;
}

/** Every saved value as label/value rows. The rig values say which reading
 *  they are; a missing one reads "unknown", never a default. */
export function savedDraftLines(
    r: SavedDraft,
    state: SaveState = 'saved'
): Array<[string, string]> {
    const f = r.fields;
    const unknown = (s: string): string => (s === '' ? 'unknown' : s);
    const mode =
        r.rig.adifMode === ''
            ? 'unknown'
            : r.rig.subMode === ''
              ? r.rig.adifMode
              : `${r.rig.adifMode} / ${r.rig.subMode}`;
    const reading =
        r.rig.basis === 'before-drop'
            ? `last reading before the connection dropped, ${formatUtc(r.rig.capturedAt)}`
            : state === 'saved'
              ? `reading when saved, ${formatUtc(r.rig.capturedAt)}`
              : `reading at the attempted save, ${formatUtc(r.rig.capturedAt)}`;
    const rows: Array<[string, string]> = [
        ['Archive', r.archiveLabel],
        ['Logbook', r.logbookName],
        ['Station callsign', r.stationCallsign],
        ...attributionRows(r),
        ['My grid', r.myGrid],
        ['Callsign', f.callsign.trim().toUpperCase()],
        ['Date on', f.dateOn],
        ['Time on', f.timeOn],
        ['Date off', f.dateOff],
        ['Time off', f.timeOff],
        ['RST sent', f.rstSent],
        ['RST rcvd', f.rstRcvd],
        ['Frequency', r.rig.freqHz === null ? 'unknown' : `${(r.rig.freqHz / 1e6).toFixed(6)} MHz`],
        ['Band', unknown(r.rig.band)],
        ['Mode', mode],
        ['Rig values', reading],
        ['Corrected rig values', correctionText(r.rigCorrection)],
        ['Name', f.name],
        ['QTH', f.qth],
        ['Grid', f.gridsquare],
        ['Comment', f.comment],
        ['Their rig', f.rig],
        ['Notes', f.notes],
        ['RX power', f.rxPwr],
        ['Saved', state === 'saved' ? formatUtc(r.savedAt) : ''],
    ];
    return rows.filter(([, v]) => v !== '');
}

// The corrected values beside the original reading (which the rows above keep).
function correctionText(c: RigReading | null): string {
    if (c === null) return '';
    const freq = c.freqHz === null ? 'unknown' : `${(c.freqHz / 1e6).toFixed(6)} MHz`;
    const mode =
        c.adifMode === ''
            ? 'unknown'
            : c.subMode === ''
              ? c.adifMode
              : `${c.adifMode} / ${c.subMode}`;
    return `${freq} · ${c.band === '' ? 'unknown' : c.band} · ${mode}`;
}

// What the original submit would have been stored with. A known-empty value is
// "(none)"; missing attribution is said, never filled in.
function attributionRows(r: SavedDraft): Array<[string, string]> {
    const a = r.attribution;
    if (a === null) {
        return [
            ['Operator', r.operator],
            ['Attribution', 'Original attribution unavailable'],
        ];
    }
    const shown = (s: string): string => (s === '' ? '(none)' : s);
    return [
        ['Operator', shown(a.operator)],
        ['Operator name', shown(a.myName)],
        ['My rig', shown(a.myRig)],
    ];
}

/** Plain text for the clipboard: the headline, then every row. */
export function savedDraftText(r: SavedDraft, state: SaveState = 'saved'): string {
    const rows = savedDraftLines(r, state).map(([k, v]) => `${k}: ${v}`);
    return [savedDraftHeadline(r, state), ...rows].join('\n');
}
