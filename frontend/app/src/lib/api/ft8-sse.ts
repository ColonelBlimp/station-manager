// EventSource transport for GET /v1/ft8/events — the FT8 subsystem's SSE stream.
// Transport only: parses each named event's JSON and hands the wire payload to
// injected handlers (ADR 0045 — the ft8 state module owns the transitions and
// never imports this layer). Mirrors lib/api/rig-sse.ts.
//
// Lifecycle is VIEW-SCOPED, unlike the rig stream (which opens once at boot):
// the daemon acquires the audio-capture device on the FIRST /v1/ft8/events
// subscriber and releases it after the LAST leaves, so the FT8 view opens this
// on mount and calls the returned close fn on destroy — the daemon only holds
// the microphone while the operator is actually looking at FT8. The browser
// owns SSE auto-reconnect on transient drops.

/** One occupied audio-frequency range. Mirrors internal/ft8.Band (snake_case wire). */
export interface Ft8Band {
    low_hz: number;
    high_hz: number;
    source?: string; // "decode" | "energy" | "both"
    level?: number; // 0..1 relative energy
}

/** Mirrors internal/ft8.SlotRef. */
export interface Ft8SlotRef {
    start_utc: string; // RFC3339 UTC
    period: string; // "even" | "odd"
}

/** ft8-occupancy — internal/ft8.OccupancyReport (one per RX slot; decision data,
 *  not a spectrogram; replayed to a late subscriber). */
export interface OccupancyPayload {
    slot: Ft8SlotRef;
    passband: Ft8Band;
    signal_width_hz: number;
    occupied: Ft8Band[] | null;
    suggested: number[] | null;
}

/** One decoded message on the wire. Mirrors internal/ft8.DecodeLine. */
export interface DecodeLine {
    text: string;
    freq_hz: number;
    dt_s: number;
    snr: number;
}

/** ft8-decode — internal/ft8.DecodeReport (fires EVERY slot, incl. our own TX
 *  slots with a null decode list, so it's the slot heartbeat). */
export interface DecodeReport {
    slot: Ft8SlotRef;
    decodes: DecodeLine[] | null;
}

/** ft8-tx — internal/ft8.TxState (arm/transmit + in-flight message; hub-cached
 *  so a reconnect replays current arm state). `error` is an i18n code. */
export interface TxPayload {
    armed?: boolean;
    transmitting?: boolean;
    message?: string;
    offset_hz?: number;
    error?: string;
}

/** ft8-qso — internal/ft8.QsoStatus (the active manual-sequencer contact;
 *  hub-cached so a reconnect replays it). */
export interface QsoPayload {
    active?: boolean;
    role?: string;
    their_call?: string;
    their_grid?: string;
    state?: string;
    next_message?: string;
    repeats?: number;
    max_repeats?: number;
    skip_armed?: boolean;
    our_report?: string;
    their_report?: string;
    their_period?: string;
    fd?: boolean;
    our_class?: string;
    our_section?: string;
    their_class?: string;
    their_section?: string;
}

/** ft8-logged — internal/ft8.LoggedQso (a completed exchange the daemon stored).
 *  NOT replay-cached (re-delivery would dup a session row); dedup on uuid. */
export interface LoggedPayload {
    uuid?: string;
    callsign?: string;
    freq_hz?: number;
    band?: string;
    rst_sent?: string;
    rst_rcvd?: string;
    mode?: string;
    time_on?: string;
    qso_date?: string;
    gridsquare?: string;
    country?: string;
    name?: string;
}

export interface Ft8EventHandlers {
    onOpen: () => void;
    /** Transient drop (browser auto-retries) or terminal — either way, not carrying frames. */
    onError: () => void;
    onOccupancy: (p: OccupancyPayload) => void;
    onDecode: (p: DecodeReport) => void;
    onTx: (p: TxPayload) => void;
    onQso: (p: QsoPayload) => void;
    onLogged: (p: LoggedPayload) => void;
}

function parse<T>(ev: MessageEvent<string>, label: string): T | null {
    try {
        return JSON.parse(ev.data) as T;
    } catch (e) {
        console.warn(`[ft8-sse] ${label} JSON parse failed`, e);
        return null;
    }
}

/**
 * Open the FT8 event stream and wire the handlers. Returns a close function;
 * calling it tears the EventSource down (the daemon then releases the capture
 * device once the last subscriber is gone). Idempotent from the caller's side —
 * each call opens one source and hands back its own closer.
 */
export function openFt8Events(handlers: Ft8EventHandlers): () => void {
    const src = new EventSource('/v1/ft8/events');

    src.addEventListener('open', () => handlers.onOpen());
    src.addEventListener('error', () => handlers.onError());

    src.addEventListener('ft8-occupancy', (ev: MessageEvent<string>) => {
        const p = parse<OccupancyPayload>(ev, 'ft8-occupancy');
        if (p !== null) handlers.onOccupancy(p);
    });
    src.addEventListener('ft8-decode', (ev: MessageEvent<string>) => {
        const p = parse<DecodeReport>(ev, 'ft8-decode');
        if (p !== null) handlers.onDecode(p);
    });
    src.addEventListener('ft8-tx', (ev: MessageEvent<string>) => {
        const p = parse<TxPayload>(ev, 'ft8-tx');
        if (p !== null) handlers.onTx(p);
    });
    src.addEventListener('ft8-qso', (ev: MessageEvent<string>) => {
        const p = parse<QsoPayload>(ev, 'ft8-qso');
        if (p !== null) handlers.onQso(p);
    });
    src.addEventListener('ft8-logged', (ev: MessageEvent<string>) => {
        const p = parse<LoggedPayload>(ev, 'ft8-logged');
        if (p !== null) handlers.onLogged(p);
    });

    return () => src.close();
}
