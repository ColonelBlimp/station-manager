/*
    Continue's list: the closed sessions the operator can go back to (ADR 0100
    §5; W-0029 part 6, criteria F; the operator's rulings of 2026-11-25 and
    2026-11-27).

    Two things are read, both from GET /v1/operating-session/closed.
      - WHETHER there is anything to continue, which decides if the panel
        offers "Continue…" at all. It is asked when a state is drawn for the
        workspace at a generation it was not asked at, when the event stream
        reopens, and after a Continue that was not applied. Until it has
        answered the control is not offered; a list that could not be read is
        "unreadable", never "none".
      - The dialog's LINES, read from the start of the list each time the
        dialog is opened: the twenty most recent, and "Show more" for the
        rest. Nothing is kept from one opening to the next, so a line on
        screen was read since the dialog opened.

    The rules for the lines (each has its own test):
      - an answer is drawn only if the page was bound to its archive when the
        read was sent and still is. An answer for another archive is reported
        (it starts the page's archive check) and the dialog says the list
        could not be read;
      - an answer to a read sent for an earlier opening, or given up since, is
        never drawn: it supplies no lines and no further pages;
      - pages of two different lists are never joined. A further page is asked
        for with the last line's session and the list's generation; refused
        as stale, the list is read again from its start;
      - a closed session's count changes without the generation moving (a
        contact of it is deleted), so a deletion heard while the dialog is
        open is followed by reading the list again from its start, as far as
        it was shown: page by page, since one read returns at most 1,000, each
        further page named as above, until as many lines are read as were
        shown or the daemon says there are no more. The lines on screen are
        replaced only when all of them have been read. A deletion heard while
        a read is out is followed by one more reading;
      - a read that fails takes away no line. The dialog says the list could
        not be read (again), and offers to try again. While the lines on
        screen are known to be out of date (a reading again failed), "Show
        more" does not append to them: it reads the whole of what is shown
        again, and one page more, so a page that succeeds can never take the
        warning off lines it did not read (codex-290);
      - a line can be chosen only while the list is of the archive and the
        generation of the state the workspace shows, and nothing is being
        read. A list of a NEWER generation is word that the session has moved:
        the state is asked for. A state of another generation drawn while the
        dialog is open is followed by reading the list again;
      - a choice carries what the operator saw: the line's session and the
        LIST's generation, and it is sent only while the page is still bound
        to the list's archive. Choosing closes the dialog; whatever the
        control comes to is shown by the panel.

    ADR 0045: this module never imports lib/api. main.ts injects the read, the
    bound archive, the read of the state and the control (setContinuePorts).
*/

export interface ClosedSessionLike {
    id: string;
    openedAt: string;
    latestSittingStartedAt: string;
    latestSittingEndedAt: string | null;
    anEndUnconfirmed: boolean;
    contactCount: number;
}

export type ClosedListReadLike =
    | {
          kind: 'ok';
          archiveId: string;
          generation: number;
          logbookUuid: string;
          sessions: ClosedSessionLike[];
          more: boolean;
      }
    | { kind: 'stale' }
    | { kind: 'error'; message: string };

export type ContinueEnd = 'applied' | 'stale' | 'failed' | 'unsent';

export interface ContinuePorts {
    read: (
        limit: number,
        after?: { sessionId: string; generation: number }
    ) => Promise<ClosedListReadLike>;
    /** The archive the page is bound to; null while none is proven. */
    archive: () => string | null;
    /** Ask for the session's state to be read and drawn again. */
    readState: () => unknown;
    /** Send Continue for a session, at the generation of the list it was chosen from. */
    send: (generation: number, sessionId: string) => Promise<ContinueEnd>;
    /** Told when a read is answered for another archive than the page is bound to. */
    otherArchive?: (archiveId: string) => void;
}

/** The state the workspace shows, as far as this module needs it. */
export interface ContinueStateLike {
    archiveId: string;
    generation: number;
    state: 'open' | 'none' | 'setup_pending' | 'recovery_pending' | 'archive_switching';
}

