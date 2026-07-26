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
import { ft8PileupStack } from './ft8Pileup.svelte';

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
    /** Daemon-armed skip-if-silent (deferred Next): a silent cycle ends the
     *  session instead of keying the repeat. Confirm-by-push via ft8-qso. */
    skipArmed: boolean;
    ourReport: string;
    theirReport: string;
    theirPeriod: string;
    fd: boolean;
    /** Reduced type-4 (nonstandard/compound call) session — the SPA renders the
     *  bare-calls→RR73→73 ladder (no grid/report rungs, ADR 0048). */
    type4: boolean;
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
    skipArmed: false,
    ourReport: '',
    theirReport: '',
    theirPeriod: '',
    fd: false,
    type4: false,
    ourClass: '',
    ourSection: '',
    theirClass: '',
    theirSection: '',
});

// Monotonic key source for decode rows. Never reset — uniqueness is all that
// matters and 2^53 ids outlast any session.
let decodeSeq = 0;

// The operator's TX-offset pick persists across a page reload (localStorage) so a
// daemon redeploy — which reloads /app/ to pick up the new build — doesn't silently
// drop the chosen channel. Best-effort: private-mode / disabled storage falls back
// to session-only. It's an audio offset within the FT8 passband (band-independent),
// so a saved value stays valid across bands.
const OFFSET_KEY = 'sm.ft8.selectedOffset';

function loadSelectedOffset(): number | null {
    try {
        const raw = localStorage.getItem(OFFSET_KEY);
        if (raw === null) return null;
        const n = Number(raw);
        return Number.isFinite(n) ? n : null;
    } catch {
        return null;
    }
}

function saveSelectedOffset(hz: number | null): void {
    try {
        if (hz === null) localStorage.removeItem(OFFSET_KEY);
        else localStorage.setItem(OFFSET_KEY, String(hz));
    } catch {
        // storage unavailable — the pick becomes session-only, which is fine
    }
}

class Ft8State {
    /** Transport OPEN — says nothing about whether slots are flowing. */
    connected = $state(false);
    /** Latest slot any event covered, or null before the first / after leaving. */
    slot: Ft8SlotRef | null = $state(null);
    /** Rolling decode history for Band Activity — newest slot on top, freq-ascending within a slot. */
    decodes: DecodeEntry[] = $state([]);
    /** Per-parity occupancy snapshots (Occupancy panel). The daemon emits one report
     *  per RX slot and slots alternate parity, so we keep the last EVEN and last ODD
     *  and show the one matching the TX slot (opposite the worked station) during a
     *  QSO, or the operator's manual pick when idle — see shownParity. null = that
     *  parity not seen yet. The daemon also skips occupancy on our own TX slots, so
     *  the TX-parity snapshot is the last one seen before keying, which is exactly the
     *  state to pick a clear offset from (you can't measure a slot you transmit in). */
    occupiedByParity: { even: Ft8Band[] | null; odd: Ft8Band[] | null } = $state({
        even: null,
        odd: null,
    });
    suggestedByParity: { even: number[] | null; odd: number[] | null } = $state({
        even: null,
        odd: null,
    });
    /** The parity the operator is VIEWING when idle (manual Even/Odd toggle); during a
     *  QSO the shown parity is forced to the TX parity. */
    occupancyParity: 'even' | 'odd' = $state('even');
    /** Audio passband the picker spans (Hz); daemon standard 200–3000 until the first report. */
    passbandLow = $state(200);
    passbandHigh = $state(3000);
    /** Nominal signal width (Hz) — the footprint a TX offset occupies. */
    signalWidth = $state(50);
    /** Band Activity typed filter (funnel popover); session-scoped, empty = no filter. */
    bandFilter = $state('');
    /** Operator-picked TX audio offset (Hz), or null before a pick. Set by the
     *  Occupancy picker (selectOffset); until then TX falls back to the daemon's
     *  top-ranked clear offset via effectiveOffset. Seeded from localStorage so a
     *  page reload (e.g. a daemon redeploy) keeps the chosen channel. */
    selectedOffset: number | null = $state(loadSelectedOffset());
    /** Call-CQ slot parity (WSJT-X "Tx even/1st"). 'next' = fire next slot regardless. */
    txParity: 'next' | 'even' | 'odd' = $state('next');
    /** Which Occupancy presentation the operator prefers — 'spectrum' (continuous
     *  click-anywhere bar, the default — operator 2026-07-13) or 'channels'
     *  (discrete ~50 Hz strip). Both render the same snapshot and write the same
     *  selectedOffset; this is just the view. In-memory (survives a view toggle;
     *  resets on a full refresh). */
    occupancyView: 'channels' | 'spectrum' = $state('spectrum');
    /** Transmit status (ft8-tx). */
    tx: Ft8TxStatus = $state(emptyTxStatus());
    /** Manual sequencer status (ft8-qso). */
    qso: Ft8QsoStatus = $state(emptyQsoStatus());

