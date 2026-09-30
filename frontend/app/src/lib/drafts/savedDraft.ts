// A Phone / CW draft saved across an archive switch (ADR 0085). It keeps its
// ORIGINAL archive and logbook identity — UUIDs, since local integer ids
// collide across archives — and the rig reading it would have been logged
// with. It is never logged automatically; its outcome says whether an earlier
// log attempt may already have stored it.

import type { QsoDraft } from '../operate/qso.svelte';
import type { RigReading } from '../operate/rigSnapshot.svelte';

export interface SavedDraft {
    version: 1;
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

/** Shape check for a record read back from browser storage: anything else
 *  (an older or damaged record) is not shown as a saved QSO. */
export function isSavedDraft(v: unknown): v is SavedDraft {
    if (!isObj(v) || v.version !== 1) return false;
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
    const r = v.rig;
    return (
        isObj(r) &&
        (r.freqHz === null || typeof r.freqHz === 'number') &&
        [r.band, r.mode, r.adifMode, r.subMode, r.capturedAt].every(isStr) &&
        (r.basis === 'before-drop' || r.basis === 'when-saved')
    );
}

function utc(iso: string): string {
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
            ? `last reading before the connection dropped, ${utc(r.rig.capturedAt)}`
            : state === 'saved'
              ? `reading when saved, ${utc(r.rig.capturedAt)}`
              : `reading at the attempted save, ${utc(r.rig.capturedAt)}`;
    const rows: Array<[string, string]> = [
        ['Archive', r.archiveLabel],
        ['Logbook', r.logbookName],
        ['Station callsign', r.stationCallsign],
        ['Operator', r.operator],
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
        ['Name', f.name],
        ['QTH', f.qth],
        ['Grid', f.gridsquare],
        ['Comment', f.comment],
        ['Their rig', f.rig],
        ['Notes', f.notes],
        ['RX power', f.rxPwr],
        ['Saved', state === 'saved' ? utc(r.savedAt) : ''],
    ];
    return rows.filter(([, v]) => v !== '');
}

/** Plain text for the clipboard: the headline, then every row. */
export function savedDraftText(r: SavedDraft, state: SaveState = 'saved'): string {
    const rows = savedDraftLines(r, state).map(([k, v]) => `${k}: ${v}`);
    return [savedDraftHeadline(r, state), ...rows].join('\n');
}
