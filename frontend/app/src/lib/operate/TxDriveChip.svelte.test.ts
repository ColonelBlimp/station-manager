// TX-drive chip render contract (ADR 0064 acceptance criterion 1's render
// half): hidden before any data; the three tellable-apart states carry
// data-state + the value text. The state LOGIC (thresholds, staleness
// windows) is pinned in txDrive.svelte.test.ts — these rules only bind
// state → DOM.

import { describe, it, expect, beforeEach } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import TxDriveChip from './TxDriveChip.svelte';
import { onRigMeters, resetTxDriveForTests } from './txDrive.svelte';

const chip = (): HTMLElement | null => document.querySelector('[data-txdrive-chip]');

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
});
