// TX-drive (ALC) readout state — ADR 0064's SPA face. Acceptance criterion 1:
// while transmitting FT8 the operator sees a live ALC reading and can tell
// apart DRIVE RIGHT (ALC in the healthy green band) from ALC ELEVATED (amber
// — approaching the red line) from OVERDRIVE (red) from NO ALC DATA (poll
// answers lost) — four distinct states, because conflating "healthy" with "no
// data" is exactly the no-RF-vs-dead-instrument confusion the meter arc
// exists to prevent.
//
// Colour grammar operator-RATIFIED 2026-08-07 from the first live FT8 TX
// data: healthy drive measured ALC 15–18 every slot with PO flat, and NEVER
// zero while keyed — so the original zero-only green could not show during a
// correct transmission, and amber ("do something") nagged toward reducing
// audio that was already right. Green now runs to the amber floor
// (ft8.meter.alc_amber, ratified default 30 — clears every healthy datum:
// FT8 15–18, low-power 7–12, voice 26); amber = floor..red-1 (genuinely
// elevated); red ≥ ft8.meter.alc_red (still PROVISIONAL 50 until a
// deliberate-overdrive datum exists).
//
// Both thresholds come from config, served resolved; the staleness window
// derives from the served poll cadence (6 × ft8_meter_poll_interval_ms)
// rather than an invented constant, so an operator who slows the poll does
// not see phantom "no data".

import { describe, it, expect, beforeEach } from 'vitest';
import {
    txDriveState,
    onRigMeters,
    setTxDriveConfig,
    txDriveStatus,
    resetTxDriveForTests,
} from './txDrive.svelte';

beforeEach(() => {
    resetTxDriveForTests();
});

describe('TX-drive ALC state', () => {
    it('T1: hidden until the first ALC answer — a PO answer alone reveals nothing', () => {
        expect(txDriveStatus(1000)).toBe('hidden');
        onRigMeters({ meter: 'PO', value: 40 }, 1000);
        expect(txDriveStatus(1000)).toBe('hidden');
        onRigMeters({ meter: 'ALC', value: 0 }, 1000);
        expect(txDriveStatus(1000)).not.toBe('hidden');
    });

    it('T2: green covers the healthy band; amber and red start at their floors', () => {
        setTxDriveConfig({ red: 50, amber: 30, intervalMs: 250 });

        onRigMeters({ meter: 'ALC', value: 0 }, 1000);
        expect(txDriveStatus(1100)).toBe('good');

        // THE ratifying flip: 18 is the measured healthy maximum, and under
        // the original zero-only green it rendered amber on every correct
        // transmission. It must be green.
        onRigMeters({ meter: 'ALC', value: 18 }, 1150);
        expect(txDriveStatus(1160)).toBe('good');
        expect(txDriveState.alc?.value).toBe(18);

        // Boundary pair: the amber floor itself is amber, one below is green.
        onRigMeters({ meter: 'ALC', value: 29 }, 1200);
        expect(txDriveStatus(1300)).toBe('good');
        onRigMeters({ meter: 'ALC', value: 30 }, 1350);
        expect(txDriveStatus(1360)).toBe('warn');
        onRigMeters({ meter: 'ALC', value: 49 }, 1370);
        expect(txDriveStatus(1380)).toBe('warn');

        // Red is unchanged: at/above the red line.
        onRigMeters({ meter: 'ALC', value: 50 }, 1400);
        expect(txDriveStatus(1500)).toBe('red');
    });

    it('T3: a stale reading is NO DATA, distinct from a fresh healthy zero', () => {
        setTxDriveConfig({ red: 50, amber: 30, intervalMs: 250 });
        onRigMeters({ meter: 'ALC', value: 0 }, 1000);
        expect(txDriveStatus(1100)).toBe('good');
        // 6 × 250 ms = 1.5 s staleness window: at +2 s the answers have stopped.
        expect(txDriveStatus(3100)).toBe('stale');
    });

    it('T4: both thresholds are the CONFIGURED ones, not baked-in defaults', () => {
        setTxDriveConfig({ red: 40, amber: 10, intervalMs: 250 });
        // 15 is green under the ratified default floor (30) — amber here
        // proves the configured floor is in force, not the default.
        onRigMeters({ meter: 'ALC', value: 15 }, 1000);
        expect(txDriveStatus(1100)).toBe('warn');
        // 45 is amber under the default red line (50) — red here proves the
        // configured red line is in force.
        onRigMeters({ meter: 'ALC', value: 45 }, 1200);
        expect(txDriveStatus(1300)).toBe('red');
    });

    it('T5: the staleness window scales with the served poll cadence', () => {
        setTxDriveConfig({ red: 50, amber: 30, intervalMs: 1000 }); // slow poll: window = 6 s
        onRigMeters({ meter: 'ALC', value: 35 }, 1000);
        expect(txDriveStatus(4000)).toBe('warn');
        expect(txDriveStatus(7100)).toBe('stale');
    });
});
