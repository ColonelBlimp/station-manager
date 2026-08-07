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
    AudioLevelPayload,
    Ft8EventHandlers,
} from '../api/ft8-sse';
import { onAudioLevel as audioLevelReceive } from './audioLevel.svelte';
import { ft8PileupStack } from './ft8Pileup.svelte';
import { sessionGet, sessionSet, sessionRemove } from '../utils/storage';
import { frequencyToBand } from '../utils/frequency';
import { rig } from './rig.svelte';

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
    /** Stable cause code for the disarm the frame reports ("" while armed). */
    disarmCause: string;
}

const emptyTxStatus = (): Ft8TxStatus => ({
    armed: false,
    transmitting: false,
    message: '',
    offsetHz: 0,
    error: '',
    disarmCause: '',
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
    /** Daemon-pending Next on a Call-CQ contact: park this answerer at the next slot
     *  evaluation and carry on with the run. Confirm-by-push via ft8-qso. Distinct
     *  from skipArmed — that one ends the session on a SILENT cycle; this one fires
     *  on a station that transmits but never advances. */
    nextArmed: boolean;
    /** An auto-work-callers run is live: the next station calling us is worked with
     *  no click. True on IDLE frames too, which is the point — armed-and-waiting and
     *  stopped are otherwise the same "no active contact" view (ADR 0059). */
    autoWorkArmed: boolean;
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
    /** The Call-CQ run's answerer-selection mode ('' outside caller frames) — how
     *  the drawer tells an operator_pick run from an auto one before any answerer
     *  arrives (ADR 0065; the knob itself is config.json-only). */
    answerMode: string;
    /** operator_pick candidates (ADR 0065): stations currently answering our CQ
     *  that the run can actually work, oldest first — the pile-up drawer's content
     *  during a pick run. Empty outside one; a terminal frame clears it. */
    answerers: Ft8CqAnswerer[];
}

/** One operator_pick candidate off the ft8-qso frame — snr is our measurement of
 *  their signal, the report a pop would send them. */
export interface Ft8CqAnswerer {
    call: string;
    snr: number;
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
    nextArmed: false,
    autoWorkArmed: false,
    ourReport: '',
    theirReport: '',
    theirPeriod: '',
    fd: false,
    type4: false,
    ourClass: '',
    ourSection: '',
    theirClass: '',
    theirSection: '',
    answerMode: '',
    answerers: [],
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
    /** Band each parity's snapshot was MEASURED on ('' = not yet known), taken from
     *  the report's own dial_mhz — not from where the rig happens to be when the
     *  report lands. Occupancy is band-specific, so this is what lets a band change
     *  invalidate it — see occupancyStale. Without it, changing band kept the previous
     *  band's picture on screen as though it were current.
     *  PER PARITY, not one shared tag: the two parities are INDEPENDENT snapshots
     *  that arrive in separate slots, so a single tag meant the first report on the
     *  new band re-validated the other parity's OLD-band data — and since
     *  effectiveOffset falls back to suggested[0], that stale cross-band pick could
     *  become the transmit offset with no operator action (codex P1 on 6088b931).
     *  The exposure is not hypothetical: during a CQ run the TX parity never fills
     *  (the daemon skips our own TX slots), so it is exactly the parity that would
     *  keep serving pre-QSY data. */
    occupancyBandByParity: { even: string; odd: string } = $state({ even: '', odd: '' });
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

    /** True when the held snapshots were captured on a DIFFERENT band than the rig is
     *  on now. Occupancy is band-specific — who is using which audio offset on 15 m
     *  says nothing about 12 m — so a carried-over snapshot must never render as
     *  current. Only invalidates when both bands are known: with CAT off the band can
     *  be blank, and blanking the panel then would be worse than showing what we have. */
    get occupancyStale(): boolean {
        const captured = this.occupancyBandByParity[this.shownParity];
        return rig.band !== '' && captured !== '' && captured !== rig.band;
    }

    /** Busy bands for the SHOWN parity — the Occupancy components read this. */
    get occupied(): Ft8Band[] {
        if (this.occupancyStale) return [];
        return this.occupiedByParity[this.shownParity] ?? [];
    }

    /** Daemon-ranked clear offsets for the SHOWN parity, best first. Feeds effectiveOffset. */
    get suggested(): number[] {
        if (this.occupancyStale) return [];
        return this.suggestedByParity[this.shownParity] ?? [];
    }

    /** Whether the shown parity has a usable snapshot (gates the empty state). */
    get hasOccupancy(): boolean {
        if (this.occupancyStale) return false;
        return this.occupiedByParity[this.shownParity] !== null;
    }

    /** WHY the panel is empty, so it can say something true instead of implying that
     *  data is imminent. 'tx-parity' is the trap: during a session the panel is locked
     *  to the parity we TRANSMIT in, and the daemon deliberately skips occupancy for a
     *  slot we transmitted in (the captured audio is our own signal) — so while a CQ
     *  run continues that parity can NEVER fill, and "Waiting for slot…" waits forever
     *  (dogfood 2026-07-26, on a band change straight into a run). */
    get occupancyEmptyReason(): '' | 'waiting' | 'tx-parity' {
        if (this.hasOccupancy) return '';
        return this.occupancyParityLocked ? 'tx-parity' : 'waiting';
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

/** Notified when a session ends for a reason the operator did NOT cause, with the
 *  daemon's stable code (`dial_moved` | `dial_unknown`) and the call we were
 *  working, if any. Null until wired; main.ts turns it into a toast.
 *
 *  Exists because a safety stop the operator cannot SEE is indistinguishable from a
 *  hang: the ladder simply vanished, and the first on-air read of a working dial
 *  guard was "moving the dial does not stop TX" (dogfood 2026-07-27). */
let sessionEndedSink: ((reason: string, theirCall: string) => void) | null = null;

export function setFt8SessionEndedSink(fn: (reason: string, theirCall: string) => void): void {
    sessionEndedSink = fn;
}

/** Notified when TX disarms underneath the operator, with the daemon's stable
 *  cause code — the arm-only sibling of the session-ended sink. The morning it
 *  exists for: a 200 Hz dial nudge with no session active disarmed TX, and the
 *  only visible change was the armed chip flipping (dogfood 2026-08-07). Fires
 *  only on an OBSERVED armed→disarmed edge with a non-operator cause, and not
 *  when the same event already produced a session-ended notice. */
let txDisarmedSink: ((cause: string) => void) | null = null;

export function setFt8TxDisarmedSink(fn: (cause: string) => void): void {
    txDisarmedSink = fn;
}

/** One-shot suppression: a session end sets it, the disarm edge of the same
 *  teardown consumes it (the daemon publishes the terminal ft8-qso frame first,
 *  from inside the disarm). Cleared on any arm/disarm edge so it cannot
 *  suppress a LATER unrelated disarm. */
let suppressDisarmNoticeFor = '';

/** A disarm notice held while the session still shows locally active: the hub
 *  replays cached events tx-BEFORE-qso, so after a reconnect the disarm edge
 *  arrives ahead of the terminal frame that explains it (codex P2 on a11079b8).
 *  The next qso frame decides — a reasoned session end drops it (one notice per
 *  event), a reasonless frame flushes it (a safety stop is never lost). */
let heldDisarmNotice = '';

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

// Stations the sequencer has engaged since this tab loaded. Deliberately keyed on
// ENGAGEMENT, not on a completed QSO: an abandoned contact ends up in here too, and
// that is the safe direction. The only consumer is the deliberate-repeat decision,
// where the flag merely bypasses duplicate protection — so over-marking stores a
// genuinely new contact (correct), while under-marking silently loses a real on-air
// exchange. In-memory like the pile-up stack: live operating state, not durable.
// Deliberately a plain Set, not SvelteSet: nothing RENDERS from it. It is read
// imperatively by the click handlers, and by the pile-up drain's $effect — which
// already re-runs on `ft8State.qso.active`, the very signal that also updates this
// set, so the effect always sees a current value without the set itself being a
// reactive dependency. Reactivity here would buy nothing and cost a proxy on a
// hot path.
// Survives a RELOAD, dies with the tab — sessionStorage matches the set's semantics
// exactly. Without it, a reload inside the async-logging window drops the engagement
// before `session.qsos` has learned it, which is precisely the data-loss case this
// set exists to prevent (codex a5667b00 P1).
const engagedStoreKey = 'sm.ft8.engaged';

// eslint-disable-next-line svelte/prefer-svelte-reactivity
const engagedThisSession = new Set<string>();

/** Keyed by CALL|BAND, matching how both consumers define a same-session duplicate.
 *  Callsign alone would classify the FIRST contact on a NEW band as a repeat, sending
 *  allow_duplicate:true — and `force` makes Submit use a RANDOM dedupe key, so that
 *  contact loses its duplicate protection entirely (codex a5667b00 P2). */
function engagedKey(call: string, band: string): string {
    return `${call.trim().toUpperCase()}|${band.trim().toUpperCase()}`;
}

function loadEngaged(): string[] {
    const raw = sessionGet(engagedStoreKey);
    if (raw === null) return [];
    try {
        const v: unknown = JSON.parse(raw);
        return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
    } catch {
        return []; // corrupt payload — start clean rather than throw on boot
    }
}

function saveEngaged(): void {
    sessionSet(engagedStoreKey, JSON.stringify([...engagedThisSession]));
}

/** True when this session has already engaged `call` — used to decide whether an
 *  operator action is a DELIBERATE repeat. Complements `session.qsos`, which only
 *  learns about a contact once the daemon has finished logging it asynchronously. */
export function ft8EngagedThisSession(call: string, band: string): boolean {
    return engagedThisSession.has(engagedKey(call, band));
}

/** Restore the engaged set from session storage. Called at MODULE INIT (below) and
 *  by tests to model a page reload — deliberately the same function, so the boot
 *  path is the one the tests actually exercise. The reset seam further down models
 *  a fresh TAB by clearing storage as well. */
export function reloadFt8EngagedFromStorage(): void {
    engagedThisSession.clear();
    for (const k of loadEngaged()) engagedThisSession.add(k);
}

// A reload rebuilds this module; the engaged set comes back with it.
reloadFt8EngagedFromStorage();

/** Test seam: forget the engaged-call set (a fresh tab starts empty). */
export function resetFt8EngagedThisSession(): void {
    engagedThisSession.clear();
    sessionRemove(engagedStoreKey);
}

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
    /** Per-click auto-work intent (ADR 0065): true arms an auto-work run alongside
     *  this contact (ctrl+shift gesture or the Auto-work toggle). Daemon-gated on
     *  ft8.tx.auto_work_callers; absent/false works this station only and clears
     *  any previously-armed run. */
    autoWork?: boolean;
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
    /** Per-click auto-work intent (ADR 0065): true arms an auto-work run alongside
     *  this contact (ctrl+shift gesture or the Auto-work toggle). Daemon-gated on
     *  ft8.tx.auto_work_callers; absent/false works this station only and clears
     *  any previously-armed run. */
    autoWork?: boolean;
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
    next(): Promise<Ft8TxResult>;
    /** Stop the auto-work run WITHOUT ending any active contact (ADR 0065 — the
     *  Auto-work pill's click action; abandon stops both). */
    stopAutoWork(): Promise<Ft8TxResult>;
    /** Commit a listed answerer into an operator_pick Call-CQ run (ADR 0065) —
     *  the pile-up drawer's per-candidate Work action. */
    pickAnswerer(call: string): Promise<Ft8TxResult>;
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

// Standing auto-work intent (ADR 0065): the visible toggle's state — "the next
// contact I start also starts a run". The RUN itself lives in the daemon
// (qso.autoWorkArmed); this is only the operator's pre-arm for the next click,
// so it is in-memory and dies with the tab. One-shot by design: consumed (reset)
// when a start carries it, so a forgotten toggle can't arm runs for the rest of
// the sitting.
export const ft8AutoWorkIntent = $state({ on: false });

// Intent sent on the last start, awaiting the daemon's verdict: the arm is gated
// daemon-side on ft8.tx.auto_work_callers, and a refusal is visible ONLY as the
// active frame arriving with auto_work_armed=false (a refused arm must never
// block the contact — G3). The sink says so once; without it the operator watches
// a toggle they set produce no pill, unexplained.
let pendingAutoWorkIntent = false;
let autoWorkRefusedSink: (() => void) | null = null;

export function setFt8AutoWorkRefusedSink(fn: (() => void) | null): void {
    autoWorkRefusedSink = fn;
}

/** Start answering a CQ (standard or FD) from a clicked Band Activity decode. */
export function answerCq(a: Ft8AnswerArgs): Promise<Ft8TxResult> {
    if (!txActions) return Promise.resolve(txUnavailable);
    // FD/type-4 never arm a run, so an intent that slips through on one must not
    // establish a pending verdict — the daemon's unarmed frame would read as a
    // false gate refusal (codex P2 on 7de6708e; call sites also exclude them).
    if (a.autoWork && !a.fd && !a.type4) {
        pendingAutoWorkIntent = true;
        ft8AutoWorkIntent.on = false; // consumed — one-shot
    }
    return txActions.answerCq(a).then((r) => {
        if (!r.ok) pendingAutoWorkIntent = false; // start refused — no frame will come
        return r;
    });
}

/** Start working a station calling us from a clicked directed-at-me decode. */
export function workCaller(a: Ft8WorkArgs): Promise<Ft8TxResult> {
    if (!txActions) return Promise.resolve(txUnavailable);
    if (a.autoWork) {
        pendingAutoWorkIntent = true;
        ft8AutoWorkIntent.on = false; // consumed — one-shot
    }
    return txActions.workCaller(a).then((r) => {
        if (!r.ok) pendingAutoWorkIntent = false;
        return r;
    });
}

/** Stop the auto-work run only — the Auto-work pill's click action (ADR 0065). */
export function stopAutoWork(): Promise<Ft8TxResult> {
    return txActions ? txActions.stopAutoWork() : Promise.resolve(txUnavailable);
}

/** Pop a listed operator_pick answerer into the Call-CQ run (ADR 0065). The commit
 *  is confirmed by push — the ft8-qso frame the pop publishes. */
export function pickAnswerer(call: string): Promise<Ft8TxResult> {
    return txActions ? txActions.pickAnswerer(call) : Promise.resolve(txUnavailable);
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

/** Move on from a stuck Call-CQ contact without ending the run (see nextFt8Answerer).
 *  The pending state renders from qso.nextArmed (confirm-by-push via ft8-qso). */
export function nextAnswerer(): Promise<Ft8TxResult> {
    return txActions ? txActions.next() : Promise.resolve(txUnavailable);
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
        // Attribute the snapshot to the band it was MEASURED on, from the dial the
        // daemon read while capturing the slot (dial_mhz). Reading rig.band here
        // instead would label the report with wherever the rig is NOW, which is wrong
        // for every report in flight across a QSY — and unfixable downstream, since a
        // report's publication lags its capture by the decode, so neither its age nor
        // its distance from the last report can show the capture happened after the
        // band change (codex P1 on 0462eb7b and again on f6ea7ce2).
        // No dial reaching us means the daemon has no CAT at all: a CAT-attached
        // daemon SKIPS any slot it cannot place rather than sending it unattributed
        // (internal/ft8 decodeLoop), so this fallback is only ever the audio-only
        // deployment. There it keeps the pre-existing one-slot ambiguity after a
        // manual band change, and that is display-only — FT8 transmit is refused
        // without a writable rig, so a mislabelled panel cannot steer anything.
        const band =
            p.dial_mhz && p.dial_mhz > 0 ? frequencyToBand(p.dial_mhz * 1_000_000) : rig.band;
        // Stamp the band on the parity that actually arrived — never both; see
        // occupancyBandByParity.
        if (period === 'even' || period === 'odd') {
            ft8State.occupancyBandByParity[period] = band;
        } else {
            ft8State.occupancyBandByParity = { even: band, odd: band };
        }
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
        const wasArmed = ft8State.tx.armed;
        ft8State.tx = {
            armed: p.armed ?? false,
            transmitting: p.transmitting ?? false,
            message: p.message ?? '',
            offsetHz: p.offset_hz ?? 0,
            error: p.error ?? '',
            disarmCause: p.disarm_cause ?? '',
        };
        // Announce a disarm only on an OBSERVED armed→disarmed edge: a replayed
        // frame after a reconnect starts from armed=false and is not a disarm
        // happening now. "operator" is silent (they pressed the button), and so
        // is a missing cause (an older daemon — nothing truthful to say). The
        // session-end suppression mutes only its own teardown's disarm frame:
        // any LATER disarm edge requires an observed armed frame first, and
        // every armed frame clears the staged suppression below — that clear is
        // what makes the suppression one-shot.
        if (wasArmed && !ft8State.tx.armed) {
            const cause = ft8State.tx.disarmCause;
            const suppressed = cause !== '' && cause === suppressDisarmNoticeFor;
            if (!suppressed && cause !== '' && cause !== 'operator') {
                if (ft8State.qso.active) {
                    // A session is (locally) live, so a qso frame is in flight
                    // behind this one — replay order, or a teardown mid-stream.
                    // Hold the notice and let that frame decide.
                    heldDisarmNotice = cause;
                } else {
                    txDisarmedSink?.(cause);
                }
            }
        } else if (ft8State.tx.armed) {
            suppressDisarmNoticeFor = '';
            heldDisarmNotice = '';
        }
    },

    // BASELINE DEBT 2026-07-31 (complexity 32) — dispatch over the FT8 QSO status
    // payload; one arm per session state the daemon can publish.
    // eslint-disable-next-line complexity
    onQso(p: QsoPayload): void {
        // Remember every station the sequencer has actually engaged this session.
        // `session.qsos` cannot answer "did we just work them?" in time: the daemon
        // publishes the terminal idle BEFORE its enrich+submit goroutine runs, so
        // there is a window (up to the 30 s submit timeout) where a contact is over
        // on the air but absent from the session list — and on the single-rung type-4
        // path the operator can re-click well inside it (codex 0f08d2b2 P1). The
        // ft8-logged event that feeds session.qsos is also one-shot and not replayed,
        // so a missed event or a fresh tab never learns it at all.
        // Resolve a sent auto-work intent against the daemon's verdict: the arm is
        // gated on ft8.tx.auto_work_callers and a refusal is visible only as the
        // active frame carrying auto_work_armed=false (G3 — a refused arm never
        // blocks the contact). One shot: cleared on the first active frame either way.
        if (p.active === true && pendingAutoWorkIntent) {
            pendingAutoWorkIntent = false;
            if (p.auto_work_armed !== true) autoWorkRefusedSink?.();
        }
        const engaged = (p.their_call ?? '').trim();
        // Band comes from the SESSION-PINNED dial the daemon reports, never from live
        // rig state: the two are independent streams, so a band change mid-contact —
        // or a skew between them — would file a 20 m contact under 40 m, or under
        // both, and persistence would make that survive a reload (codex 18008c10 P1).
        // No dial (older daemon) → record nothing rather than guess wrong.
        const mhz = p.dial_freq_mhz ?? 0;
        if (engaged !== '' && mhz > 0) {
            const band = frequencyToBand(Math.round(mhz * 1_000_000));
            if (band !== '') {
                const before = engagedThisSession.size;
                engagedThisSession.add(engagedKey(engaged, band));
                if (engagedThisSession.size !== before) saveEngaged();
            }
        }
        // A session ending with a reason is announced BEFORE the state is replaced:
        // the terminal frame carries no callsign, so the station we were working has
        // to come from the state we are about to overwrite.
        const endReason = (p.end_reason ?? '').trim();
        if (endReason !== '' && p.active !== true && ft8State.qso.active) {
            sessionEndedSink?.(endReason, ft8State.qso.theirCall);
            // The guard that ended this session also disarms TX. One knob turn
            // deserves one notice, whichever frame lands first: stage a one-shot
            // suppression for the disarm frame that FOLLOWS (live order), and
            // drop a notice held from a disarm frame that PRECEDED (replay
            // order) — this frame is the explanation either way.
            suppressDisarmNoticeFor = endReason;
            if (heldDisarmNotice === endReason) {
                heldDisarmNotice = '';
            } else if (heldDisarmNotice !== '') {
                // A different cause than this session end: the tx and qso replay
                // caches are independent, so this is a SECOND event from the same
                // outage (re-arm, then another safety disarm) — say both, in
                // event order (the held disarm postdates the session end).
                const cause = heldDisarmNotice;
                heldDisarmNotice = '';
                txDisarmedSink?.(cause);
            }
        } else if (heldDisarmNotice !== '') {
            // The frame the hold was waiting on explains nothing (no reason —
            // an abandon or a completed contact), so the disarm speaks for
            // itself after all.
            const cause = heldDisarmNotice;
            heldDisarmNotice = '';
            txDisarmedSink?.(cause);
        }
        ft8State.qso = {
            active: p.active ?? false,
            role: p.role ?? '',
            theirCall: p.their_call ?? '',
            theirGrid: p.their_grid ?? '',
            state: p.state ?? '',
            nextMessage: p.next_message ?? '',
            repeats: p.repeats ?? 0,
            skipArmed: p.skip_armed ?? false,
            nextArmed: p.next_armed ?? false,
            autoWorkArmed: p.auto_work_armed ?? false,
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
            answerMode: p.answer_mode ?? '',
            answerers: p.answerers ?? [],
        };
    },

    onLogged(p: LoggedPayload): void {
        if (loggedSink) loggedSink(p);
    },

    // RX level meter (audioLevel.svelte) — a pure hand-off: classification,
    // staleness and presentation all live there and in AudioLevelCard.
    onAudioLevel(p: AudioLevelPayload): void {
        audioLevelReceive(p);
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
    ft8State.occupancyBandByParity = { even: '', odd: '' };
    ft8State.decodes = [];
    // Keep selectedOffset across a re-open — it's an operator pick, not stream data;
    // clearing it would silently drop the chosen TX channel on a view toggle.
    ft8State.tx = emptyTxStatus();
    ft8State.qso = emptyQsoStatus();
}

/** Test seam — restore module singletons between cases. */
export function resetFt8ForTests(): void {
    // A fresh tab starts with no engaged stations; clearing here (rather than only
    // via the dedicated reset) keeps the set from leaking across tests.
    resetFt8EngagedThisSession();
    opener = null;
    closeFn = null;
    loggedSink = null;
    sessionEndedSink = null;
    txDisarmedSink = null;
    suppressDisarmNoticeFor = '';
    heldDisarmNotice = '';
    txActions = null;
    ft8AutoWorkIntent.on = false;
    pendingAutoWorkIntent = false;
    autoWorkRefusedSink = null;
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
    ft8State.occupancyBandByParity = { even: '', odd: '' };
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
