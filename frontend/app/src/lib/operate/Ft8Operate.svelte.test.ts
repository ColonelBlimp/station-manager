// Ft8Operate wiring tests: ladder reactivity, the Next control (deferred
// skip-if-silent), and Abandon. The SPA-side pile-up drain that used to be this
// file's subject retired with ADR 0067 — bagged stations are DAEMON state,
// worked by the sequencer's own drain (internal/ft8/adr0067_test.go B-rules);
// the SPA only renders qso.queue and posts bag/unbag/resume verbs.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Ft8Operate from './Ft8Operate.svelte';
import {
    ft8State,
    setFt8OperatorCall,
    setFt8MyGrid,
    setFt8TxActions,
    resetFt8ForTests,
    type Ft8TxResult,
    type Ft8TxActions,
} from './ft8.svelte';
import { rig } from './rig.svelte';
import { session } from './session.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));
const okResult = (): Promise<Ft8TxResult> => Promise.resolve({ ok: true, message: '' });

// A TX-action recorder with all seams stubbed ok; the drain only exercises
// workCaller. Also puts the rig + TX state into a "ready to transmit" shape.
function armReady(over: Partial<Ft8TxActions> = {}): void {
    setFt8TxActions({
        arm: okResult,
        callCq: okResult,
        answerCq: okResult,
        workCaller: okResult,
        abandon: okResult,
        skip: okResult,
        next: okResult,
        stopAutoWork: okResult,
        pickAnswerer: okResult,
        bagAnswerer: okResult,
        unbagAnswerer: okResult,
        resumeDrain: okResult,
        ...over,
    });
    rig.cat = 'connected';
    rig.freq = '14.074.000';
    ft8State.selectedOffset = 1500;
    ft8State.tx.armed = true;
}

beforeEach(() => {
    resetFt8ForTests();
    resetToasts();
    session.qsos.length = 0;
    setFt8OperatorCall('7Q5MLV');
    setFt8MyGrid('KH33');
    rig.band = '20m';
    rig.cat = 'off';
    rig.freq = '14.255.000';
});

// The 'Ft8Operate pile-up drain' describe (7 rules: gates, dequeue, worked-this-
// session skip, transient-failure retry) retired with the SPA drain $effect it
// pinned. The daemon drain's equivalents live in internal/ft8/adr0067_test.go
// (B2 drain order/parity, B3 stop-pauses, B5 staleness expiry, B9 CQ-run drain).

describe('Ft8Operate ladder reactivity', () => {
    // Regression: a hard reload (cache bypassed) resolves /v1/config AFTER first paint,
    // so the operator call/grid seams are set late. They must be reactive $state or the
    // idle caller-preview ladder stays stuck showing a bare "CQ" (no callsign) forever —
    // the derived never re-runs because nothing else it depends on changes while idle.
    it('CQ rung fills in when the operator call/grid arrive late', () => {
        armReady();
        setFt8OperatorCall(''); // pre-config: seams empty
        setFt8MyGrid('');
        const { container } = render(Ft8Operate);
        flushSync();
        expect(container.textContent).not.toContain('CQ 7Q5MLV KH33');
        // Config lands after first paint.
        setFt8OperatorCall('7Q5MLV');
        setFt8MyGrid('KH33');
        flushSync();
        expect(container.textContent).toContain('CQ 7Q5MLV KH33');
    });
});

// A worked-station contact with someone queued behind it — the state Next acts on.
function workingWithQueue(theirCall = 'K1ABC', queued = '9A4ZM'): void {
    ft8State.qso.active = true;
    ft8State.qso.role = 'worker';
    ft8State.qso.theirCall = theirCall;
    ft8State.qso.state = 'calling';
    ft8State.qso.repeats = 0;
    ft8State.qso.queue = [{ call: queued, snr: -12 }];
}

