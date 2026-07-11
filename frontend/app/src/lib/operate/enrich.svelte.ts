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
import { isValidCallsign } from '../validators/callsign';

// The facets the UI renders. Mirrors the useful subset of the daemon's
// /v1/enrich/callsign response (country + station), kept flat for the card.
export interface Enrichment {
    /** Country name, '' when unknown. */
    country: string;
    /** ISO-3166 alpha-2 code (hamnut ccode) — drives the flag. '' when unknown. */
    ccode: string;
    /**
     * Numeric DXCC entity code for display (e.g. "291"). The daemon populates
     * station.dxcc from its curated prefix→entity map, so this is the real
     * number, not the alphabetic prefix. '' when the entity is unknown.
     */
    dxcc: string;
    /** true = this DXCC entity has never been worked (a "new one"). null = unknown. */
    isNewEntity: boolean | null;
    /** Their Maidenhead locator — the far end of bearing/distance. '' when unknown. */
    grid: string;
    /** Operator name from the lookup, '' when unknown. */
    name: string;
    /** Their QTH from the lookup — back-filled into the draft. '' when unknown. */
    qth: string;
    /** Their email from the station lookup — display only (contact overlay). '' when unknown. */
    email: string;
    /** CQ zone (country layer). Displayed + logged as ADIF CQZ. '' when unknown. */
    cqZone: string;
    /** ITU zone (country layer). Displayed + logged as ADIF ITUZ. '' when unknown. */
    ituZone: string;
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

export type EnrichFn = (call: string, signal?: AbortSignal) => Promise<Enrichment | null>;
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
// The seq guard protects the UI; the AbortController additionally cancels the
// superseded daemon/upstream request instead of letting it run to completion.
let seq = 0;
let inflight: AbortController | null = null;

/**
 * Feed the current callsign-field value. Idempotent per rendered frame — the
 * card calls it from an $effect on draft.callsign. Too-short or shape-invalid
 * input clears the enrichment (this is also how a logged/cleared draft resets
 * the card). The validity gate keeps malformed partials off the wire — on a
 * flaky link every skipped daemon round-trip (and possible upstream QRZ miss)
 * matters. Note the validator's floor: a digit-bearing partial like "DL3" is
 * a structurally valid call, so only the debounce stops those.
 */
export function observeCall(raw: string): void {
    const call = raw.trim().toUpperCase();
    clearTimeout(timer);

    if (call.length < MIN_CALL_LEN || isValidCallsign(call) !== null) {
        seq++;
        inflight?.abort();
        retractWrites();
        enrich.status = 'idle';
        enrich.call = '';
        enrich.data = null;
        return;
    }
    // Already resolved or resolving this exact call — nothing to do.
    if (call === enrich.call && enrich.status !== 'idle') return;

    timer = setTimeout(() => void lookup(call), DEBOUNCE_MS);
}

// What the last resolution wrote into the draft. An enrichment-provided value
// must never masquerade as operator entry: when the callsign moves on, any
// draft field that STILL holds the value we wrote is retracted (a field the
// operator edited no longer matches, so it survives). Without this, the
// previous station's name/grid leak into the next QSO — the field looks
// operator-filled, so the new lookup's fill-if-empty guard skips it.
let wrote = { name: '', grid: '', qth: '' };

function retractWrites(): void {
    if (wrote.grid !== '' && draft.gridsquare === wrote.grid) draft.gridsquare = '';
    if (wrote.name !== '' && draft.name === wrote.name) draft.name = '';
    if (wrote.qth !== '' && draft.qth === wrote.qth) draft.qth = '';
    wrote = { name: '', grid: '', qth: '' };
}

async function lookup(call: string): Promise<void> {
    if (enricher === null) return; // seam not wired — stay idle, never throw

    const mine = ++seq;
    inflight?.abort();
    inflight = new AbortController();
    enrich.status = 'pending';
    enrich.call = call;
    enrich.data = null;

    let result: Enrichment | null = null;
    try {
        result = await enricher(call, inflight.signal);
    } catch {
        // fail-soft: done-with-nothing, the card shows its empty state
    }
    if (seq !== mine) return; // superseded by a newer call — discard

    enrich.data = result;
    enrich.status = 'done';

    // Write looked-up facts back into the QSO draft: grid + QTH (enrichment-
    // filled, corrected in Details — never typed on the card) and the
    // operator's name into the card's Name field. The previous station's
    // writes are retracted first; then fill-if-empty, so a value the operator
    // entered is never overwritten.
    if (draft.callsign.trim().toUpperCase() !== call) return;
    retractWrites();
    if (result === null) return;
    if (result.grid !== '' && draft.gridsquare === '') {
        draft.gridsquare = result.grid;
        wrote.grid = result.grid;
    }
    if (result.name !== '' && draft.name === '') {
        draft.name = result.name;
        wrote.name = result.name;
    }
    if (result.qth !== '' && draft.qth === '') {
        draft.qth = result.qth;
        wrote.qth = result.qth;
    }
}
