// EventSource transport for GET /v1/rig/events — the bridge's SSE stream
// (ADR 0010 wire shape). Transport only: parses each event's JSON and hands
// the payload to injected handlers (ADR 0045 — the rig state module owns the
// transitions and never imports this layer). The path is relative because the
// SPA is served from the daemon's origin; cross-origin EventSource is not a
// supported topology. The browser owns reconnect on transient drops;
// openReviving adds the two cases it does not — a stream that died while the
// tab was hidden is recreated on return, and one killed by a network bounce
// with the tab visible (the 2026-08-06 router swap, which parked the CAT
// banner on 'lost') is recreated on the window 'online' event. Both ONLY
// when dead.
//
// rig-clients is a daemon event this SPA doesn't consume yet (no multi-tab
// banner in frontend/app) — it arrives on the same stream and is not listened
// for. tune-state IS consumed (ADR 0027 tune carrier — the Tune button
// reflects it, confirm-by-push).

/** Mirrors internal/bridge.RigStatePayload — all fields optional (partial merge). */
import { openReviving } from './sse-reviving';
import { decodeFrame, makeSseWarn, isPlainObject, optNum, optStr, optBool } from './sse-decode';

export interface RigStatePayload {
    rigIdentity?: string;
    vfoA?: number;
    vfoB?: number;
    mode?: string;
    subMode?: string;
    selectedVfo?: string;
    splitOverride?: boolean;
    power?: number;
    /** Drive-monitor state code, or absent when this frame carried no meter
     *  selection. Mirrors the daemon's RigStatePayload.DriveMonitor. */
    driveMonitor?: string;
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

/** Mirrors internal/bridge.TxAlarmPayload (ADR 0051) — the stuck-TX safety
 *  alarm: active=true means the daemon cannot confirm the transmitter is
 *  unkeyed (the rig MAY be transmitting). Hub-replayed, so a late tab still
 *  learns of a standing alarm. */
/** rig-meters — internal/bridge.RigMetersPayload (ADR 0064): one decoded
 *  RM4/RM5 poll answer, raw rig 0-255 scale. Not replay-cached; flows only
 *  while an FT8 capture session is live on a METERPOLL-capable rigdef. */
export interface RigMetersPayload {
    meter?: string;
    value?: number;
}

export interface TxAlarmPayload {
    active: boolean;
    code?: string;
}

/** Mirrors internal/bridge.DriveAlarmPayload — the rig is keyed but its own
 *  meter reports no output. Deliberately NOT the tx-alarm: that one means the
 *  transmitter may be stuck and demands a safety re-check, this one means the
 *  audio path feeding a correctly-behaving transmitter has died. A one-shot per
 *  transmission — the daemon publishes no clear, because nothing it can observe
 *  proves a drive fault is over. */
export interface DriveAlarmPayload {
    active: boolean;
    code?: string;
}

export interface RigEventHandlers {
    onOpen: () => void;
    /** Transport-level failure (stream down / reconnecting). */
    onTransportError: () => void;
    onRigState: (payload: RigStatePayload) => void;
    onRigDisconnected: (payload: BridgeCodePayload) => void;
    onBridgeError: (payload: BridgeCodePayload) => void;
    onTuneState: (payload: TuneStatePayload) => void;
    onTxAlarm: (payload: TxAlarmPayload) => void;
    onDriveAlarm: (payload: DriveAlarmPayload) => void;
    onRigMeters: (payload: RigMetersPayload) => void;
}

// Per-event validators (F-03, ADR 0077). Every present load-bearing field must be its
// expected type; a wrong-typed field fails the whole frame, which is then dropped so the
// last known good state (freq/mode, tune/TX/drive alarms) stands. All rig payloads are
// partial merges, so absence is valid but a present wrong type is not.
// Every field the bridge's RigStatePayload can push. The embedded SPA ships with its daemon,
// so a legitimate frame always carries at least one of these; a frame with none (empty, or
// only keys this build does not model) is malformed and dropped rather than merged as a no-op.
const RIG_STATE_KEYS = [
    'rigIdentity',
    'vfoA',
    'vfoB',
    'mode',
    'subMode',
    'selectedVfo',
    'splitOverride',
    'power',
    'driveMonitor',
] as const;
function isRigState(v: unknown): v is RigStatePayload {
    if (!isPlainObject(v)) return false;
    if (
        !optStr(v.rigIdentity) ||
        !optNum(v.vfoA) ||
        !optNum(v.vfoB) ||
        !optStr(v.mode) ||
        !optStr(v.subMode) ||
        // selectedVfo is exactly "A" | "B" on the wire and the consumer's field is typed
        // 'A' | 'B'; any other value would corrupt that typed state, so reject it here.
        !(v.selectedVfo === undefined || v.selectedVfo === 'A' || v.selectedVfo === 'B') ||
        !optBool(v.splitOverride) ||
        !optNum(v.power) ||
        !optStr(v.driveMonitor)
    ) {
        return false;
    }
    return RIG_STATE_KEYS.some((k) => k in v);
}
function isBridgeCode(v: unknown): v is BridgeCodePayload {
    return (
        isPlainObject(v) &&
        typeof v.code === 'string' &&
        (v.details === undefined || isPlainObject(v.details))
    );
}
function isTuneState(v: unknown): v is TuneStatePayload {
    return isPlainObject(v) && typeof v.active === 'boolean';
}
function isTxAlarm(v: unknown): v is TxAlarmPayload {
    return isPlainObject(v) && typeof v.active === 'boolean' && optStr(v.code);
}
function isDriveAlarm(v: unknown): v is DriveAlarmPayload {
    return isPlainObject(v) && typeof v.active === 'boolean' && optStr(v.code);
}
function isRigMeters(v: unknown): v is RigMetersPayload {
    // The daemon always sends both fields (no omitempty): one decoded meter poll is a
    // (meter, value) pair, meaningless with either half missing, so require both.
    return isPlainObject(v) && typeof v.meter === 'string' && typeof v.value === 'number';
}

const SSE_URL = '/v1/rig/events';

/**
 * Open the stream and wire the handlers. Returns a close function; calling
 * it tears the EventSource down (the handlers see no further events).
 */
export function openRigEvents(handlers: RigEventHandlers): () => void {
    // One throttled warn per subscription (survives openReviving's internal revives; a fresh
    // openRigEvents after close resets it — F-03, ADR 0077).
    const warn = makeSseWarn('rig-sse');
    return openReviving(SSE_URL, (src) => {
        src.addEventListener('open', () => handlers.onOpen());
        src.addEventListener('error', () => handlers.onTransportError());

        src.addEventListener('rig-state', (ev: MessageEvent<string>) => {
            const p = decodeFrame(ev.data, 'rig-state', isRigState, warn);
            if (p !== null) handlers.onRigState(p);
        });

        src.addEventListener('rig-disconnected', (ev: MessageEvent<string>) => {
            const p = decodeFrame(ev.data, 'rig-disconnected', isBridgeCode, warn);
            if (p !== null) handlers.onRigDisconnected(p);
        });

        src.addEventListener('bridge-error', (ev: MessageEvent<string>) => {
            const p = decodeFrame(ev.data, 'bridge-error', isBridgeCode, warn);
            if (p !== null) handlers.onBridgeError(p);
        });

        src.addEventListener('tune-state', (ev: MessageEvent<string>) => {
            const p = decodeFrame(ev.data, 'tune-state', isTuneState, warn);
            if (p !== null) handlers.onTuneState(p);
        });

        src.addEventListener('tx-alarm', (ev: MessageEvent<string>) => {
            const p = decodeFrame(ev.data, 'tx-alarm', isTxAlarm, warn);
            if (p !== null) handlers.onTxAlarm(p);
        });

        // A separate listener because it is a separate event: EventSource delivers
        // only the named types registered here, so an unregistered event vanishes in
        // the browser with nothing to show it arrived.
        src.addEventListener('drive-alarm', (ev: MessageEvent<string>) => {
            const p = decodeFrame(ev.data, 'drive-alarm', isDriveAlarm, warn);
            if (p !== null) handlers.onDriveAlarm(p);
        });

        src.addEventListener('rig-meters', (ev: MessageEvent<string>) => {
            const p = decodeFrame(ev.data, 'rig-meters', isRigMeters, warn);
            if (p !== null) handlers.onRigMeters(p);
        });
    });
}
