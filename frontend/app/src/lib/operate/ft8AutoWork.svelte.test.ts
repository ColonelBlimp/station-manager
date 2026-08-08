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
import {
    ft8State,
    resetFt8ForTests,
    ft8Link,
    setFt8TxActions,
    setFt8AutoWorkRefusedSink,
    answerCq,
} from './ft8.svelte';

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
        stopAutoWork: ok,
        pickAnswerer: ok,
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

/*
    ADR 0065: the daemon gate's refusal is visible only as the ACTIVE frame
    arriving with auto_work_armed=false after a start that CARRIED the intent
    (autowork_test.go G3 — a refused arm never blocks the contact). The sink says
    so once — without it the operator watches a toggle they set produce no pill,
    unexplained. A start WITHOUT the intent must never fire it (the ordinary
    plain-click case), and the verdict is one-shot (later frames stay quiet).
    And the pill is a CONTROL: clicking it stops the run without abandoning.
*/
it('fires the refused sink once when an intent-carrying start comes back unarmed', async () => {
    armReadyForAbandon();
    // ADR 0066: the intent only leaves the SPA under an auto session mode —
    // under the pick default it is dropped at the source (SP2 below), and this
    // rule is about the daemon-refusal path, which needs the intent SENT.
    ft8State.answerMode = 'auto_first';
    let refused = 0;
    setFt8AutoWorkRefusedSink(() => refused++);

    await answerCq({
        theirCall: 'K1ABC',
        theirGrid: 'FN42',
        slotUtc: '2026-08-07T05:00:00Z',
        offsetHz: 1500,
        opFreqMHz: 14.074,
        fd: false,
        theirSnr: -10,
        autoWork: true,
    });
    // The daemon's verdict: contact active, arm refused (gate off).
    ft8Link.onQso({ active: true, role: 'answerer', their_call: 'K1ABC', auto_work_armed: false });
    expect(refused).toBe(1);
    // One-shot: a later frame does not re-toast.
    ft8Link.onQso({ active: true, role: 'answerer', their_call: 'K1ABC', auto_work_armed: false });
    expect(refused).toBe(1);
});

it('does not fire the refused sink for a plain start or a granted arm', async () => {
    armReadyForAbandon();
    let refused = 0;
    setFt8AutoWorkRefusedSink(() => refused++);

    // Plain start (no intent): an unarmed frame is the NORMAL outcome.
    await answerCq({
        theirCall: 'K1ABC',
        theirGrid: 'FN42',
        slotUtc: '2026-08-07T05:00:00Z',
        offsetHz: 1500,
        opFreqMHz: 14.074,
        fd: false,
        theirSnr: -10,
    });
    ft8Link.onQso({ active: true, role: 'answerer', their_call: 'K1ABC', auto_work_armed: false });
    expect(refused).toBe(0);

    // Intent granted: armed frame — nothing to explain.
    await answerCq({
        theirCall: 'W2DEF',
        theirGrid: 'FN31',
        slotUtc: '2026-08-07T05:01:00Z',
        offsetHz: 1500,
        opFreqMHz: 14.074,
        fd: false,
        theirSnr: -10,
        autoWork: true,
    });
    ft8Link.onQso({ active: true, role: 'answerer', their_call: 'W2DEF', auto_work_armed: true });
    expect(refused).toBe(0);
});

it('clicking the armed pill calls the run-only stop action', async () => {
    let stops = 0;
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
        stopAutoWork: () => (stops++, ok()),
        pickAnswerer: ok,
    });
    ft8State.tx.armed = true;
    ft8State.qso.autoWorkArmed = true;
    render(Ft8Operate);

    const pill = screen.getByTestId('auto-work-armed');
    pill.click();
    await Promise.resolve();
    expect(stops).toBe(1);
});

// ---------------------------------------------------------------------------
// ADR 0066 — session knobs. The daemon half is internal/ft8/adr0066_test.go;
// pinned here is the SPA's half of the contract: the session's Answer mode
// reaches every start, "I pick" suppresses the auto-work intent AT THE SOURCE
// (the control explains itself; no refusal round-trip), and config only SEEDS.
// ---------------------------------------------------------------------------

import { callCq, workCaller, setFt8SessionDefaults, ft8AutoWorkIntent } from './ft8.svelte';

interface SentStart {
    kind: string;
    autoWork?: boolean;
    answerMode?: string;
}

