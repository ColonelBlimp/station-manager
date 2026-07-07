// DEV STUB — fake worked-before history behind the setHistory seam, so the
// Worked panel's lookup/auto-open interaction is provable without the daemon.
// Replaced by a contact-history API client when the /v1 wiring lands; nothing
// outside main.ts may import this.

import type { WorkedQso } from '../operate/worked.svelte';

// Exact-call match (history is per station, unlike the enrichment prefixes).
// VK2ABC + G4ABC pair with the enrichment stub's prefixes so one test call
// exercises both cards.
const HISTORY: Record<string, WorkedQso[]> = {
    VK2ABC: [
        {
            date: '2026-03-14',
            timeOn: '18:42',
            band: '20m',
            mode: 'SSB',
            rstSent: '59',
            rstRcvd: '57',
            name: 'Bruce',
        },
        {
            date: '2025-11-02',
            timeOn: '06:15',
            band: '40m',
            mode: 'CW',
            rstSent: '599',
            rstRcvd: '599',
            name: 'Bruce',
        },
    ],
    G4ABC: [
        {
            date: '2026-06-21',
            timeOn: '20:03',
            band: '17m',
            mode: 'SSB',
            rstSent: '58',
            rstRcvd: '55',
            name: 'Brian',
        },
    ],
};

const LATENCY_MS = 250;

export function stubHistory(call: string): Promise<WorkedQso[]> {
    return new Promise((resolve) => setTimeout(() => resolve(HISTORY[call] ?? []), LATENCY_MS));
}
