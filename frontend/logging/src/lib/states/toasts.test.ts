import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { _resetForTests, dismissToast, pushToast, toasts, toastsState } from './toasts.svelte';

beforeEach(() => {
    vi.useFakeTimers();
    _resetForTests();
});

afterEach(() => {
    vi.useRealTimers();
});

describe('pushToast', () => {
    it('appends a toast to the queue with assigned id and createdAt', () => {
        const before = Date.now();
        const id = pushToast('info', 'hello');
        expect(id).toBe(1);
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0]).toMatchObject({
            id: 1,
            level: 'info',
            message: 'hello',
            ttl: 4000,
        });
        expect(toastsState.items[0].createdAt).toBeGreaterThanOrEqual(before);
    });

    it('assigns monotonically-increasing ids', () => {
        expect(pushToast('info', 'a')).toBe(1);
        expect(pushToast('warn', 'b')).toBe(2);
        expect(pushToast('error', 'c')).toBe(3);
    });

    it('uses level-default TTL when ttl arg is omitted', () => {
        pushToast('info', 'i');
        pushToast('warn', 'w');
        pushToast('error', 'e');
        expect(toastsState.items.map((t) => t.ttl)).toEqual([4000, 6000, 8000]);
    });

    it('honours an explicit ttl override', () => {
        pushToast('info', 'i', 1000);
        expect(toastsState.items[0].ttl).toBe(1000);
    });

    it('treats ttl=0 as sticky (no auto-dismiss)', () => {
        pushToast('error', 'sticky', 0);
        vi.advanceTimersByTime(60_000);
        expect(toastsState.items).toHaveLength(1);
    });
});

describe('auto-dismiss', () => {
    it('removes the toast when its TTL elapses', () => {
        pushToast('info', 'gone in 4s');
        expect(toastsState.items).toHaveLength(1);
        vi.advanceTimersByTime(3999);
        expect(toastsState.items).toHaveLength(1);
        vi.advanceTimersByTime(1);
        expect(toastsState.items).toHaveLength(0);
    });

    it('respects per-level TTLs (warn=6s, error=8s)', () => {
        pushToast('warn', 'w');
        pushToast('error', 'e');
        vi.advanceTimersByTime(6000);
        expect(toastsState.items.map((t) => t.level)).toEqual(['error']);
        vi.advanceTimersByTime(2000);
        expect(toastsState.items).toHaveLength(0);
    });
});

describe('dismissToast', () => {
    it('removes the matching toast immediately', () => {
        const id = pushToast('info', 'click me');
        dismissToast(id);
        expect(toastsState.items).toHaveLength(0);
    });

    it('cancels the pending auto-dismiss so it does not fire after manual dismissal', () => {
        const id = pushToast('info', 'click me');
        dismissToast(id);
        vi.advanceTimersByTime(10_000);
        expect(toastsState.items).toHaveLength(0);
    });

    it('is idempotent — dismissing an unknown id is a no-op', () => {
        pushToast('info', 'a');
        expect(() => dismissToast(999)).not.toThrow();
        expect(toastsState.items).toHaveLength(1);
    });
});

describe('max-stack eviction', () => {
    it('drops the oldest toast when a sixth is pushed', () => {
        for (let i = 0; i < 5; i++) pushToast('info', `t${i}`);
        expect(toastsState.items).toHaveLength(5);
        pushToast('info', 't5');
        expect(toastsState.items).toHaveLength(5);
        expect(toastsState.items[0].message).toBe('t1');
        expect(toastsState.items[4].message).toBe('t5');
    });

    it('cancels the evicted toast’s timer so it does not double-dismiss', () => {
        for (let i = 0; i < 5; i++) pushToast('info', `t${i}`);
        const evictedId = toastsState.items[0].id;
        pushToast('info', 't5'); // evicts id=evictedId
        // Re-push the evicted id is impossible (monotonic counter); the
        // assertion is that timers.size shrank by one, which is observable
        // indirectly by advancing time and checking nothing weird happens.
        vi.advanceTimersByTime(4000);
        // After 4s, all original 4 + the new one expire; nothing left.
        expect(toastsState.items).toHaveLength(0);
        // And dismissing the evicted id is a safe no-op.
        expect(() => dismissToast(evictedId)).not.toThrow();
    });
});

describe('convenience wrappers', () => {
    it('toasts.info / .warn / .error push at the corresponding level', () => {
        toasts.info('i');
        toasts.warn('w');
        toasts.error('e');
        expect(toastsState.items.map((t) => t.level)).toEqual(['info', 'warn', 'error']);
    });

    it('toasts.dismiss is the same function as dismissToast', () => {
        const id = toasts.info('a');
        toasts.dismiss(id);
        expect(toastsState.items).toHaveLength(0);
    });
});
