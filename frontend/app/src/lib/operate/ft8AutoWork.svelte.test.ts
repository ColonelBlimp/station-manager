// Auto-work run visibility (ADR 0059) — the operator-facing half of:
//
//   ...and at any moment I can tell whether the run is still ARMED AND WAITING (it
//   will key the rig when the next caller appears) from STOPPED.
//
// Both states render "No active contact"; only one of them transmits at whoever calls
// next. The daemon half (internal/ft8/autowork_test.go V1-V4) decides when the flag is
// true and publishes it on idle frames; what is pinned here is that the operator can
// see it.

import { it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Ft8Operate from './Ft8Operate.svelte';
import { ft8State, resetFt8ForTests, ft8Link, setFt8TxActions } from './ft8.svelte';

// Abandon is also gated on TX being armed, so a rule about the RUN must not be
// satisfied (or defeated) by the arm state.
function armReadyForAbandon(): void {
    const ok = (): Promise<{ ok: boolean; message: string }> =>
        Promise.resolve({ ok: true, message: '' });
    setFt8TxActions({
        arm: ok,
        callCq: ok,
        answerCq: ok,
        workCaller: ok,
        abandon: ok,
        skip: ok,
        next: ok,
    });
    ft8State.tx.armed = true;
}

beforeEach(() => {
    resetFt8ForTests();
});

// U1 — the flag arrives from the daemon and reaches the state. Without this the
// component rules below could pass on a state nothing ever populates.
it('carries auto_work_armed from the ft8-qso frame into state', () => {
    ft8Link.onQso({ active: false, auto_work_armed: true });
    expect(ft8State.qso.autoWorkArmed).toBe(true);

    ft8Link.onQso({ active: false, auto_work_armed: false });
    expect(ft8State.qso.autoWorkArmed).toBe(false);
});

// U2/U3 — THE PAIR. Idle-and-armed must be visibly different from idle-and-stopped,
// and both render the same "No active contact" line, so the difference has to be
// something else on the screen.
it('shows the armed indicator when idle with a live run', () => {
    ft8Link.onQso({ active: false, auto_work_armed: true });
    render(Ft8Operate);
    expect(screen.getByText('No active contact')).toBeTruthy();
    expect(screen.getByTestId('auto-work-armed')).toBeTruthy();
});

it('shows no indicator when idle with no run', () => {
    ft8Link.onQso({ active: false, auto_work_armed: false });
    render(Ft8Operate);
    expect(screen.getByText('No active contact')).toBeTruthy();
    expect(screen.queryByTestId('auto-work-armed')).toBeNull();
});

// U4 — and it stays put during a contact. The run is live throughout; an indicator
// that vanished while the run was working someone would read as the run having ended,
// which is the confusion this exists to remove.
it('keeps the indicator while the run is working a contact', () => {
    ft8Link.onQso({ active: true, their_call: 'DL9UW', auto_work_armed: true });
    render(Ft8Operate);
    expect(screen.getByTestId('auto-work-armed')).toBeTruthy();
});

// U5 — THE ADVERTISED STOP MUST BE PRESSABLE. The indicator's own text says "Abandon
// stops the run", and the armed-and-idle state is precisely where an operator reads
// it — yet Abandon was gated on a contact being in progress, so the control was
// disabled exactly when it was being advertised. The daemon accepts the call in this
// state (Service.AbandonQso abandons unconditionally and the sequencer clears the
// run), so the button was the only thing standing between the operator and the stop.
//
// This is invariant 7 inverted: that rule forbids offering a stop where it cannot
// act; this is advertising one that can act and withholding it.
it('enables Abandon when idle with an armed run', () => {
    armReadyForAbandon();
    ft8Link.onQso({ active: false, auto_work_armed: true });
    render(Ft8Operate);

    // The rendered ATTRIBUTE, not the DOM property: getByRole is typed HTMLElement,
    // and asserting on what the markup emits is what the operator's browser sees.
    expect(screen.getByRole('button', { name: 'Abandon' }).hasAttribute('disabled')).toBe(false);
});

// ...and stays disabled with nothing to stop, so U5 cannot be satisfied by simply
// enabling it always.
it('leaves Abandon disabled when idle with no run', () => {
    armReadyForAbandon();
    ft8Link.onQso({ active: false, auto_work_armed: false });
    render(Ft8Operate);

    expect(screen.getByRole('button', { name: 'Abandon' }).hasAttribute('disabled')).toBe(true);
});
