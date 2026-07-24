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
    fetchStationContext: (): Promise<{
        logbookId: number;
        myGrid: string;
        mapBandColors: Record<string, string>;
    }> => {
        calls.push('context');
        // 40m carries a config override; 20m falls to the default palette.
        return Promise.resolve({
            logbookId: 7,
            myGrid: 'KH74',
            mapBandColors: { '40m': '#101010' },
        });
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
                { id: 1, uuid: 'u1', call: 'G4ABC', gridsquare: 'IO91', band: '20M', ...ts },
                { id: 3, uuid: 'u3', call: 'ZS6DX', gridsquare: 'KG44', band: '40m', ...ts },
                { id: 2, uuid: 'u2', call: 'VU2XYZ', ...ts }, // no location — unplotted
            ],
            nextCursor: null,
        });

        const { container } = render(MapView);

        const plotted = await screen.findByTestId('plotted');
        expect(plotted.textContent).toContain('2 of 3 plotted');
        expect(plotted.textContent).toContain('1 without a location');
        expect(container.querySelectorAll('[data-testid="arc"]')).toHaveLength(2);
        expect(container.querySelector('[data-testid="origin"]')).not.toBeNull();

        // Band colour-coding: 20m (no override) strokes the default palette
        // colour; 40m takes the config override. Legend lists both bands.
        const strokes = [...container.querySelectorAll('[data-testid="arc"] path')].map((p) =>
            p.getAttribute('stroke')
        );
        expect(strokes).toContain('#22c55e'); // default 20m
        expect(strokes).toContain('#101010'); // overridden 40m
        const legend = container.querySelector('[data-testid="legend"]')?.textContent;
        expect(legend).toContain('40m');
        expect(legend).toContain('20m');

        // Stream-then-fetch ordering (the reconnect contract).
        expect(calls.indexOf('stream')).toBeLessThan(calls.indexOf('fetch'));
    });

    it('paints newer arcs on top: a newest-first page renders oldest-first', async () => {
        const ts = nowAdif();
        // The daemon pages newest-first, so items[0] is the NEWEST plotted contact.
        fetchQsoPage.mockResolvedValue({
            kind: 'ok',
            items: [
                { id: 2, uuid: 'u2', call: 'NEWEST', gridsquare: 'IO91', band: '20M', ...ts },
                { id: 1, uuid: 'u1', call: 'OLDEST', gridsquare: 'KG44', band: '20M', ...ts },
            ],
            nextCursor: null,
        });

        const { container } = render(MapView);
        await screen.findByTestId('plotted');

        // SVG paints in document order (last on top), so the newest contact must
        // render LAST — its arc sits over the older ones, not under them.
        const titles = [...container.querySelectorAll('[data-testid="arc"] title')].map(
            (t) => t.textContent ?? ''
        );
        expect(titles).toHaveLength(2);
        expect(titles[0]).toContain('OLDEST');
        expect(titles[1]).toContain('NEWEST');
    });

    it('runs a catch-up refetch when the tab becomes visible again', async () => {
        fetchQsoPage.mockResolvedValue({ kind: 'ok', items: [], nextCursor: null });
        const { unmount } = render(MapView);
        await vi.waitFor(() => expect(fetchQsoPage).toHaveBeenCalled());
        const before = fetchQsoPage.mock.calls.length;

        // jsdom tabs are always "visible"; drive the hidden→visible edge.
        Object.defineProperty(document, 'hidden', { configurable: true, value: false });
        document.dispatchEvent(new Event('visibilitychange'));
        await vi.waitFor(() => expect(fetchQsoPage.mock.calls.length).toBeGreaterThan(before));

        // Going hidden must NOT refetch (nothing to see).
        const midway = fetchQsoPage.mock.calls.length;
        Object.defineProperty(document, 'hidden', { configurable: true, value: true });
        document.dispatchEvent(new Event('visibilitychange'));
        expect(fetchQsoPage.mock.calls.length).toBe(midway);

        // Teardown detaches the listener — no refetch after unmount.
        Object.defineProperty(document, 'hidden', { configurable: true, value: false });
        unmount();
        const after = fetchQsoPage.mock.calls.length;
        document.dispatchEvent(new Event('visibilitychange'));
        expect(fetchQsoPage.mock.calls.length).toBe(after);
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
