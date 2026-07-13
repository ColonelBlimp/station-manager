// Pile-up drain test: the Operate view auto-works the operator-curated FIFO
// (ft8PileupStack) whenever the rig is armed + CAT-live + idle + an offset & dial
// freq are known + auto-drain is enabled. Guards the drain $effect's gates and its
// dequeue/skip/pause behaviour. The queue mechanics themselves are covered in
// ft8Pileup.svelte.test.ts; the click-to-enqueue wiring in Ft8BandActivity's test.

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
    type Ft8WorkArgs,
    type Ft8TxResult,
    type Ft8TxActions,
} from './ft8.svelte';
import { ft8PileupStack, _resetPileupForTests } from './ft8Pileup.svelte';
import { rig } from './rig.svelte';
import { session, type SessionQso } from './session.svelte';
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
        ...over,
    });
    rig.cat = 'connected';
    rig.freq = '14.074.000';
    ft8State.selectedOffset = 1500;
    ft8State.tx.armed = true;
}

function caller(call: string, slotUtc = '2026-06-17T14:30:00Z') {
    return { call, grid: 'FN42', snr: -12, slotUtc };
}

function sessionQso(call: string, band = '20m'): SessionQso {
    return {
        id: 1,
        callsign: call,
        timeOn: '14:30:00',
        band,
        mode: 'FT8',
        rstSent: '-12',
        rstRcvd: '-08',
        name: '',
        country: '',
        comment: '',
    };
}

beforeEach(() => {
    resetFt8ForTests();
    _resetPileupForTests();
    resetToasts();
    session.qsos.length = 0;
    setFt8OperatorCall('7Q5MLV');
    setFt8MyGrid('KH33');
    rig.band = '20m';
    rig.cat = 'off';
    rig.freq = '14.255.000';
});

describe('Ft8Operate pile-up drain', () => {
    it('works the queue head via workCaller when armed + idle, then dequeues', async () => {
        const worked: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                worked.push(a);
                return okResult();
            },
        });
        render(Ft8Operate);
        ft8PileupStack.push(caller('K1ABC'));
        flushSync();
        await flush();
        expect(worked.map((w) => w.theirCall)).toEqual(['K1ABC']);
        expect(worked[0].offsetHz).toBe(1500);
        // A successful start removes the head from the queue.
        expect(ft8PileupStack.items).toEqual([]);
    });

    it('does not drain while a contact is active', async () => {
        const worked: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                worked.push(a);
                return okResult();
            },
        });
        ft8State.qso.active = true;
        render(Ft8Operate);
        ft8PileupStack.push(caller('K1ABC'));
        flushSync();
        await flush();
        expect(worked).toEqual([]);
        expect(ft8PileupStack.items.length).toBe(1); // kept for later
    });

    it('does not drain while paused (auto-drain disabled)', async () => {
        const worked: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                worked.push(a);
                return okResult();
            },
        });
        ft8PileupStack.pause();
        render(Ft8Operate);
        ft8PileupStack.push(caller('K1ABC'));
        flushSync();
        await flush();
        expect(worked).toEqual([]);
        expect(ft8PileupStack.items.length).toBe(1);
    });

    it('does not drain when the rig is not armed', async () => {
        const worked: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                worked.push(a);
                return okResult();
            },
        });
        ft8State.tx.armed = false;
        render(Ft8Operate);
        ft8PileupStack.push(caller('K1ABC'));
        flushSync();
        await flush();
        expect(worked).toEqual([]);
        expect(ft8PileupStack.items.length).toBe(1);
    });

    it('skips (drops) a head already worked this session without transmitting', async () => {
        const worked: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                worked.push(a);
                return okResult();
            },
        });
        session.qsos.push(sessionQso('K1ABC', '20m'));
        render(Ft8Operate);
        ft8PileupStack.push(caller('K1ABC'));
        flushSync();
        await flush();
        expect(worked).toEqual([]);
        expect(ft8PileupStack.items).toEqual([]); // dropped, not worked
    });

    it('keeps the head (does not pause) on a single transient failure', async () => {
        const worked: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                worked.push(a);
                return Promise.resolve({ ok: false, message: 'rig not ready' });
            },
        });
        render(Ft8Operate);
        ft8PileupStack.push(caller('K1ABC'));
        flushSync();
        await flush();
        // Attempted once, failed → head retained, drain still enabled (a retry is
        // scheduled; we don't advance timers here, only assert nothing was lost).
        expect(worked.length).toBe(1);
        expect(ft8PileupStack.items.length).toBe(1);
        expect(ft8PileupStack.enabled).toBe(true);
    });
});

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
    ft8PileupStack.push(caller(queued));
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
        ft8PileupStack.pause(); // keep the queue static (idle)
        ft8PileupStack.push(caller('K1ABC'));
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
        expect(ft8PileupStack.enabled).toBe(true); // drain resumed → next head worked
    });

    it('a reply disarms the skip daemon-side and the button returns to Next', async () => {
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

    it('during a Call-CQ run, Next is an immediate takeover (abandon + resume)', async () => {
        let abandoned = 0;
        armReady({
            abandon: () => {
                abandoned++;
                return okResult();
            },
        });
        ft8State.qso.active = true;
        ft8State.qso.role = 'caller';
        ft8PileupStack.pause();
        ft8PileupStack.push(caller('K1ABC'));
        render(Ft8Operate);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        await flush();
        expect(abandoned).toBe(1);
        expect(ft8PileupStack.enabled).toBe(true);
    });
});

describe('Ft8Operate Abandon', () => {
    it('pauses the drain (stops the run) and keeps the queue', async () => {
        armReady({ abandon: () => okResult() });
        ft8State.qso.active = true;
        ft8State.qso.role = 'worker';
        ft8PileupStack.push(caller('K1ABC'));
        render(Ft8Operate);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Abandon' }));
        await flush();
        expect(ft8PileupStack.enabled).toBe(false); // paused → no takeover
        expect(ft8PileupStack.items.length).toBe(1); // queue kept for Resume
    });
});
