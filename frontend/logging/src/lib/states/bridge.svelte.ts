/**
 * Bridge SSE transport — EventSource consumer for `GET /v1/rig/events`.
 *
 * Per ADR 0010 (`docs/decisions/0010-rig-sse-wire-shape.md`) the daemon's
 * bridge subsystem publishes three SSE event types over a single stream:
 *
 *   - `rig-state` — partial-payload rig snapshot. Field-by-field merge
 *     into `catState`; fields omitted from the payload preserve their
 *     prior values. `splitOverride` is *bool on the wire, so the
 *     `omitted` vs `false` distinction matters and is preserved on the
 *     SPA side (the field-existence check below).
 *   - `rig-disconnected` — bridge concluded the rig went silent
 *     (liveness timeout / serial EIO / terminal close). Toasts the
 *     operator at warn level and flips `bridgeState.rigResponding=false`.
 *   - `bridge-error` — operator-actionable bridge-side error (port
 *     permission denied, unknown driver, INIT failure, identity
 *     mismatch). Toasts at error level. NOT used for transient retries.
 *
 * **bridgeState flags drive displayedState's three-flag rule** (ADR 0009).
 *
 *   - `connected` mirrors `EventSource.readyState === OPEN` — transport
 *     level only. Says nothing about whether the rig is alive at the
 *     other end.
 *   - `rigResponding` is `false` until the first `rig-state` event
 *     arrives, flips back to `false` on `rig-disconnected`, and back
 *     to `true` again on any subsequent `rig-state` event (implicit
 *     reconnect — see ADR 0009).
 *
 * **Construction is conditional on `configState.station.enabled`.** The
 * operator's "CAT enabled" intent is the gate; when false the SPA stays
 * fully manual and never opens the stream. The browser handles SSE
 * auto-reconnect on transient transport drops — no retry loop here.
 *
 * **Lifecycle:** the module exports the singleton `bridgeState` (read by
 * `displayedState`) plus `startBridge()` / `stopBridge()`. `startBridge()`
 * is called once from `app.svelte`'s onMount after config loads. It
 * wires an effect.root that tracks `configState.station.enabled` and
 * opens/closes the EventSource accordingly. Tests call `stopBridge()`
 * between cases to keep the module singleton clean.
 *
 * See ADR 0009 for the four-object decomposition this fits into and
 * ADR 0019 for the bridge subsystem's v1 design.
 */

import { catState } from './cat.svelte';
import { configState } from './config.svelte';
import { t } from '../i18n';
import { toasts } from './toasts.svelte';

class BridgeState {
    connected: boolean = $state(false);
    rigResponding: boolean = $state(false);
}

export const bridgeState = new BridgeState();

/** Payload shape mirrors `internal/bridge.RigStatePayload`. All fields optional. */
interface RigStatePayload {
    rigIdentity?: string;
    vfoA?: number;
    vfoB?: number;
    mode?: string;
    subMode?: string;
    selectedVfo?: string;
    splitOverride?: boolean;
    power?: number;
}

interface RigDisconnectedPayload {
    code: string;
    details?: Record<string, string>;
}

interface BridgeErrorPayload {
    code: string;
    details?: Record<string, string>;
}

let activeSource: EventSource | null = null;
let rootDispose: (() => void) | null = null;

/**
 * Construct the EventSource and wire its listeners. Idempotent: calling
 * with an existing open source is a no-op.
 *
 * The path is relative because the SPA is served from the daemon's
 * origin under the `ServeSPA` deployment; cross-origin EventSource is
 * not a supported topology.
 */
