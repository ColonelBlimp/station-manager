// Closing the app: a confirmed exit (ADR 0102 ruling 6; ADR 0100 §3, Closing; W-0027 A3).
//
// When the operator closes Station Manager, the shell stops FT8 itself, tells smd that this
// start of the app is exiting, and waits for smd's acknowledgement before it goes. smd then
// closes the session as `app_closed`, and the next start is not asked whether to continue it.
//
// The order, from the moment the close is confirmed:
//   1. the barrier: this lifetime is never admitted again (admission.mjs, beginClosing); the
//      page's own writes are refused; the window is hidden. Nothing more is sent until
//      admission work already issued has settled here.
//   2. the disarm, as the operator's own request, and its answer. A window that knows it has
//      been retired sends none: a disarm needs no current lifetime, so it would stop the app
//      that replaced it.
//   3. the session's state is read, and the exit reported at the generation and archive read.
//   4. acknowledged: the window is closed and the app quits.
//
// What a close does never depends on the page. Whether to ask first is another matter, and is
// `confirm`'s.
//
// THE QUESTION (section A) is asked before any of that, when and only when something would
// stop or be lost, and names each thing once: FT8, from smd's own read of its state at that
// moment (GET /v1/ft8/state), never from a page; and what the page reports of its own, a
// contact being entered, a log attempt whose outcome is not known, Settings unsaved or being
// saved. The page's word decides only whether to ask and which lines are shown. Cancel
// changes nothing at all: the question comes before the barrier.
//
// THE CLOSE THAT IS NOT CONFIRMED is said once, in a box, and is of two kinds, because an exit
// that was sent may have been stored, or may yet be: smd decides an exit whatever becomes of
// the request (it does not follow the request's context).
//   - First kind, nothing of the exit was sent (the disarm or the read failed): Try again,
//     Close anyway, or Cancel, which gives back the window, the page's writes and ordinary
//     admission.
//   - Second kind, an exit was sent and not acknowledged: there is no way back. The window is
//     closed BEFORE the box, so this lifetime holds no FT8 stream and can make no arm, and the
//     app outlives its window until the box is answered: Try again or Close anyway. smd's
//     terminal refusal (not current, still holding the session) is of this kind, with no Try
//     again. Once an exit was sent the close stays of this kind, whatever a later attempt
//     fails at, and Try again asks only for the acknowledgement: it sends no second disarm.
import { randomBytes } from 'node:crypto';

/** Ruled 2026-11-23: five seconds for each request of a close, and no other timer. It is a
 *  limit per request, not on the whole close. */
export const CLOSE_TIMEOUT_MS = 5000;

/** Names the archive the app closed in (internal/api: the exit checks it for the current
 *  lifetime). */
export const ARCHIVE_HEADER = 'X-SM-Archive-Id';

// The mark on every request the close itself sends: a value made in this process at start and
// known to no page (as the waiting page's probe is marked, waiting.mjs). While the page's
// writes are refused, the request filter lets through a write that carries it and no other.
// smd ignores the parameter.
const CLOSE_PARAM = 'sm-close';
const CLOSE_MARK = randomBytes(16).toString('hex');

/** True only for a request this shell's main process made as part of a close. */
export function isCloseRequest(url) {
    try {
        return new URL(url).searchParams.get(CLOSE_PARAM) === CLOSE_MARK;
    } catch {
        return false;
    }
}

const closeUrl = (origin, path) => `${origin}${path}?${CLOSE_PARAM}=${CLOSE_MARK}`;

/** One request of the close, held to the pin and the credential by the session, with its own
 *  limit. Resolves with { status, body } (body null when it is not JSON), or null when there
 *  was no answer. Never throws. */
async function ask(ses, url, { timeoutMs, ...init }) {
    try {
        const res = await ses.fetch(url, {
            ...init,
            redirect: 'manual',
            signal: AbortSignal.timeout(timeoutMs),
        });
        let body = null;
        try {
            body = await res.json();
        } catch {
            body = null;
        }
        return { status: res.status, body };
    } catch {
        return null;
    }
}

