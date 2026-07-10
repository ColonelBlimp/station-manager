// FT8 state-machine tests — the transitions the SSE transport feeds (transport
// routing itself is covered in ft8-sse.test.ts). Decode feed accumulate/single/
// cap, slot heartbeat, tx/qso mirrors, and the view-scoped lifecycle.

import { describe, it, expect, beforeEach } from 'vitest';
import {
    ft8State,
    ft8Link,
    setFt8DisplayPrefs,
    setFt8LoggedSink,
    setFt8Transport,
    setFt8TxActions,
    armTx,
    callCq,
    answerCq,
    workCaller,
    abandonQso,
    startFt8,
    stopFt8,
    resetFt8ForTests,
    type Ft8TxActions,
} from './ft8.svelte';
import type { DecodeReport } from '../api/ft8-sse';

beforeEach(() => {
    resetFt8ForTests();
});

function decodeSlot(startUtc: string, lines: { text: string; freq_hz: number; snr: number }[]): DecodeReport {
    return {
        slot: { start_utc: startUtc, period: 'even' },
        decodes: lines.map((l) => ({ ...l, dt_s: 0.1 })),
    };
}

describe('decode feed', () => {
    it('accumulates newest-slot-first, frequency-ascending within a slot', () => {
        ft8Link.onDecode(
            decodeSlot('t1', [
                { text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -12 },
                { text: 'CQ JA1XYZ PM95', freq_hz: 800, snr: -15 },
            ])
        );
        // Within-slot: freq-ascending (800 before 1500).
        expect(ft8State.decodes.map((d) => d.freqHz)).toEqual([800, 1500]);

        ft8Link.onDecode(decodeSlot('t2', [{ text: 'CQ VE3XYZ FN03', freq_hz: 1660, snr: -9 }]));
        // Newest slot on top.
        expect(ft8State.decodes.map((d) => d.text)).toEqual([
            'CQ VE3XYZ FN03',
            'CQ JA1XYZ PM95',
            'CQ W1ABC FN42',
        ]);
    });

    it('single feed mode shows only the latest slot', () => {
        setFt8DisplayPrefs({ feedMode: 'single' });
        ft8Link.onDecode(decodeSlot('t1', [{ text: 'A', freq_hz: 1000, snr: -1 }]));
        ft8Link.onDecode(decodeSlot('t2', [{ text: 'B', freq_hz: 1000, snr: -1 }]));
        expect(ft8State.decodes.map((d) => d.text)).toEqual(['B']);
    });

    it('caps the feed at historyMax', () => {
        setFt8DisplayPrefs({ historyMax: 3 });
        for (let i = 0; i < 5; i++) {
            ft8Link.onDecode(decodeSlot(`t${i}`, [{ text: `m${i}`, freq_hz: 1000, snr: -1 }]));
        }
        expect(ft8State.decodes).toHaveLength(3);
        expect(ft8State.decodes[0].text).toBe('m4'); // newest kept
    });

    it('a silent slot advances the slot clock but adds no rows', () => {
        ft8Link.onDecode(decodeSlot('t1', [{ text: 'A', freq_hz: 1000, snr: -1 }]));
        ft8Link.onDecode({ slot: { start_utc: 't2', period: 'odd' }, decodes: null });
        expect(ft8State.slot?.start_utc).toBe('t2'); // heartbeat advanced
        expect(ft8State.decodes.map((d) => d.text)).toEqual(['A']); // unchanged
    });

    it('assigns unique keyed-each ids across slots', () => {
        ft8Link.onDecode(decodeSlot('t1', [{ text: 'A', freq_hz: 1000, snr: -1 }]));
        ft8Link.onDecode(decodeSlot('t2', [{ text: 'B', freq_hz: 1000, snr: -1 }]));
        const ids = ft8State.decodes.map((d) => d.id);
        expect(new Set(ids).size).toBe(ids.length);
    });
});

