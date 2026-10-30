/*
    Set as default (ADR 0097, W-0024 slice 5): the change of the archive's
    default logbook, applied at an attended restart.

      - GET  /v1/default-logbook  — the effective default and any change in play
      - POST /v1/default-logbook  {logbook_uuid, believed_default_uuid}

    The routes are served only by a daemon that released them: a 404 means
    unsupported (the feature is hidden), never idle. Any other failure to read
    the status is unreadable — never idle, never unsupported. A request whose
    response never came is an unknown outcome, reconciled by the status read and
    never retried.
*/
import {
    daemonErrorMessage,
    isPlainObject,
    isUuidV7,
    readJsonBody,
    safeFetch,
    WRITE_TIMEOUT_MS,
} from './_helpers';
import { guardedFetch } from './archive-guard';

export interface DefaultLogbook {
    id: number;
    uuid: string;
    name: string;
    callsign: string;
}

/** A change in play, as the daemon reports it. */
export interface DefaultChangeView {
    requestId: string;
    logbookUuid: string;
    state: string;
    removed: string;
    failureCode: string;
    /** config.json still holds the record. */
    unconfirmed: boolean;
}

export interface DefaultChangeStatus {
    effective: DefaultLogbook;
    change: DefaultChangeView | null;
}

export type DefaultChangeStatusOutcome =
    | { kind: 'ok'; status: DefaultChangeStatus }
    /** The daemon does not serve the routes (404): hide the feature. */
    | { kind: 'unsupported' }
    /** It answered, but not with a readable status (5xx, malformed). */
    | { kind: 'unreadable'; message: string }
    | { kind: 'network'; message: string; timedOut?: boolean };

export type DefaultChangeRequestOutcome =
    | { kind: 'accepted'; requestId: string; durability: 'durable' | 'uncertain' }
    | { kind: 'refused'; code: string; message: string }
    /** No answer, or an answer that cannot prove what happened: reconcile. */
    | { kind: 'unknown'; message: string; timedOut?: boolean };

// The states the daemon reports (internal/archive/default_change.go). Anything
// else is a status this client cannot interpret: unreadable, never idle.
const STATES = new Set([
    'pending_restart',
    'pending_unclear',
    'applied_cleanup_pending',
    'withdrawal_unconfirmed',
    'restart_failed',
    'cancelled',
    'withdrawn',
    'not_applied_abandoned',
    'effective',
]);

const str = (v: unknown): string => (typeof v === 'string' ? v : '');

function toEffective(raw: unknown): DefaultLogbook | null {
    if (!isPlainObject(raw) || typeof raw.id !== 'number' || !isUuidV7(raw.uuid)) return null;
    return { id: raw.id, uuid: raw.uuid, name: str(raw.name), callsign: str(raw.callsign) };
}

/** undefined: malformed; null: no change in play. */
function toChange(raw: unknown): DefaultChangeView | null | undefined {
    if (raw === undefined || raw === null) return null;
    if (!isPlainObject(raw)) return undefined;
    const { request_id: requestId, state, unconfirmed } = raw;
    if (typeof requestId !== 'string' || requestId === '') return undefined;
    if (typeof state !== 'string' || !STATES.has(state)) return undefined;
    if (typeof unconfirmed !== 'boolean') return undefined;
    return {
        requestId,
        logbookUuid: str(raw.logbook_uuid),
        state,
        removed: str(raw.removed),
        failureCode: str(raw.failure_code),
        unconfirmed,
    };
}

export async function fetchDefaultChangeStatus(): Promise<DefaultChangeStatusOutcome> {
    const fetched = await safeFetch('/v1/default-logbook', { method: 'GET' });
    if (!fetched.ok) {
        const timedOut = fetched.kind === 'network' ? fetched.timedOut : undefined;
        return { kind: 'network', message: fetched.message, timedOut };
    }
    const res = fetched.response;
    if (res.status === 404) return { kind: 'unsupported' };
    const body = await readJsonBody(res);
    if (!res.ok) return { kind: 'unreadable', message: daemonErrorMessage(res.status, body) };
    const effective = isPlainObject(body) ? toEffective(body.effective) : null;
    const change = isPlainObject(body) ? toChange(body.change) : undefined;
    if (effective === null || change === undefined) {
        return { kind: 'unreadable', message: 'malformed /v1/default-logbook response' };
    }
    return { kind: 'ok', status: { effective, change } };
}

export async function requestDefaultChange(
    logbookUuid: string,
    believedDefaultUuid: string
): Promise<DefaultChangeRequestOutcome> {
    const fetched = await guardedFetch(
        '/v1/default-logbook',
        {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                logbook_uuid: logbookUuid,
                believed_default_uuid: believedDefaultUuid,
            }),
        },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    // The restart drops the connection: any transport failure may follow an
    // accepted request.
    if (!fetched.ok) {
        const timedOut = fetched.kind === 'network' ? fetched.timedOut : undefined;
        return { kind: 'unknown', message: fetched.message, timedOut };
    }
    const res = fetched.response;
    const body = await readJsonBody(res);
    if (res.status === 202) {
        if (!isPlainObject(body) || typeof body.request_id !== 'string' || body.request_id === '') {
            return { kind: 'unknown', message: 'The daemon accepted a request it did not name.' };
        }
        const durability = body.durability === 'uncertain' ? 'uncertain' : 'durable';
        return { kind: 'accepted', requestId: body.request_id, durability };
    }
    // Only the daemon's coded envelope says what happened; anything else (a
    // proxy's HTML page) cannot prove the request was not accepted.
    if (isPlainObject(body) && typeof body.code === 'string' && body.code !== '') {
        const message =
            typeof body.message === 'string' ? body.message : daemonErrorMessage(res.status, body);
        return { kind: 'refused', code: body.code, message };
    }
    return { kind: 'unknown', message: daemonErrorMessage(res.status, body) };
}
