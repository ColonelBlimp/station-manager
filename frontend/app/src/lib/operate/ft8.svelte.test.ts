// FT8 state-machine tests — the transitions the SSE transport feeds (transport
// routing itself is covered in ft8-sse.test.ts). Decode feed accumulate/single/
// cap, slot heartbeat, tx/qso mirrors, and the view-scoped lifecycle.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
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
    setFt8SessionEndedSink,
} from './ft8.svelte';
import type { DecodeReport } from '../api/ft8-sse';

beforeEach(() => {
    resetFt8ForTests();
});

function decodeSlot(
    startUtc: string,
    lines: { text: string; freq_hz: number; snr: number }[]
): DecodeReport {
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

    // Review P1 (2026-08-07): a report captured before a QSY can ARRIVE after
    // the band-change watcher cleared the feed — publication lags capture by
    // the decode (~0.7–1.6 s) — and its rows would repopulate the new band's
    // view with the old band's stations. The report now stamps the dial its
    // slot was captured on; rows join the feed only when that dial's band
    // matches the band the view is on. The heartbeat still ticks either way,
    // and a report with no stamp (older daemon, no CAT) keeps today's
    // fail-open display behaviour.
    it('drops a late report whose capture dial belongs to another band', () => {
        ft8State.noteOperatingBand('15m'); // the view is on 15m after a QSY

        ft8Link.onDecode({
            slot: { start_utc: 't-late', period: 'even' },
            dial_mhz: 14.074, // captured before the QSY, on 20m
            decodes: [{ text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -12, dt_s: 0.1 }],
        });
        expect(ft8State.decodes).toEqual([]);
        expect(ft8State.slot?.start_utc).toBe('t-late'); // slot clock still ticks

        ft8Link.onDecode({
            slot: { start_utc: 't-ok', period: 'odd' },
            dial_mhz: 21.074, // captured on the band the view is on
            decodes: [{ text: 'CQ JA1XYZ PM95', freq_hz: 800, snr: -15, dt_s: 0.1 }],
        });
        expect(ft8State.decodes.map((d) => d.text)).toEqual(['CQ JA1XYZ PM95']);

        ft8Link.onDecode({
            slot: { start_utc: 't-nodial', period: 'even' },
            decodes: [{ text: 'CQ VE3XYZ FN03', freq_hz: 1660, snr: -9, dt_s: 0.1 }],
        });
        expect(ft8State.decodes.map((d) => d.text)).toContain('CQ VE3XYZ FN03');
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
        ft8Link.onTx({
            armed: true,
            transmitting: true,
            offset_hz: 1500,
            message: 'K1ABC PJ4 R-08',
        });
        expect(ft8State.tx.armed).toBe(true);
        expect(ft8State.tx.offsetHz).toBe(1500);

        ft8Link.onQso({ active: true, role: 'answerer', their_call: 'PJ4/NA2AA', max_repeats: 6 });
        expect(ft8State.qso.theirCall).toBe('PJ4/NA2AA');
        expect(ft8State.qso.maxRepeats).toBe(6);
    });

    // ADR 0065 operator_pick: the candidate list the pile-up drawer renders comes
    // from these two frame fields — daemon rules in internal/ft8/operatorpick_test.go.
    it('onQso maps answer_mode + the operator_pick answerer list', () => {
        ft8Link.onQso({
            active: true,
            role: 'caller',
            state: 'calling-cq',
            answer_mode: 'operator_pick',
            answerers: [{ call: 'DL9UW', snr: -8 }],
        });
        expect(ft8State.qso.answerMode).toBe('operator_pick');
        expect(ft8State.qso.answerers).toEqual([{ call: 'DL9UW', snr: -8 }]);

        // Absent on the wire (idle frames, auto-mode runs) → empty, never stale:
        // a terminal frame must clear the drawer with the run (daemon rule 10).
        ft8Link.onQso({ active: false });
        expect(ft8State.qso.answerMode).toBe('');
        expect(ft8State.qso.answerers).toEqual([]);
    });

    it('onLogged routes to the injected sink', () => {
        const seen: string[] = [];
        setFt8LoggedSink((p) => seen.push(p.uuid ?? ''));
        ft8Link.onLogged({ uuid: 'u-1', callsign: 'PJ4/NA2AA' });
        expect(seen).toEqual(['u-1']);
    });
});

