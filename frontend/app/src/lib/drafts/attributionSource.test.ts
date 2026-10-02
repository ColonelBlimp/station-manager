/*
    The attribution a saved draft records (ADR 0085 RS1; review 2026-10-01).

      AS1 A config write invalidates the attribution AT ONCE: while its refresh
          is pending (or skipped) a save records it as MISSING — never the
          previous value beside a changed operator (operator B, attribution A).
      AS2 Refreshes cannot apply out of order: an older response (B) arriving
          after a newer one (C) is dropped.
      AS3 A response that arrives after the archive binding stopped being valid
          is dropped; the attribution stays missing until a retry, once the
          binding is proven again, reads it afresh.
      AS4 While any config write is still in flight, no refresh applies — a
          write that settles first cannot publish a value the other may change.
      AS5 The boot read applies only after the identity bracket proved the
          binding, and only if no write or refresh started meanwhile.
      AS6 A write's settle returns the refresh, so a caller can hold its save
          latch until the attribution is current (the Station save does).
      AS7 A failed read leaves it missing and marks a retry.
*/
import { beforeEach, describe, expect, it } from 'vitest';
import type { SubmitAttribution } from '../api/submit-attribution';
import {
    applyBootAttribution,
    attributionSettled,
    attributionEpoch,
    configureAttribution,
    currentAttribution,
    noteConfigWriteSettled,
    noteConfigWriteStarted,
    refreshAttribution,
    retryAttributionIfPending,
    _resetAttributionForTests,
} from './attributionSource';

const A: SubmitAttribution = { myRig: 'FTdx10', operator: 'A', myName: 'Alice' };
const B: SubmitAttribution = { myRig: 'FTdx10', operator: 'B', myName: 'Bob' };
const C: SubmitAttribution = { myRig: 'FTdx10', operator: 'C', myName: 'Carol' };

/** A fetch whose answers the test releases one by one, in any order. */
function deferredFetch() {
    const pending: Array<(a: SubmitAttribution | null) => void> = [];
    return {
        fetch: () => new Promise<SubmitAttribution | null>((r) => pending.push(r)),
        answer: (i: number, a: SubmitAttribution | null) => pending[i](a),
        count: () => pending.length,
    };
}

let bindingValid = true;
let net = deferredFetch();

beforeEach(() => {
    _resetAttributionForTests();
    bindingValid = true;
    net = deferredFetch();
    configureAttribution({ bindingValid: () => bindingValid, fetch: net.fetch });
});

async function settleWith(a: SubmitAttribution): Promise<void> {
    noteConfigWriteStarted();
    const done = noteConfigWriteSettled();
    net.answer(net.count() - 1, a);
    await done;
}

describe('attribution source', () => {
    it('AS1 a config write invalidates at once; missing while the refresh is pending', async () => {
        await settleWith(A);
        expect(currentAttribution()).toEqual(A);
        noteConfigWriteStarted(); // the Station save sends operator B
        expect(currentAttribution()).toBeNull(); // a save now records MISSING
        const done = noteConfigWriteSettled();
        expect(currentAttribution()).toBeNull(); // still pending
        net.answer(net.count() - 1, B);
        await done;
        expect(currentAttribution()).toEqual(B);
    });

    it('AS2 an older response after a newer one is dropped', async () => {
        noteConfigWriteStarted();
        const b = noteConfigWriteSettled(); // refresh #0 (for B)
        noteConfigWriteStarted();
        const c = noteConfigWriteSettled(); // refresh #1 (for C)
        net.answer(1, C);
        await c;
        net.answer(0, B); // the older answer arrives last
        await b;
        expect(currentAttribution()).toEqual(C);
    });

    it('AS3 a response after the binding became invalid is dropped; a retry reads afresh', async () => {
        noteConfigWriteStarted();
        const done = noteConfigWriteSettled();
        bindingValid = false; // the archive switch became unresolved
        net.answer(0, B);
        await done;
        expect(currentAttribution()).toBeNull();
        await retryAttributionIfPending(); // still invalid: nothing read
        expect(net.count()).toBe(1);
        bindingValid = true; // proven again
        const retry = retryAttributionIfPending();
        net.answer(1, C);
        await retry;
        expect(currentAttribution()).toEqual(C);
    });

    it('AS4 no refresh applies while another write is in flight', async () => {
        noteConfigWriteStarted(); // write 1
        noteConfigWriteStarted(); // write 2
        void noteConfigWriteSettled(); // write 1 settles; write 2 still in flight
        expect(net.count()).toBe(0); // nothing read while write 2 may still change it
        const two = noteConfigWriteSettled();
        net.answer(0, C);
        await two;
        expect(currentAttribution()).toEqual(C);
    });

    it('AS4 a write starting during a refresh drops that refresh', async () => {
        const r = refreshAttribution();
        noteConfigWriteStarted();
        net.answer(0, A);
        await r;
        expect(currentAttribution()).toBeNull();
    });

    it('AS5 the boot read applies only when proven and undisturbed', () => {
        const token = attributionEpoch();
        applyBootAttribution(A, token);
        expect(currentAttribution()).toEqual(A);

        _resetAttributionForTests();
        configureAttribution({ bindingValid: () => bindingValid, fetch: net.fetch });
        const t2 = attributionEpoch();
        noteConfigWriteStarted(); // a write started during the boot read
        applyBootAttribution(A, t2);
        expect(currentAttribution()).toBeNull();

        _resetAttributionForTests();
        configureAttribution({ bindingValid: () => false, fetch: net.fetch });
        applyBootAttribution(A, attributionEpoch()); // bracket left the page gated
        expect(currentAttribution()).toBeNull();
    });

    it('AS5 a boot read older than a completed write does not overwrite it', async () => {
        const since = attributionEpoch(); // the boot read starts …
        await settleWith(B); // … a write starts, settles and B is read …
        expect(currentAttribution()).toEqual(B);
        applyBootAttribution(A, since); // … then the older boot answer lands
        expect(currentAttribution()).toEqual(B);
    });

    it('AS6 the settle resolves only once the attribution is current', async () => {
        noteConfigWriteStarted();
        let settled = false;
        const done = noteConfigWriteSettled().then(() => (settled = true));
        await Promise.resolve();
        expect(settled).toBe(false); // a save latch held on this stays held
        net.answer(0, B);
        await done;
        expect(settled).toBe(true);
        expect(currentAttribution()).toEqual(B);
    });

    it('AS6 attributionSettled waits for the read a write started', async () => {
        noteConfigWriteStarted();
        void noteConfigWriteSettled();
        let done = false;
        const held = attributionSettled().then(() => (done = true));
        await Promise.resolve();
        expect(done).toBe(false);
        net.answer(0, C);
        await held;
        expect(currentAttribution()).toEqual(C);
    });

    it('AS7 a failed read leaves it missing and marks a retry', async () => {
        noteConfigWriteStarted();
        const done = noteConfigWriteSettled();
        net.answer(0, null);
        await done;
        expect(currentAttribution()).toBeNull();
        const retry = retryAttributionIfPending();
        net.answer(1, A);
        await retry;
        expect(currentAttribution()).toEqual(A);
    });
});
