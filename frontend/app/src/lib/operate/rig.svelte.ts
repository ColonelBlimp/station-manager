// Rig state — the rig-provided operating context (band/mode/freq) that merges
// into a QSO at log time; qso.svelte deliberately excludes these fields so the
// draft stays operator-entered data only. Two sources, same as the shipping
// logging SPA: CAT-connected = the bridge's rig-state SSE writes here (rig is
// authoritative, panel fields lock); CAT-off = manual entry in the Rig panel.
//
// The CAT-link transitions live here as `catLink` — pure state, fed by the
// injected SSE transport (lib/api/rig-sse.ts, wired in main.ts per ADR 0045)
// so they test without an EventSource. The rig-state wire carries the rig's
// OWN mode literal (e.g. DATA-U); resolution to the operator-friendly form
// goes through the bridge mode_mappings table injected from /v1/config.

import { formatFrequency, frequencyToBand } from '../utils/frequency';
import { parseFrequency } from '../validators/frequency';
import type { RigStatePayload, BridgeCodePayload, TuneStatePayload } from '../api/rig-sse';

export type CatLink = 'off' | 'connected' | 'lost';

// "Pre-filled from last session, so it's a fast confirm not data entry"
// (ADR 0044): the operating context persists across sessions. The
// CONFIRMATION deliberately does not — the gate is a per-session assertion
// that the values are right NOW.
const RIG_CTX_KEY = 'sm.rig.context';

function loadRigContext(): { band: string; mode: string; freq: string } {
    const fallback = { band: '20m', mode: 'USB', freq: '14.255.000' };
    try {
        const raw = localStorage.getItem(RIG_CTX_KEY);
        if (raw === null) return fallback;
        const p: unknown = JSON.parse(raw);
        if (typeof p !== 'object' || p === null) return fallback;
        const o = p as Record<string, unknown>;
        return {
            band: typeof o.band === 'string' && o.band !== '' ? o.band : fallback.band,
            mode: typeof o.mode === 'string' && o.mode !== '' ? o.mode : fallback.mode,
            freq: typeof o.freq === 'string' && o.freq !== '' ? o.freq : fallback.freq,
        };
    } catch {
        return fallback; // corrupt storage never blocks boot
    }
}

export const rig: {
    band: string;
    mode: string;
    modeLiteral: string;
    freq: string;
    vfoA: number | null;
    vfoB: number | null;
    selectedVfo: 'A' | 'B';
    identity: string;
    cat: CatLink;
    confirmedBand: string | null;
    linkError: string;
    tuneActive: boolean;
} = $state({
    // band/mode: operator-friendly literals (mode is a sideband, not an ADIF
    // family — resolved at submit). freq is the SELECTED VFO's frequency in
    // the SM dot-grouped display form ("14.199.950"). Manual entry accepts
    // that OR decimal MHz ("14.255") — both parse unambiguously to Hz via
    // validators/frequency parseFrequency, which every consumer uses (never
    // parseFloat: it reads the grouped form as 14.199). Rig-pushed values are
    // always written in the grouped form, so a confirmed-manual field starts
    // out reading like the rig it mirrors.
    ...loadRigContext(),
    // Rig name from the wire's rigIdentity (e.g. "FTdx10"); '' until a rig has
    // been seen. Kept across a loss — naming the rig that went away beats a
    // blank — and cleared only by the test reset.
    identity: '',
    // Per-VFO Hz + selection, straight from the merge of partial rig-state
    // payloads (null until the rig reports one). The CAT-locked panel shows
    // both; freq above stays the SELECTED VFO's derivation — the value that
    // merges into a logged QSO. Manual entry has no VFO concept.
    vfoA: null,
    vfoB: null,
    selectedVfo: 'A',
    // The rig's OWN mode literal (e.g. "DATA-U"), kept beside the friendly
    // `mode` ("FT8") because the live Option-A dropdown offers + sends literals,
    // while the log resolves ADIF from the friendly form. '' until a rig reports.
    modeLiteral: '',
    cat: 'off',
    // The band the operator last confirmed (Set/Confirm). null = nothing
    // confirmed this session; a band change re-arms the gate by comparison.
    confirmedBand: null,
    linkError: '',
    // Tune-carrier state (ADR 0027), daemon-authoritative: set only from the
    // tune-state SSE (confirm-by-push), never an optimistic local flip — so an
    // auto-off the operator didn't trigger still clears the button.
    tuneActive: false,
});

