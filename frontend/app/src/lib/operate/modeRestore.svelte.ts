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

    Snapshots are in-memory only. A reload starts with none — a page refresh
    must never surprise the operator by restoring where they were an hour ago.
    What a first FT8 entry DOES get (A25, operator-directed 2026-08-06) is a
    SEED: the current band's configured FT8 dial plus the data-mode literal,
    the same operating point the FT8 band buttons establish. Derived from
    config, never from a stale snapshot, so the reload rationale stands. The
    phone direction stays a no-op — phone has no canonical home to establish —
    and an UNCONFIGURED FT8 (no watering hole for the band) stays the
    pre-seed no-op too, silently: it is a steady state, not a fault.

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
    setMode as rigSetMode,
    selectVfo as rigSelectVfo,
    ft8FrequencyFor,
    ft8ModeLiteral,
    type RigReportVersions,
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

/*
    Boot-into-FT8 phone fallback (A26, 2026-08-07 dogfood report). A session
    that BEGINS on the FT8 view — reload or deep link, routine straight after
    a deploy — never runs a phone→ft8 switch, so nothing snapshots a phone
    position and the first FT8→phone switch used to leave the rig parked on
    the FT8 dial in a data mode. The fix: the FIRST full rig report of such a
    session (dial + mode both known) is captured as the phone snapshot — where
    the rig physically was when the session began, which is what "reset" means
    to the operator leaving FT8. This amends A4's letter, not its rationale:
    no STALE pre-reload snapshot is ever restored; the reload rationale in the
    header above stands.

    The window is boot-only: any mode switch closes it (from then on the real
    entry snapshots own the slots), a successful capture closes it, and only
    reports arriving while the app sits in ft8 mode qualify — booting into
    phone captures nothing, because FT8's null slot belongs to the A25 seed
    (filing a phone position there would suppress the seed and "restore" FT8
    to a phone frequency). A partial report (dial without mode) is skipped:
    restoring the dial while leaving the data mode asserted would be half the
    reported bug, and the honest degradation is the old no-op.
*/
let bootWindowClosed = false;

/** Called on every rig-state report (main.ts) with the router's current mode. */
export function noteRigReport(mode: OpMode): void {
    if (bootWindowClosed || mode !== 'ft8') return;
    if (rig.vfoA === null || rig.modeLiteral === '') return; // wait for a full report
    if (snapshots.phone === null) snapshots.phone = snapshotOperating();
    bootWindowClosed = true;
}

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
    The seed's sibling of `unrestored`, protecting NULL-ness rather than a
    snapshot: after a seed that could not run (rig refusal, TX in flight), FT8
    is displayed while the rig sits on phone frequencies. Snapshotting that on
    the way out would hand every later entry a "restore" to the phone position
    — permanently, because a non-null snapshot also stops the seed retrying.
    But blanket-skipping the exit snapshot would DISCARD a state the operator
    then established by hand, so rig-report counters are recorded at the
    refusal: unchanged at exit means the rig genuinely never moved (skip the
    snapshot, retry next entry); changed means FT8 really was operated (keep
    it). The fact needed was already carried — rigReportVersions — so no timer
    or threshold is invented here.

    Watched fields are ONLY the ones still OUTSTANDING at the refusal (review
    c0df1c8a): a command that landed will confirm and bump its own counter,
    and that confirmation is the seed's doing, not evidence the operator
    established FT8 — counting it kept a {FT8 dial, phone mode} snapshot alive
    and the refused data mode was never retried.
*/
let seedRefusedAt: { fields: ('vfoA' | 'vfoB' | 'mode')[]; versions: RigReportVersions } | null =
    null;

function seedNeverRan(from: OpMode): boolean {
    if (from !== 'ft8' || seedRefusedAt === null) return false;
    const at = seedRefusedAt;
    const now = rigReportVersions();
    return at.fields.every((f) => now[f] === at.versions[f]);
}

/*
    What each field was last COMMANDED to, dated with that field's rig-report
    counter as of the moment the command went out. While the counter is
    unchanged the rig has said nothing about that field since, so the commanded
    value describes it better than rig.*, which still reads what it was told to
    leave. A report supersedes the hold; the next command replaces it.

    PER FIELD AND PER COMMAND, both. One baseline for the whole restore mis-dates
    every field commanded after the first await: a report arriving in that gap
    looks newer than the command that follows it, so the app records the reported
    value while the rig has been sent somewhere else entirely (clean-room review
    e0dad4c7). "Has the rig spoken since this value was sent" can only be
    answered at the moment it is sent.
*/
type Hold<T> = { value: T; seq: number } | null;

