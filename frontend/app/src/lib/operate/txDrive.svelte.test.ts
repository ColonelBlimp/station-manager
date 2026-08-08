// TX-drive (ALC) readout state — ADR 0064's SPA face. Acceptance criterion 1,
// as amended by the fold: while transmitting FT8 the operator sees a live ALC
// reading and can tell apart DRIVE RIGHT (ALC in the healthy green band) from
// DRIVE HIGH (amber — reduce the audio level) from NO ALC DATA (poll answers
// lost) — three distinct states, because conflating "healthy" with "no data"
// is exactly the no-RF-vs-dead-instrument confusion the meter arc exists to
// prevent.
//
// Colour grammar operator-RATIFIED in two steps:
// 2026-08-07 (green): healthy drive measured ALC 15–18 every slot with PO
// flat, and NEVER zero while keyed — so the original zero-only green could
// not show during a correct transmission, and amber ("do something") nagged
// toward reducing audio that was already right. Green runs to the amber
// floor (ft8.meter.alc_amber, ratified default 30 — clears every healthy
// datum: FT8 15–18, low-power 7–12, voice 26).
// 2026-08-08 (RED FOLDED INTO AMBER): the §4 deliberate-overdrive run
// measured the RM ALC answer SATURATING at ~30 of 255 while the front-panel
// needle sat +20 dB over the zone and in-band PO collapsed 121→35
// (internal/bridge/meters.go carries the measurement) — so ~30 means AT
// LEAST zone-edge drive, no ALC-only threshold above it can ever fire, and
// the provisional red line (50) was unreachable however hot the chain ran.
// Amber is therefore the TERMINAL state and its message carries the action
// ("reduce the audio level"). The nearest confusable regression: a display
// that still maps high values to a red band the instrument cannot reach —
// T2's far-over fixture (200) exists to fail that implementation.
//
// The threshold comes from config, served resolved; the staleness window
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

    it('T2: green covers the healthy band; amber starts at the floor and is TERMINAL', () => {
        setTxDriveConfig({ amber: 30, intervalMs: 250 });

        onRigMeters({ meter: 'ALC', value: 0 }, 1000);
        expect(txDriveStatus(1100)).toBe('good');

        // THE ratifying flip (2026-08-07): 18 is the measured healthy
        // maximum, and under the original zero-only green it rendered amber
        // on every correct transmission. It must be green.
        onRigMeters({ meter: 'ALC', value: 18 }, 1150);
        expect(txDriveStatus(1160)).toBe('good');
        expect(txDriveState.alc?.value).toBe(18);

        // Boundary pair: the amber floor itself is amber, one below is green.
        onRigMeters({ meter: 'ALC', value: 29 }, 1200);
        expect(txDriveStatus(1300)).toBe('good');
        onRigMeters({ meter: 'ALC', value: 30 }, 1350);
        expect(txDriveStatus(1360)).toBe('warn');

        // The fold (2026-08-08): 50 was the old red line and 200 is far over
        // it — both are AMBER now. These two fixtures differentiate against
        // an implementation still carrying a red band.
        onRigMeters({ meter: 'ALC', value: 50 }, 1400);
        expect(txDriveStatus(1500)).toBe('warn');
        onRigMeters({ meter: 'ALC', value: 200 }, 1550);
        expect(txDriveStatus(1600)).toBe('warn');
    });

    it('T3: a stale reading is NO DATA, distinct from a fresh healthy zero', () => {
        setTxDriveConfig({ amber: 30, intervalMs: 250 });
        onRigMeters({ meter: 'ALC', value: 0 }, 1000);
        expect(txDriveStatus(1100)).toBe('good');
        // 6 × 250 ms = 1.5 s staleness window: at +2 s the answers have stopped.
        expect(txDriveStatus(3100)).toBe('stale');
    });

    it('T4: the amber floor is the CONFIGURED one, not the baked-in default', () => {
        setTxDriveConfig({ amber: 10, intervalMs: 250 });
        // 15 is green under the ratified default floor (30) — amber here
        // proves the configured floor is in force, not the default.
        onRigMeters({ meter: 'ALC', value: 15 }, 1000);
        expect(txDriveStatus(1100)).toBe('warn');
    });

    it('T5: the staleness window scales with the served poll cadence', () => {
        setTxDriveConfig({ amber: 30, intervalMs: 1000 }); // slow poll: window = 6 s
        onRigMeters({ meter: 'ALC', value: 35 }, 1000);
        expect(txDriveStatus(4000)).toBe('warn');
        expect(txDriveStatus(7100)).toBe('stale');
    });
});
