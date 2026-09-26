import { describe, expect, it } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import StoredSecretField from './StoredSecretField.svelte';

/*
    A stored credential as a STATUS, not an empty box (operator ruling
    2026-09-26). The daemon never sends a stored value back, so a saved field
    shows "✓ saved" with Replace (and Remove where allowed) and no input; the
    input appears only when there is something to type — nothing stored, or
    Replace pressed. A pending removal is its own line with Undo. Help sits
    behind an ⓘ, not a line under the field.
*/

function setup(props: Record<string, unknown> = {}) {
    const calls = { input: [] as string[], remove: 0, undo: 0 };
    render(StoredSecretField, {
        props: {
            label: 'API key',
            kind: 'password',
            stored: false,
            value: '',
            cleared: false,
            removable: true,
            oninput: (v: string) => calls.input.push(v),
            onremove: () => calls.remove++,
            onundo: () => calls.undo++,
            ...props,
        },
    });
    return calls;
}

describe('StoredSecretField', () => {
    it('S1: nothing stored shows the labelled input and no status', () => {
        setup({
            emptyPlaceholder: 'M0ABC — the logbook’s callsign unless you type another',
            kind: 'text',
        });
        const input = screen.getByLabelText('API key');
        expect(input.getAttribute('placeholder')).toMatch(/^M0ABC/);
        expect(screen.queryByTestId('saved-status')).toBeNull();
        expect(screen.queryByRole('button', { name: /replace/i })).toBeNull();
    });

    it('S2: a stored value is a status line with Replace and Remove, and no input', () => {
        setup({ stored: true });
        expect(screen.getByTestId('saved-status').textContent).toMatch(/✓\s*saved/);
        expect(screen.queryByLabelText('API key')).toBeNull();
        expect(screen.getByRole('button', { name: 'Replace API key' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Remove API key' })).toBeInTheDocument();
        expect(document.body.textContent).not.toMatch(/leave blank to keep/);
    });

    it('S2b: Remove is offered only where removal is allowed', () => {
        setup({ stored: true, removable: false });
        expect(screen.queryByRole('button', { name: /^Remove/ })).toBeNull();
    });

    it('S3: Replace opens an empty input; Cancel drops what was typed and returns to the status', async () => {
        const calls = setup({ stored: true });
        await fireEvent.click(screen.getByRole('button', { name: 'Replace API key' }));
        const input = screen.getByLabelText('API key');
        expect((input as HTMLInputElement).value).toBe('');
        await fireEvent.input(input, { target: { value: 'NEW' } });
        expect(calls.input).toEqual(['NEW']);
        await fireEvent.click(screen.getByRole('button', { name: 'Cancel replacing API key' }));
        expect(calls.input).toEqual(['NEW', '']);
        expect(screen.getByTestId('saved-status')).toBeInTheDocument();
        expect(screen.queryByLabelText('API key')).toBeNull();
    });

    it('S3b: Cancel over a typed value sends a blank, so the saved value is kept', async () => {
        const calls = setup({ stored: true, value: 'TYPED' });
        await fireEvent.click(screen.getByRole('button', { name: 'Cancel replacing API key' }));
        expect(calls.input).toEqual(['']);
    });

    it('S4: a typed value over a stored one keeps the input open (e.g. after a refused save)', () => {
        setup({ stored: true, value: 'TYPED' });
        expect(screen.getByLabelText<HTMLInputElement>('API key').value).toBe('TYPED');
        expect(
            screen.getByRole('button', { name: 'Cancel replacing API key' })
        ).toBeInTheDocument();
    });

    it('S5: a pending removal is its own line with Undo, and no input', async () => {
        const calls = setup({ stored: true, cleared: true, removedNote: 'Removed when you save.' });
        expect(screen.getByTestId('removal-pending').textContent).toMatch(
            /Removed when you save\./
        );
        expect(screen.queryByLabelText('API key')).toBeNull();
        await fireEvent.click(screen.getByRole('button', { name: 'Undo removing API key' }));
        expect(calls.undo).toBe(1);
    });

    it('S5b: Remove reports the intent to the parent', async () => {
        const calls = setup({ stored: true, removeLabel: 'Reset to default' });
        await fireEvent.click(screen.getByRole('button', { name: 'Reset to default API key' }));
        expect(calls.remove).toBe(1);
    });

    it('S6: help sits behind an ⓘ whose tooltip holds it, not a line under the field', () => {
        setup({ help: 'Your QRZ Logbook API key.' });
        const tip = screen.getByRole('button', { name: 'About API key' });
        const text = document.getElementById(tip.getAttribute('aria-describedby')!)!;
        expect(text.textContent).toBe('Your QRZ Logbook API key.');
        expect(text.className).toMatch(/\bhidden\b/);
        expect(text.className).toMatch(/peer-focus:block/);
    });

    it('S7: a marked field says why, on the input', () => {
        setup({ invalid: true, invalidNote: 'Required to turn this on.' });
        expect(screen.getByLabelText('API key').getAttribute('aria-invalid')).toBe('true');
        expect(screen.getByText('Required to turn this on.')).toBeInTheDocument();
    });

    it('S8: a marked stored field opens its input so the mark is visible', () => {
        setup({ stored: true, cleared: false, invalid: true });
        expect(screen.getByLabelText('API key')).toBeInTheDocument();
    });

    it('S9: disabled (a save in flight) disables the actions', () => {
        setup({ stored: true, disabled: true });
        expect(screen.getByRole('button', { name: 'Replace API key' })).toBeDisabled();
        expect(screen.getByRole('button', { name: 'Remove API key' })).toBeDisabled();
    });
});
