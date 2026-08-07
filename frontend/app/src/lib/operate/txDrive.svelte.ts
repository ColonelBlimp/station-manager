/*
    TX-drive (ALC) readout state (ADR 0064) — fed by the rig-meters SSE event
    (one decoded RM4/RM5 poll answer, raw 0-255). Display policy lives HERE,
    per the daemon's payload contract ("the SPA owns thresholds/rendering"):

      hidden — no ALC answer has ever arrived this page-load (no METERPOLL
               rigdef, CAT off, or the FT8 view has not run a session yet);
      good   — fresh answer, value < ft8.meter.alc_amber: drive is right.
               The green band is the HEALTHY band, not "ALC at zero"
               (operator-ratified 2026-08-07): live FT8 TX measured healthy
               drive at ALC 15–18 with PO flat and never zero while keyed, so
               a zero-only green could not show during a correct transmission
               and amber nagged toward an action that would cost output;
      warn   — fresh answer, amber ≤ value < red: genuinely elevated,
               approaching the red line;
      red    — fresh answer, value ≥ ft8.meter.alc_red (config, served
               resolved; PROVISIONAL default until calibrated on hardware);
      stale  — answers stopped (> 6 × the served poll cadence): NO DATA,
               deliberately distinct from `good` — a healthy reading and a
               dead poll must never render the same (the
               no-RF-vs-dead-instrument rule from the meter arc).

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
// older daemon that serves none of them. alcAmber's default is the RATIFIED
// 30 (green ceiling); alcRed's 50 is still provisional.
let alcRed = 50;
let alcAmber = 30;
let pollIntervalMs = 250;

// staleFactor: how many missed poll cycles count as "answers stopped". Six
// cycles (1.5 s at the default cadence) rides out a TX→RX tail eating a
// couple of answers without flapping to stale mid-transmission. PROVISIONAL,
// same standing as the daemon's meterAnswerStaleAfter.
const staleFactor = 6;

export function setTxDriveConfig(cfg: { red: number; amber: number; intervalMs: number }): void {
    if (Number.isFinite(cfg.red) && cfg.red >= 1) alcRed = cfg.red;
    if (Number.isFinite(cfg.amber) && cfg.amber >= 1) alcAmber = cfg.amber;
    if (Number.isFinite(cfg.intervalMs) && cfg.intervalMs > 0) pollIntervalMs = cfg.intervalMs;
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
    if (a.value >= alcAmber) return 'warn';
    return 'good';
}

/** Test seam — restore module state between cases. */
export function resetTxDriveForTests(): void {
    txDriveState.alc = null;
    txDriveState.open = false;
    alcRed = 50;
    alcAmber = 30;
    pollIntervalMs = 250;
}
