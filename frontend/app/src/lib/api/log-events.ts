// EventSource transport for GET /v1/events — the daemon's general event
// firehose (qso.stored / qso.updated / qso.deleted + forward.*). Transport
// only: parses each event's JSON and hands the payload to injected handlers
// (ADR 0045 — consumers own the transitions and never import this layer).
// No server-side filtering (personal-operator scale); consumers select by
// event name and logbook_id. The reconnect contract is the documented one:
// the stream keeps no backlog, so consumers open the stream FIRST, then
// fetch baseline state — events for already-fetched rows are idempotent.
// The browser owns reconnect on transient drops; openReviving adds the two
// cases it does not — a stream that died while the tab was hidden is recreated
// on return, and one killed by a network bounce with the tab visible is
// recreated on the window 'online' event. Both ONLY when dead (a healthy one
// is never torn down).

/** Mirrors internal/events.QsoStoredPayload (and updated/deleted — same
 *  minimal shape by design: clients re-query for details). */
import { openReviving } from './sse-reviving';

export interface QsoEventPayload {
    /** Canonical QSO identifier (AW-1). Present from v2.0.0-alpha.2. */
    qso_uuid?: string;
    /** DEPRECATED daemon-local numeric id — removed in v2.0.0-alpha.3. Prefer qso_uuid. */
    qso_id?: number;
    logbook_id: number;
}

export interface LogEventHandlers {
    onOpen: () => void;
    /** Transport-level failure (stream down / browser reconnecting). */
    onTransportError: () => void;
    /** Any of qso.stored / qso.updated / qso.deleted — consumers that only
     *  re-query don't care which mutation it was. */
    onQsoChanged: (event: string, payload: QsoEventPayload) => void;
}

/**
 * Decode a qso.* event's JSON payload, or null when it is unusable.
 *
 * AW-1 alpha.2 (additive): a payload is accepted when it carries a numeric `logbook_id`
 * AND at least one QSO identifier — `qso_uuid` (canonical) preferred, the deprecated
 * numeric `qso_id` still tolerated so a legacy alpha.1 event is not dropped. A payload with
 * neither identifier, or a non-numeric `logbook_id` (the map keys on it), is rejected. In
 * alpha.3 the `qso_id`-only fallback is removed and `qso_uuid` becomes required.
 */
export function decodeQsoEvent(data: string): QsoEventPayload | null {
    let decoded: unknown;
    try {
        decoded = JSON.parse(data) as unknown;
    } catch {
        return null;
    }
    if (typeof decoded !== 'object' || decoded === null || Array.isArray(decoded)) return null;
    const p = decoded as QsoEventPayload;
    if (typeof p.logbook_id !== 'number') return null;
    const hasUuid = typeof p.qso_uuid === 'string' && p.qso_uuid !== '';
    const hasId = typeof p.qso_id === 'number';
    return hasUuid || hasId ? p : null;
}

function parse(ev: MessageEvent<string>, label: string): QsoEventPayload | null {
    const p = decodeQsoEvent(ev.data);
    if (p === null) console.warn(`[log-events] ${label} payload rejected`, ev.data);
    return p;
}

const QSO_EVENTS = ['qso.stored', 'qso.updated', 'qso.deleted'] as const;

const SSE_URL = '/v1/events';

/**
 * Open the stream and wire the handlers. Returns a close function; calling
 * it tears the EventSource down (the handlers see no further events).
 */
export function openLogEvents(handlers: LogEventHandlers): () => void {
    return openReviving(SSE_URL, (src) => {
        src.addEventListener('open', () => handlers.onOpen());
        src.addEventListener('error', () => handlers.onTransportError());

        for (const name of QSO_EVENTS) {
            src.addEventListener(name, (ev: MessageEvent<string>) => {
                const p = parse(ev, name);
                if (p !== null) handlers.onQsoChanged(name, p);
            });
        }
    });
}