    /** The offset TX will actually use: the operator's explicit pick, else the
     *  daemon's best-ranked clear offset, else null (no clear channel known yet —
     *  the TX surface gates off). Both the Operate readout and the click-to-answer
     *  handlers read this, so "where will I transmit" is answered in one place. */
    get effectiveOffset(): number | null {
        return this.selectedOffset ?? this.suggested[0] ?? null;
    }

    /** Parity of the slot the operator will TRANSMIT in — the occupancy that actually
     *  matters. During a QSO it's the OPPOSITE of the worked station's parity (you TX
     *  on the alternate slot); idle, it's the manual Even/Odd pick. */
    get shownParity(): 'even' | 'odd' {
        const tp = this.qso.active ? this.qso.theirPeriod : '';
        if (tp === 'even') return 'odd';
        if (tp === 'odd') return 'even';
        return this.occupancyParity;
    }

    /** True while a QSO forces the shown parity (the Even/Odd toggle is locked to TX). */
    get occupancyParityLocked(): boolean {
        const tp = this.qso.active ? this.qso.theirPeriod : '';
        return tp === 'even' || tp === 'odd';
    }

    /** Busy bands for the SHOWN parity — the Occupancy components read this. */
    get occupied(): Ft8Band[] {
        return this.occupiedByParity[this.shownParity] ?? [];
    }

    /** Daemon-ranked clear offsets for the SHOWN parity, best first. Feeds effectiveOffset. */
    get suggested(): number[] {
        return this.suggestedByParity[this.shownParity] ?? [];
    }

    /** Whether the shown parity has received a snapshot yet (gates "Waiting for slot"). */
    get hasOccupancy(): boolean {
        return this.occupiedByParity[this.shownParity] !== null;
    }

    /** Commit the operator's TX-offset pick (Hz). One mutation point so both
     *  Occupancy views funnel through here; picking pins effectiveOffset, ending the
     *  daemon-suggested auto fallback (which otherwise moves each slot). */
    selectOffset(hz: number): void {
        this.selectedOffset = hz;
        saveSelectedOffset(hz);
    }

    /** Switch the Occupancy presentation (persists only in memory). */
    setOccupancyView(v: 'channels' | 'spectrum'): void {
        this.occupancyView = v;
    }

    /** Switch the manually-viewed occupancy parity (idle only; a QSO overrides it). */
    setOccupancyParity(p: 'even' | 'odd'): void {
        this.occupancyParity = p;
    }

    /** Drop the accumulated feed — a band change makes prior rows misleading. */
    clearDecodes(): void {
        this.decodes = [];
    }

    /** Last operating band seen by noteOperatingBand — transition bookkeeping,
     *  plain (non-reactive) and deliberately NOT reset on view close: a band
     *  change made while the FT8 view is closed must still clear the
     *  (persistent, module-singleton) pile-up queue on reopen. */
    lastSeenBand = '';

    /** Band-change watcher for Band Activity (ported from the logging SPA's
     *  Ft8Panel, dogfood niggle 2026-07-19): the FT8 view feeds it the rig's
     *  operating band each render. Crossing a band boundary clears the decode
     *  feed — accumulated rows are the previous band's watering hole and would
     *  be misleading mixed with the new band's traffic. Intra-band dial nudges
     *  don't wipe the list, and an empty band ('' — no/invalid dial freq) is
     *  ignored so a transient unknown doesn't clear it. On a GENUINE
     *  band-to-band change (not the first sighting) the pile-up queue drops
     *  too: its callers were heard on the old band and aren't workable here. */
    noteOperatingBand(band: string): void {
        if (band === '' || band === this.lastSeenBand) return;
        const genuineChange = this.lastSeenBand !== '';
        this.lastSeenBand = band;
        this.clearDecodes();
        if (genuineChange) ft8PileupStack.clear();
    }
}

export const ft8State = new Ft8State();

/*
    Injected seams (ADR 0045). The shipping module read these from configState /
    sessionQsosState / toasts directly; here main.ts wires them.
*/

/** Band Activity display prefs (config.json ft8.display). Defaults until /v1/config
 *  loads: accumulate the feed, cap at 100 rows, don't float CQ rows to the top.
 *  `$state` because /v1/config is fetched async — on a hard reload it lands AFTER
 *  first paint, and readers (cq-to-top ordering, hide-hashed filter) must re-derive
 *  when it does. (A plain `let` self-heals only on the next decode; empty until then.) */
