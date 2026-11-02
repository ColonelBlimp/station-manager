// The live band-selection sequence (W-0012 slice 2; AC6, rulings D1–D6 and the
// review corrections of 2026-11-02). Not wired to any control yet — slice 3 does
// that once memory ownership exists.
//
// A selection resolves its destination (bandSelection.ts), then writes the
// frequency and the mode through the band-selection sender, whose requests carry
// `band_selection: true` so the daemon stops TX first (ruling (a)). A step counts
// only on the rig's OWN report of the value it set — a 202 means "written", never
// "the rig is there" — and only once the transmit state is stopped (below), both
// within BAND_CONFIRM_DEADLINE_MS of the step's HTTP 202. Immediately before the
// mode is dispatched the sequence rechecks the VFO, the frequency and the TX
// guards. Any other outcome stops it, and it is returned for display. Nothing
// here ever arms TX.
//
// The transmit state comes from the FT8 stream, independent of the rig reports,
// so the daemon's own stop can arrive after the frequency report (ruling (a) on
// that race). The selection therefore observes the transmit state from before
// its first write: keyed at the start, it waits — to the same absolute deadline
// as the frequency — for the complete stopped state; once stopped (or idle from
// the start), any newly observed TX invalidates the sequence.

import {
    resolveBandDestination,
    resolveStepDestination,
    type BandView,
    type Destination,
    type MemoryLookup,
} from './bandSelection';
import {
    rig,
    confirmedRig,
    onRigReport,
    literalForMode,
    ft8ModeLiteral,
    type RigSendOutcome,
} from './rig.svelte';
import { transmitting } from './modeRestore.svelte';
import type { RigStatePayload } from '../api/rig-sse';

/** Sends one op with `band_selection: true`. Injected; null when unwired. */
export type BandSelectionSender = (op: string, value: string | number) => Promise<RigSendOutcome>;

/** How long after a step's HTTP 202 the rig has to report its value — and the
 *  transmit state to show stopped (operator ruling 2026-11-02). Its own rule:
 *  the 2 s timeout grace in rig.svelte.ts is a different one. */
export const BAND_CONFIRM_DEADLINE_MS = 2000;

export type BandTarget = { band: string } | { step: 1 | -1 };

type FailKind = 'refused' | 'transport' | 'unknown' | 'invalidated';

export type BandSelectionResult =
    | { status: 'done'; band: string }
    | { status: 'alreadySatisfied'; band: string }
    | { status: 'failed'; kind: FailKind; message: string }
    | { status: 'superseded'; message: string };

let sender: BandSelectionSender | null = null;

export function setBandSelectionSender(fn: BandSelectionSender | null): void {
    sender = fn;
}

type Field = 'vfoA' | 'vfoB' | 'mode';

/** A write that was (or may have been) sent and whose value the rig has not
 *  reported yet. Kept across supersession and ambiguous failures: a cancelled
 *  selection cannot retract a write that may still land (D5/D6). */
type Issued = { field: Field; value: number | string };

const issued: Issued[] = [];
let latest = 0; // the id of the newest selection; older ids are superseded
let pendingBand: string | null = null; // the newest selection's destination band
let chain: Promise<void> = Promise.resolve(); // serialises the HTTP requests
const wakers: (() => void)[] = []; // re-evaluate every pending wait (plain: not reactive)
let unsubscribe: (() => void) | null = null;

function wakeAll(): void {
    for (const w of [...wakers]) w();
}

function reported(p: RigStatePayload, field: Field): number | string | undefined {
    return field === 'mode' ? p.mode : p[field];
}

function onReport(p: RigStatePayload): void {
    for (const w of [...issued]) {
        if (reported(p, w.field) === w.value) forget(w);
    }
    wakeAll();
}

function forget(w: Issued): void {
    const i = issued.indexOf(w);
    if (i !== -1) issued.splice(i, 1);
}

/** Test seam: drop all sequence state between cases. */
export function resetBandSequence(): void {
    latest++;
    wakeAll();
    wakers.length = 0;
    issued.length = 0;
    pendingBand = null;
    chain = Promise.resolve();
    unsubscribe?.();
    unsubscribe = null;
}

const TX_UNRESOLVED = 'Transmit state is unresolved — the band was not changed.';
const TX_CHANGED = 'Transmit state changed during the band selection — the mode was not set.';

function failed(kind: FailKind, message: string): BandSelectionResult {
    return { status: 'failed', kind, message };
}

function superseded(band: string, frequencyReached: boolean): BandSelectionResult {
    const base = `Band selection to ${band} replaced by a newer selection.`;
    return {
        status: 'superseded',
        message: frequencyReached ? `${base} Its frequency change had reached the rig.` : base,
    };
}

