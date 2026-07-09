// FT8 operating state — the reactive mirror of the daemon's FT8 subsystem, fed
// by the injected SSE transport (lib/api/ft8-sse.ts, wired in main.ts per ADR
// 0045 so this module tests without an EventSource). This is the presentation-
// free half of the shipping logging SPA's ft8.svelte.ts monolith: the couplings
// that module reached for directly (config display prefs, the session log, the
// worked-before cache, toasts) become INJECTED seams here.
//
// Lifecycle is VIEW-SCOPED: the FT8 view calls startFt8() on mount and stopFt8()
// on destroy, so the stream — and the daemon's demand-driven audio device — is
// live only while the operator is looking at FT8.

import type {
    Ft8SlotRef,
    Ft8Band,
    DecodeReport,
    OccupancyPayload,
    TxPayload,
    QsoPayload,
    LoggedPayload,
    Ft8EventHandlers,
} from '../api/ft8-sse';

export type { Ft8SlotRef, Ft8Band } from '../api/ft8-sse';

/** One row in the accumulating Band Activity list. `id` is a stable monotonic
 *  key for the keyed {#each} so rows aren't re-created on every update. */
export interface DecodeEntry {
    id: number;
    startUtc: string;
    freqHz: number;
    dtSec: number;
    snr: number;
    text: string;
}

/** FT8 transmit status (ft8-tx). */
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

/** Manual sequencer status (ft8-qso) — the active contact the Operate ladder renders. */
export interface Ft8QsoStatus {
    active: boolean;
    role: string; // 'answerer' | 'caller' | 'worker'; '' when idle
    theirCall: string;
    theirGrid: string;
    state: string;
    nextMessage: string;
    repeats: number;
    maxRepeats: number;
    ourReport: string;
    theirReport: string;
    theirPeriod: string;
    fd: boolean;
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

// Monotonic key source for decode rows. Never reset — uniqueness is all that
// matters and 2^53 ids outlast any session.
let decodeSeq = 0;

class Ft8State {
    /** Transport OPEN — says nothing about whether slots are flowing. */
    connected = $state(false);
    /** Latest slot any event covered, or null before the first / after leaving. */
    slot: Ft8SlotRef | null = $state(null);
    /** Rolling decode history for Band Activity — newest slot on top, freq-ascending within a slot. */
    decodes: DecodeEntry[] = $state([]);
    /** Merged busy bands from the latest occupancy report (Occupancy panel). */
    occupied: Ft8Band[] = $state([]);
    /** Daemon-ranked clear base offsets (Hz), best first (▾/★ markers). */
    suggested: number[] = $state([]);
    /** Audio passband the picker spans (Hz); daemon standard 200–3000 until the first report. */
    passbandLow = $state(200);
    passbandHigh = $state(3000);
    /** Nominal signal width (Hz) — the footprint a TX offset occupies. */
    signalWidth = $state(50);
    /** Band Activity typed filter (funnel popover); session-scoped, empty = no filter. */
    bandFilter = $state('');
    /** Transmit status (ft8-tx). */
    tx: Ft8TxStatus = $state(emptyTxStatus());
    /** Manual sequencer status (ft8-qso). */
    qso: Ft8QsoStatus = $state(emptyQsoStatus());

