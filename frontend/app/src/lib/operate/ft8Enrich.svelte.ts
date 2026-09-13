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
    /** Numeric DXCC entity code (e.g. "291"). undefined = pending; '' when unknown. */
    dxcc?: string;
    /** true = this DXCC entity has never been worked (a "new one"). undefined = pending. */
    isNewEntity?: boolean;
    /** Operator name (enrichment). undefined = pending; '' when none. */
    opName?: string;
    /** Operator's locator — a fallback far-end for the bearing when the CQ line
     *  carries no grid. undefined/'' when unknown. */
    grid?: string;
}

/** The FT-family profile a lookup is made for (ADR 0080). */
export type FtProfile = 'FT8' | 'FT4';

/** The ADIF axis "worked before" is asked on: FT8 is a mode of its own; FT4 is
 *  filed as MODE=MFSK SUBMODE=FT4 (ADIF 3.1.5), so its dupe is the pair. */
export function dupeAxis(profile: FtProfile): { mode: string; submode: string } {
    return profile === 'FT4' ? { mode: 'MFSK', submode: 'FT4' } : { mode: 'FT8', submode: '' };
}

// The cache is keyed by profile as well: an FT4 "worked" answer is a different
// fact from an FT8 one, and a lookup started under FT8 that resolves after the
// view remounted on FT4 lands in the FT8 bucket, never in FT4's.
function cacheKey(profile: FtProfile, call: string, band: string): string {
    return `${profile}|${call}|${band}`;
}

/*
    Injected seams (ADR 0045). enricher resolves the flag/country/new/name/grid;
    dupe answers "worked this call on this band already?" (null = unknown). Both
    wired in main.ts; unwired = no decoration (fail-soft).
*/
export type Ft8Enricher = (call: string, signal?: AbortSignal) => Promise<Enrichment | null>;
export type Ft8Dupe = (
    call: string,
    band: string,
    mode: string,
    submode: string
) => Promise<boolean | null>;

/** At most this many /v1/enrich/callsign lookups in flight per tab (operator
 *  ruling 2026-09-13, W-0012): a busy FT4 slot with fifteen new calls used to
 *  fire them all at once and fill the browser's per-host connection budget,
 *  queueing the operator's own requests behind decoration (2026-09-11). The
 *  worked-before check is NOT under this cap — a local read that drives the
 *  grey-out, wanted within the slot. */
export const FT8_ENRICH_CONCURRENCY = 2;

/** What a Band Activity pass says about the row it observes: a station calling
 *  us (or an answerer) goes ahead of a plain CQ, and a newer slot ahead of an
 *  older one. Absent (markWorked's re-kick) = plain CQ, no slot. */
export interface ObserveOpts {
    kind?: 'cq' | 'call';
    slot?: string;
}

interface Queued {
    key: string;
    call: string;
    prio: 0 | 1; // 0 = calling us / answerer, 1 = CQ
    slot: string; // ISO slot start; a later string is a newer slot
    seq: number; // FIFO within the same prio + slot
}

let enricher: Ft8Enricher | null = null;
let dupe: Ft8Dupe | null = null;

export function setFt8Enricher(fn: Ft8Enricher): void {
    enricher = fn;
}

export function setFt8Dupe(fn: Ft8Dupe): void {
    dupe = fn;
}

class Ft8EnrichState {
    /** Decoration cache keyed by `profile|call|band`. Reassigned (not mutated) on
     *  each resolution so $state notifies template reads of info(...). */
    cache: Record<string, Ft8CallInfo> = $state({});

    // Non-reactive plumbing (gates work, never rendered). The two facets are
    // tracked apart: the worked-before check fires at once and settles on its
    // own; the enrich lookup goes through the scheduler below.
    #dupeBusy = new Set<string>(); // worked-before lookup running
    #dupeDone = new Set<string>(); // ...or settled (lookup-once, even on failure)
    #pending = new Map<string, Queued>(); // enrich lookups waiting for a slot in the cap
    #active = new Map<string, AbortController>(); // enrich lookups in flight
    #enrichDone = new Set<string>(); // enrich settled (lookup-once, even on failure)
    #passSeen: Set<string> | null = null; // keys observed during the current pass
    #seq = 0;
    #epoch = 0; // bumped by clear(): a late answer from before it is discarded

    /** Decoration for a CQ call on a band under a profile, or undefined if not yet looked up. */
    info(call: string, band: string, profile: FtProfile = 'FT8'): Ft8CallInfo | undefined {
        return this.cache[cacheKey(profile, call, band)];
    }

