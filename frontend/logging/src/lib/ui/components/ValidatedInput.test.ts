import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import ValidatedInput from './ValidatedInput.svelte';

/**
 * ValidatedInput is the generic primitive used by Rst (and any future
 * single-field validated input component). The validator stays a pure
 * predicate; presence/required is enforced at the form layer per the
 * project convention. Behaviour to verify:
 *
 *   - Wires the supplied `validator` predicate to invalid styling
 *   - Clears the invalid flag once the value becomes valid
 *   - Treats empty as not-invalid (presence is form-level)
 *   - Focus-trap on blur for invalid non-empty values
 *   - `inputClass` prop appends to the input's class list
 *   - HTML attributes spread through `...rest` (e.g. maxlength)
 */

// Test fixture mirrors the production validator contract: returns null
// when valid (including empty — presence is form-level) and an i18n
// key when malformed. We borrow `'validators.rst'` rather than
// inventing a key so the rendered error text resolves through the
// real catalogue (avoids `[missing: ...]` sentinel noise in test
// output).
const acceptDigits = (v: string): string | null =>
    v.trim() === '' || /^\d+$/.test(v.trim()) ? null : 'validators.rst';

describe('ValidatedInput', () => {
    afterEach(() => cleanup());

    function setup(props: { value?: string; inputClass?: string; maxlength?: number } = {}) {
        const { container } = render(ValidatedInput, {
            id: 'test-vi',
            label: 'Digits',
            value: props.value ?? '',
            validator: acceptDigits,
            ...(props.inputClass !== undefined ? { inputClass: props.inputClass } : {}),
            ...(props.maxlength !== undefined ? { maxlength: props.maxlength } : {}),
        });
        const input = container.querySelector('input') as HTMLInputElement;
        return { container, input };
    }

    describe('rendering', () => {
        it('renders the label', () => {
            const { container } = setup();
            expect(container.textContent ?? '').toContain('Digits');
        });

        it('reflects the initial value', () => {
            const { input } = setup({ value: '123' });
            expect(input.value).toBe('123');
        });
    });

    describe('validator wiring', () => {
        it('flags invalid input via aria-invalid + .invalid-input', async () => {
            const { input } = setup();
            await fireEvent.input(input, { target: { value: 'abc' } });
            expect(input.getAttribute('aria-invalid')).toBe('true');
            expect(input.classList.contains('invalid-input')).toBe(true);
        });

        it('clears invalid styling once input becomes valid', async () => {
            const { input } = setup();
            await fireEvent.input(input, { target: { value: 'abc' } });
            expect(input.getAttribute('aria-invalid')).toBe('true');
            await fireEvent.input(input, { target: { value: '123' } });
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
        it('refocuses + selects when blurring with an invalid non-empty value', async () => {
            const { input } = setup({ value: 'abc' });
            await fireEvent.blur(input);
            expect(document.activeElement).toBe(input);
            expect(input.selectionStart).toBe(0);
            expect(input.selectionEnd).toBe(input.value.length);
        });

        it('does not trap focus when blurring with an empty value', async () => {
            const { input } = setup({ value: '' });
            const sibling = document.createElement('button');
            document.body.appendChild(sibling);
            input.focus();
            sibling.focus();
            await fireEvent.blur(input);
            expect(document.activeElement).not.toBe(input);
            sibling.remove();
        });

        it('does not trap focus when blurring with a valid value', async () => {
            const { input } = setup({ value: '123' });
            const sibling = document.createElement('button');
            document.body.appendChild(sibling);
            input.focus();
            sibling.focus();
            await fireEvent.blur(input);
            expect(document.activeElement).not.toBe(input);
            sibling.remove();
        });
    });

    describe('inputClass prop', () => {
        it('appends inputClass to the input element', () => {
            const { input } = setup({ inputClass: 'extra-thing' });
            expect(input.classList.contains('extra-thing')).toBe(true);
        });

        it('keeps the base class even when inputClass is supplied', () => {
            const { input } = setup({ inputClass: 'extra-thing' });
            expect(input.classList.contains('input-base')).toBe(true);
        });

        it('does not break when inputClass is omitted', () => {
            const { input } = setup();
            expect(input.classList.contains('input-base')).toBe(true);
        });
    });

    describe('HTML attribute pass-through', () => {
        it('forwards maxlength through ...rest', () => {
            const { input } = setup({ maxlength: 3 });
            expect(input.maxLength).toBe(3);
        });
    });

    describe('validator invocation', () => {
        it('calls the validator on input', async () => {
            const validator = vi.fn(
                (v: string): string | null =>
                    v === '' || /^\d+$/.test(v) ? null : 'validators.rst'
            );
            const { container } = render(ValidatedInput, {
                id: 'test-vi-v',
                label: 'X',
                value: '',
                validator,
            });
            const input = container.querySelector('input') as HTMLInputElement;
            await fireEvent.input(input, { target: { value: '12' } });
            expect(validator).toHaveBeenCalledWith('12');
        });

        it('calls the validator on blur (focus-trap path)', async () => {
            const validator = vi.fn(
                (v: string): string | null =>
                    v === '' || /^\d+$/.test(v) ? null : 'validators.rst'
            );
            const { container } = render(ValidatedInput, {
                id: 'test-vi-vb',
                label: 'X',
                value: '12',
                validator,
            });
            const input = container.querySelector('input') as HTMLInputElement;
            validator.mockClear();
            await fireEvent.blur(input);
            expect(validator).toHaveBeenCalledWith('12');
        });
    });

    describe('error message rendering (I17)', () => {
        it('renders the rendered i18n string in a <p id="{id}-err"> when invalid', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: 'abc' } });
            const err = container.querySelector('#test-vi-err');
            expect(err).not.toBeNull();
            expect(err?.tagName).toBe('P');
            // 'validators.rst' resolves to the English catalogue entry
            // — same text the operator sees in production. Tests don't
            // assert the exact wording (that's the catalogue's job)
            // beyond a content-not-empty check.
            expect(err?.textContent ?? '').toContain('RST');
        });

        it('wires aria-describedby to the error id when invalid', async () => {
            const { input } = setup();
            await fireEvent.input(input, { target: { value: 'abc' } });
            expect(input.getAttribute('aria-describedby')).toBe('test-vi-err');
        });

        it('omits the error <p> and aria-describedby when valid', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: '123' } });
            expect(container.querySelector('#test-vi-err')).toBeNull();
            expect(input.getAttribute('aria-describedby')).toBeNull();
        });

        it('omits the error <p> for empty input (presence is form-level)', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: '' } });
            expect(container.querySelector('#test-vi-err')).toBeNull();
            expect(input.getAttribute('aria-describedby')).toBeNull();
        });

        it('removes the error <p> once the operator corrects the input', async () => {
            const { container, input } = setup();
            await fireEvent.input(input, { target: { value: 'abc' } });
            expect(container.querySelector('#test-vi-err')).not.toBeNull();
            await fireEvent.input(input, { target: { value: '12' } });
            expect(container.querySelector('#test-vi-err')).toBeNull();
            expect(input.getAttribute('aria-describedby')).toBeNull();
        });
    });

    describe('transform prop', () => {
        const stripNonDigits = (raw: string): string => raw.replace(/[^0-9]/g, '');

        it('overwrites the input value when transform changes it', async () => {
            const { container } = render(ValidatedInput, {
                id: 'test-vi-t1',
                label: 'X',
                value: '',
                validator: acceptDigits,
                transform: stripNonDigits,
            });
            const input = container.querySelector('input') as HTMLInputElement;
            await fireEvent.input(input, { target: { value: '5A9' } });
            expect(input.value).toBe('59');
        });

        it('runs the validator on the transformed value, not the raw input', async () => {
            const validator = vi.fn(acceptDigits);
            const { container } = render(ValidatedInput, {
                id: 'test-vi-t2',
                label: 'X',
                value: '',
                validator,
                transform: stripNonDigits,
            });
            const input = container.querySelector('input') as HTMLInputElement;
            await fireEvent.input(input, { target: { value: '5A9' } });
            expect(validator).toHaveBeenCalledWith('59');
            expect(input.getAttribute('aria-invalid')).toBe('false');
        });

        it('parks cursor at end of cleaned text after a strip', async () => {
            const { container } = render(ValidatedInput, {
                id: 'test-vi-t3',
                label: 'X',
                value: '',
                validator: acceptDigits,
                transform: stripNonDigits,
            });
            const input = container.querySelector('input') as HTMLInputElement;
            await fireEvent.input(input, { target: { value: '5A9' } });
            expect(input.selectionStart).toBe(2);
            expect(input.selectionEnd).toBe(2);
        });

        it('leaves already-clean input untouched (transform must be idempotent)', async () => {
            const { container } = render(ValidatedInput, {
                id: 'test-vi-t4',
                label: 'X',
                value: '',
                validator: acceptDigits,
                transform: stripNonDigits,
            });
            const input = container.querySelector('input') as HTMLInputElement;
            await fireEvent.input(input, { target: { value: '599' } });
            expect(input.value).toBe('599');
        });

        it('still validates when transform is omitted', async () => {
            const { container } = render(ValidatedInput, {
                id: 'test-vi-t5',
                label: 'X',
                value: '',
                validator: acceptDigits,
            });
            const input = container.querySelector('input') as HTMLInputElement;
            await fireEvent.input(input, { target: { value: '5A9' } });
            expect(input.value).toBe('5A9');
            expect(input.getAttribute('aria-invalid')).toBe('true');
        });
    });
});
