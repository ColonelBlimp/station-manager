// EventSource transport for GET /v1/rig/events — the bridge's SSE stream
// (ADR 0010 wire shape). Transport only: parses each event's JSON and hands
// the payload to injected handlers (ADR 0045 — the rig state module owns the
// transitions and never imports this layer). The path is relative because the
// SPA is served from the daemon's origin; cross-origin EventSource is not a
// supported topology. The browser owns reconnect on transient drops — no
// retry loop here.
//
// rig-clients is a daemon event this SPA doesn't consume yet (no multi-tab
// banner in frontend/app) — it arrives on the same stream and is not listened
// for. tune-state IS consumed (ADR 0027 tune carrier — the Tune button
// reflects it, confirm-by-push).

/** Mirrors internal/bridge.RigStatePayload — all fields optional (partial merge). */
export interface RigStatePayload {
    rigIdentity?: string;
    vfoA?: number;
    vfoB?: number;
    mode?: string;
    subMode?: string;
    selectedVfo?: string;
    splitOverride?: boolean;
    power?: number;
}

/** Mirrors the bridge's rig-disconnected / bridge-error payloads (ADR 0010 rev 6). */
export interface BridgeCodePayload {
    code: string;
    details?: Record<string, string>;
}

/** Mirrors internal/bridge.TuneStatePayload (ADR 0027) — the daemon-owned
 *  tune-carrier state. The Tune button reflects this, never an optimistic flip. */
export interface TuneStatePayload {
    active: boolean;
}

export interface RigEventHandlers {
    onOpen: () => void;
    /** Transport-level failure (stream down / reconnecting). */
    onTransportError: () => void;
    onRigState: (payload: RigStatePayload) => void;
    onRigDisconnected: (payload: BridgeCodePayload) => void;
    onBridgeError: (payload: BridgeCodePayload) => void;
    onTuneState: (payload: TuneStatePayload) => void;
}

function parse<T>(ev: MessageEvent<string>, label: string): T | null {
    try {
        return JSON.parse(ev.data) as T;
    } catch (e) {
        console.warn(`[rig-sse] ${label} JSON parse failed`, e);
        return null;
    }
}

/**
 * Open the stream and wire the handlers. Returns a close function; calling
 * it tears the EventSource down (the handlers see no further events).
 */
export function openRigEvents(handlers: RigEventHandlers): () => void {
    const src = new EventSource('/v1/rig/events');

    src.addEventListener('open', () => handlers.onOpen());
    src.addEventListener('error', () => handlers.onTransportError());

    src.addEventListener('rig-state', (ev: MessageEvent<string>) => {
        const p = parse<RigStatePayload>(ev, 'rig-state');
        if (p !== null) handlers.onRigState(p);
    });

    src.addEventListener('rig-disconnected', (ev: MessageEvent<string>) => {
        const p = parse<BridgeCodePayload>(ev, 'rig-disconnected');
        if (p !== null) handlers.onRigDisconnected(p);
    });

    src.addEventListener('bridge-error', (ev: MessageEvent<string>) => {
        const p = parse<BridgeCodePayload>(ev, 'bridge-error');
        if (p !== null) handlers.onBridgeError(p);
    });

    src.addEventListener('tune-state', (ev: MessageEvent<string>) => {
        const p = parse<TuneStatePayload>(ev, 'tune-state');
        if (p !== null) handlers.onTuneState(p);
    });

    return () => src.close();
}
