/**
 * FT8 occupancy SSE transport — EventSource consumer for `GET /v1/ft8/events`.
 *
 * The daemon's FT8 subsystem publishes one SSE event type, `ft8-occupancy`, a
 * per-slot `OccupancyReport` (ADR 0029 step a): which audio offsets are busy
 * and a daemon-ranked list of clear offsets for TX-frequency selection. This is
 * decision data, NOT a spectrogram — a single static frame that replaces itself
 * once per 15-second slot, so there is no animation or canvas, just a reactive
 * `$state` the display reads.
 *
 * Far simpler than `bridge.svelte.ts`: one event type, read-only, no toast
 * state machine and no manual-state snapshot. The daemon's hub replays the
 * latest report to a freshly-connected subscriber, so a tab opening mid-slot
 * sees current occupancy immediately.
 *
 * **Lifecycle:** the stream is scoped to the FT8 operating-mode view. `Ft8Panel`
 * calls `startFt8()` on mount and `stopFt8()` on destroy, so the EventSource is
 * open exactly while the operator is looking at FT8 (the daemon decodes
 * regardless; the SPA only listens when it matters). The browser handles SSE
 * auto-reconnect on transient drops. When the FT8 subsystem is disabled
 * server-side the route 404s; the stream simply never connects and the display
 * stays in its waiting state.
 */

import { configState } from './config.svelte';

/** One occupied audio-frequency range. Mirrors `internal/ft8.Band` (snake_case wire). */
export interface Ft8Band {
    low_hz: number;
    high_hz: number;
    source?: string; // "decode" | "energy" | "both"
    level?: number; // 0..1 relative energy
}

/** Mirrors `internal/ft8.SlotRef`. */
export interface Ft8SlotRef {
    start_utc: string; // RFC3339 UTC
    period: string; // "even" | "odd"
}

/** Mirrors `internal/ft8.OccupancyReport`. */
interface OccupancyPayload {
    slot: Ft8SlotRef;
    passband: Ft8Band;
    signal_width_hz: number;
    occupied: Ft8Band[] | null;
    suggested: number[] | null;
}

/** One decoded message on the wire. Mirrors `internal/ft8.DecodeLine`. */
interface DecodeLine {
    text: string;
    freq_hz: number;
    dt_s: number;
    snr: number;
}

/** Mirrors `internal/ft8.DecodeReport`. */
interface DecodeReport {
    slot: Ft8SlotRef;
    decodes: DecodeLine[] | null;
}

/** FT8 transmit status (ADR 0030 step e1). Mirrors `internal/ft8.TxState` (the
 *  `ft8-tx` SSE payload): armed/transmitting + the in-flight message/offset and
 *  an i18n error code for the last failed send. */
export interface Ft8TxStatus {
    armed: boolean;
    transmitting: boolean;
    message: string;
    offsetHz: number;
    error: string;
}

const emptyTxStatus = (): Ft8TxStatus => ({
    armed: false,
    transmitting: false,
    message: '',
    offsetHz: 0,
    error: '',
});

/**
 * One row in the accumulating Band Activity list. `startUtc` is the slot's
 * RFC3339 time (formatted for display by the panel); `id` is a stable,
 * monotonic key for the {#each} so rows aren't re-created on every update.
 */
export interface DecodeEntry {
    id: number;
    startUtc: string;
    freqHz: number;
    dtSec: number;
    snr: number;
    text: string;
}

// Band Activity display preferences (row cap + feed mode) and the CQ highlight
// colours are daemon-owned settings now (config.json `ft8.display`, served on
// /v1/config) — read from configState.ft8Display, edited via the FT8 Settings
// tab. They are NOT localStorage: durable, per-operator, not per-browser. The
// decode handler below reads the cap + mode from configState each slot.

/**
 * Selected TX base offset (Hz) — a per-device operator choice in localStorage so
 * the picked channel survives a browser refresh and a view-leave/return, not just
 * a slot change. A restored offset that is now occupied is harmless: the daemon
 * TX gate (ADR 0029) refuses/snaps an overlapping offset at send time.
 */
