// The in-progress QSO draft — the shared spine of the Operate surface (ADR 0045).
// The logging card writes it; the info panels read from it (Worked reads
// .callsign, Details enriches .callsign and can write name/qth/grid back, Session
// receives the QSO on log). Rig-provided fields (freq/mode/band) are NOT here —
// they live in the rig state and merge in at log time — so this stays the
// operator-entered per-QSO data only. Presentation (LoggingCard) is separate.

import { isValidCallsign } from '../validators/callsign';
import { isValidRs, isValidRst, isValidSignalReport } from '../validators/rst';
import { usesSignalReport } from '../utils/mode';
import { rig, rigReady } from './rig.svelte';
import { toasts } from '../ui/toasts.svelte';

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
    comment: string; // ADIF COMMENT — things shared during the QSO (logging card)
    // Rarely-touched per-contact fields — off the fast-path logging card, edited
    // on demand in the contact overlay (ContactDialog).
    rig: string; // ADIF RIG — contacted station's rig / working conditions
    notes: string; // ADIF NOTES — operator's PRIVATE notes (distinct from COMMENT)
    rxPwr: string; // ADIF RX_PWR — contacted station's TX power in watts (their signal, as received)
}

/**
 * RST default values — voice modes use the two-digit Readability/Strength
 * scale; CW adds a third tone digit. (Ported from the shipping SPA; the
 * WSJT-X modes keep '59', which happens to also pass the SNR pattern.)
 */
export const DEFAULT_RST_VOICE = '59';
export const DEFAULT_RST_CW = '599';

export function rstDefaultFor(mode: string): string {
    return mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE;
}

function blank(): QsoDraft {
    return {
        callsign: '',
        rstSent: rstDefaultFor(rig.mode),
        rstRcvd: rstDefaultFor(rig.mode),
        name: '',
        qth: '',
        gridsquare: '',
        dateOn: '',
        timeOn: '',
        dateOff: '',
        timeOff: '',
        comment: '',
        rig: '',
        notes: '',
        rxPwr: '',
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

// Memoized on purpose: the fill effect below tracks THIS, not rig.mode, so it
// only re-fires when the default actually changes (mode crosses the CW ↔
// voice boundary) — never on a same-family change like USB ↔ LSB, where an
// operator-typed report must survive.
const defaultRst = $derived(rstDefaultFor(rig.mode));

/*
    RST default-fill effect (shipping rule). $effect.root because this runs at
    module level, outside a component lifecycle — alive for the page lifetime.

    Mode-flip overwrite: refill BOTH fields when defaultRst changes. The
    tradeoff — an operator-typed value gets clobbered if the mode flips after
    they typed — is deliberate: '59' is meaningless on CW and '599' on voice,
    and reports are typically set once the mode is settled. With the rig SSE
    live this matters daily: spinning the rig to CW-U must not leave 59/59.

    Intentionally NOT here (shipping learned this): an "empty → refill"
    effect. It snapped the field back to the default the moment the operator
    deleted the last digit mid-edit. A deliberately-blanked report still
    submits — the daemon default-fills '59' for non-FT8 blanks, the decided
    posture (2026-07-08 review, finding 3 skipped).
*/
$effect.root(() => {
    $effect(() => {
        const d = defaultRst;
        draft.rstSent = d;
        draft.rstRcvd = d;
    });
});

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
}

// Format checks for the card's free-text date/time fields (what the ADIF
// serializer + daemon accept). Empty is valid here — presence is canLog's
// concern, and dateOff/timeOff are legitimately blank until log.
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
const TIME_RE = /^\d{2}:\d{2}(:\d{2})?$/;
// RX_PWR is an ADIF Number (non-negative): digits with an optional single decimal
// point — including a leading (".5") or trailing ("100.") dot, both valid per ADIF
// 3.1.7. Anything else — a localised "2,5", exponent "1e3", a sign, or a stray
// letter — is malformed and gets FLAGGED (red outline + blocks Log), never
// silently stripped into a different number (codex db053f3b/ab14e974 P2).
const RX_PWR_RE = /^(\d+\.?\d*|\.\d+)$/;

export interface DraftProblems {
    callsign: boolean;
    rstSent: boolean;
    rstRcvd: boolean;
    dateOn: boolean;
    timeOn: boolean;
    dateOff: boolean;
    timeOff: boolean;
    rxPwr: boolean;
}

/** Per-field validity (true = malformed) for the card's red outlines. */
export function draftProblems(): DraftProblems {
    const bad = (re: RegExp, v: string): boolean => v !== '' && !re.test(v.trim());
    // The report validator tracks the rig's mode (the one rig read in this
    // module — mode drives validation, it never enters the draft): WSJT-X
    // weak-signal modes report a signed dB SNR ("-12"); CW reports RST (tone
    // optional); everything else reports RS — the tone digit only exists on
    // CW, so 599 on USB is malformed.
    const report = usesSignalReport(rig.mode)
        ? isValidSignalReport
        : rig.mode === 'CW'
          ? isValidRst
          : isValidRs;
    return {
        callsign: isValidCallsign(draft.callsign) !== null,
        rstSent: report(draft.rstSent) !== null,
        rstRcvd: report(draft.rstRcvd) !== null,
        dateOn: bad(DATE_RE, draft.dateOn),
        timeOn: bad(TIME_RE, draft.timeOn),
        dateOff: bad(DATE_RE, draft.dateOff),
        timeOff: bad(TIME_RE, draft.timeOff),
        rxPwr: bad(RX_PWR_RE, draft.rxPwr),
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

// Submit progress + the ONE outcome that stays card-local: the duplicate
// refusal (its "Log anyway" action belongs next to the Log button). All
// other outcomes — success, non-duplicate refusals — go through toasts so
// nothing reflows the card. busy doubles as the double-click latch: a write
// POST is ambiguous on timeout, so firing a second submit while one is in
// flight risks a double log.
export const submitState = $state({ busy: false, error: '', duplicate: false });

/** Returns true when the QSO was stored (callers refocus the callsign field
 *  on success); false on refusal or when the gate/busy latch blocked it. */
/** Operator declined the force (dialog Cancel / Esc / backdrop): drop the
 *  refusal, keep the draft — nothing typed is lost. */
export function dismissDuplicate(): void {
    submitState.error = '';
    submitState.duplicate = false;
}

export async function logDraft(force = false): Promise<boolean> {
    // The CAT/rig gate is enforced HERE, not only on the buttons — a
    // button-level-only guard grew a bypass once already (the duplicate
    // "Log anyway" path, 2026-07-08 review finding).
    if (!canLog() || !rigReady() || submitState.busy) return false;
    stampOff(); // QSO end = now, unless the operator entered one
    const call = draft.callsign.trim().toUpperCase();
    submitState.busy = true;
    submitState.error = '';
    submitState.duplicate = false;
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
        toasts.info(`Logged ${call}`);
        return true;
    }
    if (res.duplicate) {
        // Card-local: the refusal with a follow-up action. Draft preserved.
        submitState.error = res.message;
        submitState.duplicate = true;
    } else {
        // Draft is preserved so nothing typed is lost; the operator fixes
        // and retries. The message itself is a toast — off the card.
        toasts.error(res.message);
    }
    return false;
}
