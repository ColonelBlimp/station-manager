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
