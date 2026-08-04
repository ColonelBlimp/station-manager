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

import { setOperatingBands } from '../operate/rig.svelte';
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

/*
    Band filter (dogfood-inbox 2026-08-01). A select, defaulting to "All", whose
    options are the station's CONFIGURED operating bands.

    WHY THE LIST IS THE STATION'S BANDS AND NOT THE WINDOW'S. Offering only the
    bands present in the current window would make the control flicker as QSOs
    age out of the window and would hide a band the operator works — the list
    describes the STATION, which is stable, not the current six hours. It is the
    same `station.operating_bands` that already drives the Phone/CW band grid and
    the FT8 buttons, so a station that skips 160/60/30 never sees them anywhere.

    WHY IT IS NOT PERSISTED, unlike the grey-line toggle beside it. A filter that
    survives into the next session opens the map on an apparently empty world
    with no indication why — the "why is my map broken" trap. Grey line is safe
    to persist because it adds an overlay; this one REMOVES contacts.

    THE NEAREST CONFUSABLE STATE (B2): a filtered-empty map versus a broken one.
    The count line keeps saying how many QSOs the window holds, so "0 of 46
    plotted" reads as a filter doing its job rather than a failure.
*/

describe('MapView band filter', () => {
    beforeEach(() => {
        // The station's configured bands — the real store, as main.ts fills it
        // from station.operating_bands. 15m is configured but unworked in this
        // window; 160m is not configured at all.
        setOperatingBands(['15m', '20m', '40m']);
    });

    const twoBandPage = (): void => {
        const ts = nowAdif();
        fetchQsoPage.mockResolvedValue({
            kind: 'ok',
            items: [
                { id: 1, uuid: 'u1', call: 'G4ABC', gridsquare: 'IO91', band: '20M', ...ts },
                { id: 2, uuid: 'u2', call: 'ZS6DX', gridsquare: 'KG44', band: '40m', ...ts },
                { id: 3, uuid: 'u3', call: 'VK3XX', gridsquare: 'QF22', band: '20m', ...ts },
            ],
            nextCursor: null,
        });
    };

    it('B1: defaults to All and plots the whole window', async () => {
        twoBandPage();
        const { container } = render(MapView);
        await screen.findByTestId('plotted');

        const sel = screen.getByLabelText<HTMLSelectElement>('Band');
        expect(sel.value).toBe('');
        expect(container.querySelectorAll('[data-testid="arc"]')).toHaveLength(3);
    });

    it('B2: selecting a band plots only that band, and says what was hidden', async () => {
        twoBandPage();
        const { container } = render(MapView);
        await screen.findByTestId('plotted');

        const sel = screen.getByLabelText<HTMLSelectElement>('Band');
        sel.value = '40m';
        sel.dispatchEvent(new Event('change', { bubbles: true }));
        await Promise.resolve();

        expect(container.querySelectorAll('[data-testid="arc"]')).toHaveLength(1);
        // The window's size is still stated, so an empty result is legible as a
        // filter rather than a fault.
        expect(screen.getByTestId('plotted').textContent).toContain('of 3');
    });

    it('B3/B4: the options are the configured bands, including ones absent from the window', async () => {
        twoBandPage();
        render(MapView);
        await screen.findByTestId('plotted');

        const opts = [...screen.getByLabelText<HTMLSelectElement>('Band').options].map(
            (o) => o.value
        );
        expect(opts[0]).toBe(''); // All
        // 15m is configured but has no QSO in this window — still offered.
        expect(opts).toContain('15m');
        expect(opts).toContain('20m');
        expect(opts).toContain('40m');
        // 160m is NOT in this station's configured list and must not appear.
        expect(opts).not.toContain('160m');
    });

    it('B5: the legend describes what is plotted, not the whole window', async () => {
        twoBandPage();
        const { container } = render(MapView);
        await screen.findByTestId('plotted');

        const sel = screen.getByLabelText<HTMLSelectElement>('Band');
        sel.value = '40m';
        sel.dispatchEvent(new Event('change', { bubbles: true }));
        await Promise.resolve();

        const legend = container.querySelector('[data-testid="legend"]')?.textContent ?? '';
        expect(legend).toContain('40m');
        expect(legend).not.toContain('20m');
    });

    it('B6: a band with no contacts plots nothing but still reports the window', async () => {
        twoBandPage();
        const { container } = render(MapView);
        await screen.findByTestId('plotted');

        const sel = screen.getByLabelText<HTMLSelectElement>('Band');
        sel.value = '15m';
        sel.dispatchEvent(new Event('change', { bubbles: true }));
        await Promise.resolve();

        expect(container.querySelectorAll('[data-testid="arc"]')).toHaveLength(0);
        expect(screen.getByTestId('plotted').textContent).toContain('of 3');
    });
});
