import { describe, it, expect, beforeEach } from 'vitest';
import { commentHistory, MAX_COMMENTS } from './commentHistory.svelte';

/*
    Recent-comments paste list (restored from the retired logging SPA, W-0003).
    The picker renders straight off `items`, so these pin the MRU discipline it
    relies on: newest-first, trim + dedup (a repeat rises, never duplicates),
    empty ignored, capped with the oldest evicted.
*/

beforeEach(() => {
    localStorage.clear();
    commentHistory.clear();
});

describe('commentHistory', () => {
    it('add prepends, newest first', () => {
        commentHistory.add('first');
        commentHistory.add('second');
        expect(commentHistory.items).toEqual(['second', 'first']);
    });

    it('ignores empty / whitespace-only comments', () => {
        commentHistory.add('   ');
        commentHistory.add('');
        expect(commentHistory.items).toEqual([]);
    });

    it('trims and dedups — a repeat rises to the top instead of duplicating', () => {
        commentHistory.add('Tnx QSO 73');
        commentHistory.add('Lot of QRN');
        commentHistory.add('  Tnx QSO 73  '); // identical after trim
        expect(commentHistory.items).toEqual(['Tnx QSO 73', 'Lot of QRN']);
    });

    it('caps at MAX_COMMENTS, evicting the oldest', () => {
        for (let i = 0; i < MAX_COMMENTS + 3; i++) commentHistory.add(`c${i}`);
        expect(commentHistory.items.length).toBe(MAX_COMMENTS);
        expect(commentHistory.items[0]).toBe(`c${MAX_COMMENTS + 2}`); // newest kept
        expect(commentHistory.items).not.toContain('c0'); // oldest evicted
    });

    it('clear empties the list', () => {
        commentHistory.add('x');
        commentHistory.clear();
        expect(commentHistory.items).toEqual([]);
    });
});
