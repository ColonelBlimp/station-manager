/*
    The recovery gate: what the main window shows for the operating session's
    state (ADR 0100 §3, §6; W-0029 part 4, criteria E).

    After a start that did not follow a confirmed exit, the archive can hold a
    session with contacts that an earlier start left open. The daemon names
    that state `recovery_pending`, and the page then shows one question in
    place of the workspace: continue that session, or start a new one.

    Status:
      - loading     the state has not been read on this load: nothing of the
                    workspace is shown;
      - question    recovery pending: the question, for the session and at the
                    generation read;
      - unreadable  the state could not be read: the page says so and offers to
                    try again. An unreadable state is never a clean start;
      - clear       any other state: the page shows what it shows without this
                    gate (the workspace, or the first-run card above it).

    The rules that keep it honest (each has its own test):
      - one rule draws every state, whether it came from a read, an applied
        answer or a stale refusal;
      - the page never draws a state older than the one it shows, by
        generation, within one archive: a read sent before an answer cannot put
        the old question back;
      - a state is drawn only for the archive the boot has PROVEN. One read
        before that proof, or for another archive, is not drawn and is not
        kept: the page stays as it was. A read SENT before the proof draws
        nothing whenever it returns (codex-228). When the proof comes the state
        is read then, with the workspace still withheld until it is known. An earlier
        archive's "open" must never stand as permission to show this archive's
        workspace (codex-226), and an answer that arrived before the proof may
        be older than one that arrived before it (the clean-room review of
        2c362c3d), so nothing from before the proof is drawn at it;
      - a read that fails takes away nothing that was drawn. It makes the
        page unreadable only when nothing has been drawn yet, or when an
        answer has just failed and what the daemon holds is not known; and
        only if nothing has been drawn since that read was sent (codex-226);
      - the page never sends an answer again by itself. After a stale refusal
        it draws the state returned; after a failure it reads and draws.

    ADR 0045: this module never imports lib/api. main.ts injects the read, the
    answer and the bound archive (setRecoveryPorts), which is why the shapes
    are declared here.
*/

export type SessionStateNameLike =
    'open' | 'none' | 'setup_pending' | 'recovery_pending' | 'archive_switching';

export interface SessionStateLike {
    archiveId: string;
    generation: number;
    state: SessionStateNameLike;
    session: { id: string; openedAt: string; contactCount: number; heldByYou: boolean } | null;
}

export type SessionReadLike =
    { kind: 'ok'; state: SessionStateLike } | { kind: 'error'; message: string };

export type SessionAnswerLike =
    | { kind: 'applied'; state: SessionStateLike }
    | { kind: 'stale'; state: SessionStateLike }
    | { kind: 'failed'; message: string };

export type RecoveryAnswer = 'continue' | 'new';

export interface RecoveryPorts {
    read: () => Promise<SessionReadLike>;
    answer: (
        answer: RecoveryAnswer,
        generation: number,
        sessionId: string
    ) => Promise<SessionAnswerLike>;
    /** The archive the page is bound to; null while none is proven. */
    archive: () => string | null;
}

export type RecoveryStatus = 'loading' | 'question' | 'unreadable' | 'clear';

/** The session the question is about, as it is shown. */
export interface RecoverySession {
    id: string;
    openedAt: string;
    contactCount: number;
}

export const recovery: {
    status: RecoveryStatus;
    session: RecoverySession | null;
    /** The generation of the state shown: what an answer pressed now carries. */
    generation: number;
    /** An answer is in flight: both buttons are disabled. */
    answering: boolean;
    /** What went wrong with the last read or answer; '' when nothing did. */
    error: string;
} = $state({ status: 'loading', session: null, generation: 0, answering: false, error: '' });

let ports: RecoveryPorts | null = null;
// The state last drawn: its archive and generation.
let drawn: { archiveId: string; generation: number } | null = null;
// Counts the states drawn. A read notes it when it is sent: if it has moved by
// the time that read fails, something authoritative has been drawn since, by a
// later read or by an answer, and the failure is obsolete.
let draws = 0;

