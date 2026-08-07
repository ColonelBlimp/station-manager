/*
    TX-drive (ALC) readout state (ADR 0064) — fed by the rig-meters SSE event
    (one decoded RM4/RM5 poll answer, raw 0-255). Display policy lives HERE,
    per the daemon's payload contract ("the SPA owns thresholds/rendering"):

      hidden — no ALC answer has ever arrived this page-load (no METERPOLL
               rigdef, CAT off, or the FT8 view has not run a session yet);
      good   — fresh answer, ALC 0: drive is right;
      warn   — fresh answer, 0 < value < red threshold: ALC deflecting;
      red    — fresh answer, value ≥ ft8.meter.alc_red (config, served
               resolved; PROVISIONAL default until calibrated on hardware);
      stale  — answers stopped (> 6 × the served poll cadence): NO DATA,
               deliberately distinct from `good` — a zero reading and a dead
               poll must never render the same (the no-RF-vs-dead-instrument
               rule from the meter arc).

    Time is a parameter (txDriveStatus(nowMs)) so the rules are clock-free in
    tests; the chip passes Date.now() on a short ticker.
*/

import type { RigMetersPayload } from '../api/rig-sse';

export type TxDriveStatus = 'hidden' | 'good' | 'warn' | 'red' | 'stale';

interface TxDriveState {
    alc: { value: number; at: number } | null;
    /** Chip ↔ card toggle (same grammar as the RX audio meter beside it). */
    open: boolean;
}

export const txDriveState: TxDriveState = $state({ alc: null, open: false });

export function setTxDriveOpen(on: boolean): void {
    txDriveState.open = on;
}

// Config-served knobs (setTxDriveConfig from main.ts): defaults cover an
// older daemon that serves neither.
let alcRed = 50;
let pollIntervalMs = 250;

// staleFactor: how many missed poll cycles count as "answers stopped". Six
// cycles (1.5 s at the default cadence) rides out a TX→RX tail eating a
// couple of answers without flapping to stale mid-transmission. PROVISIONAL,
// same standing as the daemon's meterAnswerStaleAfter.
const staleFactor = 6;

export function setTxDriveConfig(red: number, intervalMs: number): void {
    if (Number.isFinite(red) && red >= 1) alcRed = red;
    if (Number.isFinite(intervalMs) && intervalMs > 0) pollIntervalMs = intervalMs;
}

/** Route one rig-meters event. Only ALC drives the display; PO answers keep
 *  flowing to the daemon's accumulator and are deliberately ignored here. */
export function onRigMeters(p: RigMetersPayload, atMs: number): void {
    if (p.meter !== 'ALC' || typeof p.value !== 'number') return;
    txDriveState.alc = { value: p.value, at: atMs };
}

/** The configured red threshold — the card renders its marker at this value. */
export function txDriveRedThreshold(): number {
    return alcRed;
}

export function txDriveStatus(nowMs: number): TxDriveStatus {
    const a = txDriveState.alc;
    if (a === null) return 'hidden';
    if (nowMs - a.at > staleFactor * pollIntervalMs) return 'stale';
    if (a.value >= alcRed) return 'red';
    if (a.value > 0) return 'warn';
    return 'good';
}

/** Test seam — restore module state between cases. */
export function resetTxDriveForTests(): void {
    txDriveState.alc = null;
    txDriveState.open = false;
    alcRed = 50;
    pollIntervalMs = 250;
}
