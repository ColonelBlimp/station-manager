// Worked-before state — previous QSOs with the callsign being entered. The
// LoggingCard drives the observation (the lookup must run even while the panel
// is closed, so auto-open can fire); WorkedPanel renders the result. The lookup
// is an INJECTED seam (setHistory, wired in main.ts) — dev stub now, the
// daemon's contact-history endpoint later, this module unchanged.
//
// Fail-soft like enrichment: a failed lookup is "done with nothing" — never an
// error the operator has to deal with mid-QSO.

import { operate } from './state.svelte';

// One previous contact, in display form. Mirrors the useful subset of the
// daemon's per-QSO record; the logbook SPA owns the full view.
export interface WorkedQso {
    date: string; // YYYY-MM-DD (UTC)
    timeOn: string; // HH:MM
    band: string;
    mode: string;
    rstSent: string;
    rstRcvd: string;
    name: string;
}

export type WorkedStatus = 'idle' | 'pending' | 'done';

export const worked: { status: WorkedStatus; call: string; qsos: WorkedQso[] } = $state({
    status: 'idle',
    call: '',
    qsos: [],
});

export type HistoryFn = (call: string) => Promise<WorkedQso[]>;
let history: HistoryFn | null = null;

export function setHistory(fn: HistoryFn): void {
    history = fn;
}

// Same settle discipline as enrich.svelte: one lookup per station, not per
// keystroke, and a monotonic token so a slow lookup for the previous call can
// never overwrite the current one.
const DEBOUNCE_MS = 400;
const MIN_CALL_LEN = 3;

let timer: ReturnType<typeof setTimeout> | undefined;
let seq = 0;
// The call the panel was auto-opened for. '' = the operator opened (or never
// auto-opened) — auto-close must never take a panel the operator asked for.
let autoOpenedFor = '';

export function observeWorked(raw: string): void {
    const call = raw.trim().toUpperCase();
    clearTimeout(timer);

    if (call.length < MIN_CALL_LEN) {
        seq++;
        // The auto-opened panel leaves with the callsign that summoned it
        // (draft logged or cleared); a manually-opened panel stays put.
        if (operate.panel === 'worked' && autoOpenedFor !== '' && autoOpenedFor === worked.call) {
            operate.panel = null;
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

async function lookup(call: string): Promise<void> {
    if (history === null) return; // seam not wired — stay idle

    const mine = ++seq;
    worked.status = 'pending';
    worked.call = call;
    worked.qsos = [];

    let rows: WorkedQso[] = [];
    try {
        rows = await history(call);
    } catch {
        // fail-soft: shows as "no previous QSOs"
    }
    if (seq !== mine) return; // superseded

    worked.qsos = rows;
    worked.status = 'done';

    // Found history → surface it, but never steal the stage: only when no
    // panel is open, and at most once per call (a re-open the operator closed
    // stays closed for this station).
    if (rows.length > 0 && operate.panel === null && autoOpenedFor !== call) {
        operate.panel = 'worked';
        autoOpenedFor = call;
    }
}
