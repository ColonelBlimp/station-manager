// Render-path test: a pushToast from ANY module must appear in the mounted
// overlay. Guards the state-module ↔ renderer wiring (the queue logic itself
// is covered in toasts.test.ts).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Toasts from './Toasts.svelte';
import { toasts, _resetForTests } from './toasts.svelte';

beforeEach(() => {
    _resetForTests();
});

describe('Toasts renderer', () => {
    it('shows a pushed toast with its severity prefix for screen readers', async () => {
        render(Toasts);
        toasts.info('Logged DL3YA ✓');
        flushSync();
        const item = await screen.findByRole('status');
        expect(item.textContent).toContain('Logged DL3YA ✓');
        expect(item.textContent).toContain('Info:');
    });

    it('renders errors as role=alert', async () => {
        render(Toasts);
        toasts.error('boom');
        flushSync();
        const item = await screen.findByRole('alert');
        expect(item.textContent).toContain('boom');
    });
});

/*
    LAYER CONTRACT — dogfood 2026-08-07: clicking Send in the Export session
    card raised a toast UNDER the card's overlay, dimmed by its backdrop —
    both layers sat at z-50 and the later-mounted dialog won. The rule is
    comparative, not a pinned literal: the toast layer must sit STRICTLY
    above every modal overlay, because toasts are the feedback for actions
    taken INSIDE those modals. Renumbering layers is fine; a tie is not.
*/
describe('Toasts layering', () => {
    it('the toast layer sits strictly above the export dialog overlay', async () => {
        const { openExport } = await import('../operate/state.svelte');
        const { default: ExportDialog } = await import('../operate/ExportDialog.svelte');

        render(Toasts);
        render(ExportDialog);
        openExport();
        toasts.info('Session emailed ✓');
        flushSync();

        const zOf = (el: Element | null): number => {
            const m = /(?:^|\s)z-(\d+)(?:\s|$)/.exec(el?.className ?? '');
            expect(
                m,
                `no z-N utility on ${el?.getAttribute('role') ?? el?.tagName}`
            ).not.toBeNull();
            return Number(m![1]);
        };
        const toastLayer = (await screen.findByRole('status')).closest('.fixed');
        const dialogLayer = screen.getByRole('dialog');
        expect(zOf(toastLayer)).toBeGreaterThan(zOf(dialogLayer));
    });
});
