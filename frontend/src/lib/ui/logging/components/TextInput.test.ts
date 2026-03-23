import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import TextInput from './TextInput.svelte';

describe('TextInput', () => {
    describe('rendering', () => {
        it('should render with label and input', () => {
            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: '',
                },
            });

            expect(screen.getByLabelText('Test Label')).toBeInTheDocument();
            expect(screen.getByRole('textbox')).toBeInTheDocument();
        });

        it('should display the initial value', () => {
            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: 'initial value',
                },
            });

            expect(screen.getByRole('textbox')).toHaveValue('initial value');
        });

        it('should apply custom CSS classes', () => {
            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: '',
                    labelCss: 'custom-label-class',
                    inputCss: 'custom-input-class',
                    overallWidthCss: 'custom-width-class',
                },
            });

            const label = screen.getByText('Test Label');
            const input = screen.getByRole('textbox');

            expect(label).toHaveClass('custom-label-class');
            expect(input).toHaveClass('custom-input-class');
        });

        it('should set the correct id on the input', () => {
            render(TextInput, {
                props: {
                    id: 'my-unique-id',
                    label: 'Test Label',
                    value: '',
                },
            });

            const input = screen.getByRole('textbox');
            expect(input).toHaveAttribute('id', 'my-unique-id');
        });

        it('should have autocomplete off', () => {
            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: '',
                },
            });

            const input = screen.getByRole('textbox');
            expect(input).toHaveAttribute('autocomplete', 'off');
        });

        it('should have spellcheck disabled', () => {
            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: '',
                },
            });

            const input = screen.getByRole('textbox');
            expect(input).toHaveAttribute('spellcheck', 'false');
        });
    });

    describe('user interaction', () => {
        it('should update value when user types', async () => {
            const user = userEvent.setup();

            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: '',
                },
            });

            const input = screen.getByRole('textbox');
            await user.type(input, 'hello world');

            expect(input).toHaveValue('hello world');
        });

        it('should allow clearing the input', async () => {
            const user = userEvent.setup();

            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: 'initial',
                },
            });

            const input = screen.getByRole('textbox');
            await user.clear(input);

            expect(input).toHaveValue('');
        });

        it('should append to existing value when typing', async () => {
            const user = userEvent.setup();

            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: 'hello',
                },
            });

            const input = screen.getByRole('textbox');
            await user.type(input, ' world');

            expect(input).toHaveValue('hello world');
        });
    });

    describe('accessibility', () => {
        it('should associate label with input via for/id', () => {
            render(TextInput, {
                props: {
                    id: 'accessible-input',
                    label: 'Accessible Label',
                    value: '',
                },
            });

            const label = screen.getByText('Accessible Label');
            expect(label).toHaveAttribute('for', 'accessible-input');
        });

        it('should be focusable', async () => {
            const user = userEvent.setup();

            render(TextInput, {
                props: {
                    id: 'test-input',
                    label: 'Test Label',
                    value: '',
                },
            });

            const input = screen.getByRole('textbox');
            await user.click(input);

            expect(input).toHaveFocus();
        });
    });
});