describe('Ft8Operate Next control (deferred skip-if-silent)', () => {
    it('is hidden when the pile-up is empty', () => {
        armReady();
        ft8State.qso.active = true;
        ft8State.qso.role = 'worker';
        render(Ft8Operate);
        flushSync();
        expect(screen.queryByRole('button', { name: 'Next' })).toBeNull();
    });

    it('is hidden when idle even with a queue (nothing to advance from)', () => {
        armReady();
        ft8State.qso.queue = [{ call: 'K1ABC', snr: -12 }];
        render(Ft8Operate);
        flushSync();
        expect(screen.queryByRole('button', { name: 'Next' })).toBeNull();
    });

    // The skip decision lives DAEMON-side (2026-07-13): Next POSTs the arm; the
    // armed rendering follows the ft8-qso SSE (skip_armed); the daemon ends the
    // session INSTEAD of keying the repeat, so the SPA never abandons — it just
    // reacts to the falling edge (toast + hand the drain the next caller).
    it('Next arms the daemon-side skip; the SSE renders the armed state', async () => {
        const skips: boolean[] = [];
        let abandoned = 0;
        armReady({
            skip: (a) => {
                skips.push(a);
                return okResult();
            },
            abandon: () => {
                abandoned++;
                return okResult();
            },
        });
        workingWithQueue();
        render(Ft8Operate);
        flushSync();

        await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        flushSync();
        expect(skips).toEqual([true]); // intent POSTed…
        expect(abandoned).toBe(0); // …and nothing abandoned client-side

        // Confirm-by-push: the armed rendering follows the SSE, not the POST.
        ft8State.qso.skipArmed = true;
        flushSync();
        expect(screen.getByRole('button', { name: /Skip if silent/ })).toBeInTheDocument();

        // Daemon fires the skip (silent cycle): session ends WITHOUT a repeat.
        ft8State.qso = { ...ft8State.qso, active: false, skipArmed: false, theirCall: '' };
        flushSync();
        await flush();
        expect(abandoned).toBe(0); // the daemon ended it — no client abandon
        // (What happens NEXT is the daemon's business: its drain takes the
        // queue head — adr0067_test.go B2. The SPA no longer resumes anything.)
    });

    it('a reply disarms the skip daemon-side and the button returns to Next', () => {
        armReady();
        workingWithQueue();
        render(Ft8Operate);
        flushSync();
        ft8State.qso.skipArmed = true;
        flushSync();
        expect(screen.getByRole('button', { name: /Skip if silent/ })).toBeInTheDocument();

        // They came back: the daemon clears skip_armed with the session still live.
        ft8State.qso.skipArmed = false;
        flushSync();
        expect(screen.getByRole('button', { name: 'Next' })).toBeInTheDocument();
    });

    it('clicking the armed button POSTs the disarm', async () => {
        const skips: boolean[] = [];
        armReady({
            skip: (a) => {
                skips.push(a);
                return okResult();
            },
        });
        workingWithQueue();
        render(Ft8Operate);
        flushSync();
        ft8State.qso.skipArmed = true;
        flushSync();

        await fireEvent.click(screen.getByRole('button', { name: /Skip if silent/ }));
        flushSync();
        expect(skips).toEqual([false]); // cancel = disarm intent; SSE clears the amber
    });

    // The Call-CQ arm of Next moved to ft8OperateNext.svelte.test.ts (2026-07-27).
    // It no longer abandons the run and resumes the drain — it parks the stuck
    // answerer daemon-side and the run carries on. The test that lived here pinned
    // the superseded takeover, including the state it used (role 'caller' with no
    // theirCall), which now offers no Next at all: there is nothing to move on from
    // while merely calling CQ.
});

describe('Ft8Operate Abandon', () => {
    // Under ADR 0067 Abandon posts the abandon verb and NOTHING else — no
    // pause, no queue touch. Daemon-side, abandon stops the whole run (ADR
    // 0059 W6), which is a harder stop than the old SPA behaviour (pause +
    // keep queue). Stop on the run surface is the pause now.
    it('POSTs abandon only — the daemon owns what happens to the run and queue', async () => {
        let abandoned = 0;
        const verbs: string[] = [];
        armReady({
            abandon: () => {
                abandoned++;
                return okResult();
            },
            bagAnswerer: (c) => (verbs.push(`bag:${c}`), okResult()),
            unbagAnswerer: (c) => (verbs.push(`unbag:${c}`), okResult()),
            resumeDrain: () => (verbs.push('resume'), okResult()),
            stopAutoWork: () => (verbs.push('stop'), okResult()),
        });
        ft8State.qso.active = true;
        ft8State.qso.role = 'worker';
        ft8State.qso.queue = [{ call: 'K1ABC', snr: -12 }];
        render(Ft8Operate);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Abandon' }));
        await flush();
        expect(abandoned).toBe(1);
        expect(verbs).toEqual([]); // no queue verb rode along
        expect(ft8State.qso.queue.length).toBe(1); // SPA state untouched — SSE owns it
    });
});
