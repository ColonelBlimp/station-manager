/**
 * FT8 occupancy SSE transport — EventSource consumer for `GET /v1/ft8/events`.
 *
 * The daemon's FT8 subsystem publishes an `ft8-occupancy` SSE event per slot — a
 * per-slot `OccupancyReport` (ADR 0029 step a): which audio offsets are busy
 * and a daemon-ranked list of clear offsets for TX-frequency selection. This is
 * decision data, NOT a spectrogram — a single static frame that replaces itself
 * each 15-second slot, so there is no animation or canvas, just a reactive
 * `$state` the display reads. (A slot we transmitted in emits no occupancy event —
 * its audio is our own signal — so the slot clock is driven from `ft8-decode`,
 * which fires every slot; the prior RX-slot occupancy report stays on screen.)
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
import { sessionQsosState } from './sessionQsos.svelte';
import { ft8EnrichState } from './ft8Enrich.svelte';
import { toasts } from './toasts.svelte';
import { qsoDefaults } from './qsoDefaults.svelte';
import { pathInfo } from '../utils/bearing';

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

/** Manual sequencer status (ADR 0031 step e3 / ADR 0033 caller side). Mirrors
 *  `internal/ft8.QsoStatus` (the `ft8-qso` SSE payload): the active contact's role
 *  (answerer/caller), rung, worked call, the message the daemon will send next, and
 *  the unanswered-repeat count. */
export interface Ft8QsoStatus {
    active: boolean;
    role: string; // 'answerer' | 'caller' | 'worker'; '' when idle
    theirCall: string;
    theirGrid: string; // worked station's grid (fills the ladder's opening row); '' until known
    state: string; // answerer: calling|reporting|confirming · caller: calling-cq|reporting|rogering
    nextMessage: string;
    repeats: number;
    // maxRepeats — the unanswered-rung repeat cap, set by the daemon ONLY on the rungs
    // it governs (an answerer pre-73 / a caller working an answerer pre-RR73); 0 on the
    // uncapped (calling CQ) and one-shot (73/RR73) rungs. The "calls left" countdown
    // (= maxRepeats - repeats) is shown iff maxRepeats > 0, so the SPA needs no copy of
    // the cap-vs-one-shot rule.
    maxRepeats: number;
    // Signal reports exchanged, formatted as on the air (e.g. '-12'); '' until known.
    // ourReport = the report we send; theirReport = the one they sent us. The ladder
    // fills its <RST> placeholders from these.
    ourReport: string;
    theirReport: string;
    // theirPeriod — the FT8 slot parity ('even'|'odd') of the slots we PROCESS, i.e.
    // the operator's RX parity (the worked station transmits in these). The single
    // workable parity for the pile-up: only same-parity stations can be queued/worked
    // next. '' when idle.
    theirPeriod: string;
    // fd — an ARRL Field Day session: the exchange carries class/section (below)
    // instead of grid/report, so the SPA renders the FD ladder. The role is still
    // 'answerer' (answer a CQ FD) or 'worker' (work an FD caller).
    fd: boolean;
    // FD exchange — our + their class/section, formatted as on the air (e.g. '1D'/'DX').
    // Ours are known from config at start; theirs '' until known (an answerer learns
    // them in the R-exchange; a worker has them from the picked call). Fill the FD
    // ladder's <CLS>/<SEC> placeholders.
    ourClass: string;
    ourSection: string;
    theirClass: string;
    theirSection: string;
}

