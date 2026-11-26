/*
    The operating session as the page reads and answers it (ADR 0100 §3, §5, §6;
    W-0029 part 4).

    GET /v1/operating-session is the state. POST /v1/operating-session/new and
    /continue are the two answers to the recovery question: each carries the
    generation of the state the page showed, and the archive the page is bound
    to.

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
