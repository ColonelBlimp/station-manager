// FT8 Band Activity enrichment — per-session decoration cache for CQ callsigns.
// For each CQ call in the feed it resolves a country flag (+ NEW-entity + op
// name/grid) via the enricher and a worked-before flag via the dupe lookup, so
// the Band Activity panel can prepend a flag and tint the row.
//
// Per ADR 0045 the two lookups the shipping module imported directly
// (enrichCallsign, fetchContestDupe) + the config it read (default logbook) are
// INJECTED here: main.ts wires an enricher (→ apiEnrich) and a dupe fn (which
// closes over the logbook id). Progressive + fail-soft + lookup-once, exactly
// like shipping — enrichment decorates, it never blocks.

import { ccodeToFlag } from '../utils/flag';
import type { Enrichment } from './enrich.svelte';

/** Decoration for one CQ callsign; fields fill in independently as lookups land. */
export interface Ft8CallInfo {
    /** true = worked on this band+mode (dupe), false = not, undefined = pending. */
    worked?: boolean;
    /** Flag emoji, or '' when the country is unknown / not alpha-2. undefined = pending. */
    flag?: string;
    /** Country name for the flag's hover tooltip. undefined when unknown/pending. */
    country?: string;
    /** true = this DXCC entity has never been worked (a "new one"). undefined = pending. */
    isNewEntity?: boolean;
    /** Operator name (enrichment). undefined = pending; '' when none. */
    opName?: string;
    /** Operator's locator — a fallback far-end for the bearing when the CQ line
     *  carries no grid. undefined/'' when unknown. */
    grid?: string;
}

// FT8 is itself an ADIF main mode, so the dupe axis is band + "FT8".
const FT8_MODE = 'FT8';

function cacheKey(call: string, band: string): string {
    return `${call}|${band}`;
}

/*
    Injected seams (ADR 0045). enricher resolves the flag/country/new/name/grid;
    dupe answers "worked this call on this band already?" (null = unknown). Both
    wired in main.ts; unwired = no decoration (fail-soft).
*/
export type Ft8Enricher = (call: string) => Promise<Enrichment | null>;
export type Ft8Dupe = (call: string, band: string, mode: string) => Promise<boolean | null>;

let enricher: Ft8Enricher | null = null;
let dupe: Ft8Dupe | null = null;

export function setFt8Enricher(fn: Ft8Enricher): void {
    enricher = fn;
}

export function setFt8Dupe(fn: Ft8Dupe): void {
    dupe = fn;
}

class Ft8EnrichState {
    /** Decoration cache keyed by `call|band`. Reassigned (not mutated) on each
     *  resolution so $state notifies template reads of info(...). */
    cache: Record<string, Ft8CallInfo> = $state({});

    // Keys with a lookup in flight — dedupes concurrent observes for the same
    // call+band. Non-reactive plumbing; gates work, never rendered.
    #inFlight = new Set<string>();

    /** Decoration for a CQ call on a band, or undefined if not yet looked up. */
    info(call: string, band: string): Ft8CallInfo | undefined {
        return this.cache[cacheKey(call, band)];
    }

    /**
     * Kick off (at most once) the flag + worked-before lookups. Idempotent: a
     * cached or in-flight key is a no-op, so the panel can call it for every
     * visible CQ row each slot without re-fetching.
     */
    observe(call: string, band: string): void {
        const key = cacheKey(call, band);
        if (this.cache[key] !== undefined || this.#inFlight.has(key)) return;
        this.#inFlight.add(key);

        const merge = (patch: Ft8CallInfo): void => {
            const prev = this.cache[key] ?? {};
            this.cache = { ...this.cache, [key]: { ...prev, ...patch } };
        };

        const flagLookup = enricher
            ? enricher(call)
                  .then((e) => {
                      if (e)
                          merge({
                              flag: ccodeToFlag(e.ccode),
                              country: e.country,
                              isNewEntity: e.isNewEntity ?? undefined,
                              opName: e.name,
                              grid: e.grid,
                          });
                  })
                  .catch(() => {
                      /* fail-soft: no flag */
                  })
            : Promise.resolve();

        // Worked-before needs a real band; a blank band would 400 the daemon.
        const dupeLookup =
            dupe && band !== ''
                ? dupe(call, band, FT8_MODE)
                      .then((w) => {
                          if (w !== null) merge({ worked: w });
                      })
                      .catch(() => {
                          /* fail-soft: worked stays unknown */
                      })
                : Promise.resolve();

        void Promise.allSettled([flagLookup, dupeLookup]).then(() => {
            this.#inFlight.delete(key);
        });
    }

    /**
     * Mark a call worked on a band (dupe) so the row grays out immediately —
     * called when an FT8 QSO logs, since the lookup-once cache would otherwise
     * hold the stale worked:false captured before the contact. Also re-kicks
     * observe() so a station worked without ever showing as a CQ row still gets
     * its flag; the worked:true merge wins.
     */
    markWorked(call: string, band: string): void {
        const c = call.trim().toUpperCase();
        if (c === '' || band === '') return;
        this.observe(c, band);
        const key = cacheKey(c, band);
        const prev = this.cache[key] ?? {};
        this.cache = { ...this.cache, [key]: { ...prev, worked: true } };
    }

    /** Drop all decorations — called on FT8-view close so a re-open starts clean. */
    clear(): void {
        this.cache = {};
        this.#inFlight.clear();
    }
}

export const ft8EnrichState = new Ft8EnrichState();

/** Test seam — clear cache + injected lookups between cases. */
export function resetFt8EnrichForTests(): void {
    enricher = null;
    dupe = null;
    ft8EnrichState.clear();
}
