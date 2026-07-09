// CAT-link state-machine tests — the transitions the SSE transport feeds
// (transport itself is covered in rig-sse.test.ts). Fake timers drive the
// flash-suppression window: the FTdx10's idle false-positive disconnects are
// exactly the path nothing exercises by hand until a rig sits idle on a live
// link, so these tests are its routine exercise.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { flushSync } from 'svelte';
import {
    rig,
    rigReady,
    rigGate,
    confirmRig,
    catLink,
    setModeMappings,
    resetCatLink,
    FLASH_SUPPRESS_MS,
    rigCaps,
    setRigCaps,
    hasOp,
    setTuneSender,
    toggleTune,
    setCommandSender,
    selectVfo,
    swapVfo,
    setMode,
    selectBand,
    bandUp,
    bandDown,
    setOperatingBands,
    operatingBands,
    DEFAULT_BANDS,
    nudgeFreqCoarse,
    nudgeFreqFine,
    bandForDigit,
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
        expect(rigGate()).toBe('unconfirmed'); // not 'lost' — a fault would be
        confirmRig();
        expect(rigReady()).toBe(true); // manual logging day after one confirm
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

describe('confirm-once-per-band gate (ADR 0044)', () => {
    it('CAT-off blocks until the band is confirmed; a band change re-arms', () => {
        expect(rigGate()).toBe('unconfirmed');
        expect(rigReady()).toBe(false);
        confirmRig();
        expect(rigGate()).toBe('manual');
        expect(rigReady()).toBe(true);
        rig.band = '40m'; // single-slot memory: ANY band change re-arms
        expect(rigGate()).toBe('unconfirmed');
        expect(rigReady()).toBe(false);
    });

    it('confirming after a loss takes manual ownership, keeping the last rig values', () => {
        catLink.onRigState({ vfoA: 7_074_000, mode: 'USB' });
        catLink.onTransportError();
        expect(rigGate()).toBe('lost');
        confirmRig();
        expect(rig.cat).toBe('off');
        expect(rigGate()).toBe('manual');
        expect(rig.freq).toBe('7.074.000'); // continuity beats defaults
        expect(rigReady()).toBe(true);
    });

    it('is a no-op while connected — the rig speaks for itself', () => {
        catLink.onRigState({ vfoA: 14_255_000 });
        confirmRig();
        expect(rig.confirmedBand).toBeNull();
        expect(rigGate()).toBe('live');
    });

    it('CAT coming online auto-lifts an unconfirmed gate', () => {
        expect(rigReady()).toBe(false);
        catLink.onRigState({ vfoA: 14_255_000 });
        expect(rigGate()).toBe('live');
        expect(rigReady()).toBe(true);
    });

    it('persists the operating context for the next-session prefill (never the confirmation)', () => {
        rig.band = '40m';
        rig.mode = 'CW';
        rig.freq = '7.030.000';
        flushSync(); // run the persistence effect
        const stored = JSON.parse(localStorage.getItem('sm.rig.context') ?? '{}') as unknown;
        expect(stored).toEqual({ band: '40m', mode: 'CW', freq: '7.030.000' });
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

describe('bridge capabilities (BridgeInfo)', () => {
    it('setRigCaps populates ops/tune/rigModes and hasOp reads membership', () => {
        setRigCaps({ ops: ['set_freq', 'swap_vfo'], tune: true, rigModes: ['LSB', 'USB'] });
        expect(rigCaps.tune).toBe(true);
        expect(rigCaps.rigModes).toEqual(['LSB', 'USB']);
        expect(hasOp('swap_vfo')).toBe(true);
        expect(hasOp('set_mode')).toBe(false);
    });

    it('resetCatLink clears caps back to closed', () => {
        setRigCaps({ ops: ['set_freq'], tune: true, rigModes: ['USB'] });
        resetCatLink();
        expect(rigCaps).toEqual({ ops: [], tune: false, rigModes: [] });
        expect(hasOp('set_freq')).toBe(false);
    });
});

describe('tune carrier (ADR 0027)', () => {
    it('onTuneState mirrors the daemon-pushed state (confirm-by-push)', () => {
        expect(rig.tuneActive).toBe(false);
        catLink.onTuneState({ active: true });
        expect(rig.tuneActive).toBe(true);
        catLink.onTuneState({ active: false }); // e.g. hard auto-off the operator didn't click
        expect(rig.tuneActive).toBe(false);
    });

    it('toggleTune sends the OPPOSITE of the pushed state, never an optimistic flip', async () => {
        const sent: boolean[] = [];
        setTuneSender((active) => {
            sent.push(active);
            return Promise.resolve({ ok: true, message: '' });
        });

        // Carrier down → toggle asks to key it; state does NOT flip until a push.
        const r1 = await toggleTune();
        expect(r1.ok).toBe(true);
        expect(sent).toEqual([true]);
        expect(rig.tuneActive).toBe(false);

        // The daemon confirms the carrier is up; now a toggle asks to drop it.
        catLink.onTuneState({ active: true });
        await toggleTune();
        expect(sent).toEqual([true, false]);
    });

    it('toggleTune fails soft when no sender is wired', async () => {
        // resetCatLink() in beforeEach clears the injected sender.
        const r = await toggleTune();
        expect(r.ok).toBe(false);
        expect(r.message).not.toBe('');
    });
});

describe('VFO swap / select (ADR 0026)', () => {
    // Put the rig live with a two-VFO snapshot + swap_vfo exposed.
    function live(): { sent: { op: string; value?: string | number }[] } {
        catLink.onRigState({ vfoA: 14_100_000, vfoB: 14_200_000, selectedVfo: 'A' });
        setRigCaps({ ops: ['swap_vfo'], tune: false, rigModes: [] });
        const sent: { op: string; value?: string | number }[] = [];
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve({ ok: true, message: '' });
        });
        return { sent };
    }

    it('CAT-off swapVfo toggles the local selection, sends nothing', async () => {
        const sent: unknown[] = [];
        setCommandSender((op) => {
            sent.push(op);
            return Promise.resolve({ ok: true, message: '' });
        });
        expect(rig.cat).toBe('off');
        rig.selectedVfo = 'A';
        await swapVfo();
        expect(rig.selectedVfo).toBe('B');
        expect(sent).toEqual([]);
    });

    it('live swap optimistically mirrors VFO-B ← VFO-A and drives swap_vfo', async () => {
        const { sent } = live();
        const p = swapVfo();
        // Optimistic mirror is synchronous, before the await resolves: a
        // single-RX rig that never pushes VFO-B still shows the swap at once.
        expect(rig.vfoB).toBe(14_100_000);
        await p;
        expect(sent).toEqual([{ op: 'swap_vfo', value: undefined }]);
    });

    it('rolls the optimistic VFO-B back when the command is rejected', async () => {
        catLink.onRigState({ vfoA: 14_100_000, vfoB: 14_200_000, selectedVfo: 'A' });
        setRigCaps({ ops: ['swap_vfo'], tune: false, rigModes: [] });
        setCommandSender(() => Promise.resolve({ ok: false, message: 'rig_not_connected' }));
        const r = await swapVfo();
        expect(r.ok).toBe(false);
        expect(rig.vfoB).toBe(14_200_000); // restored, not left showing the false swap
    });

    it('selectVfo of the already-selected VFO is a no-op', async () => {
        const { sent } = live(); // selectedVfo A
        const r = await selectVfo('A');
        expect(r.ok).toBe(true);
        expect(sent).toEqual([]);
    });

    it('selectVfo of the other VFO swaps', async () => {
        const { sent } = live(); // selectedVfo A
        await selectVfo('B');
        expect(sent).toEqual([{ op: 'swap_vfo', value: undefined }]);
    });

    it('live swap fails soft (no command) when the rig lacks swap_vfo', async () => {
        catLink.onRigState({ vfoA: 14_100_000, vfoB: 14_200_000, selectedVfo: 'A' });
        setRigCaps({ ops: [], tune: false, rigModes: [] });
        const sent: unknown[] = [];
        setCommandSender((op) => {
            sent.push(op);
            return Promise.resolve({ ok: true, message: '' });
        });
        const r = await swapVfo();
        expect(r.ok).toBe(false);
        expect(sent).toEqual([]);
    });
});

describe('band + mode control (ADR 0026)', () => {
    function recorder(): { sent: { op: string; value?: string | number }[] } {
        const sent: { op: string; value?: string | number }[] = [];
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve({ ok: true, message: '' });
        });
        return { sent };
    }

    it('CAT-off setMode writes the friendly mode locally, sends nothing', async () => {
        const { sent } = recorder();
        await setMode('CW');
        expect(rig.mode).toBe('CW');
        expect(sent).toEqual([]);
    });

    it('live setMode sends the rig literal and optimistically reflects literal + friendly', async () => {
        setModeMappings(FTDX10_MAPPINGS);
        catLink.onRigState({ mode: 'USB' });
        setRigCaps({ ops: ['set_mode'], tune: false, rigModes: ['USB', 'DATA-U'] });
        const { sent } = recorder();

        const r = await setMode('DATA-U');
        expect(r.ok).toBe(true);
        expect(sent).toEqual([{ op: 'set_mode', value: 'DATA-U' }]);
        expect(rig.modeLiteral).toBe('DATA-U');
        expect(rig.mode).toBe('FT8'); // friendlyMode(DATA-U) via the mappings
    });

    it('live setMode rolls back the optimistic mode on rejection', async () => {
        setModeMappings(FTDX10_MAPPINGS);
        catLink.onRigState({ mode: 'USB' });
        setRigCaps({ ops: ['set_mode'], tune: false, rigModes: ['USB', 'DATA-U'] });
        setCommandSender(() => Promise.resolve({ ok: false, message: 'rig_command_rejected' }));

        const r = await setMode('DATA-U');
        expect(r.ok).toBe(false);
        expect(rig.modeLiteral).toBe('USB'); // restored
        expect(rig.mode).toBe('USB');
    });

    it('selectBand sends set_band with the band NAME when live, sets band + default freq when off', async () => {
        // Off-CAT: local band + the band's general-portion default freq (so band
        // and freq can't disagree), no command.
        const off = recorder();
        await selectBand('40m');
        expect(rig.band).toBe('40m');
        expect(rig.freq).toBe('7.100.000');
        expect(off.sent).toEqual([]);

        // Live: drive the rig (freq comes from the rig's band stack, not us).
        catLink.onRigState({ vfoA: 14_100_000 });
        setRigCaps({ ops: ['set_band'], tune: false, rigModes: [] });
        const on = recorder();
        await selectBand('15m');
        expect(on.sent).toEqual([{ op: 'set_band', value: '15m' }]);
    });

    it('bandUp / bandDown drive band_up / band_down live and no-op off-CAT', async () => {
        // Off-CAT: silent no-op (ok, no command) so nothing toasts.
        const off = recorder();
        const r0 = await bandUp();
        expect(r0.ok).toBe(true);
        expect(off.sent).toEqual([]);

        catLink.onRigState({ vfoA: 14_100_000 });
        setRigCaps({ ops: ['band_up', 'band_down'], tune: false, rigModes: [] });
        const on = recorder();
        await bandUp();
        await bandDown();
        expect(on.sent).toEqual([
            { op: 'band_up', value: undefined },
            { op: 'band_down', value: undefined },
        ]);
    });

    it('live band/mode fail soft (no command) when the rig lacks the op', async () => {
        catLink.onRigState({ vfoA: 14_100_000 });
        setRigCaps({ ops: [], tune: false, rigModes: [] });
        const { sent } = recorder();
        expect((await selectBand('20m')).ok).toBe(false);
        expect((await bandUp()).ok).toBe(false);
        expect((await setMode('USB')).ok).toBe(false);
        expect(sent).toEqual([]);
    });
});

describe('operating bands (station.operating_bands)', () => {
    it('defaults to the full HF..6m set when unset', () => {
        // resetCatLink() in beforeEach clears any configured list.
        expect(operatingBands()).toEqual(DEFAULT_BANDS);
    });

    it('uses the configured list, preserving order', () => {
        setOperatingBands(['80m', '40m', '20m', '10m']);
        expect(operatingBands()).toEqual(['80m', '40m', '20m', '10m']);
    });

    it('drops blanks and dedupes (keeping first occurrence)', () => {
        setOperatingBands(['20m', '', '40m', '20m', '15m']);
        expect(operatingBands()).toEqual(['20m', '40m', '15m']);
    });

    it('an empty configured list falls back to the default', () => {
        setOperatingBands([]);
        expect(operatingBands()).toEqual(DEFAULT_BANDS);
    });
});

describe('band-jump digit mapping (Ctrl+Shift+digit)', () => {
    it('follows the configured operating_bands order (digit 1 = first band)', () => {
        setOperatingBands(['80m', '40m', '20m', '15m', '10m']);
        expect(bandForDigit('Digit1')).toBe('80m');
        expect(bandForDigit('Digit5')).toBe('10m');
        expect(bandForDigit('Digit6')).toBeUndefined(); // past the configured list
        expect(bandForDigit('Enter')).toBeUndefined(); // not a digit code
    });

    it('maps against the default set when unset (digit 0 = the 10th band)', () => {
        setOperatingBands([]);
        expect(bandForDigit('Digit1')).toBe('160m');
        expect(bandForDigit('Digit0')).toBe('10m'); // index 9 of DEFAULT_BANDS
    });
});

describe('frequency nudge (Ctrl+Shift arrows)', () => {
    function recorder(): { sent: { op: string; value?: string | number }[] } {
        const sent: { op: string; value?: string | number }[] = [];
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve({ ok: true, message: '' });
        });
        return { sent };
    }

    it('CAT-off nudge adjusts the single manual freq field, sends nothing', async () => {
        rig.freq = '14.255'; // 14.255 MHz
        const { sent } = recorder();
        await nudgeFreqCoarse(1); // +100 Hz
        expect(rig.freq).toBe('14.255.100');
        expect(sent).toEqual([]);
    });

    it('live nudge drives set_freq (VFO-A) / set_freq_b (VFO-B) by selection', async () => {
        catLink.onRigState({ vfoA: 14_100_000, vfoB: 7_100_000, selectedVfo: 'A' });
        setRigCaps({ ops: ['set_freq', 'set_freq_b'], tune: false, rigModes: [] });
        const { sent } = recorder();

        await nudgeFreqFine(1); // VFO-A +10
        expect(sent).toEqual([{ op: 'set_freq', value: '14100010' }]);

        catLink.onRigState({ selectedVfo: 'B' });
        await nudgeFreqFine(-1); // VFO-B -10
        expect(sent[1]).toEqual({ op: 'set_freq_b', value: '7099990' });
    });

    it('key-repeat computes from the previous target, not the (lagging) pushed freq', async () => {
        // Fake timers (beforeEach) freeze Date.now(), so successive nudges are one
        // burst — the second must base off the first target, not the stale vfoA.
        catLink.onRigState({ vfoA: 14_100_000, selectedVfo: 'A' });
        setRigCaps({ ops: ['set_freq'], tune: false, rigModes: [] });
        const { sent } = recorder();
        await nudgeFreqCoarse(1);
        await nudgeFreqCoarse(1);
        await nudgeFreqCoarse(1);
        expect(sent.map((s) => s.value)).toEqual(['14100100', '14100200', '14100300']);
    });

    it('live nudge is a silent no-op when the rig cannot tune that VFO', async () => {
        catLink.onRigState({ vfoA: 14_100_000, selectedVfo: 'A' });
        setRigCaps({ ops: [], tune: false, rigModes: [] }); // no set_freq
        const { sent } = recorder();
        const r = await nudgeFreqCoarse(1);
        expect(r.ok).toBe(true);
        expect(sent).toEqual([]);
    });
});