/*
    The transmit-state observer for one selection. Created before the first write
    so events during the POST are seen. `stopped` latches the first observation
    of the complete stopped state (FT8 disarmed, not transmitting, no contact,
    no tune carrier — modeRestore's transmitting()); `keyedAgain` latches TX
    observed after that. Idle at the start counts as already stopped.
*/
type TxWatch = { stopped: boolean; keyedAgain: boolean; dispose: () => void };

function observe(w: TxWatch, keyed: boolean): void {
    if (!keyed) w.stopped = true;
    else if (w.stopped) w.keyedAgain = true;
}

function watchTx(): TxWatch {
    const w: TxWatch = { stopped: !transmitting(), keyedAgain: false, dispose: () => {} };
    // The one recording mechanism, active for the whole selection — including
    // while a POST is pending and nothing is waiting (review 2026-11-02). Every
    // SSE message is its own task, so this runs before the next report is
    // handled; the guard reads what it recorded.
    w.dispose = $effect.root(() => {
        $effect(() => {
            observe(w, transmitting());
            void rig.cat;
            void rig.txAlarmActive;
            wakeAll();
        });
    });
    return w;
}

// The guards every wait and the pre-dispatch recheck share. null = fine so far.
function txGuard(tx: TxWatch): BandSelectionResult | null {
    if (rig.cat !== 'connected') {
        return failed('invalidated', 'The rig connection was lost during the band selection.');
    }
    if (rig.txAlarmActive) return failed('invalidated', TX_UNRESOLVED);
    if (tx.keyedAgain) return failed('invalidated', TX_CHANGED);
    return null;
}

type Ctx = { id: number; band: string; tx: TxWatch };

type StepOutcome = 'confirmed' | BandSelectionResult;

// The rig literal the destination's mode needs: null = leave the mode alone
// (FT with an empty configured literal); undefined = the rig has none.
function destinationLiteral(view: BandView, dest: Destination & { kind: 'tune' }) {
    if (view !== 'phone') {
        const lit = ft8ModeLiteral();
        return lit === '' ? null : lit;
    }
    return literalForMode(dest.mode);
}

/*
    One write. Serialised behind any request still in flight; `precheck` runs
    after that wait, immediately before dispatch; the write is registered as
    issued before it is sent. After the 202 it waits — to one absolute deadline —
    for the rig's report of its value AND the stopped transmit state.
*/
async function writeStep(
    ctx: Ctx,
    field: Field,
    op: string,
    value: number | string,
    frequencyReached: boolean,
    precheck: () => BandSelectionResult | null
): Promise<StepOutcome> {
    const send = sender;
    if (send === null) return failed('transport', 'Rig control is unavailable.');
    const prev = chain;
    let release!: () => void;
    chain = new Promise<void>((r) => (release = r));
    await prev;
    if (ctx.id !== latest) {
        release();
        return superseded(ctx.band, frequencyReached); // unsent work is abandoned
    }
    const blocked = precheck();
    if (blocked !== null) {
        release();
        return blocked;
    }
    const w: Issued = { field, value };
    issued.push(w);
    let out: RigSendOutcome;
    try {
        out = await send(op, value);
    } finally {
        release();
    }
    switch (out.kind) {
        case 'refused':
            forget(w); // a definite rejection: nothing was written
            return failed('refused', out.message);
        case 'transport':
            return failed('transport', out.message); // ambiguous: stays issued
        case 'timedOut':
            return failed(
                'unknown',
                `Couldn't confirm the band change to ${ctx.band} — check the rig.`
            );
        case 'accepted':
            return awaitStep(ctx, w, frequencyReached);
    }
}

function awaitStep(ctx: Ctx, w: Issued, frequencyReached: boolean): Promise<StepOutcome> {
    return new Promise<StepOutcome>((resolve) => {
        let done = false;
        const finish = (r: StepOutcome) => {
            if (done) return;
            done = true;
            clearTimeout(timer);
            const i = wakers.indexOf(evaluate);
            if (i !== -1) wakers.splice(i, 1);
            resolve(r);
        };
        const evaluate = () => {
            if (ctx.id !== latest) {
                // A frequency the rig already reported has reached it, even if
                // the stop had not yet been observed.
                const reached = frequencyReached || (w.field !== 'mode' && !issued.includes(w));
                return finish(superseded(ctx.band, reached));
            }
            const guard = txGuard(ctx.tx);
            if (guard !== null) return finish(guard);
            if (!issued.includes(w) && ctx.tx.stopped) finish('confirmed');
        };
        const timer = setTimeout(() => {
            if (issued.includes(w)) {
                finish(
                    failed(
                        'unknown',
                        `Couldn't confirm the band change to ${ctx.band} — check the rig.`
                    )
                );
            } else {
                finish(
                    failed(
                        'unknown',
                        w.field === 'mode'
                            ? 'Mode changed, but transmit-stop confirmation did not arrive.'
                            : 'Frequency changed, but transmit-stop confirmation did not arrive; mode was not set.'
                    )
                );
            }
        }, BAND_CONFIRM_DEADLINE_MS);
        wakers.push(evaluate);
        evaluate(); // the report and the stop may both have landed during the POST
    });
}