export const continueList: {
    /** Whether there is anything to continue: not yet known ("unread"), yes,
     *  no, or the list could not be read. The panel offers "Continue…" for
     *  "some" and for "unreadable". */
    available: 'unread' | 'some' | 'none' | 'unreadable';
    /** The dialog is open. */
    open: boolean;
    /** The lines read since the dialog opened, the most recent first. */
    lines: ClosedSessionLike[];
    /** There are sessions after the last line. */
    more: boolean;
    /** A read for the dialog is out. */
    reading: boolean;
    /** Why the list could not be read, or read again; '' when nothing went wrong. */
    error: string;
    /** Why no line can be chosen now: a read is out; or the list is of another
     *  generation or archive than the state shown. '' when one can. */
    held: '' | 'reading' | 'moved';
} = $state({
    available: 'unread',
    open: false,
    lines: [],
    more: false,
    reading: false,
    error: '',
    held: '',
});

const PAGE = 20;
// The most one read may ask for (internal/api, closedSessionsMaxLimit).
const MOST = 1000;
// How many times a reading starts again because the list changed under it,
// before the dialog says so and leaves it to "Try again".
const RESTARTS = 3;

const OTHER_ARCHIVE = 'The daemon answered for another archive than this page is bound to.';
const KEPT_CHANGING = 'The list kept changing while it was being read.';

let ports: ContinuePorts | null = null;
// The state the workspace shows, while it is one the controls are offered in.
let shown: { archiveId: string; generation: number } | null = null;
// What the answer to "is there anything to continue" was read at.
let availableAt: { archiveId: string; generation: number } | null = null;
let availableOut = false;
let availableAgain = false;
// Counts the times the question was dropped (the state left the workspace): an
// answer to a read sent before is not drawn.
let availableEpoch = 0;
// Counts the readings started for the dialog. A reading that is not the latest
// draws nothing: the dialog was closed, or a later reading replaced it.
let run = 0;
// What the lines on screen are of.
let list: { archiveId: string; generation: number } | null = null;
// Something happened while a reading was out that it may not have seen.
let again = false;
// A reading again failed with lines on screen: they may be out of date, and
// only a reading that replaces them makes them good.
let outOfDate = false;
// The generation the state was last asked for, so that it is asked once.
let askedStateFor: number | null = null;

/** Inject the reads, the bound archive and the control (main.ts). */
export function setContinuePorts(p: ContinuePorts): void {
    ports = p;
}

const same = (
    a: { archiveId: string; generation: number },
    b: { archiveId: string; generation: number }
): boolean => a.archiveId === b.archiveId && a.generation === b.generation;

/** Whether a line may be chosen at this moment. */
export function continueChoosable(): boolean {
    if (ports === null || !continueList.open || continueList.reading) return false;
    if (list === null || shown === null || !same(list, shown)) return false;
    return ports.archive() === list.archiveId;
}

// mark says why no line can be chosen, from what is known now. It is called
// after everything that can change the answer.
function mark(): void {
    if (!continueList.open) continueList.held = '';
    else if (continueList.reading) continueList.held = 'reading';
    else if (list !== null && !continueChoosable()) continueList.held = 'moved';
    else continueList.held = '';
}

async function read(
    p: ContinuePorts,
    limit: number,
    after?: { sessionId: string; generation: number }
): Promise<ClosedListReadLike> {
    try {
        return await p.read(limit, after);
    } catch (e) {
        return { kind: 'error', message: e instanceof Error ? e.message : String(e) };
    }
}

// Whether an answer is of the archive the page was bound to when its read was
// sent, and still is. One that names another archive is reported.
function ofThisArchive(p: ContinuePorts, answered: string, sentFor: string | null): boolean {
    if (sentFor !== null && answered === sentFor && p.archive() === sentFor) return true;
    if (sentFor !== null && answered !== sentFor) p.otherArchive?.(answered);
    return false;
}

// A list of a newer generation than the state shown: the session has moved, and
// the state is asked for, once for that generation.
function ifNewerAskForTheState(p: ContinuePorts, generation: number): void {
    if (shown === null || generation <= shown.generation || askedStateFor === generation) return;
    askedStateFor = generation;
    void p.readState();
}