function openSource(): void {
    if (activeSource) return;

    const src = new EventSource('/v1/rig/events');
    activeSource = src;

    src.addEventListener('open', () => {
        bridgeState.connected = true;
    });

    // EventSource fires `error` on transient transport drops (readyState
    // → CONNECTING, browser auto-retries) and on terminal failure
    // (readyState → CLOSED). We can't distinguish reliably across
    // engines/jsdom, so the conservative read is "transport is no
    // longer carrying frames" — flip both connected and rigResponding
    // to false. The browser's auto-reconnect re-fires `open` when the
    // stream re-establishes.
    src.addEventListener('error', () => {
        bridgeState.connected = false;
        bridgeState.rigResponding = false;
    });

    src.addEventListener('rig-state', (ev: MessageEvent<string>) => {
        let payload: RigStatePayload;
        try {
            payload = JSON.parse(ev.data) as RigStatePayload;
        } catch (e) {
            console.warn('[bridge] rig-state JSON parse failed', e);
            return;
        }
        mergeRigState(payload);
        bridgeState.rigResponding = true;
    });

    src.addEventListener('rig-disconnected', (ev: MessageEvent<string>) => {
        let payload: RigDisconnectedPayload;
        try {
            payload = JSON.parse(ev.data) as RigDisconnectedPayload;
        } catch (e) {
            console.warn('[bridge] rig-disconnected JSON parse failed', e);
            return;
        }
        bridgeState.rigResponding = false;
        // Daemon sends a machine-readable code + per-instance details
        // (e.g. {"code":"rig_no_data"} or {"code":"serial_port_error",
        // "details":{"error":"i/o timeout"}}). The i18n catalogue
        // keyed by `bridge.disconnected.<code>` owns the operator-
        // facing wording + future localizations (Tumbuka, Chichewa).
        toasts.warn(t(`bridge.disconnected.${payload.code}`, payload.details));
    });

    src.addEventListener('bridge-error', (ev: MessageEvent<string>) => {
        let payload: BridgeErrorPayload;
        try {
            payload = JSON.parse(ev.data) as BridgeErrorPayload;
        } catch (e) {
            console.warn('[bridge] bridge-error JSON parse failed', e);
            return;
        }
        toasts.error(t(`bridge.error.${payload.code}`, payload.details));
    });
}

function closeSource(): void {
    if (!activeSource) return;
    activeSource.close();
    activeSource = null;
    bridgeState.connected = false;
    bridgeState.rigResponding = false;
}

/**
 * Merge a partial RigStatePayload into catState. Fields absent from the
 * payload preserve their prior catState value (Svelte 5 $state proxy
 * retains values on no-op assignment).
 *
 * `splitOverride` deserves the explicit existence check: the wire uses
 * *bool, so `false` is a legitimate value distinct from "field omitted."
 * A plain `if (payload.splitOverride)` would treat `false` as omission
 * and silently drop a legitimate state change.
 */
function mergeRigState(payload: RigStatePayload): void {
    if (payload.rigIdentity !== undefined) catState.rigIdentity = payload.rigIdentity;
    if (payload.vfoA !== undefined) catState.vfoA = payload.vfoA;
    if (payload.vfoB !== undefined) catState.vfoB = payload.vfoB;
    if (payload.mode !== undefined) catState.mode = payload.mode;
    if (payload.subMode !== undefined) catState.subMode = payload.subMode;
    if (payload.selectedVfo === 'A' || payload.selectedVfo === 'B') {
        catState.selectedVfo = payload.selectedVfo;
    }
    if (payload.splitOverride !== undefined) catState.splitOverride = payload.splitOverride;
    if (payload.power !== undefined) catState.power = payload.power;
}

/**
 * Start tracking `configState.station.enabled` and open/close the
 * EventSource accordingly. Idempotent. Call once from `app.svelte`
 * onMount after `fetchConfig()` settles.
 */
export function startBridge(): void {
    if (rootDispose) return;
    rootDispose = $effect.root(() => {
        $effect(() => {
            if (configState.station.enabled) {
                openSource();
            } else {
                closeSource();
            }
        });
    });
}

/**
 * Tear down the EventSource and the effect.root subscription. Tests
 * call this between cases; production has no need (the page unload
 * cleans up the EventSource).
 */
export function stopBridge(): void {
    closeSource();
    if (rootDispose) {
        rootDispose();
        rootDispose = null;
    }
}

/** Test seam — peek at the active EventSource for assertions. */
export function _activeSourceForTests(): EventSource | null {
    return activeSource;
}
