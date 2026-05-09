import { describe, it, expect, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import Mode from './Mode.svelte';

/**
 * Mode select component. Covers:
 *   - renders one <option> per list entry
 *   - bind:value reflects user selection back to the parent
 *   - disabled prop disables the underlying <select>
 *
 * The list shape is `string[]` — both the option's value and its visible
 * label come from the same string.
 */

describe('Mode', () => {
    afterEach(() => cleanup());

    function setup(props: { value?: string; list?: string[]; disabled?: boolean } = {}) {
        const { container } = render(Mode, {
            id: 'test-mode',
            label: 'Mode',
            value: props.value ?? 'USB',
            list: props.list ?? ['USB', 'LSB', 'CW'],
            disabled: props.disabled ?? false,
        });
        const select = container.querySelector('select') as HTMLSelectElement;
        return { container, select };
    }

    it('renders one <option> per list entry', () => {
        const { container } = setup({ list: ['USB', 'LSB', 'CW', 'FM'] });
        const options = container.querySelectorAll('option');
        expect(options).toHaveLength(4);
        expect(options[0].textContent).toBe('USB');
        expect(options[3].textContent).toBe('FM');
    });

    it('option value equals option label (single-string list shape)', () => {
        const { container } = setup({ list: ['USB', 'CW'] });
        const options = container.querySelectorAll('option');
        expect(options[0].value).toBe('USB');
        expect(options[1].value).toBe('CW');
    });

    it('reflects the initial value as the selected option', () => {
        const { select } = setup({ value: 'CW' });
        expect(select.value).toBe('CW');
    });

    it('change event updates the bound select value', async () => {
        const { select } = setup({ value: 'USB' });
        await fireEvent.change(select, { target: { value: 'CW' } });
        await tick();
        expect(select.value).toBe('CW');
    });

    it('renders enabled by default', () => {
        const { select } = setup();
        expect(select.disabled).toBe(false);
    });

    it('renders disabled when disabled prop is true', () => {
        const { select } = setup({ disabled: true });
        expect(select.disabled).toBe(true);
    });

    it('renders the label', () => {
        const { container } = setup();
        expect(container.textContent ?? '').toContain('Mode');
    });
});
