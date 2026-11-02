// W-0012 slice 2 — the live band-selection sequence (AC6; rulings D1–D6 and the
// review corrections of 2026-11-02). The sequence resolves a destination
// (slice 1), sends each write through the band-selection sender (the daemon
// stops TX first, ruling (a)), and releases the next write only on the rig's OWN
// report of the previous one, within the 2 s deadline after HTTP 202. Not wired
// to any control yet.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    selectBandLive,
    setBandSelectionSender,
    resetBandSequence,
    BAND_CONFIRM_DEADLINE_MS,
    type BandSelectionResult,
} from './bandSequence.svelte';
import type { MemoryLookup, Position } from './bandSelection';
import {
    rig,
    catLink,
    resetCatLink,
    setRigCaps,
    setModeMappings,
    setOperatingBands,
    setFt8Frequencies,
    setFt8Mode,
    type RigSendOutcome,
} from './rig.svelte';
import type { RigStatePayload } from '../api/rig-sse';
import { ft8State, resetFt8ForTests } from './ft8.svelte';

type Call = { op: string; value: string | number; reply: (o: RigSendOutcome) => void };
let calls: Call[] = [];

function sender(op: string, value: string | number): Promise<RigSendOutcome> {
    return new Promise((reply) => calls.push({ op, value, reply }));
}

const ACCEPTED: RigSendOutcome = { kind: 'accepted' };
const NO_MEMORY: MemoryLookup = () => undefined;
const memoryOf =
    (m: Record<string, Position>): MemoryLookup =>
    (b) =>
        m[b];

function report(p: RigStatePayload): void {
    catLink.onRigState(p);
}

async function flush(ms = 0): Promise<void> {
    await vi.advanceTimersByTimeAsync(ms);
}

/** Start a selection and collect its result when it settles. */
function start(
    target: Parameters<typeof selectBandLive>[1],
    memory: MemoryLookup = NO_MEMORY,
    view: Parameters<typeof selectBandLive>[0] = 'phone'
): { result: () => BandSelectionResult | undefined } {
    let out: BandSelectionResult | undefined;
    void selectBandLive(view, target, memory).then((r) => (out = r));
    return { result: () => out };
}

beforeEach(() => {
    vi.useFakeTimers();
    calls = [];
    resetCatLink();
    resetBandSequence();
    resetFt8ForTests();
    setBandSelectionSender(sender);
    setRigCaps({
        ops: ['set_freq', 'set_freq_b', 'set_mode'],
        tune: true,
        rigModes: ['LSB', 'USB', 'CW-U', 'CW-L', 'DATA-U'],
    });
    setModeMappings({
        LSB: { mode: 'SSB', submode: 'LSB' },
        USB: { mode: 'SSB', submode: 'USB' },
        'CW-U': { mode: 'CW' },
        'CW-L': { mode: 'CW' },
        'DATA-U': { mode: 'MFSK', submode: 'FT8' },
    });
    setOperatingBands(['80m', '40m', '20m', '15m']);
    setFt8Frequencies({ '40m': 7_074_000, '20m': 14_074_000 });
    setFt8Mode('DATA-U');
    // The rig is on 20m phone, VFO-A selected.
    report({ vfoA: 14_200_000, vfoB: 7_074_000, selectedVfo: 'A', mode: 'USB' });
});

afterEach(() => {
    setBandSelectionSender(null);
    resetBandSequence();
    resetCatLink();
    resetFt8ForTests();
    vi.useRealTimers();
});

