/*
    The operating session as the page reads and answers it (ADR 0100 §3, §5, §6;
    W-0029 part 4).

    GET /v1/operating-session is the state. POST /v1/operating-session/new and
    /continue are the two controls, sent as the answer to the recovery question
    and from the Session panel: each carries a generation (of the state the
    page showed, or of the list a session was chosen from), and the archive the
    page is bound to. GET /v1/operating-session/closed is Continue's list, a
    page at a time (part 6).

    Two rules shape the outcomes here.
      - A read that fails, or that the page cannot understand, is an ERROR. It
        is never an empty session: the caller withholds the workspace on it.
      - An answer never reports "applied" unless the daemon returned the state
        it left. A stale refusal carries the current state, and the page
        redraws to that; it does not send the answer again by itself.
*/

import { guardedFetch } from './archive-guard';
import {
    daemonErrorMessage,
    isPlainObject,
    isUuidV7,
    readJsonBody,
    safeFetch,
    WRITE_TIMEOUT_MS,
} from './_helpers';

/** The states the daemon names (internal/api/operating_session.go). */
export type SessionStateName =
    'open' | 'none' | 'setup_pending' | 'recovery_pending' | 'archive_switching';

const STATES: ReadonlySet<string> = new Set<SessionStateName>([
    'open',
    'none',
    'setup_pending',
    'recovery_pending',
    'archive_switching',
]);

export interface SessionInfo {
    id: string;
    /** When the session started (RFC 3339, UTC). */
    openedAt: string;
    /** When the sitting now open started (RFC 3339, UTC): what the header's
     *  clock counts from. Null when the daemon names none the page can read. */
    sittingStartedAt: string | null;
    /** Contacts not deleted since. It can be 0 for a session that is asked about. */
    contactCount: number;
    heldByYou: boolean;
}

export interface SessionState {
    archiveId: string;
    generation: number;
    state: SessionStateName;
    session: SessionInfo | null;
}

export type SessionRead = { kind: 'ok'; state: SessionState } | { kind: 'error'; message: string };

export type SessionAnswer =
    | { kind: 'applied'; state: SessionState }
    | { kind: 'stale'; state: SessionState }
    | { kind: 'failed'; message: string };

function parseSession(v: unknown): SessionInfo | null {
    if (!isPlainObject(v)) return null;
    if (!isUuidV7(v.id)) return null;
    if (typeof v.opened_at !== 'string' || Number.isNaN(Date.parse(v.opened_at))) return null;
    if (typeof v.contact_count !== 'number' || !Number.isInteger(v.contact_count)) return null;
    if (v.contact_count < 0 || typeof v.held_by_you !== 'boolean') return null;
    const sitting =
        typeof v.sitting_started_at === 'string' && !Number.isNaN(Date.parse(v.sitting_started_at))
            ? v.sitting_started_at
            : null;
    return {
        id: v.id,
        openedAt: v.opened_at,
        sittingStartedAt: sitting,
        contactCount: v.contact_count,
        heldByYou: v.held_by_you,
    };
}

/** The daemon's state body, or null when it is not one the page can draw. */
function parseState(body: unknown): SessionState | null {
    if (!isPlainObject(body)) return null;
    if (!isUuidV7(body.archive_id)) return null;
    if (typeof body.generation !== 'number' || !Number.isInteger(body.generation)) return null;
    if (typeof body.state !== 'string' || !STATES.has(body.state)) return null;
    let session: SessionInfo | null = null;
    if (body.session !== null && body.session !== undefined) {
        session = parseSession(body.session);
        if (session === null) return null;
    }
    // The question cannot be asked about nothing.
    if (body.state === 'recovery_pending' && session === null) return null;
    return {
        archiveId: body.archive_id,
        generation: body.generation,
        state: body.state as SessionStateName,
        session,
    };
}

/** Read the served archive's session state. It changes nothing. */
export async function fetchOperatingSession(): Promise<SessionRead> {
    const fetched = await safeFetch('/v1/operating-session');
    if (!fetched.ok) {
        return { kind: 'error', message: 'Cannot reach the daemon.' };
    }
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    }
    const state = parseState(body);
    if (state === null) {
        return {
            kind: 'error',
            message: 'The daemon answered with a session state this page cannot read.',
        };
    }
    return { kind: 'ok', state };
}

