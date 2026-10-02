import { afterEach, describe, expect, it, vi } from 'vitest';
import { draftLocksAvailable, reserveSavedDraft } from './draftLock';
import { fakeDraftLocks } from './draftLock.fixture';

afterEach(() => vi.unstubAllGlobals());

describe('saved QSO ownership (RS8/RS10; browser RS8/RS11 pending)', () => {
    it('L1 absent or rejected Web Locks never grants a claim', async () => {
        vi.stubGlobal('navigator', {});
        expect(draftLocksAvailable()).toBe(false);
        await expect(reserveSavedDraft('a')).rejects.toThrow(/cannot reserve/);
        vi.stubGlobal('navigator', {
            locks: { request: () => Promise.reject(new Error('denied')) },
        });
        expect(draftLocksAvailable()).toBe(true);
        await expect(reserveSavedDraft('a')).rejects.toThrow('denied');
    });
    it('L2 competing requests have one winner, no queue, and different UUIDs are independent', async () => {
        const locks = fakeDraftLocks();
        vi.stubGlobal('navigator', { locks: locks.manager });
        const [a, b] = await Promise.allSettled([reserveSavedDraft('a'), reserveSavedDraft('a')]);
        expect(a.status).toBe('fulfilled');
        expect(b.status).toBe('rejected');
        if (a.status !== 'fulfilled' || b.status !== 'rejected') return;
        expect(String(b.reason)).toContain('In use in another tab');
        expect(locks.request.mock.calls[0][1]).toEqual({ mode: 'exclusive', ifAvailable: true });
        expect(locks.held.size).toBe(1);
        const other = await reserveSavedDraft('b');
        expect(locks.held.size).toBe(2);
        await other.release();
        await a.value.release();
        expect(locks.held.size).toBe(0);
        const retry = await reserveSavedDraft('a');
        await retry.release();
    });
});
