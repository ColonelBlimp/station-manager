// Render-path test over a fixture: the basemap draws, arcs appear with
// their tooltip labels, and the origin marker tracks the prop. The
// spherical math itself is pinned in engine.test.ts, the zoom/hit math in
// zoom.test.ts — this guards the engine/zoom ↔ component wiring. The
// interaction tests pin the svg's bounding rect to the 960×500 viewBox so
// client coordinates equal viewBox coordinates (jsdom has no layout).

import { describe, it, expect } from 'vitest';
import { render, waitFor } from '@testing-library/svelte';
import { tick } from 'svelte';
import WorldMap from './WorldMap.svelte';
import { createEngine, project } from './engine';

const LILONGWE = { lat: -14.0, lon: 33.8 };
const LONDON = { lat: 51.5, lon: -0.1 };

function pinRect(el: Element): void {
    (el as HTMLElement).getBoundingClientRect = () =>
        ({ left: 0, top: 0, width: 960, height: 500, right: 960, bottom: 500 }) as DOMRect;
}

function pointerMove(el: Element, clientX: number, clientY: number): void {
    el.dispatchEvent(new MouseEvent('pointermove', { clientX, clientY, bubbles: true }));
}

describe('WorldMap', () => {
    it('renders the country basemap with no data', () => {
        const { container } = render(WorldMap, { props: {} });
        const countries = container.querySelectorAll('[data-testid="country"]');
        expect(countries.length).toBeGreaterThan(150);
        expect(container.querySelector('[data-testid="origin"]')).toBeNull();
        expect(container.querySelectorAll('[data-testid="arc"]')).toHaveLength(0);
    });

    it('draws origin and one arc per entry, with the label as a tooltip', () => {
        const { container } = render(WorldMap, {
            props: {
                origin: LILONGWE,
                arcs: [
                    {
                        key: 'q1',
                        from: LILONGWE,
                        to: { lat: 51.5, lon: -0.1 },
                        label: 'G4ABC · IO91 · 8,431 km',
                    },
                    { key: 'q2', from: LILONGWE, to: { lat: 35.7, lon: 139.7 } },
                ],
            },
        });
        expect(container.querySelector('[data-testid="origin"]')).not.toBeNull();
        const arcs = container.querySelectorAll('[data-testid="arc"]');
        expect(arcs).toHaveLength(2);
        expect(arcs[0].querySelector('title')?.textContent).toContain('G4ABC');
        expect(arcs[1].querySelector('title')).toBeNull();
    });

    it('strokes an arc with its given colour, theme accent otherwise', () => {
        const { container } = render(WorldMap, {
            props: {
                origin: LILONGWE,
                arcs: [
                    {
                        key: 'q1',
                        from: LILONGWE,
                        to: { lat: 51.5, lon: -0.1 },
                        color: '#22c55e',
                    },
                    { key: 'q2', from: LILONGWE, to: { lat: 35.7, lon: 139.7 } },
                ],
            },
        });
        const arcs = container.querySelectorAll('[data-testid="arc"]');
        expect(arcs[0].querySelector('path')?.getAttribute('stroke')).toBe('#22c55e');
        expect(arcs[0].querySelector('circle')?.getAttribute('fill')).toBe('#22c55e');
        expect(arcs[1].querySelector('path')?.getAttribute('stroke')).toBe('var(--color-focus)');
    });

    it('draws the grey-line rings only when given a terminator instant', () => {
        const { container: off } = render(WorldMap, { props: {} });
        expect(off.querySelectorAll('[data-testid="night"]')).toHaveLength(0);

        const { container: on } = render(WorldMap, {
            props: { terminator: new Date(Date.UTC(2026, 6, 17, 12)) },
        });
        expect(on.querySelectorAll('[data-testid="night"]')).toHaveLength(3);
    });

    it('zooms on wheel and resets via the Reset view button', async () => {
        const { container } = render(WorldMap, { props: {} });
        const svg = container.querySelector('svg')!;
        const viewport = container.querySelector('[data-testid="viewport"]')!;
        pinRect(svg);

        expect(viewport.getAttribute('transform')).toBe('translate(0 0) scale(1)');
        expect(container.querySelector('[data-testid="reset-view"]')).toBeNull();

        svg.dispatchEvent(
            new WheelEvent('wheel', {
                deltaY: -240,
                clientX: 480,
                clientY: 250,
                bubbles: true,
                cancelable: true,
            })
        );
        await tick();
        expect(viewport.getAttribute('transform')).not.toBe('translate(0 0) scale(1)');

        const reset = container.querySelector<HTMLButtonElement>('[data-testid="reset-view"]');
        expect(reset).not.toBeNull();
        reset!.click();
        await tick();
        expect(viewport.getAttribute('transform')).toBe('translate(0 0) scale(1)');
        expect(container.querySelector('[data-testid="reset-view"]')).toBeNull();
    });

    it('swaps in the 50m basemap once zoomed past the LOD threshold', async () => {
        const { container } = render(WorldMap, { props: {} });
        const svg = container.querySelector('svg')!;
        pinRect(svg);

        const loCount = container.querySelectorAll('[data-testid="country"]').length;
        // One deep wheel: exp(0.002 · 1000) ≈ 7.4× — past LOD_ZOOM (3).
        svg.dispatchEvent(
            new WheelEvent('wheel', {
                deltaY: -1000,
                clientX: 480,
                clientY: 250,
                bubbles: true,
                cancelable: true,
            })
        );
        await tick();
        // The 50m set (241 countries vs ~177 at 110m) lazy-loads, then draws.
        await waitFor(() => {
            expect(container.querySelectorAll('[data-testid="country"]').length).toBeGreaterThan(
                loCount + 30
            );
        });
    });

    it('shows a tooltip with every stacked contact near an endpoint, hides it away', async () => {
        const { container } = render(WorldMap, {
            props: {
                origin: LILONGWE,
                arcs: [
                    {
                        key: 'q1',
                        from: LILONGWE,
                        to: LONDON,
                        label: 'G4ABC · IO91 · 8,431 km',
                        color: '#22c55e',
                    },
                    // Same far end → stacked endpoint, must list both.
                    { key: 'q2', from: LILONGWE, to: LONDON, label: 'M0XYZ · IO91' },
                    { key: 'q3', from: LILONGWE, to: { lat: 35.7, lon: 139.7 }, label: 'JA1AAA' },
                ],
            },
        });
        const svg = container.querySelector('svg')!;
        pinRect(svg);

        // With the rect pinned 1:1, client coords == viewBox == projected px.
        const [ex, ey] = project(createEngine(960, 500), LONDON)!;
        pointerMove(svg, ex + 2, ey - 2);
        await tick();

        const tipText = container.querySelector('[data-testid="map-tooltip"]')?.textContent;
        expect(tipText).toContain('G4ABC');
        expect(tipText).toContain('M0XYZ');
        expect(tipText).not.toContain('JA1AAA');

        pointerMove(svg, ex + 200, ey + 100);
        await tick();
        expect(container.querySelector('[data-testid="map-tooltip"]')).toBeNull();
    });
});