describe('confirmation wait (D3 correction): the rig must report each step', () => {
    it('HTTP 202 without a matching frequency report does not release the mode write', async () => {
        start({ band: '40m' });
        await flush();
        expect(calls.map((c) => c.op)).toEqual(['set_freq']);
        calls[0].reply(ACCEPTED);
        await flush(BAND_CONFIRM_DEADLINE_MS - 100);
        expect(calls.map((c) => c.op)).toEqual(['set_freq']);
    });

    it('an unrelated report does not release it', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ mode: 'USB' });
        await flush(10);
        expect(calls.map((c) => c.op)).toEqual(['set_freq']);
        expect(sel.result()).toBeUndefined();
    });

    // A report of the same VFO at another value is not the write landing: the
    // step keeps waiting, and the real confirmation still releases the mode.
    it('a report of another frequency on the same VFO does not release it', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 14_201_000 });
        await flush(10);
        expect(sel.result()).toBeUndefined();
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls.map((c) => c.op)).toEqual(['set_freq', 'set_mode']);
    });

    it('a report of the destination on the other VFO does not release it', async () => {
        start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoB: 7_100_000 });
        await flush(10);
        expect(calls.map((c) => c.op)).toEqual(['set_freq']);
    });

    it('the matching report releases the mode write; the mode report completes it', async () => {
        const sel = start({ band: '40m' });
        await flush();
        expect(calls[0]).toMatchObject({ op: 'set_freq', value: 7_100_000 });
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_mode', value: 'LSB' });
        calls[1].reply(ACCEPTED);
        await flush();
        expect(sel.result()).toBeUndefined(); // 202 alone is not success
        report({ mode: 'LSB' });
        await flush();
        expect(sel.result()).toEqual({ status: 'done', band: '40m' });
    });

    it('no frequency report within the deadline: unknown, and the mode is never sent', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        await flush(BAND_CONFIRM_DEADLINE_MS + 1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'unknown' });
        expect(calls).toHaveLength(1);
    });

    it('no mode report within the deadline: unknown, not done', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        calls[1].reply(ACCEPTED);
        await flush(BAND_CONFIRM_DEADLINE_MS + 1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'unknown' });
    });

    it('VFO-B selected: writes set_freq_b and waits for the VFO-B report', async () => {
        report({ selectedVfo: 'B', vfoB: 14_250_000 });
        start({ band: '40m' });
        await flush();
        expect(calls[0]).toMatchObject({ op: 'set_freq_b', value: 7_100_000 });
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 }); // the other VFO
        await flush(10);
        expect(calls).toHaveLength(1);
        report({ vfoB: 7_100_000 });
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_mode', value: 'LSB' });
    });
});

describe('outcomes of a write end the sequence', () => {
    it.each([
        [
            'refused (a failed stop)',
            { kind: 'refused', message: 'could not stop transmitting; the rig was not retuned' },
            'refused',
        ],
        ['transport', { kind: 'transport', message: 'network down' }, 'transport'],
        ['timed out', { kind: 'timedOut', message: 'no response' }, 'unknown'],
    ] as const)('%s: no further write, failure returned', async (_l, outcome, kind) => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(outcome);
        await flush(BAND_CONFIRM_DEADLINE_MS + 1);
        expect(calls).toHaveLength(1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind });
    });

    it('a refused stop carries the daemon message', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply({ kind: 'refused', message: 'could not stop transmitting' });
        await flush();
        expect(sel.result()).toEqual({
            status: 'failed',
            kind: 'refused',
            message: 'could not stop transmitting',
        });
    });
});

