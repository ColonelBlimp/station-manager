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
        requests that overlap, the one sent later is taken as the newer: its
        answer is drawn over the earlier one's, and the earlier one's is not
        drawn over its (codex-276). But the order two requests were SENT in is
        not the order the daemon served them in: either may have been served
        last, and be the newer in fact (the review of ff5e855c). The page
        cannot tell, and a read that was already out when the second answer
        arrived cannot tell it either: that one too may have been served before
        (the review of 5f48eaa0). So whenever an answer arrives whose request
        was out while the state shown, of the same generation, was drawn, the
        page reads once more, and sends that read only when no read is out.
        Every answer has arrived by then, so the daemon serves it after all of
        them, and its answer is what the page ends on. Reads that did not
        overlap are in order and nothing more is read. A read sent before an
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
// The state last drawn: its archive and generation, the number its request
// took (`asked`), and which draw it was (`drawNo`).
let drawn: { archiveId: string; generation: number; asked: number; drawNo: number } | null = null;
// Counts the requests sent, reads and answers alike: each takes the next number
// as it is sent, and that number orders two answers at one generation.
let asked = 0;
// How many reads are out.
let readsOut = 0;
// Two answers of one generation overlapped, and which of them the daemon served
// last is not known. One read settles it, sent when no read is out: every
// answer has arrived by then. It is one flag and one read however many
// overlapped, so the reads the page sends for itself never chase one another.
let unsure = false;
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
    draws++;
    drawn = {
        archiveId: state.archiveId,
        generation: state.generation,
        asked: sentAs,
        drawNo: draws,
    };
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

// take draws an answer by the one rule, and notes when its order against the
// state shown is not known: that state is of the same generation and was drawn
// while this answer's request was out (`drawsWhenSent` is the count of draws
// when it was sent). Whichever of the two the rule keeps, the daemon may have
// served the other last. An answer of another generation is ordered by it.
function take(state: SessionStateLike, sentAs: number, drawsWhenSent: number): boolean {
    if (
        drawn !== null &&
        drawn.archiveId === state.archiveId &&
        drawn.generation === state.generation &&
        drawn.drawNo > drawsWhenSent
    ) {
        unsure = true;
    }
    return draw(state, sentAs);
}

// With an order not known and no read out, read once. That read is sent after
// every answer arrived. If another request overlaps it in its turn, the same
// rule sends one more after that; it ends when nothing else is asking. A read
// that fails is not repeated: nothing here asks an unreachable daemon in a loop.
function settle(): void {
    if (!unsure || readsOut > 0) return;
    unsure = false;
    void read(false);
}

// read reports whether its answer was drawn: false when it failed, and when
// its answer was refused (another archive's, sent before the proof, or older
// than the state shown).
async function read(uncertain: boolean): Promise<boolean> {
    if (ports === null) return false;
    readsOut++;
    try {
        return await readOnce(ports, uncertain);
    } finally {
        readsOut--;
        settle();
    }
}

async function readOnce(p: RecoveryPorts, uncertain: boolean): Promise<boolean> {
    const drawsWhenSent = draws;
    // What a read may draw is decided by when it was SENT, not by what is bound
    // when it returns (codex-228). One sent with no archive proven draws
    // nothing, even if it comes back after the proof and for that very
    // archive: it may be older than the read the proof itself sends, and
    // drawing it would also make that read's failure look obsolete.
    const sentFor = p.archive();
    const sentAs = ++asked;
    const out = await p.read();
    if (out.kind === 'ok') {
        if (sentFor === null) return false;
        if (out.state.archiveId !== sentFor) {
            // Sent with an archive proven, answered for another: positive word
            // that the daemon serves another archive now. It is not drawn, and
            // it starts the page's archive check (codex-275): when the event
            // stream opens on that daemon for the first time, nothing else does.
            p.otherArchive?.(out.state.archiveId);
            return false;
        }
        const drawnNow = take(out.state, sentAs, drawsWhenSent);
        if (drawnNow && !uncertain) recovery.error = '';
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
    const drawsWhenSent = draws;
    const out = await ports.answer(answer, recovery.generation, session.id);
    if (out.kind === 'applied' || out.kind === 'stale') {
        recovery.answering = false;
        take(out.state, sentAs, drawsWhenSent);
        settle();
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
    readsOut = 0;
    unsure = false;
    draws = 0;
}
