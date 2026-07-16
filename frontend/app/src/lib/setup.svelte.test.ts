import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setup, setSetupSave, saveSetup, dismissSetupDone, _resetSetupForTests } from './setup.svelte';

beforeEach(() => {
    _resetSetupForTests();
});

describe('setup gate state', () => {
    it('starts loading with no interstitial', () => {
        expect(setup.status).toBe('loading');
        expect(setup.justCompleted).toBe(false);
    });

    it('a successful save opens the gate and arms the interstitial', async () => {
        const save = vi.fn().mockResolvedValue({ ok: true, message: '' });
        setSetupSave(save);
        setup.status = 'needed';

        const out = await saveSetup('7Q5MLV');
        expect(out.ok).toBe(true);
        expect(save).toHaveBeenCalledWith('7Q5MLV');
        expect(setup.status).toBe('complete');
        expect(setup.justCompleted).toBe(true);
    });

    it('a failed save keeps the gate closed and reports the message', async () => {
        setSetupSave(vi.fn().mockResolvedValue({ ok: false, message: 'invalid callsign' }));
        setup.status = 'needed';

        const out = await saveSetup('BAD');
        expect(out).toEqual({ ok: false, message: 'invalid callsign' });
        expect(setup.status).toBe('needed');
        expect(setup.justCompleted).toBe(false);
    });

    it('refuses when no save action is wired (boot never ran)', async () => {
        const out = await saveSetup('7Q5MLV');
        expect(out.ok).toBe(false);
        expect(out.message).not.toBe('');
    });

    it('dismissSetupDone clears the interstitial', async () => {
        setSetupSave(vi.fn().mockResolvedValue({ ok: true, message: '' }));
        await saveSetup('7Q5MLV');
        expect(setup.justCompleted).toBe(true);
        dismissSetupDone();
        expect(setup.justCompleted).toBe(false);
        expect(setup.status).toBe('complete');
    });
});