/**
 * Bridge capability advertisement (BridgeInfo, ADR 0026), injected from
 * /v1/config at boot. Every rig-control surface gates on this: a control the
 * rig doesn't expose is hidden, not merely disabled. Empty until config loads
 * (and stays empty when the bridge is disabled), so nothing keys the rig
 * before the daemon has said what the rig can do.
 *
 *   ops      — exposed command names (set_freq, swap_vfo, set_mode, …).
 *   tune     — the rig supports the tune carrier (ADR 0027).
 *   rigModes — the rig's OWN mode literals, for the live mode dropdown (Option A).
 */
export const rigCaps: { ops: string[]; tune: boolean; rigModes: string[] } = $state({
    ops: [],
    tune: false,
    rigModes: [],
});

export function setRigCaps(c: { ops: string[]; tune: boolean; rigModes: string[] }): void {
    rigCaps.ops = c.ops;
    rigCaps.tune = c.tune;
    rigCaps.rigModes = c.rigModes;
}

/**
 * The operator's configured operating bands (station.operating_bands, ADR/
 * dogfood 2026-07-09), injected from /v1/config. ONE source for every band
 * surface — the band-selector grid, the FT8 band buttons, and the keyboard
 * band-jump (Slice 4) — so a band change is consistent whichever way the
 * operator operates. Order is preserved as the operator listed it (the grid,
 * and later the digit shortcuts, follow that order). Empty = unset → the
 * DEFAULT_BANDS HF..6m set, so an existing config behaves exactly as before.
 */
export const DEFAULT_BANDS = [
    '160m',
    '80m',
    '60m',
    '40m',
    '30m',
    '20m',
    '17m',
    '15m',
    '12m',
    '10m',
    '6m',
];

const bandPrefs: { list: string[] } = $state({ list: [] });

export function setOperatingBands(b: string[]): void {
    // Drop blanks + dedupe (keep first, preserve order). The daemon already
    // validates band names + rejects duplicates at load/PUT; this is defensive
    // normalisation of the wire value. Plain-array dedupe (not a Set) to satisfy
    // the svelte reactivity lint in this .svelte.ts module.
    const list: string[] = [];
    for (const x of b) {
        if (typeof x === 'string' && x !== '' && !list.includes(x)) list.push(x);
    }
    bandPrefs.list = list;
}

/** The bands to offer — the configured list, or the HF..6m default when unset. */
export function operatingBands(): string[] {
    return bandPrefs.list.length > 0 ? bandPrefs.list : DEFAULT_BANDS;
}

/** True when the configured rig exposes the named command (BridgeInfo.ops). */
export function hasOp(op: string): boolean {
    return rigCaps.ops.includes(op);
}

/*
    Rig-control write seam (ADR 0045: components never import the api layer).
    The daemon owns the wire encoding + all TX-keying discipline; the SPA sends
    only an intent. The tune sender is injected in main.ts (adapting the
    lib/api/rig-tune client); the later slices add a command sender the same way.
    An outcome is {ok,message} — the caller (the Tune button) toasts on failure;
    the button's on/off state comes from the tune-state SSE, not this return.
*/
export type RigWriteResult = { ok: boolean; message: string };
export type TuneSender = (active: boolean) => Promise<RigWriteResult>;

let tuneSender: TuneSender | null = null;

export function setTuneSender(fn: TuneSender): void {
    tuneSender = fn;
}

/**
 * Toggle the tune carrier: send the OPPOSITE of the current pushed state. No
 * optimistic flip — the button reflects tuneActive, updated when the daemon's
 * tune-state event lands (confirm-by-push, matching the shipping SPA). Returns
 * the write outcome so the caller can surface a failure.
 */
