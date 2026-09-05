// The run surface (ADR 0067, operator-ratified UI) — ONE home for the run
// lifecycle, in the panel slot the checkbox/chip morph occupied. The criterion:
//
//   Whatever the entry point, I can read from one place how callers will be
//   treated (the Answer mode), whether a run is live and what it is doing, and
//   I can stop/resume it there — and I can tell "run live, waiting" apart from
//   "no run" even though the ladder says "No active contact" in both.
//
// The state STRINGS are the ratified table in the ADR (docs/decisions/0067) —
// they are asserted verbatim because they are the operator-facing contract, not
// styling. Fixtures drive the daemon side through ft8-qso frames (what the SSE
// actually carries: answer_mode, queue, drain_paused, auto_work_armed) and the
// session side through ft8State.answerMode (the selector's own state).
//
// Selector lock rules live in ft8AutoWork.svelte.test.ts (SP4/SP4b, rendered
// through Ft8Operate); Stop-vs-Abandon separation is there too (U6).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import RunSurface from './RunSurface.svelte';
import {
    ft8State,
    ft8Link,
    resetFt8ForTests,
    setFt8TxActions,
    type Ft8TxActions,
} from './ft8.svelte';
import { operate, setPileup } from './state.svelte';
import { _resetForTests as resetToasts, toastsState } from '../ui/toasts.svelte';

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

const stateLine = (): string =>
    document.querySelector('[data-run-state]')?.textContent?.trim() ?? '';

beforeEach(() => {
    resetFt8ForTests();
    resetToasts();
    setPileup(false);
    installActions();
});

describe('RunSurface state line — pick mode (the session default)', () => {
    it('explains the mode when no pick context is live', () => {
        render(RunSurface);
        expect(stateLine()).toBe(
            'Manual — callers will be listed; nothing transmits until you choose'
        );
    });

    it('a live pick context with nobody calling says so', () => {
        ft8Link.onQso({ active: false, answer_mode: 'operator_pick', auto_work_armed: true });
        render(RunSurface);
        expect(stateLine()).toBe('Listing callers — nobody calling yet');
    });

    it('callers listed → count + where to act', () => {
        ft8Link.onQso({
            active: false,
            answer_mode: 'operator_pick',
            auto_work_armed: true,
            answerers: [
                { call: 'DL9UW', snr: -8 },
                { call: 'K1ABC', snr: -12 },
            ],
        });
        render(RunSurface);
        expect(stateLine()).toBe('2 calling you — open the drawer to work or bag');
    });

    it('a bagged queue outranks the caller count — the drain is the story now', () => {
        ft8Link.onQso({
            active: false,
            answer_mode: 'operator_pick',
            auto_work_armed: true,
            answerers: [{ call: 'DL9UW', snr: -8 }],
            queue: [
                { call: '9A4ZM', snr: -3 },
                { call: 'PA3KUS', snr: -15 },
            ],
        });
        render(RunSurface);
        expect(stateLine()).toBe('Working your queue — 2 bagged left');
    });

    it('paused outranks everything — the ratified pause line', () => {
        ft8Link.onQso({
            active: false,
            answer_mode: 'operator_pick',
            auto_work_armed: true,
            queue: [{ call: '9A4ZM', snr: -3 }],
            drain_paused: true,
        });
        render(RunSurface);
        expect(stateLine()).toBe('Drain paused — 1 bagged waiting');
    });
});

describe('RunSurface state line — auto modes', () => {
    it('idle, no run: the mode explains what the next contact does, with the order word', () => {
        ft8State.answerMode = 'auto_first';
        render(RunSurface);
        expect(stateLine()).toBe('Your next contact starts a run — callers worked first come');
    });

    it('auto_strongest carries its own order word — the two modes must read differently', () => {
        ft8State.answerMode = 'auto_strongest';
        render(RunSurface);
        expect(stateLine()).toBe('Your next contact starts a run — callers worked strongest first');
    });

    it('armed and idle: run live, waiting', () => {
        ft8State.answerMode = 'auto_first';
        ft8Link.onQso({ active: false, auto_work_armed: true });
        render(RunSurface);
        expect(stateLine()).toBe('Run live — waiting for callers (first come)');
    });

    it('armed and working: run live, naming the station', () => {
        ft8State.answerMode = 'auto_first';
        ft8Link.onQso({ active: true, their_call: 'DL9UW', auto_work_armed: true });
        render(RunSurface);
        expect(stateLine()).toBe('Run live — working DL9UW (first come)');
    });
});

describe('RunSurface state line click', () => {
    it('opens the pile-up drawer when a pick context is live', async () => {
        ft8Link.onQso({ active: false, answer_mode: 'operator_pick', auto_work_armed: true });
        render(RunSurface);
        await fireEvent.click(document.querySelector('[data-run-state]')!);
        expect(operate.pileup).toBe(true);
    });

    it('does nothing outside a pick context — there is no list to open', async () => {
        ft8State.answerMode = 'auto_first';
        ft8Link.onQso({ active: false, auto_work_armed: true });
        render(RunSurface);
        await fireEvent.click(document.querySelector('[data-run-state]')!);
        expect(operate.pileup).toBe(false);
    });
});

describe('RunSurface Stop / Resume', () => {
    it('Stop shows for a live run and posts the run-only stop', async () => {
        let stops = 0;
        installActions({ stopAutoWork: () => (stops++, ok()) });
        ft8State.answerMode = 'auto_first';
        ft8Link.onQso({ active: false, auto_work_armed: true });
        render(RunSurface);

        await fireEvent.click(screen.getByRole('button', { name: 'Stop run' }));
        expect(stops).toBe(1);
    });

    it('no run → no Stop and no Resume (the action row is empty, not absent)', () => {
        render(RunSurface);
        expect(document.querySelector('[data-run-stop]')).toBeNull();
        expect(document.querySelector('[data-run-resume]')).toBeNull();
    });

    it('a paused pick drain swaps Stop for Resume, which posts the resume verb', async () => {
        let resumes = 0;
        installActions({ resumeDrain: () => (resumes++, ok()) });
        ft8Link.onQso({
            active: false,
            answer_mode: 'operator_pick',
            auto_work_armed: true,
            queue: [{ call: '9A4ZM', snr: -3 }],
            drain_paused: true,
        });
        render(RunSurface);

        expect(document.querySelector('[data-run-stop]')).toBeNull();
        await fireEvent.click(screen.getByRole('button', { name: 'Resume queue' }));
        expect(resumes).toBe(1);
    });

    it('a refused stop surfaces the daemon message as a toast', async () => {
        installActions({
            stopAutoWork: () => Promise.resolve({ ok: false, message: 'no auto-work run to stop' }),
        });
        ft8State.answerMode = 'auto_first';
        ft8Link.onQso({ active: false, auto_work_armed: true });
        render(RunSurface);

        await fireEvent.click(screen.getByRole('button', { name: 'Stop run' }));
        flushSync();
        expect(toastsState.items.map((t) => t.message).join(' ')).toMatch(/no auto-work run/);
    });
});
