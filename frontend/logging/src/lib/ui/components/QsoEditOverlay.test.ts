import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import QsoEditOverlay from './QsoEditOverlay.svelte';
import { qsoEditState } from '../../states/qsoEdit.svelte';

/**
 * Focus-trap + ESC propagation tests for the QSO edit overlay.
 *
 * Targets reviewer findings C3 (Tab walks through the dialog into the
 * underlying form) and C4 (ESC must stopPropagation so the underlying
 * QsoPanel keydown cannot also clear the live draft).
 *
 * Test approach: render the overlay with `qsoEditState.open=true,
 * loading=false`, drive focus + keystrokes via @testing-library, and
 * assert document.activeElement / propagation flags.
 */

function resetState(): void {
    qsoEditState.open = false;
    qsoEditState.loading = false;
    qsoEditState.saving = false;
    qsoEditState.uuid = 'test-uuid';
    qsoEditState.callsign = 'M0ABC';
    qsoEditState.name = '';
    qsoEditState.qth = '';
    qsoEditState.country = '';
    qsoEditState.comment = '';
    qsoEditState.mode = 'USB';
    qsoEditState.freq = '14.250';
    qsoEditState.freqRx = '';
    qsoEditState.rstSent = '59';
    qsoEditState.rstRcvd = '59';
    qsoEditState.qsoDate = '2026-05-12';
    qsoEditState.timeOn = '15:30';
    qsoEditState.timeOff = '15:35';
    qsoEditState.rig = '';
    qsoEditState.rxPwr = '';
    qsoEditState.notes = '';
    qsoEditState.requestQsl = false;
}

describe('QsoEditOverlay — focus trap (C3)', () => {
    beforeEach(() => {
        resetState();
    });

    afterEach(() => {
        cleanup();
        resetState();
    });

    it('focuses the first input when the form has rendered', async () => {
        qsoEditState.open = true;
        qsoEditState.loading = false;
        render(QsoEditOverlay);
        await tick();
        await tick();

        const active = document.activeElement as HTMLElement | null;
        expect(active).not.toBeNull();
        expect(active?.tagName).toBe('INPUT');
        expect(active?.id).toBe('edit-callsign');
    });

    it('does not focus inputs while loading=true (no form rendered yet)', async () => {
        qsoEditState.open = true;
        qsoEditState.loading = true;
        render(QsoEditOverlay);
        await tick();

        // No input rendered, so focus shouldn't be on edit-callsign.
        const active = document.activeElement as HTMLElement | null;
        expect(active?.id).not.toBe('edit-callsign');
    });

    it('wraps focus from the last button back to the first input on Tab', async () => {
        qsoEditState.open = true;
        qsoEditState.loading = false;
        const { container } = render(QsoEditOverlay);
        await tick();
        await tick();

        // Find first input and last focusable (Save button).
        const dialog = container.querySelector('[role="document"]') as HTMLElement;
        const focusables = dialog.querySelectorAll<HTMLElement>(
            'a[href],button:not([disabled]),input:not([disabled]),' +
                'select:not([disabled]),textarea:not([disabled]),' +
                '[tabindex]:not([tabindex="-1"])'
        );
        const first = focusables[0];
        const last = focusables[focusables.length - 1];

        // Focus the last element and Tab forward — should wrap to first.
        last.focus();
        expect(document.activeElement).toBe(last);
        await fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(first);
    });

    it('wraps focus from the first input back to the last button on Shift+Tab', async () => {
        qsoEditState.open = true;
        qsoEditState.loading = false;
        const { container } = render(QsoEditOverlay);
        await tick();
        await tick();

        const dialog = container.querySelector('[role="document"]') as HTMLElement;
        const focusables = dialog.querySelectorAll<HTMLElement>(
            'a[href],button:not([disabled]),input:not([disabled]),' +
                'select:not([disabled]),textarea:not([disabled]),' +
                '[tabindex]:not([tabindex="-1"])'
        );
        const first = focusables[0];
        const last = focusables[focusables.length - 1];

        first.focus();
        expect(document.activeElement).toBe(first);
        await fireEvent.keyDown(window, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(last);
    });

    it('pulls focus back into the dialog if it has escaped', async () => {
        qsoEditState.open = true;
        qsoEditState.loading = false;
        const { container } = render(QsoEditOverlay);
        await tick();
        await tick();

        // Create an element outside the dialog and focus it.
        const stranger = document.createElement('input');
        document.body.appendChild(stranger);
        stranger.focus();
        expect(document.activeElement).toBe(stranger);

        await fireEvent.keyDown(window, { key: 'Tab' });

        const dialog = container.querySelector('[role="document"]') as HTMLElement;
        expect(dialog.contains(document.activeElement)).toBe(true);

        document.body.removeChild(stranger);
    });
});

describe('QsoEditOverlay — ESC propagation (C4)', () => {
    beforeEach(() => {
        resetState();
    });

    afterEach(() => {
        cleanup();
        resetState();
    });

    it('closes the overlay on ESC', async () => {
        qsoEditState.open = true;
        qsoEditState.loading = false;
        render(QsoEditOverlay);
        await tick();

        expect(qsoEditState.open).toBe(true);
        await fireEvent.keyDown(window, { key: 'Escape' });
        expect(qsoEditState.open).toBe(false);
    });

    it('does NOT close on ESC while saving=true', async () => {
        qsoEditState.open = true;
        qsoEditState.loading = false;
        qsoEditState.saving = true;
        render(QsoEditOverlay);
        await tick();

        await fireEvent.keyDown(window, { key: 'Escape' });
        // Overlay must still be open — operator should wait for the
        // PATCH to resolve rather than discarding mid-flight.
        expect(qsoEditState.open).toBe(true);
    });

    it('stops ESC propagation so a sibling window handler does not fire', async () => {
        qsoEditState.open = true;
        qsoEditState.loading = false;
        render(QsoEditOverlay);
        await tick();

        // Install a competing window keydown handler that mimics
        // QsoPanel.handleKeydown's "clear draft on ESC" behaviour.
        // If the overlay's handler stops propagation correctly this
        // shouldn't fire.
        let siblingFired = false;
        const sibling = (): void => {
            siblingFired = true;
        };
        window.addEventListener('keydown', sibling);

        try {
            await fireEvent.keyDown(window, { key: 'Escape' });
        } finally {
            window.removeEventListener('keydown', sibling);
        }

        expect(qsoEditState.open).toBe(false);
        expect(siblingFired).toBe(false);
    });
});
