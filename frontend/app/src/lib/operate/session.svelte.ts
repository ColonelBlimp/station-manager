/*
    The Session as the page shows it: the daemon's operating session and its
    contacts (ADR 0100 §7; W-0029 part 6, criteria A to D).

    The daemon owns the session. This module holds what the page has READ of
    it and nothing else: no row is added because this window logged a contact,
    and nothing is kept in the browser's storage. The panel, the rail's count,
    the Export dialog, the in-place edit and Band Activity's worked check all
    read `session.qsos`, so they cannot disagree.

    Two things are read.
      - The session's STATE (which session is open, at which generation, when
        it started) is read and drawn by the recovery gate (recovery.svelte.ts),
        by its one rule. Each state it draws is handed here (sessionStateDrawn).
      - The session's ROWS are read here (GET /v1/operating-session/members),
        whole, newest first, as the daemon orders them.

    The rules for the rows (each has its own test):
      - an event carries no row. A contact stored, edited or deleted, an email
        stamp, this window's own submit or edit: each only asks for the rows to
        be read again (refreshSessionRows). So an event from a daemon whose
        archive the page has not proven can change nothing: what is drawn is an
        answer, and an answer names its archive (B9);
      - an answer is drawn only if the page was bound to its archive when the
        read was sent and still is, and it is of the session and generation the
        page shows. Anything else is dropped, and the state is read again (B2);
      - one read is out at a time. A change that arrives while it is out is
        followed by ONE more read, for everything that arrived during it. The
        reads are therefore answered in the order they were sent, each from a
        later snapshot than the one before: a late answer cannot undo a newer
        one, cannot put back a contact deleted since, and cannot leave two
        contacts in another order than the daemon's (B3, B4, B10);
      - when the event stream reopens, or the session shown changes, a read
        that is still out is given up (its answer is never drawn) and a new one
        is sent at once: changes made while the stream was down were announced
        to nobody, and a read sent on the earlier connection may be older than
        them (B3, B6);
      - when the session shown changes, its rows are gone before the new ones
        are read: no row of the session before stays, even for a moment (B5);
      - a read that fails takes away nothing drawn. With no rows on screen,
        the panel says the rows could not be read: never that the session is
        empty, even when the last answer was an empty list, and never that it
        is still reading once nothing is being read (B7). It is tried again at
        the next change or reopening;
      - after an email is sent from the rows on screen, the "emailed" marks
        are older than the send until a read TAKEN AFTER it has answered. The
        Session says so (emailPending), and the Export dialog sends nothing
        more until then: the contact's record is what says it was emailed, and
        the window does not guess.

    ADR 0045: this module never imports lib/api. main.ts injects the read of
    the rows, the read of the state and the bound archive (setSessionPorts).
*/

import { registerArchiveStore } from './archiveStores.svelte';

export interface SessionQso {
    /** Stable for the page's life for one contact (the list's key). */
    id: number;
    /** The contact's UUID (ADR 0016): what an edit, an export and an email name. */
    uuid?: string;
    callsign: string;
    timeOn: string; // HH:MM:SS (UTC)
    band: string;
    mode: string; // the operator-friendly literal: the ADIF submode when there is one
    rstSent: string;
    rstRcvd: string;
    name: string;
    country: string; // '' when unknown
    comment: string;
    /** The contact's own record says it was forwarded by email (ruling 6). */
    emailed?: boolean;
}

/** One contact as the daemon's read gives it. */
export interface SessionRowLike {
    uuid: string;
    call: string;
    /** ADIF HHMM or HHMMSS. */
    timeOn: string;
    band: string;
    mode: string;
    submode?: string;
    rstSent: string;
    rstRcvd: string;
    name: string;
    country: string;
    comment: string;
    emailed: boolean;
}

export type SessionRowsReadLike =
    | {
          kind: 'ok';
          archiveId: string;
          generation: number;
          sessionId: string | null;
          members: SessionRowLike[];
      }
    | { kind: 'error'; message: string };

/** The session's state as the recovery gate draws it. */
export interface DrawnSessionState {
    archiveId: string;
    generation: number;
    state: 'open' | 'none' | 'setup_pending' | 'recovery_pending' | 'archive_switching';
    session: { id: string; openedAt: string; sittingStartedAt: string | null } | null;
}

export interface SessionPorts {
    readRows: () => Promise<SessionRowsReadLike>;
    /** Ask for the session's state to be read and drawn again. It settles when
     *  that read has finished, whether it drew a state or failed. */
    readState: () => Promise<void>;
    /** The archive the page is bound to; null while none is proven. */
    archive: () => string | null;
}

/** What the panel says of the rows of the session shown. */
export type SessionRowsStatus = 'none' | 'loading' | 'ok' | 'failed';