const post = (body, headers = {}) => ({
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...headers },
    body: JSON.stringify(body),
});

/**
 * Stop FT8, as the operator's own request, and wait for smd's answer. Resolves with
 * 'stopped', or 'not-served' when smd has no such route (the disarm exists only when FT8 is
 * enabled: exactly smd's "no such route" and nothing else), or 'failed' for any other answer
 * or none. A failed disarm is never read as "FT8 has stopped".
 */
export async function stopFt8(ses, origin, { timeoutMs = CLOSE_TIMEOUT_MS } = {}) {
    const r = await ask(ses, closeUrl(origin, '/v1/ft8/tx/arm'), {
        timeoutMs,
        ...post({ armed: false }),
    });
    if (r?.status === 202) return 'stopped';
    if (r?.status === 404 && r.body?.code === 'not_found') return 'not-served';
    return 'failed';
}

/**
 * Report this lifetime's exit and wait for smd's acknowledgement. Resolves with
 * { state, sent }: state is 'acknowledged', 'terminal' (not current and still holding the
 * session: never repeated) or 'unconfirmed'; `sent` is whether an exit report went out, and
 * so whether smd may have stored one.
 *
 * A stale answer is the exit's one exception (ADR 0100 §5; agreed for A3): the exit, and only
 * the exit, is sent again at the generation smd returned, without reading or admitting again.
 * Each repeat follows a definite answer, so there is no timer on the loop, as in admission.
 */
export async function reportExit(ses, origin, { timeoutMs = CLOSE_TIMEOUT_MS } = {}) {
    const read = await ask(ses, closeUrl(origin, '/v1/operating-session'), { timeoutMs });
    const archive = read?.body?.archive_id;
    let generation = read?.body?.generation;
    if (read?.status !== 200 || typeof archive !== 'string' || archive === '') {
        return { state: 'unconfirmed', sent: false };
    }
    if (!Number.isSafeInteger(generation)) return { state: 'unconfirmed', sent: false };
    for (;;) {
        const r = await ask(ses, closeUrl(origin, '/v1/operating-session/exit'), {
            timeoutMs,
            ...post({ generation }, { [ARCHIVE_HEADER]: archive }),
        });
        if (r?.status === 200 && typeof r.body?.already_recorded === 'boolean') {
            return { state: 'acknowledged', sent: true };
        }
        const code = r?.status === 409 ? r.body?.code : '';
        if (code === 'operating_session_exit_not_current') return { state: 'terminal', sent: true };
        if (code !== 'operating_session_stale' || !Number.isSafeInteger(r.body?.generation)) {
            return { state: 'unconfirmed', sent: true };
        }
        generation = r.body.generation;
    }
}

/**
 * FT8's state, as smd says it at this moment (W-0027 A3, section I): 'active' (a stop would
 * stop something), 'idle', 'not-served' (FT8 is not enabled: an answer of its own), or
 * 'unknown' for anything that is not a valid answer: none, a missing route, a malformed one.
 * "Not known" is never read as idle and never as "not served". It is one observation.
 */
export async function readFt8State(ses, origin, { timeoutMs = CLOSE_TIMEOUT_MS } = {}) {
    const r = await ask(ses, closeUrl(origin, '/v1/ft8/state'), { timeoutMs });
    const b = r?.body;
    if (r?.status !== 200 || b === null || typeof b !== 'object' || Array.isArray(b)) {
        return 'unknown';
    }
    if (b.served === false) return Object.keys(b).length === 1 ? 'not-served' : 'unknown';
    if (b.served === true && typeof b.would_stop === 'boolean') {
        return b.would_stop ? 'active' : 'idle';
    }
    return 'unknown';
}

