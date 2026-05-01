<script lang="ts">
    import type { HTMLInputAttributes } from 'svelte/elements';

    interface Props extends Omit<HTMLInputAttributes, 'value' | 'class' | 'oninput' | 'onblur'> {
        id: string;
        label: string;
        value: string;
        validator: (v: string) => boolean;
        widthClass?: string;
        inputClass?: string;
    }

    let {
        id,
        label,
        value = $bindable(''),
        validator,
        widthClass = 'w-full',
        inputClass = '',
        ...rest
    }: Props = $props();

    let invalid = $state(false);
    let inputElement: HTMLInputElement;

    const handleInput = (e: Event): void => {
        const target = e.currentTarget as HTMLInputElement;
        if (!target) return;
        invalid = !validator(target.value);
    };

    const validateAndFocus = (): void => {
        invalid = !validator(value);
        if (invalid && inputElement) {
            inputElement.focus();
            inputElement.select();
        }
    };
</script>

<div class="{widthClass} input-row">
    <label for={id} class="input-label">{label}</label>
    <div class="mt-1">
        <input
            {id}
            bind:this={inputElement}
            bind:value
            class="input-base {inputClass} {invalid ? 'invalid-input' : ''}"
            aria-invalid={invalid}
            oninput={handleInput}
            onblur={validateAndFocus}
            type="text"
            autocomplete="off"
            spellcheck="false"
            {...rest}
        />
    </div>
</div>
