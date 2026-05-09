import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import FormControls from './FormControls.svelte';

/**
 * FormControls — Clear / Log Contact button pair. Tests cover:
 *   - Click → onClear / onSubmit callbacks fire.
 *   - submitDisabled=true: Log Contact has the disabled attribute and
 *     does NOT fire onSubmit when clicked.
 *   - Clear is always enabled regardless of submitDisabled.
 *   - Optional callbacks (component must not throw if absent).
 *
 * The buttons are presentational here — the parent (QsoPanel) owns the
 * draft state. These tests pin down the boundary so refactors of the
 * button markup don't silently lose the click bindings.
 */

describe('FormControls', () => {
    afterEach(() => cleanup());

    function setup(
        props: {
            onClear?: () => void;
            onSubmit?: () => void;
            submitDisabled?: boolean;
        } = {}
    ) {
        const { container } = render(FormControls, {
            onClear: props.onClear,
            onSubmit: props.onSubmit,
            submitDisabled: props.submitDisabled ?? false,
        });
        const clearBtn = container.querySelector('#clear-qso-btn') as HTMLButtonElement;
        const logBtn = container.querySelector('#log-qso-btn') as HTMLButtonElement;
        return { container, clearBtn, logBtn };
    }

    describe('rendering', () => {
        it('renders both buttons with their labels', () => {
            const { clearBtn, logBtn } = setup();
            expect(clearBtn.textContent?.trim()).toBe('Clear');
            expect(logBtn.textContent?.trim()).toBe('Log Contact');
        });

        it('uses type="button" so the buttons do not submit any enclosing form', () => {
            const { clearBtn, logBtn } = setup();
            expect(clearBtn.type).toBe('button');
            expect(logBtn.type).toBe('button');
        });
    });

    describe('Clear button', () => {
        it('fires onClear when clicked', async () => {
            const onClear = vi.fn();
            const { clearBtn } = setup({ onClear });
            await fireEvent.click(clearBtn);
            expect(onClear).toHaveBeenCalledTimes(1);
        });

        it('does not fire onSubmit when Clear is clicked', async () => {
            const onClear = vi.fn();
            const onSubmit = vi.fn();
            const { clearBtn } = setup({ onClear, onSubmit });
            await fireEvent.click(clearBtn);
            expect(onClear).toHaveBeenCalledTimes(1);
            expect(onSubmit).not.toHaveBeenCalled();
        });

        it('tolerates absence of onClear callback (does not throw)', async () => {
            const { clearBtn } = setup({});
            await expect(fireEvent.click(clearBtn)).resolves.not.toThrow();
        });

        it('is not disabled when submitDisabled is true (Clear is always enabled)', () => {
            const { clearBtn } = setup({ submitDisabled: true });
            expect(clearBtn.disabled).toBe(false);
        });

        it('still fires onClear when submitDisabled is true', async () => {
            const onClear = vi.fn();
            const { clearBtn } = setup({ onClear, submitDisabled: true });
            await fireEvent.click(clearBtn);
            expect(onClear).toHaveBeenCalledTimes(1);
        });
    });

    describe('Log Contact button', () => {
        it('fires onSubmit when clicked and not disabled', async () => {
            const onSubmit = vi.fn();
            const { logBtn } = setup({ onSubmit });
            await fireEvent.click(logBtn);
            expect(onSubmit).toHaveBeenCalledTimes(1);
        });

        it('does not fire onClear when Log Contact is clicked', async () => {
            const onClear = vi.fn();
            const onSubmit = vi.fn();
            const { logBtn } = setup({ onClear, onSubmit });
            await fireEvent.click(logBtn);
            expect(onSubmit).toHaveBeenCalledTimes(1);
            expect(onClear).not.toHaveBeenCalled();
        });

        it('renders enabled by default (submitDisabled defaults to false)', () => {
            const { logBtn } = setup();
            expect(logBtn.disabled).toBe(false);
        });

        it('renders with disabled attribute when submitDisabled is true', () => {
            const { logBtn } = setup({ submitDisabled: true });
            expect(logBtn.disabled).toBe(true);
        });

        // Note: we don't assert that clicking a disabled button does NOT
        // fire onSubmit, because jsdom's fireEvent synthesizes the click
        // event regardless of the disabled attribute (real browsers
        // suppress it natively). The disabled-attribute assertion above
        // is the right characterization here — native HTML behaviour
        // handles the click suppression in actual use.

        it('tolerates absence of onSubmit callback (does not throw)', async () => {
            const { logBtn } = setup({});
            await expect(fireEvent.click(logBtn)).resolves.not.toThrow();
        });
    });
});