describe('before any write', () => {
    it('TX alarm: refused, nothing sent', async () => {
        rig.txAlarmActive = true;
        const sel = start({ band: '40m' });
        await flush();
        expect(calls).toHaveLength(0);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'refused' });
    });

    it('TX alarm takes precedence over already satisfied', async () => {
        rig.txAlarmActive = true;
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'USB' } }));
        await flush();
        expect(calls).toHaveLength(0);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'refused' });
    });

    it('both targets confirmed: no write, no stop', async () => {
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'USB' } }));
        await flush();
        expect(calls).toHaveLength(0);
        expect(sel.result()).toEqual({ status: 'alreadySatisfied', band: '20m' });
    });

    it('already satisfied means confirmed, not the optimistic display', async () => {
        rig.modeLiteral = 'LSB'; // optimistic: the rig has not reported it
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'LSB' } }));
        await flush();
        expect(calls.map((c) => c.op)).toEqual(['set_mode']);
        expect(sel.result()).toBeUndefined();
    });

    it('frequency already there, mode differs: one band-selection mode write', async () => {
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'CW' } }));
        await flush();
        expect(calls[0]).toMatchObject({ op: 'set_mode', value: 'CW-U' });
        calls[0].reply(ACCEPTED);
        report({ mode: 'CW-U' });
        await flush();
        expect(sel.result()).toEqual({ status: 'done', band: '20m' });
    });

    it('a friendly mode the rig has no literal for: refused, nothing sent', async () => {
        const sel = start({ band: '40m' }, memoryOf({ '40m': { hz: 7_100_000, mode: 'AM' } }));
        await flush();
        expect(calls).toHaveLength(0);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'refused' });
    });

    it('no destination (60m): its message, nothing sent', async () => {
        const sel = start({ band: '60m' });
        await flush();
        expect(calls).toHaveLength(0);
        expect(sel.result()).toEqual({
            status: 'failed',
            kind: 'refused',
            message: 'No Phone/CW default configured for 60m.',
        });
    });

    it('CAT not connected: refused, nothing sent', async () => {
        rig.cat = 'off';
        const sel = start({ band: '40m' });
        await flush();
        expect(calls).toHaveLength(0);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'refused' });
    });

    it('FT8: the dial, then the configured data literal', async () => {
        start({ band: '40m' }, NO_MEMORY, 'ft8');
        await flush();
        expect(calls[0]).toMatchObject({ op: 'set_freq', value: 7_074_000 });
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_074_000 });
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_mode', value: 'DATA-U' });
    });

    it('FT8 with an empty data literal leaves the mode alone', async () => {
        setFt8Mode('');
        const sel = start({ band: '40m' }, NO_MEMORY, 'ft8');
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_074_000 });
        await flush();
        expect(calls).toHaveLength(1);
        expect(sel.result()).toEqual({ status: 'done', band: '40m' });
    });
});

