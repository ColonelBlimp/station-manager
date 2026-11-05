// Transport for the daemon's general event firehose (qso.stored / qso.updated /
// qso.deleted + forward.*): GET /v1/events/ws, the WebSocket twin of
// /v1/events (ADR 0099 W1 — an open WebSocket does not hold one of the
// browser's six HTTP connections; openRevivingSocket owns reconnect). Transport
// only: parses each event's JSON and hands the payload to injected handlers
// (ADR 0045 — consumers own the transitions and never import this layer).
// No server-side filtering (personal-operator scale); consumers select by
// event name and logbook_id. The reconnect contract is the documented one:
// the stream keeps no backlog, so consumers open the stream FIRST, then
// fetch baseline state — events for already-fetched rows are idempotent.
// openRevivingSocket retries a dropped socket on a fixed interval, and brings a
// waiting retry forward when the tab becomes visible or the window reports
// 'online' — never touching a connecting or healthy socket.
//
// ONE CONNECTION PER TAB (ADR 0079 dated update 2026-09-14). Every subscriber
// in a tab rides the same connection: the shell opens it at boot and never
// closes it, and a view that subscribes later (the contacts map) joins the live
// connection instead of opening a second one. The Map view's own /v1/events
// was the sixth long-lived connection across two tabs — the browser's per-host
// cap — and the map's data request never reached the daemon (dogfood
// 2026-09-11, recurred 2026-09-13). Subscribers are ref-counted so a tab with
// no shell (tests) still opens on the first and closes on the last.

/** Mirrors internal/events.QsoStoredPayload (and updated/deleted — same
 *  minimal shape by design: clients re-query for details). */
import { openRevivingSocket } from './socket-reviving';
import { makeSseWarn } from './sse-decode';

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
    /** The stream reopened after a transport error — once per reconnection
     *  transition, never on the boot open. This is the moment a reconnecting
     *  client must re-fetch its baseline (the stream keeps no backlog), and the
     *  moment a restarted daemon — `smctl import` restarts it — is picked up
     *  (ADR 0079, dogfood Finding #16). Transport-specific on purpose: build
     *  identity keeps its own transition on the rig stream (W-0004 AC3). */
    onReconnect?: () => void;
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
    return decodeQsoEventValue(decoded);
}

/** The value-level entry: validates an already-parsed payload — the `data`
 *  member of a WebSocket envelope (ADR 0099 W1). decodeQsoEvent is JSON.parse
 *  followed by this, so both transports share one validator. */
export function decodeQsoEventValue(decoded: unknown): QsoEventPayload | null {
    if (typeof decoded !== 'object' || decoded === null || Array.isArray(decoded)) return null;
    const p = decoded as QsoEventPayload;
    if (typeof p.logbook_id !== 'number') return null;
    const hasUuid = typeof p.qso_uuid === 'string' && p.qso_uuid !== '';
    const hasId = typeof p.qso_id === 'number';
    return hasUuid || hasId ? p : null;
}

function parse(data: unknown, label: string): QsoEventPayload | null {
    const p = decodeQsoEventValue(data);
    if (p === null) console.warn(`[log-events] ${label} payload rejected`, data);
    return p;
}

const QSO_EVENTS: ReadonlySet<string> = new Set(['qso.stored', 'qso.updated', 'qso.deleted']);

const STREAM_PATH = '/v1/events/ws';

interface Subscription {
    handlers: LogEventHandlers;
    // A transport error seen by THIS subscriber since its last open. Per
    // subscriber, not per stream: a newcomer that joins during an outage saw no
    // drop, so the reopen is its boot open, while the shell that did see it
    // re-fetches its baseline. Lives in the subscription, not the wire closure,
    // so a drop on a socket that a later reconnect replaces still counts as the drop
    // half of the transition when the replacement opens.
    sawError: boolean;
}

interface SharedStream {
    subs: Subscription[];
    /** The connection is open right now — a late subscriber is told at once. */
    isOpen: boolean;
    close: () => void;
}

let shared: SharedStream | null = null;

function connect(): SharedStream {
    const s: SharedStream = { subs: [], isOpen: false, close: () => {} };
    // Fan-out iterates a copy: a handler may unsubscribe mid-delivery.
    const each = (fn: (sub: Subscription) => void): void => {
        for (const sub of [...s.subs]) fn(sub);
    };
    s.close = openRevivingSocket(
        STREAM_PATH,
        {
            onOpen: () => {
                s.isOpen = true;
                each((sub) => {
                    sub.handlers.onOpen();
                    if (sub.sawError) {
                        sub.sawError = false;
                        sub.handlers.onReconnect?.();
                    }
                });
            },
            onTransportError: () => {
                s.isOpen = false;
                each((sub) => {
                    sub.sawError = true;
                    sub.handlers.onTransportError();
                });
            },
            onFrame: (name, data) => {
                if (!QSO_EVENTS.has(name)) return; // forward.* etc.: not consumed here
                const p = parse(data, name);
                if (p !== null) each((sub) => sub.handlers.onQsoChanged(name, p));
            },
        },
        makeSseWarn('log-events')
    );
    return s;
}

/**
 * Subscribe to the tab's shared stream, opening it if this is the first
 * subscriber. Returns a close function; calling it detaches the handlers (they
 * see no further events) and tears the connection down only when no
 * subscriber remains. Joining an already-open stream fires onOpen at once, so
 * a consumer's "open first, then fetch the baseline" contract holds unchanged.
 */
export function openLogEvents(handlers: LogEventHandlers): () => void {
    if (shared === null) shared = connect();
    const s = shared;
    const sub: Subscription = { handlers, sawError: false };
    s.subs.push(sub);
    if (s.isOpen) handlers.onOpen();
    return () => {
        const i = s.subs.indexOf(sub);
        if (i < 0) return; // already closed
        s.subs.splice(i, 1);
        if (s.subs.length === 0 && shared === s) {
            shared = null;
            s.close();
        }
    };
}
