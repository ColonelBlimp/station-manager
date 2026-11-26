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
      - the page never draws a state older than the one it shows, within one
        archive: by generation, and at the SAME generation by when it was asked
        for. A state changes without its generation changing (a contact joins
        and the count rises; the same session reads "open" or "recovery
        pending" by which app is admitted when the daemon is asked), so of two
        requests that overlap, the one sent later is taken as the newer when
        its answer is the first to arrive, and the earlier one's answer is
        then not drawn (codex-276). But the order two requests were SENT in is
        not the order the daemon served them in: the earlier one may have been
        served last, and be the newer in fact. The page cannot tell. So when
        it sets such an answer aside it makes sure a read sent after BOTH is on
        its way, and that read's answer, which is newer than either, is what
        the page ends on (the review of ff5e855c). A read sent before an
        answer cannot put the old question back;
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
    session: {
        id: string;
        openedAt: string;
        sittingStartedAt: string | null;
        contactCount: number;
        heldByYou: boolean;
    } | null;
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
    /** Told each state this gate draws, and no state it refuses (W-0029 part
     *  6): the Session the workspace shows follows the same one rule, so the
     *  panel can never be of a state the gate did not draw. */
    drawn?: (state: SessionStateLike) => void;
    /** Told when a read sent with an archive proven is answered for ANOTHER
     *  archive: what starts the page's own archive check. */
    otherArchive?: (archiveId: string) => void;
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
let drawn: { archiveId: string; generation: number; asked: number } | null = null;
// Counts the requests sent, reads and answers alike: each takes the next number
// as it is sent, and that number orders two answers at one generation.
let asked = 0;
// The numbers of the READS that are out. When an answer is set aside for its
// number, the page looks here for a read sent after the state it shows; with
// none on its way it sends one.
// (A plain list: nothing reads it reactively, and it holds a handful at most.)
let readsOut: number[] = [];
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

// draw is the one rule (E5). It reports whether the state was drawn. `sentAs`
// is the number its request took when it was sent.
function draw(state: SessionStateLike, sentAs: number): boolean {
    const bound = ports?.archive() ?? null;
    if (bound === null || state.archiveId !== bound) return false;
    if (drawn !== null && drawn.archiveId === state.archiveId) {
        if (state.generation < drawn.generation) return false;
        // The same generation: the one asked for later is the newer.
        if (state.generation === drawn.generation && sentAs < drawn.asked) return false;
    }
    drawn = { archiveId: state.archiveId, generation: state.generation, asked: sentAs };
    draws++;
    recovery.generation = state.generation;
    ports?.drawn?.(state);
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

// An answer was not drawn. If that was because it was sent before the state
// shown at the SAME generation, which of the two the daemon served last is not
// known: make sure a read sent after the one shown is on its way. One that is
// already out will do; with none, one is sent. Its answer is newer than both.
function reconcile(state: SessionStateLike, sentAs: number): void {
    if (drawn === null || drawn.archiveId !== state.archiveId) return;
    if (state.generation !== drawn.generation || sentAs >= drawn.asked) return;
    const shownAs = drawn.asked;
    if (readsOut.some((n) => n > shownAs)) return;
    void read(false);
}

// read reports whether its answer was drawn: false when it failed, and when
// its answer was refused (another archive's, sent before the proof, or older
// than the state shown).
async function read(uncertain: boolean): Promise<boolean> {
    if (ports === null) return false;
    const drawsWhenSent = draws;
    // What a read may draw is decided by when it was SENT, not by what is bound
    // when it returns (codex-228). One sent with no archive proven draws
    // nothing, even if it comes back after the proof and for that very
    // archive: it may be older than the read the proof itself sends, and
    // drawing it would also make that read's failure look obsolete.
    const sentFor = ports.archive();
    const sentAs = ++asked;
    readsOut.push(sentAs);
    let out: SessionReadLike;
    try {
        out = await ports.read();
    } finally {
        readsOut = readsOut.filter((n) => n !== sentAs);
    }
    if (out.kind === 'ok') {
        if (sentFor === null) return false;
        if (out.state.archiveId !== sentFor) {
            // Sent with an archive proven, answered for another: positive word
            // that the daemon serves another archive now. It is not drawn, and
            // it starts the page's archive check (codex-275): when the event
            // stream opens on that daemon for the first time, nothing else does.
            ports.otherArchive?.(out.state.archiveId);
            return false;
        }
        const drawnNow = draw(out.state, sentAs);
        if (drawnNow && !uncertain) recovery.error = '';
        if (!drawnNow) reconcile(out.state, sentAs);
        return drawnNow;
    }
    if (draws !== drawsWhenSent) return false; // obsolete: a state has been drawn since this was sent
    const nothingDrawn = recovery.status === 'loading' || recovery.status === 'unreadable';
    if (nothingDrawn || uncertain) {
        recovery.status = 'unreadable';
        recovery.session = null;
        recovery.error = out.message;
    }
    return false;
}

/**
 * Read the session's state and draw it: at the load, when the event stream
 * opens or reopens, on an operating_session.changed event, and for "try
 * again". No timer calls it. It resolves to whether the answer was drawn,
 * which is what the Session waits for before it lets export and email act.
 */
export function readSessionState(): Promise<boolean> {
    return read(false);
}

/**
 * The boot has proven which archive the page is bound to (main.ts calls this
 * when the boot's bracket of identity reads has settled). Until now nothing has
 * been drawn, and nothing read before now is used: the state is read, and the
 * workspace stays withheld until it is known. This is the load's read.
 */
export function sessionArchiveProven(): Promise<boolean> {
    return read(false);
}

/** Send the operator's answer to the question that is showing. */
export async function answerRecovery(answer: RecoveryAnswer): Promise<void> {
    if (ports === null || recovery.status !== 'question' || recovery.answering) return;
    const session = recovery.session;
    if (session === null) return;
    recovery.answering = true;
    recovery.error = '';
    const sentAs = ++asked;
    const out = await ports.answer(answer, recovery.generation, session.id);
    if (out.kind === 'applied' || out.kind === 'stale') {
        recovery.answering = false;
        draw(out.state, sentAs);
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
    asked = 0;
    readsOut = [];
    draws = 0;
}
