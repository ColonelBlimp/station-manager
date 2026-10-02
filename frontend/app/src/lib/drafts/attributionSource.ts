// The attribution a saved draft records (ADR 0085 RS1; review 2026-10-01): what
// a Phone / CW submit from this page is stored with, as GET
// /v1/submit-attribution reports it. Kept honest rather than merely recent:
//   - every config write invalidates it AT ONCE (a save made meanwhile records
//     it as missing — never the old value beside a changed operator);
//   - a read applies only if it is the newest, no config write started since it
//     began or is still in flight, and the archive binding is still valid;
//   - a dropped or failed read leaves it missing and marks a retry, taken once
//     the binding is proven again.
// The page wires the binding check and the fetch (main.ts); config writes are
// reported by safeFetch, the one path every request takes.

import { fetchSubmitAttribution, type SubmitAttribution } from '../api/submit-attribution';

let current: SubmitAttribution | null = null;
let epoch = 0; // advanced by every write start and every read start
let writesInFlight = 0;
let retryWanted = false;
let latest: Promise<void> = Promise.resolve();
let bindingValid: () => boolean = () => true;
let fetchAttribution: () => Promise<SubmitAttribution | null> = () => fetchSubmitAttribution();

export function configureAttribution(opts: {
    bindingValid: () => boolean;
    fetch?: () => Promise<SubmitAttribution | null>;
}): void {
    bindingValid = opts.bindingValid;
    if (opts.fetch) fetchAttribution = opts.fetch;
}

/** The attribution to record now; null = missing. */
export function currentAttribution(): SubmitAttribution | null {
    return current;
}

/** The epoch a boot read starts from (read it BEFORE the read). */
export function attributionEpoch(): number {
    return epoch;
}

/** A config write is about to be sent: the attribution may change. */
export function noteConfigWriteStarted(): void {
    writesInFlight++;
    epoch++;
    current = null;
}

/** A config write settled (whatever its outcome): read the attribution again
 *  once no other write is in flight. Resolves when that read has applied or
 *  been dropped, so a caller can hold its save latch on it. */
export function noteConfigWriteSettled(): Promise<void> {
    writesInFlight = Math.max(0, writesInFlight - 1);
    return refreshAttribution();
}

/** Read the attribution now, under the rules above. */
export function refreshAttribution(): Promise<void> {
    if (writesInFlight > 0) return latest; // the last write's settle reads it
    const mine = ++epoch;
    if (!bindingValid()) {
        retryWanted = true;
        return Promise.resolve();
    }
    latest = (async () => {
        const a = await fetchAttribution();
        if (mine !== epoch || writesInFlight > 0) return; // superseded
        if (!bindingValid() || a === null) {
            retryWanted = true; // stays missing until a proven re-read
            return;
        }
        current = a;
        retryWanted = false;
    })();
    return latest;
}

/** The latest read in progress (or settled): a caller holds its save latch on
 *  it so the attribution is current before the save is called done. */
export function attributionSettled(): Promise<void> {
    return latest;
}

/** The binding was proven again: re-read if a read was dropped or failed. */
export function retryAttributionIfPending(): Promise<void> {
    if (!retryWanted || !bindingValid()) return Promise.resolve();
    retryWanted = false;
    return refreshAttribution();
}

/** The boot read, made inside the identity bracket: applied only once the
 *  bracket proved the binding, and only if nothing started since `since`. */
export function applyBootAttribution(a: SubmitAttribution | null, since: number): void {
    if (since !== epoch || writesInFlight > 0 || !bindingValid() || a === null) {
        retryWanted = true;
        return;
    }
    current = a;
}

export function _resetAttributionForTests(): void {
    current = null;
    epoch = 0;
    writesInFlight = 0;
    retryWanted = false;
    latest = Promise.resolve();
    bindingValid = () => true;
    fetchAttribution = () => fetchSubmitAttribution();
}