export async function toggleTune(): Promise<RigWriteResult> {
    if (tuneSender === null) return { ok: false, message: 'Tune control is unavailable.' };
    return tuneSender(!rig.tuneActive);
}

/*
    Rig command seam — one semantic op + optional scalar (ADR 0026). Injected in
    main.ts (adapting lib/api/rig-command); the daemon owns the wire encoding.
    Actions return the outcome so the caller can toast a failure — matching the
    tune path — and so an optimistic write can roll back on a non-ok result.
*/
export type CommandSender = (op: string, value?: string | number) => Promise<RigWriteResult>;

let commandSender: CommandSender | null = null;

export function setCommandSender(fn: CommandSender): void {
    commandSender = fn;
}

async function driveRig(op: string, value?: string | number): Promise<RigWriteResult> {
    if (commandSender === null) return { ok: false, message: 'Rig control is unavailable.' };
    return commandSender(op, value);
}

/**
 * Select VFO A or B (the mouse click-to-swap path; the keyboard swap shares
 * swapVfo). CAT off → local selection only (the app shows no VFO boxes in
 * manual mode, so this is inert there). CAT live → the FTdx10 has no "move onto
 * a specific VFO" CAT command that changes the operating frequency (VS toggles
 * a flag only); the working op is swap_vfo (SV;), which exchanges A↔B. With two
 * VFOs, "select the other" === "swap", so a live select of the non-current VFO
 * swaps; selecting the already-current one is a no-op.
 */
export async function selectVfo(vfo: 'A' | 'B'): Promise<RigWriteResult> {
    if (rig.cat !== 'connected') {
        rig.selectedVfo = vfo;
        return { ok: true, message: '' };
    }
    if (vfo === rig.selectedVfo) return { ok: true, message: '' };
    return swapVfoLive();
}

/**
 * Swap the VFOs (A↔B) — the Shift+Ctrl VFO swap and the click-to-swap path.
 * CAT off → toggle the local selection. CAT live → drive the rig's swap (SV;).
 */
export async function swapVfo(): Promise<RigWriteResult> {
    if (rig.cat !== 'connected') {
        rig.selectedVfo = rig.selectedVfo === 'A' ? 'B' : 'A';
        return { ok: true, message: '' };
    }
    return swapVfoLive();
}

/*
    Drive the rig's VFO swap (swap_vfo), capability-gated.

    Optimistic VFO-B mirror — the one deliberate SPA write to the rig mirror (a
    narrow exception, from the shipping SPA's ADR 0009 note). A dual-VFO rig
    (FTdx10, SV;) pushes BOTH VFOs back, so confirm-by-push repaints both boxes
    and this mirror is overwritten at once — harmless. But a single-RX rig
    (IC-7300, CI-V) confirms the swap with a bare ACK and never pushes VFO-B,
    and the daemon's read-after-swap refreshes only the operating VFO (→ vfoA).
    After the swap that rig's VFO-B genuinely holds the old VFO-A, so reflect it
    immediately; the read-back fills the new vfoA ~100 ms later. Rolled back on a
    non-ok outcome so a rejected command doesn't strand a false VFO-B on screen.
*/
async function swapVfoLive(): Promise<RigWriteResult> {
    if (!hasOp('swap_vfo')) return { ok: false, message: 'This rig cannot swap VFOs.' };
    const prevVfoB = rig.vfoB;
    rig.vfoB = rig.vfoA;
    const r = await driveRig('swap_vfo');
    if (!r.ok) rig.vfoB = prevVfoB;
    return r;
}

/**
 * Set the operating mode. CAT off → write the operator-friendly pick locally
 * (the log resolves ADIF at submit). CAT live → send the rig's OWN mode literal
 * (e.g. "USB", "DATA-U") via set_mode, capability-gated. Optimistic: reflect the
 * literal + its friendly form at once (no snap-back on the MD0 push lag), rolled
 * back if the command is rejected. `value` is a friendly mode off-CAT and a rig
 * literal live — the caller's dropdown supplies the matching vocabulary.
 */
