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

/**
 * Cap on the rolling decode history — a per-device operator preference in
 * localStorage (no visible control yet; a future FT8 display-settings surface
 * exposes it). ~100 rows ≈ several minutes of a busy band — enough to scroll
 * back and spot who's active without unbounded growth. Clamped on load + set so
 * a bad stored value can't hide every row or balloon memory.
 */
const KEY_HISTORY_MAX = 'sm.ft8.history.max';
const DEFAULT_HISTORY_MAX = 100;
const HISTORY_MAX_MIN = 10;
const HISTORY_MAX_MAX = 2000;

function clampHistoryMax(n: number): number {
    if (!Number.isFinite(n)) return DEFAULT_HISTORY_MAX;
    return Math.min(HISTORY_MAX_MAX, Math.max(HISTORY_MAX_MIN, Math.trunc(n)));
}

function loadHistoryMax(): number {
    try {
        const raw = localStorage.getItem(KEY_HISTORY_MAX);
        if (raw === null) return DEFAULT_HISTORY_MAX;
        const n = Number.parseInt(raw, 10);
        return Number.isNaN(n) ? DEFAULT_HISTORY_MAX : clampHistoryMax(n);
    } catch {
        // localStorage unavailable (private mode / quota) — use the default.
        return DEFAULT_HISTORY_MAX;
    }
}

function saveHistoryMax(n: number): void {
    try {
        localStorage.setItem(KEY_HISTORY_MAX, String(n));
    } catch {
        // Best-effort persistence; the in-memory value still applies this session.
    }
}

/**
 * Band Activity feed mode — a per-device operator preference in localStorage (no
 * visible control yet; future FT8 display-settings surface). `accumulate` is the
 * rolling history (newest slot prepended onto prior slots, capped at historyMax);
 * `single` shows only the current 15 s slot's decodes, replacing the list each
 * slot (WSJT-X "clear each period" style).
 */
export type Ft8FeedMode = 'accumulate' | 'single';
const KEY_FEED_MODE = 'sm.ft8.feed.mode';
const DEFAULT_FEED_MODE: Ft8FeedMode = 'accumulate';

function loadFeedMode(): Ft8FeedMode {
    try {
        const raw = localStorage.getItem(KEY_FEED_MODE);
        return raw === 'single' || raw === 'accumulate' ? raw : DEFAULT_FEED_MODE;
    } catch {
        // localStorage unavailable (private mode / quota) — use the default.
        return DEFAULT_FEED_MODE;
    }
}

function saveFeedMode(mode: Ft8FeedMode): void {
    try {
        localStorage.setItem(KEY_FEED_MODE, mode);
    } catch {
        // Best-effort persistence; the in-memory value still applies this session.
    }
}

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
     * frequency-ascending within each slot, capped at historyMax.
     */
    decodes: DecodeEntry[] = $state([]);
    /**
     * Max rows kept in the Band Activity feed (localStorage, per device). The
     * decode handler caps the rolling history to this; setHistoryMax updates it
     * (clamped + persisted) and re-applies the cap immediately.
     */
    historyMax: number = $state(loadHistoryMax());
    /**
     * Band Activity feed mode (localStorage, per device): `accumulate` rolls
     * slots up (capped at historyMax); `single` shows only the current slot.
     */
    feedMode: Ft8FeedMode = $state(loadFeedMode());

    /** Pick (or re-pick) the TX base offset; persisted so it survives a refresh. */
    selectOffset(hz: number): void {
        this.selectedOffset = hz;
        saveTxOffset(hz);
    }

    /**
     * Set the Band Activity row cap. Clamped to a sane range, persisted to
     * localStorage, and applied to the current history at once so shrinking it
     * takes effect without waiting for the next slot.
     */
    setHistoryMax(n: number): void {
        this.historyMax = clampHistoryMax(n);
        saveHistoryMax(this.historyMax);
        if (this.decodes.length > this.historyMax) {
            this.decodes = this.decodes.slice(0, this.historyMax);
        }
    }

    /**
     * Set the Band Activity feed mode (persisted). Switching to `single` trims
     * the current list to just the latest slot at once, so the change is visible
     * without waiting for the next slot (the rows sharing the top startUtc).
     */
    setFeedMode(mode: Ft8FeedMode): void {
        this.feedMode = mode;
        saveFeedMode(mode);
        if (mode === 'single' && this.decodes.length > 0) {
            const top = this.decodes[0].startUtc;
            this.decodes = this.decodes.filter((d) => d.startUtc === top);
        }
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
        // busy single slot too).
        const next = ft8State.feedMode === 'single' ? fresh : [...fresh, ...ft8State.decodes];
        ft8State.decodes = next.slice(0, ft8State.historyMax);
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
