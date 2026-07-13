// FT8 Band Activity enrichment cache — the flag + worked-before lookups behind
// the injected seams. Fail-soft, lookup-once, markWorked.

import { describe, it, expect, beforeEach } from 'vitest';
import {
    ft8EnrichState,
    setFt8Enricher,
    setFt8Dupe,
    resetFt8EnrichForTests,
} from './ft8Enrich.svelte';
import type { Enrichment } from './enrich.svelte';

beforeEach(() => {
    resetFt8EnrichForTests();
});

function enrichment(over: Partial<Enrichment> = {}): Enrichment {
    return {
        country: 'United States',
        ccode: 'US',
        dxcc: '291',
        isNewEntity: false,
        grid: 'FN42',
        name: 'Bob',
        qth: '',
        email: '',
        cqZone: '',
        ituZone: '',
        ...over,
    };
}

// Let the observe() lookups (Promise.allSettled chains) settle.
const flush = () => new Promise((r) => setTimeout(r, 0));

describe('ft8EnrichState', () => {
    it('resolves flag + worked into the cache, and is lookup-once', async () => {
        let enrichCalls = 0;
        let dupeCalls = 0;
        setFt8Enricher(() => {
            enrichCalls++;
            return Promise.resolve(enrichment({ isNewEntity: true }));
        });
        setFt8Dupe(() => {
            dupeCalls++;
            return Promise.resolve(true);
        });

        ft8EnrichState.observe('W1ABC', '20m');
        await flush();

        const info = ft8EnrichState.info('W1ABC', '20m');
        expect(info?.flag).toBe('🇺🇸');
        expect(info?.country).toBe('United States');
        expect(info?.isNewEntity).toBe(true);
        expect(info?.worked).toBe(true);

        // A second observe of the same key does not re-fetch.
        ft8EnrichState.observe('W1ABC', '20m');
        await flush();
        expect(enrichCalls).toBe(1);
        expect(dupeCalls).toBe(1);
    });

    it('is band-specific — a new band is a fresh key + lookup', async () => {
        setFt8Enricher(() => Promise.resolve(enrichment()));
        setFt8Dupe((_call, band) => Promise.resolve(band === '20m'));

        ft8EnrichState.observe('W1ABC', '20m');
        ft8EnrichState.observe('W1ABC', '40m');
        await flush();

        expect(ft8EnrichState.info('W1ABC', '20m')?.worked).toBe(true);
        expect(ft8EnrichState.info('W1ABC', '40m')?.worked).toBe(false);
    });

    it('is fail-soft — a rejected lookup leaves that facet undecorated', async () => {
        setFt8Enricher(() => Promise.reject(new Error('down')));
        setFt8Dupe(() => Promise.reject(new Error('down')));

        ft8EnrichState.observe('W1ABC', '20m');
        await flush();

        // Both failed → no flag / worked, but no throw and the row still renders.
        const info = ft8EnrichState.info('W1ABC', '20m');
        expect(info?.flag).toBeUndefined();
        expect(info?.worked).toBeUndefined();
    });

    it('markWorked flips the tint immediately (before any lookup)', () => {
        ft8EnrichState.markWorked('W1ABC', '20m');
        expect(ft8EnrichState.info('W1ABC', '20m')?.worked).toBe(true);
    });

    // Operator bug 2026-07-13: a worked new-entity kept its ★ all session —
    // the lookup-once cache held is_new_entity captured BEFORE the QSO. After
    // a log, the star must drop from the worked call AND every other cached
    // call of the same entity (same dxcc, any band).
    it('markWorked clears the new-entity star for the whole entity', async () => {
        setFt8Enricher((call) =>
            Promise.resolve(
                enrichment({
                    country: 'Tuvalu',
                    ccode: 'TV',
                    dxcc: call.startsWith('T2') ? '282' : '291',
                    isNewEntity: call.startsWith('T2'),
                })
            )
        );
        // Three cached rows: two Tuvalu calls (one on another band) + a US call.
        ft8EnrichState.observe('T22TT', '20m');
        ft8EnrichState.observe('T20AA', '15m');
        ft8EnrichState.observe('W1ABC', '20m');
        await flush();
        expect(ft8EnrichState.info('T22TT', '20m')?.isNewEntity).toBe(true);
        expect(ft8EnrichState.info('T20AA', '15m')?.isNewEntity).toBe(true);

        ft8EnrichState.markWorked('T22TT', '20m');

        expect(ft8EnrichState.info('T22TT', '20m')?.isNewEntity).toBe(false);
        expect(ft8EnrichState.info('T20AA', '15m')?.isNewEntity).toBe(false); // same entity
        expect(ft8EnrichState.info('W1ABC', '20m')?.isNewEntity).toBe(false); // was never new
        expect(ft8EnrichState.info('T22TT', '20m')?.worked).toBe(true);
    });
});
