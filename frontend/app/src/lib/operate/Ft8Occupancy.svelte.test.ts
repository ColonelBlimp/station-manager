// Render-path test for the Occupancy TX-offset picker: the daemon's occupancy
// snapshot (fed through the state module's ft8-occupancy handler) must render both
// presentations, a channel click must pin ft8State.selectedOffset, and the
// Channels/Spectrum toggle must switch views without disturbing the pick. The pure
// grading/mapping maths is covered in ft8Spectrum.test.ts.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Ft8Occupancy from './Ft8Occupancy.svelte';
import { ft8State, ft8Link, resetFt8ForTests } from './ft8.svelte';
import type { OccupancyPayload } from '../api/ft8-sse';

beforeEach(() => {
    resetFt8ForTests();
});

function occupancy(): OccupancyPayload {
    return {
        slot: { start_utc: '2026-07-10T12:00:00Z', period: 'even' },
        passband: { low_hz: 200, high_hz: 3000 },
        signal_width_hz: 50,
        occupied: [{ low_hz: 1000, high_hz: 1050 }],
        suggested: [1500, 700],
    };
}

describe('Ft8Occupancy picker', () => {
    it('waits for a slot before rendering the strip', () => {
        render(Ft8Occupancy);
        expect(screen.getByText(/Waiting for slot/)).toBeInTheDocument();
    });

    it('clicking a channel pins selectedOffset (ending the auto fallback)', async () => {
        ft8Link.onOccupancy(occupancy());
        flushSync();
        expect(ft8State.effectiveOffset).toBe(1500); // auto = suggested[0]

        render(Ft8Occupancy);
        await fireEvent.click(screen.getByLabelText('Select TX offset 700 hertz'));
        flushSync();

        expect(ft8State.selectedOffset).toBe(700);
        expect(ft8State.effectiveOffset).toBe(700); // pinned, no longer the daemon's pick
    });

    it('toggles to the Spectrum view without disturbing the pick', async () => {
        ft8Link.onOccupancy(occupancy());
        ft8State.selectedOffset = 1234;
        flushSync();

        render(Ft8Occupancy);
        // Channels is the default — the continuous slider is absent.
        expect(screen.queryByLabelText('TX offset (continuous)')).toBeNull();

        await fireEvent.click(screen.getByRole('button', { name: 'Spectrum' }));
        flushSync();

        expect(ft8State.occupancyView).toBe('spectrum');
        expect(screen.getByLabelText('TX offset (continuous)')).toBeInTheDocument();
        expect(ft8State.selectedOffset).toBe(1234); // view choice ≠ offset pick
    });
});