const KEY_TX_OFFSET = 'sm.ft8.tx.offset';

function loadTxOffset(): number | null {
    try {
        const raw = localStorage.getItem(KEY_TX_OFFSET);
        if (raw === null) return null;
        const n = Number.parseInt(raw, 10);
        return Number.isNaN(n) ? null : n;
    } catch {
        // localStorage unavailable (private mode / quota) — no restored pick.
        return null;
    }
}

function saveTxOffset(hz: number | null): void {
    try {
        if (hz === null) localStorage.removeItem(KEY_TX_OFFSET);
        else localStorage.setItem(KEY_TX_OFFSET, String(hz));
    } catch {
        // Best-effort persistence; the in-memory value still applies this session.
    }
}

// Monotonic key source for decode rows. Never reset — uniqueness is all that
// matters, and 2^53 ids outlast any session.
let decodeSeq = 0;

class Ft8State {
    /** Transport open (EventSource OPEN). Says nothing about whether slots are flowing. */
    connected: boolean = $state(false);
    /** Latest slot this report covers, or null before the first event. */
    slot: Ft8SlotRef | null = $state(null);
    /** Count of occupied bands in the latest slot — the "N busy" readout. */
    busyCount: number = $state(0);
    /** Daemon-ranked clear base offsets (Hz), best first. */
    suggested: number[] = $state([]);
    /**
     * The merged busy bands. Rendered by the occupancy strip (busy shading) and
     * carried for the step-e TX picker.
     */
    occupied: Ft8Band[] = $state([]);
    /**
     * Audio passband the picker spans (Hz). Defaults to the daemon's standard
     * 200–3000 before the first occupancy report; each report refreshes it.
     */
    passbandLow: number = $state(200);
    passbandHigh: number = $state(3000);
    /** Nominal signal width (Hz) — the footprint a TX offset occupies on the strip. */
    signalWidth: number = $state(50);
    /**
     * Operator-selected TX base offset (Hz), or null when none is picked. Set by
     * clicking a clear offset on the strip or a Clear Slots chip. Persisted to
     * localStorage (per device), so the chosen channel survives a slot change, a
     * browser refresh, and a view-leave/return — it is the operator's "this is
     * the channel I chose" until they pick another. Still **inert** until the TX
     * controller (step d/e) consumes it — picking it keys nothing today.
     */
    selectedOffset: number | null = $state(loadTxOffset());
    /**
     * Rolling decode history for the Band Activity feed — newest slot on top,
     * frequency-ascending within each slot. Capped at configState.ft8Display
     * .historyMax (daemon-owned) and shown either accumulated or single-slot per
     * configState.ft8Display.feedMode; both applied by the decode handler.
     */
    decodes: DecodeEntry[] = $state([]);
    /**
     * FT8 transmit status, hydrated from the `ft8-tx` SSE event (daemon-owned,
     * hub-cached so a reconnect replays the current arm state). The SPA reads it;
     * arm/disarm/send go through lib/api/ft8tx.ts and the daemon confirms by push.
     */
    tx: Ft8TxStatus = $state(emptyTxStatus());

    /** Pick (or re-pick) the TX base offset; persisted so it survives a refresh. */
    selectOffset(hz: number): void {
        this.selectedOffset = hz;
        saveTxOffset(hz);
    }
}

export const ft8State = new Ft8State();

let activeSource: EventSource | null = null;

/**
 * Open the EventSource and wire its listeners. Idempotent — a second call with
 * an existing source is a no-op. The path is relative because the SPA is served
 * from the daemon origin.
 */