describe('rechecks before the mode write', () => {
    async function frequencyConfirmed(): Promise<{
        result: () => BandSelectionResult | undefined;
    }> {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        return sel;
    }

    it.each([
        ['FT armed again', () => (ft8State.tx.armed = true)],
        ['FT transmitting', () => (ft8State.tx.transmitting = true)],
        ['an FT contact active', () => (ft8State.qso.active = true)],
        ['the tune carrier up', () => (rig.tuneActive = true)],
        ['the TX alarm raised', () => (rig.txAlarmActive = true)],
    ])('%s: the mode write is abandoned', async (_l, change) => {
        const sel = await frequencyConfirmed();
        change();
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls).toHaveLength(1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
    });

    it('a VFO switch invalidates the rest of the sequence', async () => {
        const sel = await frequencyConfirmed();
        report({ vfoA: 7_100_000, selectedVfo: 'B' });
        await flush();
        expect(calls).toHaveLength(1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
    });

    it('a dial change after confirmation invalidates it', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        // Both reports land before the sequence resumes: confirmed, then moved.
        report({ vfoA: 7_100_000 });
        report({ vfoA: 7_101_000 });
        await flush();
        expect(calls).toHaveLength(1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
    });

    it('only the two band-selection ops are ever sent (nothing arms)', async () => {
        const sel = await frequencyConfirmed();
        report({ vfoA: 7_100_000 });
        await flush();
        calls[1].reply(ACCEPTED);
        report({ mode: 'LSB' });
        await flush();
        expect(sel.result()).toEqual({ status: 'done', band: '40m' });
        expect(calls.map((c) => c.op)).toEqual(['set_freq', 'set_mode']);
        expect(ft8State.tx.armed).toBe(false);
    });
});

// Codex review of 18994842 (2026-11-02), two P2s: the mode decision must not go
// stale across the frequency write, a queued frequency write must still target
// the selected VFO, and success needs the whole destination confirmed.
describe('state that changes while a write is pending', () => {
    it('the mode changed during the frequency POST: the mode is re-evaluated and written', async () => {
        const sel = start({ band: '15m' }); // USB, like the current 20m USB
        await flush();
        expect(calls.map((c) => c.op)).toEqual(['set_freq']);
        report({ mode: 'LSB' }); // the operator turned the mode knob
        calls[0].reply(ACCEPTED);
        report({ vfoA: 21_300_000 });
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_mode', value: 'USB' });
        expect(sel.result()).toBeUndefined();
        calls[1].reply(ACCEPTED);
        report({ mode: 'USB' });
        await flush();
        expect(sel.result()).toEqual({ status: 'done', band: '15m' });
    });

    it('a queued frequency write is not dispatched after the VFO changed', async () => {
        start({ band: '40m' });
        await flush();
        const queued = start({ band: '15m' }); // waits behind 40m's request
        await flush();
        report({ selectedVfo: 'B' });
        await flush();
        calls[0].reply(ACCEPTED);
        await flush();
        expect(calls).toHaveLength(1);
        expect(queued.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
    });

    it('the dial moved while the mode was awaited: not done', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        calls[1].reply(ACCEPTED);
        report({ vfoA: 7_101_000 });
        report({ mode: 'LSB' });
        await flush();
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
    });
});

describe('ordering and supersession (D5/D6)', () => {
    it('a newer selection replaces the pending one, which is reported', async () => {
        const first = start({ band: '40m' });
        await flush();
        start({ band: '15m' });
        calls[0].reply(ACCEPTED);
        await flush();
        expect(first.result()).toEqual({
            status: 'superseded',
            message: 'Band selection to 40m replaced by a newer selection.',
        });
        expect(calls.map((c) => c.op)).not.toContain('set_mode');
    });

    it('a replaced selection whose frequency step completed says so', async () => {
        const first = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls[1].op).toBe('set_mode'); // in flight
        start({ band: '15m' });
        calls[1].reply(ACCEPTED);
        await flush();
        expect(first.result()).toEqual({
            status: 'superseded',
            message:
                'Band selection to 40m replaced by a newer selection. Its frequency change had reached the rig.',
        });
    });

    it('selections are serialised: the next write waits for the in-flight request', async () => {
        start({ band: '40m' });
        await flush();
        start({ band: '15m' });
        await flush();
        expect(calls).toHaveLength(1); // 40m's request has not returned
        calls[0].reply(ACCEPTED);
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_freq', value: 21_300_000 });
    });

    it('two rapid next presses advance two eligible bands', async () => {
        start({ step: 1 }); // 20m → 15m
        await flush();
        start({ step: 1 }); // from the pending 15m → 80m (wrap)
        calls[0].reply(ACCEPTED);
        await flush();
        expect(calls[0]).toMatchObject({ value: 21_300_000 });
        expect(calls[1]).toMatchObject({ op: 'set_freq', value: 3_700_000 });
    });

    it('20m → pending 40m → 20m: no "already satisfied" from the old 20m report', async () => {
        const memory = memoryOf({ '20m': { hz: 14_200_000, mode: 'USB' } });
        start({ band: '40m' }, memory);
        await flush();
        const back = start({ band: '20m' }, memory);
        calls[0].reply(ACCEPTED); // the 40m write reached the daemon; no report yet
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_freq', value: 14_200_000 });
        expect(back.result()).toBeUndefined();
    });

    it.each([
        ['a timed-out request', { kind: 'timedOut', message: 'no response' }],
        ['a transport failure', { kind: 'transport', message: 'connection reset' }],
        ['a 202 never reported within the deadline', { kind: 'accepted' }],
    ] as const)('%s stays unresolved for the next selection', async (_l, outcome) => {
        const memory = memoryOf({ '20m': { hz: 14_200_000, mode: 'USB' } });
        start({ band: '40m' }, memory);
        await flush();
        calls[0].reply(outcome);
        await flush(BAND_CONFIRM_DEADLINE_MS + 1);
        const back = start({ band: '20m' }, memory);
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_freq', value: 14_200_000 });
        expect(back.result()).toBeUndefined();
    });

    it('a queued selection superseded before sending never sends', async () => {
        start({ band: '40m' });
        await flush();
        const queued = start({ band: '15m' }); // waits behind 40m's request
        await flush();
        start({ band: '80m' });
        calls[0].reply(ACCEPTED);
        await flush();
        expect(calls.map((c) => c.value)).toEqual([7_100_000, 3_700_000]);
        expect(queued.result()).toEqual({
            status: 'superseded',
            message: 'Band selection to 15m replaced by a newer selection.',
        });
    });

    it('an unresolved write is resolved by the rig reporting its value', async () => {
        const memory = memoryOf({ '40m': { hz: 7_100_000, mode: 'LSB' } });
        start({ band: '40m' }, memory);
        await flush();
        calls[0].reply(ACCEPTED);
        await flush(BAND_CONFIRM_DEADLINE_MS + 1); // unknown
        report({ vfoA: 7_100_000, mode: 'LSB' }); // it landed late
        const again = start({ band: '40m' }, memory);
        await flush();
        expect(calls).toHaveLength(1);
        expect(again.result()).toEqual({ status: 'alreadySatisfied', band: '40m' });
    });

    it('a dropped link clears confirmed state: no "already satisfied" after it', async () => {
        catLink.onTransportError();
        report({ selectedVfo: 'A' }); // link back; no dial or mode reported yet
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'USB' } }));
        await flush();
        expect(calls.map((c) => c.op)).toEqual(['set_freq']);
        expect(sel.result()).toBeUndefined();
    });
});

