/*
    The attribution a saved draft records (ADR 0085 RS1; reviews 2026-10-01 and
    Codex 21feafae, operator ruling 2026-10-02).

      AS1 A config write invalidates the attribution AT ONCE: while its refresh
          is pending (or skipped) a save records it as MISSING.
      AS2 Refreshes cannot apply out of order: an older response arriving after
          a newer one is dropped.
      AS3 A response arriving after the archive binding stopped being valid is
          dropped; the attribution stays missing until a proven reconnect.
      AS4 While any config write is still in flight, no refresh applies; a write
          starting during a refresh drops it.
      AS5 The boot read applies only after the identity bracket proved the
          binding, only if nothing started since, and only for the operator it
          was requested for.
      AS6 A write's settle, and attributionSettled, resolve only once the
          attribution is current (the Station save holds its latch on it).
      AS7 A failed read leaves it missing until a proven reconnect re-reads it.
      AS8 Cache identity: the attribution is read FOR an operator — the one this
          page's submit sends — and is only ever returned for that operator;
          asking for any other is missing. A change of requested operator
          invalidates and re-reads.
      AS9 The landed-write, timed-out-response sequence cannot pair operator A
          with attribution resolved for B: the read asks for the operator the
          page still sends (A), and B's resolution is never filed under A.
      AS10 A config.updated event (another client's write) clears it, DROPS a
          read in flight, and re-reads.
      AS11 A disconnect clears it and drops a read in flight; every proven
          reconnect re-reads, even after a successful earlier read.
      AS12 The requested operator may be empty: it is passed on as such (the
          server applies the default-operator fallback).
*/
import { beforeEach, describe, expect, it } from 'vitest';
import type { SubmitAttribution } from '../api/submit-attribution';
import {
    applyBootAttribution,
    attributionEpoch,
    attributionSettled,
    configureAttribution,
    currentAttribution,
    noteConfigUpdated,
    noteConfigWriteSettled,
    noteConfigWriteStarted,
    noteDisconnected,
    noteReconnectProven,
    refreshAttribution,
    setRequestedOperator,
    _resetAttributionForTests,
} from './attributionSource';

const forOp = (op: string): SubmitAttribution => ({
    myRig: 'FTdx10',
    operator: op === '' ? 'DEFAULT' : op,
    myName: `name of ${op}`,
});

/** A fetch whose answers the test releases one by one, in any order. */
function deferredFetch() {
    const asked: string[] = [];
    const pending: Array<(a: SubmitAttribution | null) => void> = [];
    return {
        asked,
        fetch: (operator: string) => {
            asked.push(operator);
            return new Promise<SubmitAttribution | null>((r) => pending.push(r));
        },
        answer: (i: number, a: SubmitAttribution | null) => pending[i](a),
        count: () => pending.length,
    };
}

let bindingValid = true;
let net = deferredFetch();

function configure(): void {
    configureAttribution({ bindingValid: () => bindingValid, fetch: net.fetch });
}

beforeEach(async () => {
    _resetAttributionForTests();
    bindingValid = true;
    net = deferredFetch();
    configure();
    const first = setRequestedOperator('A');
    net.answer(0, forOp('A'));
    await first;
});

/** Run one read to completion with the given answer. */
async function readWith(a: SubmitAttribution | null): Promise<void> {
    const done = refreshAttribution();
    net.answer(net.count() - 1, a);
    await done;
}