/** The one fixed expression the shell evaluates in the page when a close is asked for: three
 *  attributes of the close's own on the root element (frontend/app/src/lib/closeReasons.ts),
 *  and nothing else of the page. */
export const PAGE_REASONS_EXPRESSION =
    "(() => { const e = document.documentElement; return ['data-sm-close-entry', 'data-sm-close-unsaved', 'data-sm-close-saving'].map((n) => e.getAttribute(n)); })()";

/** The Settings sections a page may name, by their tabs (the page's own list, in its order:
 *  frontend/app/src/lib/config/unsaved.ts; a test here reads that file and compares). */
export const SETTINGS_SECTIONS = Object.freeze([
    'Station',
    'Logbooks',
    'Rigs',
    'FT8 / FT4',
    'Forwarding',
    'Email',
    'Enrichment',
    'General',
    'Archives',
]);

const NO_PAGE_REASONS = () => ({ entry: '', unsaved: [], saving: [] });

/** A list of sections as the page wrote it, or none if any part of it is not a section the
 *  shell knows, named once. */
function sectionsOf(value) {
    if (typeof value !== 'string' || value === '') return [];
    const labels = value.split('|');
    const known = labels.every((l) => SETTINGS_SECTIONS.includes(l));
    return known && new Set(labels).size === labels.length ? labels : [];
}

/**
 * What the page reports a close would lose, from what the fixed expression returned. Only
 * the values the shell knows are accepted, each attribute by itself; anything else is "not
 * reported" and shows no line. A page that publishes nothing (the waiting page) reports
 * nothing. A section being saved is named once, as being saved.
 */
export function pageReasons(raw) {
    if (!Array.isArray(raw) || raw.length !== 3) return NO_PAGE_REASONS();
    const saving = sectionsOf(raw[2]);
    return {
        entry: raw[0] === 'draft' || raw[0] === 'unknown' ? raw[0] : '',
        unsaved: sectionsOf(raw[1]).filter((l) => !saving.includes(l)),
        saving,
    };
}

/**
 * Read the page's reasons. `readPage` evaluates PAGE_REASONS_EXPRESSION in the window's
 * page. A page that cannot be read, or does not answer within the limit, reports nothing: it
 * never means a step of the close is skipped, and it silences no reason that is not its own.
 */
export async function readPageReasons(readPage, { timeoutMs = CLOSE_TIMEOUT_MS } = {}) {
    let timer;
    try {
        const limit = new Promise((_res, reject) => {
            timer = setTimeout(() => reject(new Error('the page did not answer')), timeoutMs);
        });
        return pageReasons(await Promise.race([Promise.resolve().then(readPage), limit]));
    } catch {
        return NO_PAGE_REASONS();
    } finally {
        clearTimeout(timer);
    }
}

/** "Station, Email and General", as the page's own prompt lists sections. */
function listOf(labels) {
    if (labels.length <= 1) return labels[0] ?? '';
    return `${labels.slice(0, -1).join(', ')} and ${labels[labels.length - 1]}`;
}

/**
 * The question before a close (the words ruled 2026-11-23), or null when there is nothing to
 * ask about. Each line is shown only when it is true, and each thing is named once. `ft8` is
 * readFt8State's word, or 'none' when it was not read. The line for an unknown log outcome
 * never says the contact is lost: it may already be stored. The line for a section being
 * saved promises neither that the save completes nor that it does not.
 */
