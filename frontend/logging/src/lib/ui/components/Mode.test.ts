import { describe, it, expect, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import Mode from './Mode.svelte';

/**
 * Mode select component (controlled: `value` + `onchange`). Covers:
 *   - renders one <option> per list entry
 *   - onchange fires with the selected value (parent owns the value)
 *   - disabled prop disables the underlying <select>
 *
 * The list shape is `string[]` — both the option's value and its visible
 * label come from the same string.
 */

describe('Mode', () => {
    afterEach(() => cleanup());

    function setup(
        props: {
            value?: string;
            list?: string[];
            disabled?: boolean;
            onchange?: (value: string) => void;
        } = {}
    ) {
        const { container } = render(Mode, {
            id: 'test-mode',
            label: 'Mode',
            value: props.value ?? 'USB',
            list: props.list ?? ['USB', 'LSB', 'CW'],
            disabled: props.disabled ?? false,
            onchange: props.onchange ?? (() => {}),
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

    it('fires onchange with the newly selected value', async () => {
        let picked = '';
        const { select } = setup({ value: 'USB', onchange: (v) => (picked = v) });
        await fireEvent.change(select, { target: { value: 'CW' } });
        await tick();
        expect(picked).toBe('CW');
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

    // Pins the C5 fix: when enabled (CAT off, operator drives mode
    // manually) the select must be keyboard-reachable. When disabled
    // (CAT live, rig drives mode) it skips the tab order so the row
    // doesn't add dead stops between RST and the VFO pair.
    it('is keyboard-reachable (tabindex=0) when enabled', () => {
        const { select } = setup();
        expect(select.tabIndex).toBe(0);
    });

    it('is tab-skipped (tabindex=-1) when disabled', () => {
        const { select } = setup({ disabled: true });
        expect(select.tabIndex).toBe(-1);
    });
});
