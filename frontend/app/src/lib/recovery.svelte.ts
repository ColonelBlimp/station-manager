/*
    The recovery gate: what the main window shows for the operating session's
    state (ADR 0100 §3, §6; W-0029 part 4, criteria E).

    After a start that did not follow a confirmed exit, the archive can hold a
    session with contacts that an earlier start left open. The daemon names
    that state `recovery_pending`, and the page then shows one question in
    place of the workspace: continue that session, or start a new one.

    Status:
      - loading     the state has not been read on this load, or two answers
                    whose order is not known disagree on whether the workspace
                    may be shown: nothing of the workspace is shown, and no
                    question;
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
        was out while another answer of the same generation arrived, the two
        are IN DOUBT, and the page reads once more, sending that read only
        when no read is out. Every read has answered by then, so the daemon
        serves it after all of them, and its answer is what the page ends on.
        Reads that did not overlap are in order and nothing more is read. A
        read sent before an answer cannot put the old question back;
      - answers in doubt that agree on whether the workspace may be shown are
        drawn as they come. When they DISAGREE (one says recovery is pending,
        the other does not), neither is shown: the page shows what it shows
        before any state is read, hands the Session nothing, and stays so
        until an answer that is not in doubt arrives. If the read that should
        decide fails, the page says the session could not be read and offers
        to try again. A workspace is never opened, and a question never asked,
        on an answer the page knows may be the older (codex-277);
      - a state is drawn only for the archive the boot has PROVEN. One read
        before that proof, or for another archive, is not drawn and is not
        kept: the page stays as it was. A read SENT before the proof draws
        nothing whenever it returns (codex-228). When the proof comes the state
        is read then, with the workspace still withheld until it is known. An earlier
        archive's "open" must never stand as permission to show this archive's
        workspace (codex-226), and an answer that arrived before the proof may
        be older than one that arrived before it (the clean-room review of
        2c362c3d), so nothing from before the proof is drawn at it;
      - nothing sent before the event stream's connection last changed (it
        was lost, or it opened) is drawn after it. What the daemon says of a
        session depends on which app is admitted, and that can change across
        a connection; the page reads afresh at every opening, and an answer
        from before must not open the workspace, or dismiss the question,
        while the replacement is connecting or that read is out (codex-281,
        codex-282). A READ from before is dropped, answered or failed. An
        ANSWER to the question from before is not shown, but what it did is
        then not known: nothing is shown until a read sent after it arrived
        has answered, and a read that was already out decides nothing. An
        answer that FAILED is not part of this: as ever, the page says what
        went wrong and reads;
      - a read that fails takes away nothing that was drawn. It makes the
        page unreadable only when nothing has been drawn yet, or when an
        answer has just failed and what the daemon holds is not known; and
        only if nothing has been drawn since that read was sent (codex-226);
      - the question's count is good only if no contact was stored into a
        session or deleted, that the page has heard of, since the request
        that supplied it was sent; else one read is owed for it (codex-279,
        codex-280). If THAT read fails, the question says what went wrong,
        with the read's own message, on the line it has for it: the count
        shown is the last one received. The choices stay and the read is
        not repeated; the next read drawn clears it (the operator's ruling,
        2026-11-27);
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
// The state last kept: its archive and generation, the number its request took
// (`asked`), whether it lets the workspace show (`mayShow`), and the count of
// arrivals when the latest answer of this generation arrived, kept or not
// (`at`). An answer whose request was sent before that is in doubt.
let drawn: {
    archiveId: string;
    generation: number;
    asked: number;
    mayShow: boolean;
    at: number;
} | null = null;
// Counts the answers that arrived for the archive bound, whatever was done
// with them. A request notes it when it is sent.
let arrivals = 0;
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
// Answers in doubt disagree on whether the workspace may be shown. Nothing is
// shown until an answer that is not in doubt arrives.
let torn = false;
// Counts the changes of membership the page has heard of: a contact stored
// into a session, a contact deleted. A request notes it when it is sent. The
// question's count is good only if none has been heard since the request that
// supplied it was sent, whether that request was a read or an answer.
let changes = 0;
// The question shown has a count from before a change heard of since. One read
// is owed for it, sent when no read is out; it is the same single read that
// settles answers in doubt.
let recount = false;
// Counts the times the event stream was lost or opened: its connection
// changed. A request notes it when it is sent, and its answer is drawn only if
// the connection has not changed since (codex-281, codex-282).
let links = 0;
// How many reads have been sent since the connection last changed.
let readsThisLink = 0;
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

// take is the one rule (E5): every answer, from a read, an applied answer or a
// stale refusal, comes through it. It reports whether the state was drawn.
// `sentAs` is the number the request took when it was sent, `arrivalsWhenSent`
// the count of arrivals then, and `changesWhenSent` the count of membership
// changes heard of by then.
//
// `oldLink` is true for an ANSWER to the question that was sent before the
// connection changed (a read like that is dropped before it gets here). Its
// state is recorded, so that every request sent before it arrived is in doubt
// against it, and it is never shown: what the answer did is not known from it,
// and nothing is shown until a read sent after it has answered (codex-282).
function take(
    state: SessionStateLike,
    sentAs: number,
    arrivalsWhenSent: number,
    changesWhenSent: number,
    oldLink = false
): boolean {
    const bound = ports?.archive() ?? null;
    if (bound === null || state.archiveId !== bound) return false;
    arrivals++;
    const prior = drawn !== null && drawn.archiveId === state.archiveId ? drawn : null;
    if (prior !== null && state.generation < prior.generation) return false;
    const same = prior !== null && state.generation === prior.generation;
    // In doubt: another answer of this generation arrived while this one's
    // request was out. Whichever of the two is kept, the daemon may have
    // served the other last. An answer of a later generation is ordered by it.
    const doubt = same && prior.at > arrivalsWhenSent;
    // The same generation: the one asked for later is kept.
    const kept = !(same && sentAs < prior.asked);
    const mayShow = !asksTheQuestion(state);
    noteOrder(doubt, doubt && mayShow !== prior.mayShow, oldLink);
    if (same) prior.at = arrivals;
    if (kept) {
        drawn = {
            archiveId: state.archiveId,
            generation: state.generation,
            asked: sentAs,
            mayShow,
            at: arrivals,
        };
    }
    if (torn) {
        withhold();
        return false;
    }
    if (!kept) return false;
    show(state, changesWhenSent);
    return true;
}

// Show neither the question nor the workspace: what the page shows before any
// state is read. A page that already says the session could not be read goes
// on saying so.
function withhold(): void {
    if (recovery.status !== 'question' && recovery.status !== 'clear') return;
    recovery.status = 'loading';
    recovery.session = null;
}

// Recovery is pending, for a session: the state that is the question. Every
// other state lets the workspace show.
function asksTheQuestion(state: SessionStateLike): boolean {
    return state.state === 'recovery_pending' && state.session !== null;
}

// noteOrder records what is known of an answer's order as it arrives. In
// doubt: one read is owed. In doubt and disagreeing with the state kept on
// whether the workspace may show: nothing is shown. Not in doubt: it is the
// newest, and the withholding ends. An answer from an old connection is always
// in doubt and always withheld.
function noteOrder(doubt: boolean, disagrees: boolean, oldLink: boolean): void {
    if (doubt || oldLink) unsure = true;
    if (disagrees || oldLink) torn = true;
    else if (!doubt) torn = false;
}

function show(state: SessionStateLike, changesWhenSent: number): void {
    draws++;
    recovery.generation = state.generation;
    ports?.drawn?.(state);
    if (state.state === 'recovery_pending' && state.session !== null) {
        // The daemon took the count at some time AFTER this request was sent.
        // A contact stored or deleted that the page heard of since the request
        // was sent may or may not be in it: one read settles it (codex-279,
        // codex-280).
        if (changesWhenSent < changes) recount = true;
        recovery.status = 'question';
        recovery.session = {
            id: state.session.id,
            openedAt: state.session.openedAt,
            contactCount: state.session.contactCount,
        };
        return;
    }
    recovery.status = 'clear';
    recovery.session = null;
}

// With an order not known and no read out, read once. That read is sent after
// every answer arrived. If another request overlaps it in its turn, the same
// rule sends one more after that; it ends when nothing else is asking. A read
// that fails is not repeated: nothing here asks an unreachable daemon in a loop.
// The same one read serves a question whose count is from before a contact
// stored or deleted since.
// When the answers in doubt disagreed, the page shows nothing while this read
// is out, and its failure is the failure of a read with nothing drawn: the page
// says the session could not be read, and "Try again" reads it.
function settle(): void {
    if (readsOut > 0) return;
    // A count is owed only to a question that is showing. With the workspace
    // showing, or the session unreadable, nothing shows a count, and any read
    // sent from now on is sent after the change.
    const forCount = recount && recovery.status === 'question';
    const again = unsure || forCount;
    unsure = false;
    recount = false;
    if (again) void read(false, forCount);
}

// read reports whether its answer was drawn: false when it failed, and when
// its answer was refused (another archive's, sent before the proof, or older
// than the state shown).
//
// `forCount` marks the read the page owes for the question's count. If it
// fails with the question still showing and nothing drawn since, the question
// says what went wrong on the line it has for that, with the read's own
// message; both choices stay, and the next read that is drawn clears it (the
// operator's ruling, 2026-11-27). Any other read that fails with a question
// showing says nothing, as ever.
async function read(uncertain: boolean, forCount = false): Promise<boolean> {
    if (ports === null) return false;
    readsOut++;
    let end: ReadEnd = 'undrawn';
    try {
        end = await readOnce(ports, uncertain, forCount);
        return end === 'drawn';
    } finally {
        readsOut--;
        settle();
        // An answer dropped for its connection, with nothing shown and no
        // read sent since the connection changed (settle may just have sent
        // one): that is a LOSS, after which nothing reads until the stream
        // opens. The page says what it knows and offers "Try again" rather
        // than stay blank. (An opening always sends a read; what that read
        // found stands. A read sent during the outage is waited for.)
        if (end === 'dropped' && readsThisLink === 0 && recovery.status === 'loading') {
            recovery.status = 'unreadable';
            recovery.session = null;
        }
    }
}

// How a read ended: its answer was drawn; it was not (it failed, or was refused
// or set aside or withheld); or it was dropped for its connection.
type ReadEnd = 'drawn' | 'undrawn' | 'dropped';

async function readOnce(p: RecoveryPorts, uncertain: boolean, forCount: boolean): Promise<ReadEnd> {
    const drawsWhenSent = draws;
    const arrivalsWhenSent = arrivals;
    const changesWhenSent = changes;
    // What a read may draw is decided by when it was SENT, not by what is bound
    // when it returns (codex-228). One sent with no archive proven draws
    // nothing, even if it comes back after the proof and for that very
    // archive: it may be older than the read the proof itself sends, and
    // drawing it would also make that read's failure look obsolete.
    const sentFor = p.archive();
    const sentAs = ++asked;
    const linkWhenSent = links;
    readsThisLink++;
    const out = await p.read();
    // Sent before the event stream's connection last changed: it says nothing
    // now, whether it answered or failed. An opening sends the read to draw.
    if (linkWhenSent !== links) return 'dropped';
    if (out.kind === 'ok') {
        if (sentFor === null) return 'undrawn';
        if (out.state.archiveId !== sentFor) {
            // Sent with an archive proven, answered for another: positive word
            // that the daemon serves another archive now. It is not drawn, and
            // it starts the page's archive check (codex-275): when the event
            // stream opens on that daemon for the first time, nothing else does.
            p.otherArchive?.(out.state.archiveId);
            return 'undrawn';
        }
        const drawnNow = take(out.state, sentAs, arrivalsWhenSent, changesWhenSent);
        if (drawnNow && !uncertain) recovery.error = '';
        return drawnNow ? 'drawn' : 'undrawn';
    }
    if (draws !== drawsWhenSent) return 'undrawn'; // obsolete: a state has been drawn since this was sent
    const nothingDrawn = recovery.status === 'loading' || recovery.status === 'unreadable';
    if (nothingDrawn || uncertain) {
        recovery.status = 'unreadable';
        recovery.session = null;
        recovery.error = out.message;
    } else if (forCount && recovery.status === 'question') {
        // The count shown is the last one received: say that reading it
        // again failed. The question and its choices stay.
        recovery.error = out.message;
    }
    return 'undrawn';
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

/**
 * A contact was deleted. The question says how many contacts the session has,
 * and while it shows nothing else reads the state again: no session is shown,
 * so the Session reads no rows (codex-277). The state is read, once no read is
 * out; the daemon's count is what the question then says, whichever session
 * the contact was in. With the workspace showing, the Session reads its own
 * rows and nothing is asked here.
 *
 * With no question showing the deletion is only counted. If the question is
 * then drawn from a request sent before it (the read that decides between two
 * answers in doubt, the load's read, a read after the session was said to have
 * changed, the refusal of an answer), one read follows (codex-279, codex-280).
 */
