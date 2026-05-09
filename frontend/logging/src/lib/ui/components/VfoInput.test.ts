import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import VfoInput from './VfoInput.svelte';

/**
 * VfoInput edit/commit behaviour. Covers:
 *   - commit on blur and Enter (only when valid)
 *   - revert on Escape (no commit)
 *   - invalid styling via aria-invalid + .invalid-input class
 *   - prop value vs in-progress edit display
 *
 * Uses @testing-library/svelte's fireEvent. The component is a controlled
 * input — parent passes `value` (display string), VfoInput holds the local
 * edit buffer and emits onCommit(hz) when the operator commits a valid input.
 */

describe('VfoInput', () => {
    afterEach(() => cleanup());

    function setup(
        initialProps: { value?: string; band?: string; onCommit?: (hz: number) => void } = {}
    ) {
        const onCommit = initialProps.onCommit ?? vi.fn();
        const { container } = render(VfoInput, {
            id: 'test-vfo',
            value: initialProps.value ?? '14.250.000',
            band: initialProps.band ?? '20m',
            onCommit,
        });
        const input = container.querySelector('input') as HTMLInputElement;
        return { container, input, onCommit };
    }

    describe('display', () => {
        it('shows the prop value when not editing', () => {
            const { input } = setup({ value: '14.250.000' });
            expect(input.value).toBe('14.250.000');
        });

        it('renders the band text', () => {
            const { container } = setup({ band: '20m' });
            expect(container.textContent ?? '').toContain('20m');
        });
    });

    describe('commit on blur', () => {
        it('commits a valid display-format value', async () => {
            const onCommit = vi.fn();
            const { input } = setup({ onCommit });
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '7.100.000' } });
            await fireEvent.blur(input);
            expect(onCommit).toHaveBeenCalledTimes(1);
            expect(onCommit).toHaveBeenCalledWith(7_100_000);
        });

        it('commits a valid decimal-MHz value', async () => {
            const onCommit = vi.fn();
            const { input } = setup({ onCommit });
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '14.250' } });
            await fireEvent.blur(input);
            expect(onCommit).toHaveBeenCalledWith(14_250_000);
        });

        it('does not commit an invalid value', async () => {
            const onCommit = vi.fn();
            const { input } = setup({ onCommit });
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '14.' } });
            await fireEvent.blur(input);
            expect(onCommit).not.toHaveBeenCalled();
        });

        it('does not commit an empty value', async () => {
            const onCommit = vi.fn();
            const { input } = setup({ onCommit });
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '' } });
            await fireEvent.blur(input);
            expect(onCommit).not.toHaveBeenCalled();
        });

        it('does not commit an out-of-range value', async () => {
            const onCommit = vi.fn();
            const { input } = setup({ onCommit });
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '99999' } });
            await fireEvent.blur(input);
            expect(onCommit).not.toHaveBeenCalled();
        });
    });

    describe('commit on Enter', () => {
        it('commits a valid value when Enter is pressed', async () => {
            const onCommit = vi.fn();
            const { input } = setup({ onCommit });
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '14.250' } });
            await fireEvent.keyDown(input, { key: 'Enter' });
            expect(onCommit).toHaveBeenCalledWith(14_250_000);
        });

        it('does not commit an invalid value on Enter', async () => {
            const onCommit = vi.fn();
            const { input } = setup({ onCommit });
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '14.' } });
            await fireEvent.keyDown(input, { key: 'Enter' });
            expect(onCommit).not.toHaveBeenCalled();
        });
    });

    describe('Escape revert', () => {
        it('does not commit when Escape is pressed', async () => {
            const onCommit = vi.fn();
            const { input } = setup({ onCommit });
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '14.250' } });
            await fireEvent.keyDown(input, { key: 'Escape' });
            expect(onCommit).not.toHaveBeenCalled();
        });
    });

    describe('invalid styling', () => {
        it('sets aria-invalid="true" while typing an invalid value', async () => {
            const { input } = setup();
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '14.' } });
            expect(input.getAttribute('aria-invalid')).toBe('true');
            expect(input.classList.contains('invalid-input')).toBe(true);
        });

        it('clears aria-invalid when typing becomes valid', async () => {
            const { input } = setup();
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '14.' } });
            expect(input.getAttribute('aria-invalid')).toBe('true');
            await fireEvent.input(input, { target: { value: '14.250' } });
            expect(input.getAttribute('aria-invalid')).toBe('false');
            expect(input.classList.contains('invalid-input')).toBe(false);
        });

        it('does not flag empty input as invalid (presence is form-level)', async () => {
            const { input } = setup();
            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '' } });
            expect(input.getAttribute('aria-invalid')).toBe('false');
        });
    });
});