describe('parity-aware occupancy', () => {
    function occ(
        period: 'even' | 'odd',
        occupied: { low_hz: number; high_hz: number }[],
        suggested: number[]
    ) {
        return {
            slot: { start_utc: 't', period },
            passband: { low_hz: 200, high_hz: 3000 },
            signal_width_hz: 50,
            occupied,
            suggested,
        };
    }

    it('keeps even + odd snapshots apart and shows the manual pick when idle', () => {
        ft8Link.onOccupancy(occ('even', [{ low_hz: 1000, high_hz: 1050 }], [1500]));
        ft8Link.onOccupancy(occ('odd', [], [2000]));

        expect(ft8State.shownParity).toBe('even'); // idle default
        expect(ft8State.occupied).toHaveLength(1);
        expect(ft8State.suggested).toEqual([1500]);

        ft8State.setOccupancyParity('odd');
        expect(ft8State.shownParity).toBe('odd');
        expect(ft8State.occupied).toEqual([]);
        expect(ft8State.suggested).toEqual([2000]);
        expect(ft8State.effectiveOffset).toBe(2000); // auto-pick follows the shown parity
    });

    it('during a QSO the shown parity LOCKS to the TX slot (opposite the worked station)', () => {
        ft8Link.onOccupancy(occ('even', [], [1500]));
        ft8Link.onOccupancy(occ('odd', [], [2000]));
        ft8State.setOccupancyParity('even'); // manual pick would be even…

        // …but their CQ is on even → we TX on odd → show odd, toggle locked.
        ft8Link.onQso({
            active: true,
            role: 'answerer',
            their_call: 'W1ABC',
            their_period: 'even',
        });
        expect(ft8State.occupancyParityLocked).toBe(true);
        expect(ft8State.shownParity).toBe('odd');
        expect(ft8State.suggested).toEqual([2000]); // odd snapshot, not the manual even
    });

    it('hasOccupancy is false until the SHOWN parity has a snapshot', () => {
        expect(ft8State.hasOccupancy).toBe(false); // nothing yet (shown even)
        ft8Link.onOccupancy(occ('odd', [], [2000])); // only odd arrived
        expect(ft8State.hasOccupancy).toBe(false); // still showing even → no snapshot
        ft8State.setOccupancyParity('odd');
        expect(ft8State.hasOccupancy).toBe(true);
    });
});

describe('effectiveOffset', () => {
    it('prefers the operator pick, else the daemon top clear offset, else null', () => {
        expect(ft8State.effectiveOffset).toBeNull(); // nothing known yet
        ft8State.suggestedByParity.even = [1400, 900]; // idle shows the even snapshot
        expect(ft8State.effectiveOffset).toBe(1400); // daemon's best
        ft8State.selectedOffset = 2100;
        expect(ft8State.effectiveOffset).toBe(2100); // explicit pick wins
    });

    it('survives a view toggle (stopFt8 keeps the operator pick, clears stream data)', () => {
        ft8State.selectedOffset = 1750;
        ft8State.suggestedByParity.even = [800];
        stopFt8();
        expect(ft8State.selectedOffset).toBe(1750); // pick retained
        expect(ft8State.suggested).toEqual([]); // stream data cleared
    });

    it('selectOffset pins the pick, ending the auto fallback', () => {
        ft8State.suggestedByParity.even = [900];
        expect(ft8State.effectiveOffset).toBe(900); // auto
        ft8State.selectOffset(1234);
        expect(ft8State.selectedOffset).toBe(1234);
        expect(ft8State.effectiveOffset).toBe(1234); // pinned
    });

    it('persists the pick to localStorage so it survives a page reload (daemon redeploy)', () => {
        ft8State.selectOffset(1234);
        expect(localStorage.getItem('sm.ft8.selectedOffset')).toBe('1234');
        // The reset seam clears it too, so a fresh session starts on auto.
        resetFt8ForTests();
        expect(localStorage.getItem('sm.ft8.selectedOffset')).toBeNull();
    });
});

