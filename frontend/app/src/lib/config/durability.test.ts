import { describe, it, expect, vi, afterEach } from 'vitest';
import { isDurabilityUnconfirmed, noteConfigDurability } from './durability';
import { toasts } from '../ui/toasts.svelte';

afterEach(() => vi.restoreAllMocks());

describe('isDurabilityUnconfirmed', () => {
    it('is true only for durability === "unconfirmed"', () => {
        expect(isDurabilityUnconfirmed({ durability: 'unconfirmed' })).toBe(true);
        expect(isDurabilityUnconfirmed({ durability: '' })).toBe(false);
        expect(isDurabilityUnconfirmed({})).toBe(false); // omitted on an ordinary durable save
        expect(isDurabilityUnconfirmed(null)).toBe(false);
        expect(isDurabilityUnconfirmed('nope')).toBe(false);
    });
});

describe('noteConfigDurability', () => {
    it('toasts the single combined caveat and returns true when unconfirmed', () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        expect(noteConfigDurability(true)).toBe(true);
        expect(warn).toHaveBeenCalledOnce();
        expect(String(warn.mock.calls[0][0])).toMatch(/survive a crash/i);
    });

    it('does nothing and returns false on an ordinary durable save', () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        expect(noteConfigDurability(false)).toBe(false);
        expect(warn).not.toHaveBeenCalled();
    });
});
