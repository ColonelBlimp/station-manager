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

    it('moveUp swaps an entry toward the head; head and out-of-bounds are no-ops', () => {
        ft8PileupStack.push(entry('K1ABC'));
        ft8PileupStack.push(entry('9A4ZM'));
        ft8PileupStack.push(entry('PA3KUS'));
        // Prioritise PA3KUS (index 2 → 1).
        ft8PileupStack.moveUp(2);
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['K1ABC', 'PA3KUS', '9A4ZM']);
        // Again → reaches the head.
        ft8PileupStack.moveUp(1);
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['PA3KUS', 'K1ABC', '9A4ZM']);
        // Head and out-of-bounds: no change.
        ft8PileupStack.moveUp(0);
        ft8PileupStack.moveUp(9);
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['PA3KUS', 'K1ABC', '9A4ZM']);
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

    // Run parity lock — the stable anchor the panel enforces against. The stack only
    // SETS it (from the first add) and clears it; the wrong-parity rejection is the
    // panel's job (it owns the toast + the new-run detection).
    const ODD = '2026-06-17T14:30:15Z'; // :15 → odd
    const EVEN = '2026-06-17T14:30:00Z'; // :00 → even

    it('first push locks the run parity; later pushes do not change it', () => {
        expect(ft8PileupStack.lockedParity).toBe('');
        ft8PileupStack.push(entry('K1ABC', -10, 'JO21', EVEN));
        expect(ft8PileupStack.lockedParity).toBe('even');
        // A later add (even an odd-slot one — the stack doesn't reject) leaves the lock.
        ft8PileupStack.push(entry('9A4ZM', -10, 'JO21', ODD));
        expect(ft8PileupStack.lockedParity).toBe('even');
    });

    it('clear() and clearLock() both unlock; clearLock keeps the queue', () => {
        ft8PileupStack.push(entry('K1ABC', -10, 'JO21', ODD));
        expect(ft8PileupStack.lockedParity).toBe('odd');
        ft8PileupStack.clearLock();
        expect(ft8PileupStack.lockedParity).toBe('');
        expect(ft8PileupStack.items.length).toBe(1); // queue untouched
        // Re-locks on the next add.
        ft8PileupStack.push(entry('PA3KUS', -10, 'JO21', EVEN));
        expect(ft8PileupStack.lockedParity).toBe('even');
        ft8PileupStack.clear();
        expect(ft8PileupStack.lockedParity).toBe('');
        expect(ft8PileupStack.items).toEqual([]);
    });
});