describe('occupancy view toggle', () => {
    it('defaults to spectrum and switches presentation without touching the pick', () => {
        expect(ft8State.occupancyView).toBe('spectrum'); // operator default, 2026-07-13
        ft8State.selectedOffset = 1500;
        ft8State.setOccupancyView('channels');
        expect(ft8State.occupancyView).toBe('channels');
        expect(ft8State.selectedOffset).toBe(1500); // view choice ≠ offset pick
    });
});

describe('TX action wrappers', () => {
    function recorder(): { calls: string[]; actions: Ft8TxActions } {
        const calls: string[] = [];
        const ok = Promise.resolve({ ok: true, message: '' });
        return {
            calls,
            actions: {
                arm: (a) => (
                    calls.push(`arm:${a}`),
                    Promise.resolve({ kind: 'accepted' as const })
                ),
                callCq: (o, f, p) => (calls.push(`cq:${o}:${f}:${p}`), ok),
                answerCq: (a) => (calls.push(`answer:${a.theirCall}:${a.fd}`), ok),
                workCaller: (a) => (calls.push(`work:${a.theirCall}`), ok),
                abandon: () => (calls.push('abandon'), ok),
                next: () => (calls.push('next'), ok),
                stopAutoWork: () => (calls.push('stopAutoWork'), ok),
                pickAnswerer: () => (calls.push('pickAnswerer'), ok),
                bagAnswerer: (c) => (calls.push(`bag:${c}`), ok),
                unbagAnswerer: (c) => (calls.push(`unbag:${c}`), ok),
                resumeDrain: () => (calls.push('resumeDrain'), ok),
                skip: (a) => (calls.push(`skip:${a}`), ok),
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
        expect(r.status).toBe('failed');
        if (r.status !== 'failed') return;
        expect(r.kind).toBe('transport'); // nothing was sent — not a daemon refusal
        expect(r.message).toMatch(/unavailable/i);
    });
});

/*
    F-04 confirm-by-push for the FT8 arm (ADR 0078; operator-ratified 2026-09-05).
    Arming grants permission to key but transmits nothing; the daemon 202s the
    intent and the authoritative state arrives on the ft8-tx SSE. A fired timeout
    is therefore outcome-UNKNOWN, reconciled against the next VALID ft8-tx frame
    matching the requested state — never a definite failure, never a claim of
    success. The control has NO optimistic state: it renders only pushed state,
    so a timed-out disarm can never show TX as down.

    Result (FT8-specific, deliberately smaller than the rig family's — no
    alreadySatisfied, no rollback): accepted (a 202) | observed (timeout, then a
    matching frame inside the 2 s grace) | unknown (grace exhausted) |
    failed{refused|transport} | superseded (a newer opposite request, silent).
    Every terminal result cancels the watch and its timer.

    Fake timers: only a FIRED timeout ever enters the grace; a prompt 202,
    refusal, transport failure or supersession resolves without the clock.
*/
describe('FT8 arm confirm-by-push (F-04)', () => {
    const ENABLE_UNKNOWN =
        "Couldn't confirm that FT8 TX was enabled. The control will update when Station Manager reports its state.";
    const DISABLE_UNKNOWN =
        "Couldn't confirm that FT8 TX was disabled. Check the radio; the control will update when Station Manager reports its state.";

    beforeEach(() => {
        vi.useFakeTimers();
    });
    afterEach(() => {
        vi.useRealTimers();
    });

    /** Wire a seam whose arm returns `outcome`; EVERY other action records 'OTHER'
     *  so the seam-isolation rule can assert arming touches no transmit path. */
    function armSeam(outcome: () => Promise<unknown>): string[] {
        const calls: string[] = [];
        const other = (): Promise<{ ok: boolean; message: string }> => {
            calls.push('OTHER');
            return Promise.resolve({ ok: true, message: '' });
        };
        setFt8TxActions({
            arm: (a) => {
                calls.push(`arm:${a}`);
                return outcome() as Promise<never>;
            },
            callCq: other,
            answerCq: other,
            workCaller: other,
            abandon: other,
            skip: other,
            next: other,
            stopAutoWork: other,
            pickAnswerer: other,
            bagAnswerer: other,
            unbagAnswerer: other,
            resumeDrain: other,
        });
        return calls;
    }
    const timedOut = () => Promise.resolve({ kind: 'timedOut', message: 'request timed out' });
    const frame = (armed: boolean): void => ft8Link.onTx({ armed, transmitting: false });

    it('a 202 resolves accepted without the clock, and the control stays pushed-state only', async () => {
        armSeam(() => Promise.resolve({ kind: 'accepted' }));
        const r = await armTx(true);
        expect(r.status).toBe('accepted');
        expect(ft8State.tx.armed).toBe(false); // no optimistic flip — the push decides
        frame(true);
        expect(ft8State.tx.armed).toBe(true);
    });

    it('a fired timeout reconciled by a matching ft8-tx frame inside the grace resolves observed', async () => {
        armSeam(timedOut);
        const p = armTx(true);
        frame(true);
        expect((await p).status).toBe('observed');
    });

    it('a timed-out ENABLE with no confirming frame resolves unknown with the enable wording', async () => {
        armSeam(timedOut);
        const p = armTx(true);
        await vi.advanceTimersByTimeAsync(2000);
        const r = await p;
        expect(r.status).toBe('unknown');
        if (r.status !== 'unknown') return;
        expect(r.message).toBe(ENABLE_UNKNOWN);
        expect(ft8State.tx.armed).toBe(false); // unknown never claims it armed
    });

    it('a timed-out DISABLE resolves unknown with the disable wording and never claims TX is down', async () => {
        frame(true); // the daemon reported armed
        armSeam(timedOut);
        const p = armTx(false);
        await vi.advanceTimersByTimeAsync(2000);
        const r = await p;
        expect(r.status).toBe('unknown');
        if (r.status !== 'unknown') return;
        expect(r.message).toBe(DISABLE_UNKNOWN);
        expect(ft8State.tx.armed).toBe(true); // still armed until the daemon says otherwise
    });

    it('a replayed frame carrying the PRE-request state does not satisfy the watch', async () => {
        armSeam(timedOut);
        const p = armTx(true); // watching for armed:true
        let settled = false;
        void p.then(() => (settled = true));
        frame(false); // a hub replay of the state before the request
        await vi.advanceTimersByTimeAsync(1999);
        expect(settled).toBe(false); // the stale value confirmed nothing
        await vi.advanceTimersByTimeAsync(1);
        expect((await p).status).toBe('unknown');
    });

    it('a daemon refusal is failed{refused} with its message; the control never shows armed', async () => {
        armSeam(() => Promise.resolve({ kind: 'refused', message: 'rig not ready' }));
        const r = await armTx(true);
        expect(r.status).toBe('failed');
        if (r.status !== 'failed') return;
        expect(r.kind).toBe('refused');
        expect(r.message).toBe('rig not ready');
        expect(ft8State.tx.armed).toBe(false);
    });

    it('a non-timeout transport failure is failed{transport}, distinct from a refusal', async () => {
        armSeam(() => Promise.resolve({ kind: 'transport', message: 'connection refused' }));
        const r = await armTx(true);
        expect(r.status).toBe('failed');
        if (r.status !== 'failed') return;
        expect(r.kind).toBe('transport');
    });

    it('an opposite request during the POST supersedes the older one, silently', async () => {
        armSeam(timedOut);
        const p1 = armTx(true);
        const p2 = armTx(false); // before #1's POST even resolved
        expect((await p1).status).toBe('superseded');
        frame(false);
        expect((await p2).status).toBe('observed');
    });

    it('an opposite request during the GRACE cancels the older wait at once — no clock, no late override', async () => {
        armSeam(timedOut);
        const p1 = armTx(true);
        await Promise.resolve();
        await Promise.resolve(); // #1's POST has timed out; it is now inside its grace
        const p2 = armTx(false);
        const r1 = await p1; // resolves WITHOUT advancing the clock: the grace wait was cancelled
        expect(r1.status).toBe('superseded');
        frame(true); // a late match for #1 — cannot revive it, cannot satisfy #2
        let settled = false;
        void p2.then(() => (settled = true));
        await Promise.resolve();
        expect(settled).toBe(false);
        frame(false);
        expect((await p2).status).toBe('observed');
    });

    it('a prompt 202 leaves no watch behind: the next timed-out request reconciles on its own frame', async () => {
        armSeam(() => Promise.resolve({ kind: 'accepted' }));
        expect((await armTx(true)).status).toBe('accepted');
        armSeam(timedOut);
        const p = armTx(true);
        frame(true);
        expect((await p).status).toBe('observed');
    });

    it('SEAM ISOLATION — arming issues only the arm intent and never a transmit-start action', async () => {
        const calls = armSeam(() => Promise.resolve({ kind: 'accepted' }));
        await armTx(true);
        await armTx(false);
        expect(calls).toEqual(['arm:true', 'arm:false']); // no callCq / answer / work / …
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

describe('noteOperatingBand (band-change clear, dogfood 2026-07-19)', () => {
    function seedDecodes() {
        ft8Link.onDecode(
            decodeSlot('2026-07-19T05:00:00Z', [{ text: 'CQ K1ABC FN42', freq_hz: 1200, snr: -5 }])
        );
        expect(ft8State.decodes.length).toBe(1);
    }
    it('first sighting records the band', () => {
        ft8State.noteOperatingBand('20m');
        expect(ft8State.lastSeenBand).toBe('20m');
    });

    it('same band repeated leaves the feed alone (intra-band dial nudges)', () => {
        ft8State.noteOperatingBand('20m');
        seedDecodes();
        ft8State.noteOperatingBand('20m');
        expect(ft8State.decodes.length).toBe(1);
    });

    // The old third clause — "AND the pile-up queue" — retired with the SPA
    // stack (ADR 0067): the pick queue is daemon state, dropped daemon-side
    // when the dial guard ends the run.
    it('a band change clears the decode feed', () => {
        ft8State.noteOperatingBand('20m');
        seedDecodes();
        ft8State.noteOperatingBand('40m');
        expect(ft8State.decodes.length).toBe(0);
        expect(ft8State.lastSeenBand).toBe('40m');
    });

    it('an empty band (unknown dial) is ignored', () => {
        ft8State.noteOperatingBand('20m');
        seedDecodes();
        ft8State.noteOperatingBand('');
        expect(ft8State.decodes.length).toBe(1);
        expect(ft8State.lastSeenBand).toBe('20m');
    });
});

describe('session-ended notice (dogfood 2026-07-27, on air)', () => {
    // The daemon ends a session it cannot confirm the frequency of. That is a
    // deliberate safety stop, but it presented as the ladder simply vanishing —
    // indistinguishable from a hang. The first on-air read of a WORKING dial guard
    // was "moving the dial does not stop TX"; it took a log dive to see that it had.
    function activeSession() {
        ft8Link.onQso({
            active: true,
            role: 'caller',
            their_call: 'K1ABC',
            their_period: 'even',
        });
    }

    it('announces the reason and who we were working when a session is cut short', () => {
        const seen: { reason: string; call: string }[] = [];
        setFt8SessionEndedSink((reason, call) => seen.push({ reason, call }));

        activeSession();
        // The terminal frame carries no callsign — the station has to come from the
        // state being replaced, which is why the notice fires before the overwrite.
        ft8Link.onQso({ active: false, end_reason: 'dial_moved' });

        expect(seen).toEqual([{ reason: 'dial_moved', call: 'K1ABC' }]);
        expect(ft8State.qso.active).toBe(false);
    });

    it('stays silent for an ordinary end — an abandon or a completed contact', () => {
        const seen: string[] = [];
        setFt8SessionEndedSink((reason) => seen.push(reason));

        activeSession();
        ft8Link.onQso({ active: false }); // no reason: the operator caused this

        expect(seen).toEqual([]);
    });

    it('does not fire when no session was running', () => {
        const seen: string[] = [];
        setFt8SessionEndedSink((reason) => seen.push(reason));

        ft8Link.onQso({ active: false, end_reason: 'dial_unknown' });

        expect(seen).toEqual([]);
    });
});
