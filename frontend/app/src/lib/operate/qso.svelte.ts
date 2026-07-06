// The in-progress QSO draft — the shared spine of the Operate surface (ADR 0045).
// The logging card writes it; the info panels read from it (Worked reads
// .callsign, Details enriches .callsign and can write name/qth/grid back, Session
// receives the QSO on log). Rig-provided fields (freq/mode/band) are NOT here —
// they live in the rig state and merge in at log time — so this stays the
// operator-entered per-QSO data only. Presentation (LoggingCard) is separate.

export interface QsoDraft {
    callsign: string;
    rstSent: string;
    rstRcvd: string;
    name: string;
    qth: string; // ADIF QTH — edited in the Details card (rarely changed), not on the logging card
    gridsquare: string; // ADIF GRIDSQUARE — filled by enrichment (Details), not entered on the card
    dateOn: string; // ADIF QSO_DATE — UTC, YYYY-MM-DD
    timeOn: string; // ADIF TIME_ON — UTC, HH:MM:SS
    dateOff: string; // ADIF QSO_DATE_OFF
    timeOff: string; // ADIF TIME_OFF
    comment: string; // ADIF COMMENT / notes
}

function blank(): QsoDraft {
    return {
        callsign: '',
        rstSent: '59',
        rstRcvd: '59',
        name: '',
        qth: '',
        gridsquare: '',
        dateOn: '',
        timeOn: '',
        dateOff: '',
        timeOff: '',
        comment: '',
    };
}

// UTC now, split into the card's display formats. `new Date()` is fine here (app
// code, not a workflow script); the daemon re-derives canonical ADIF at store time.
function nowUtc(): { date: string; time: string } {
    const iso = new Date().toISOString();
    return { date: iso.slice(0, 10), time: iso.slice(11, 19) };
}

export function stampOn(): void {
    const { date, time } = nowUtc();
    draft.dateOn = date;
    draft.timeOn = time;
}

export function stampOff(): void {
    const { date, time } = nowUtc();
    draft.dateOff = date;
    draft.timeOff = time;
}

export const draft = $state<QsoDraft>(blank());

export function resetDraft(): void {
    Object.assign(draft, blank());
}

// Frontend gate: a callsign is the minimum to log. (The CAT/rig confirm gate is
// added with the Rig panel; enrichment never gates — invariant.)
export function canLog(): boolean {
    return draft.callsign.trim() !== '';
}

// Submit seam — injected later in main.ts, mirroring the daemon's SetQsoLogger
// sink (ADR 0045 principle 3: backend coupling injected, not imported). Until
// wired, logDraft() is a no-op that just clears the draft; the card never imports
// the real submit path, so it stays relocatable + testable in isolation.
type SubmitFn = (qso: QsoDraft) => void;
let submit: SubmitFn | null = null;

export function setSubmit(fn: SubmitFn): void {
    submit = fn;
}

export function logDraft(): void {
    if (!canLog()) return;
    stampOff(); // QSO end = now (operator can override before logging)
    submit?.({ ...draft });
    resetDraft();
    stampOn(); // next QSO starts now
}
