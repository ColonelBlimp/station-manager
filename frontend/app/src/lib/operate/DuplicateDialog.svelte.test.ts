// Dialog render-path test: the duplicate refusal must surface as a modal
// with a working safe path (Cancel keeps the draft). The state transitions
// themselves are covered in qso.svelte.test.ts.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import DuplicateDialog from './DuplicateDialog.svelte';
import { draft, submitState, clearDraft } from './qso.svelte';
import { rig, confirmRig, resetCatLink } from './rig.svelte';

beforeEach(() => {
    resetCatLink();
    rig.cat = 'off';
    confirmRig();
    clearDraft();
});

describe('DuplicateDialog', () => {
    it('is absent until a duplicate refusal arrives, then renders as a modal', async () => {
        render(DuplicateDialog);
        expect(screen.queryByRole('dialog')).toBeNull();

        submitState.error = 'Duplicate — this QSO is already in the log.';
        submitState.duplicate = true;
        flushSync();

        const dialog = await screen.findByRole('dialog');
        expect(dialog).toHaveAttribute('aria-modal', 'true');
        expect(dialog.textContent).toContain('Duplicate QSO');
        expect(dialog.textContent).toContain('already in the log');
    });

    it('Cancel dismisses the refusal and keeps the draft', async () => {
        draft.callsign = 'DL3YA';
        submitState.error = 'Duplicate.';
        submitState.duplicate = true;
        render(DuplicateDialog);
        flushSync();

        (await screen.findByRole('button', { name: 'Cancel' })).click();
        flushSync();

        // State flips immediately; the panel's own leave transition (fade)
        // handles the un-mount, so we assert the state, not DOM removal.
        expect(submitState.duplicate).toBe(false);
        expect(draft.callsign).toBe('DL3YA');
    });
});
