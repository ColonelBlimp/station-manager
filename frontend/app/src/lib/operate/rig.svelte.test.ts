// CAT-link state-machine tests — the transitions the SSE transport feeds
// (transport itself is covered in rig-sse.test.ts). Fake timers drive the
// flash-suppression window: the FTdx10's idle false-positive disconnects are
// exactly the path nothing exercises by hand until a rig sits idle on a live
// link, so these tests are its routine exercise.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    rig,
    rigReady,
    catLink,
    goManual,
    setModeMappings,
    resetCatLink,
    FLASH_SUPPRESS_MS,
} from './rig.svelte';

beforeEach(() => {
    vi.useFakeTimers();
    resetCatLink();
    rig.band = '20m';
    rig.mode = 'USB';
    rig.freq = '14.255';
});

afterEach(() => {
    vi.useRealTimers();
});

// The FTdx10 table's shape (rigdef defaults merged with overrides).
const FTDX10_MAPPINGS = {
    USB: { mode: 'SSB', submode: 'USB' },
    LSB: { mode: 'SSB', submode: 'LSB' },
    'CW-U': { mode: 'CW' },
    'DATA-U': { mode: 'FT8' },
};

describe('rig-state', () => {
    it('connects and derives freq/band/mode/identity from the payload', () => {
        setModeMappings(FTDX10_MAPPINGS);
        catLink.onRigState({
            rigIdentity: 'FTdx10',
            vfoA: 7_074_000,
            mode: 'DATA-U',
            selectedVfo: 'A',
        });
        expect(rig.cat).toBe('connected');
        expect(rig.freq).toBe('7.074.000');
        expect(rig.band).toBe('40m');
        expect(rig.mode).toBe('FT8');
        expect(rig.identity).toBe('FTdx10');
    });

    it('merges partial payloads — a mode-only push keeps the frequency', () => {
        setModeMappings(FTDX10_MAPPINGS);
        catLink.onRigState({ vfoA: 14_255_000, mode: 'USB' });
        catLink.onRigState({ mode: 'CW-U' });
        expect(rig.freq).toBe('14.255.000');
        expect(rig.mode).toBe('CW');
    });

    it('follows the selected VFO across pushes', () => {
        catLink.onRigState({ vfoA: 14_255_000, vfoB: 7_074_000, selectedVfo: 'B' });
        expect(rig.freq).toBe('7.074.000');
        expect(rig.band).toBe('40m');
        catLink.onRigState({ selectedVfo: 'A' });
        expect(rig.freq).toBe('14.255.000');
        expect(rig.band).toBe('20m');
    });

    it('exposes both VFOs + selection for the panel read-outs', () => {
        expect(rig.vfoA).toBeNull(); // '—' placeholder until the rig reports
        catLink.onRigState({ vfoA: 14_199_950, vfoB: 24_950_000, selectedVfo: 'A' });
        expect(rig.vfoA).toBe(14_199_950);
        expect(rig.vfoB).toBe(24_950_000);
        expect(rig.selectedVfo).toBe('A');
        catLink.onRigState({ vfoB: 24_951_000 }); // partial: B ticks, A holds
        expect(rig.vfoA).toBe(14_199_950);
        expect(rig.vfoB).toBe(24_951_000);
    });

    it('resolves the mapped pair to subMode||mode; an unmapped literal passes raw', () => {
        setModeMappings(FTDX10_MAPPINGS);
        catLink.onRigState({ mode: 'USB' });
        expect(rig.mode).toBe('USB'); // submode wins over the SSB family
        catLink.onRigState({ mode: 'WEIRD-X' });
        expect(rig.mode).toBe('WEIRD-X'); // odd beats invisible
    });

    it('mirrors the selected VFO into freq in the dot-grouped display form', () => {
        catLink.onRigState({ vfoA: 14_199_950 });
        expect(rig.freq).toBe('14.199.950'); // parseFrequency round-trips this
        catLink.onRigState({ vfoA: 7_000_000 });
        expect(rig.freq).toBe('7.000.000');
    });
});