function openSource(): void {
    if (activeSource) return;

    const src = new EventSource('/v1/ft8/events');
    activeSource = src;

    src.addEventListener('open', () => {
        ft8State.connected = true;
    });

    // EventSource fires `error` on both transient drops (browser auto-retries)
    // and terminal failure; either way the stream isn't carrying frames, so
    // mark disconnected. The latest report stays displayed — stale occupancy is
    // better than a blank panel, and the next slot refreshes it on reconnect.
    src.addEventListener('error', () => {
        ft8State.connected = false;
    });

    src.addEventListener('ft8-occupancy', (ev: MessageEvent<string>) => {
        let payload: OccupancyPayload;
        try {
            payload = JSON.parse(ev.data) as OccupancyPayload;
        } catch (e) {
            console.warn('[ft8] occupancy JSON parse failed', e);
            return;
        }
        ft8State.slot = payload.slot ?? null;
        ft8State.occupied = payload.occupied ?? [];
        ft8State.busyCount = ft8State.occupied.length;
        ft8State.suggested = payload.suggested ?? [];
        if (payload.passband) {
            ft8State.passbandLow = payload.passband.low_hz;
            ft8State.passbandHigh = payload.passband.high_hz;
        }
        if (payload.signal_width_hz > 0) ft8State.signalWidth = payload.signal_width_hz;
    });

    src.addEventListener('ft8-decode', (ev: MessageEvent<string>) => {
        let report: DecodeReport;
        try {
            report = JSON.parse(ev.data) as DecodeReport;
        } catch (e) {
            console.warn('[ft8] decode JSON parse failed', e);
            return;
        }
        const lines = report.decodes ?? [];
        if (lines.length === 0) return; // silent slot — nothing to add

        const startUtc = report.slot?.start_utc ?? '';
        // Frequency-ascending within the slot so the new block reads like a band.
        const fresh: DecodeEntry[] = [...lines]
            .sort((a, b) => a.freq_hz - b.freq_hz)
            .map((d) => ({
                id: decodeSeq++,
                startUtc,
                freqHz: d.freq_hz,
                dtSec: d.dt_s,
                snr: d.snr,
                text: d.text,
            }));
        // `single` shows only this slot; `accumulate` prepends onto prior slots.
        // Either way, cap to the operator's row limit (a safety bound for a very
        // busy single slot too). Both knobs are daemon-owned (configState.ft8Display).
        const display = configState.ft8Display;
        const next = display.feedMode === 'single' ? fresh : [...fresh, ...ft8State.decodes];
        ft8State.decodes = next.slice(0, display.historyMax);
    });

    src.addEventListener('ft8-tx', (ev: MessageEvent<string>) => {
        try {
            const p = JSON.parse(ev.data) as Partial<{
                armed: boolean;
                transmitting: boolean;
                message: string;
                offset_hz: number;
                error: string;
            }>;
            ft8State.tx = {
                armed: p.armed ?? false,
                transmitting: p.transmitting ?? false,
                message: p.message ?? '',
                offsetHz: p.offset_hz ?? 0,
                error: p.error ?? '',
            };
        } catch (e) {
            console.warn('[ft8] tx-state JSON parse failed', e);
        }
    });
}

function closeSource(): void {
    if (!activeSource) return;
    activeSource.close();
    activeSource = null;
    ft8State.connected = false;
    // Forget the last slot so re-entering the FT8 view starts clean rather than
    // flashing a stale slot from a previous visit until the next event lands.
    ft8State.slot = null;
    ft8State.busyCount = 0;
    ft8State.suggested = [];
    ft8State.occupied = [];
    ft8State.decodes = [];
    // TX status reset to its empty display state; the daemon is authoritative and
    // the hub replays the real `ft8-tx` (e.g. still-armed) on the next connect.
    ft8State.tx = emptyTxStatus();
    // selectedOffset is deliberately NOT reset: it's a persisted operator choice
    // (localStorage) that survives view-leave/return and refresh, unlike the
    // per-session occupancy/decode state cleared above.
}

/** Open the occupancy stream. Called from Ft8Panel onMount. Idempotent. */
export function startFt8(): void {
    openSource();
}

/** Close the occupancy stream. Called from Ft8Panel onDestroy. Idempotent. */
export function stopFt8(): void {
    closeSource();
}

/** Test seam — peek at the active EventSource for assertions. */
export function _activeSourceForTests(): EventSource | null {
    return activeSource;
}
