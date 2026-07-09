// Render-path test: decodes pushed through the state module's ft8-decode handler
// must appear in the mounted pane, grouped by slot, with CQ + calling-you tints.
// Guards the ft8State ↔ Band Activity wiring (the feed logic is covered in
// ft8.svelte.test.ts).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Ft8BandActivity from './Ft8BandActivity.svelte';
import { ft8Link, setFt8OperatorCall, setFt8MyGrid, resetFt8ForTests } from './ft8.svelte';
import { setFt8Enricher, resetFt8EnrichForTests } from './ft8Enrich.svelte';
import { rig } from './rig.svelte';
import type { DecodeReport } from '../api/ft8-sse';

const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
    resetFt8ForTests();
    resetFt8EnrichForTests();
    rig.band = '20m';
});

function decode(
    startUtc: string,
    lines: { text: string; freq_hz: number; snr: number }[]
): DecodeReport {
    return {
        slot: { start_utc: startUtc, period: 'even' },
        decodes: lines.map((l) => ({ ...l, dt_s: 0 })),
    };
}

describe('Ft8BandActivity renderer', () => {
    it('shows the empty state before any decode', () => {
        render(Ft8BandActivity);
        expect(screen.getByText(/Decodes appear here/)).toBeInTheDocument();
    });

    it('renders live decodes grouped by slot, with CQ + calling-you tints', () => {
        setFt8OperatorCall('7Q5MLV');
        render(Ft8BandActivity);

        ft8Link.onDecode(
            decode('2026-07-09T14:30:15Z', [
                { text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -12 },
                { text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }, // calling us
            ])
        );
        flushSync();

        // Both decodes rendered.
        expect(screen.getByText('CQ W1ABC FN42')).toBeInTheDocument();
        expect(screen.getByText('7Q5MLV PA3KUS JO21')).toBeInTheDocument();
        // Slot divider carries the UTC clock.
        expect(screen.getByText(/14:30:15/)).toBeInTheDocument();

        // CQ row tinted amber; the station calling us tinted blue.
        const cqRow = screen.getByText('CQ W1ABC FN42').closest('tr');
        expect(cqRow?.className).toContain('amber');
        const callRow = screen.getByText('7Q5MLV PA3KUS JO21').closest('tr');
        expect(callRow?.className).toContain('blue');
    });

    it('shows a per-CQ short-path bearing from the operator grid + decode grid', () => {
        setFt8MyGrid('IO91'); // London-ish
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -8 }]));
        flushSync();
        // FN42 (New England) from IO91 is roughly WNW — just assert a degree value renders.
        const row = screen.getByText('CQ W1ABC FN42').closest('tr');
        expect(row?.textContent).toMatch(/\d+°/);
    });

    it('renders the country flag once the enricher resolves', async () => {
        setFt8Enricher(() =>
            Promise.resolve({
                country: 'United States',
                ccode: 'US',
                dxcc: '291',
                isNewEntity: false,
                grid: 'FN42',
                name: '',
                qth: '',
                email: '',
                cqZone: '',
                ituZone: '',
            })
        );
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -8 }]));
        flushSync();
        await flush();
        flushSync();
        expect(screen.getByText('CQ W1ABC FN42').closest('tr')?.textContent).toContain('🇺🇸');
    });
});
