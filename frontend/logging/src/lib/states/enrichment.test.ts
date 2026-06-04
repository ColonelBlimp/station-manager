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

    describe('resultForCallsign (H1)', () => {
        it('returns the result when the callsign matches (case-insensitive)', () => {
            const r = makeResult();
            enrichmentState.setResult(r);
            flushSync();
            expect(enrichmentState.resultForCallsign('7Q7EB')).toStrictEqual(r);
            expect(enrichmentState.resultForCallsign('7q7eb')).toStrictEqual(r);
        });

        it('returns null when the callsign does not match (stale-result guard)', () => {
            enrichmentState.setResult(makeResult()); // callsign 7Q7EB
            flushSync();
            expect(enrichmentState.resultForCallsign('M0CMC')).toBeNull();
        });

        it('returns null with no result, or for an empty target', () => {
            expect(enrichmentState.resultForCallsign('7Q7EB')).toBeNull(); // cleared in beforeEach
            enrichmentState.setResult(makeResult());
            flushSync();
            expect(enrichmentState.resultForCallsign('')).toBeNull();
        });
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

        it('recomputes when only myGridsquare changes after the first read (M3)', () => {
            // paths is a $derived over configState.loggingStation.myGridsquare,
            // and applyResponse can re-hydrate that field (daemon-normalised
            // grid) AFTER the panel first read paths. myGridsquare must be
            // $state for the derived to track it — a plain field would leave
            // paths cached at the stale distance. Read paths once to lock the
            // initial computation, THEN change only the operator's grid.
            enrichmentState.setResult(makeResult({ gridsquare: 'KH66UB' })); // Malawi
            configState.loggingStation.myGridsquare = 'KH78an'; // also Malawi (close)
            flushSync();
            const firstDistance = enrichmentState.paths?.shortPathDistanceKm;
            expect(firstDistance).toBeGreaterThan(0);

            configState.loggingStation.myGridsquare = 'IO91wm'; // UK (far)
            flushSync();
            const secondDistance = enrichmentState.paths?.shortPathDistanceKm;
            expect(secondDistance).toBeGreaterThan(0);
            expect(secondDistance).not.toBe(firstDistance);
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