function resolveTarget(view: BandView, target: BandTarget, memory: MemoryLookup): Destination {
    if ('band' in target) return resolveBandDestination(view, target.band, memory);
    return resolveStepDestination(view, pendingBand ?? rig.band, target.step, memory);
}

/**
 * Select a band on a connected rig. Resolves to the outcome the caller shows:
 * done, already satisfied (nothing written, nothing stopped), failed, or
 * superseded by a newer selection.
 */
export async function selectBandLive(
    view: BandView,
    target: BandTarget,
    memory: MemoryLookup
): Promise<BandSelectionResult> {
    if (rig.cat !== 'connected') return failed('refused', 'The rig is not connected.');
    if (rig.txAlarmActive) return failed('refused', TX_UNRESOLVED);
    const dest = resolveTarget(view, target, memory);
    if (dest.kind === 'none') return failed('refused', dest.message);
    const literal = destinationLiteral(view, dest);
    if (literal === undefined) {
        return failed('refused', `This rig has no ${dest.mode} mode.`);
    }
    // Neither the default A nor an optimistic selection says which physical
    // VFO to write (ruling 2026-11-02).
    const vfo = confirmedRig().selectedVfo;
    if (vfo === null) {
        return failed('refused', 'The rig has not reported its selected VFO yet.');
    }

    if (unsubscribe === null) unsubscribe = onRigReport(onReport);
    const id = ++latest;
    wakeAll(); // the pending selection, if any, sees it is superseded
    pendingBand = dest.band;
    try {
        return await runSequence(id, dest, literal, vfo);
    } finally {
        if (id === latest) pendingBand = null;
    }
}

async function runSequence(
    id: number,
    dest: Destination & { kind: 'tune' },
    literal: string | null,
    vfo: 'A' | 'B'
): Promise<BandSelectionResult> {
    const field = vfo === 'A' ? 'vfoA' : 'vfoB';
    const c = confirmedRig();
    // "Already there" only from confirmed state, and never while an earlier
    // write could still land (ruling 2). The true no-op writes, stops and
    // waits for nothing.
    const unresolved = issued.length > 0;
    const needFreq = unresolved || c[field] !== dest.hz;
    const needMode = literal !== null && (unresolved || c.modeLiteral !== literal);
    if (!needFreq && !needMode) return { status: 'alreadySatisfied', band: dest.band };

    const ctx: Ctx = { id, band: dest.band, tx: watchTx() };
    try {
        if (needFreq) {
            const op = vfo === 'A' ? 'set_freq' : 'set_freq_b';
            const r = await writeStep(ctx, field, op, dest.hz, false, () => txGuard(ctx.tx));
            if (r !== 'confirmed') return r;
        }
        if (needMode && literal !== null) {
            // The recheck reads the latched TX state, not "keyed now": a mode-only
            // selection's first write is meant to go out while keyed, since the
            // daemon stops TX before writing it.
            const r = await writeStep(ctx, 'mode', 'set_mode', literal, needFreq, () =>
                recheck(ctx.tx, vfo, field, dest.hz)
            );
            if (r !== 'confirmed') return r;
        }
        return { status: 'done', band: dest.band };
    } finally {
        ctx.tx.dispose();
    }
}

// Immediately before the mode is dispatched: the rig must still be where this
// selection put it, and nothing may be transmitting.
function recheck(
    tx: TxWatch,
    vfo: 'A' | 'B',
    field: 'vfoA' | 'vfoB',
    hz: number
): BandSelectionResult | null {
    const guard = txGuard(tx);
    if (guard !== null) return guard;
    const c = confirmedRig();
    if (c.selectedVfo !== vfo) {
        return failed(
            'invalidated',
            'The VFO changed during the band selection — the mode was not set.'
        );
    }
    if (c[field] !== hz) {
        return failed(
            'invalidated',
            'The frequency changed during the band selection — the mode was not set.'
        );
    }
    return null;
}