let displayPrefs = $state<{
    feedMode: 'accumulate' | 'single';
    historyMax: number;
    cqToTop: boolean;
    hideHashedCalls: boolean;
}>({
    feedMode: 'accumulate',
    historyMax: 100,
    cqToTop: false,
    hideHashedCalls: false,
});

export function setFt8DisplayPrefs(p: Partial<typeof displayPrefs>): void {
    displayPrefs = { ...displayPrefs, ...p };
}

/** Whether Band Activity floats CQ rows above the rest (config ft8.display.cq_to_top).
 *  Read by the Band Activity renderer; reactive so a late /v1/config re-orders the feed. */
export function ft8CqToTop(): boolean {
    return displayPrefs.cqToTop;
}

/** Whether Band Activity hides decodes with an unresolved hashed call ("<...>")
 *  (config ft8.display.hide_hashed_calls). Config-read like the feed prefs; the
 *  live toggle UI arrives with the app's config-editing surface. */
export function ft8HideHashed(): boolean {
    return displayPrefs.hideHashedCalls;
}

/** The operator's station callsign (config), so Band Activity can flag decodes
 *  that are calling US (`<me> <them> <grid>`). Injected from `/v1/config`, which is
 *  fetched async — so a hard reload (cache bypassed) sets this AFTER first paint.
 *  `$state` so late-arriving config re-derives readers (e.g. the Operate ladder's
 *  CQ rung); a plain `let` left the ladder showing a bare `CQ` with no callsign.
 *  '' matches nothing. An injected seam, not a prop drilled through the view. */
let operatorCall = $state('');

export function setFt8OperatorCall(c: string): void {
    operatorCall = c;
}

export function ft8OperatorCall(): string {
    return operatorCall;
}

/** The operator's Maidenhead grid (config), the near end of Band Activity's
 *  per-CQ short-path bearing + the ladder's CQ-rung grid. Injected async from
 *  `/v1/config` (see operatorCall) — `$state` so a late set re-derives readers.
 *  '' → no bearing shown. */
let myGrid = $state('');

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
    TX action seam (ADR 0045 + ADR 0029/0030/0031/0033). This is the first path
    from this SPA that keys the rig, so it goes through the daemon exactly like the
    tune carrier: the SPA sends an INTENT (arm, call CQ, answer, work, abandon); the
    daemon owns arming, the guaranteed stop, and the CQ→73 sequencing, then confirms
    by push (ft8-tx / ft8-qso SSE). No optimistic local state — the buttons reflect
    ft8State.tx / ft8State.qso. main.ts injects the actions (adapting lib/api
    ft8tx/ft8qso), so this module never imports the api layer.

    Result is {ok,message}: the caller (control bar / Band Activity click) toasts on
    failure; the daemon single-flights competing starts and 409s the loser, so the
    per-component in-flight latches are a nicety over the daemon's own guarantee.
*/
export type Ft8TxResult = { ok: boolean; message: string };

export interface Ft8AnswerArgs {
    theirCall: string;
    theirGrid: string;
    slotUtc: string;
    offsetHz: number;
    opFreqMHz: number;
    fd: boolean;
    /** Reduced type-4 (nonstandard/compound call) answer — bare-calls→RR73→73, no
     *  grid/report (ADR 0048). Mutually exclusive with fd. */
    type4?: boolean;
    /** Our SNR of their CQ — logged as RST_SENT for an FD or type-4 answer (neither
     *  exchanges a report). */
    theirSnr: number;
    /** True when the operator is deliberately working a station ALREADY logged this
     *  session — a repair (they never copied our RR73), a sked, a second report. SM
     *  deduplicates on call+band+mode+freq+date+HH:MM, so without this a second contact
     *  inside one minute is folded into the first and never stored: the operator
     *  transmits a full exchange and sees no row. Only set from an explicit operator
     *  action on a station the UI already shows as worked. */
    allowDuplicate?: boolean;
}

export interface Ft8WorkArgs {
    theirCall: string;
    theirGrid: string;
    /** Our SNR of their call to us — the report we send back (RST_SENT). */
    theirSnr: number;
    slotUtc: string;
    offsetHz: number;
    opFreqMHz: number;
    /** Present when the caller sent an FD exchange — work them Field Day style. */
    fd?: { class: string; section: string };
    /** True when the operator is deliberately working a station ALREADY logged this
     *  session — a repair (they never copied our RR73), a sked, a second report. SM
     *  deduplicates on call+band+mode+freq+date+HH:MM, so without this a second contact
     *  inside one minute is folded into the first and never stored: the operator
     *  transmits a full exchange and sees no row. Only set from an explicit operator
     *  action on a station the UI already shows as worked. */
    allowDuplicate?: boolean;
}

