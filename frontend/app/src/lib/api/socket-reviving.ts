/*
    A reviving WebSocket — the transport for the shell and rig streams since
    ADR 0099 W1 ("WebSocket event delivery", responsibility 9).

    WHY A WEBSOCKET. Every EventSource holds one of the browser's six HTTP/1.1
    connections to the daemon, so four SPA windows starved ordinary requests
    (W-0024 R5). Open WebSockets do not draw from that pool (ADR 0099, WebSocket
    pool probe). FT8 stays on EventSource: its subscription is the FT8 presence
    signal, and its lifetime semantics must not change.

    WHAT THIS OWNS that EventSource used to. A WebSocket never reconnects by
    itself, so this is a small state machine:
      connecting — one attempt in flight;
      open       — handshake complete;
      waiting    — the attempt failed or the socket dropped; ONE retry timer is
                   armed, at a FIXED interval (operator ruling 2026-11-05: 1 s,
                   no backoff — D2b allows 10 s from the new daemon being ready);
      closed     — torn down; nothing acts again.

    THE RULES (socket-reviving.test.ts S1–S7):
      - One attempt and one timer at a time. A failure arms the timer once; an
        'error' followed by 'close' is the same failure (the first detaches the
        socket, so the second is a stale callback).
      - Callbacks from a replaced or torn-down socket are ignored.
      - Becoming visible, or 'online', revives ONLY a socket that is waiting to
        retry, and only while the tab is visible — it brings the retry forward.
        A CONNECTING socket (the initial connect included) is not dead, and an
        OPEN one is healthy: neither is ever replaced. That keeps the SSE
        wrapper's "revive only when dead" (sse-reviving.ts) without its reason
        for existing: there, the browser could give up for good; here, the timer
        never gives up.
      - Automatic retries do not depend on visibility, as the browser's own
        EventSource retries did not.

    FRAMES. The daemon sends one JSON text frame per event, {event, data}. Each
    frame is parsed ONCE here; the caller's value-level validators take `data`
    as it is. 'connected' and 'keepalive' are transport frames, not events.
    A malformed frame is dropped with the caller's throttled warning.
*/

import { isPlainObject } from './_helpers';

export const SOCKET_RETRY_MS = 1000;
let retryMs = SOCKET_RETRY_MS;

/** Test seam: real-timer composition tests (main.*.test.ts) reconnect at once;
 *  null restores the ruled interval. */
export function setSocketRetryMsForTests(ms: number | null): void {
    retryMs = ms ?? SOCKET_RETRY_MS;
}

export interface SocketWire {
    /** The handshake completed (every successful (re)connect). */
    onOpen: () => void;
    /** The attempt failed or the socket dropped (once per failure). */
    onTransportError: () => void;
    /** One event: its name and its already-parsed data. */
    onFrame: (event: string, data: unknown) => void;
}

type WarnFn = (event: string, reason: string, detail?: unknown) => void;
type State = 'connecting' | 'open' | 'waiting' | 'closed';

const TRANSPORT_FRAMES = new Set(['connected', 'keepalive']);

/** Same host and port as the page (the SPA is served by the daemon). */
function socketUrl(path: string): string {
    const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${scheme}//${location.host}${path}`;
}

export function openRevivingSocket(path: string, wire: SocketWire, warn: WarnFn): () => void {
    let state: State = 'connecting';
    let sock: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const dispatch = (raw: unknown): void => {
        if (typeof raw !== 'string') {
            warn('frame', 'not_text');
            return;
        }
        let frame: unknown;
        try {
            frame = JSON.parse(raw);
        } catch {
            warn('frame', 'invalid_json');
            return;
        }
        if (!isPlainObject(frame) || typeof frame.event !== 'string') {
            warn('frame', 'invalid_envelope', frame);
            return;
        }
        if (TRANSPORT_FRAMES.has(frame.event)) return;
        if (!('data' in frame)) {
            warn(frame.event, 'missing_data');
            return;
        }
        wire.onFrame(frame.event, frame.data);
    };

    const fail = (ws: WebSocket): void => {
        if (ws !== sock) return; // stale (replaced or torn down), or the 'close' after an 'error'
        sock = null;
        ws.close(); // harmless on a failed socket; ends a half-open one
        state = 'waiting';
        wire.onTransportError();
        timer = setTimeout(() => {
            timer = null;
            connect();
        }, retryMs);
    };

    const connect = (): void => {
        if (state === 'closed') return;
        state = 'connecting';
        const ws = new WebSocket(socketUrl(path));
        sock = ws;
        ws.onopen = () => {
            if (ws !== sock) return;
            state = 'open';
            wire.onOpen();
        };
        ws.onmessage = (ev: MessageEvent) => {
            if (ws !== sock) return;
            dispatch(ev.data);
        };
        ws.onerror = () => fail(ws);
        ws.onclose = () => fail(ws);
    };

    // One handler for both triggers: bring a waiting retry forward, in a visible
    // tab only. Never touches a connecting or open socket.
    const reviveIfWaiting = (): void => {
        if (state !== 'waiting') return;
        if (document.visibilityState !== 'visible') return;
        if (timer !== null) {
            clearTimeout(timer);
            timer = null;
        }
        connect();
    };

    connect();
    document.addEventListener('visibilitychange', reviveIfWaiting);
    window.addEventListener('online', reviveIfWaiting);

    // Teardown. Two guards stop a late retry, deliberately: clearing the timer,
    // and connect() refusing to run once closed. Either alone keeps S6 green;
    // removing both turns it red (the paired reversion proof).
    return () => {
        state = 'closed';
        document.removeEventListener('visibilitychange', reviveIfWaiting);
        window.removeEventListener('online', reviveIfWaiting);
        if (timer !== null) {
            clearTimeout(timer);
            timer = null;
        }
        const ws = sock;
        sock = null; // its late callbacks are now stale
        ws?.close();
    };
}
