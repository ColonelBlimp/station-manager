import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
    contestHistory,
    loadContestHistory,
    noteContestQso,
    setContestHistoryLoader,
} from './contestHistory.svelte';

beforeEach(() => {
    setContestHistoryLoader(null);
    contestHistory.key = '';
    contestHistory.completions = [];
});

describe('contest history store', () => {
    it('loads the occurrence and adds each new contact of it', async () => {
        setContestHistoryLoader(vi.fn(() => Promise.resolve([3000, 1000])));
        await loadContestHistory('UKEIDXSSB', '20261031');
        expect(contestHistory.completions).toEqual([3000, 1000]);
        noteContestQso('UKEIDXSSB', '20261031', 5000);
        noteContestQso('UKEIDXCW', '20270424', 9000); // another occurrence: ignored
        expect(contestHistory.completions).toEqual([3000, 1000, 5000]);
    });

    it('a load for an earlier choice never overwrites a later one', async () => {
        let release: (v: number[]) => void = () => {};
        const loader = vi
            .fn()
            .mockImplementationOnce(() => new Promise<number[]>((r) => (release = r)))
            .mockResolvedValueOnce([7]);
        setContestHistoryLoader(loader);
        const first = loadContestHistory('UKEIDXSSB', '20261031');
        await loadContestHistory('UKEIDXCW', '20270424');
        release([1, 2, 3]);
        await first;
        expect(contestHistory.key).toBe('UKEIDXCW|20270424');
        expect(contestHistory.completions).toEqual([7]);
    });

    // Codex review ad746e2f: a contact logged while the history is loading
    // must survive the load's earlier snapshot.
    it('keeps a contact logged while the history was loading', async () => {
        let release: (v: number[]) => void = () => {};
        setContestHistoryLoader(() => new Promise<number[]>((r) => (release = r)));
        const load = loadContestHistory('UKEIDXSSB', '20261031');
        noteContestQso('UKEIDXSSB', '20261031', 9000);
        release([1000, 9000 - 0, 3000].filter((v) => v !== 9000)); // the daemon had not seen it
        await load;
        expect([...contestHistory.completions].sort()).toEqual([1000, 3000, 9000]);
        // ...and one the daemon already returned is not counted twice.
        setContestHistoryLoader(() => new Promise<number[]>((r) => (release = r)));
        const again = loadContestHistory('UKEIDXSSB', '20261031');
        noteContestQso('UKEIDXSSB', '20261031', 5000);
        release([1000, 5000]);
        await again;
        expect([...contestHistory.completions].sort()).toEqual([1000, 5000]);
    });

    it('a failed load leaves no history rather than a wrong one', async () => {
        setContestHistoryLoader(vi.fn(() => Promise.resolve(null)));
        await loadContestHistory('UKEIDXSSB', '20261031');
        expect(contestHistory.completions).toEqual([]);
        expect(contestHistory.failed).toBe(true);
    });
});
