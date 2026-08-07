// TX-drive chip render contract (ADR 0064 acceptance criterion 1's render
// half): hidden before any data; the three tellable-apart states carry
// data-state + the value text. The state LOGIC (thresholds, staleness
// windows) is pinned in txDrive.svelte.test.ts — these rules only bind
// state → DOM.

import { describe, it, expect, beforeEach } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import TxDriveChip from './TxDriveChip.svelte';
import { onRigMeters, resetTxDriveForTests, setTxDriveConfig } from './txDrive.svelte';

const chip = (): HTMLElement | null => document.querySelector('[data-txdrive-chip]');
const card = (): HTMLElement | null => document.querySelector('[data-txdrive-card]');

beforeEach(() => {
    resetTxDriveForTests();
});

describe('TxDriveChip', () => {
    it('renders nothing before the first ALC answer', () => {
        render(TxDriveChip);
        flushSync();
        expect(chip()).toBeNull();
    });

    it('a fresh zero renders good with the value', () => {
        render(TxDriveChip);
        onRigMeters({ meter: 'ALC', value: 0 }, Date.now());
        flushSync();
        expect(chip()!.dataset.state).toBe('good');
        expect(chip()!.textContent).toContain('ALC 0');
    });

    it('an over-threshold reading renders red with the value', () => {
        render(TxDriveChip);
        onRigMeters({ meter: 'ALC', value: 62 }, Date.now());
        flushSync();
        expect(chip()!.dataset.state).toBe('red');
        expect(chip()!.textContent).toContain('ALC 62');
    });

    // Same interaction grammar as the RX audio meter beside it: chip click
    // opens the card, the card's MINUS folds back to the chip (never closed).
    it('chip opens the card and minimise folds it back', () => {
        render(TxDriveChip);
        onRigMeters({ meter: 'ALC', value: 26 }, Date.now());
        flushSync();

        chip()!.click();
        flushSync();
        expect(card()).not.toBeNull();
        expect(chip()).toBeNull();

        card()!.querySelector<HTMLButtonElement>('[data-txdrive-collapse]')!.click();
        flushSync();
        expect(card()).toBeNull();
        expect(chip()).not.toBeNull();
    });

    // The card's fixed structure (the audio card's V6 lesson): bar track +
    // two lines render in a value state AND in stale — only content varies.
    // The threshold marker sits at the configured red value on the 0-255
    // scale, so the operator can SEE how close the fill is to red.
    it('card keeps its structure across states and marks the red threshold', () => {
        setTxDriveConfig(51, 250); // 51/255 = 20% — a round marker position
        render(TxDriveChip);
        onRigMeters({ meter: 'ALC', value: 26 }, Date.now());
        flushSync();
        chip()!.click();
        flushSync();

        expect(card()!.querySelector('[data-meter-bar]')).not.toBeNull();
        expect(card()!.querySelectorAll('[data-meter-line]').length).toBe(2);
        // Anchored by RIGHT so the marker's 2px body always sits INSIDE the
        // overflow-hidden track — left-anchoring clipped it entirely at the
        // valid threshold 255 (left:100%, codex P2 on 84886af2).
        const marker = card()!.querySelector<HTMLElement>('[data-alc-red-marker]')!;
        expect(marker.style.right).toBe('80%');
        expect(card()!.textContent).toContain('ALC 26 of 255');

        // Stale: same structure, no-data content (a dead poll must not look
        // like a clean zero).
        onRigMeters({ meter: 'ALC', value: 26 }, Date.now() - 10_000);
        flushSync();
        expect(card()!.dataset.state).toBe('stale');
        expect(card()!.querySelector('[data-meter-bar]')).not.toBeNull();
        expect(card()!.querySelectorAll('[data-meter-line]').length).toBe(2);
        expect(card()!.textContent).toContain('no poll answers');
    });

    it('the marker stays visible at the maximum valid threshold (255)', () => {
        setTxDriveConfig(255, 250);
        render(TxDriveChip);
        onRigMeters({ meter: 'ALC', value: 10 }, Date.now());
        flushSync();
        chip()!.click();
        flushSync();

        const marker = card()!.querySelector<HTMLElement>('[data-alc-red-marker]')!;
        // right: 0% puts the marker's right edge at the track's right edge,
        // its body extending inward — never into the clipped overflow.
        expect(marker.style.right).toBe('0%');
    });
});
