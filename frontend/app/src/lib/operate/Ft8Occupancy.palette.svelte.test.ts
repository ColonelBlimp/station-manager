// Palette architecture (W-0004 AC5/AC6): the two occupancy surfaces carry ONE
// semantic token layer (--color-occ-*), not first-pass literal red/green/amber/
// orange utilities. These rules pin the STATE → TOKEN mapping and that colour is
// never the sole carrier (titles, ★/▼ arrows, captions survive). They deliberately
// do NOT assert colour VALUES — readability is judged on the light/dark state-matrix
// fixture, not from a class name (dossier: source-class assertions cannot prove
// readability). The value knobs live in one place (app.css) precisely so tuning
// them never touches these components or these tests.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Ft8Occupancy from './Ft8Occupancy.svelte';
import { ft8Link, ft8State, resetFt8ForTests } from './ft8.svelte';
import { rig } from './rig.svelte';
import type { OccupancyPayload } from '../api/ft8-sse';

beforeEach(() => {
    resetFt8ForTests();
    rig.band = '20m';
});

// occupied [1000,1050], 50 Hz slots from 200 Hz; recommended (suggested[0]) = 1500.
function occupancy(): OccupancyPayload {
    return {
        slot: { start_utc: '2026-07-10T12:00:00Z', period: 'even' },
        passband: { low_hz: 200, high_hz: 3000 },
        signal_width_hz: 50,
        occupied: [{ low_hz: 1000, high_hz: 1050 }],
        suggested: [1500, 700],
    };
}

describe('Occupancy Channels palette (Ft8OccupancyStrip)', () => {
    async function channels() {
        ft8Link.onOccupancy(occupancy());
        flushSync();
        const { container } = render(Ft8Occupancy);
        const { fireEvent } = await import('@testing-library/svelte');
        await fireEvent.click(screen.getByRole('button', { name: 'Channels' }));
        flushSync();
        return container;
    }

    it('a busy cell fills with the occ-busy token, not a literal red', async () => {
        await channels();
        const busy = screen.getByLabelText('Select TX offset 1000 hertz'); // overlaps [1000,1050]
        expect(busy.className).toContain('bg-occ-busy');
        expect(busy.className).not.toMatch(/bg-red-\d/);
    });

    it('a clear cell fills with the occ-clear token, not a literal green', async () => {
        await channels();
        const clear = screen.getByLabelText('Select TX offset 200 hertz'); // no overlap
        expect(clear.className).toContain('bg-occ-clear');
        expect(clear.className).not.toMatch(/bg-green-\d/);
    });

    it("marks the daemon's recommended cell with the occ-recommend underline", async () => {
        await channels();
        const rec = screen.getByLabelText('Select TX offset 1500 hertz'); // suggested[0]
        expect(rec.className).toContain('border-b-occ-recommend');
        expect(rec.className).not.toMatch(/border-b-amber-\d/);
    });

    it('carries busy/clear/recommended in the title, not colour alone (AC6)', async () => {
        await channels();
        expect(screen.getByLabelText('Select TX offset 1000 hertz').getAttribute('title')).toMatch(
            /busy/
        );
        expect(screen.getByLabelText('Select TX offset 200 hertz').getAttribute('title')).toMatch(
            /clear/
        );
        expect(screen.getByLabelText('Select TX offset 1500 hertz').getAttribute('title')).toMatch(
            /recommended/
        );
    });
});

describe('Occupancy Spectrum palette (Ft8OccupancySpectrum)', () => {
    // Spectrum is the default view; set a proximity via the selected offset, then read
    // the graded caption + footprint. occupied [1000,1050], width 50, near margin 50.
    function spectrumAt(selected: number) {
        ft8Link.onOccupancy(occupancy());
        ft8State.selectedOffset = selected;
        flushSync();
        return render(Ft8Occupancy).container;
    }

    it('a sharing pick uses occ-sharing on the caption and footprint, not orange literals', () => {
        const container = spectrumAt(1030); // [1030,1080] overlaps the signal
        const caption = screen.getByText(/1030 Hz · sharing/);
        expect(caption.className).toContain('text-occ-sharing');
        expect(caption.className).not.toMatch(/text-orange-\d/);
        const footprint = container.querySelector('.border-x-2');
        expect(footprint?.className).toContain('bg-occ-sharing');
        expect(footprint?.className).toContain('border-occ-sharing');
    });

    it('a near pick uses occ-near, not amber literals', () => {
        const container = spectrumAt(1080); // gap 30 Hz < signal width
        const caption = screen.getByText(/near/);
        expect(caption.className).toContain('text-occ-near');
        expect(caption.className).not.toMatch(/text-amber-\d/);
        expect(container.querySelector('.border-x-2')?.className).toContain('bg-occ-near');
    });

    it('a clear pick uses occ-clear, not green literals', () => {
        const container = spectrumAt(2000);
        const caption = screen.getByText(/2000 Hz · clear/);
        expect(caption.className).toContain('text-occ-clear');
        expect(caption.className).not.toMatch(/text-green-\d/);
        expect(container.querySelector('.border-x-2')?.className).toContain('bg-occ-clear');
    });

    it('shades signals with the neutral occ-signal token', () => {
        const container = spectrumAt(2000);
        // the "a signal" region — soft neutral shading at the occupied band's position.
        // Substring match: the class carries the /40 opacity suffix (bg-occ-signal/40).
        const shaded = container.querySelector('[class*="bg-occ-signal"]');
        expect(shaded).not.toBeNull();
    });

    it("marks the daemon's top pick with occ-recommend on the ★, not an amber literal", () => {
        spectrumAt(2000);
        const star = screen.getByTitle(/top pick/);
        expect(star.textContent).toBe('★'); // symbol carrier, not colour alone (AC6)
        expect(star.className).toContain('text-occ-recommend');
        expect(star.className).not.toMatch(/text-amber-\d/);
    });
});
