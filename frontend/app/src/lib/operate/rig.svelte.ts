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

export const rig: {
    band: string;
    mode: string;
    freq: string;
    vfoA: number | null;
    vfoB: number | null;
    selectedVfo: 'A' | 'B';
    identity: string;
    cat: CatLink;
    linkError: string;
} = $state({
    band: '20m',
    mode: 'USB', // operator-friendly literal (sideband, not ADIF family) — resolved at submit
    // Rig name from the wire's rigIdentity (e.g. "FTdx10"); '' until a rig has
    // been seen. Kept across a loss — naming the rig that went away beats a
    // blank — and cleared only by the test reset.
    identity: '',
    // The SELECTED VFO's frequency in the SM dot-grouped display form
    // ("14.199.950"). Manual entry accepts this OR decimal MHz ("14.255") —
    // both parse unambiguously to Hz via validators/frequency parseFrequency,
    // which every consumer uses (never parseFloat: it reads the grouped form
    // as 14.199). Rig-pushed values are always written in the grouped form,
    // so a Go-manual field starts out reading like the rig it mirrors.
    freq: '14.255.000',
    // Per-VFO Hz + selection, straight from the merge of partial rig-state
    // payloads (null until the rig reports one). The CAT-locked panel shows
    // both; freq above stays the SELECTED VFO's derivation — the value that
    // merges into a logged QSO. Manual entry has no VFO concept.
    vfoA: null,
    vfoB: null,
    selectedVfo: 'A',
    cat: 'off',
    linkError: '',
});

// The CAT gate for logging. 'off' = no bridge (or operator went manual), the
// values are the operator's own entry — trusted. 'connected' = rig-pushed —
// trusted. 'lost' = the rig WAS live this session and went away: the displayed
// context may be stale, so logging blocks rather than record a QSO against a
// wrong band/mode. A bridge-enabled stream that never saw the rig stays 'off'
// — CAT configured but rig off is a manual-logging day, not a fault.
export function rigReady(): boolean {
    return rig.cat !== 'lost';
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

/** Operator takes manual ownership after a loss ('lost' → 'off'). The last
 *  rig-pushed values stay — continuity beats defaults — and a returning rig
 *  auto-lifts back to 'connected' (ADR 0044). */
export function goManual(): void {
    if (rig.cat !== 'lost') return;
    cancelPendingLost();
    rig.cat = 'off';
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
    rig.vfoA = null;
    rig.vfoB = null;
    rig.selectedVfo = 'A';
    rig.identity = '';
    rig.linkError = '';
}
