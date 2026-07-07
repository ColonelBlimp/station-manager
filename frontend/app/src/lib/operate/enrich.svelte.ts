// Callsign enrichment state — the always-on, glanceable lookup that decorates
// the in-progress QSO (flag / DXCC + NEW / bearing + distance). Presentation
// (EnrichmentCard) is separate and positionless (ADR 0045): the same state can
// back the logging-card square today and a standalone/repositioned card later.
//
// The lookup itself is an INJECTED seam (setEnricher, wired in main.ts —
// mirrors qso.svelte's setSubmit): this module never imports an API client, so
// it runs against the dev stub now and /v1/enrich/callsign later, unchanged.
//
// Invariant carried over from the daemon: enrichment degrades, never blocks.
// A failed lookup resolves to "done with nothing" — the operator can always log.

import { draft } from './qso.svelte';

// The facets the UI renders. Mirrors the useful subset of the daemon's
// /v1/enrich/callsign response (country + station), kept flat for the card.
export interface Enrichment {
    /** Country name, '' when unknown. */
    country: string;
    /** ISO-3166 alpha-2 code (hamnut ccode) — drives the flag. '' when unknown. */
    ccode: string;
    /** DXCC entity number, null when unknown. */
    dxcc: number | null;
    /** true = this DXCC entity has never been worked (a "new one"). null = unknown. */
    isNewEntity: boolean | null;
    /** Their Maidenhead locator — the far end of bearing/distance. '' when unknown. */
    grid: string;
    /** Operator name from the lookup, '' when unknown. */
    name: string;
}

export type EnrichStatus = 'idle' | 'pending' | 'done';

export const enrich: { status: EnrichStatus; call: string; data: Enrichment | null } = $state({
    status: 'idle',
    /** The (normalised) callsign the current status/data belong to. */
    call: '',
    /** Lookup result; null while idle/pending or when the lookup found nothing. */
    data: null,
});

// My locator — the near end of bearing/distance (and, later, the rotator's
// reference). Stubbed via setMyGrid in main.ts; moves to a station/config state
// module when /v1/config wires up.
export const station = $state({ myGrid: '' });

export function setMyGrid(grid: string): void {
    station.myGrid = grid.trim().toUpperCase();
}

export type PathChoice = 'sp' | 'lp';

// Which propagation path the operator is working — the card's SP/LP radio
// group writes it. Shared state rather than card-local UI because the future
// rotator control must drive the antenna to the SAME heading the card shows.
// Deliberately sticky across lookups: long path is a band/opening condition,
// not a per-station fact.
export const prefs: { path: PathChoice } = $state({ path: 'sp' });

export type EnrichFn = (call: string) => Promise<Enrichment | null>;
let enricher: EnrichFn | null = null;

export function setEnricher(fn: EnrichFn): void {
    enricher = fn;
}

// Typing settles before we ask: a fast-path operator keys the whole call in
// well under a second, so one debounced lookup fires per station, not per
// keystroke. 3 chars is the shortest real-world callsign.
const DEBOUNCE_MS = 400;
const MIN_CALL_LEN = 3;

let timer: ReturnType<typeof setTimeout> | undefined;
// Monotonic lookup token: a newer observe invalidates any in-flight resolution,
// so a slow lookup for the previous call can never overwrite the current one.
let seq = 0;

/**
 * Feed the current callsign-field value. Idempotent per rendered frame — the
 * card calls it from an $effect on draft.callsign. Too-short input clears the
 * enrichment (this is also how a logged/cleared draft resets the card).
 */
export function observeCall(raw: string): void {
    const call = raw.trim().toUpperCase();
    clearTimeout(timer);

    if (call.length < MIN_CALL_LEN) {
        seq++;
        enrich.status = 'idle';
        enrich.call = '';
        enrich.data = null;
        return;
    }
    // Already resolved or resolving this exact call — nothing to do.
    if (call === enrich.call && enrich.status !== 'idle') return;

    timer = setTimeout(() => void lookup(call), DEBOUNCE_MS);
}

async function lookup(call: string): Promise<void> {
    if (enricher === null) return; // seam not wired — stay idle, never throw

    const mine = ++seq;
    enrich.status = 'pending';
    enrich.call = call;
    enrich.data = null;

    let result: Enrichment | null = null;
    try {
        result = await enricher(call);
    } catch {
        // fail-soft: done-with-nothing, the card shows its empty state
    }
    if (seq !== mine) return; // superseded by a newer call — discard

    enrich.data = result;
    enrich.status = 'done';

    // Write the looked-up grid back into the QSO draft (GRIDSQUARE is
    // enrichment-filled, never typed on the card). Guarded: only while the
    // draft still holds the call this lookup was for, and never over a value
    // the Details card set by hand.
    if (
        result !== null &&
        result.grid !== '' &&
        draft.gridsquare === '' &&
        draft.callsign.trim().toUpperCase() === call
    ) {
        draft.gridsquare = result.grid;
    }
}
