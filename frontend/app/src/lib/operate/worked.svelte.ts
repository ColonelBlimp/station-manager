// Worked-before state — previous QSOs with the callsign being entered. The
// LoggingCard drives the observation (the lookup must run even while the panel
// is closed, so auto-open can fire); WorkedPanel renders the result. The lookup
// is an INJECTED seam (setHistory, wired in main.ts) — dev stub now, the
// daemon's contact-history endpoint later, this module unchanged.
//
// Fail-soft like enrichment: a failed lookup is "done with nothing" — never an
// error the operator has to deal with mid-QSO.

import { isVisible, showTile, hideTile } from './layout.svelte';
import { isValidCallsign } from '../validators/callsign';

// One previous contact, in display form. Mirrors the useful subset of the
// daemon's per-QSO record; the logbook SPA owns the full view.
export interface WorkedQso {
    // Render key: force-logged duplicates share date/time/band/mode (the
    // dedupe tuple), and Svelte 5 throws on duplicate each-keys.
    uuid: string;
    date: string; // YYYY-MM-DD (UTC)
    timeOn: string; // HH:MM
    band: string;
    mode: string;
    rstSent: string;
    rstRcvd: string;
    name: string;
    notes: string; // operator's private NOTES for that contact
}

export type WorkedStatus = 'idle' | 'pending' | 'done';

export const worked: { status: WorkedStatus; call: string; qsos: WorkedQso[] } = $state({
    status: 'idle',
    call: '',
    qsos: [],
});

export type HistoryFn = (call: string, signal?: AbortSignal) => Promise<WorkedQso[]>;
let history: HistoryFn | null = null;

export function setHistory(fn: HistoryFn): void {
    history = fn;
}

// Same settle discipline as enrich.svelte: one lookup per station, not per
// keystroke, a monotonic token so a slow lookup for the previous call can
// never overwrite the current one, and an AbortController so the superseded
// request is cancelled at the wire rather than run to completion.
const DEBOUNCE_MS = 400;
const MIN_CALL_LEN = 3;

let timer: ReturnType<typeof setTimeout> | undefined;
let seq = 0;
let inflight: AbortController | null = null;
// The call the panel was auto-opened for. '' = the operator opened (or never
// auto-opened) — auto-close must never take a panel the operator asked for.
let autoOpenedFor = '';

export function observeWorked(raw: string): void {
    const call = raw.trim().toUpperCase();
    clearTimeout(timer);

    // Same gate as enrich (2026-07-08 QA catch): the daemon 400s a call that
    // fails ITS validator (no digit, etc.) — fail-soft hid it, but malformed
    // partials shouldn't reach the wire at all.
    if (call.length < MIN_CALL_LEN || isValidCallsign(call) !== null) {
        seq++;
        inflight?.abort();
        // The auto-shown tile leaves with the callsign that summoned it (draft
        // logged or cleared); a tile the operator opened by hand stays put.
        if (isVisible('worked') && autoOpenedFor !== '' && autoOpenedFor === worked.call) {
            hideTile('worked');
        }
        autoOpenedFor = '';
        worked.status = 'idle';
        worked.call = '';
        worked.qsos = [];
        return;
    }
    if (call === worked.call && worked.status !== 'idle') return;

    timer = setTimeout(() => void lookup(call), DEBOUNCE_MS);
}

// Tab out of the callsign field = "I'm working this station": surface the
// worked-before panel if nothing is open. Marked as auto-opened for this call
// so it leaves with the station on the next contact (same auto-close path as a
// lookup-driven open); a panel the operator opened by hand is left untouched.
export function openWorkedForQso(raw: string): void {
    const call = raw.trim().toUpperCase();
    if (!isVisible('worked')) {
        showTile('worked');
        autoOpenedFor = call;
    }
}

async function lookup(call: string): Promise<void> {
    if (history === null) return; // seam not wired — stay idle

    const mine = ++seq;
    inflight?.abort();
    inflight = new AbortController();
    worked.status = 'pending';
    worked.call = call;
    worked.qsos = [];

    let rows: WorkedQso[] = [];
    try {
        rows = await history(call, inflight.signal);
    } catch {
        // fail-soft: shows as "no previous QSOs"
    }
    if (seq !== mine) return; // superseded

    worked.qsos = rows;
    worked.status = 'done';

    // Found history → surface it, but never steal the stage: only when the
    // worked tile is hidden, and at most once per call (a tile the operator
    // hid stays hidden for this station).
    if (rows.length > 0 && !isVisible('worked') && autoOpenedFor !== call) {
        showTile('worked');
        autoOpenedFor = call;
    }
}
