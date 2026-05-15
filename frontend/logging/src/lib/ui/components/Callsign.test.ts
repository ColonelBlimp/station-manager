import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import Callsign from './Callsign.svelte';

/**
 * Callsign component. Domain-specific input with three behaviours
 * beyond plain text entry:
 *   - Validation via isValidCallsign (real-time as the operator types)
 *   - Invalid flag set on blur (red border + aria-invalid stay); focus
 *     is NOT trapped — operator can Shift+Tab away from a half-typed
 *     callsign to abandon the QSO entry. The submit button is the
 *     load-bearing guard against logging an invalid callsign.
 *   - Tab → onenrich callback (Tab only, not Shift+Tab) when value is
 *     valid + non-empty; uppercase-normalized to the callback
 *   - Uppercase visual styling via the `uppercase` Tailwind class
 *
 * Per the project's validator-presence convention, an empty value is
 * treated as "not invalid" — the validator has no objection to absence;
 * required-ness is enforced at the form layer when the QSO draft store
 * lands.
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

    describe('blur behaviour', () => {
        // I18 — Callsign used to forcibly refocus on invalid blur,
        // trapping keyboard users who wanted to leave the form. New
        // contract: invalid flag is set on blur, focus is respected.

        it('sets aria-invalid on blur with an invalid non-empty value', async () => {
            const { input } = setup({ value: 'M0' });
            await fireEvent.blur(input);
            expect(input.getAttribute('aria-invalid')).toBe('true');
        });

        it('does not refocus on blur with an invalid value (no focus trap)', async () => {
            const { input } = setup({ value: 'M0' });
            const sibling = document.createElement('button');
            document.body.appendChild(sibling);
            input.focus();
            sibling.focus();
            await fireEvent.blur(input);
            expect(document.activeElement).toBe(sibling);
            sibling.remove();
        });

        it('does not refocus on blur with an empty value', async () => {
            const { input } = setup({ value: '' });
            const sibling = document.createElement('button');
            document.body.appendChild(sibling);
            input.focus();
            sibling.focus();
            await fireEvent.blur(input);
            expect(document.activeElement).not.toBe(input);
            sibling.remove();
        });

        it('does not refocus on blur with a valid value', async () => {
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
            await expect(fireEvent.keyDown(input, { key: 'Tab' })).resolves.not.toThrow();
        });
    });

    describe('error message rendering (I17)', () => {
        it('renders the rendered i18n string in a <p id="{id}-err"> when invalid', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: 'M0' } });
            const err = container.querySelector('#test-call-err');
            expect(err).not.toBeNull();
            expect(err?.tagName).toBe('P');
            // 'validators.callsign' resolves through the English
            // catalogue — content assertion stays loose (catalogue
            // owns exact wording) but non-empty.
            expect((err?.textContent ?? '').length).toBeGreaterThan(0);
            expect(err?.textContent ?? '').toContain('Callsign');
        });

        it('wires aria-describedby to the error id when invalid', async () => {
            const { input } = setup();
            await fireEvent.input(input, { target: { value: 'M0' } });
            expect(input.getAttribute('aria-describedby')).toBe('test-call-err');
        });

        it('omits the error <p> and aria-describedby when valid', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: 'M0ABC' } });
            expect(container.querySelector('#test-call-err')).toBeNull();
            expect(input.getAttribute('aria-describedby')).toBeNull();
        });

        it('omits the error <p> for empty input (presence is form-level)', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: '' } });
            expect(container.querySelector('#test-call-err')).toBeNull();
            expect(input.getAttribute('aria-describedby')).toBeNull();
        });

        it('removes the error <p> once the operator corrects the input', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: 'M0' } });
            expect(container.querySelector('#test-call-err')).not.toBeNull();
            await fireEvent.input(input, { target: { value: 'M0ABC' } });
            expect(container.querySelector('#test-call-err')).toBeNull();
            expect(input.getAttribute('aria-describedby')).toBeNull();
        });

        it('also renders the error on blur with an invalid non-empty value', async () => {
            const { container, input } = setup({ value: 'M0' });
            await fireEvent.blur(input);
            expect(container.querySelector('#test-call-err')).not.toBeNull();
            expect(input.getAttribute('aria-describedby')).toBe('test-call-err');
        });

        it('renders the error <p> as screen-reader-only (no visible text)', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: 'M0' } });
            const err = container.querySelector('#test-call-err');
            // The red border is the visual cue; the message is for AT only.
            expect(err?.classList.contains('sr-only')).toBe(true);
            expect(err?.classList.contains('input-error')).toBe(false);
        });

        it('clears the error state when value is programmatically reset to empty (ESC path)', async () => {
            // Reproduces the bug: operator types "M0", presses ESC →
            // qsoDraft.clear() flips bind:value to ''. Pre-fix, errorKey
            // was a $state mutated only by oninput/onblur, so the
            // programmatic clear left the red border + sr-only message
            // stuck. Post-fix (errorKey is $derived(isValidCallsign(value))),
            // the change to value re-runs validation automatically.
            const { container, input, rerender } = renderWithRerender({ value: 'M0' });
            expect(input.getAttribute('aria-invalid')).toBe('true');
            expect(container.querySelector('#test-call-err')).not.toBeNull();
            await rerender({ value: '' });
            expect(input.getAttribute('aria-invalid')).toBe('false');
            expect(input.classList.contains('invalid-input')).toBe(false);
            expect(container.querySelector('#test-call-err')).toBeNull();
            expect(input.getAttribute('aria-describedby')).toBeNull();
        });
    });
});

function renderWithRerender(props: { value: string }) {
    const result = render(Callsign, {
        id: 'test-call',
        label: 'Callsign',
        value: props.value,
    });
    const input = result.container.querySelector('input') as HTMLInputElement;
    return {
        container: result.container,
        input,
        rerender: async (next: { value: string }) => {
            await result.rerender({
                id: 'test-call',
                label: 'Callsign',
                value: next.value,
            });
        },
    };
}
