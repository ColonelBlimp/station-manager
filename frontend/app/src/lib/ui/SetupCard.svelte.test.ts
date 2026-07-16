// First-run setup card: welcome form → save → interstitial. The save action
// is injected (setSetupSave), so these run without a daemon.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SetupCard from './SetupCard.svelte';
import { setup, setSetupSave, _resetSetupForTests } from '../setup.svelte';
import { toastsState, _resetForTests as _resetToastsForTests } from './toasts.svelte';

beforeEach(() => {
    _resetSetupForTests();
    setup.status = 'needed';
    _resetToastsForTests();
});

describe('SetupCard — welcome form', () => {
    it('renders the welcome copy with Save disabled until a callsign is typed', async () => {
        render(SetupCard);
        expect(screen.getByText(/Welcome to Station Manager/)).toBeInTheDocument();
        const save = screen.getByRole('button', { name: 'Save' });
        expect(save).toBeDisabled();

        await fireEvent.input(screen.getByLabelText('Callsign'), {
            target: { value: '7q5mlv' },
        });
        expect(save).not.toBeDisabled();
    });

    it('normalises (trim + uppercase) and saves; success flips to the interstitial', async () => {
        const saveFn = vi.fn().mockResolvedValue({ ok: true, message: '' });
        setSetupSave(saveFn);
        render(SetupCard);

        await fireEvent.input(screen.getByLabelText('Callsign'), {
            target: { value: '  7q5mlv ' },
        });
        await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        expect(saveFn).toHaveBeenCalledWith('7Q5MLV');
        expect(await screen.findByText(/Setup complete/)).toBeInTheDocument();
    });

    it('rejects a malformed callsign with a toast and never calls the daemon', async () => {
        const saveFn = vi.fn();
        setSetupSave(saveFn);
        render(SetupCard);

        // No digit — fails the validator before any network call.
        await fireEvent.input(screen.getByLabelText('Callsign'), {
            target: { value: 'NODIGIT' },
        });
        await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        expect(saveFn).not.toHaveBeenCalled();
        expect(toastsState.items.some((t) => /Invalid callsign/.test(t.message))).toBe(true);
        expect(screen.getByText(/Welcome to Station Manager/)).toBeInTheDocument();
    });

    it('surfaces a daemon rejection as a toast and stays on the form', async () => {
        setSetupSave(vi.fn().mockResolvedValue({ ok: false, message: 'callsign rejected' }));
        render(SetupCard);

        await fireEvent.input(screen.getByLabelText('Callsign'), {
            target: { value: '7Q5MLV' },
        });
        await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        expect(toastsState.items.some((t) => t.message === 'callsign rejected')).toBe(true);
        expect(screen.getByText(/Welcome to Station Manager/)).toBeInTheDocument();
    });
});

describe('SetupCard — interstitial', () => {
    beforeEach(() => {
        setup.status = 'complete';
        setup.justCompleted = true;
    });

    it('Start logging clears the interstitial', async () => {
        render(SetupCard);
        await fireEvent.click(screen.getByRole('button', { name: /Start logging/ }));
        expect(setup.justCompleted).toBe(false);
    });

    it('Open Settings clears the interstitial and routes to the config view', async () => {
        render(SetupCard);
        await fireEvent.click(screen.getByRole('button', { name: /Open Settings/ }));
        expect(setup.justCompleted).toBe(false);
        expect(window.location.pathname).toMatch(/config$/);
    });
});