describe('attribution source', () => {
    it('AS1 a config write invalidates at once; missing while the refresh is pending', async () => {
        expect(currentAttribution('A')).toEqual(forOp('A'));
        noteConfigWriteStarted();
        expect(currentAttribution('A')).toBeNull();
        const done = noteConfigWriteSettled();
        expect(currentAttribution('A')).toBeNull();
        net.answer(net.count() - 1, forOp('A'));
        await done;
        expect(currentAttribution('A')).toEqual(forOp('A'));
    });

    it('AS2 an older response after a newer one is dropped', async () => {
        const older = refreshAttribution();
        const n = net.count();
        const newer = refreshAttribution();
        net.answer(n, { ...forOp('A'), myRig: 'NEW' });
        await newer;
        net.answer(n - 1, { ...forOp('A'), myRig: 'OLD' });
        await older;
        expect(currentAttribution('A')?.myRig).toBe('NEW');
    });

    it('AS3 a response after the binding became invalid is dropped', async () => {
        noteDisconnected(); // start from missing
        const done = refreshAttribution();
        bindingValid = false;
        net.answer(net.count() - 1, forOp('A'));
        await done;
        expect(currentAttribution('A')).toBeNull();
    });

    it('AS4 no refresh applies while another write is in flight', async () => {
        noteConfigWriteStarted();
        noteConfigWriteStarted();
        const before = net.count();
        void noteConfigWriteSettled();
        expect(net.count()).toBe(before);
        const two = noteConfigWriteSettled();
        net.answer(net.count() - 1, forOp('A'));
        await two;
        expect(currentAttribution('A')).toEqual(forOp('A'));
    });

    it('AS4 a write starting during a refresh drops that refresh', async () => {
        noteDisconnected();
        const r = refreshAttribution();
        noteConfigWriteStarted();
        net.answer(net.count() - 1, forOp('A'));
        await r;
        expect(currentAttribution('A')).toBeNull();
    });

    it('AS5 the boot read applies only when proven, undisturbed and for its operator', () => {
        _resetAttributionForTests();
        configure();
        applyBootAttribution(forOp('A'), attributionEpoch(), 'A');
        expect(currentAttribution('A')).toEqual(forOp('A'));
        expect(currentAttribution('B')).toBeNull();

        _resetAttributionForTests();
        configure();
        const since = attributionEpoch();
        noteConfigWriteStarted();
        void noteConfigWriteSettled();
        applyBootAttribution(forOp('A'), since, 'A');
        expect(currentAttribution('A')).toBeNull();

        _resetAttributionForTests();
        bindingValid = false;
        configure();
        applyBootAttribution(forOp('A'), attributionEpoch(), 'A');
        expect(currentAttribution('A')).toBeNull();
    });

    it('AS6 the settle and attributionSettled resolve only once current', async () => {
        noteConfigWriteStarted();
        let settled = false;
        const done = noteConfigWriteSettled().then(() => (settled = true));
        const held = attributionSettled();
        await Promise.resolve();
        expect(settled).toBe(false);
        net.answer(net.count() - 1, forOp('A'));
        await done;
        await held;
        expect(currentAttribution('A')).toEqual(forOp('A'));
    });

    it('AS7 a failed read stays missing until a proven reconnect re-reads', async () => {
        noteDisconnected();
        await readWith(null);
        expect(currentAttribution('A')).toBeNull();
        const r = noteReconnectProven();
        net.answer(net.count() - 1, forOp('A'));
        await r;
        expect(currentAttribution('A')).toEqual(forOp('A'));
    });

    it('AS8 the attribution is returned only for the operator it was read for', async () => {
        expect(currentAttribution('A')).toEqual(forOp('A'));
        expect(currentAttribution('B')).toBeNull();
        const r = setRequestedOperator('B');
        expect(currentAttribution('A')).toBeNull(); // invalidated at once
        expect(net.asked.at(-1)).toBe('B');
        net.answer(net.count() - 1, forOp('B'));
        await r;
        expect(currentAttribution('B')).toEqual(forOp('B'));
        expect(currentAttribution('A')).toBeNull();
    });

    it('AS9 a landed write with a lost response cannot pair A with B', async () => {
        // The Station save sending operator B is written, its response is lost,
        // and the reconciliation read fails — this page still sends A.
        noteConfigWriteStarted();
        const settled = noteConfigWriteSettled();
        expect(net.asked.at(-1)).toBe('A'); // read FOR the operator still sent
        net.answer(net.count() - 1, forOp('A'));
        await settled;
        expect(currentAttribution('A')?.operator).toBe('A');
        expect(currentAttribution('B')).toBeNull();
    });

    it('AS10 config.updated clears, drops the read in flight, and re-reads', async () => {
        const stale = refreshAttribution(); // a read already in flight
        const staleIdx = net.count() - 1;
        const fresh = noteConfigUpdated(); // another client changed the config
        expect(currentAttribution('A')).toBeNull();
        net.answer(staleIdx, { ...forOp('A'), myRig: 'BEFORE THE CHANGE' });
        await stale;
        expect(currentAttribution('A')).toBeNull(); // dropped
        net.answer(net.count() - 1, { ...forOp('A'), myRig: 'AFTER' });
        await fresh;
        expect(currentAttribution('A')?.myRig).toBe('AFTER');
    });

    it('AS11 a disconnect clears; every proven reconnect re-reads', async () => {
        const inFlight = refreshAttribution();
        noteDisconnected();
        expect(currentAttribution('A')).toBeNull();
        net.answer(net.count() - 1, forOp('A'));
        await inFlight;
        expect(currentAttribution('A')).toBeNull(); // dropped
        const asked = net.count();
        const r = noteReconnectProven();
        expect(net.count()).toBe(asked + 1);
        net.answer(net.count() - 1, { ...forOp('A'), myRig: 'AFTER RECONNECT' });
        await r;
        expect(currentAttribution('A')?.myRig).toBe('AFTER RECONNECT');
        const again = noteReconnectProven(); // even after a successful read
        expect(net.count()).toBe(asked + 2);
        net.answer(net.count() - 1, forOp('A'));
        await again;
    });

    it('AS12 an empty requested operator is passed on as empty', async () => {
        const r = setRequestedOperator('');
        expect(net.asked.at(-1)).toBe('');
        net.answer(net.count() - 1, forOp(''));
        await r;
        expect(currentAttribution('')).toEqual(forOp(''));
    });
});
