// Smoke test over the full view wiring: station context resolves the
// logbook + origin, the event stream opens BEFORE the windowed fetch
// (documented reconnect contract), rows inside the window become arcs, and
// a qso.stored event for our logbook triggers the debounced head-refresh.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import type { LogEventHandlers } from '../api/log-events';

const nowAdif = (): { qso_date: string; time_on: string } => {
    const d = new Date();
    const pad = (n: number): string => String(n).padStart(2, '0');
    return {
        qso_date: `${d.getUTCFullYear()}${pad(d.getUTCMonth() + 1)}${pad(d.getUTCDate())}`,
        time_on: `${pad(d.getUTCHours())}${pad(d.getUTCMinutes())}`,
    };
};

const calls: string[] = [];
let handlers: LogEventHandlers | null = null;
const closeSpy = vi.fn();
const fetchQsoPage = vi.fn();

vi.mock('../api/seams', () => ({
    fetchStationContext: (): Promise<{ logbookId: number; myGrid: string }> => {
        calls.push('context');
        return Promise.resolve({ logbookId: 7, myGrid: 'KH74' });
    },
}));
vi.mock('../api/log-events', () => ({
    openLogEvents: (h: LogEventHandlers): (() => void) => {
        calls.push('stream');
        handlers = h;
        return closeSpy;
    },
}));
vi.mock('../api/logbooks', () => ({
    fetchQsoPage: (...args: unknown[]): unknown => {
        calls.push('fetch');
        return fetchQsoPage(...args) as unknown;
    },
}));

import MapView from './MapView.svelte';

beforeEach(() => {
    calls.length = 0;
    handlers = null;
    fetchQsoPage.mockReset();
});

describe('MapView', () => {
    it('opens the stream before fetching, plots the window, counts the grid-less row', async () => {
        const ts = nowAdif();
        fetchQsoPage.mockResolvedValue({
            kind: 'ok',
            items: [
                { id: 1, uuid: 'u1', call: 'G4ABC', gridsquare: 'IO91', ...ts },
                { id: 2, uuid: 'u2', call: 'VU2XYZ', ...ts }, // no location — unplotted
            ],
            nextCursor: null,
        });

        const { container } = render(MapView);

        const plotted = await screen.findByTestId('plotted');
        expect(plotted.textContent).toContain('1 of 2 plotted');
        expect(plotted.textContent).toContain('1 without a location');
        expect(container.querySelectorAll('[data-testid="arc"]')).toHaveLength(1);
        expect(container.querySelector('[data-testid="origin"]')).not.toBeNull();

        // Stream-then-fetch ordering (the reconnect contract).
        expect(calls.indexOf('stream')).toBeLessThan(calls.indexOf('fetch'));
    });

    it('re-fetches on a qso.stored event for our logbook only', async () => {
        vi.useFakeTimers();
        try {
            const ts = nowAdif();
            fetchQsoPage.mockResolvedValue({ kind: 'ok', items: [], nextCursor: null });
            render(MapView);
            await vi.waitFor(() => expect(handlers).not.toBeNull());
            await vi.waitFor(() => expect(fetchQsoPage).toHaveBeenCalled());
            const before = fetchQsoPage.mock.calls.length;

            handlers!.onQsoChanged('qso.stored', { qso_id: 9, logbook_id: 999 }); // other book
            await vi.advanceTimersByTimeAsync(400);
            expect(fetchQsoPage.mock.calls.length).toBe(before);

            handlers!.onQsoChanged('qso.stored', { qso_id: 10, logbook_id: 7 });
            fetchQsoPage.mockResolvedValue({
                kind: 'ok',
                items: [{ id: 10, uuid: 'u10', call: 'ZS6DX', gridsquare: 'KG44', ...ts }],
                nextCursor: null,
            });
            await vi.advanceTimersByTimeAsync(400);
            expect(fetchQsoPage.mock.calls.length).toBeGreaterThan(before);
        } finally {
            vi.useRealTimers();
        }
    });
});
