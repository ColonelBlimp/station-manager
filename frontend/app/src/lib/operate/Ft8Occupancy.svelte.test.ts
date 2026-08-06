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
import { rig } from './rig.svelte';
import type { OccupancyPayload } from '../api/ft8-sse';

beforeEach(() => {
    resetFt8ForTests();
    rig.band = '20m'; // occupancy is band-scoped now; keep tests order-independent
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

    it('idle: an Even/Odd toggle switches which parity snapshot is shown', async () => {
        ft8Link.onOccupancy(occupancy()); // period: 'even'
        flushSync();
        render(Ft8Occupancy);

        // The toggle is labelled as the TX slot so its purpose is obvious.
        expect(screen.getByText('TX slot')).toBeInTheDocument();
        // Even active by default; switching to Odd changes the shown parity.
        await fireEvent.click(screen.getByRole('button', { name: 'Odd' }));
        flushSync();
        expect(ft8State.shownParity).toBe('odd');
    });

    it('during a QSO the toggle is replaced by a locked TX-parity cue', () => {
        ft8Link.onOccupancy(occupancy()); // even
        ft8Link.onQso({
            active: true,
            role: 'answerer',
            their_call: 'W1ABC',
            their_period: 'even',
        });
        flushSync();
        render(Ft8Occupancy);

        expect(screen.queryByRole('button', { name: 'Even' })).toBeNull(); // no toggle
        expect(screen.getByText(/odd · TX/)).toBeInTheDocument(); // TX slot = opposite even
    });

    it('clicking a channel pins selectedOffset (ending the auto fallback)', async () => {
        ft8Link.onOccupancy(occupancy());
        flushSync();
        expect(ft8State.effectiveOffset).toBe(1500); // auto = suggested[0]

        render(Ft8Occupancy);
        // Spectrum is the default — switch to Channels for the discrete markers.
        await fireEvent.click(screen.getByRole('button', { name: 'Channels' }));
        await fireEvent.click(screen.getByLabelText('Select TX offset 700 hertz'));
        flushSync();

        expect(ft8State.selectedOffset).toBe(700);
        expect(ft8State.effectiveOffset).toBe(700); // pinned, no longer the daemon's pick
    });

    it('toggles to the Channels view without disturbing the pick', async () => {
        ft8Link.onOccupancy(occupancy());
        ft8State.selectedOffset = 1234;
        flushSync();

        render(Ft8Occupancy);
        // Spectrum is the default (operator, 2026-07-13) — the slider renders at once.
        expect(screen.getByLabelText('TX offset (continuous)')).toBeInTheDocument();

        await fireEvent.click(screen.getByRole('button', { name: 'Channels' }));
        flushSync();

        expect(ft8State.occupancyView).toBe('channels');
        expect(screen.queryByLabelText('TX offset (continuous)')).toBeNull();
        expect(ft8State.selectedOffset).toBe(1234); // view choice ≠ offset pick
    });
});