    /** Drop the accumulated feed — a band change makes prior rows misleading. */
    clearDecodes(): void {
        this.decodes = [];
    }
}

export const ft8State = new Ft8State();

/*
    Injected seams (ADR 0045). The shipping module read these from configState /
    sessionQsosState / toasts directly; here main.ts wires them.
*/

/** Band Activity display prefs (config.json ft8.display). Defaults until /v1/config
 *  loads: accumulate the feed, cap at 100 rows. */
let displayPrefs: { feedMode: 'accumulate' | 'single'; historyMax: number } = {
    feedMode: 'accumulate',
    historyMax: 100,
};

export function setFt8DisplayPrefs(p: Partial<typeof displayPrefs>): void {
    displayPrefs = { ...displayPrefs, ...p };
}

/** The operator's station callsign (config), so Band Activity can flag decodes
 *  that are calling US (`<me> <them> <grid>`). Injected once at boot; '' matches
 *  nothing. Kept here (not a prop drilled through the view) as an injected seam. */
let operatorCall = '';

export function setFt8OperatorCall(c: string): void {
    operatorCall = c;
}

export function ft8OperatorCall(): string {
    return operatorCall;
}

/** The operator's Maidenhead grid (config), the near end of Band Activity's
 *  per-CQ short-path bearing. Injected once at boot; '' → no bearing shown. */
let myGrid = '';

export function setFt8MyGrid(g: string): void {
    myGrid = g;
}

export function ft8MyGrid(): string {
    return myGrid;
}

/** Sink for a completed FT8 QSO (ft8-logged) — main.ts routes it to the shared
 *  session log + enrich cache + toast. Null until wired; the event is one-shot
 *  (not replayed), so a dropped early event just isn't shown. */
let loggedSink: ((p: LoggedPayload) => void) | null = null;

export function setFt8LoggedSink(fn: (p: LoggedPayload) => void): void {
    loggedSink = fn;
}

/*
    Transport handlers — the object handed to the injected opener (openFt8Events).
    Pure state transitions; the transport does the EventSource + JSON parse.
*/
export const ft8Link: Ft8EventHandlers = {
    onOpen(): void {
        ft8State.connected = true;
    },

    // EventSource fires `error` on transient drops (browser auto-retries) and on
    // terminal failure; either way frames aren't flowing. The latest data stays
    // on screen — stale beats blank, and the next slot refreshes on reconnect.
    onError(): void {
        ft8State.connected = false;
    },

    onOccupancy(p: OccupancyPayload): void {
        ft8State.slot = p.slot ?? null;
        ft8State.occupied = p.occupied ?? [];
        ft8State.suggested = p.suggested ?? [];
        if (p.passband) {
            ft8State.passbandLow = p.passband.low_hz;
            ft8State.passbandHigh = p.passband.high_hz;
        }
        if (p.signal_width_hz > 0) ft8State.signalWidth = p.signal_width_hz;
    },

    onDecode(p: DecodeReport): void {
        // Slot heartbeat: ft8-decode fires EVERY slot (the daemon skips
        // ft8-occupancy on our own TX slots), so advance the slot clock here too —
        // before the empty-slot return, so a silent / own-TX slot still ticks.
        if (p.slot) ft8State.slot = p.slot;

        const lines = p.decodes ?? [];
        if (lines.length === 0) return; // silent slot — nothing to add

        const startUtc = p.slot?.start_utc ?? '';
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
        // Either way cap to the row limit (also a safety bound for a busy slot).
        const next =
            displayPrefs.feedMode === 'single' ? fresh : [...fresh, ...ft8State.decodes];
        ft8State.decodes = next.slice(0, displayPrefs.historyMax);
    },

    onTx(p: TxPayload): void {
        ft8State.tx = {
            armed: p.armed ?? false,
            transmitting: p.transmitting ?? false,
            message: p.message ?? '',
            offsetHz: p.offset_hz ?? 0,
            error: p.error ?? '',
        };
    },

    onQso(p: QsoPayload): void {
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
    },

    onLogged(p: LoggedPayload): void {
        if (loggedSink) loggedSink(p);
    },
};

/*
    View-scoped lifecycle. The transport opener is injected (setFt8Transport) so
    this module never imports lib/api — startFt8() opens via it and keeps the
    close fn; stopFt8() closes and clears the volatile per-session state.
*/
type Opener = (handlers: Ft8EventHandlers) => () => void;
let opener: Opener | null = null;
let closeFn: (() => void) | null = null;

export function setFt8Transport(fn: Opener): void {
    opener = fn;
}

/** Open the FT8 stream (idempotent). Called on FT8-view mount. No-op until the
 *  transport is injected (main.ts) or if already open. */
export function startFt8(): void {
    if (closeFn !== null || opener === null) return;
    closeFn = opener(ft8Link);
}

/** Close the FT8 stream + clear volatile state (idempotent). Called on FT8-view
 *  destroy — this is what lets the daemon release the capture device. */
export function stopFt8(): void {
    if (closeFn !== null) {
        closeFn();
        closeFn = null;
    }
    // Forget per-session data so re-entering the view starts clean, not flashing
    // stale occupancy/decodes from a previous visit. The daemon is authoritative
    // for tx/qso and the hub replays them on the next connect.
    ft8State.connected = false;
    ft8State.slot = null;
    ft8State.occupied = [];
    ft8State.suggested = [];
    ft8State.decodes = [];
    ft8State.tx = emptyTxStatus();
    ft8State.qso = emptyQsoStatus();
}

/** Test seam — restore module singletons between cases. */
export function resetFt8ForTests(): void {
    opener = null;
    closeFn = null;
    loggedSink = null;
    operatorCall = '';
    myGrid = '';
    displayPrefs = { feedMode: 'accumulate', historyMax: 100 };
    ft8State.connected = false;
    ft8State.slot = null;
    ft8State.occupied = [];
    ft8State.suggested = [];
    ft8State.decodes = [];
    ft8State.bandFilter = '';
    ft8State.tx = emptyTxStatus();
    ft8State.qso = emptyQsoStatus();
}
