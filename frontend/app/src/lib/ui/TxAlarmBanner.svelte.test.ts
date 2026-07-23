// Stuck-TX banner: the Re-check affordance (2026-07-21 incident).
//
// The load-bearing assertion is the NEGATIVE one — re-checking must not retire
// the banner. The daemon cannot clear a TX alarm without positive evidence from
// the rig, and a button that hid the warning on its own would be claiming a
// safety verdict the browser has no basis for.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import TxAlarmBanner from './TxAlarmBanner.svelte';
import { rig, catLink } from '../operate/rig.svelte';

function mockFetch(status: number, body: unknown): ReturnType<typeof vi.fn> {
    const spy = vi.fn(() =>
        Promise.resolve(
            new Response(body === null ? null : JSON.stringify(body), {
                status,
                headers: { 'Content-Type': 'application/json' },
            })
        )
    );
    vi.stubGlobal('fetch', spy);
    return spy;
}

beforeEach(() => {
    // Raise the alarm through the SSE handler, the way the daemon does.
    catLink.onTxAlarm({ active: true, code: 'tx_still_keyed' });
    flushSync();
});

afterEach(() => {
    catLink.onTxAlarm({ active: false, code: '' });
    rig.txAlarmDismissed = false;
    vi.restoreAllMocks();
});

describe('TxAlarmBanner re-check', () => {
    it('POSTs to the recheck endpoint and reports that it asked', async () => {
        const spy = mockFetch(200, { asked: true, alarm_active: true });
        render(TxAlarmBanner);

        const button = screen.getByRole('button', { name: /re-check/i });
        button.click();
        await vi.waitFor(() => expect(spy).toHaveBeenCalledTimes(1));

        const [url, init] = spy.mock.calls[0] as [string, RequestInit];
        expect(url).toBe('/v1/rig/tx/recheck');
        expect(init.method).toBe('POST');
        await screen.findByText(/asked the rig/i);
    });

    it('does NOT clear the banner on a successful re-check', async () => {
        mockFetch(200, { asked: true, alarm_active: true });
        render(TxAlarmBanner);

        screen.getByRole('button', { name: /re-check/i }).click();
        await screen.findByText(/asked the rig/i);

        // Still standing: only the daemon's tx-alarm clear may retire it.
        expect(screen.getByText(/CHECK YOUR RADIO/)).toBeTruthy();
        expect(rig.txAlarmActive).toBe(true);
    });

    it('retires the banner only when the daemon publishes the clear', async () => {
        mockFetch(200, { asked: true, alarm_active: true });
        render(TxAlarmBanner);
        screen.getByRole('button', { name: /re-check/i }).click();
        await screen.findByText(/asked the rig/i);

        // The rig answered "in RX"; the daemon confirms and clears.
        catLink.onTxAlarm({ active: false, code: '' });
        flushSync();
        expect(screen.queryByText(/CHECK YOUR RADIO/)).toBeNull();
    });

    it('disables re-check for a rig that cannot report transmit state', async () => {
        mockFetch(501, {
            code: 'rig_tx_recheck_unsupported',
            message: 'this rig cannot be asked for its transmit state',
        });
        render(TxAlarmBanner);

        const button = screen.getByRole('button', { name: /re-check/i });
        button.click();
        await screen.findByText(/cannot report its transmit state/i);
        expect((button as HTMLButtonElement).disabled).toBe(true);
        // The warning itself is untouched by an unsupported re-check.
        expect(screen.getByText(/CHECK YOUR RADIO/)).toBeTruthy();
    });

    it('reports a transport failure without touching the alarm', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('network down')))
        );
        render(TxAlarmBanner);

        screen.getByRole('button', { name: /re-check/i }).click();
        await screen.findByText(/couldn't reach the daemon/i);
        expect(rig.txAlarmActive).toBe(true);
    });

    it('Dismiss still hides locally without clearing daemon state', () => {
        render(TxAlarmBanner);
        screen.getByRole('button', { name: /dismiss/i }).click();
        flushSync();

        expect(screen.queryByText(/CHECK YOUR RADIO/)).toBeNull();
        expect(rig.txAlarmActive).toBe(true); // daemon still knows
    });
});
