import { describe, it, expect, beforeEach } from 'vitest';
import { flushSync } from 'svelte';
import { enrichmentState } from './enrichment.svelte';
import { configState } from './config.svelte';
import type { EnrichmentResult } from '../api/enrichment';

/**
 * enrichmentState — latest-result holder + path selection.
 *
 * Verifies the reactive contract that CountryPanel relies on:
 *   - setResult populates the latest result and resets path to short.
 *   - clear empties the result and resets path.
 *   - paths is null when either grid is missing; populated when both
 *     are present.
 *   - activeBearing reflects the selected path's bearing.
 *
 * The module-level singleton is shared across tests; beforeEach resets
 * it to a known empty state.
 */

const makeResult = (overrides: Partial<EnrichmentResult['station']> = {}): EnrichmentResult => ({
    callsign: '7Q7EB',
    country: { name: 'Malawi', prefix: '7Q', time_offset: '2h 0m', is_new_entity: true },
    station: {
        call: '7Q7EB',
        name: 'ELAYI BANDA',
        qth: 'LILONGWE',
        gridsquare: 'KH66UB',
        ...overrides,
    },
    country_source: 'hamnut',
    station_source: 'qrzlookupservice',
});

describe('enrichmentState', () => {
    beforeEach(() => {
        enrichmentState.clear();
        configState.loggingStation.myGridsquare = '';
        flushSync();
    });

    describe('setResult / clear', () => {
        it('stores the latest result', () => {
            const r = makeResult();
            enrichmentState.setResult(r);
            flushSync();
            expect(enrichmentState.result).toStrictEqual(r);
        });

        it('resets path to short on every setResult', () => {
            enrichmentState.setResult(makeResult());
            enrichmentState.path = 'long';
            enrichmentState.setResult(makeResult());
            flushSync();
            expect(enrichmentState.path).toBe('short');
        });

        it('clear empties the result and resets path', () => {
            enrichmentState.setResult(makeResult());
            enrichmentState.path = 'long';
            enrichmentState.clear();
            flushSync();
            expect(enrichmentState.result).toBeNull();
            expect(enrichmentState.path).toBe('short');
        });
    });

    describe('paths derivation', () => {
        it('is null when no result is set', () => {
            flushSync();
            expect(enrichmentState.paths).toBeNull();
        });

        it('is null when station has no gridsquare', () => {
            configState.loggingStation.myGridsquare = 'KH78an';
            enrichmentState.setResult(makeResult({ gridsquare: '' }));
            flushSync();
            expect(enrichmentState.paths).toBeNull();
        });

        it('is null when operator has no gridsquare', () => {
            configState.loggingStation.myGridsquare = '';
            enrichmentState.setResult(makeResult());
            flushSync();
            expect(enrichmentState.paths).toBeNull();
        });

        it('is populated when both grids are present', () => {
            configState.loggingStation.myGridsquare = 'KH78an';
            enrichmentState.setResult(makeResult({ gridsquare: 'KH66UB' }));
            flushSync();
            expect(enrichmentState.paths).not.toBeNull();
            // Sanity: short path between two close Malawi grids is in the
            // tens-to-hundreds-of-km range, not >10000.
            expect(enrichmentState.paths!.shortPathDistanceKm).toBeGreaterThan(0);
            expect(enrichmentState.paths!.shortPathDistanceKm).toBeLessThan(1000);
        });
    });

    describe('activeBearing', () => {
        it('is empty string when paths is null', () => {
            flushSync();
            expect(enrichmentState.activeBearing).toBe('');
        });

        it('reflects short-path bearing when path is short', () => {
            configState.loggingStation.myGridsquare = 'KH78an';
            enrichmentState.setResult(makeResult({ gridsquare: 'KH66UB' }));
            flushSync();
            const expected = enrichmentState.paths!.shortPathBearing.toFixed(1);
            expect(enrichmentState.activeBearing).toBe(expected);
        });

        it('reflects long-path bearing when path is long', () => {
            configState.loggingStation.myGridsquare = 'KH78an';
            enrichmentState.setResult(makeResult({ gridsquare: 'KH66UB' }));
            enrichmentState.path = 'long';
            flushSync();
            const expected = enrichmentState.paths!.longPathBearing.toFixed(1);
            expect(enrichmentState.activeBearing).toBe(expected);
        });
    });
});
