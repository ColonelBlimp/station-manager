/**
 * Bridge SSE transport — EventSource consumer for `GET /v1/rig/events`.
 *
 * Per ADR 0010 (`docs/decisions/0010-rig-sse-wire-shape.md`) + ADR 0027 the
 * daemon's bridge subsystem publishes four SSE event types over a single
 * stream:
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
 *   - `tune-state` — daemon-owned tune-carrier state (ADR 0027). Sets
 *     `bridgeState.tuneActive`, which the Tune button reflects. Includes a
 *     hard auto-off / disconnect-release the operator didn't trigger.
 *
 * **bridgeState flags drive displayedState's three-flag rule** (ADR 0009).
 *
 *   - `connected` mirrors `EventSource.readyState === OPEN` — transport
 *     level only. Says nothing about whether the rig is alive at the
 *     other end.
 *   - `rigResponding` is `false` until the first `rig-state` event
 *     arrives, and `true` again on any subsequent `rig-state`. A
 *     `rig-disconnected` does NOT flip it immediately: the flip is
 *     deferred FLASH_SUPPRESS_MS (with the warn toast), so the FTdx10's
 *     idle false-positive disconnects — recovered by the daemon probe
 *     within ms — don't churn `isLive` and flicker the CAT fields. Only
 *     a disconnect with no `rig-state` inside the window flips it. The
 *     transport `error` path still flips immediately (stream genuinely
 *     down). See ADR 0009.
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
    /**
     * Whether the daemon's tune carrier is currently keyed (ADR 0027).
     * Daemon-authoritative — set from the `tune-state` SSE event, including a
     * hard auto-off / disconnect-release the operator didn't trigger. The Tune
     * button reflects this; there is no optimistic local flip.
     */
    tuneActive: boolean = $state(false);
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

/** Payload shape mirrors `internal/bridge.TuneStatePayload`. */
interface TuneStatePayload {
    active: boolean;
}

let activeSource: EventSource | null = null;
let rootDispose: (() => void) | null = null;