function recordingActions(sent: SentStart[]): void {
    const ok = (): Promise<{ ok: boolean; message: string }> =>
        Promise.resolve({ ok: true, message: '' });
    setFt8TxActions({
        arm: ok,
        callCq: (_o, _f, _p, answerMode) => {
            sent.push({ kind: 'cq', answerMode });
            return ok();
        },
        answerCq: (a) => {
            sent.push({ kind: 'answer', autoWork: a.autoWork, answerMode: a.answerMode });
            return ok();
        },
        workCaller: (a) => {
            sent.push({ kind: 'work', autoWork: a.autoWork, answerMode: a.answerMode });
            return ok();
        },
        abandon: ok,
        skip: ok,
        next: ok,
        stopAutoWork: ok,
        pickAnswerer: ok,
    });
}

// SP1 — Call CQ carries the SESSION's mode, and changing the selector changes
// the next start (no config edit anywhere in sight).
it('callCq sends the session answer mode', async () => {
    const sent: SentStart[] = [];
    recordingActions(sent);
    ft8State.answerMode = 'auto_strongest';
    await callCq(1500, 14.074, 'next');
    ft8State.answerMode = 'operator_pick';
    await callCq(1500, 14.074, 'next');
    expect(sent.map((x) => x.answerMode)).toEqual(['auto_strongest', 'operator_pick']);
});

// SP2 — "I pick" suppresses the intent at the source: the start goes out
// WITHOUT auto_work, and the one-shot toggle is NOT consumed (the operator's
// standing intent survives for a session where it can act).
it('operator_pick drops the auto-work intent and preserves the toggle', async () => {
    const sent: SentStart[] = [];
    recordingActions(sent);
    ft8State.answerMode = 'operator_pick';
    ft8AutoWorkIntent.on = true;
    await workCaller({
        theirCall: 'K1ABC',
        theirGrid: 'FN42',
        theirSnr: -12,
        slotUtc: '2026-08-08T08:00:00Z',
        offsetHz: 1500,
        opFreqMHz: 14.074,
        autoWork: true,
    });
    expect(sent[0].autoWork).toBe(false);
    expect(ft8AutoWorkIntent.on).toBe(true);
});

// SP2b — the discriminating pair: an auto mode passes the intent through and
// consumes the toggle (the pre-0066 behaviour survives where it should).
it('an auto mode passes the intent through and consumes the toggle', async () => {
    const sent: SentStart[] = [];
    recordingActions(sent);
    ft8State.answerMode = 'auto_first';
    ft8AutoWorkIntent.on = true;
    await workCaller({
        theirCall: 'K1ABC',
        theirGrid: 'FN42',
        theirSnr: -12,
        slotUtc: '2026-08-08T08:00:00Z',
        offsetHz: 1500,
        opFreqMHz: 14.074,
        autoWork: true,
    });
    expect(sent[0].autoWork).toBe(true);
    expect(sent[0].answerMode).toBe('auto_first');
    expect(ft8AutoWorkIntent.on).toBe(false);
});

// SP3 — config only SEEDS: the setter installs both defaults, junk is ignored
// (the session keeps the licensing-safe pick default), and reset restores it.
it('setFt8SessionDefaults seeds the selector and the toggle; junk is ignored', () => {
    setFt8SessionDefaults('auto_first', true);
    expect(ft8State.answerMode).toBe('auto_first');
    expect(ft8AutoWorkIntent.on).toBe(true);
    setFt8SessionDefaults('bogus', false);
    expect(ft8State.answerMode).toBe('auto_first');
    expect(ft8AutoWorkIntent.on).toBe(false);
    resetFt8ForTests();
    expect(ft8State.answerMode).toBe('operator_pick');
});

// SP4 — the render half: the Answer selector is in the control bar, locked
// while a run is active (the parity precedent — changes apply to the NEXT
// run), and the auto-work toggle is disabled-with-reason under "I pick".
it('the Answer selector renders, and locks while a run is active', () => {
    ft8Link.onQso({ active: true, their_call: 'DL9UW' });
    render(Ft8Operate);
    const sel = screen.getByTestId('answer-mode') as HTMLSelectElement;
    expect(sel.disabled).toBe(true);
});

it('the auto-work toggle is disabled under operator_pick', () => {
    ft8State.answerMode = 'operator_pick';
    ft8Link.onQso({ active: false, auto_work_armed: false });
    render(Ft8Operate);
    const box = screen.getByTestId('auto-work-intent') as HTMLInputElement;
    expect(box.disabled).toBe(true);
});