export function closeQuestion({ ft8, entry, unsaved, saving }) {
    const lines = [];
    if (ft8 === 'active') lines.push('FT8 will be stopped and transmit disarmed.');
    if (ft8 === 'unknown') {
        lines.push("FT8's state could not be read. If it is running, it will be stopped.");
    }
    if (entry === 'draft') lines.push('The contact you are entering will be discarded.');
    if (entry === 'unknown') {
        lines.push(
            'A contact was sent to be logged and its outcome is not known. It may already be logged.'
        );
    }
    if (unsaved.length > 0) {
        lines.push(`Unsaved changes in Settings (${listOf(unsaved)}) will be lost.`);
    }
    if (saving.length > 0) {
        lines.push(
            `Settings (${listOf(saving)}) is still being saved. Closing does not wait: the save may not complete, and anything changed since it began will be lost.`
        );
    }
    if (lines.length === 0) return null;
    return {
        message: 'Close Station Manager?',
        detail: lines.join('\n\n'),
        buttons: ['Close', 'Cancel'],
        choices: ['close', 'cancel'],
        cancelId: 1,
    };
}

/**
 * The `confirm` of a close: gathers the reasons from both sources, together, and asks the one
 * question when there is any. Resolves true to go on with the close (nothing to ask, or the
 * answer was Close), false for Cancel or for a question that gave no answer.
 *
 * A window that knows it has been retired, and a lifetime smd was never asked to admit, read
 * no FT8 state and show no FT8 line: their close sends no disarm (closeFlow), so it must not
 * say that it stops FT8, which may be another app's.
 *
 *   readPage     () => what PAGE_REASONS_EXPRESSION evaluates to in the window's page
 *   askQuestion  (question) => 'close' | 'cancel': shows closeQuestion's box
 */
export function closeConfirmation({
    ses,
    origin,
    admission,
    readPage,
    askQuestion,
    timeoutMs = CLOSE_TIMEOUT_MS,
}) {
    return async () => {
        const stopsFt8 = admission.everAsked() && !admission.isRetired();
        const [ft8, page] = await Promise.all([
            stopsFt8 ? readFt8State(ses, origin(), { timeoutMs }) : 'none',
            readPageReasons(readPage, { timeoutMs }),
        ]);
        const question = closeQuestion({ ft8, ...page });
        if (question === null) return true;
        return (await askQuestion(question)) === 'close';
    };
}

const TITLE = 'The daemon did not confirm the close';
const NOT_CONFIRMED =
    'Station Manager could not confirm with the daemon that it is closing, so the daemon may treat this sitting as interrupted.';
const FT8_NOT_CONFIRMED =
    'Station Manager could not confirm that FT8 was stopped. The daemon stops it by itself shortly after this window goes.';
// E1: a disarm sent and not answered may still arrive. It is a stop, and it is said.
const STOP_MAY_ARRIVE = 'If you cancel, the stop already sent may still take effect.';

/**
 * The box for a close that was not confirmed (the words ruled 2026-11-23): its headline, its
 * text, the buttons and what each means. It says the close could not be CONFIRMED, never that
 * smd was not told, and names a failed disarm as such. Dismissing the box cancels a close of
 * the first kind; a close of the second kind has nothing to cancel to, so it closes.
 */
export function unconfirmedBox({ kind, disarmFailed, terminal }) {
    const text = disarmFailed ? FT8_NOT_CONFIRMED : NOT_CONFIRMED;
    if (kind === 'first') {
        return {
            message: TITLE,
            detail: disarmFailed ? `${text}\n\n${STOP_MAY_ARRIVE}` : text,
            buttons: ['Try again', 'Close anyway', 'Cancel'],
            choices: ['retry', 'close', 'cancel'],
            cancelId: 2,
        };
    }
    if (terminal) {
        return {
            message: TITLE,
            detail: text,
            buttons: ['Close'],
            choices: ['close'],
            cancelId: 0,
        };
    }
    return {
        message: TITLE,
        detail: text,
        buttons: ['Try again', 'Close anyway'],
        choices: ['retry', 'close'],
        cancelId: 1,
    };
}

