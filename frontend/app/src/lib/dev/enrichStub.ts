// DEV STUB — fake enrichment behind the setEnricher seam, so the card's
// idle → pending → done interaction is provable without the daemon. Replaced
// wholesale by a /v1/enrich/callsign client when the API wiring lands; nothing
// outside main.ts may import this.

import type { Enrichment } from '../operate/enrich.svelte';

// Longest-prefix match over a handful of recognisable prefixes. Grids are the
// entities' rough centres — enough for believable bearing/distance from KH66.
const TABLE: Record<string, Enrichment> = {
    G: {
        country: 'England',
        ccode: 'GB',
        dxcc: 223,
        isNewEntity: false,
        grid: 'IO91',
        name: 'Brian',
    },
    JA: {
        country: 'Japan',
        ccode: 'JP',
        dxcc: 339,
        isNewEntity: false,
        grid: 'PM95',
        name: 'Hiro',
    },
    W: {
        country: 'United States',
        ccode: 'US',
        dxcc: 291,
        isNewEntity: false,
        grid: 'EM48',
        name: 'Chuck',
    },
    K: {
        country: 'United States',
        ccode: 'US',
        dxcc: 291,
        isNewEntity: false,
        grid: 'EM48',
        name: 'Chuck',
    },
    VK: {
        country: 'Australia',
        ccode: 'AU',
        dxcc: 150,
        isNewEntity: true,
        grid: 'QF56',
        name: 'Bruce',
    },
    ZS: {
        country: 'South Africa',
        ccode: 'ZA',
        dxcc: 462,
        isNewEntity: false,
        grid: 'KG43',
        name: 'Pieter',
    },
    '7Q': {
        country: 'Malawi',
        ccode: 'MW',
        dxcc: 440,
        isNewEntity: false,
        grid: 'KH66',
        name: 'Marc',
    },
    VP8: {
        country: 'Falkland Islands',
        ccode: 'FK',
        dxcc: 141,
        isNewEntity: true,
        grid: 'GD18',
        name: 'Bob',
    },
};

const LATENCY_MS = 350;

export function stubEnrich(call: string): Promise<Enrichment | null> {
    let best: Enrichment | null = null;
    let bestLen = 0;
    for (const [prefix, e] of Object.entries(TABLE)) {
        if (call.startsWith(prefix) && prefix.length > bestLen) {
            best = e;
            bestLen = prefix.length;
        }
    }
    return new Promise((resolve) => setTimeout(() => resolve(best), LATENCY_MS));
}