/**
 * Send one answer: Continue the named session, or start a New one. `generation`
 * is the generation of the state the page showed when the operator pressed.
 */
export async function answerOperatingSession(
    answer: 'continue' | 'new',
    generation: number,
    sessionId: string
): Promise<SessionAnswer> {
    const body = answer === 'continue' ? { generation, session_id: sessionId } : { generation };
    const fetched = await guardedFetch(
        `/v1/operating-session/${answer}`,
        {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        return { kind: 'failed', message: 'Cannot reach the daemon.' };
    }
    const parsed = await readJsonBody(fetched.response);
    if (fetched.response.ok) {
        const state = parseState(parsed);
        if (state !== null) return { kind: 'applied', state };
        return {
            kind: 'failed',
            message: 'The daemon answered with a session state this page cannot read.',
        };
    }
    if (isPlainObject(parsed) && parsed.code === 'operating_session_stale') {
        const state = parseState(parsed.operating_session);
        if (state !== null) return { kind: 'stale', state };
    }
    return { kind: 'failed', message: daemonErrorMessage(fetched.response.status, parsed) };
}

/** One contact of the open session, in the panel's columns
 *  (GET /v1/operating-session/members). */
export interface SessionMember {
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
    /** The contact's own record says it was forwarded by email. */
    emailed: boolean;
}

export type SessionMembersRead =
    | {
          kind: 'ok';
          archiveId: string;
          generation: number;
          /** The open session the rows are of; null when none is open. */
          sessionId: string | null;
          members: SessionMember[];
      }
    | { kind: 'error'; message: string };

const MEMBER_TEXT = [
    'call',
    'time_on',
    'band',
    'mode',
    'rst_sent',
    'rst_rcvd',
    'name',
    'country',
    'comment',
] as const;

function parseMember(v: unknown): SessionMember | null {
    if (!isPlainObject(v)) return null;
    if (!isUuidV7(v.uuid) || typeof v.emailed !== 'boolean') return null;
    for (const k of MEMBER_TEXT) if (typeof v[k] !== 'string') return null;
    if (v.submode !== undefined && typeof v.submode !== 'string') return null;
    const text = v as Record<(typeof MEMBER_TEXT)[number], string>;
    return {
        uuid: v.uuid,
        call: text.call,
        timeOn: text.time_on,
        band: text.band,
        mode: text.mode,
        submode: v.submode,
        rstSent: text.rst_sent,
        rstRcvd: text.rst_rcvd,
        name: text.name,
        country: text.country,
        comment: text.comment,
        emailed: v.emailed,
    };
}

const UNREADABLE_ROWS = 'The daemon answered with session contacts this page cannot read.';

/**
 * Read the open session's contacts, newest stored first. An answer the page
 * cannot understand in full is an ERROR, never a shorter list: a row left out
 * would be drawn as the whole session.
 */
export async function fetchSessionMembers(): Promise<SessionMembersRead> {
    const fetched = await safeFetch('/v1/operating-session/members');
    if (!fetched.ok) {
        return { kind: 'error', message: 'Cannot reach the daemon.' };
    }
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    }
    if (
        !isPlainObject(body) ||
        !isUuidV7(body.archive_id) ||
        typeof body.generation !== 'number' ||
        !Number.isInteger(body.generation) ||
        !(body.session_id === null || isUuidV7(body.session_id)) ||
        !Array.isArray(body.members)
    ) {
        return { kind: 'error', message: UNREADABLE_ROWS };
    }
    const members: SessionMember[] = [];
    for (const raw of body.members as unknown[]) {
        const m = parseMember(raw);
        if (m === null) return { kind: 'error', message: UNREADABLE_ROWS };
        members.push(m);
    }
    return {
        kind: 'ok',
        archiveId: body.archive_id,
        generation: body.generation,
        sessionId: body.session_id,
        members,
    };
}

/** One closed session, as Continue's list gives it
 *  (GET /v1/operating-session/closed). */
