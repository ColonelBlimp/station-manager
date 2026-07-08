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
import type { RigStatePayload, BridgeCodePayload } from '../api/rig-sse';

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
    freq: string;
    vfoA: number | null;
    vfoB: number | null;
    selectedVfo: 'A' | 'B';
    identity: string;
    cat: CatLink;
    confirmedBand: string | null;
    linkError: string;
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
    cat: 'off',
    // The band the operator last confirmed (Set/Confirm). null = nothing
    // confirmed this session; a band change re-arms the gate by comparison.
    confirmedBand: null,
    linkError: '',
});

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
        if (p.mode !== undefined) rig.mode = friendlyMode(p.mode);

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
};

/** Test seam — restore the module singleton between cases. */
export function resetCatLink(): void {
    cancelPendingLost();
    modeMappings = {};
    rig.cat = 'off';
    rig.confirmedBand = null;
    rig.vfoA = null;
    rig.vfoB = null;
    rig.selectedVfo = 'A';
    rig.identity = '';
    rig.linkError = '';
}
