// Pile-up drawer render/interaction test: the drawer lists ft8PileupStack FIFO
// (head first, no up-arrow on the head), per-row remove / move-up work, and the
// footer Resume (paused only) + Clear & abandon drive the stack. Queue mechanics
// are unit-tested in ft8Pileup.svelte.test.ts; this guards the drawer wiring.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import PileupDrawer from './PileupDrawer.svelte';
import { ft8PileupStack, _resetPileupForTests } from './ft8Pileup.svelte';
import { ft8State, setFt8TxActions, resetFt8ForTests, type Ft8TxResult } from './ft8.svelte';
import { setPileup } from './state.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

const okResult = (): Promise<Ft8TxResult> => Promise.resolve({ ok: true, message: '' });

function caller(call: string, snr = -12, slotUtc = '2026-06-17T14:30:00Z') {
    return { call, grid: 'FN42', snr, slotUtc };
}

beforeEach(() => {
    resetFt8ForTests();
    _resetPileupForTests();
    resetToasts();
    setPileup(true); // render the body content (open); it renders regardless, but be explicit
    setFt8TxActions({
        arm: okResult,
        callCq: okResult,
        answerCq: okResult,
        workCaller: okResult,
        abandon: okResult,
    });
});

describe('PileupDrawer', () => {
    it('shows the empty-state hint when the queue is empty', () => {
        render(PileupDrawer);
        expect(screen.getByText(/Ctrl-click/)).toBeInTheDocument();
    });

    it('lists callers FIFO with a count; the head has no move-up control', () => {
        ft8PileupStack.push(caller('K1ABC'));
        ft8PileupStack.push(caller('9A4ZM'));
        ft8PileupStack.push(caller('PA3KUS'));
        render(PileupDrawer);
        flushSync();
        // Count badge in the header.
        expect(screen.getByText('(3)')).toBeInTheDocument();
        // Head (K1ABC) has no up-arrow; the other two do.
        expect(screen.queryByLabelText('Move K1ABC up the pile-up')).toBeNull();
        expect(screen.getByLabelText('Move 9A4ZM up the pile-up')).toBeInTheDocument();
        expect(screen.getByLabelText('Move PA3KUS up the pile-up')).toBeInTheDocument();
    });

    it('per-row remove drops that caller', async () => {
        ft8PileupStack.push(caller('K1ABC'));
        ft8PileupStack.push(caller('9A4ZM'));
        render(PileupDrawer);
        flushSync();
        await fireEvent.click(screen.getByLabelText('Remove K1ABC from the pile-up'));
        flushSync();
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['9A4ZM']);
    });

    it('per-row move-up promotes a caller toward the head', async () => {
        ft8PileupStack.push(caller('K1ABC'));
        ft8PileupStack.push(caller('9A4ZM'));
        render(PileupDrawer);
        flushSync();
        await fireEvent.click(screen.getByLabelText('Move 9A4ZM up the pile-up'));
        flushSync();
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['9A4ZM', 'K1ABC']);
    });

    it('Resume shows only when paused and un-pauses the drain', async () => {
        ft8PileupStack.push(caller('K1ABC'));
        render(PileupDrawer);
        flushSync();
        // Not paused → no Resume.
        expect(screen.queryByText('Resume')).toBeNull();
        ft8PileupStack.pause();
        flushSync();
        const resume = screen.getByText('Resume');
        await fireEvent.click(resume);
        flushSync();
        expect(ft8PileupStack.enabled).toBe(true);
    });

    it('hides Resume during a caller (Call-CQ) run even when paused', () => {
        ft8PileupStack.push(caller('K1ABC'));
        ft8PileupStack.pause();
        ft8State.qso.active = true;
        ft8State.qso.role = 'caller';
        render(PileupDrawer);
        flushSync();
        expect(screen.queryByText('Resume')).toBeNull();
    });

    it('Clear & abandon pauses and empties the queue', async () => {
        ft8PileupStack.push(caller('K1ABC'));
        ft8PileupStack.push(caller('9A4ZM'));
        render(PileupDrawer);
        flushSync();
        await fireEvent.click(screen.getByText(/Clear & abandon/));
        flushSync();
        expect(ft8PileupStack.items).toEqual([]);
        expect(ft8PileupStack.enabled).toBe(false);
    });
});
