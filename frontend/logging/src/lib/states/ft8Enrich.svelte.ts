/**
 * FT8 Band Activity enrichment — per-session decoration cache for CQ callsigns.
 *
 * Purely SPA-side, reusing existing daemon endpoints (no daemon change, so the
 * narrow FT8 subsystem stays decoupled): for each CQ callsign in the live feed
 * it asks `/v1/enrich/callsign` (→ country flag) and `/v1/contest-dupe` (→ have
 * I worked this call on the current band+mode). The Band Activity panel reads
 * the result to prepend a flag and tint the row.
 *
 * Design notes:
 *   - **Keyed by `call|band`.** Worked-before is band+mode-specific (a station
 *     worked on 20 m is still fresh on 40 m), so a band change is a new key and
 *     a fresh lookup. The flag is band-independent but re-resolving it on the
 *     rare band change is negligible (daemon-cached).
 *   - **Progressive + fail-soft.** The row renders immediately; the flag and the
 *     worked tint each appear when their lookup resolves. Any failure (network,
 *     hamnut down, validation) simply leaves that facet undecorated — never an
 *     error to the operator. This is "enrichment degrades, never blocks logging"
 *     applied to a display feed.
 *   - **Lookup-once.** A key is fetched at most once per session (deduped while
 *     in-flight, then cached on any resolution). CQ stations recur slot-to-slot,
 *     so steady-state lookup volume is ~zero. A *total* failure (both lookups
 *     error, nothing cached) retries on the next observation; a partial success
 *     is treated as terminal for the session (a transient dupe-endpoint blip can
 *     leave `worked` blank until a band change — acceptable for a display aid).
 *
 * Persistence tier: in-memory for the cache (cleared when the FT8 view closes);
 * localStorage for the two highlight colours (per-device operator preference).
 */

import { configState } from './config.svelte';
import { enrichCallsign } from '../api/enrichment';
import { fetchContestDupe } from '../api/contest-dupe';
import { ccodeToFlag } from '../utils/flag';

/** Decoration for one CQ callsign. Fields fill in independently as lookups land. */
export interface Ft8CallInfo {
    /** true = worked on this band+mode (dupe), false = not, undefined = unknown/pending. */
    worked?: boolean;
    /** Flag emoji, or '' when the country is unknown / not alpha-2. undefined = pending. */
    flag?: string;
    /** Country name (from the same enrichment lookup as the flag) for the flag's
     *  hover tooltip. undefined when unknown/pending. */
    country?: string;
}

// FT8 is itself an ADIF main mode, so the dupe axis is band + "FT8".
const FT8_MODE = 'FT8';

// Highlight-colour preference (localStorage). Defaults follow WSJT-X-land
// emphasis: the un-worked station gets the attention colour (green — "new, go
// work them"); a worked-before station is muted (grey) so the eye skips dupes.
const KEY_UNWORKED = 'sm.ft8.highlight.unworked';
const KEY_WORKED = 'sm.ft8.highlight.worked';
const DEFAULT_UNWORKED = '#15803d'; // tailwind green-700
const DEFAULT_WORKED = '#9ca3af'; // tailwind gray-400

function loadColor(key: string, fallback: string): string {
    try {
        return localStorage.getItem(key) ?? fallback;
    } catch {
        // localStorage unavailable (private mode / quota) — use the default.
        return fallback;
    }
}

function saveColor(key: string, value: string): void {
    try {
        localStorage.setItem(key, value);
    } catch {
        // Best-effort persistence; an in-memory value still applies this session.
    }
}

function cacheKey(call: string, band: string): string {
    return `${call}|${band}`;
}

class Ft8EnrichState {
    /**
     * Decoration cache keyed by `call|band`. Reassigned (not mutated) on each
     * resolution so `$state` notifies template reads of `info(...)`.
     */
    cache: Record<string, Ft8CallInfo> = $state({});

    /** Attention colour for an un-worked CQ station. */
    colorUnworked: string = $state(loadColor(KEY_UNWORKED, DEFAULT_UNWORKED));
    /** Muted colour for a worked-before CQ station (dupe de-emphasis). */
    colorWorked: string = $state(loadColor(KEY_WORKED, DEFAULT_WORKED));

    // Keys with a lookup currently in flight — dedupes concurrent observes for
    // the same call+band within and across slots. Non-reactive: it gates work,
    // it isn't rendered.
    #inFlight = new Set<string>();

    /** Decoration for a CQ call on a band, or undefined if not yet looked up. */
    info(call: string, band: string): Ft8CallInfo | undefined {
        return this.cache[cacheKey(call, band)];
    }

    setColorUnworked(hex: string): void {
        this.colorUnworked = hex;
        saveColor(KEY_UNWORKED, hex);
    }

    setColorWorked(hex: string): void {
        this.colorWorked = hex;
        saveColor(KEY_WORKED, hex);
    }

    /**
     * Kick off (at most once) the flag + worked-before lookups for a CQ call on
     * the given band. Idempotent: a cached or in-flight key is a no-op, so the
     * panel can call it for every visible CQ row each slot without re-fetching.
     */
    observe(call: string, band: string): void {
        const key = cacheKey(call, band);
        if (this.cache[key] !== undefined || this.#inFlight.has(key)) return;
        this.#inFlight.add(key);

        const merge = (patch: Ft8CallInfo): void => {
            const prev = this.cache[key] ?? {};
            this.cache = { ...this.cache, [key]: { ...prev, ...patch } };
        };

        // Flag — country via enrichment (hamnut-backed, daemon-cached).
        const flagLookup = enrichCallsign(call)
            .then((out) => {
                if (out.kind === 'ok')
                    merge({
                        flag: ccodeToFlag(out.result.country?.ccode),
                        country: out.result.country?.name,
                    });
            })
            .catch(() => {
                /* fail-soft: no flag */
            });

        // Worked-before — only meaningful with a real logbook and a known band.
        // Without either, leave `worked` unknown rather than asking a malformed
        // query (the daemon would 400 a blank band anyway).
        const logbook = configState.defaultLogbook.id;
        const dupeLookup =
            logbook >= 1 && band !== ''
                ? fetchContestDupe({ logbook, call, band, mode: FT8_MODE })
                      .then((out) => {
                          if (out.kind === 'ok') merge({ worked: out.duplicate });
                      })
                      .catch(() => {
                          /* fail-soft: worked stays unknown */
                      })
                : Promise.resolve();

        void Promise.allSettled([flagLookup, dupeLookup]).then(() => {
            this.#inFlight.delete(key);
        });
    }

    /** Drop all decorations. Called when the FT8 view closes so a re-open starts clean. */
    clear(): void {
        this.cache = {};
        this.#inFlight.clear();
    }
}

export const ft8EnrichState = new Ft8EnrichState();
