/*
    RX audio-level card — the bottom-left corner instrument (operator-designed
    2026-08-06: "a separate card near bottom left - one that has an icon and
    can be toggled open/close", plus the agreed enhancement that the COLLAPSED
    icon carries the current state colour, so closed is never blind).

    Criterion + classification rules: audioLevel.svelte.test.ts. These rules
    pin the card's own contract:
      - data-state carries the classification (the colour styling hangs off
        it — jsdom does no rendering, so the attribute IS the seam);
      - the chip toggles the open card and back;
      - TX STAND-DOWN: while the rig is keyed the capture path carries
        nothing useful, so the card shows 'tx', never a misleading orange —
        this is the card's rule, deliberately not the classifier's;
      - the open card shows the dB readout.
    Geometric placement (bottom-left, clear of toasts/drawers) is the
    Playwright layer's, when it exists.
*/

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import AudioLevelCard from './AudioLevelCard.svelte';
import { onAudioLevel, resetAudioLevel, setAudioLevelOpen } from './audioLevel.svelte';
import { ft8State, resetFt8ForTests } from './ft8.svelte';

beforeEach(() => {
    vi.useFakeTimers();
    resetAudioLevel();
    resetFt8ForTests();
});

afterEach(() => {
    vi.useRealTimers();
});

const chip = (): HTMLElement | null => document.querySelector('[data-audio-chip]');
const card = (): HTMLElement | null => document.querySelector('[data-audio-card]');

describe('AudioLevelCard', () => {
    // V1 — collapsed by default: the chip is there, the card is not, and the
    // chip carries the classification for its colour.
    it('V1: renders a state-carrying chip, collapsed by default', () => {
        render(AudioLevelCard);
        onAudioLevel({ peak_dbfs: -20, rms_dbfs: -30 });
        flushSync();

        expect(card()).toBeNull();
        expect(chip()).not.toBeNull();
        expect(chip()!.dataset.state).toBe('good');
    });

    // V2 — the toggle: chip opens the card; the card's MINIMISE (a minus,
    // not an X — the meter is never 'closed', it folds back to the live chip)
    // collapses it again.
    it('V2: chip opens the card and minimise folds it back', async () => {
        render(AudioLevelCard);
        flushSync();

        chip()!.click();
        flushSync();
        expect(card()).not.toBeNull();
        expect(chip()).toBeNull();

        card()!.querySelector<HTMLButtonElement>('[data-audio-collapse]')!.click();
        flushSync();
        expect(card()).toBeNull();
        expect(chip()).not.toBeNull();
    });

    // V3 — TX stand-down: keyed → 'tx' whatever the last reading said. The
    // fixture plants a CLIPPING reading first, so an implementation that
    // ignores TX shows 'high' and fails.
    it('V3: shows tx while transmitting, whatever the level was', () => {
        render(AudioLevelCard);
        onAudioLevel({ peak_dbfs: -0.2, rms_dbfs: -5 });
        ft8State.tx.transmitting = true;
        flushSync();

        expect(chip()!.dataset.state).toBe('tx');
    });

    // V4 — no reading yet: 'off', which the open card names as no capture —
    // the nearest-confusable guard from the criterion.
    it('V4: reports off with no capture', () => {
        render(AudioLevelCard);
        flushSync();

        expect(chip()!.dataset.state).toBe('off');
    });

    // V5 — the open card carries the numeric readout the operator calibrates
    // against.
    it('V5: the open card shows the dB readout', () => {
        render(AudioLevelCard);
        onAudioLevel({ peak_dbfs: -12.3, rms_dbfs: -34.5 });
        setAudioLevelOpen(true);
        flushSync();

        const text = card()!.textContent ?? '';
        expect(text).toContain('-34.5');
        expect(text).toContain('-12.3');
    });
});