// Ruling (a) on the recheck race (2026-11-02): a selection made while FT8 may be
// keyed waits — concurrently with the frequency confirmation and to the same
// absolute deadline (first write's 202 + 2 s) — for the complete stopped state
// the daemon's own stop produces, observed from before the first write.
describe('stop observation when FT8 was keyed at the start', () => {
    const STOP_EXPIRED =
        'Frequency changed, but transmit-stop confirmation did not arrive; mode was not set.';

    beforeEach(() => {
        ft8State.tx.armed = true;
    });

    function disarm(): void {
        ft8State.tx.armed = false;
        ft8State.tx.transmitting = false;
    }

    it('frequency report first, then the stop: the mode follows the stop', async () => {
        start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls).toHaveLength(1);
        disarm();
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_mode', value: 'LSB' });
    });

    it('stop first, then the frequency report', async () => {
        start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        disarm();
        await flush();
        expect(calls).toHaveLength(1);
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_mode', value: 'LSB' });
    });

    it('stop events that arrive before the 202 are observed', async () => {
        start({ band: '40m' });
        await flush();
        disarm(); // during the POST
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_mode', value: 'LSB' });
    });

    it('disarmed while the contact still shows active is not stopped', async () => {
        ft8State.qso.active = true;
        start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        disarm();
        await flush();
        expect(calls).toHaveLength(1);
        ft8State.qso.active = false;
        await flush();
        expect(calls[1]).toMatchObject({ op: 'set_mode', value: 'LSB' });
    });

    it('deadline expiry: no mode, the agreed message; a late stop does not resume', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush(BAND_CONFIRM_DEADLINE_MS + 1);
        expect(sel.result()).toEqual({ status: 'failed', kind: 'unknown', message: STOP_EXPIRED });
        disarm();
        await flush(BAND_CONFIRM_DEADLINE_MS);
        expect(calls).toHaveLength(1);
    });

    it('one absolute deadline: a late frequency report does not restart the allowance', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        await flush(1500);
        report({ vfoA: 7_100_000 });
        await flush(600); // 2.1 s after the 202
        expect(sel.result()).toEqual({ status: 'failed', kind: 'unknown', message: STOP_EXPIRED });
        disarm();
        await flush();
        expect(calls).toHaveLength(1);
    });

    it('supersession during the wait ends it', async () => {
        const first = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        start({ band: '15m' });
        await flush();
        expect(first.result()).toEqual({
            status: 'superseded',
            message:
                'Band selection to 40m replaced by a newer selection. Its frequency change had reached the rig.',
        });
        expect(calls.map((c) => c.op)).not.toContain('set_mode');
    });

    it('a re-arm after the stop was observed invalidates the sequence', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        disarm();
        await flush();
        ft8State.tx.armed = true; // the operator re-armed
        await flush();
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls).toHaveLength(1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
    });

    // The latch, not a live read: TX that started and ended again after the stop
    // was observed still invalidates the sequence.
    it('a brief re-arm after the stop invalidates it even once disarmed again', async () => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        disarm();
        await flush();
        ft8State.tx.armed = true;
        await flush();
        disarm();
        await flush();
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls).toHaveLength(1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
    });

    // Review 2026-11-02: history is recorded for the whole selection, including
    // while the first POST is pending and no confirmation is being awaited.
    it('disarm → re-arm → disarm during the pending POST invalidates it', async () => {
        const sel = start({ band: '40m' });
        await flush(); // the frequency POST is pending
        disarm();
        await flush();
        ft8State.tx.armed = true;
        await flush();
        disarm();
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        expect(calls).toHaveLength(1);
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
    });

    it('mode-only: confirmed mode without the stop → "Mode changed…"; a late stop does not resume', async () => {
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'CW' } }));
        await flush();
        calls[0].reply(ACCEPTED);
        report({ mode: 'CW-U' });
        await flush(BAND_CONFIRM_DEADLINE_MS + 1);
        expect(sel.result()).toEqual({
            status: 'failed',
            kind: 'unknown',
            message: 'Mode changed, but transmit-stop confirmation did not arrive.',
        });
        disarm();
        await flush();
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'unknown' });
        expect(calls).toHaveLength(1);
    });

    it('mode-only: no mode report → the generic unknown message', async () => {
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'CW' } }));
        await flush();
        calls[0].reply(ACCEPTED);
        disarm();
        await flush(BAND_CONFIRM_DEADLINE_MS + 1);
        expect(sel.result()).toEqual({
            status: 'failed',
            kind: 'unknown',
            message: "Couldn't confirm the band change to 20m — check the rig.",
        });
    });

    it.each([
        ['a lost connection', () => catLink.onTransportError()],
        ['a TX alarm', () => (rig.txAlarmActive = true)],
    ])('%s ends the wait, with no mode write', async (_l, end) => {
        const sel = start({ band: '40m' });
        await flush();
        calls[0].reply(ACCEPTED);
        report({ vfoA: 7_100_000 });
        await flush();
        end();
        await flush();
        expect(sel.result()).toMatchObject({ status: 'failed', kind: 'invalidated' });
        disarm();
        await flush();
        expect(calls).toHaveLength(1);
    });

    it('a mode-only selection completes only once the stop is observed', async () => {
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'CW' } }));
        await flush();
        expect(calls[0]).toMatchObject({ op: 'set_mode', value: 'CW-U' });
        calls[0].reply(ACCEPTED);
        report({ mode: 'CW-U' });
        await flush();
        expect(sel.result()).toBeUndefined();
        disarm();
        await flush();
        expect(sel.result()).toEqual({ status: 'done', band: '20m' });
    });

    it('the true no-op writes nothing, stops nothing and does not wait', async () => {
        const sel = start({ band: '20m' }, memoryOf({ '20m': { hz: 14_200_000, mode: 'USB' } }));
        await flush();
        expect(calls).toHaveLength(0);
        expect(sel.result()).toEqual({ status: 'alreadySatisfied', band: '20m' });
        expect(ft8State.tx.armed).toBe(true);
    });
});

