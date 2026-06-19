/**
 * Enrichment state — latest `/v1/enrich/callsign` response and the
 * operator's short/long path choice.
 *
 * Lives at the SPA level (per ADR 0004) — the daemon owns the cache
 * and chain orchestration; this module is just the latest-result
 * holder + the path-selection UX state. Persistence tier: in-memory
 * only. A half-completed lookup is per-session; surviving a refresh
 * would mislead more than help.
 *
 * Reactivity: every field used by a template, `$derived`, or
 * `$effect` is `$state`. The class doubles as the read source for
 * `CountryPanel.svelte` (template binds) and for `QsoPanel.submitQso`
 * (which reads `activeBearing` to populate ADIF ANT_AZ).
 */

import { configState } from './config.svelte';
import { pathInfo as computePaths, type PathInfo } from '../utils/bearing';
import type { EnrichmentResult } from '../api/enrichment';

export type Path = 'short' | 'long';

class EnrichmentState {
    /**
     * Latest `/v1/enrich/callsign` response, or `null` when no Tab has
     * run yet (or after `clear()` fires post-submit). Templates branch
     * on `result === null` to render the empty-panel state.
     */
    result: EnrichmentResult | null = $state(null);

    /**
     * Operator-selected path for distance / bearing display and for
     * ADIF ANT_AZ on submit. Resets to `'short'` on each new result —
     * a fresh Tab means a fresh QSO setup, not a carryover from the
     * previous one.
     */
    path: Path = $state('short');

    /**
     * Short + long path bearings/distances between the operator's
     * grid (`configState.loggingStation.myGridsquare`) and the
     * contacted station's grid (`result.station.gridsquare`). Null
     * when either grid is absent — typically the case before any
     * lookup has run, or for stations whose record has no grid.
     */
    paths: PathInfo | null = $derived.by(() => {
        const myGrid = configState.loggingStation.myGridsquare;
        const remoteGrid = this.result?.station?.gridsquare ?? '';
        if (myGrid === '' || remoteGrid === '') return null;
        return computePaths(myGrid, remoteGrid);
    });

    /**
     * Bearing string for the currently-selected path, rounded to the
     * nearest whole degree — ANT_AZ is a rotator heading, so sub-degree
     * precision is spurious. Empty string when paths is null — the form-
     * level ADIF emitter omits ANT_AZ when antAz is empty, so the
     * "no grid available" case naturally produces a record with no
     * ANT_AZ rather than a fabricated zero.
     */
    activeBearing: string = $derived.by(() => {
        if (this.paths === null) return '';
        const b = this.path === 'short' ? this.paths.shortPathBearing : this.paths.longPathBearing;
        return b.toFixed(0);
    });

    /**
     * Distance string for the currently-selected path, in km, no
     * unit suffix. Empty when paths is null. Snapshotted into the
     * session-QSO record at submit time so the SessionPanel column
     * reflects what was true at the moment of logging — once the
     * next Tab fires, this value would otherwise reset.
     */
    activeDistanceKm: string = $derived.by(() => {
        if (this.paths === null) return '';
        const d =
            this.path === 'short' ? this.paths.shortPathDistanceKm : this.paths.longPathDistanceKm;
        return String(d);
    });

    /**
     * setResult — called by `QsoPanel.handleEnrich` after a successful
     * `/v1/enrich/callsign` response. Path always resets to 'short'.
     */
    setResult(r: EnrichmentResult): void {
        this.result = r;
        this.path = 'short';
    }

    /**
     * clear — called by `QsoPanel.submitQso` after a successful QSO
     * submit. The country panel returns to its empty state per the
     * UX rule that each QSO is a clean slate.
     */
    clear(): void {
        this.result = null;
        this.path = 'short';
    }

    /**
     * resultForCallsign returns the latest result ONLY when it belongs to the
     * given callsign, else null. submitQso uses this so a stale result from a
     * previously-looked-up callsign can never contaminate the QSO being logged
     * (review 2026-06-04 H1) — the definitive guard, independent of the
     * lookup-token race handling in QsoPanel.runLookup. Comparison is
     * case-insensitive; an empty target or a result for a different call
     * yields null.
     */
    resultForCallsign(call: string): EnrichmentResult | null {
        const want = call.trim().toUpperCase();
        if (want === '' || this.result?.callsign?.toUpperCase() !== want) {
            return null;
        }
        return this.result;
    }
}

export const enrichmentState = new EnrichmentState();
