import { afterEach, describe, expect, it } from 'vitest';
import { ft8PileupStack, type PileupEntry } from './ft8PileupStack.svelte';

function entry(
    call: string,
    snr = -10,
    grid = 'JO21',
    slotUtc = '2026-06-17T14:30:00Z'
): PileupEntry {
    return { call, grid, snr, slotUtc };
}

afterEach(() => {
    ft8PileupStack.clear();
    ft8PileupStack.resume();
});

describe('ft8PileupStack', () => {
    it('pushes FIFO — newest at the tail, head worked next', () => {
        ft8PileupStack.push(entry('K1ABC'));
        ft8PileupStack.push(entry('9A4ZM'));
        ft8PileupStack.push(entry('PA3KUS'));
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['K1ABC', '9A4ZM', 'PA3KUS']);
        expect(ft8PileupStack.peek()?.call).toBe('K1ABC');
        expect(ft8PileupStack.count).toBe(3);
    });

    it('normalises the call and ignores empty', () => {
        ft8PileupStack.push(entry('  pa3kus '));
        ft8PileupStack.push(entry('   '));
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['PA3KUS']);
    });

    it('dedup refreshes data in place without reordering', () => {
        ft8PileupStack.push(entry('K1ABC', -15));
        ft8PileupStack.push(entry('9A4ZM', -8));
        // Re-capture K1ABC with a stronger, later decode.
        ft8PileupStack.push(entry('k1abc', -3, 'FN42', '2026-06-17T14:31:00Z'));
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['K1ABC', '9A4ZM']); // order kept
        expect(ft8PileupStack.peek()).toMatchObject({ call: 'K1ABC', snr: -3, grid: 'FN42' });
    });

    it('push reports whether the entry was new (true) or refreshed/empty (false)', () => {
        // The Ctrl+click handler relies on this to resume the drain ONLY for a
        // genuinely new caller — a re-click of a queued station must not un-pause.
        expect(ft8PileupStack.push(entry('K1ABC'))).toBe(true); // new → appended
        expect(ft8PileupStack.push(entry('k1abc', -3))).toBe(false); // dedup → refreshed
        expect(ft8PileupStack.push(entry('   '))).toBe(false); // empty → no-op
        expect(ft8PileupStack.push(entry('9A4ZM'))).toBe(true); // another new caller
    });

    it('dequeue pops the head (oldest)', () => {
        ft8PileupStack.push(entry('K1ABC'));
        ft8PileupStack.push(entry('9A4ZM'));
        expect(ft8PileupStack.dequeue()?.call).toBe('K1ABC');
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['9A4ZM']);
        expect(ft8PileupStack.dequeue()?.call).toBe('9A4ZM');
        expect(ft8PileupStack.dequeue()).toBeUndefined();
    });

    it('remove drops a specific entry; out-of-bounds is a no-op', () => {
        ft8PileupStack.push(entry('K1ABC'));
        ft8PileupStack.push(entry('9A4ZM'));
        ft8PileupStack.push(entry('PA3KUS'));
        ft8PileupStack.remove(1);
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['K1ABC', 'PA3KUS']);
        ft8PileupStack.remove(9);
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['K1ABC', 'PA3KUS']);
    });

    it('clear empties the queue', () => {
        ft8PileupStack.push(entry('K1ABC'));
        ft8PileupStack.clear();
        expect(ft8PileupStack.items).toEqual([]);
        expect(ft8PileupStack.count).toBe(0);
    });

    it('pause/resume toggle the drain flag (default enabled)', () => {
        expect(ft8PileupStack.enabled).toBe(true);
        ft8PileupStack.pause();
        expect(ft8PileupStack.enabled).toBe(false);
        ft8PileupStack.resume();
        expect(ft8PileupStack.enabled).toBe(true);
    });
});
