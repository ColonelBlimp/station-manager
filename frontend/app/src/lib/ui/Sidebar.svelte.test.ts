// The sidebar's bottom-utilities Map link is the app's ONLY entry point to the
// contacts map — the Session tile carried a duplicate until 2026-07-25, and this
// coverage moved here when that one was removed. The map is a standalone
// time-window view meant for a second monitor, so it must open in its own tab
// (ADR 0049 rejection); losing target="_blank" would unmount a live FT8 run.

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Sidebar from './Sidebar.svelte';

describe('Sidebar map link', () => {
    it('opens the contacts map in a new tab', () => {
        render(Sidebar);

        const link = screen.getByRole('link', { name: /Map/ });
        expect(link).toHaveAttribute('target', '_blank');
        expect(link).toHaveAttribute('rel', 'noopener');
        expect(link.getAttribute('href')).toMatch(/map$/);
    });
});
