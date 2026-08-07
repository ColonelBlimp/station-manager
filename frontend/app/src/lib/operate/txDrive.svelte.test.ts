// TX-drive (ALC) readout state — ADR 0064's SPA face. Acceptance criterion 1:
// while transmitting FT8 the operator sees a live ALC reading and can tell
// apart ALC DEFLECTING (drive hot) from ALC AT ZERO (drive right) from NO ALC
// DATA (poll answers lost) — three distinct states, because conflating "zero"
// with "no data" is exactly the no-RF-vs-dead-instrument confusion the meter
// arc exists to prevent.
//
// The red threshold comes from config (ft8.meter.alc_red, served resolved,
// PROVISIONAL default until the on-hardware calibration); the staleness
// window derives from the served poll cadence (6 × ft8_meter_poll_interval_ms)
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

    it('T2: zero, deflecting, and red are three distinct fresh states', () => {
        setTxDriveConfig(50, 250);

        onRigMeters({ meter: 'ALC', value: 0 }, 1000);
        expect(txDriveStatus(1100)).toBe('good');

        onRigMeters({ meter: 'ALC', value: 26 }, 1200);
        expect(txDriveStatus(1300)).toBe('warn');
        expect(txDriveState.alc?.value).toBe(26);

        onRigMeters({ meter: 'ALC', value: 62 }, 1400);
        expect(txDriveStatus(1500)).toBe('red');
    });

    it('T3: a stale reading is NO DATA, distinct from a fresh zero', () => {
        setTxDriveConfig(50, 250);
        onRigMeters({ meter: 'ALC', value: 0 }, 1000);
        expect(txDriveStatus(1100)).toBe('good');
        // 6 × 250 ms = 1.5 s staleness window: at +2 s the answers have stopped.
        expect(txDriveStatus(3100)).toBe('stale');
    });

    it('T4: the red threshold is the CONFIGURED one, not a baked-in default', () => {
        setTxDriveConfig(30, 250);
        onRigMeters({ meter: 'ALC', value: 35 }, 1000);
        expect(txDriveStatus(1100)).toBe('red');
    });

    it('T5: the staleness window scales with the served poll cadence', () => {
        setTxDriveConfig(50, 1000); // slow poll: window = 6 s
        onRigMeters({ meter: 'ALC', value: 10 }, 1000);
        expect(txDriveStatus(4000)).toBe('warn');
        expect(txDriveStatus(7100)).toBe('stale');
    });
});
