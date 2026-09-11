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
import { setMode as setRouterMode } from '../router.svelte';
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
    ft8State.claimed = true; // …and only once the profile claim stands (ADR 0080)
    ft8State.connected = true; // …with its stream open (a claim alone can be stale)
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

// ADR 0080: arming is a TX-starting intent, so it waits for the profile claim —
// an FT4-labelled view must not arm the still-active FT8 profile. Disarm stays
// available once armed even without the claim (a transient SSE loss must not
// trap TX armed).
describe("the Operate heading names the view's profile (dogfood 2026-09-11)", () => {
    it('reads Operate · FT8 on the FT8 view and Operate · FT4 on the FT4 view, whatever was last granted', () => {
        setRouterMode('ft8');
        ft8State.profile = 'FT4'; // a remembered grant from an earlier FT4 session
        render(Ft8Operate);
        flushSync();
        expect(screen.getByRole('heading', { name: 'Operate · FT8' })).toBeTruthy();
        setRouterMode('ft4');
        flushSync();
        expect(screen.getByRole('heading', { name: 'Operate · FT4' })).toBeTruthy();
        setRouterMode('phone');
    });
});

describe('Enable TX waits for the profile claim (ADR 0080)', () => {
    it('is disabled with CAT live but no claim standing', () => {
        ft8State.claimed = false;
        render(Ft8Operate);
        flushSync();
        expect(screen.getByRole('button', { name: 'Enable TX' })).toBeDisabled();
    });

    it('is enabled once the claim stands', () => {
        ft8State.claimed = true;
        render(Ft8Operate);
        flushSync();
        expect(screen.getByRole('button', { name: 'Enable TX' })).toBeEnabled();
    });

    it('keeps Disable TX enabled while armed even if the claim is gone', () => {
        ft8State.claimed = false;
        ft8State.tx.armed = true;
        render(Ft8Operate);
        flushSync();
        expect(screen.getByRole('button', { name: 'Disable TX' })).toBeEnabled();
    });

    // codex 67cc1b96 P1: a claim outlives its stream, but the daemon may be on
    // the other profile by the time the stream is back — Enable waits for it.
    it('is disabled while the claimed stream is down; Disable TX stays enabled while armed', () => {
        ft8State.claimed = true;
        ft8State.connected = false;
        render(Ft8Operate);
        flushSync();
        expect(screen.getByRole('button', { name: 'Enable TX' })).toBeDisabled();
        ft8State.tx.armed = true;
        flushSync();
        expect(screen.getByRole('button', { name: 'Disable TX' })).toBeEnabled();
    });
});