function askAvailable(): void {
    if (ports === null || shown === null) return;
    if (availableOut) {
        availableAgain = true;
        return;
    }
    void readAvailable(ports);
}

async function readAvailable(p: ContinuePorts): Promise<void> {
    availableOut = true;
    const epoch = availableEpoch;
    const sentFor = p.archive();
    const out = await read(p, 1);
    availableOut = false;
    if (epoch === availableEpoch && shown !== null) {
        if (out.kind !== 'ok') {
            continueList.available = 'unreadable';
            availableAt = null;
        } else if (ofThisArchive(p, out.archiveId, sentFor)) {
            continueList.available = out.sessions.length > 0 ? 'some' : 'none';
            availableAt = { archiveId: out.archiveId, generation: out.generation };
            ifNewerAskForTheState(p, out.generation);
        }
    }
    if (availableAgain) {
        availableAgain = false;
        askAvailable();
    }
}

/**
 * The recovery gate has drawn a state. In a state where the panel offers its
 * controls (a session open, or none), whether there is anything to continue is
 * asked for that generation; in any other the question is dropped and the
 * dialog closed.
 */
export function continueStateDrawn(state: ContinueStateLike): void {
    if (state.state !== 'open' && state.state !== 'none') {
        shown = null;
        availableAt = null;
        availableEpoch++;
        availableAgain = false;
        continueList.available = 'unread';
        closeContinue();
        return;
    }
    const now = { archiveId: state.archiveId, generation: state.generation };
    const moved = shown === null || !same(shown, now);
    shown = now;
    if (availableAt === null || !same(availableAt, now)) askAvailable();
    if (continueList.open && moved && !(list !== null && same(list, now))) readAgain();
    mark();
}

/** The event stream has opened or reopened: changes made while it was down
 *  were announced to nobody. */
export function continueStreamReopened(): void {
    askAvailable();
    if (continueList.open) readAgain();
}

/**
 * A contact was deleted. It may have been a closed session's, whose count is
 * then lower with no generation moved. Only the lines on screen can be wrong:
 * with the dialog closed there are none.
 */
export function continueContactDeleted(): void {
    if (continueList.open) readAgain();
}

// As far as the list is shown, and never less than the first page.
function extent(): number {
    return Math.max(PAGE, continueList.lines.length);
}

// Read the list again from its start, as far as it is shown; or, with a
// reading out, once more when that one ends.
function readAgain(): void {
    if (ports === null) return;
    if (continueList.reading) {
        again = true;
        return;
    }
    void readList(ports, extent());
}

type Pass =
    | { kind: 'read'; lines: ClosedSessionLike[]; more: boolean; generation: number }
    | { kind: 'failed'; message: string }
    | { kind: 'stale' }
    | { kind: 'given-up' };

// One pass over the list from its start: page by page until `want` lines are
// read or the daemon says there are no more.
async function pass(p: ContinuePorts, want: number, mine: number): Promise<Pass> {
    const sentFor = p.archive();
    const lines: ClosedSessionLike[] = [];
    let generation: number | null = null;
    for (;;) {
        const last = lines.at(-1);
        const after =
            last !== undefined && generation !== null
                ? { sessionId: last.id, generation }
                : undefined;
        const out = await read(p, Math.min(want - lines.length, MOST), after);
        if (mine !== run) return { kind: 'given-up' };
        if (out.kind === 'stale') return { kind: 'stale' };
        if (out.kind === 'error') return { kind: 'failed', message: out.message };
        if (!ofThisArchive(p, out.archiveId, sentFor)) {
            return { kind: 'failed', message: OTHER_ARCHIVE };
        }
        if (generation !== null && out.generation !== generation) return { kind: 'stale' };
        generation = out.generation;
        lines.push(...out.sessions);
        if (!out.more || lines.length >= want || out.sessions.length === 0) {
            return { kind: 'read', lines, more: out.more, generation };
        }
    }
}