export async function setMode(value: string): Promise<RigWriteResult> {
    if (rig.cat !== 'connected') {
        rig.mode = value;
        return { ok: true, message: '' };
    }
    if (!hasOp('set_mode')) return { ok: false, message: 'This rig cannot set the mode.' };
    const prevLiteral = rig.modeLiteral;
    const prevFriendly = rig.mode;
    rig.modeLiteral = value;
    rig.mode = friendlyMode(value);
    const r = await driveRig('set_mode', value);
    if (!r.ok) {
        rig.modeLiteral = prevLiteral;
        rig.mode = prevFriendly;
    }
    return r;
}

// Representative default frequency per band (CAT-off), so picking a band can't
// leave the freq parked on the old band. A general-portion centre only — the
// operator fine-tunes; mode/region-aware defaults are the band-plan feature
// (backlog). Lives here (not the panel) so the band-selector grid AND the
// keyboard digit-jump apply it identically. Values in Hz.
const BAND_DEFAULT_HZ: Record<string, number> = {
    '160m': 1_900_000,
    '80m': 3_700_000,
    '60m': 5_357_000,
    '40m': 7_100_000,
    '30m': 10_125_000,
    '20m': 14_200_000,
    '17m': 18_130_000,
    '15m': 21_300_000,
    '12m': 24_950_000,
    '10m': 28_400_000,
    '6m': 50_150_000,
};

/**
 * Jump straight to a band by name ("20m") via set_band — the band-selector grid,
 * the Ctrl+Shift+digit shortcuts, and (later) the FT8 band buttons. CAT off →
 * set the band + jump the freq to its general-portion default (band + freq can't
 * disagree). CAT live → drive set_band: the rig restores that band's stack (its
 * own last freq + mode) and pushes them back, so the displayed band updates via
 * confirm-by-push (no optimistic write). Else fail soft.
 */
export async function selectBand(band: string): Promise<RigWriteResult> {
    if (rig.cat !== 'connected') {
        rig.band = band;
        const hz = BAND_DEFAULT_HZ[band];
        if (hz !== undefined) rig.freq = formatFrequency(hz);
        return { ok: true, message: '' };
    }
    if (!hasOp('set_band')) return { ok: false, message: 'This rig cannot jump bands.' };
    return driveRig('set_band', band);
}

// Physical digit-key codes in the operator's finger order (1..9 then 0), mapped
// by INDEX onto operatingBands() — so the Ctrl+Shift+digit band-jump follows the
// operator's configured band list (digit 1 = first configured band …), not a
// fixed 160m..6m table. A digit past the end of the list maps to nothing.
const DIGIT_CODES = [
    'Digit1',
    'Digit2',
    'Digit3',
    'Digit4',
    'Digit5',
    'Digit6',
    'Digit7',
    'Digit8',
    'Digit9',
    'Digit0',
];

/** The band a Ctrl+Shift+digit selects, from operatingBands() order — or
 *  undefined for a non-digit code or a digit past the configured list. */
export function bandForDigit(code: string): string | undefined {
    const idx = DIGIT_CODES.indexOf(code);
    if (idx < 0) return undefined;
    return operatingBands()[idx];
}

/**
 * Step the rig up/down one band (band_up / band_down — the rig walks its
 * band-stack registers). Live-only and capability-gated: band-stepping has no
 * meaning with no rig, so it's a silent no-op off-CAT. The new band shows via
 * the rig's freq push (confirm-by-push — no optimistic write).
 */
export function bandUp(): Promise<RigWriteResult> {
    return stepBand('band_up');
}

export function bandDown(): Promise<RigWriteResult> {
    return stepBand('band_down');
}

async function stepBand(op: 'band_up' | 'band_down'): Promise<RigWriteResult> {
    if (rig.cat !== 'connected') return { ok: true, message: '' }; // nothing to step off-CAT
    if (!hasOp(op)) return { ok: false, message: 'This rig cannot step bands.' };
    return driveRig(op);
}