// Ruling 2026-11-02: neither the default A nor an optimistic selection says
// which physical VFO to write.
describe('unknown selected VFO', () => {
    it('at startup: refused, zero commands', async () => {
        resetCatLink();
        setRigCaps({
            ops: ['set_freq', 'set_freq_b', 'set_mode'],
            tune: true,
            rigModes: ['LSB', 'USB', 'CW-U', 'CW-L', 'DATA-U'],
        });
        report({ vfoA: 14_200_000, mode: 'USB' }); // no selectedVfo yet
        const sel = start({ band: '40m' });
        await flush();
        expect(calls).toHaveLength(0);
        expect(sel.result()).toEqual({
            status: 'failed',
            kind: 'refused',
            message: 'The rig has not reported its selected VFO yet.',
        });
    });

    it('after a reconnect: refused until a fresh selected-VFO report arrives', async () => {
        catLink.onTransportError();
        report({ vfoA: 14_200_000, mode: 'USB' });
        const refused = start({ band: '40m' });
        await flush();
        expect(calls).toHaveLength(0);
        expect(refused.result()).toMatchObject({ status: 'failed', kind: 'refused' });

        report({ selectedVfo: 'A' });
        start({ band: '40m' });
        await flush();
        expect(calls[0]).toMatchObject({ op: 'set_freq', value: 7_100_000 });
    });
});
