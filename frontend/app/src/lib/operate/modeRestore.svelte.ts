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
import {
    rig,
    hasOp,
    driveRig,
    clampFreq,
    seedFreqTarget,
    rigReportVersions,
    type RigReportVersions,
    setMode as rigSetMode,
} from './rig.svelte';
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

/*
    What we last returned the rig to, with the per-field rig-report counters as
    of the moment we commanded it. A field whose counter is unchanged has not
    been reported since, so `snap` — what we commanded — describes it better
    than rig.*, which still reads the value being left behind. Dropped when
    another restore begins, and superseded field by field as reports arrive.
*/
let restored: { snap: OperatingSnapshot; at: RigReportVersions } | null = null;

/*
    Switches are SERIALISED. Each restore awaits the rig between commands, so
    two overlapping ones interleave their commands and finish in call order,
    not click order — the earlier restore resumes after the later one and leaves
    the rig in the mode the operator only clicked THROUGH. Chaining also means
    the outgoing snapshot is taken once the previous restore has actually been
    applied, rather than from a rig halfway through it.
*/
let queue: Promise<void> = Promise.resolve();

export function onOperatingModeChange(from: OpMode, to: OpMode): Promise<void> {
    const run = queue.then(() => applySwitch(from, to));
    // The chain must survive a rejected link, or one thrown error wedges every
    // later switch.
    queue = run.catch(() => undefined);
    return run;
}

async function applySwitch(from: OpMode, to: OpMode): Promise<void> {
    // Where the rig actually is, resolved ONCE and used for both jobs below —
    // what to remember about the mode being left, and what still needs
    // commanding for the one being entered. They are the same question, and
    // answering them differently is what let a stale read decide the rig was
    // already where it was about to be sent.
    const here = effective();

    const foreign = from === unrestored;
    unrestored = null;
    // Taken FIRST, before anything below moves the rig — and taken even when
    // the restore is then refused, since a refused re-tune is not an abandoned
    // switch and the mode being left still needs something to return to.
    if (!foreign) snapshots[from] = here;

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
    //
    // A REJECTION ABANDONS THE REST. The commands are not independent: they put
    // the rig at one operating point together. Carrying on past a refused
    // set_freq is how a data mode ends up asserted on a phone frequency, and it
    // would also seed a nudge target the rig never reached.
    restored = null;
    const selected = rig.selectedVfo;
    if (incoming.vfoA !== null && incoming.vfoA !== here.vfoA && hasOp('set_freq')) {
        const hz = clampFreq(incoming.vfoA);
        const r = await driveRig('set_freq', String(hz));
        if (!r.ok) return abandon(to, r.message);
        if (selected === 'A') seedFreqTarget('A', hz);
    }
    if (incoming.vfoB !== null && incoming.vfoB !== here.vfoB && hasOp('set_freq_b')) {
        const hz = clampFreq(incoming.vfoB);
        const r = await driveRig('set_freq_b', String(hz));
        if (!r.ok) return abandon(to, r.message);
        if (selected === 'B') seedFreqTarget('B', hz);
    }
    if (incoming.liveMode !== '' && incoming.liveMode !== here.liveMode && hasOp('set_mode')) {
        // rig.setMode, not a raw set_mode: it owns the optimistic write of BOTH
        // the literal and its friendly form plus the rollback on rejection.
        // Hand-copying that here would be a second copy to keep in step. The
        // capability check stays OUTSIDE it — setMode reports an unsupported op
        // as a failure, and a rig that simply cannot change mode is not an
        // error worth interrupting the operator over.
        const r = await rigSetMode(incoming.liveMode);
        if (!r.ok) return abandon(to, r.message);
    }
    // What we have just commanded now describes the rig better than its own
    // last report does, until it sends the next one.
    restored = { snap: incoming, at: rigReportVersions() };
}

/*
    A restore the rig refused leaves it on the OTHER mode's frequencies, which
    is the same position a TX refusal leaves it in — so it earns the same
    snapshot protection (see `unrestored`). Reported, because unlike the TX
    refusal this one is a fault: the operator asked for nothing and the rig said
    no to something they cannot see.
*/
function abandon(to: OpMode, message: string): void {
    unrestored = to;
    restored = null;
    toasts.error(`Could not return the rig: ${message}`);
}

/**
 * Where the rig is, as well as we can know. Normally its own last report — but
 * once we have commanded a value and the rig has not reported THAT value back,
 * its last report still describes what it was told to leave, so what we
 * commanded is the truer answer.
 *
 * Resolved field by field: the rig confirms frequency and mode in separate
 * frames, and a mode confirmation is no evidence about the dial. The display
 * fields always come from rig.* — they are used only on the CAT-off path, where
 * nothing is ever commanded and so nothing lags.
 */
function effective(): OperatingSnapshot {
    const now = snapshotOperating();
    if (restored === null) return now;
    const seen = rigReportVersions();
    return {
        ...now,
        vfoA: seen.vfoA === restored.at.vfoA ? restored.snap.vfoA : now.vfoA,
        vfoB: seen.vfoB === restored.at.vfoB ? restored.snap.vfoB : now.vfoB,
        liveMode: seen.mode === restored.at.mode ? restored.snap.liveMode : now.liveMode,
    };
}

/** Test seam — drop both snapshots and re-arm the knob. */
export function resetModeRestore(): void {
    snapshots.phone = null;
    snapshots.ft8 = null;
    restoreOnSwitch = true;
    unrestored = null;
    restored = null;
    queue = Promise.resolve();
}
