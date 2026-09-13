// The Phone/CW card announces a draft that survived a mode switch (operator
// ruling 2026-09-13): its age ticks, and the ORIGINAL Time On is named so the
// operator can see what would be logged.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from './LoggingCard.svelte';
import { draft, clearDraft, startQso, noteModeSwitchForDraft } from './qso.svelte';

describe('LoggingCard: a draft that survived a mode switch shows its age', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-09-13T14:30:00Z'));
        clearDraft();
    });
    afterEach(() => {
        clearDraft();
        vi.useRealTimers();
    });

    it('no age line for a fresh draft; the line appears after a switch and ticks', () => {
        render(LoggingCard);
        draft.callsign = 'ZS6BOS';
        startQso();
        flushSync();
        expect(screen.queryByRole('status', { name: 'Draft age' })).toBeNull();

        noteModeSwitchForDraft('phone', 'ft8');
        vi.advanceTimersByTime(3 * 60_000);
        flushSync();
        const age = screen.getByRole('status', { name: 'Draft age' });
        expect(age).toHaveTextContent('3 min');
        expect(age).toHaveTextContent('14:30:00'); // the original Time On, kept
        expect(draft.timeOn).toBe('14:30:00');

        vi.advanceTimersByTime(2 * 60_000);
        flushSync();
        expect(screen.getByRole('status', { name: 'Draft age' })).toHaveTextContent('5 min');
    });
});