describe('occupancy + status mirrors', () => {
    it('onOccupancy fills passband, busy bands and clear offsets', () => {
        ft8Link.onOccupancy({
            slot: { start_utc: 't', period: 'even' },
            passband: { low_hz: 300, high_hz: 2800 },
            signal_width_hz: 60,
            occupied: [{ low_hz: 1000, high_hz: 1050 }],
            suggested: [1500, 700],
        });
        expect(ft8State.passbandLow).toBe(300);
        expect(ft8State.passbandHigh).toBe(2800);
        expect(ft8State.signalWidth).toBe(60);
        expect(ft8State.occupied).toHaveLength(1);
        expect(ft8State.suggested).toEqual([1500, 700]);
    });

    it('onTx / onQso mirror the wire payload (snake → camel)', () => {
        ft8Link.onTx({ armed: true, transmitting: true, offset_hz: 1500, message: 'K1ABC PJ4 R-08' });
        expect(ft8State.tx.armed).toBe(true);
        expect(ft8State.tx.offsetHz).toBe(1500);

        ft8Link.onQso({ active: true, role: 'answerer', their_call: 'PJ4/NA2AA', max_repeats: 6 });
        expect(ft8State.qso.theirCall).toBe('PJ4/NA2AA');
        expect(ft8State.qso.maxRepeats).toBe(6);
    });

    it('onLogged routes to the injected sink', () => {
        const seen: string[] = [];
        setFt8LoggedSink((p) => seen.push(p.uuid ?? ''));
        ft8Link.onLogged({ uuid: 'u-1', callsign: 'PJ4/NA2AA' });
        expect(seen).toEqual(['u-1']);
    });
});

describe('effectiveOffset', () => {
    it('prefers the operator pick, else the daemon top clear offset, else null', () => {
        expect(ft8State.effectiveOffset).toBeNull(); // nothing known yet
        ft8State.suggested = [1400, 900];
        expect(ft8State.effectiveOffset).toBe(1400); // daemon's best
        ft8State.selectedOffset = 2100;
        expect(ft8State.effectiveOffset).toBe(2100); // explicit pick wins
    });

    it('survives a view toggle (stopFt8 keeps the operator pick, clears stream data)', () => {
        ft8State.selectedOffset = 1750;
        ft8State.suggested = [800];
        stopFt8();
        expect(ft8State.selectedOffset).toBe(1750); // pick retained
        expect(ft8State.suggested).toEqual([]); // stream data cleared
    });
});

describe('TX action wrappers', () => {
    function recorder(): { calls: string[]; actions: Ft8TxActions } {
        const calls: string[] = [];
        const ok = Promise.resolve({ ok: true, message: '' });
        return {
            calls,
            actions: {
                arm: (a) => (calls.push(`arm:${a}`), ok),
                callCq: (o, f, p) => (calls.push(`cq:${o}:${f}:${p}`), ok),
                answerCq: (a) => (calls.push(`answer:${a.theirCall}:${a.fd}`), ok),
                workCaller: (a) => (calls.push(`work:${a.theirCall}`), ok),
                abandon: () => (calls.push('abandon'), ok),
            },
        };
    }

    it('forward to the injected actions', async () => {
        const { calls, actions } = recorder();
        setFt8TxActions(actions);
        await armTx(true);
        await callCq(1500, 14.074, 'odd');
        await answerCq({
            theirCall: 'W1ABC',
            theirGrid: 'FN42',
            slotUtc: 't',
            offsetHz: 1500,
            opFreqMHz: 14.074,
            fd: false,
            theirSnr: -12,
        });
        await workCaller({
            theirCall: 'DL9UW',
            theirGrid: 'JO21',
            theirSnr: -5,
            slotUtc: 't',
            offsetHz: 1500,
            opFreqMHz: 14.074,
        });
        await abandonQso();
        expect(calls).toEqual([
            'arm:true',
            'cq:1500:14.074:odd',
            'answer:W1ABC:false',
            'work:DL9UW',
            'abandon',
        ]);
    });

    it('return an unavailable result when no actions are wired', async () => {
        const r = await armTx(true); // resetFt8ForTests cleared the seam
        expect(r.ok).toBe(false);
        expect(r.message).toMatch(/unavailable/i);
    });
});

describe('view-scoped lifecycle', () => {
    it('startFt8 opens via the injected transport once; stopFt8 closes + clears', () => {
        let opened = 0;
        let closed = 0;
        setFt8Transport((h) => {
            opened++;
            h.onOpen();
            return () => closed++;
        });

        startFt8();
        startFt8(); // idempotent — no second open
        expect(opened).toBe(1);
        expect(ft8State.connected).toBe(true);

        // Seed some volatile state, then leave the view.
        ft8Link.onDecode(decodeSlot('t1', [{ text: 'A', freq_hz: 1000, snr: -1 }]));
        stopFt8();
        expect(closed).toBe(1);
        expect(ft8State.connected).toBe(false);
        expect(ft8State.decodes).toEqual([]);
        expect(ft8State.slot).toBeNull();
    });

    it('startFt8 is a no-op with no transport injected', () => {
        startFt8();
        expect(ft8State.connected).toBe(false);
    });
});
