// The attribution a saved draft records (ADR 0085 RS1; reviews 2026-10-01 and
// Codex 21feafae, operator ruling 2026-10-02): what a Phone / CW submit from
// this page is stored with — read with GET /v1/submit-attribution?operator=
// for the operator this page's submit SENDS, and filed under that requested
// operator. Kept honest rather than merely recent:
//   - it is returned only for the operator it was read for; a change of the
//     requested operator invalidates it and re-reads;
//   - a config write from this page (reported by safeFetch), a config.updated
//     event (any client's write) and a disconnect each clear it AT ONCE and
//     drop any read in flight; a save meanwhile records it as MISSING;
//   - a read applies only if it is the newest, no config write is in flight or
//     started since, and the archive binding is still valid;
//   - every proven reconnect re-reads it.
// SSE is eventual invalidation, not instant knowledge of a remote write; the
// server's submit-time expectation check stays the final protection.

import { fetchSubmitAttribution, type SubmitAttribution } from '../api/submit-attribution';

interface Entry {
    requested: string;
    attribution: SubmitAttribution;
}

// Reactive: the Saved QSOs panel's Restore offer follows it (ADR 0085).
let current = $state.raw<Entry | null>(null);
let requested = '';
let requestedKnown = false;
let epoch = 0; // advanced by everything that invalidates, and by every read start
let writesInFlight = 0;
let latest: Promise<void> = Promise.resolve();
let bindingValid: () => boolean = () => true;
let fetchFor: (operator: string) => Promise<SubmitAttribution | null> = (operator) =>
    fetchSubmitAttribution(operator);

export function configureAttribution(opts: {
    bindingValid: () => boolean;
    fetch?: (operator: string) => Promise<SubmitAttribution | null>;
}): void {
    bindingValid = opts.bindingValid;
    if (opts.fetch) fetchFor = opts.fetch;
}

/** The attribution to record for a draft whose submit sends `operator`; null =
 *  missing (never one read for another operator). */
export function currentAttribution(operator: string): SubmitAttribution | null {
    return current !== null && current.requested === operator ? current.attribution : null;
}

/** The epoch a boot read starts from (read it BEFORE the read). */
export function attributionEpoch(): number {
    return epoch;
}

function invalidate(): void {
    epoch++; // drops every read in flight
    current = null;
}

/** The operator this page's submit sends (ctx.operator). A change invalidates
 *  and re-reads; the same value changes nothing. */
export function setRequestedOperator(operator: string): Promise<void> {
    if (requestedKnown && operator === requested) return latest;
    requestedKnown = true;
    requested = operator;
    invalidate();
    return refreshAttribution();
}

/** A config write from this page is about to be sent. */
export function noteConfigWriteStarted(): void {
    writesInFlight++;
    invalidate();
}

/** A config write settled (whatever its outcome): re-read once no other write
 *  is in flight. Resolves when that read has applied or been dropped. */
export function noteConfigWriteSettled(): Promise<void> {
    writesInFlight = Math.max(0, writesInFlight - 1);
    return refreshAttribution();
}

/** config.updated on /v1/events: some client's write made a new config live. */
export function noteConfigUpdated(): Promise<void> {
    invalidate();
    return refreshAttribution();
}

/** The events stream dropped: nothing heard meanwhile can be trusted. */
export function noteDisconnected(): void {
    invalidate();
}

/** A reconnect proved the binding again: always re-read (an event may have been
 *  missed while disconnected). */
export function noteReconnectProven(): Promise<void> {
    return refreshAttribution();
}

/** Read the attribution now, for the operator this page sends. */
export function refreshAttribution(): Promise<void> {
    if (writesInFlight > 0) return latest; // the last write's settle reads it
    const mine = ++epoch;
    const forOperator = requested;
    if (!bindingValid()) return Promise.resolve();
    latest = (async () => {
        const a = await fetchFor(forOperator);
        if (mine !== epoch || writesInFlight > 0) return; // superseded or invalidated
        if (!bindingValid() || a === null) return; // stays missing until a proven re-read
        current = { requested: forOperator, attribution: a };
    })();
    return latest;
}

/** The latest read in progress (or settled): a caller holds its save latch on it. */
export function attributionSettled(): Promise<void> {
    return latest;
}

/** The boot read, made inside the identity bracket for `operator`: applied only
 *  once the bracket proved the binding, nothing started since `since`, and the
 *  requested operator is still the one it was read for. */
export function applyBootAttribution(
    a: SubmitAttribution | null,
    since: number,
    operator: string
): void {
    if (since !== epoch || writesInFlight > 0 || !bindingValid() || a === null) return;
    if (requestedKnown && operator !== requested) return;
    requestedKnown = true;
    requested = operator;
    current = { requested: operator, attribution: a };
}

export function _resetAttributionForTests(): void {
    current = null;
    requested = '';
    requestedKnown = false;
    epoch = 0;
    writesInFlight = 0;
    latest = Promise.resolve();
    bindingValid = () => true;
    fetchFor = (operator) => fetchSubmitAttribution(operator);
}
