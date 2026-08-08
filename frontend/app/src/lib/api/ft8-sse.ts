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
// owns SSE auto-reconnect on transient drops; openReviving recreates a DEAD
// stream on return-to-visible or on the window 'online' event. Note the
// stakes here: this stream dying starts the daemon's 5 s capture linger and
// then disarms TX, so a network bounce longer than the linger still ends a
// run — the revive brings the SURFACE back, it cannot un-ring that bell.

/** One occupied audio-frequency range. Mirrors internal/ft8.Band (snake_case wire). */
import { openReviving } from './sse-reviving';

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
    /** Rig dial frequency (MHz) the slot was captured on; absent when the daemon
     *  had no CAT to read it from. This is what makes the snapshot attributable to
     *  a band on its own — see ft8.svelte.ts onOccupancy. */
    dial_mhz?: number;
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
    /** The rig dial this slot's audio was CAPTURED on (MHz), absent when
     *  unknown. Attribute these decodes to a band from THIS, never from live
     *  rig state: publication lags capture by the decode, so a QSY in that gap
     *  otherwise files stations heard on band A as band B (review P1,
     *  2026-08-07; same rule as the occupancy report's dial_mhz). */
    dial_mhz?: number;
}

/** ft8-tx — internal/ft8.TxState (arm/transmit + in-flight message; hub-cached
 *  so a reconnect replays current arm state). `error` is an i18n code. */
export interface TxPayload {
    armed?: boolean;
    transmitting?: boolean;
    message?: string;
    offset_hz?: number;
    error?: string;
    /** Stable code for the disarm this frame reports (internal/ft8 disarm*
     *  constants: operator | unattended | cat_lost | shutdown | band_change |
     *  dial_moved). "" while armed, and absent from daemons predating it. */
    disarm_cause?: string;
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
    /** Pending Call-CQ Next: park this answerer at the next slot evaluation. */
    next_armed?: boolean;
    /** An auto-work-callers run is live (ADR 0059): the next station to call us is
     *  worked with no operator action. Carried on IDLE frames too — that is the
     *  state the operator cannot otherwise see, since an armed run between contacts
     *  looks exactly like a finished one. */
    auto_work_armed?: boolean;
    our_report?: string;
    their_report?: string;
    their_period?: string;
    /** Rig dial PINNED to the session at start (MHz) — the frequency the contact will
     *  be logged on. Use this, not live rig state, to attribute a contact to a band:
     *  the rig and FT8 status are independent streams. */
    dial_freq_mhz?: number;
    /** Why a session ended, when the operator did not cause it (carried only on the
     *  terminal active:false frame; absent for an abandon or a completed contact).
     *  A stable code — `dial_moved` | `dial_unknown` — rendered by the client. */
    end_reason?: string;
    fd?: boolean;
    /** Reduced type-4 (nonstandard/compound call) session — bare-calls→RR73→73,
     *  no grid/report rungs (ADR 0048). */
    type4?: boolean;
    our_class?: string;
    our_section?: string;
    their_class?: string;
    their_section?: string;
    /** The Call-CQ run's answerer-selection mode (caller frames only) — lets a
     *  client tell an operator_pick run from an auto one before any answerer
     *  arrives. The mode itself is config.json-only (ft8.tx.caller_answer_mode). */
    answer_mode?: string;
    /** operator_pick candidate list (ADR 0065): stations currently answering our
     *  CQ that the run can actually work, oldest first. Pop one via
     *  POST /v1/ft8/cq/pick; grid/offset/dial stay daemon-side. */
    answerers?: { call: string; snr: number }[];
    /** The pick run's BAGGED stations (ADR 0067), in bag order — the operator's
     *  explicit choices, auto-worked by the drain. */
    queue?: { call: string; snr: number }[];
    /** Stop paused the drain (queue kept); Resume continues (ADR 0067). */
    drain_paused?: boolean;
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

/** Mirrors internal/ft8.AudioLevel — one 250 ms RX capture measurement
 *  window (~4 Hz while capture is live; nothing at all without capture). */
export interface AudioLevelPayload {
    peak_dbfs: number;
    rms_dbfs: number;
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
    onAudioLevel: (p: AudioLevelPayload) => void;
}

function parse<T>(ev: MessageEvent<string>, label: string): T | null {
    try {
        return JSON.parse(ev.data) as T;
    } catch (e) {
        console.warn(`[ft8-sse] ${label} JSON parse failed`, e);
        return null;
    }
}

const SSE_URL = '/v1/ft8/events';

/**
 * Open the FT8 event stream and wire the handlers. Returns a close function;
 * calling it tears the EventSource down (the daemon then releases the capture
 * device once the last subscriber is gone). Idempotent from the caller's side —
 * each call opens one source and hands back its own closer.
 */
export function openFt8Events(handlers: Ft8EventHandlers): () => void {
    return openReviving(SSE_URL, (src) => {
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
        src.addEventListener('ft8-audio-level', (ev: MessageEvent<string>) => {
            const p = parse<AudioLevelPayload>(ev, 'ft8-audio-level');
            if (p !== null) handlers.onAudioLevel(p);
        });
    });
}