describe('Ft8Occupancy empty states', () => {
    // The trap (dogfood 2026-07-26): the panel is locked to the parity we TRANSMIT
    // in, and the daemon skips occupancy for a slot we transmitted in — so during a
    // run that parity NEVER fills. "Waiting for slot…" implied it was imminent, and
    // the operator waited indefinitely for a reading that could not arrive.
    it('names the TX-parity blind spot instead of implying data is coming', () => {
        ft8Link.onQso({ active: true, role: 'caller', their_call: 'W1ABC', their_period: 'odd' });
        flushSync();
        render(Ft8Occupancy);

        expect(screen.getByText(/can't listen while it transmits/)).toBeInTheDocument();
        expect(screen.getByText(/Pause TX for one slot/)).toBeInTheDocument();
        expect(screen.queryByText(/Waiting for slot/)).toBeNull();
        expect(ft8State.occupancyEmptyReason).toBe('tx-parity');
    });

    it('still says "waiting" when idle and nothing has simply arrived yet', () => {
        render(Ft8Occupancy);
        expect(screen.getByText(/Waiting for slot/)).toBeInTheDocument();
        expect(ft8State.occupancyEmptyReason).toBe('waiting');
    });

    // Occupancy is band-specific: who is using 1200 Hz on 15 m says nothing about
    // 12 m, so a QSY must invalidate the snapshot rather than keep rendering it.
    it('discards a snapshot captured on a different band', () => {
        rig.band = '15m';
        ft8Link.onOccupancy(occupancy());
        flushSync();
        expect(ft8State.hasOccupancy).toBe(true);
        expect(ft8State.suggested).toEqual([1500, 700]);

        rig.band = '12m'; // QSY — the 15 m picture is now meaningless
        flushSync();
        expect(ft8State.hasOccupancy).toBe(false);
        expect(ft8State.occupied).toEqual([]);
        expect(ft8State.suggested).toEqual([]);
    });

    it('keeps the snapshot when the rig band is unknown (CAT off)', () => {
        rig.band = '';
        ft8Link.onOccupancy(occupancy());
        flushSync();
        expect(ft8State.hasOccupancy).toBe(true);
    });
});

describe('Ft8Occupancy per-parity band guard', () => {
    // The two parities are independent snapshots. With ONE shared band tag, the
    // first report on the new band revalidated the other parity's old-band data —
    // and during a CQ run the TX parity is exactly the one that never refreshes.
    it('does not let a fresh parity revalidate the other parity from the old band', () => {
        rig.band = '15m';
        ft8Link.onOccupancy(occupancy()); // even, on 15m
        flushSync();
        expect(ft8State.hasOccupancy).toBe(true);

        rig.band = '12m'; // QSY
        flushSync();
        expect(ft8State.hasOccupancy).toBe(false); // even is now 15m data

        // A 12m report arrives for the ODD parity only.
        const odd = occupancy();
        odd.slot.period = 'odd';
        ft8Link.onOccupancy(odd);
        flushSync();

        // Odd is current; even must STILL be invalid — it is untouched 15m data.
        ft8State.occupancyParity = 'odd';
        expect(ft8State.hasOccupancy).toBe(true);
        ft8State.occupancyParity = 'even';
        expect(ft8State.hasOccupancy).toBe(false);
        expect(ft8State.suggested).toEqual([]);
    });
});

describe('Ft8Occupancy band attribution (dial_mhz)', () => {
    // A report labelled with rig.band ON ARRIVAL is wrong for every report in flight
    // across a QSY, and no downstream test can repair it: publication lags capture by
    // the decode, so neither the report's age nor its distance from the last report
    // shows the capture happened after the band change. The daemon therefore stamps
    // the dial it captured on, and a slot whose dial moved mid-window is never
    // published at all.

    it('attributes a snapshot to the dial it was captured on, not the current band', () => {
        rig.band = '20m';
        const onTwenty = occupancy();
        onTwenty.dial_mhz = 14.074;
        ft8Link.onOccupancy(onTwenty);
        flushSync();
        expect(ft8State.hasOccupancy).toBe(true);

        // QSY to 40m. A report still in the daemon's pipeline lands afterwards — it
        // measured 20m, and says so, however late it arrives.
        rig.band = '40m';
        const inFlight = occupancy();
        inFlight.dial_mhz = 14.074;
        ft8Link.onOccupancy(inFlight);
        flushSync();

        expect(ft8State.hasOccupancy).toBe(false);
        expect(ft8State.suggested).toEqual([]);
        // The load-bearing one: no 20m offset may reach TX by fallback.
        expect(ft8State.effectiveOffset).toBeNull();
    });

    it('accepts a snapshot measured on the band the rig is actually on', () => {
        rig.band = '40m';
        const onForty = occupancy();
        onForty.dial_mhz = 7.074;
        onForty.suggested = [2200];
        ft8Link.onOccupancy(onForty);
        flushSync();

        expect(ft8State.hasOccupancy).toBe(true);
        expect(ft8State.effectiveOffset).toBe(2200);
    });

    it('falls back to the arrival band when the daemon has no dial (CAT off)', () => {
        rig.band = '20m';
        ft8Link.onOccupancy(occupancy()); // no dial_mhz
        flushSync();
        expect(ft8State.hasOccupancy).toBe(true);
        expect(ft8State.suggested).toEqual([1500, 700]);
    });

    it('invalidates a dial-attributed snapshot on QSY without waiting for a report', () => {
        rig.band = '20m';
        const onTwenty = occupancy();
        onTwenty.dial_mhz = 14.074;
        ft8Link.onOccupancy(onTwenty);
        flushSync();
        expect(ft8State.hasOccupancy).toBe(true);

        rig.band = '17m';
        flushSync();
        expect(ft8State.hasOccupancy).toBe(false);
    });

    // Z1 — IN-PANEL STACKING STAYS IN THE PANEL (operator, 2026-08-06: "the
    // Occupancy Panel is overlaying" the RX audio-level meter). The ★ top-pick
    // marker carries z-50 to win against its SIBLING ▼ markers when offsets
    // crowd — but its positioned ancestors created no stacking context, so
    // that z-50 leaked into the page's ROOT context and painted over every
    // fixed overlay below z-50: the meter card (z-30), the drawers (z-20).
    // `isolate` on the marker container is the containment; the ★ KEEPS its
    // z-50 for the sibling contest, which is why this asserts both halves —
    // dropping either one quietly reintroduces a defect (remove isolate → the
    // leak; remove z-50 → ▼ markers can bury the recommendation). jsdom does
    // no painting: this pins the mechanism; the visual outcome is
    // Playwright's when that layer exists.
    it('Z1: contains the top-pick marker stacking inside the spectrum', () => {
        render(Ft8Occupancy);
        ft8Link.onOccupancy(occupancy());
        flushSync();

        const star = screen.getByTitle(/top pick/);
        expect(star.className).toContain('z-50');
        expect(
            star.parentElement!.classList.contains('isolate'),
            'in-panel z must stay in-panel'
        ).toBe(true);
    });
});
