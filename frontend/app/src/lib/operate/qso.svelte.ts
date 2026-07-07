// The in-progress QSO draft — the shared spine of the Operate surface (ADR 0045).
// The logging card writes it; the info panels read from it (Worked reads
// .callsign, Details enriches .callsign and can write name/qth/grid back, Session
// receives the QSO on log). Rig-provided fields (freq/mode/band) are NOT here —
// they live in the rig state and merge in at log time — so this stays the
// operator-entered per-QSO data only. Presentation (LoggingCard) is separate.

import { isValidCallsign } from '../validators/callsign';
import { isValidRst } from '../validators/rst';

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

// --- QSO clock -------------------------------------------------------------
// The QSO starts when the operator COMMITS to a callsign (Tab out of the
// field), not when the card appears: that stamps Date/Time On and starts the
// off-time ticking (the running Time Off IS the QSO timer). Ticking stops the
// moment the operator hand-edits an off field — their correction must survive
// — while `started` stays true so re-Tabbing after a typo fix can't reset
// TIME_ON.

let ticker: ReturnType<typeof setInterval> | undefined;
export const qsoClock = $state({ started: false, ticking: false });

function tickOff(): void {
    const { date, time } = nowUtc();
    draft.dateOff = date;
    draft.timeOff = time;
}

export function startQso(): void {
    if (qsoClock.started) return; // typo-fix re-Tab: the QSO already began
    qsoClock.started = true;
    qsoClock.ticking = true;
    stampOn();
    tickOff();
    ticker = setInterval(tickOff, 1_000);
}

/** Operator edited Date/Time Off by hand — stop ticking over their value. */
export function holdOffTimes(): void {
    if (!qsoClock.ticking) return;
    clearInterval(ticker);
    qsoClock.ticking = false;
}

function resetClock(): void {
    clearInterval(ticker);
    qsoClock.started = false;
    qsoClock.ticking = false;
}

// Fill-if-empty: a manually entered off date/time (backlogging, correcting an
// end time) must survive submit — only blank fields get "now".
export function stampOff(): void {
    const { date, time } = nowUtc();
    if (draft.dateOff === '') draft.dateOff = date;
    if (draft.timeOff === '') draft.timeOff = time;
}

export const draft = $state<QsoDraft>(blank());

export function resetDraft(): void {
    Object.assign(draft, blank());
}

// The operator-facing clear: blank the draft and disarm the QSO clock. The
// next QSO's times stamp when its callsign is committed (Tab) — until then
// canLog() blocks on the empty date/time, so a blank start can never log.
export function clearDraft(): void {
    resetDraft();
    resetClock();
    submitState.error = '';
    submitState.duplicate = false;
    submitState.logged = '';
}

// Format checks for the card's free-text date/time fields (what the ADIF
// serializer + daemon accept). Empty is valid here — presence is canLog's
// concern, and dateOff/timeOff are legitimately blank until log.
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
const TIME_RE = /^\d{2}:\d{2}(:\d{2})?$/;

export interface DraftProblems {
    callsign: boolean;
    rstSent: boolean;
    rstRcvd: boolean;
    dateOn: boolean;
    timeOn: boolean;
    dateOff: boolean;
    timeOff: boolean;
}

/** Per-field validity (true = malformed) for the card's red outlines. */
export function draftProblems(): DraftProblems {
    const bad = (re: RegExp, v: string): boolean => v !== '' && !re.test(v.trim());
    return {
        callsign: isValidCallsign(draft.callsign) !== null,
        rstSent: isValidRst(draft.rstSent) !== null,
        rstRcvd: isValidRst(draft.rstRcvd) !== null,
        dateOn: bad(DATE_RE, draft.dateOn),
        timeOn: bad(TIME_RE, draft.timeOn),
        dateOff: bad(DATE_RE, draft.dateOff),
        timeOff: bad(TIME_RE, draft.timeOff),
    };
}

// Frontend gate: a well-formed callsign + start date/time are the minimum to
// log (the daemon requires QSO_DATE / TIME_ON), and no field may be malformed
// — the red outlines make a blocked Log button self-explanatory. (The CAT/rig
// confirm gate is added with the Rig panel; enrichment never gates —
// invariant.)
export function canLog(): boolean {
    if (draft.callsign.trim() === '' || draft.dateOn === '' || draft.timeOn === '') return false;
    const p = draftProblems();
    return !Object.values(p).some(Boolean);
}

// Submit seam — injected in main.ts, mirroring the daemon's SetQsoLogger sink
// (ADR 0045 principle 3: backend coupling injected, not imported). The card
// never imports the real submit path, so it stays relocatable + testable in
// isolation. `duplicate` marks the one refusal with a follow-up action (the
// daemon supports force), so the card can offer "Log anyway".
export type SubmitResult = { ok: true } | { ok: false; message: string; duplicate?: boolean };
type SubmitFn = (qso: QsoDraft, opts?: { force?: boolean }) => Promise<SubmitResult>;
let submit: SubmitFn | null = null;

export function setSubmit(fn: SubmitFn): void {
    submit = fn;
}

// Submit progress + outcome surface for the card. busy doubles as the
// double-click latch: a write POST is ambiguous on timeout, so firing a
// second submit while one is in flight risks a double log.
export const submitState = $state({ busy: false, error: '', duplicate: false, logged: '' });

export async function logDraft(force = false): Promise<void> {
    if (!canLog() || submitState.busy) return;
    stampOff(); // QSO end = now, unless the operator entered one
    const call = draft.callsign.trim().toUpperCase();
    submitState.busy = true;
    submitState.error = '';
    submitState.duplicate = false;
    submitState.logged = '';
    let res: SubmitResult;
    try {
        res = (await submit?.({ ...draft }, { force })) ?? { ok: true };
    } catch {
        res = { ok: false, message: 'Submit failed unexpectedly — the QSO was NOT logged.' };
    } finally {
        submitState.busy = false;
    }
    if (res.ok) {
        clearDraft(); // next QSO starts now
        submitState.logged = call; // set after clearDraft (which resets it)
    } else {
        // Draft is preserved so nothing typed is lost; the operator fixes,
        // retries, or — on a duplicate — forces.
        submitState.error = res.message;
        submitState.duplicate = res.duplicate ?? false;
    }
}