export const session: {
    /** The rows drawn, newest stored first, as the daemon orders them. */
    qsos: SessionQso[];
    /** The state last drawn; null until one has been. */
    state: DrawnSessionState['state'] | null;
    /** The open session shown; null when none is. */
    sessionId: string | null;
    /** The session's first start (RFC 3339); null with no session shown. */
    openedAt: string | null;
    /** The start of the sitting now open (RFC 3339); null with none. */
    sittingStartedAt: string | null;
    rows: SessionRowsStatus;
    /** Why the rows could not be read; '' when nothing went wrong. */
    error: string;
    /** An email was sent from these rows and no read taken after it has been
     *  drawn: the "emailed" marks on screen may be older than the send. */
    emailPending: boolean;
} = $state({
    qsos: [],
    state: null,
    sessionId: null,
    openedAt: null,
    sittingStartedAt: null,
    rows: 'none',
    error: '',
    emailPending: false,
});

let ports: SessionPorts | null = null;
// The session and generation the rows on screen are of.
let shown: { archiveId: string; sessionId: string; generation: number } | null = null;
// Counts the reads given up. A read notes it when it is sent: if it has moved
// by the time the answer comes, that read was given up and nothing of it is
// drawn.
let epoch = 0;
let out = false; // a read is out
let again = false; // a change arrived while it was out
// Email sends from this window, counted; and the count a drawn read was sent
// at. While the first is ahead, no read taken after the last send has been drawn.
let emailSends = 0;
let emailSendsRead = 0;
// One number per contact for the page's life, so a row keeps its place in the
// keyed list from one read to the next.
// A plain record, not a Map: nothing reads it reactively, and it is cleared by
// being replaced.
let ids: Record<string, number> = {};
let nextId = 1;

const KEY_PREFIX = 'sm.session.qsos.';
const TIMER_KEY = 'sm.session.startedAt';

/** Inject the reads and the bound archive (main.ts). */
export function setSessionPorts(p: SessionPorts): void {
    ports = p;
}

// What earlier builds kept in the tab's storage (a list for each archive, and
// the clock's start) is removed and never shown (B8).
function removeEarlierBuildsStorage(): void {
    try {
        const stale: string[] = [];
        for (let i = 0; i < sessionStorage.length; i++) {
            const k = sessionStorage.key(i);
            if (k !== null && (k.startsWith(KEY_PREFIX) || k === TIMER_KEY)) stale.push(k);
        }
        for (const k of stale) sessionStorage.removeItem(k);
    } catch {
        // storage unavailable: there is nothing of an earlier build to remove
    }
}

function forget(): void {
    shown = null;
    epoch++; // a read that is out belongs to what was shown before
    again = false;
    session.qsos = [];
    session.state = null;
    session.sessionId = null;
    session.openedAt = null;
    session.sittingStartedAt = null;
    session.rows = 'none';
    session.error = '';
    emailSendsRead = emailSends;
    session.emailPending = false;
}

registerArchiveStore({
    load() {
        removeEarlierBuildsStorage();
    },
    clear() {
        forget();
        ids = {};
        nextId = 1;
    },
});

/** The operator-friendly mode literal a session row shows (USB, FT8, FT4): the
 *  ADIF submode when the record has one, the mode otherwise — the same rule the
 *  logbook renders pairs by, so an FT4 contact filed as MFSK/FT4 reads "FT4". */
export function sessionModeLiteral(mode?: string, submode?: string): string {
    return submode !== undefined && submode !== '' ? submode : (mode ?? '');
}

/** ADIF HHMM[SS] → the list's HH:MM[:SS]; anything else is shown as it is. */
export function sessionDisplayTime(t: string): string {
    if (!/^\d{4}(\d{2})?$/.test(t)) return t;
    const parts = [t.slice(0, 2), t.slice(2, 4)];
    if (t.length === 6) parts.push(t.slice(4, 6));
    return parts.join(':');
}

function idFor(uuid: string): number {
    ids[uuid] ??= nextId++;
    return ids[uuid];
}

function toRow(m: SessionRowLike): SessionQso {
    return {
        id: idFor(m.uuid),
        uuid: m.uuid,
        callsign: m.call,
        timeOn: sessionDisplayTime(m.timeOn),
        band: m.band,
        mode: sessionModeLiteral(m.mode, m.submode),
        rstSent: m.rstSent,
        rstRcvd: m.rstRcvd,
        name: m.name,
        country: m.country,
        comment: m.comment,
        emailed: m.emailed,
    };
}

/**
 * The recovery gate has drawn a state (its rule has already refused one of
 * another archive, or older than the one shown). The session shown follows it.
 */
export function sessionStateDrawn(state: DrawnSessionState): void {
    session.state = state.state;
    const open = state.state === 'open' ? state.session : null;
    if (open === null) {
        shown = null;
        epoch++;
        again = false;
        session.qsos = [];
        session.sessionId = null;
        session.openedAt = null;
        session.sittingStartedAt = null;
        session.rows = 'none';
        session.error = '';
        emailSendsRead = emailSends; // no session shown: nothing can be sent from it
        session.emailPending = false;
        return;
    }
    session.openedAt = open.openedAt;
    session.sittingStartedAt = open.sittingStartedAt;
    if (
        shown !== null &&
        shown.archiveId === state.archiveId &&
        shown.sessionId === open.id &&
        shown.generation === state.generation
    ) {
        return; // the session and generation already shown: its rows stand
    }
    // Another session, or this one at another generation (New and then
    // Continue bring a session back at a new one): its rows are read again,
    // not kept, and nothing of the session before stays while they are.
    shown = { archiveId: state.archiveId, sessionId: open.id, generation: state.generation };
    session.sessionId = open.id;
    session.qsos = [];
    session.rows = 'loading';
    session.error = '';
    emailSendsRead = emailSends; // its rows are all read after any send so far
    session.emailPending = false;
    readAfresh();
}

