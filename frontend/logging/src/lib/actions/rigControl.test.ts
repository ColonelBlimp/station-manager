import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    selectVfo,
    swapVfo,
    bandUp,
    bandDown,
    selectBand,
    toggleTune,
    nudgeFreq,
    nudgeFreqJump,
    setMode,
    _resetFreqStepForTests,
} from './rigControl';
import { manualState } from '../states/manual.svelte';
import { configState } from '../states/config.svelte';
import { bridgeState } from '../states/bridge.svelte';
import { catState } from '../states/cat.svelte';
import { sendRigCommand } from '../api/rigCommand';
import { sendRigTune } from '../api/rigTune';

// The transport is mocked — these tests pin the CAT-on/off + capability
// branching, not the HTTP wire (that's rigCommand's own concern).
vi.mock('../api/rigCommand', () => ({
    sendRigCommand: vi.fn().mockResolvedValue({ kind: 'ok' }),
}));
vi.mock('../api/rigTune', () => ({
    sendRigTune: vi.fn().mockResolvedValue({ kind: 'ok' }),
}));

const mockSend = vi.mocked(sendRigCommand);
const mockTune = vi.mocked(sendRigTune);

function setCatLive(): void {
    configState.station.enabled = true;
    bridgeState.connected = true;
    bridgeState.rigResponding = true;
}