/**
 * The close of one app window. `close()` runs it, and resolves with 'closed' (acknowledged, or
 * nothing to report), 'closed-unconfirmed' or 'cancelled'. One close at a time: a call while
 * one is under way joins it, and shows no second box. `holdsApp()` is true while a close is
 * under way: the app must not quit because its last window closed (the second kind closes the
 * window before its box). `allowsQuit()` is false for any quit but the close's own.
 *
 *   admission  { everAsked, isRetired, beginClosing, endClosing }   (admission.mjs)
 *   window     { hide, show, destroy }
 *   pageWrites { refuse, allow }: the request filter's switch (hardening.mjs)
 *   ask        (state) => 'retry' | 'close' | 'cancel': shows unconfirmedBox(state)
 *   quit       ends the app
 *   confirm    () => boolean: the question, when something would stop (closeConfirmation)
 */
export function closeFlow({
    ses,
    origin,
    admission,
    window,
    pageWrites,
    ask: askOperator,
    quit,
    confirm = async () => true,
    timeoutMs = CLOSE_TIMEOUT_MS,
}) {
    let running = null;
    let leaving = false; // the close has reached its own final quit

    async function run() {
        if (!(await confirm())) return 'cancelled';
        let gone = false;
        const closeWindow = () => {
            if (gone) return;
            gone = true;
            window.destroy();
        };
        const leave = (how) => {
            closeWindow();
            leaving = true;
            quit();
            return how;
        };

        // The barrier first, whatever follows: from here this lifetime is never admitted.
        const settledAdmission = admission.beginClosing();

        // No admission was ever asked for this lifetime (smd was down from the start, say):
        // smd has never heard of it, so there is no session to close and nothing it can have
        // armed, and a disarm from it could only stop somebody else. It simply goes. The
        // barrier still stands: a start-up probe that is out when the window goes must not
        // become its first admission afterwards (codex-239; waiting.mjs checks the window
        // too). `everAsked` turns true when an admission is asked for, before anything is
        // sent, so a doubt falls on the side of running the whole close.
        if (!admission.everAsked()) return leave('closed');

        pageWrites.refuse();
        window.hide();
        await settledAdmission;

        let exitSent = false; // once true, for good: that exit may be stored, now or later
        for (;;) {
            const outcome = await attempt();
            if (outcome.state === 'acknowledged') return leave('closed');

            const kind = exitSent ? 'second' : 'first';
            if (kind === 'second') closeWindow(); // before the box: no FT8 stream, no arm
            const choice = await askOperator({
                kind,
                disarmFailed: outcome.disarmFailed,
                terminal: outcome.state === 'terminal',
            });
            if (choice === 'retry' && outcome.state !== 'terminal') continue;
            if (choice === 'cancel' && kind === 'first') {
                pageWrites.allow();
                admission.endClosing();
                window.show();
                return 'cancelled';
            }
            return leave('closed-unconfirmed');
        }

        async function attempt() {
            // No disarm from a window that knows it is retired, and none once an exit was
            // sent: smd may have stored that exit, so this lifetime may be retired without
            // knowing it, and a disarm needs no lifetime: it would stop whoever operates now
            // (the clean-room review of ff653c6c). Nothing is lost by it. This close's own
            // disarm was answered before its exit went out, the page has written nothing
            // since, and an acknowledged exit stops FT8 for its own lifetime anyway.
            if (!exitSent && !admission.isRetired()) {
                const stop = await stopFt8(ses, origin(), { timeoutMs });
                // Not on to the exit as if FT8 had stopped.
                if (stop === 'failed') return { state: 'unconfirmed', disarmFailed: true };
            }
            const exit = await reportExit(ses, origin(), { timeoutMs });
            if (exit.sent) exitSent = true;
            return { state: exit.state, disarmFailed: false };
        }
    }

    return {
        close() {
            running ??= run().finally(() => {
                running = null;
            });
            return running;
        },
        holdsApp: () => running !== null,
        /** Whether the app may quit now: always when no close is under way, and during a
         *  close only for the close's own final quit. Any other request to quit while a
         *  close is under way would end the app under its box, without the operator's
         *  answer (codex-239). */
        allowsQuit: () => running === null || leaving,
    };
}
