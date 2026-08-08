// The Next control's behaviour BY MODE (2026-07-27).
//
// One button, two different jobs, and conflating them is the bug this pins:
//
//   - answering / working a station: Next arms the daemon's deferred skip-if-silent.
//     Unchanged.
//   - a Call-CQ run working an answerer: Next short-circuits the repeat cap on a
//     stuck rung — park this answerer, take another live one or resume CQ. The run
//     CONTINUES.
//
// Before this, Call-CQ's Next called abandon() and then resumed the pile-up drain, so
// it ended the run and switched the operator from calling CQ to working their curated
// queue. That coupling is removed deliberately: the pile-up drawer is for a pile-up
// that did NOT come from a CQ call, and the drain cannot run during a CQ run anyway
// (it bails whenever a session is active). Sequencing rules live in
// internal/ft8/nextanswerer_test.go; these are the wiring rules.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
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
import { _resetForTests as resetToasts, toastsState } from '../ui/toasts.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));
const okResult = (): Promise<Ft8TxResult> => Promise.resolve({ ok: true, message: '' });

interface Calls {
    next: number;
    abandon: number;
    skip: boolean[];
}

function armReady(over: Partial<Ft8TxActions> = {}): Calls {
    const calls: Calls = { next: 0, abandon: 0, skip: [] };
    setFt8TxActions({
        arm: okResult,
        callCq: okResult,
        answerCq: okResult,
        workCaller: okResult,
        abandon: () => {
            calls.abandon++;
            return okResult();
        },
        skip: (armed: boolean) => {
            calls.skip.push(armed);
            return okResult();
        },
        next: () => {
            calls.next++;
            return okResult();
        },
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
    return calls;
}

/** A Call-CQ run that is currently WORKING an answerer. */
function callingCqWorking(call = 'DL9UW'): void {
    ft8State.qso.active = true;
    ft8State.qso.role = 'caller';
    ft8State.qso.theirCall = call;
    ft8State.qso.state = 'reporting';
}

/** A Call-CQ run that is merely CALLING — nobody being worked. */
function callingCqIdle(): void {
    ft8State.qso.active = true;
    ft8State.qso.role = 'caller';
    ft8State.qso.theirCall = '';
    ft8State.qso.state = 'calling-cq';
}

/** Answering a CQ — the mode whose Next is the deferred skip. */
function answering(call = 'K1ABC'): void {
    ft8State.qso.active = true;
    ft8State.qso.role = 'answerer';
    ft8State.qso.theirCall = call;
    ft8State.qso.state = 'calling';
}

const clickNext = async (): Promise<void> => {
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await flush();
};

describe('Next by mode', () => {
    beforeEach(() => {
        resetFt8ForTests();
        resetToasts();
        setFt8OperatorCall('7Q5MLV');
        setFt8MyGrid('KH78');
    });

    it('parks the answerer and keeps the CQ run going — it does not abandon', () => {
        const calls = armReady();
        callingCqWorking();
        render(Ft8Operate);

        return clickNext().then(() => {
            expect(calls.next).toBe(1);
            // The whole point: the run survives. Abandon is a separate control.
            expect(calls.abandon).toBe(0);
            expect(calls.skip).toEqual([]);
        });
    });

    it('offers Next during a CQ run even with an empty pile-up queue', () => {
        armReady();
        callingCqWorking();
        expect(ft8State.qso.queue.length).toBe(0);
        render(Ft8Operate);

        // The old gate required a queued station, because Next handed over to the
        // drain. The daemon now picks the replacement from the slot's own decodes, so
        // the queue is irrelevant here — and requiring it hid the control in exactly
        // the case it is for: one stuck station and nothing curated.
        expect(screen.queryByRole('button', { name: 'Next' })).not.toBeNull();
    });

    // "Leaves the pile-up queue alone" retired with the SPA drain (ADR 0067):
    // the queue and its pause are daemon state now, and Next never posts a
    // bag/unbag/resume verb — the recorder above would count one if it did.

    it('is not offered while merely calling CQ — there is no contact to move on from', () => {
        armReady();
        callingCqIdle();
        // A QUEUED station, deliberately: without it this passes for the wrong reason
        // (the old gate hid the button whenever the queue was empty) and would stay
        // green against any implementation at all.
        ft8State.qso.queue = [{ call: 'G0ABC', snr: -10 }];
        render(Ft8Operate);

        expect(screen.queryByRole('button', { name: 'Next' })).toBeNull();
    });

    it('still arms the deferred skip when answering a CQ', async () => {
        const calls = armReady();
        answering();
        ft8State.qso.queue = [{ call: 'G0ABC', snr: -10 }];
        render(Ft8Operate);

        await clickNext();

        expect(calls.skip).toEqual([true]);
        expect(calls.next).toBe(0);
    });

    it('surfaces a refusal from the daemon', async () => {
        armReady({
            next: () => Promise.resolve({ ok: false, message: 'no station is being worked' }),
        });
        callingCqWorking();
        render(Ft8Operate);

        await clickNext();

        expect(
            toastsState.items.some(
                (t) => t.level === 'error' && t.message.includes('no station is being worked')
            )
        ).toBe(true);
    });

    it('shows the press landed, since the park only happens a slot later', async () => {
        armReady();
        callingCqWorking();
        render(Ft8Operate);

        // Confirm-by-push: the daemon reports it via ft8-qso, exactly like skip_armed.
        ft8State.qso.nextArmed = true;
        await flush();

        expect(screen.queryByRole('button', { name: 'Next' })).toBeNull();
        expect(screen.queryByRole('button', { name: /Next…|Moving on…/ })).not.toBeNull();
    });
});