describe('rigControl', () => {
    beforeEach(() => {
        mockSend.mockClear();
        mockSend.mockResolvedValue({ kind: 'ok' });
        mockTune.mockClear();
        mockTune.mockResolvedValue({ kind: 'ok' });
        // CAT off by default.
        configState.station.enabled = false;
        bridgeState.connected = false;
        bridgeState.rigResponding = false;
        bridgeState.tuneActive = false;
        configState.bridge.ops = [];
        configState.bridge.tune = false;
        manualState.selectedVfo = 'A';
        catState.selectedVfo = 'A';
    });

    afterEach(() => {
        vi.clearAllMocks();
    });

    describe('selectVfo — CAT off', () => {
        it('writes manualState and does not touch the rig', () => {
            selectVfo('B');
            expect(manualState.selectedVfo).toBe('B');
            expect(mockSend).not.toHaveBeenCalled();
        });
    });

    describe('selectVfo — CAT live', () => {
        beforeEach(() => {
            setCatLive();
            catState.selectedVfo = 'A';
        });

        it('swaps the rig (swap_vfo) when selecting the OTHER VFO', () => {
            configState.bridge.ops = ['swap_vfo'];
            selectVfo('B'); // currently A → selecting B swaps
            expect(mockSend).toHaveBeenCalledWith('swap_vfo', undefined);
            // manualState is untouched — confirm-by-push owns the UI when live.
            expect(manualState.selectedVfo).toBe('A');
        });

        it('is a no-op when selecting the already-current VFO', () => {
            configState.bridge.ops = ['swap_vfo'];
            selectVfo('A'); // already on A
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('is a no-op when the rig does not expose swap_vfo', () => {
            configState.bridge.ops = [];
            selectVfo('B');
            expect(mockSend).not.toHaveBeenCalled();
            expect(manualState.selectedVfo).toBe('A');
        });
    });

    describe('swapVfo', () => {
        it('toggles to the other VFO via manualState when CAT is off', () => {
            manualState.selectedVfo = 'A';
            swapVfo();
            expect(manualState.selectedVfo).toBe('B');
        });

        it('drives the rig swap (swap_vfo) when CAT is live', () => {
            setCatLive();
            configState.bridge.ops = ['swap_vfo'];
            catState.selectedVfo = 'B';
            swapVfo();
            expect(mockSend).toHaveBeenCalledWith('swap_vfo', undefined);
        });

        it('optimistically mirrors VFO-A into VFO-B (single-RX rigs never push VFO-B)', () => {
            setCatLive();
            configState.bridge.ops = ['swap_vfo'];
            catState.vfoA = 14_074_000;
            catState.vfoB = 7_074_000;
            swapVfo();
            // After CI-V 07 B0 the rig's VFO-B holds the old VFO-A; reflect it now.
            // The daemon read-back fills the new vfoA shortly after (not in this unit).
            expect(catState.vfoB).toBe(14_074_000);
            expect(mockSend).toHaveBeenCalledWith('swap_vfo', undefined);
        });

        it('is a no-op when CAT is live but the rig does not expose swap_vfo', () => {
            setCatLive();
            configState.bridge.ops = [];
            swapVfo();
            expect(mockSend).not.toHaveBeenCalled();
        });
    });

    describe('band step', () => {
        it('no-ops when CAT is off (no rig to step)', () => {
            bandUp();
            bandDown();
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('sends band_up / band_down (valueless) when CAT is live + capable', () => {
            setCatLive();
            configState.bridge.ops = ['band_up', 'band_down'];
            bandUp();
            expect(mockSend).toHaveBeenCalledWith('band_up', undefined);
            bandDown();
            expect(mockSend).toHaveBeenCalledWith('band_down', undefined);
        });

        it('no-ops when CAT is live but the rig lacks the band-step op', () => {
            setCatLive();
            configState.bridge.ops = [];
            bandUp();
            expect(mockSend).not.toHaveBeenCalled();
        });
    });

    describe('selectBand', () => {
        it('no-ops when CAT is off', () => {
            selectBand('20m');
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('sends set_band when CAT is live + capable', () => {
            setCatLive();
            configState.bridge.ops = ['set_band'];
            selectBand('20m');
            expect(mockSend).toHaveBeenCalledWith('set_band', '20m');
        });

        it('no-ops when CAT is live but the rig lacks set_band', () => {
            setCatLive();
            configState.bridge.ops = [];
            selectBand('20m');
            expect(mockSend).not.toHaveBeenCalled();
        });
    });

    describe('toggleTune', () => {
        it('no-ops when CAT is off (cannot tune a non-live rig)', () => {
            configState.bridge.tune = true;
            toggleTune();
            expect(mockTune).not.toHaveBeenCalled();
        });

        it('no-ops when the rig cannot tune (capability absent)', () => {
            setCatLive();
            configState.bridge.tune = false;
            toggleTune();
            expect(mockTune).not.toHaveBeenCalled();
        });

        it('sends active:true to start when idle + live + capable', () => {
            setCatLive();
            configState.bridge.tune = true;
            bridgeState.tuneActive = false;
            toggleTune();
            expect(mockTune).toHaveBeenCalledWith(true);
        });

        it('sends active:false to stop when already tuning', () => {
            setCatLive();
            configState.bridge.tune = true;
            bridgeState.tuneActive = true;
            toggleTune();
            expect(mockTune).toHaveBeenCalledWith(false);
        });
    });

    describe('nudgeFreq — CAT off', () => {
        beforeEach(_resetFreqStepForTests);

        it('nudges the selected VFO-A manualState freq, no rig command', () => {
            manualState.selectedVfo = 'A';
            manualState.vfoA = 14_074_000;
            nudgeFreq(10);
            expect(manualState.vfoA).toBe(14_074_010);
            nudgeFreq(-100);
            expect(manualState.vfoA).toBe(14_073_910);
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('nudges VFO-B manualState when B is selected', () => {
            manualState.selectedVfo = 'B';
            manualState.vfoB = 7_100_000;
            nudgeFreq(100);
            expect(manualState.vfoB).toBe(7_100_100);
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('clamps to >= 0', () => {
            manualState.selectedVfo = 'A';
            manualState.vfoA = 5;
            nudgeFreq(-100);
            expect(manualState.vfoA).toBe(0);
        });
    });

    describe('nudgeFreq — CAT live', () => {
        beforeEach(() => {
            setCatLive();
            _resetFreqStepForTests();
        });

        it('drives set_freq (FA) for VFO-A off the displayed freq; manualState untouched', () => {
            catState.selectedVfo = 'A';
            catState.vfoA = 14_074_000;
            manualState.vfoA = 999; // must NOT change while live
            configState.bridge.ops = ['set_freq'];
            nudgeFreq(10);
            expect(mockSend).toHaveBeenCalledWith('set_freq', '14074010');
            expect(manualState.vfoA).toBe(999);
        });

        it('drives set_freq_b (FB) for VFO-B', () => {
            catState.selectedVfo = 'B';
            catState.vfoB = 7_100_000;
            configState.bridge.ops = ['set_freq_b'];
            nudgeFreq(-100);
            expect(mockSend).toHaveBeenCalledWith('set_freq_b', '7099900');
        });

        it('no-ops when the rig exposes no freq op for the selected VFO', () => {
            catState.selectedVfo = 'A';
            catState.vfoA = 14_074_000;
            configState.bridge.ops = [];
            nudgeFreq(10);
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('no-ops on VFO-B when only set_freq (A) is exposed', () => {
            catState.selectedVfo = 'B';
            catState.vfoB = 7_100_000;
            configState.bridge.ops = ['set_freq']; // A only — no set_freq_b
            nudgeFreq(10);
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('accumulates rapid key-repeat steps optimistically (does not re-read the lagging push)', () => {
            catState.selectedVfo = 'A';
            catState.vfoA = 14_074_000; // displayed never updates (no push in test)
            configState.bridge.ops = ['set_freq'];
            nudgeFreq(10); // 14074000 → 14074010
            nudgeFreq(10); // off pending 14074010 → 14074020 (not re-read 14074000)
            nudgeFreq(10); // → 14074030
            expect(mockSend).toHaveBeenNthCalledWith(1, 'set_freq', '14074010');
            expect(mockSend).toHaveBeenNthCalledWith(2, 'set_freq', '14074020');
            expect(mockSend).toHaveBeenNthCalledWith(3, 'set_freq', '14074030');
        });
    });

    describe('nudgeFreqJump — ±5 kHz band hop', () => {
        beforeEach(_resetFreqStepForTests);

        it('nudges the selected VFO manualState by 5 kHz when CAT is off', () => {
            manualState.selectedVfo = 'A';
            manualState.vfoA = 14_074_000;
            nudgeFreqJump(1);
            expect(manualState.vfoA).toBe(14_079_000);
            nudgeFreqJump(-1);
            expect(manualState.vfoA).toBe(14_074_000);
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('drives set_freq with the +5 kHz absolute target when CAT is live', () => {
            setCatLive();
            catState.selectedVfo = 'A';
            catState.vfoA = 14_074_000;
            configState.bridge.ops = ['set_freq'];
            nudgeFreqJump(1);
            expect(mockSend).toHaveBeenCalledWith('set_freq', '14079000');
        });
    });

    describe('setMode', () => {
        it('writes the friendly mode to manualState when CAT is off (no rig command)', () => {
            setMode('USB');
            expect(manualState.mode).toBe('USB');
            expect(mockSend).not.toHaveBeenCalled();
        });

        it('drives set_mode with the rig literal when CAT is live + capable', () => {
            setCatLive();
            configState.bridge.ops = ['set_mode'];
            manualState.mode = 'CW'; // must NOT change while live
            setMode('LSB');
            expect(mockSend).toHaveBeenCalledWith('set_mode', 'LSB');
            expect(manualState.mode).toBe('CW');
        });

        it('is a no-op when CAT is live but the rig does not expose set_mode', () => {
            setCatLive();
            configState.bridge.ops = [];
            setMode('LSB');
            expect(mockSend).not.toHaveBeenCalled();
        });
    });
});
