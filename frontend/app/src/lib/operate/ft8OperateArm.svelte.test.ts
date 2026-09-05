// Ft8Operate's Enable/Disable TX button maps the FT8 arm confirm-by-push outcome
// three ways for the operator (F-04, ADR 0078): silent on a success (accepted /
// observed / superseded), a WARN on unknown, an ERROR only on a definite failure.
// The old `if (!r.ok) toasts.error` collapsed a fired timeout — no response, the
// request possibly committed — into a false failure the ft8-tx SSE then contradicted. The button itself renders only pushed state,
// so no outcome here ever flips it.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Ft8Operate from './Ft8Operate.svelte';
import { ft8State, resetFt8ForTests, armTx } from './ft8.svelte';
import { rig } from './rig.svelte';
import { toasts, _resetForTests as resetToasts } from '../ui/toasts.svelte';

// Mock only armTx so the button's outcome handling can be driven directly; the
// confirm-by-push mechanics themselves are pinned in ft8.svelte.test.ts.
vi.mock('./ft8.svelte', async (importOriginal) => {
    const actual = await importOriginal<typeof import('./ft8.svelte')>();
    return { ...actual, armTx: vi.fn() };
});

beforeEach(() => {
    vi.clearAllMocks();
    resetFt8ForTests();
    resetToasts();
    rig.cat = 'connected'; // the arm control is enabled only with CAT live
    rig.freq = '14.074.000';
    ft8State.tx.armed = false; // → "Enable TX"
});

async function clickEnable(): Promise<void> {
    render(Ft8Operate);
    flushSync();
    await fireEvent.click(screen.getByRole('button', { name: 'Enable TX' }));
}

describe('Enable TX — confirm-by-push outcomes (F-04)', () => {
    it('warns (not errors) on an unknown outcome, with the outcome wording verbatim', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        vi.mocked(armTx).mockResolvedValue({ status: 'unknown', message: 'could not confirm' });

        await clickEnable();

        expect(warn).toHaveBeenCalledExactlyOnceWith('could not confirm');
        expect(error).not.toHaveBeenCalled();
        expect(ft8State.tx.armed).toBe(false); // unknown never renders TX as enabled
    });

    it('is silent on an accepted outcome', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        const info = vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        vi.mocked(armTx).mockResolvedValue({ status: 'accepted' });

        await clickEnable();

        expect(warn).not.toHaveBeenCalled();
        expect(error).not.toHaveBeenCalled();
        expect(info).not.toHaveBeenCalled();
    });

    it('is silent on a superseded outcome (a newer request took over)', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        vi.mocked(armTx).mockResolvedValue({ status: 'superseded' });

        await clickEnable();

        expect(warn).not.toHaveBeenCalled();
        expect(error).not.toHaveBeenCalled();
    });

    it('errors on a definite refusal, and the control stays disabled-state', async () => {
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        vi.mocked(armTx).mockResolvedValue({
            status: 'failed',
            kind: 'refused',
            message: 'rig not ready',
        });

        await clickEnable();

        expect(error).toHaveBeenCalledExactlyOnceWith('rig not ready');
        expect(ft8State.tx.armed).toBe(false); // a refused arm is never shown as armed
    });
});