const held: { vfoA: Hold<number>; vfoB: Hold<number>; mode: Hold<string> } = {
    vfoA: null,
    vfoB: null,
    mode: null,
};

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
    // Any switch closes the boot-capture window: from here on the exit
    // snapshot below owns the slots (A26 — the capture only ever fills the
    // never-switched case).
    bootWindowClosed = true;

    // Where the rig actually is, resolved ONCE and used for both jobs below —
    // what to remember about the mode being left, and what still needs
    // commanding for the one being entered. They are the same question, and
    // answering them differently is what let a stale read decide the rig was
    // already where it was about to be sent.
    const here = effective();

    const foreign = from === unrestored || seedNeverRan(from);
    unrestored = null;
    seedRefusedAt = null;
    // Taken FIRST, before anything below moves the rig — and taken even when
    // the restore is then refused, since a refused re-tune is not an abandoned
    // switch and the mode being left still needs something to return to.
    if (!foreign) snapshots[from] = here;

    const incoming = snapshots[to];
    if (incoming === null) {
        // Never operated this mode: nothing to RESTORE — but FT8 has a
        // canonical home to ESTABLISH (A25). Phone does not; it stays a no-op.
        if (to === 'ft8') await seedFt8();
        return;
    }

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

    await applyRestore(to, incoming);
}

// Only what FT8 (or Phone) actually changed, each capability-gated. The
// per-physical-VFO ops (FA/FB) are selection-independent; the SELECTION
// itself is restored first when it drifted and the rig can move it
// (select_vfo — before that op existed the selection genuinely could not
// change during an excursion, which is why this step didn't: codex ec2fd42d
// P1). No swap_vfo is ever sent here: it exchanges VFO CONTENTS, which
// would corrupt the very values this restore sets.
//
// A REJECTION ABANDONS THE REST. The commands are not independent: they put
// the rig at one operating point together. Carrying on past a refused
// set_freq is how a data mode ends up asserted on a phone frequency, and it
// would also seed a nudge target the rig never reached.
// Each field is re-compared against the rig's CURRENT effective position
// immediately before its own command, not against applySwitch's opening read —
// the rig can report in the gaps between commands, and comparing a later field
// against a pre-restore reading decides whether to move the rig from stale
// information.
//
// A field is recorded as commanded ONLY when its command went out and
// succeeded. A gate that skips one (no capability, no reported value,
// nothing to change) leaves the rig where it was, and recording the wish
// instead of the outcome makes the app believe the rig reached somewhere it
// has never been — a belief that outlives the capability which suppressed
// the command, since capabilities are re-applied on any context reload.
async function applyRestore(to: OpMode, incoming: OperatingSnapshot): Promise<void> {
    // Selection FIRST — ordering is load-bearing, not cosmetic: set_mode (MD0)
    // acts on the OPERATING VFO, so restoring the mode before the selection
    // writes it onto the wrong VFO. Capability-gated on select_vfo itself
    // rather than routed through selectVfo's fallback: on a rig without the
    // op the fallback is a content SWAP, which would corrupt the values this
    // restore sets. A drifted selection that CANNOT be restored (front-panel
    // A/B on such a rig) abandons the whole restore — carrying on would put
    // the mode on the wrong VFO, the exact corruption the ordering prevents
    // (codex 8092fa81 P1: the first shape skipped the selection and carried
    // on). The abandon names the front panel as the fix; the unrestored
    // protection retries on the next switch, once the operator has pressed
    // A/B back.
    if (incoming.selectedVfo !== rig.selectedVfo) {
        if (!hasOp('select_vfo')) {
            return abandon(
                to,
                `this rig cannot select VFO-${incoming.selectedVfo} over CAT — press the rig's A/B, then switch modes again`
            );
        }
        const r = await rigSelectVfo(incoming.selectedVfo);
        if (!r.ok) return abandon(to, r.message);
    }
    const selected = rig.selectedVfo;
    if (incoming.vfoA !== null && hasOp('set_freq') && incoming.vfoA !== effective().vfoA) {
        const hz = clampFreq(incoming.vfoA);
        const seq = rigReportVersions().vfoA;
        const r = await driveRig('set_freq', String(hz));
        if (!r.ok) return abandon(to, r.message);
        held.vfoA = { value: hz, seq };
        if (selected === 'A') seedFreqTarget('A', hz);
    }
    if (incoming.vfoB !== null && hasOp('set_freq_b') && incoming.vfoB !== effective().vfoB) {
        const hz = clampFreq(incoming.vfoB);
        const seq = rigReportVersions().vfoB;
        const r = await driveRig('set_freq_b', String(hz));
        if (!r.ok) return abandon(to, r.message);
        held.vfoB = { value: hz, seq };
        if (selected === 'B') seedFreqTarget('B', hz);
    }
    if (
        incoming.liveMode !== '' &&
        hasOp('set_mode') &&
        incoming.liveMode !== effective().liveMode
    ) {
        // rig.setMode, not a raw set_mode: it owns the optimistic write of BOTH
        // the literal and its friendly form plus the rollback on rejection.
        // Hand-copying that here would be a second copy to keep in step. The
        // capability check stays OUTSIDE it — setMode reports an unsupported op
        // as a failure, and a rig that simply cannot change mode is not an
        // error worth interrupting the operator over.
        const seq = rigReportVersions().mode;
        const r = await rigSetMode(incoming.liveMode);
        if (!r.ok) return abandon(to, r.message);
        held.mode = { value: incoming.liveMode, seq };
    }
}