const emptyQsoStatus = (): Ft8QsoStatus => ({
    active: false,
    role: '',
    theirCall: '',
    theirGrid: '',
    state: '',
    nextMessage: '',
    repeats: 0,
    maxRepeats: 0,
    ourReport: '',
    theirReport: '',
    theirPeriod: '',
    fd: false,
    ourClass: '',
    ourSection: '',
    theirClass: '',
    theirSection: '',
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
 * a slot change. The daemon does NOT snap or refuse an *overlapping* offset —
 * occupancy overlap is guidance only (docs/ft8.md). It DOES, however, reject an
 * offset outside the usable passband at send time (review 2026-06-19 M1), so a
 * stale/corrupt restored value can't key an out-of-band tone — but a restored
 * value that merely overlaps a signal transmits as picked.
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

/**
 * Selected Call-CQ slot parity (WSJT-X "Tx even/1st"). 'next' = fire on the very
 * next slot regardless of parity (SM's fast default); 'even' = :00/:30; 'odd' =
 * :15/:45. Operating state, per-device localStorage like the TX offset — not a
 * durable per-operator config setting. Applies ONLY to caller-side Call CQ;
 * answering a CQ forces the opposite parity daemon-side, so this never affects it.
 */
export type TxParity = 'next' | 'even' | 'odd';
const KEY_TX_PARITY = 'sm.ft8.tx.parity';

function loadTxParity(): TxParity {
    try {
        const raw = localStorage.getItem(KEY_TX_PARITY);
        return raw === 'even' || raw === 'odd' ? raw : 'next';
    } catch {
        return 'next';
    }
}

function saveTxParity(p: TxParity): void {
    try {
        if (p === 'next') localStorage.removeItem(KEY_TX_PARITY);
        else localStorage.setItem(KEY_TX_PARITY, p);
    } catch {
        // Best-effort; the in-memory value still applies this session.
    }
}

/**
 * Occupancy view mode: 'channels' (the discrete ~50 Hz red/green strip, the default)
 * or 'spectrum' (a continuous bar — signals at their true positions, click-anywhere,
 * graded clear/near/sharing proximity rather than binary red). Both present the SAME
 * per-slot occupancy snapshot and write the same selectedOffset; this is just which
 * presentation the operator prefers. Operating state, per-device localStorage like
 * the offset + parity.
 */
export type OccupancyView = 'channels' | 'spectrum';
const KEY_OCC_VIEW = 'sm.ft8.occupancy.view';

function loadOccupancyView(): OccupancyView {
    try {
        return localStorage.getItem(KEY_OCC_VIEW) === 'spectrum' ? 'spectrum' : 'channels';
    } catch {
        return 'channels';
    }
}

function saveOccupancyView(v: OccupancyView): void {
    try {
        if (v === 'channels') localStorage.removeItem(KEY_OCC_VIEW);
        else localStorage.setItem(KEY_OCC_VIEW, v);
    } catch {
        // Best-effort; the in-memory value still applies this session.
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
     * Selected Call-CQ slot parity (WSJT-X "Tx even/1st") — 'next' (default,
     * fire ASAP) | 'even' | 'odd'. Operating state, persisted per-device like the
     * offset. Consumed by the Call CQ button (caller side only).
     */
    txParity: TxParity = $state(loadTxParity());
    /**
     * Occupancy view mode — 'channels' (default strip) or 'spectrum' (continuous).
     * Operating state, persisted per-device. Drives which picker Ft8OccupancyPanel
     * renders; both write selectedOffset.
     */
    occupancyView: OccupancyView = $state(loadOccupancyView());
    /**
     * Band Activity typed filter (the funnel popover). Token-prefix match: a decode
     * shows when any whitespace token starts with this text (case-insensitive);
     * stations calling us bypass it. Session-scoped, in-memory only — a transient
     * hunt, NOT a durable setting (unlike the hide-hashed toggle, which is config).
     * Empty = no filter.
     */
    bandFilter: string = $state('');
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
    /**
     * Manual sequencer status, hydrated from the `ft8-qso` SSE (daemon-owned,
     * hub-cached so a reconnect replays the active contact). Start/abandon go
     * through lib/api/ft8qso.ts; the daemon confirms by push.
     */
    qso: Ft8QsoStatus = $state(emptyQsoStatus());

    /** Pick (or re-pick) the TX base offset; persisted so it survives a refresh. */
    selectOffset(hz: number): void {
        this.selectedOffset = hz;
        saveTxOffset(hz);
    }

    /**
     * Set the TX offset WITHOUT persisting — used for live drag on the Spectrum bar
     * so the whole UI (footprint, footer readout, Rx pane) follows the pointer, while
     * localStorage is written only once on release (via selectOffset). Writing on
     * every pointermove would hammer storage (synchronous, dozens/sec).
     */
    previewOffset(hz: number): void {
        this.selectedOffset = hz;
    }

    /** Set the Call-CQ slot parity; persisted so it survives a refresh. */
    setTxParity(p: TxParity): void {
        this.txParity = p;
        saveTxParity(p);
    }

    /** Switch the occupancy view (channels ↔ spectrum); persisted across refresh. */
    setOccupancyView(v: OccupancyView): void {
        this.occupancyView = v;
        saveOccupancyView(v);
    }

    /**
     * Is the selected TX channel currently sitting under another signal? The
     * channel spans [selectedOffset, selectedOffset + signalWidth]; it is
     * occupied when that span overlaps any band in the latest occupancy report.
     *
     * Returns null ("unknown") only when no offset is picked OR no occupancy
     * report has arrived yet (slot === null). A report that arrived with an EMPTY
     * occupied list is a valid "clear slot" → false, NOT unknown — conflating the
     * two made a quiet/clear slot read as "channel unknown" (review 2026-06-19 L1).
     *
     * This is the pick-time → TX-time gap closer: a channel chosen clear can have
     * a station land on it a slot or two later, and this re-evaluates each slot
     * as a fresh `occupied` list arrives (the getter reads reactive $state).
     */
    get channelOccupied(): boolean | null {
        if (this.selectedOffset === null) return null;
        if (this.slot === null) return null; // no report yet → genuinely unknown
        if (this.occupied.length === 0) return false; // report arrived, nothing busy → clear
        const lo = this.selectedOffset;
        const hi = this.selectedOffset + this.signalWidth;
        // Half-open overlap: band.low < channel.high && band.high > channel.low.
        return this.occupied.some((b) => b.low_hz < hi && b.high_hz > lo);
    }

    /**
     * Drop the accumulated Band Activity feed. Called on a band change — the
     * prior rows are decodes from a different band's watering hole and would be
     * misleading alongside the new band's traffic. Occupancy/suggested refresh
     * on their own each slot, so only the decode history needs clearing.
     */
    clearDecodes(): void {
        this.decodes = [];
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
        // Slot heartbeat: the decode feed fires every slot (the daemon skips the
        // ft8-occupancy event on our own TX slots), so drive the footer slot clock /
        // parity from here too — before the empty-slot early-return, so a silent or
        // own-TX slot still advances the clock.
        if (report.slot) ft8State.slot = report.slot;

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

    src.addEventListener('ft8-qso', (ev: MessageEvent<string>) => {
        try {
            const p = JSON.parse(ev.data) as Partial<{
                active: boolean;
                role: string;
                their_call: string;
                their_grid: string;
                state: string;
                next_message: string;
                repeats: number;
                max_repeats: number;
                our_report: string;
                their_report: string;
                their_period: string;
                fd: boolean;
                our_class: string;
                our_section: string;
                their_class: string;
                their_section: string;
            }>;
            ft8State.qso = {
                active: p.active ?? false,
                role: p.role ?? '',
                theirCall: p.their_call ?? '',
                theirGrid: p.their_grid ?? '',
                state: p.state ?? '',
                nextMessage: p.next_message ?? '',
                repeats: p.repeats ?? 0,
                maxRepeats: p.max_repeats ?? 0,
                ourReport: p.our_report ?? '',
                theirReport: p.their_report ?? '',
                theirPeriod: p.their_period ?? '',
                fd: p.fd ?? false,
                ourClass: p.our_class ?? '',
                ourSection: p.our_section ?? '',
                theirClass: p.their_class ?? '',
                theirSection: p.their_section ?? '',
            };
        } catch (e) {
            console.warn('[ft8] qso JSON parse failed', e);
        }
    });

    // ft8-logged (EventLogged): a completed exchange the daemon just stored. Add
    // it to the shared session list so FT8 QSOs sit alongside Phone/CW ones for
    // email-out / edit (the daemon's UUID flows through, so both paths work). The
    // event is one-shot (not replayed on reconnect), but a uuid-dedup guards any
    // double-delivery. Distance is computed here from the operator's grid; country
    // comes from the payload (the daemon enriches the contact before submit), so
    // the Session-tab Country column is populated for FT8 rows like Phone/CW ones.
    src.addEventListener('ft8-logged', (ev: MessageEvent<string>) => {
        try {
            const p = JSON.parse(ev.data) as Partial<{
                uuid: string;
                callsign: string;
                freq_hz: number;
                band: string;
                rst_sent: string;
                rst_rcvd: string;
                mode: string;
                time_on: string;
                qso_date: string;
                gridsquare: string;
                country: string;
                name: string;
            }>;
            const uuid = p.uuid ?? '';
            if (uuid === '' || sessionQsosState.items.some((q) => q.uuid === uuid)) return;
            const call = p.callsign ?? '';
            const band = p.band ?? '';
            const grid = p.gridsquare ?? '';
            const myGrid = configState.loggingStation.myGridsquare;
            const path = grid && myGrid ? pathInfo(myGrid, grid) : null;
            sessionQsosState.add({
                uuid,
                callsign: call,
                name: p.name ?? '',
                freqHz: p.freq_hz ?? 0,
                band,
                rstSent: p.rst_sent ?? '',
                rstRcvd: p.rst_rcvd ?? '',
                mode: p.mode ?? 'FT8',
                timeOn: p.time_on ?? '',
                qsoDate: p.qso_date ?? '',
                country: p.country ?? '',
                distanceKm: path ? String(Math.round(path.shortPathDistanceKm)) : '',
            });
            // Refresh the default-logbook QSO count so the LoggingCard header
            // reflects the new row — FT8 QSOs log daemon-side (no form submit), so
            // this is the only place the count gets bumped for them, mirroring the
            // Phone/CW submit path in QsoPanel. Fire-and-forget: a refresh failure
            // leaves the previous count visible until the next QSO succeeds.
            void configState.refreshLogbookCount();
            // Gray the station out in Band Activity so it can't be accidentally
            // re-worked. The worked-before cache is lookup-once per session and was
            // populated (worked:false) before the contact, so without this it stays
            // clickable until a band change.
            ft8EnrichState.markWorked(call, band);
            // FT8 QSOs log daemon-side with no form to clear, so a toast is the only
            // visible "it's in the log" signal. Same setting + wording as the Phone/CW
            // logged-toast (qsoDefaults.notifyQsoStored) so one switch governs both.
            if (qsoDefaults.notifyQsoStored) {
                toasts.info(
                    call ? `QSO logged — ${call}${band ? ` (${band})` : ''}` : 'QSO logged'
                );
            }
        } catch (e) {
            console.warn('[ft8] logged JSON parse failed', e);
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
    ft8State.suggested = [];
    ft8State.occupied = [];
    ft8State.decodes = [];
    // TX + QSO status reset to empty display state; the daemon is authoritative
    // and the hub replays the real `ft8-tx` / `ft8-qso` on the next connect.
    ft8State.tx = emptyTxStatus();
    ft8State.qso = emptyQsoStatus();
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
