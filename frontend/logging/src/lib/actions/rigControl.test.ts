import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { selectVfo, swapVfo, bandUp, bandDown, selectBand } from './rigControl';
import { manualState } from '../states/manual.svelte';
import { configState } from '../states/config.svelte';
import { bridgeState } from '../states/bridge.svelte';
import { catState } from '../states/cat.svelte';
import { sendRigCommand } from '../api/rigCommand';

// The transport is mocked — these tests pin the CAT-on/off + capability
// branching, not the HTTP wire (that's rigCommand's own concern).
vi.mock('../api/rigCommand', () => ({
    sendRigCommand: vi.fn().mockResolvedValue({ kind: 'ok' }),
}));

const mockSend = vi.mocked(sendRigCommand);

function setCatLive(): void {
    configState.station.enabled = true;
    bridgeState.connected = true;
    bridgeState.rigResponding = true;
}

describe('rigControl', () => {
    beforeEach(() => {
        mockSend.mockClear();
        mockSend.mockResolvedValue({ kind: 'ok' });
        // CAT off by default.
        configState.station.enabled = false;
        bridgeState.connected = false;
        bridgeState.rigResponding = false;
        configState.bridge.ops = [];
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
        beforeEach(setCatLive);

        it('sends set_vfo to the rig (not manualState) when the rig exposes set_vfo', () => {
            configState.bridge.ops = ['set_vfo'];
            selectVfo('B');
            expect(mockSend).toHaveBeenCalledWith('set_vfo', 'VFO-B');
            // manualState is untouched — confirm-by-push owns the UI when live.
            expect(manualState.selectedVfo).toBe('A');
        });

        it('is a no-op when the rig does not expose set_vfo', () => {
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

        it('toggles off the displayed (rig) selection when CAT is live', () => {
            setCatLive();
            configState.bridge.ops = ['set_vfo'];
            catState.selectedVfo = 'B'; // rig on B → swap targets A
            swapVfo();
            expect(mockSend).toHaveBeenCalledWith('set_vfo', 'VFO-A');
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
});
