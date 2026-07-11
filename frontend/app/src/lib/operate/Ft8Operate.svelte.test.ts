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

describe('Ft8Operate Next control', () => {
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

    it('abandons the contact and resumes the drain (queue takes over)', async () => {
        let abandoned = 0;
        armReady({
            abandon: () => {
                abandoned++;
                return okResult();
            },
        });
        ft8State.qso.active = true;
        ft8State.qso.role = 'worker';
        ft8PileupStack.pause();
        ft8PileupStack.push(caller('K1ABC'));
        render(Ft8Operate);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        await flush();
        expect(abandoned).toBe(1);
        expect(ft8PileupStack.enabled).toBe(true); // drain resumed → next head worked
    });

    it('is available during a Call-CQ run (queue-takes-over-CQ kept)', async () => {
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
        const next = screen.getByRole('button', { name: 'Next' });
        expect(next).toBeInTheDocument();
        await fireEvent.click(next);
        await flush();
        expect(abandoned).toBe(1);
        expect(ft8PileupStack.enabled).toBe(true);
    });

    it('is disabled while transmitting (advances only between transmissions)', () => {
        armReady();
        ft8State.qso.active = true;
        ft8State.qso.role = 'worker';
        ft8State.tx.transmitting = true;
        ft8PileupStack.push(caller('K1ABC'));
        render(Ft8Operate);
        flushSync();
        expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
    });
});
