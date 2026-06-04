import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import TuneButton from './TuneButton.svelte';
import { configState } from '../../states/config.svelte';
import { bridgeState } from '../../states/bridge.svelte';
import { sendRigTune } from '../../api/rigTune';

/**
 * TuneButton tests (ADR 0027). The button is rendered only when the rig can
 * tune (configState.bridge.tune), disabled unless the rig is live, and
 * reflects the daemon-owned bridgeState.tuneActive. A click drives the real
 * toggleTune action; the transport is mocked so we assert the request shape
 * (start vs stop) without the HTTP wire.
 */
vi.mock('../../api/rigTune', () => ({
    sendRigTune: vi.fn().mockResolvedValue({ kind: 'ok' }),
}));

const mockTune = vi.mocked(sendRigTune);

function setCatLive(): void {
    configState.station.enabled = true;
    bridgeState.connected = true;
    bridgeState.rigResponding = true;
}

describe('TuneButton', () => {
    beforeEach(() => {
        mockTune.mockClear();
        mockTune.mockResolvedValue({ kind: 'ok' });
        // CAT off + tune unsupported by default.
        configState.station.enabled = false;
        configState.bridge.tune = false;
        bridgeState.connected = false;
        bridgeState.rigResponding = false;
        bridgeState.tuneActive = false;
    });

    afterEach(() => {
        cleanup();
    });

    it('renders nothing when the rig cannot tune', () => {
        configState.bridge.tune = false;
        const { container } = render(TuneButton);
        expect(container.querySelector('button')).toBeNull();
    });

    it('renders a Tune button when the rig can tune', () => {
        configState.bridge.tune = true;
        const { container } = render(TuneButton);
        const btn = container.querySelector('button');
        expect(btn).not.toBeNull();
        expect(btn?.textContent).toContain('Tune');
    });

    it('is disabled when CAT is off (cannot tune a non-live rig)', () => {
        configState.bridge.tune = true;
        // CAT off → not live.
        const { container } = render(TuneButton);
        const btn = container.querySelector('button') as HTMLButtonElement;
        expect(btn.disabled).toBe(true);
    });

    it('is enabled when the rig is live', () => {
        configState.bridge.tune = true;
        setCatLive();
        const { container } = render(TuneButton);
        const btn = container.querySelector('button') as HTMLButtonElement;
        expect(btn.disabled).toBe(false);
    });

    it('shows the transmitting state when tune is active', () => {
        configState.bridge.tune = true;
        setCatLive();
        bridgeState.tuneActive = true;
        const { container } = render(TuneButton);
        const btn = container.querySelector('button') as HTMLButtonElement;
        expect(btn.textContent).toContain('Tuning');
        expect(btn.getAttribute('aria-pressed')).toBe('true');
    });

    it('sends active:true on click when idle + live', async () => {
        configState.bridge.tune = true;
        setCatLive();
        bridgeState.tuneActive = false;
        const { container } = render(TuneButton);
        await fireEvent.click(container.querySelector('button') as HTMLButtonElement);
        expect(mockTune).toHaveBeenCalledWith(true);
    });

    it('sends active:false on click when already tuning', async () => {
        configState.bridge.tune = true;
        setCatLive();
        bridgeState.tuneActive = true;
        const { container } = render(TuneButton);
        await fireEvent.click(container.querySelector('button') as HTMLButtonElement);
        expect(mockTune).toHaveBeenCalledWith(false);
    });

    it('does not transmit on click when CAT is off (button disabled)', async () => {
        configState.bridge.tune = true;
        // CAT off → button disabled → click is inert.
        const { container } = render(TuneButton);
        await fireEvent.click(container.querySelector('button') as HTMLButtonElement);
        expect(mockTune).not.toHaveBeenCalled();
    });
});
