// Render-path test over a fixture: the basemap draws, arcs appear with
// their tooltip labels, and the origin marker tracks the prop. The
// spherical math itself is pinned in engine.test.ts — this guards the
// engine ↔ component wiring.

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import WorldMap from './WorldMap.svelte';

const LILONGWE = { lat: -14.0, lon: 33.8 };

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
});