/*
    Disconnect-toast state machine.

    Three states:
      A. idle — neither timer nor toast pending (no known disconnect).
      B. scheduled — `pendingDisconnectTimerId` is set, the warn toast
         has NOT yet been pushed. A fast reconnect at this stage
         cancels the timer and produces no UI churn at all.
      C. visible — `pendingDisconnectToastId` is set, the warn toast
         IS on screen. A reconnect here dismisses the warn and pushes
         the positive "Rig reconnected" info toast.

    Why the schedule-then-push design: when the daemon's
    `liveness_ms` is close to the operator's actual rig-off duration
    (common operator pattern: switch rig off, switch back on within
    seconds), the rig-disconnected SSE event and the rig-state SSE
    event arrive within milliseconds of each other. Pushing the warn
    immediately produced an operator-visible flash — Svelte renders
    the warn between the two SSE messages, then the reconnect
    handler replaces it with the info. The flash is technically
    correct ("disconnect happened, then reconnect") but UX-noisy for
    blips the operator caused intentionally.

    The suppression window (FLASH_SUPPRESS_MS) is the upper bound on
    "how fast can probe round-trip + rig-state delivery be." For an
    alive rig on local serial + loopback SSE it's typically <200ms;
    800ms gives headroom for slower setups while still surfacing
    genuine outages within ~1s of detection.

    Module-local rather than $state on BridgeState because these are
    bookkeeping handles, not state the rest of the SPA should react
    to. Survive EventSource churn (browser auto-reconnect) because
    openSource is the only producer.
*/
const FLASH_SUPPRESS_MS = 800;
let pendingDisconnectTimerId: ReturnType<typeof setTimeout> | null = null;
let pendingDisconnectToastId: number | null = null;

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

        // State B → A: a disconnect was scheduled but never
        // surfaced. The reconnect beat the suppression window — the
        // operator never saw a disconnect toast, so they don't need
        // a "reconnected" toast either. Cancel quietly, no UI churn.
        if (pendingDisconnectTimerId !== null) {
            clearTimeout(pendingDisconnectTimerId);
            pendingDisconnectTimerId = null;
            return;
        }
        // State C → A: a disconnect toast IS visible. Dismiss it
        // and surface the positive "Rig reconnected" toast for
        // symmetric operator closure.
        if (pendingDisconnectToastId !== null) {
            toasts.dismiss(pendingDisconnectToastId);
            pendingDisconnectToastId = null;
            toasts.info(t('bridge.reconnected'));
        }
    });

    src.addEventListener('rig-disconnected', (ev: MessageEvent<string>) => {
        let payload: RigDisconnectedPayload;
        try {
            payload = JSON.parse(ev.data) as RigDisconnectedPayload;
        } catch (e) {
            console.warn('[bridge] rig-disconnected JSON parse failed', e);
            return;
        }
        // rigResponding is NOT flipped here — it's deferred into the
        // suppression timer below, together with the warn toast. The FTdx10
        // fires a false-positive rig-disconnected whenever the rig sits idle
        // (no AUTO pushes — e.g. parked on FT8), and the daemon's read-probe
        // recovers it within milliseconds. Flipping rigResponding immediately
        // dropped isLive for that gap, so displayedState fell back to
        // manualState defaults and every CAT field (VFO, mode, power)
        // flickered ~every liveness_ms. Deferring means a blip that recovers
        // inside the window never drops isLive; only a genuine outage (no
        // rig-state within FLASH_SUPPRESS_MS) flips it.

        // Cancel any prior scheduled flip+push (state B): a newer disconnect
        // wins; its payload is what surfaces if the timer fires.
        if (pendingDisconnectTimerId !== null) {
            clearTimeout(pendingDisconnectTimerId);
            pendingDisconnectTimerId = null;
        }
        // Dismiss any visible disconnect toast (state C): replacing
        // with the newer one. Avoids stacking duplicate warns.
        if (pendingDisconnectToastId !== null) {
            toasts.dismiss(pendingDisconnectToastId);
            pendingDisconnectToastId = null;
        }

        // Schedule the rigResponding=false flip + warn push after the
        // suppression window. If rig-state arrives within FLASH_SUPPRESS_MS,
        // the rig-state handler cancels this timer and neither fires —
        // isLive never drops, no flicker, no toast. ttl=0 (sticky): a genuine
        // outage's warn stays until reconnect or operator manual dismiss.
        //
        // Daemon sends a machine-readable code + per-instance details
        // (e.g. {"code":"rig_no_data"}); the i18n catalogue keyed by
        // `bridge.disconnected.<code>` owns the operator-facing wording.
        const msg = t(`bridge.disconnected.${payload.code}`, payload.details);
        pendingDisconnectTimerId = setTimeout(() => {
            pendingDisconnectTimerId = null;
            bridgeState.rigResponding = false;
            pendingDisconnectToastId = toasts.warn(msg, 0);
        }, FLASH_SUPPRESS_MS);
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

    src.addEventListener('tune-state', (ev: MessageEvent<string>) => {
        let payload: TuneStatePayload;
        try {
            payload = JSON.parse(ev.data) as TuneStatePayload;
        } catch (e) {
            console.warn('[bridge] tune-state JSON parse failed', e);
            return;
        }
        // Daemon-authoritative: reflect whatever the daemon reports, including
        // a hard auto-off / disconnect-release the operator didn't trigger.
        bridgeState.tuneActive = payload.active;
    });
}

function closeSource(): void {
    if (!activeSource) return;
    activeSource.close();
    activeSource = null;
    bridgeState.connected = false;
    bridgeState.rigResponding = false;
    // Deliberate teardown (CAT disabled / stopBridge) — forget tune state so a
    // re-open starts clean. NOT reset on a transport `error` (browser
    // auto-reconnect): the daemon stays authoritative and replays the cached
    // tune-state on reconnect.
    bridgeState.tuneActive = false;
    // Drop both disconnect-toast trackers so a future startBridge
    // after a stopBridge doesn't carry stale state.
    //
    // State B (timer pending, toast not yet pushed): cancel the
    // timer — without this it would fire after closeSource and push
    // a warn for an event no longer relevant.
    if (pendingDisconnectTimerId !== null) {
        clearTimeout(pendingDisconnectTimerId);
        pendingDisconnectTimerId = null;
    }
    // State C (warn toast visible): dismiss it explicitly. Sticky
    // (ttl=0) toasts never auto-expire, so clearing the id alone
    // would leave the toast on screen forever after CAT is disabled
    // / SPA closes the EventSource — operator sees a phantom
    // "disconnect" message with no way to clear it short of a page
    // reload. The dismiss is idempotent (no-op if the operator has
    // already manually clicked the × on the toast).
    if (pendingDisconnectToastId !== null) {
        toasts.dismiss(pendingDisconnectToastId);
        pendingDisconnectToastId = null;
    }
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