/*
    First-entry FT8 seed (A25): the band's configured dial, then the data mode
    — the ft8SelectBand order and rationale (a refused mode write must not
    cost the dial move; a data mode on a phone frequency is the bad outcome),
    but issued here with modeRestore's own per-command holds, because a seed
    inside a switch has the same wish-vs-outcome gap as a restore: switch back
    before the rig confirms and, without the hold, effective() still reads the
    phone frequency — the phone restore then agrees with a rig that is
    actually on its way to the watering hole, and the exit snapshot records
    the phone position as FT8's.
*/
async function seedFt8(): Promise<void> {
    if (rig.cat !== 'connected') return; // CAT-off FT8 cannot work at all — nothing to establish (operator, 2026-08-06)
    if (!restoreOnSwitch) return; // the knob's promise: no switch moves a live rig
    const hz = ft8FrequencyFor(rig.band);
    if (hz === undefined) return; // unconfigured: nothing to establish, and no nagging

    // FT8 operates on the SELECTED VFO — rig.band derives from it and
    // ft8SelectBand/setFreq route the dial move by it (review c0df1c8a:
    // seeding VFO A under a B selection tunes the wrong dial and then asserts
    // the data mode on the phone frequency still selected). Everything
    // downstream — command, capability gate, comparison, hold, retry watch —
    // is that VFO's.
    const selected = rig.selectedVfo;
    const vfoField = selected === 'B' ? 'vfoB' : ('vfoA' as const);
    const freqOp = selected === 'B' ? 'set_freq_b' : 'set_freq';
    // No dial op for the operating VFO means no dial to establish — and
    // without the dial, the mode assert alone would be exactly the
    // wrong-frequency data mode.
    if (!hasOp(freqOp)) return;
    if (transmitting()) {
        seedRefusedAt = { fields: [vfoField, 'mode'], versions: rigReportVersions() };
        toasts.info('Transmitting — the rig was left where it is, not tuned for FT8.');
        return;
    }

    const target = clampFreq(hz);
    if (target !== effective()[vfoField]) {
        const seq = rigReportVersions()[vfoField];
        const r = await driveRig(freqOp, String(target));
        if (!r.ok) {
            seedRefusedAt = { fields: [vfoField, 'mode'], versions: rigReportVersions() };
            toasts.error(`Could not tune for FT8: ${r.message}`);
            return;
        }
        held[vfoField] = { value: target, seq };
        seedFreqTarget(selected, target);
    }
    const literal = ft8ModeLiteral();
    if (literal !== '' && hasOp('set_mode') && literal !== effective().liveMode) {
        const seq = rigReportVersions().mode;
        const r = await rigSetMode(literal);
        if (!r.ok) {
            // The dial landed and its hold is kept (that command really
            // happened); ONLY the mode is outstanding, so only the mode
            // counter is watched — the landed dial command will confirm and
            // bump its own counter, and that confirmation is the seed's
            // doing, not operator evidence (review c0df1c8a). The retry
            // re-enters with the dial already effective and sends mode alone.
            seedRefusedAt = { fields: ['mode'], versions: rigReportVersions() };
            toasts.error(`Could not set the FT8 mode: ${r.message}`);
            return;
        }
        held.mode = { value: literal, seq };
    }
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
    // Holds for fields that DID land are kept: those commands really happened,
    // and forgetting them would put the app back to reading a rig position the
    // rig has already left.
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
    const seen = rigReportVersions();
    return {
        ...now,
        vfoA: held.vfoA !== null && seen.vfoA === held.vfoA.seq ? held.vfoA.value : now.vfoA,
        vfoB: held.vfoB !== null && seen.vfoB === held.vfoB.seq ? held.vfoB.value : now.vfoB,
        liveMode:
            held.mode !== null && seen.mode === held.mode.seq ? held.mode.value : now.liveMode,
    };
}

/** Test seam — drop both snapshots and re-arm the knob. */
export function resetModeRestore(): void {
    snapshots.phone = null;
    snapshots.ft8 = null;
    bootWindowClosed = false;
    restoreOnSwitch = true;
    unrestored = null;
    seedRefusedAt = null;
    held.vfoA = null;
    held.vfoB = null;
    held.mode = null;
    queue = Promise.resolve();
}