export function recoveryContactDeleted(): void {
    changes++;
    if (recovery.status !== 'question') return;
    recount = true;
    settle();
}

/**
 * A contact was stored into a session. It is counted, so that a question drawn
 * from a request sent before it is read once more (codex-280: a contact joins
 * the session shown while a read is out that brings the question; joining a
 * session moves no generation). Nothing is asked here: with the question
 * showing, the Session reads the state for a contact stored into a session it
 * does not show, and main.ts calls this BEFORE it tells the Session, so that
 * read is sent after the contact was counted and its answer is current.
 */
export function recoveryContactStored(): void {
    changes++;
}

/**
 * The event stream has opened, for the first time or again. main.ts calls
 * this BEFORE it tells the Session, whose read of the state is then the first
 * request of the new connection. Nothing sent before now is drawn after it.
 */
export function recoveryStreamOpened(): void {
    links++;
    readsThisLink = 0;
}

/**
 * The event stream was lost. Nothing sent before now is drawn after it: an
 * answer from before the loss must not open the workspace, or dismiss the
 * question, while the replacement is connecting (codex-282). A read sent
 * during the outage is drawn if it answers during it; the opening that ends
 * the outage reads afresh, and that read replaces it.
 */
export function recoveryStreamLost(): void {
    links++;
    readsThisLink = 0;
}

/** Send the operator's answer to the question that is showing. */
export async function answerRecovery(answer: RecoveryAnswer): Promise<void> {
    if (ports === null || recovery.status !== 'question' || recovery.answering) return;
    const session = recovery.session;
    if (session === null) return;
    recovery.answering = true;
    recovery.error = '';
    const sentAs = ++asked;
    const arrivalsWhenSent = arrivals;
    const changesWhenSent = changes;
    const linkWhenSent = links;
    const out = await ports.answer(answer, recovery.generation, session.id);
    if (out.kind === 'applied' || out.kind === 'stale') {
        recovery.answering = false;
        take(out.state, sentAs, arrivalsWhenSent, changesWhenSent, linkWhenSent !== links);
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
    torn = false;
    recount = false;
    changes = 0;
    links = 0;
    readsThisLink = 0;
    arrivals = 0;
    draws = 0;
}