export interface ClosedSession {
    id: string;
    /** The session's first start (RFC 3339, UTC). */
    openedAt: string;
    /** The start of its latest sitting: what the list is ordered by. */
    latestSittingStartedAt: string;
    /** The end of its latest sitting; null when that sitting has no end. */
    latestSittingEndedAt: string | null;
    /** A sitting of it, the last or an earlier one, ended without a confirmed exit. */
    anEndUnconfirmed: boolean;
    /** Its contacts that are not deleted. It can be 0. */
    contactCount: number;
}

export type ClosedSessionsRead =
    | {
          kind: 'ok';
          archiveId: string;
          generation: number;
          /** The default logbook the list is of; '' when the file has none. */
          logbookUuid: string;
          sessions: ClosedSession[];
          /** There are sessions after the last of these. */
          more: boolean;
      }
    /** A further page of a list that has changed since the page it follows:
     *  the list is read again from its start. */
    | { kind: 'stale' }
    | { kind: 'error'; message: string };

const UNREADABLE_LIST = 'The daemon answered with a list of sessions this page cannot read.';

const isTime = (v: unknown): v is string => typeof v === 'string' && !Number.isNaN(Date.parse(v));

function parseClosedSession(v: unknown): ClosedSession | null {
    if (!isPlainObject(v)) return null;
    if (!isUuidV7(v.id) || !isTime(v.opened_at) || !isTime(v.latest_sitting_started_at)) {
        return null;
    }
    // The end is always stated: a time, or an explicit null for a sitting
    // that has none. A line that leaves it out is not one the page can read.
    const ended = v.latest_sitting_ended_at;
    if (ended !== null && !isTime(ended)) return null;
    if (typeof v.an_end_unconfirmed !== 'boolean') return null;
    if (typeof v.contact_count !== 'number' || !Number.isInteger(v.contact_count)) return null;
    if (v.contact_count < 0) return null;
    return {
        id: v.id,
        openedAt: v.opened_at,
        latestSittingStartedAt: v.latest_sitting_started_at,
        latestSittingEndedAt: ended,
        anEndUnconfirmed: v.an_end_unconfirmed,
        contactCount: v.contact_count,
    };
}

/**
 * Read a page of Continue's list: at most `limit` closed sessions, the most
 * recent first. With `after`, the page that follows that session in the list of
 * that generation. An answer the page cannot understand in full is an ERROR,
 * never a shorter list: a session left out would not be offered.
 */
export async function fetchClosedSessions(
    limit: number,
    after?: { sessionId: string; generation: number }
): Promise<ClosedSessionsRead> {
    const query = new URLSearchParams({ limit: String(limit) });
    if (after !== undefined) {
        query.set('after', after.sessionId);
        query.set('generation', String(after.generation));
    }
    const fetched = await safeFetch(`/v1/operating-session/closed?${query.toString()}`);
    if (!fetched.ok) {
        return { kind: 'error', message: 'Cannot reach the daemon.' };
    }
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        if (isPlainObject(body) && body.code === 'operating_session_stale') {
            return { kind: 'stale' };
        }
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    }
    if (
        !isPlainObject(body) ||
        !isUuidV7(body.archive_id) ||
        typeof body.generation !== 'number' ||
        !Number.isInteger(body.generation) ||
        typeof body.logbook_uuid !== 'string' ||
        typeof body.more !== 'boolean' ||
        !Array.isArray(body.sessions)
    ) {
        return { kind: 'error', message: UNREADABLE_LIST };
    }
    const sessions: ClosedSession[] = [];
    for (const raw of body.sessions as unknown[]) {
        const c = parseClosedSession(raw);
        if (c === null) return { kind: 'error', message: UNREADABLE_LIST };
        sessions.push(c);
    }
    // A page with no session that says there are more names nothing to ask
    // for next, and would be drawn as "nothing to continue".
    if (sessions.length === 0 && body.more) return { kind: 'error', message: UNREADABLE_LIST };
    return {
        kind: 'ok',
        archiveId: body.archive_id,
        generation: body.generation,
        logbookUuid: body.logbook_uuid,
        sessions,
        more: body.more,
    };
}