// Give up a read that is out and send one now.
function readAfresh(): void {
    epoch++;
    out = false;
    again = false;
    void read();
}

async function read(): Promise<void> {
    if (ports === null || shown === null) return;
    if (out) {
        again = true;
        return;
    }
    out = true;
    const sentEpoch = epoch;
    const sentFor = ports.archive();
    const sentAtSends = emailSends;
    let answer: SessionRowsReadLike;
    try {
        answer = await ports.readRows();
    } catch (e) {
        answer = { kind: 'error', message: e instanceof Error ? e.message : String(e) };
    }
    if (sentEpoch !== epoch) return; // given up: a later read is the one to draw
    out = false;
    if (draw(answer, sentFor)) {
        // This read was taken after every send counted when it was sent.
        emailSendsRead = Math.max(emailSendsRead, sentAtSends);
        session.emailPending = emailSends > emailSendsRead;
    }
    if (again) {
        again = false;
        void read();
    }
}

const STATE_UNREAD =
    'The daemon answered for another session than the one shown, and the session could not be read again.';

// draw reports whether the answer was drawn as the rows of the session shown.
function draw(answer: SessionRowsReadLike, sentFor: string | null): boolean {
    if (ports === null || shown === null) return false;
    if (answer.kind === 'error') {
        // Rows on screen stay. With none on screen the failure is said, even
        // when the last answer was an empty list: the page asked because
        // something may have changed, and "no contacts" is no longer known.
        if (session.qsos.length === 0) {
            session.rows = 'failed';
            session.error = answer.message;
        }
        return false;
    }
    if (sentFor === null || answer.archiveId !== sentFor || ports.archive() !== sentFor) {
        return false; // another archive's rows, or the page's archive was not proven
    }
    if (answer.sessionId !== shown.sessionId || answer.generation !== shown.generation) {
        // Rows of a session or generation the page does not show: the page's
        // state, or this answer, is behind. The state is read; drawing a new
        // one is what asks for its rows. If that read draws nothing new (it
        // failed), nothing is being read any more, and with no rows on screen
        // the panel must say so and not go on saying it is reading.
        const at = epoch;
        void ports.readState().then(() => {
            if (epoch !== at || shown === null) return; // a state was drawn: its rows are being read
            if (session.qsos.length === 0) {
                session.rows = 'failed';
                session.error = STATE_UNREAD;
            }
        });
        return false;
    }
    session.qsos = answer.members.map(toRow);
    session.rows = 'ok';
    session.error = '';
    return true;
}

/**
 * The session's contacts may have changed: a contact was stored, edited or
 * deleted, an email stamped one, this window logged or edited one. The rows
 * are read again. It carries no row and draws nothing by itself.
 */
export function refreshSessionRows(): void {
    void read();
}

/**
 * A contact was stored. When the daemon says it joined a session the page does
 * not show (the contact that opens a session is announced before the session's
 * own change), the state is read: drawing it is what brings the rows.
 */
export function sessionContactStored(sessionId: string | undefined): void {
    if (sessionId === undefined || sessionId === '') return; // it joined no session
    if (shown === null || shown.sessionId !== sessionId) {
        void ports?.readState();
        return;
    }
    void read();
}

/**
 * The event stream has opened or reopened. Changes made while it was down were
 * announced to nobody, so the rows are read again now, and a read sent before
 * is given up: it may be older than those changes.
 */
export function sessionStreamReopened(): void {
    if (shown === null) return;
    readAfresh();
}

/**
 * An email has been sent from the rows on screen. Their "emailed" marks are
 * now older than the send: the Session is pending until a read taken after
 * this moment has been drawn, and that read is asked for now.
 */
export function sessionEmailSent(): void {
    if (shown === null) return;
    emailSends++;
    session.emailPending = true;
    void read();
}

/** Test seam — draw rows as if a read of the session shown had answered them. */
export function _seedSessionForTests(rows: Omit<SessionQso, 'id'>[]): void {
    session.state = 'open';
    session.rows = 'ok';
    session.qsos = rows.map((r, i) => ({
        ...r,
        id: r.uuid !== undefined && r.uuid !== '' ? idFor(r.uuid) : -(i + 1),
    }));
}

/** Test seam — restore the singleton between cases. */
export function _resetSessionForTests(): void {
    forget();
    out = false;
    emailSends = 0;
    emailSendsRead = 0;
    ports = null;
    ids = {};
    nextId = 1;
}