async function readList(p: ContinuePorts, want: number): Promise<void> {
    const mine = ++run;
    again = false;
    continueList.reading = true;
    mark();
    let got: Pass = { kind: 'stale' };
    for (let n = 0; n <= RESTARTS && got.kind === 'stale'; n++) got = await pass(p, want, mine);
    if (got.kind === 'given-up' || mine !== run) return;
    continueList.reading = false;
    if (got.kind === 'read') {
        const of = p.archive();
        continueList.lines = got.lines;
        continueList.more = got.more;
        continueList.error = '';
        outOfDate = false;
        if (of !== null) {
            list = { archiveId: of, generation: got.generation };
            // What the dialog read also says whether there is anything to continue.
            continueList.available = got.lines.length > 0 ? 'some' : 'none';
            availableAt = { ...list };
        }
        ifNewerAskForTheState(p, got.generation);
    } else {
        continueList.error = got.kind === 'failed' ? got.message : KEPT_CHANGING;
        outOfDate = continueList.lines.length > 0;
    }
    mark();
    if (again) readAgain();
}

/** Open the dialog: the list is read from its start. */
export function openContinue(): void {
    if (ports === null || shown === null || continueList.open) return;
    continueList.open = true;
    askedStateFor = null;
    void readList(ports, PAGE);
}

/** Close the dialog. Nothing of it is kept, and a read that is out is given up. */
export function closeContinue(): void {
    continueList.open = false;
    run++;
    again = false;
    list = null;
    outOfDate = false;
    continueList.lines = [];
    continueList.more = false;
    continueList.reading = false;
    continueList.error = '';
    mark();
}

/** "Show more": the page that follows the last line, of the list shown. */
export function showMoreContinue(): void {
    if (ports === null || !continueList.open || continueList.reading) return;
    if (!continueList.more || list === null) return;
    if (outOfDate) {
        void readList(ports, continueList.lines.length + PAGE);
        return;
    }
    void readMore(ports, list);
}

async function readMore(
    p: ContinuePorts,
    of: { archiveId: string; generation: number }
): Promise<void> {
    const last = continueList.lines.at(-1);
    if (last === undefined) return;
    const mine = ++run;
    again = false;
    continueList.reading = true;
    mark();
    const sentFor = p.archive();
    const out = await read(p, PAGE, { sessionId: last.id, generation: of.generation });
    if (mine !== run) return;
    continueList.reading = false;
    if (out.kind === 'stale' || (out.kind === 'ok' && out.generation !== of.generation)) {
        // Another list than the one shown: read it from its start, as far as
        // the operator has now asked to see.
        await readList(p, continueList.lines.length + PAGE);
        return;
    }
    if (out.kind === 'error') continueList.error = out.message;
    else if (!ofThisArchive(p, out.archiveId, sentFor)) continueList.error = OTHER_ARCHIVE;
    else {
        continueList.lines = [...continueList.lines, ...out.sessions];
        continueList.more = out.more;
        continueList.error = '';
    }
    mark();
    if (again) readAgain();
}

/** "Try again": read the list again from its start, as far as it is shown. */
export function retryContinue(): void {
    if (ports === null || !continueList.open || continueList.reading) return;
    askedStateFor = null;
    continueList.error = '';
    void readList(ports, extent());
}

/**
 * Choose a line. The control carries that line's session and the generation
 * of the list on screen, and is sent only while a line may be chosen
 * (continueChoosable). The dialog closes; the panel shows what it came to.
 */
export async function chooseContinue(sessionId: string): Promise<void> {
    if (ports === null || list === null || !continueChoosable()) return;
    if (!continueList.lines.some((l) => l.id === sessionId)) return;
    const p = ports;
    const generation = list.generation;
    closeContinue();
    const end = await p.send(generation, sessionId);
    // Not applied: the list may be another now (the target was refused, the
    // default logbook changed), with no state drawn to say so.
    if (end !== 'applied') askAvailable();
}

/** Test seam — restore the singleton between cases. */
export function _resetContinueForTests(): void {
    ports = null;
    shown = null;
    availableAt = null;
    availableOut = false;
    availableAgain = false;
    availableEpoch++;
    askedStateFor = null;
    continueList.available = 'unread';
    closeContinue();
}
