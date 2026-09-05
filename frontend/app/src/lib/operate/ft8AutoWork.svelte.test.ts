// Auto-work run visibility (ADR 0059, re-derived for ADR 0067's run surface) —
// the operator-facing half of:
//
//   ...and at any moment I can tell whether the run is still ARMED AND WAITING (it
//   will key the rig when the next caller appears) from STOPPED.
//
// Both states render "No active contact"; only one of them transmits at whoever calls
// next. The daemon half (internal/ft8/autowork_test.go V1-V4, adr0067_test.go) decides
// when the flag is true and publishes it on idle frames; what is pinned here is that
// the operator can see it — since ADR 0067 on the RUN SURFACE (state line + Stop),
// which replaced the armed pill. Detailed state-string/dot/Resume rendering is in
// RunSurface.svelte.test.ts; here the rules stay at the Ft8Operate level.
//
// The ADR 0065 refusal-sink rules that lived here (intent-carrying start comes
// back unarmed → one-shot toast) retired WITH the intent: under 0067 the Answer
// mode alone arms a run, there is no per-start arming to refuse, and the daemon
// no longer reads the wire field (kept inert for old clients).

import { it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Ft8Operate from './Ft8Operate.svelte';
import {
    ft8State,
    resetFt8ForTests,
    ft8Link,
    setFt8TxActions,
    type Ft8TxActions,
} from './ft8.svelte';

const ok = (): Promise<{ ok: boolean; message: string }> =>
    Promise.resolve({ ok: true, message: '' });

function installActions(over: Partial<Ft8TxActions> = {}): void {
    setFt8TxActions({
        arm: () => Promise.resolve({ kind: 'accepted' as const }),
        callCq: ok,
        answerCq: ok,
        workCaller: ok,
        abandon: ok,
        skip: ok,
        next: ok,
        stopAutoWork: ok,
        pickAnswerer: ok,
        bagAnswerer: ok,
        unbagAnswerer: ok,
        resumeDrain: ok,
        ...over,
    });
}

// Abandon is also gated on TX being armed, so a rule about the RUN must not be
// satisfied (or defeated) by the arm state.
function armReadyForAbandon(): void {
    installActions();
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
// something else on the screen: the run surface's Stop control, which exists only
// while there is a run to stop.
it('offers Stop run when idle with a live run', () => {
    ft8State.answerMode = 'auto_first';
    ft8Link.onQso({ active: false, auto_work_armed: true });
    render(Ft8Operate);
    expect(screen.getByText('No active contact')).toBeTruthy();
    expect(document.querySelector('[data-run-stop]')).not.toBeNull();
});

it('offers no Stop when idle with no run', () => {
    ft8State.answerMode = 'auto_first';
    ft8Link.onQso({ active: false, auto_work_armed: false });
    render(Ft8Operate);
    expect(screen.getByText('No active contact')).toBeTruthy();
    expect(document.querySelector('[data-run-stop]')).toBeNull();
});

// U4 — and it stays put during a contact. The run is live throughout; a control
// that vanished while the run was working someone would read as the run having
// ended, which is the confusion this exists to remove.
it('keeps Stop run offered while the run is working a contact', () => {
    ft8State.answerMode = 'auto_first';
    ft8Link.onQso({ active: true, their_call: 'DL9UW', auto_work_armed: true });
    render(Ft8Operate);
    expect(document.querySelector('[data-run-stop]')).not.toBeNull();
});

// U5 — THE ADVERTISED STOP MUST BE PRESSABLE. The armed-and-idle state is where
// the operator reads the run as live — yet Abandon was once gated on a contact
// being in progress, so the control was disabled exactly when advertised. The
// daemon accepts the call in this state (Service.AbandonQso abandons
// unconditionally and the sequencer clears the run).
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

// U6 — the Stop control posts the run-only stop (never abandon): an active
// contact continues; only the run/queue is addressed. (Daemon semantics — auto
// run stops outright, pick queue pauses — are adr0067_test.go B3/B8.)
it('clicking Stop run calls the run-only stop action', async () => {
    let stops = 0;
    let abandons = 0;
    installActions({
        stopAutoWork: () => (stops++, ok()),
        abandon: () => (abandons++, ok()),
    });
    ft8State.tx.armed = true;
    ft8State.answerMode = 'auto_first';
    ft8State.qso.autoWorkArmed = true;
    render(Ft8Operate);

    const stop = document.querySelector<HTMLButtonElement>('[data-run-stop]');
    expect(stop).not.toBeNull();
    stop!.click();
    await Promise.resolve();
    expect(stops).toBe(1);
    expect(abandons).toBe(0);
});

// ---------------------------------------------------------------------------
// ADR 0066/0067 — the session's Answer mode is the single run input. Pinned
// here: the mode reaches EVERY start (the wrapper stamps it — no caller can
// forget it), and config only SEEDS the session default.
// ---------------------------------------------------------------------------

import { callCq, answerCq, workCaller, setFt8SessionDefaults } from './ft8.svelte';

interface SentStart {
    kind: string;
    answerMode?: string;
}

function recordingActions(sent: SentStart[]): void {
    installActions({
        callCq: (_o, _f, _p, answerMode) => {
            sent.push({ kind: 'cq', answerMode });
            return ok();
        },
        answerCq: (a) => {
            sent.push({ kind: 'answer', answerMode: a.answerMode });
            return ok();
        },
        workCaller: (a) => {
            sent.push({ kind: 'work', answerMode: a.answerMode });
            return ok();
        },
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

// SP2 — the OTHER entry points carry it too, unconditionally — under 0067 the
// mode is how a run arms, so a start that omitted it would silently fall back
// to the config default and contradict the selector the operator can see. The
// two modes here differentiate against a wrapper that hardcodes either.
it('answerCq and workCaller stamp the session answer mode on every start', async () => {
    const sent: SentStart[] = [];
    recordingActions(sent);
    ft8State.answerMode = 'operator_pick';
    await answerCq({
        theirCall: 'K1ABC',
        theirGrid: 'FN42',
        slotUtc: '2026-08-08T08:00:00Z',
        offsetHz: 1500,
        opFreqMHz: 14.074,
        fd: false,
        theirSnr: -10,
    });
    ft8State.answerMode = 'auto_first';
    await workCaller({
        theirCall: 'W2DEF',
        theirGrid: 'FN31',
        theirSnr: -12,
        slotUtc: '2026-08-08T08:01:00Z',
        offsetHz: 1500,
        opFreqMHz: 14.074,
    });
    expect(sent).toEqual([
        { kind: 'answer', answerMode: 'operator_pick' },
        { kind: 'work', answerMode: 'auto_first' },
    ]);
});

// SP3 — config only SEEDS: the setter installs the default, junk is ignored
// (the session keeps the licensing-safe pick default), and reset restores it.
it('setFt8SessionDefaults seeds the selector; junk is ignored', () => {
    setFt8SessionDefaults('auto_first');
    expect(ft8State.answerMode).toBe('auto_first');
    setFt8SessionDefaults('bogus');
    expect(ft8State.answerMode).toBe('auto_first');
    resetFt8ForTests();
    expect(ft8State.answerMode).toBe('operator_pick');
});

// SP4 — the render half: the Answer mode selector (now on the run surface) is
// locked while a run is active (the parity precedent — changes apply to the
// NEXT run).
it('the Answer mode selector renders, and locks while a run is active', () => {
    ft8Link.onQso({ active: true, their_call: 'DL9UW' });
    render(Ft8Operate);
    const sel: HTMLSelectElement = screen.getByTestId('answer-mode');
    expect(sel.disabled).toBe(true);
});

// SP4b — codex d7fbf935 P1: idle-and-ARMED locks it too. An armed auto-work
// run holds its pinned selection mode past each completed contact
// (qso.active false, autoWorkArmed true); an editable selector there lets
// the UI claim "I pick" while the run keeps auto-working with the old mode.
// Changing the mode legitimately means stopping the run first (Stop run).
it('the Answer mode selector stays locked while an auto-work run is armed', () => {
    ft8Link.onQso({ active: false, auto_work_armed: true });
    render(Ft8Operate);
    const sel: HTMLSelectElement = screen.getByTestId('answer-mode');
    expect(sel.disabled).toBe(true);
});