// Frequency-step sizes (Hz) for the tuning shortcuts. Three tiers on the arrow
// cluster: fine (±10 Hz, →/←), coarse (±100 Hz, ↑/↓), and a ±5 kHz jump
// (Ctrl+Shift+Alt+↑/↓) for hopping across a band quickly.
const FREQ_STEP_COARSE_HZ = 100;
const FREQ_STEP_FINE_HZ = 10;
const FREQ_STEP_JUMP_HZ = 5_000;
const MAX_FREQ_HZ = 999_999_999; // the rigdef set_freq field (FA/FB, pad 9) ceiling

// Optimistic-target window for live key-repeat tuning. Each live step computes
// an absolute set_freq from the PREVIOUS target, not the displayed freq, because
// the confirming FA/FB push lags key-repeat — reading the pushed VFO every press
// would compute several steps off one stale value and stutter. After a pause (no
// step within the window) or a VFO switch, it re-syncs to the pushed freq, so a
// physical-knob turn between bursts is picked up.
const FREQ_REPEAT_WINDOW_MS = 350;
const pendingFreqHz: { A: number | null; B: number | null } = { A: null, B: null };
let lastFreqNudgeAt = 0;
let lastFreqVfo: 'A' | 'B' | null = null;

function clampFreq(hz: number): number {
    if (hz < 0) return 0;
    if (hz > MAX_FREQ_HZ) return MAX_FREQ_HZ;
    return hz;
}

/**
 * Nudge the selected VFO's frequency by deltaHz — the Ctrl+Shift arrow tuning.
 * CAT off → adjust the single manual freq field (parse → add → reformat). CAT
 * live → drive the rig: set_freq (FA) for VFO-A, set_freq_b (FB) for VFO-B, each
 * capability-gated (silent no-op if the rig can't tune that VFO). Uses an
 * optimistic per-VFO target so fast key-repeat tracks cleanly despite the
 * confirm-by-push lag. Off-CAT / not-exposed / no-known-freq are silent
 * (ok:true, no toast) — only a genuine command rejection surfaces.
 */
export async function nudgeFreq(deltaHz: number): Promise<RigWriteResult> {
    const vfo = rig.selectedVfo;

    if (rig.cat !== 'connected') {
        const cur = parseFrequency(rig.freq);
        if (cur === null) return { ok: true, message: '' }; // nothing to nudge
        rig.freq = formatFrequency(clampFreq(cur + deltaHz));
        return { ok: true, message: '' };
    }

    const op = vfo === 'A' ? 'set_freq' : 'set_freq_b';
    if (!hasOp(op)) return { ok: true, message: '' }; // rig can't tune this VFO — silent

    const now = Date.now();
    const prev = pendingFreqHz[vfo];
    const inBurst =
        prev !== null && lastFreqVfo === vfo && now - lastFreqNudgeAt <= FREQ_REPEAT_WINDOW_MS;
    const base = inBurst ? prev : vfo === 'A' ? rig.vfoA : rig.vfoB;
    if (base === null) return { ok: true, message: '' }; // no known freq yet

    const target = clampFreq(base + deltaHz);
    pendingFreqHz[vfo] = target;
    lastFreqNudgeAt = now;
    lastFreqVfo = vfo;
    return driveRig(op, String(target));
}

/** Coarse (±100 Hz) nudge — Ctrl+Shift+ArrowUp/Down. dir is +1/-1. */
export function nudgeFreqCoarse(dir: 1 | -1): Promise<RigWriteResult> {
    return nudgeFreq(dir * FREQ_STEP_COARSE_HZ);
}

/** Fine (±10 Hz) nudge — Ctrl+Shift+ArrowRight/Left. dir is +1/-1. */
export function nudgeFreqFine(dir: 1 | -1): Promise<RigWriteResult> {
    return nudgeFreq(dir * FREQ_STEP_FINE_HZ);
}