export interface Ft8TxActions {
    arm(armed: boolean): Promise<Ft8TxResult>;
    callCq(
        offsetHz: number,
        opFreqMHz: number,
        parity: 'next' | 'even' | 'odd'
    ): Promise<Ft8TxResult>;
    answerCq(a: Ft8AnswerArgs): Promise<Ft8TxResult>;
    workCaller(a: Ft8WorkArgs): Promise<Ft8TxResult>;
    abandon(): Promise<Ft8TxResult>;
    skip(armed: boolean): Promise<Ft8TxResult>;
}

let txActions: Ft8TxActions | null = null;

export function setFt8TxActions(a: Ft8TxActions): void {
    txActions = a;
}

const txUnavailable: Ft8TxResult = { ok: false, message: 'FT8 transmit is unavailable.' };

/** Arm (true) or disarm (false) the TX path — the operator's consent to key. */
export function armTx(armed: boolean): Promise<Ft8TxResult> {
    return txActions ? txActions.arm(armed) : Promise.resolve(txUnavailable);
}

/** Start a Call-CQ session on the given offset + dial frequency and slot parity. */
export function callCq(
    offsetHz: number,
    opFreqMHz: number,
    parity: 'next' | 'even' | 'odd'
): Promise<Ft8TxResult> {
    return txActions
        ? txActions.callCq(offsetHz, opFreqMHz, parity)
        : Promise.resolve(txUnavailable);
}

/** Start answering a CQ (standard or FD) from a clicked Band Activity decode. */
export function answerCq(a: Ft8AnswerArgs): Promise<Ft8TxResult> {
    return txActions ? txActions.answerCq(a) : Promise.resolve(txUnavailable);
}

/** Start working a station calling us from a clicked directed-at-me decode. */
export function workCaller(a: Ft8WorkArgs): Promise<Ft8TxResult> {
    return txActions ? txActions.workCaller(a) : Promise.resolve(txUnavailable);
}

/** Abandon any active sequenced session. */
export function abandonQso(): Promise<Ft8TxResult> {
    return txActions ? txActions.abandon() : Promise.resolve(txUnavailable);
}

/** Arm/disarm skip-if-silent on the active session (deferred Next, daemon-side):
 *  armed, a silent cycle ends the session instead of keying the repeat. The armed
 *  state renders from qso.skipArmed (confirm-by-push via ft8-qso). */
export function skipQso(armed: boolean): Promise<Ft8TxResult> {
    return txActions ? txActions.skip(armed) : Promise.resolve(txUnavailable);
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
        // Route the snapshot into its parity slot (daemon-provided slot.period) so the
        // even and odd views stay distinct. A period-less payload (shouldn't happen)
        // fills both so it still shows.
        const occ = p.occupied ?? [];
        const sug = p.suggested ?? [];
        const period = p.slot?.period;
        if (period === 'even' || period === 'odd') {
            ft8State.occupiedByParity[period] = occ;
            ft8State.suggestedByParity[period] = sug;
        } else {
            ft8State.occupiedByParity = { even: occ, odd: occ };
            ft8State.suggestedByParity = { even: sug, odd: sug };
        }
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
        const next = displayPrefs.feedMode === 'single' ? fresh : [...fresh, ...ft8State.decodes];
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
            skipArmed: p.skip_armed ?? false,
            maxRepeats: p.max_repeats ?? 0,
            ourReport: p.our_report ?? '',
            theirReport: p.their_report ?? '',
            theirPeriod: p.their_period ?? '',
            fd: p.fd ?? false,
            type4: p.type4 ?? false,
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
    ft8State.occupiedByParity = { even: null, odd: null };
    ft8State.suggestedByParity = { even: null, odd: null };
    ft8State.decodes = [];
    // Keep selectedOffset across a re-open — it's an operator pick, not stream data;
    // clearing it would silently drop the chosen TX channel on a view toggle.
    ft8State.tx = emptyTxStatus();
    ft8State.qso = emptyQsoStatus();
}

/** Test seam — restore module singletons between cases. */
export function resetFt8ForTests(): void {
    opener = null;
    closeFn = null;
    loggedSink = null;
    txActions = null;
    operatorCall = '';
    myGrid = '';
    displayPrefs = {
        feedMode: 'accumulate',
        historyMax: 100,
        cqToTop: false,
        hideHashedCalls: false,
    };
    ft8State.connected = false;
    ft8State.slot = null;
    ft8State.occupiedByParity = { even: null, odd: null };
    ft8State.suggestedByParity = { even: null, odd: null };
    ft8State.occupancyParity = 'even';
    ft8State.decodes = [];
    ft8State.bandFilter = '';
    ft8State.selectedOffset = null;
    saveSelectedOffset(null);
    ft8State.txParity = 'next';
    ft8State.occupancyView = 'spectrum';
    ft8State.tx = emptyTxStatus();
    ft8State.qso = emptyQsoStatus();
    ft8State.lastSeenBand = '';
}
