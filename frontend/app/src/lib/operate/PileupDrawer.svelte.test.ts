// Pile-up drawer render/interaction test (ADR 0067): the drawer renders the
// DAEMON's two lists — "Calling you" (listed answerers; Work commits one now,
// Bag queues it) and "Bagged" (the drain's queue, in bag order; × unbags) —
// plus the paused indicator and the footer Resume. Every verb is
// confirm-by-push: the click POSTs and the list changes only when the ft8-qso
// SSE says so, so these tests assert on the POSTs and on rendering, never on
// local mutation. Queue/drain semantics live in internal/ft8/adr0067_test.go
// (B-rules); this suite guards the drawer wiring. The operator-curated
// ctrl-click stack this file used to pin retired with the ADR.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import PileupDrawer from './PileupDrawer.svelte';
import {
    ft8State,
    setFt8TxActions,
    resetFt8ForTests,
    type Ft8TxResult,
    type Ft8TxActions,
} from './ft8.svelte';
import { operate, setPileup } from './state.svelte';
import { _resetForTests as resetToasts, toastsState } from '../ui/toasts.svelte';

const okResult = (): Promise<Ft8TxResult> => Promise.resolve({ ok: true, message: '' });

function actions(over: Partial<Ft8TxActions> = {}): void {
    setFt8TxActions({
        arm: () => Promise.resolve({ kind: 'accepted' as const }),
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
}

beforeEach(() => {
    resetFt8ForTests();
    resetToasts();
    setPileup(true); // open — the body renders regardless, but be explicit
    actions();
});

describe('PileupDrawer sections', () => {
    it('empty state: both sections explain themselves', () => {
        render(PileupDrawer);
        expect(screen.getByTestId('listed-heading')).toBeInTheDocument();
        expect(screen.getByTestId('bagged-heading')).toBeInTheDocument();
        expect(screen.getByText(/stations calling you are listed here/i)).toBeInTheDocument();
        expect(screen.getByText(/Nothing bagged/)).toBeInTheDocument();
    });

    it('header count totals listed + bagged', () => {
        ft8State.qso.answerers = [
            { call: 'DL9UW', snr: -8 },
            { call: 'K1ABC', snr: -12 },
        ];
        ft8State.qso.queue = [{ call: '9A4ZM', snr: -3 }];
        render(PileupDrawer);
        flushSync();
        expect(screen.getByText('(3)')).toBeInTheDocument();
    });

    it('lists the daemon-published answerers; Work posts the pick', async () => {
        const picked: string[] = [];
        actions({
            pickAnswerer: (call) => {
                picked.push(call);
                return okResult();
            },
        });
        ft8State.qso.answerers = [
            { call: 'DL9UW', snr: -8 },
            { call: 'K1ABC', snr: -12 },
        ];
        render(PileupDrawer);
        flushSync();

        expect(screen.getByText('DL9UW')).toBeInTheDocument();
        expect(screen.getByText('-8 dB')).toBeInTheDocument();
        await fireEvent.click(screen.getByLabelText('Work DL9UW now'));
        expect(picked).toEqual(['DL9UW']);
    });

    it('Bag posts the bag verb and mutates nothing locally — the SSE owns the lists', async () => {
        const bagged: string[] = [];
        actions({
            bagAnswerer: (call) => {
                bagged.push(call);
                return okResult();
            },
        });
        ft8State.qso.answerers = [{ call: 'DL9UW', snr: -8 }];
        render(PileupDrawer);
        flushSync();

        await fireEvent.click(screen.getByLabelText('Bag DL9UW'));
        expect(bagged).toEqual(['DL9UW']);
        // Confirm-by-push: still listed, still un-bagged, until a frame arrives.
        expect(ft8State.qso.answerers.map((a) => a.call)).toEqual(['DL9UW']);
        expect(ft8State.qso.queue).toEqual([]);
    });

    it('renders the bagged queue in bag order and unbags via ×', async () => {
        const unbagged: string[] = [];
        actions({
            unbagAnswerer: (call) => {
                unbagged.push(call);
                return okResult();
            },
        });
        ft8State.qso.queue = [
            { call: '9A4ZM', snr: -3 },
            { call: 'PA3KUS', snr: -15 },
        ];
        render(PileupDrawer);
        flushSync();

        const list = screen.getByTestId('bagged-list');
        const rows = Array.from(list.querySelectorAll('li')).map((li) =>
            li.textContent?.includes('9A4ZM') ? '9A4ZM' : 'PA3KUS'
        );
        expect(rows).toEqual(['9A4ZM', 'PA3KUS']); // bag order, head first
        await fireEvent.click(screen.getByLabelText('Unbag PA3KUS'));
        expect(unbagged).toEqual(['PA3KUS']);
    });

    it('a refused verb surfaces the daemon message as a toast', async () => {
        actions({
            pickAnswerer: () =>
                Promise.resolve({
                    ok: false,
                    message: 'that station is no longer answering your CQ — pick a listed one',
                }),
        });
        ft8State.qso.answerers = [{ call: 'DL9UW', snr: -8 }];
        render(PileupDrawer);
        flushSync();
        await fireEvent.click(screen.getByLabelText('Work DL9UW now'));
        await Promise.resolve();
        expect(toastsState.items.map((t) => t.message).join(' ')).toMatch(/no longer answering/);
    });

    it('the header × closes the drawer without posting any verb', async () => {
        const verbs: string[] = [];
        actions({
            stopAutoWork: () => (verbs.push('stop'), okResult()),
            resumeDrain: () => (verbs.push('resume'), okResult()),
            unbagAnswerer: (c) => (verbs.push(`unbag:${c}`), okResult()),
        });
        ft8State.qso.queue = [{ call: '9A4ZM', snr: -3 }];
        render(PileupDrawer);
        flushSync();
        await fireEvent.click(screen.getByText('Close panel'));
        expect(operate.pileup).toBe(false);
        expect(verbs).toEqual([]); // a slide-over close never destroys run state
    });
});

describe('PileupDrawer paused drain', () => {
    it('shows Paused + footer Resume only when paused with a queue; Resume posts the verb', async () => {
        let resumed = 0;
        actions({
            resumeDrain: () => {
                resumed++;
                return okResult();
            },
        });
        ft8State.qso.queue = [{ call: '9A4ZM', snr: -3 }];
        render(PileupDrawer);
        flushSync();

        // Not paused → no Resume, no Paused badge.
        expect(screen.queryByTestId('drawer-resume')).toBeNull();
        expect(screen.queryByText('Paused')).toBeNull();

        ft8State.qso.drainPaused = true;
        flushSync();
        expect(screen.getByText('Paused')).toBeInTheDocument();
        await fireEvent.click(screen.getByTestId('drawer-resume'));
        expect(resumed).toBe(1);
    });

    it('paused with an EMPTY queue offers no Resume — nothing to drain', () => {
        ft8State.qso.drainPaused = true;
        render(PileupDrawer);
        flushSync();
        expect(screen.queryByTestId('drawer-resume')).toBeNull();
    });
});