/** Jump (±5 kHz) band hop — Ctrl+Shift+Alt+ArrowUp/Down. dir is +1/-1. */
export function nudgeFreqJump(dir: 1 | -1): Promise<RigWriteResult> {
    return nudgeFreq(dir * FREQ_STEP_JUMP_HZ);
}

/** Test seam — clear the optimistic freq-step state between cases so a prior
 *  test's pending target can't leak into the next within the repeat window. */
export function resetFreqStep(): void {
    pendingFreqHz.A = null;
    pendingFreqHz.B = null;
    lastFreqNudgeAt = 0;
    lastFreqVfo = null;
}

// Persist the operating context on every change so the next session's
// confirm is one click over familiar values, not data entry.
$effect.root(() => {
    $effect(() => {
        localStorage.setItem(
            RIG_CTX_KEY,
            JSON.stringify({ band: rig.band, mode: rig.mode, freq: rig.freq })
        );
    });
});

/**
 * The CAT / rig gate (ADR 0044, full confirm-once-per-band flow):
 *
 *   'live'        — rig-pushed values, correct automatically. Logs.
 *   'manual'      — CAT off AND the operator confirmed THIS band. Logs.
 *   'unconfirmed' — CAT off, this band not confirmed this session: the
 *                   pre-filled context might be last week's. Blocks until
 *                   Set/Confirm; auto-lifts if CAT comes online.
 *   'lost'        — the rig WAS live and went away: the context may be
 *                   stale. Blocks; Confirm takes manual ownership.
 */
export type RigGate = 'live' | 'manual' | 'unconfirmed' | 'lost';

export function rigGate(): RigGate {
    if (rig.cat === 'connected') return 'live';
    if (rig.cat === 'lost') return 'lost';
    return rig.confirmedBand === rig.band ? 'manual' : 'unconfirmed';
}

export function rigReady(): boolean {
    const g = rigGate();
    return g === 'live' || g === 'manual';
}

/**
 * Set/Confirm: the operator asserts the displayed band/mode/freq are right.
 * On 'lost' this also takes manual ownership (cat → 'off', keeping the last
 * rig values — continuity beats defaults); a returning rig auto-lifts back
 * to 'live' either way. Confirmation is per-band: changing band re-arms.
 */
export function confirmRig(): void {
    if (rig.cat === 'connected') return; // nothing to assert — the rig speaks for itself
    if (rig.cat === 'lost') {
        cancelPendingLost();
        rig.cat = 'off';
    }
    rig.confirmedBand = rig.band;
}

/** ADIF (MODE, SUBMODE) pair — the value shape of the bridge mode_mappings table. */
export interface AdifModePair {
    mode: string;
    submode?: string;
}

// Merged rigdef+override table from /v1/config (bridge.mode_mappings), keyed
// by rig mode literal. Injected at boot; empty until config loads (unmapped
// literals pass through raw — odd beats invisible, same as shipping).
let modeMappings: Record<string, AdifModePair> = {};

export function setModeMappings(m: Record<string, AdifModePair>): void {
    modeMappings = m;
}

// Rig literal → the operator-friendly single string the rest of the surface
// uses (subMode || mode of the mapped ADIF pair — e.g. USB→"USB", DATA-U→
// "FT8"). resolveModeAndSubmode round-trips it to the (MODE, SUBMODE) pair at
// submit, so the CAT-live and manual paths converge on one representation.
function friendlyMode(literal: string): string {
    const mapped = modeMappings[literal];
    return mapped ? mapped.submode || mapped.mode : literal;
}

/*
    Disconnect flash suppression (shipping bridge.svelte.ts rule, ADR 0009):
    the FTdx10 family fires a false-positive rig-disconnected whenever the rig
    sits idle past liveness_ms (no AUTO pushes), and the daemon's read-probe
    recovers it within milliseconds. Flipping to 'lost' immediately would
    block logging + flicker the panel every blip, so the flip is deferred:
    only a disconnect with no rig-state inside the window becomes 'lost'.
    800 ms = probe round-trip + SSE delivery upper bound with headroom.
*/
export const FLASH_SUPPRESS_MS = 800;
let pendingLostTimer: ReturnType<typeof setTimeout> | null = null;