    /**
     * Kick off (at most once) the flag + worked-before lookups. Idempotent: a
     * cached or in-flight key is a no-op, so the panel can call it for every
     * visible CQ row each slot without re-fetching.
     */
    observe(call: string, band: string, profile: FtProfile = 'FT8', opts: ObserveOpts = {}): void {
        const key = cacheKey(profile, call, band);
        this.#passSeen?.add(key);
        const prio: 0 | 1 = opts.kind === 'call' ? 0 : 1;
        const slot = opts.slot ?? '';

        // Worked-before: at once, outside the cap; once per key.
        if (!this.#dupeDone.has(key) && !this.#dupeBusy.has(key) && dupe && band !== '') {
            this.#dupeBusy.add(key);
            const axis = dupeAxis(profile);
            const epoch = this.#epoch;
            void dupe(call, band, axis.mode, axis.submode)
                .then((w) => {
                    if (w !== null && epoch === this.#epoch) this.#merge(key, { worked: w });
                })
                .catch(() => {
                    /* fail-soft: worked stays unknown */
                })
                .finally(() => {
                    this.#dupeBusy.delete(key);
                    if (epoch === this.#epoch) this.#dupeDone.add(key);
                });
        }

        // Enrich: through the scheduler. A pending entry can only gain priority
        // (a plain CQ row that then calls us; a call seen again in a newer slot).
        if (!enricher || this.#enrichDone.has(key) || this.#active.has(key)) return;
        const queued = this.#pending.get(key);
        if (queued) {
            if (prio < queued.prio) queued.prio = prio;
            if (slot > queued.slot) queued.slot = slot;
            return;
        }
        this.#pending.set(key, { key, call, prio, slot, seq: this.#seq++ });
        // Inside a pass, dispatch waits for endPass(): the first rows observed
        // must not fill the cap before a caller later in the same pass is seen
        // (cq_to_top lists CQ rows first; codex 1fe16e2b P2). Outside a pass
        // (markWorked's re-kick) there is nothing to wait for.
        if (this.#passSeen === null) this.#pump();
    }

    /** A Band Activity pass brackets the observes of every row on screen: what
     *  was pending before and is not observed during the pass has scrolled off
     *  unheard and is dropped (stale work never consumes the budget). Lookups
     *  already in flight are left to land. */
    beginPass(): void {
        this.#passSeen = new Set();
    }

    endPass(): void {
        const seen = this.#passSeen;
        this.#passSeen = null;
        if (!seen) return;
        for (const key of [...this.#pending.keys()]) {
            if (!seen.has(key)) this.#pending.delete(key);
        }
        this.#pump(); // the whole pass is known: priority runs over all of it
    }

    #merge(key: string, patch: Ft8CallInfo): void {
        const prev = this.cache[key] ?? {};
        this.cache = { ...this.cache, [key]: { ...prev, ...patch } };
    }

    // Start pending lookups up to the cap: a station calling us before a plain
    // CQ, a newer slot before an older one, FIFO within that.
    #pump(): void {
        while (this.#active.size < FT8_ENRICH_CONCURRENCY && this.#pending.size > 0) {
            let best: Queued | undefined;
            for (const q of this.#pending.values()) {
                if (
                    !best ||
                    q.prio < best.prio ||
                    (q.prio === best.prio &&
                        (q.slot > best.slot || (q.slot === best.slot && q.seq < best.seq)))
                )
                    best = q;
            }
            if (!best || !enricher) return;
            this.#pending.delete(best.key);
            const ctrl = new AbortController();
            this.#active.set(best.key, ctrl);
            const epoch = this.#epoch;
            const key = best.key;
            void enricher(best.call, ctrl.signal)
                .then((e) => {
                    if (e && epoch === this.#epoch)
                        this.#merge(key, {
                            flag: ccodeToFlag(e.ccode),
                            country: e.country,
                            dxcc: e.dxcc,
                            isNewEntity: e.isNewEntity ?? undefined,
                            opName: e.name,
                            grid: e.grid,
                        });
                })
                .catch(() => {
                    /* fail-soft: no flag */
                })
                .finally(() => {
                    if (epoch !== this.#epoch) return; // cleared meanwhile: nothing to resume
                    this.#active.delete(key);
                    this.#enrichDone.add(key);
                    this.#pump();
                });
        }
    }

    /**
     * Mark a call worked on a band (dupe) so the row grays out immediately —
     * called when an FT8 QSO logs, since the lookup-once cache would otherwise
     * hold the stale worked:false captured before the contact. Also re-kicks
     * observe() so a station worked without ever showing as a CQ row still gets
     * its flag; the worked:true merge wins.
     */
    markWorked(call: string, band: string, profile: FtProfile = 'FT8'): void {
        const c = call.trim().toUpperCase();
        if (c === '' || band === '') return;
        this.observe(c, band, profile);
        const key = cacheKey(profile, c, band);
        const prev = this.cache[key] ?? {};
        this.cache = { ...this.cache, [key]: { ...prev, worked: true, isNewEntity: false } };
        // A worked new-entity is new no longer — the ★ must drop from EVERY
        // cached call of the same entity, on every band (the daemon's
        // is_new_entity flips the moment the QSO stores; this mirrors that
        // onto the lookup-once cache, which would otherwise show stale stars
        // all session). Sweep by the worked call's dxcc when it's known; if
        // its enrich is still pending, other entries can't be matched — but
        // any FUTURE lookup gets the fresh (false) flag from the daemon, so
        // only pre-cached same-entity stars could linger in that edge.
        const dx = prev.dxcc ?? '';
        if (dx !== '') {
            const swept = { ...this.cache };
            let hit = false;
            for (const [k, v] of Object.entries(swept)) {
                if (v.dxcc === dx && v.isNewEntity === true) {
                    swept[k] = { ...v, isNewEntity: false };
                    hit = true;
                }
            }
            if (hit) this.cache = swept;
        }
    }

    /** Drop all decorations — called on FT8-view close so a re-open starts clean. */
    clear(): void {
        this.cache = {};
        this.#epoch++; // late answers from before now are discarded
        for (const ctrl of this.#active.values()) ctrl.abort();
        this.#active.clear();
        this.#pending.clear();
        this.#dupeBusy.clear();
        this.#dupeDone.clear();
        this.#enrichDone.clear();
        this.#passSeen = null;
    }
}

export const ft8EnrichState = new Ft8EnrichState();

/** Test seam — clear cache + injected lookups between cases. */
export function resetFt8EnrichForTests(): void {
    enricher = null;
    dupe = null;
    ft8EnrichState.clear();
}