describe('disconnect flash suppression', () => {
    function connect(): void {
        catLink.onRigState({ vfoA: 14_255_000, mode: 'USB' });
    }

    it('a never-connected stream stays off — replayed rig-disconnected is not a loss', () => {
        catLink.onRigDisconnected({ code: 'rig_no_data' });
        vi.advanceTimersByTime(FLASH_SUPPRESS_MS + 1);
        expect(rig.cat).toBe('off');
        expect(rigReady()).toBe(true); // manual logging day, not a fault
    });

    it('a blip recovered inside the window never drops the link', () => {
        connect();
        catLink.onRigDisconnected({ code: 'rig_no_data' });
        vi.advanceTimersByTime(FLASH_SUPPRESS_MS - 100);
        expect(rig.cat).toBe('connected'); // deferred, not flipped
        catLink.onRigState({ mode: 'USB' }); // daemon probe recovered the rig
        vi.advanceTimersByTime(FLASH_SUPPRESS_MS * 2);
        expect(rig.cat).toBe('connected'); // cancelled — no flip, no flicker
    });

    it('a genuine outage flips to lost after the window and blocks logging', () => {
        connect();
        catLink.onRigDisconnected({ code: 'rig_no_data' });
        vi.advanceTimersByTime(FLASH_SUPPRESS_MS + 1);
        expect(rig.cat).toBe('lost');
        expect(rigReady()).toBe(false);
    });

    it('a returning rig auto-lifts lost back to connected', () => {
        connect();
        catLink.onRigDisconnected({ code: 'rig_no_data' });
        vi.advanceTimersByTime(FLASH_SUPPRESS_MS + 1);
        expect(rig.cat).toBe('lost');
        catLink.onRigState({ mode: 'USB' });
        expect(rig.cat).toBe('connected');
    });
});

describe('transport error', () => {
    it('flips a live link to lost immediately — no window with no stream', () => {
        catLink.onRigState({ vfoA: 14_255_000 });
        catLink.onTransportError();
        expect(rig.cat).toBe('lost');
    });

    it('leaves a never-connected link off (daemon down at boot)', () => {
        catLink.onTransportError();
        expect(rig.cat).toBe('off');
    });

    it('cancels a pending suppressed flip (error supersedes the timer)', () => {
        catLink.onRigState({ vfoA: 14_255_000 });
        catLink.onRigDisconnected({ code: 'rig_no_data' });
        catLink.onTransportError();
        expect(rig.cat).toBe('lost');
        vi.advanceTimersByTime(FLASH_SUPPRESS_MS * 2); // timer must not re-fire
        expect(rig.cat).toBe('lost');
    });
});

describe('goManual', () => {
    it('takes manual ownership after a loss, keeping the last rig values', () => {
        catLink.onRigState({ vfoA: 7_074_000, mode: 'USB' });
        catLink.onTransportError();
        goManual();
        expect(rig.cat).toBe('off');
        expect(rig.freq).toBe('7.074.000'); // continuity beats defaults
        expect(rigReady()).toBe(true);
    });

    it('is a no-op unless lost — a live link cannot be dismissed', () => {
        catLink.onRigState({ vfoA: 14_255_000 });
        goManual();
        expect(rig.cat).toBe('connected');
    });
});

describe('bridge-error', () => {
    it('surfaces the code + details raw, and a working rig clears it', () => {
        catLink.onBridgeError({ code: 'port_permission', details: { port: '/dev/ttyUSB0' } });
        expect(rig.linkError).toBe('port_permission (/dev/ttyUSB0)');
        catLink.onRigState({ mode: 'USB' });
        expect(rig.linkError).toBe('');
    });

    it('renders a details-free payload as the bare code', () => {
        catLink.onBridgeError({ code: 'unknown_driver' });
        expect(rig.linkError).toBe('unknown_driver');
    });
});