function cancelPendingLost(): void {
    if (pendingLostTimer !== null) {
        clearTimeout(pendingLostTimer);
        pendingLostTimer = null;
    }
}

/** CAT-link state transitions, fed by the SSE transport injected in main.ts. */
export const catLink = {
    /** Transport open is not rig-alive — 'connected' waits for a rig-state. */
    onOpen(): void {},

    /** Stream down (daemon gone / reconnecting). No suppression window here:
     *  with no stream there is nothing to recover within it. Only a link that
     *  was live goes 'lost' — a never-connected stream stays manual. */
    onTransportError(): void {
        cancelPendingLost();
        if (rig.cat === 'connected') rig.cat = 'lost';
    },

    onRigState(p: RigStatePayload): void {
        // A rig-state event carries only what changed; the merge combines it
        // with the last-known VFOs + selection held in the state itself.
        if (p.rigIdentity !== undefined) rig.identity = p.rigIdentity;
        if (p.vfoA !== undefined) rig.vfoA = p.vfoA;
        if (p.vfoB !== undefined) rig.vfoB = p.vfoB;
        if (p.selectedVfo === 'A' || p.selectedVfo === 'B') rig.selectedVfo = p.selectedVfo;

        const hz = rig.selectedVfo === 'A' ? rig.vfoA : rig.vfoB;
        if (hz !== null) {
            // The mirror keeps Go-manual continuity free — the last rig freq
            // is already sitting in the editable field, in parseable form.
            rig.freq = formatFrequency(hz);
            const band = frequencyToBand(hz);
            if (band !== '') rig.band = band;
        }
        if (p.mode !== undefined) {
            rig.modeLiteral = p.mode; // raw literal drives the live Option-A dropdown
            rig.mode = friendlyMode(p.mode);
        }

        rig.cat = 'connected';
        rig.linkError = ''; // the rig is demonstrably working
        cancelPendingLost(); // a blip that recovered — no flip, no flicker
    },

    onRigDisconnected(_p: BridgeCodePayload): void {
        // Never-connected (the daemon replays rig-disconnected to late
        // subscribers) or already lost: nothing to schedule.
        if (rig.cat !== 'connected') return;
        cancelPendingLost(); // a newer disconnect supersedes a pending one
        pendingLostTimer = setTimeout(() => {
            pendingLostTimer = null;
            rig.cat = 'lost';
        }, FLASH_SUPPRESS_MS);
    },

    /** Operator-actionable bridge fault (port permission, identity mismatch…).
     *  Shown raw (code + details) in the Rig panel — no i18n catalogue in this
     *  SPA yet, and odd beats invisible. */
    onBridgeError(p: BridgeCodePayload): void {
        const details = p.details ? ` (${Object.values(p.details).join(', ')})` : '';
        rig.linkError = `${p.code}${details}`;
    },

    /** Tune-carrier state pushed by the daemon (ADR 0027). Authoritative — the
     *  Tune button mirrors it, so a hard auto-off / release-on-disconnect the
     *  operator didn't click still clears the button. Replayed to late
     *  subscribers, so a tab opened mid-tune sees the carrier is up. */
    onTuneState(p: TuneStatePayload): void {
        rig.tuneActive = p.active;
    },
};

/** Test seam — restore the module singleton between cases. */
export function resetCatLink(): void {
    cancelPendingLost();
    modeMappings = {};
    tuneSender = null;
    commandSender = null;
    rigCaps.ops = [];
    rigCaps.tune = false;
    rigCaps.rigModes = [];
    bandPrefs.list = [];
    resetFreqStep();
    rig.cat = 'off';
    rig.confirmedBand = null;
    rig.vfoA = null;
    rig.vfoB = null;
    rig.selectedVfo = 'A';
    rig.modeLiteral = '';
    rig.identity = '';
    rig.linkError = '';
    rig.tuneActive = false;
}
