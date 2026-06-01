import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { flushSync } from 'svelte';
import { commentHistory, MAX_COMMENTS } from './commentHistory.svelte';

/**
 * Comment paste-list — bounded MRU semantics + localStorage mirror.
 *
 * As with manualState, the module-level singleton is created once on
 * first import, so the fresh-hydration path can't be observed against
 * empty defaults here. We reset the singleton via `clear()` in
 * beforeEach and exercise the add() discipline (order, dedup, cap,
 * empty-skip) plus the live mirror (writes land in localStorage).
 */

const KEY = 'sm.commentHistory.items';

beforeEach(() => {
    try {
        localStorage.clear();
    } catch {
        /* noop */
    }
    commentHistory.clear();
    flushSync();
});

afterEach(() => {
    try {
        localStorage.clear();
    } catch {
        /* noop */
    }
});

describe('commentHistory.add', () => {
    it('prepends new comments (newest first)', () => {
        commentHistory.add('Lot of QRN');
        commentHistory.add('Tnx QSO 73');
        expect(commentHistory.items).toEqual(['Tnx QSO 73', 'Lot of QRN']);
    });

    it('trims surrounding whitespace before storing', () => {
        commentHistory.add('  Lot of QRN  ');
        expect(commentHistory.items).toEqual(['Lot of QRN']);
    });

    it('ignores empty / whitespace-only comments', () => {
        commentHistory.add('');
        commentHistory.add('   ');
        expect(commentHistory.items).toEqual([]);
    });

    it('dedups by moving an existing identical entry back to the top', () => {
        commentHistory.add('Lot of QRN');
        commentHistory.add('Tnx QSO 73');
        commentHistory.add('Lot of QRN'); // re-log the same phrase
        expect(commentHistory.items).toEqual(['Lot of QRN', 'Tnx QSO 73']);
        // No duplicate left behind.
        expect(commentHistory.items.filter((c) => c === 'Lot of QRN')).toHaveLength(1);
    });

    it('caps the list at MAX_COMMENTS, evicting the oldest', () => {
        for (let i = 0; i < MAX_COMMENTS + 5; i++) {
            commentHistory.add(`comment ${i}`);
        }
        expect(commentHistory.items).toHaveLength(MAX_COMMENTS);
        // Newest is at the top.
        expect(commentHistory.items[0]).toBe(`comment ${MAX_COMMENTS + 4}`);
        // The first 5 added have been evicted off the end.
        expect(commentHistory.items).not.toContain('comment 0');
        expect(commentHistory.items).not.toContain('comment 4');
    });
});

describe('commentHistory.clear', () => {
    it('empties the list', () => {
        commentHistory.add('Lot of QRN');
        commentHistory.clear();
        expect(commentHistory.items).toEqual([]);
    });
});

describe('localStorage mirror', () => {
    it('persists the list as a JSON array under sm.commentHistory.items', () => {
        commentHistory.add('Lot of QRN');
        commentHistory.add('Tnx QSO 73');
        flushSync();
        expect(JSON.parse(localStorage.getItem(KEY) ?? 'null')).toEqual([
            'Tnx QSO 73',
            'Lot of QRN',
        ]);
    });

    it('does not throw when localStorage.setItem fails', () => {
        const setItemSpy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
            throw new Error('quota exceeded');
        });
        try {
            expect(() => {
                commentHistory.add('Lot of QRN');
                flushSync();
            }).not.toThrow();
            // In-memory state still updates even when the write fails.
            expect(commentHistory.items).toEqual(['Lot of QRN']);
        } finally {
            setItemSpy.mockRestore();
        }
    });
});
