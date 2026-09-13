// FT8 Band Activity enrichment cache — the flag + worked-before lookups behind
// the injected seams. Fail-soft, lookup-once, markWorked.

import { describe, it, expect, beforeEach } from 'vitest';
import {
    ft8EnrichState,
    setFt8Enricher,
    setFt8Dupe,
    resetFt8EnrichForTests,
    FT8_ENRICH_CONCURRENCY,
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

// ADR 0080: the worked-before lookup is asked on the profile's ADIF axis — FT8,
// or MFSK/FT4 for FT4 — and cached per profile, so a late FT8 answer can never
// land in FT4's bucket after the view remounted.
describe('profile-aware worked-before (ADR 0080)', () => {
    it('asks MFSK/FT4 under FT4 and FT8 under FT8, and keeps the answers apart', async () => {
        const asked: string[] = [];
        setFt8Enricher(() => Promise.resolve(null));
        setFt8Dupe((_call, _band, mode, submode) => {
            asked.push(`${mode}/${submode}`);
            return Promise.resolve(mode === 'MFSK');
        });
        ft8EnrichState.observe('K1ABC', '20m', 'FT4');
        ft8EnrichState.observe('K1ABC', '20m', 'FT8');
        await flush();
        expect(asked.sort()).toEqual(['FT8/', 'MFSK/FT4']);
        expect(ft8EnrichState.info('K1ABC', '20m', 'FT4')?.worked).toBe(true);
        expect(ft8EnrichState.info('K1ABC', '20m', 'FT8')?.worked).toBe(false);
        expect(ft8EnrichState.info('K1ABC', '20m')?.worked).toBe(false); // the default profile is FT8
    });

    it('a late FT8 answer resolving after the remount lands in the FT8 bucket only', async () => {
        let resolveFt8!: (v: boolean) => void;
        setFt8Enricher(() => Promise.resolve(null));
        setFt8Dupe((_call, _band, mode) =>
            mode === 'FT8' ? new Promise<boolean>((r) => (resolveFt8 = r)) : Promise.resolve(false)
        );
        ft8EnrichState.observe('K1ABC', '20m', 'FT8'); // in flight under FT8…
        ft8EnrichState.clear(); // …the view remounts on FT4
        ft8EnrichState.observe('K1ABC', '20m', 'FT4');
        await flush();
        expect(ft8EnrichState.info('K1ABC', '20m', 'FT4')?.worked).toBe(false);
        resolveFt8(true); // the stale FT8 lookup lands now
        await flush();
        expect(ft8EnrichState.info('K1ABC', '20m', 'FT4')?.worked).toBe(false);
    });

    it('markWorked files under the given profile', () => {
        ft8EnrichState.markWorked('K1ABC', '20m', 'FT4');
        expect(ft8EnrichState.info('K1ABC', '20m', 'FT4')?.worked).toBe(true);
        expect(ft8EnrichState.info('K1ABC', '20m', 'FT8')?.worked).toBeUndefined();
    });
});

// Operator ruling 2026-09-13 (W-0012 enrichment cap, inbox 2026-09-11 (b)): at
// most FT8_ENRICH_CONCURRENCY (2) /v1/enrich/callsign lookups in flight per
// tab; the worked-before check stays OUTSIDE that budget; stations calling us
// go ahead of plain CQ rows and newer slots ahead of older; a pending lookup
// for a row that has scrolled off (not observed in the next pass) is dropped;
// clear() aborts what is in flight and drops what is pending.
describe('enrichment scheduler: cap, priority, stale-work drop, cancellation', () => {
    type Pending = { call: string; resolve: () => void; signal?: AbortSignal };
    let started: Pending[];
    function controllableEnricher(): void {
        started = [];
        setFt8Enricher(
            (call, signal) =>
                new Promise((resolve) => {
                    started.push({ call, resolve: () => resolve(enrichment()), signal });
                })
        );
    }
    const inFlight = () => started.map((p) => p.call);

    it('holds at most 2 enrich lookups in flight, starts the next as one lands, and never queues the worked-before check', async () => {
        controllableEnricher();
        const dupeCalls: string[] = [];
        setFt8Dupe((call) => {
            dupeCalls.push(call);
            return Promise.resolve(false);
        });

        ft8EnrichState.beginPass();
        for (const c of ['A1AA', 'B1BB', 'C1CC', 'D1DD'])
            ft8EnrichState.observe(c, '20m', 'FT8', { kind: 'cq', slot: 't1' });
        ft8EnrichState.endPass();
        await flush();

        expect(inFlight()).toHaveLength(FT8_ENRICH_CONCURRENCY);
        expect(FT8_ENRICH_CONCURRENCY).toBe(2);
        expect(dupeCalls.sort()).toEqual(['A1AA', 'B1BB', 'C1CC', 'D1DD']); // outside the budget: all at once

        started[0].resolve();
        await flush();
        expect(inFlight()).toHaveLength(3); // one landed, one more started
        started[1].resolve();
        started[2].resolve();
        await flush();
        expect(inFlight()).toHaveLength(4);
        expect(ft8EnrichState.info('A1AA', '20m')?.flag).toBe('🇺🇸');
    });

    it('a station calling us jumps the queue, and a newer slot goes before an older one', async () => {
        controllableEnricher();
        ft8EnrichState.beginPass();
        ft8EnrichState.observe('X1XX', '20m', 'FT8', { kind: 'cq', slot: 't9' }); // fills the cap
        ft8EnrichState.observe('Y1YY', '20m', 'FT8', { kind: 'cq', slot: 't9' });
        ft8EnrichState.observe('OLD1', '20m', 'FT8', { kind: 'cq', slot: 't1' });
        ft8EnrichState.observe('NEW1', '20m', 'FT8', { kind: 'cq', slot: 't5' });
        ft8EnrichState.observe('CALL', '20m', 'FT8', { kind: 'call', slot: 't1' }); // calling us, old slot
        ft8EnrichState.endPass();
        await flush();
        expect(inFlight()).toEqual(['X1XX', 'Y1YY']);

        started[0].resolve();
        await flush();
        expect(inFlight()[2]).toBe('CALL'); // the caller first, though its slot is the oldest
        started[1].resolve();
        await flush();
        expect(inFlight()[3]).toBe('NEW1'); // then the newer slot
        started[2].resolve();
        started[3].resolve();
        await flush();
        expect(inFlight()[4]).toBe('OLD1');
    });

    it('a pending lookup for a row that scrolled off is dropped; one still visible survives', async () => {
        controllableEnricher();
        ft8EnrichState.beginPass();
        ft8EnrichState.observe('X1XX', '20m', 'FT8', { kind: 'cq', slot: 't9' });
        ft8EnrichState.observe('Y1YY', '20m', 'FT8', { kind: 'cq', slot: 't9' });
        ft8EnrichState.observe('GONE', '20m', 'FT8', { kind: 'cq', slot: 't1' });
        ft8EnrichState.observe('STAY', '20m', 'FT8', { kind: 'cq', slot: 't2' });
        ft8EnrichState.endPass();
        await flush();

        ft8EnrichState.beginPass(); // next slot: GONE has scrolled off, STAY is still on screen
        ft8EnrichState.observe('X1XX', '20m', 'FT8', { kind: 'cq', slot: 't9' });
        ft8EnrichState.observe('Y1YY', '20m', 'FT8', { kind: 'cq', slot: 't9' });
        ft8EnrichState.observe('STAY', '20m', 'FT8', { kind: 'cq', slot: 't2' });
        ft8EnrichState.endPass();

        started[0].resolve();
        started[1].resolve();
        await flush();
        expect(inFlight()).toEqual(['X1XX', 'Y1YY', 'STAY']);
        started[2].resolve();
        await flush();
        expect(inFlight()).toHaveLength(3); // GONE never fetched
        expect(ft8EnrichState.info('GONE', '20m')).toBeUndefined();
    });

    it('clear() aborts the lookups in flight and drops the pending ones', async () => {
        controllableEnricher();
        ft8EnrichState.beginPass();
        for (const c of ['A1AA', 'B1BB', 'C1CC'])
            ft8EnrichState.observe(c, '20m', 'FT8', { kind: 'cq', slot: 't1' });
        ft8EnrichState.endPass();
        await flush();
        expect(inFlight()).toHaveLength(2);

        ft8EnrichState.clear();
        expect(started[0].signal?.aborted).toBe(true);
        expect(started[1].signal?.aborted).toBe(true);
        started[0].resolve();
        started[1].resolve();
        await flush();
        expect(inFlight()).toHaveLength(2); // C1CC never started
        expect(ft8EnrichState.info('A1AA', '20m')).toBeUndefined(); // a late answer after clear is not cached
    });
});
