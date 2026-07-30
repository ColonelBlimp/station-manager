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
import { ft8State, resetFt8ForTests, ft8Link } from './ft8.svelte';

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
