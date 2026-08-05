/*
    Operating-state memory across a Phone/CW ↔ FT8 switch.

    Tuning in either mode moves the rig (or, CAT-off, the displayed context).
    Without this, coming back from FT8 leaves you parked on the watering hole
    and coming back from Phone leaves FT8 on your last SSB frequency. On every
    switch we snapshot the OUTGOING mode's operating state and restore the
    INCOMING mode's last one.

    This module sits ABOVE rig.svelte and ft8.svelte and may import both.
    It cannot live in rig.svelte: ft8.svelte already imports that, so reading
    the FT8 TX state from there would close an import cycle.

    Snapshots are in-memory only. A reload starts with none, so the first
    switch after one re-tunes nothing — a page refresh must never surprise the
    operator by moving the rig to where they were an hour ago.

    Ported from the retired logging SPA (`rigControl.ts` snapshot/restore,
    driven by a LoggingCard $effect). Two things changed in the move:
    the switch now lives in the ROUTER, so browser Back/Forward reaches it
    too; and CAT-off the app shell has no VFO A/B — its manual context is a
    single band + mode + freq, so that is what a CAT-off snapshot holds.
*/

import type { OpMode } from '../router.svelte';
import { rig, hasOp, driveRig, clampFreq, seedFreqTarget } from './rig.svelte';
import { ft8State } from './ft8.svelte';
import { toasts } from '../ui/toasts.svelte';

export interface OperatingSnapshot {
    /** Per-VFO Hz as the rig reported them; null when it never has. */
    vfoA: number | null;
    vfoB: number | null;
    selectedVfo: 'A' | 'B';
    /** The rig's OWN mode literal (e.g. "DATA-U") — what set_mode takes. */
    liveMode: string;
    /** The CAT-off displayed context. No VFOs exist in manual mode. */
    band: string;
    mode: string;
    freq: string;
}

/** Config `restore_rig_on_mode_switch` (default ON), injected at boot. Gates
 *  the CAT-live re-tune ONLY — the CAT-off restore touches no rig, so there is
 *  nothing for an operator to opt out of there. */
let restoreOnSwitch = true;

export function setRestoreOnModeSwitch(on: boolean): void {
    restoreOnSwitch = on;
}

const snapshots: Record<OpMode, OperatingSnapshot | null> = { phone: null, ft8: null };

export function snapshotOperating(): OperatingSnapshot {
    return {
        vfoA: rig.vfoA,
        vfoB: rig.vfoB,
        selectedVfo: rig.selectedVfo,
        liveMode: rig.modeLiteral,
        band: rig.band,
        mode: rig.mode,
        freq: rig.freq,
    };
}

/** True while the rig may be keyed: FT8 TX armed or actually transmitting, a
 *  sequencer contact in flight, or the tune carrier up. */
function transmitting(): boolean {
    return ft8State.tx.armed || ft8State.tx.transmitting || ft8State.qso.active || rig.tuneActive;
}

/*
    A mode whose restore was REFUSED is one the rig was never returned to: the
    app shows that mode while the rig sits on the other one's frequencies. On
    the way back out, snapshotting it would overwrite its operating state with
    those foreign values — losing exactly what the refusal was protecting.
    Null once the rig and the displayed mode agree again.
*/
let unrestored: OpMode | null = null;

export async function onOperatingModeChange(from: OpMode, to: OpMode): Promise<void> {
    const foreign = from === unrestored;
    unrestored = null;
    // Taken FIRST, before anything below moves the rig — and taken even when
    // the restore is then refused, since a refused re-tune is not an abandoned
    // switch and the mode being left still needs something to return to.
    if (!foreign) snapshots[from] = snapshotOperating();

    const incoming = snapshots[to];
    if (incoming === null) return; // never operated this mode: nothing to return to

    // CAT off (or lost): rewrite what's displayed. No rig is touched, so the
    // opt-out knob has nothing to opt out of here.
    if (rig.cat !== 'connected') {
        rig.band = incoming.band;
        rig.mode = incoming.mode;
        rig.freq = incoming.freq;
        return;
    }

    if (!restoreOnSwitch) return;

    if (transmitting()) {
        unrestored = to;
        toasts.info('Transmitting — the rig was left where it is, not returned.');
        return;
    }

    // Only what FT8 (or Phone) actually changed, each capability-gated. The
    // per-physical-VFO ops are selection-independent, so no VFO swap is needed
    // — and none is sent deliberately: swap_vfo exchanges VFO CONTENTS, and the
    // selection survives an excursion anyway (both modes tune the selected VFO).
    const selected = rig.selectedVfo;
    if (incoming.vfoA !== null && incoming.vfoA !== rig.vfoA && hasOp('set_freq')) {
        const hz = clampFreq(incoming.vfoA);
        await driveRig('set_freq', String(hz));
        if (selected === 'A') seedFreqTarget('A', hz);
    }
    if (incoming.vfoB !== null && incoming.vfoB !== rig.vfoB && hasOp('set_freq_b')) {
        const hz = clampFreq(incoming.vfoB);
        await driveRig('set_freq_b', String(hz));
        if (selected === 'B') seedFreqTarget('B', hz);
    }
    if (incoming.liveMode !== '' && incoming.liveMode !== rig.modeLiteral && hasOp('set_mode')) {
        await driveRig('set_mode', incoming.liveMode);
    }
}

/** Test seam — drop both snapshots and re-arm the knob. */
export function resetModeRestore(): void {
    snapshots.phone = null;
    snapshots.ft8 = null;
    restoreOnSwitch = true;
    unrestored = null;
}