/** Inject the read, the answer and the bound archive (main.ts). */
export function setRecoveryPorts(p: RecoveryPorts): void {
    ports = p;
}

/**
 * Which screen replaces the workspace, or null when none does. App uses this
 * one predicate, so the gate and what it shows cannot disagree.
 */
export function recoveryGate(): 'loading' | 'question' | 'unreadable' | null {
    return recovery.status === 'clear' ? null : recovery.status;
}

// draw is the one rule (E5). It reports whether the state was drawn.
function draw(state: SessionStateLike): boolean {
    const bound = ports?.archive() ?? null;
    if (bound === null || state.archiveId !== bound) return false;
    if (
        drawn !== null &&
        drawn.archiveId === state.archiveId &&
        state.generation < drawn.generation
    ) {
        return false;
    }
    drawn = { archiveId: state.archiveId, generation: state.generation };
    draws++;
    recovery.generation = state.generation;
    if (state.state === 'recovery_pending' && state.session !== null) {
        recovery.status = 'question';
        recovery.session = {
            id: state.session.id,
            openedAt: state.session.openedAt,
            contactCount: state.session.contactCount,
        };
        return true;
    }
    recovery.status = 'clear';
    recovery.session = null;
    return true;
}

async function read(uncertain: boolean): Promise<void> {
    if (ports === null) return;
    const drawsWhenSent = draws;
    // What a read may draw is decided by when it was SENT, not by what is bound
    // when it returns (codex-228). One sent with no archive proven draws
    // nothing, even if it comes back after the proof and for that very
    // archive: it may be older than the read the proof itself sends, and
    // drawing it would also make that read's failure look obsolete.
    const sentFor = ports.archive();
    const out = await ports.read();
    if (out.kind === 'ok') {
        if (sentFor === null || out.state.archiveId !== sentFor) return;
        if (draw(out.state) && !uncertain) recovery.error = '';
        return;
    }
    if (draws !== drawsWhenSent) return; // obsolete: a state has been drawn since this was sent
    const nothingDrawn = recovery.status === 'loading' || recovery.status === 'unreadable';
    if (nothingDrawn || uncertain) {
        recovery.status = 'unreadable';
        recovery.session = null;
        recovery.error = out.message;
    }
}

/**
 * Read the session's state and draw it: at the load, when the event stream
 * opens or reopens, on an operating_session.changed event, and for "try
 * again". No timer calls it.
 */
export function readSessionState(): Promise<void> {
    return read(false);
}

/**
 * The boot has proven which archive the page is bound to (main.ts calls this
 * when the boot's bracket of identity reads has settled). Until now nothing has
 * been drawn, and nothing read before now is used: the state is read, and the
 * workspace stays withheld until it is known. This is the load's read.
 */
export function sessionArchiveProven(): Promise<void> {
    return read(false);
}

/** Send the operator's answer to the question that is showing. */
export async function answerRecovery(answer: RecoveryAnswer): Promise<void> {
    if (ports === null || recovery.status !== 'question' || recovery.answering) return;
    const session = recovery.session;
    if (session === null) return;
    recovery.answering = true;
    recovery.error = '';
    const out = await ports.answer(answer, recovery.generation, session.id);
    if (out.kind === 'applied' || out.kind === 'stale') {
        recovery.answering = false;
        draw(out.state);
        return;
    }
    // The answer may or may not have been applied: say what went wrong, then
    // read what the daemon holds. Nothing is sent again by the page, and no
    // answer can be pressed until that read has settled: the buttons stay
    // disabled, so a second answer is never sent on a state the page is still
    // finding out about.
    recovery.error = out.message;
    try {
        await read(true);
    } finally {
        recovery.answering = false;
    }
}

/** Test seam — restore the singleton between cases. */
export function _resetRecoveryForTests(): void {
    recovery.status = 'loading';
    recovery.session = null;
    recovery.generation = 0;
    recovery.answering = false;
    recovery.error = '';
    ports = null;
    drawn = null;
    draws = 0;
}
