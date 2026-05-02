import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import Callsign from './Callsign.svelte';

/**
 * Callsign component. Domain-specific input with three behaviours
 * beyond plain text entry:
 *   - Validation via isValidCallsign (real-time as the operator types)
 *   - Focus-trap on blur when the value is invalid AND non-empty
 *   - Tab → onenrich callback (Tab only, not Shift+Tab) when value is
 *     valid + non-empty; uppercase-normalized to the callback
 *   - Uppercase visual styling via the `uppercase` Tailwind class
 *
 * Per the project's validator-presence convention, an empty value is
 * treated as "not invalid" — the validator has no objection to absence;
 * required-ness is enforced at the form layer when the QSO draft store
 * lands. Callsign therefore does NOT trap focus on empty blur.
 */

describe('Callsign', () => {
    afterEach(() => cleanup());

    function setup(props: { value?: string; onenrich?: (cs: string) => void } = {}) {
        const onenrich = props.onenrich ?? vi.fn();
        const { container } = render(Callsign, {
            id: 'test-call',
            label: 'Callsign',
            value: props.value ?? '',
            onenrich,
        });
        const input = container.querySelector('input') as HTMLInputElement;
        return { container, input, onenrich };
    }

    describe('rendering', () => {
        it('renders the label', () => {
            const { container } = setup();
            expect(container.textContent ?? '').toContain('Callsign');
        });

        it('applies the uppercase visual class to the input', () => {
            const { input } = setup();
            expect(input.classList.contains('uppercase')).toBe(true);
        });

        it('renders empty by default', () => {
            const { input } = setup();
            expect(input.value).toBe('');
        });

        it('renders the initial value when provided', () => {
            const { input } = setup({ value: 'M0ABC' });
            expect(input.value).toBe('M0ABC');
        });
    });

    describe('validation', () => {
        it('flags invalid input via aria-invalid + .invalid-input', async () => {
            const { input } = setup();
            await fireEvent.input(input, { target: { value: 'M0' } }); // too short
            expect(input.getAttribute('aria-invalid')).toBe('true');
            expect(input.classList.contains('invalid-input')).toBe(true);
        });

        it('clears invalid styling once input becomes valid', async () => {
            const { input } = setup();
            await fireEvent.input(input, { target: { value: 'M0' } });
            expect(input.getAttribute('aria-invalid')).toBe('true');
            await fireEvent.input(input, { target: { value: 'M0ABC' } });
            expect(input.getAttribute('aria-invalid')).toBe('false');
            expect(input.classList.contains('invalid-input')).toBe(false);
        });

        it('treats empty input as not invalid (presence is form-level)', async () => {
            const { input } = setup();
            await fireEvent.input(input, { target: { value: '' } });
            expect(input.getAttribute('aria-invalid')).toBe('false');
        });
    });

    describe('focus-trap on blur', () => {
        it('refocuses the input when blurring with an invalid non-empty value', async () => {
            const { input } = setup({ value: 'M0' }); // invalid
            await fireEvent.blur(input);
            expect(document.activeElement).toBe(input);
        });

        it('selects the input contents when refocusing after invalid blur', async () => {
            const { input } = setup({ value: 'M0' });
            await fireEvent.blur(input);
            // selectionStart/End should span the whole value
            expect(input.selectionStart).toBe(0);
            expect(input.selectionEnd).toBe(input.value.length);
        });

        it('does not trap focus when blurring with an empty value', async () => {
            const { input } = setup({ value: '' });
            // Focus a sibling element to give blur somewhere to go
            const sibling = document.createElement('button');
            document.body.appendChild(sibling);
            input.focus();
            sibling.focus();
            await fireEvent.blur(input);
            expect(document.activeElement).not.toBe(input);
            sibling.remove();
        });

        it('does not trap focus when blurring with a valid value', async () => {
            const { input } = setup({ value: 'M0ABC' });
            const sibling = document.createElement('button');
            document.body.appendChild(sibling);
            input.focus();
            sibling.focus();
            await fireEvent.blur(input);
            expect(document.activeElement).not.toBe(input);
            sibling.remove();
        });
    });

    describe('Tab → onenrich', () => {
        it('fires onenrich on Tab when value is valid + non-empty', async () => {
            const onenrich = vi.fn();
            const { input } = setup({ value: 'M0ABC', onenrich });
            await fireEvent.keyDown(input, { key: 'Tab' });
            expect(onenrich).toHaveBeenCalledTimes(1);
            expect(onenrich).toHaveBeenCalledWith('M0ABC');
        });

        it('uppercase-normalizes the callsign before invoking onenrich', async () => {
            const onenrich = vi.fn();
            const { input } = setup({ value: 'm0abc', onenrich });
            await fireEvent.keyDown(input, { key: 'Tab' });
            expect(onenrich).toHaveBeenCalledWith('M0ABC');
        });

        it('trims surrounding whitespace before invoking onenrich', async () => {
            const onenrich = vi.fn();
            const { input } = setup({ value: '  M0ABC  ', onenrich });
            await fireEvent.keyDown(input, { key: 'Tab' });
            expect(onenrich).toHaveBeenCalledWith('M0ABC');
        });

        it('does not fire onenrich on Tab when value is empty', async () => {
            const onenrich = vi.fn();
            const { input } = setup({ value: '', onenrich });
            await fireEvent.keyDown(input, { key: 'Tab' });
            expect(onenrich).not.toHaveBeenCalled();
        });

        it('does not fire onenrich on Tab when value is whitespace-only', async () => {
            const onenrich = vi.fn();
            const { input } = setup({ value: '   ', onenrich });
            await fireEvent.keyDown(input, { key: 'Tab' });
            expect(onenrich).not.toHaveBeenCalled();
        });

        it('does not fire onenrich on Tab when value is invalid', async () => {
            const onenrich = vi.fn();
            const { input } = setup({ value: 'M0', onenrich });
            await fireEvent.keyDown(input, { key: 'Tab' });
            expect(onenrich).not.toHaveBeenCalled();
        });

        it('does not fire onenrich on Shift+Tab even when value is valid', async () => {
            const onenrich = vi.fn();
            const { input } = setup({ value: 'M0ABC', onenrich });
            await fireEvent.keyDown(input, { key: 'Tab', shiftKey: true });
            expect(onenrich).not.toHaveBeenCalled();
        });

        it('does not fire onenrich on other keys', async () => {
            const onenrich = vi.fn();
            const { input } = setup({ value: 'M0ABC', onenrich });
            await fireEvent.keyDown(input, { key: 'Enter' });
            await fireEvent.keyDown(input, { key: ' ' });
            await fireEvent.keyDown(input, { key: 'a' });
            expect(onenrich).not.toHaveBeenCalled();
        });

        it('tolerates absence of onenrich callback (optional prop)', async () => {
            const { container } = render(Callsign, {
                id: 'test-call-no-cb',
                label: 'Callsign',
                value: 'M0ABC',
                // intentionally no onenrich
            });
            const input = container.querySelector('input') as HTMLInputElement;
            await expect(
                fireEvent.keyDown(input, { key: 'Tab' }),
            ).resolves.not.toThrow();
        });
    });
});
