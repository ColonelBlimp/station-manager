<script lang="ts">
    import type { HTMLInputAttributes } from 'svelte/elements';
    import { t } from '../../i18n';

    interface Props extends Omit<HTMLInputAttributes, 'value' | 'class' | 'oninput' | 'onblur'> {
        id: string;
        label: string;
        value: string;
        /*
            Returns null when the value is valid (including empty —
            presence is enforced at the form layer) and an i18n key
            (e.g. `'validators.callsign'`) when malformed. The key is
            rendered via the `t()` helper into operator-facing text in
            the error <p> below the input, and `aria-describedby` is
            wired so screen readers announce the error too.
        */
        validator: (v: string) => string | null;
        /*
            Optional input sanitiser. When set, runs on every keystroke
            BEFORE the validator and overwrites both the bound value
            and the input element's text if the cleaned form differs
            from what the operator typed. Use for "strip the characters
            that can never be valid" — e.g., RST stripping non-digits.

            Distinct from validator: validator decides display state
            (red border / aria-invalid / error message); transform
            decides what actually lands in the field. Transform must
            be idempotent (transform(transform(x)) === transform(x))
            so a paste of already-clean text isn't re-clobbered.
        */
        transform?: (raw: string) => string;
        widthClass?: string;
        inputClass?: string;
    }

    let {
        id,
        label,
        value = $bindable(''),
        validator,
        transform,
        widthClass = 'w-full',
        inputClass = '',
        ...rest
    }: Props = $props();

    let errorKey = $state<string | null>(null);
    let inputElement: HTMLInputElement;

    const errorId = $derived(`${id}-err`);

    const handleInput = (e: Event): void => {
        const target = e.currentTarget as HTMLInputElement;
        if (!target) return;
        let next = target.value;
        if (transform !== undefined) {
            const cleaned = transform(next);
            if (cleaned !== next) {
                /*
                    Operator typed an invalid character. Overwrite the
                    visible input AND the bound prop so reactive
                    consumers see the cleaned value. setSelectionRange
                    parks the cursor at the end so the operator can
                    keep typing without a jump back into the middle of
                    a 2-char RST.
                */
                target.value = cleaned;
                value = cleaned;
                target.setSelectionRange(cleaned.length, cleaned.length);
                next = cleaned;
            }
        }
        errorKey = validator(next);
    };

    const validateAndFocus = (): void => {
        errorKey = validator(value);
        if (errorKey !== null && inputElement) {
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
            class="input-base {inputClass} {errorKey !== null ? 'invalid-input' : ''}"
            aria-invalid={errorKey !== null}
            aria-describedby={errorKey !== null ? errorId : undefined}
            oninput={handleInput}
            onblur={validateAndFocus}
            type="text"
            autocomplete="off"
            spellcheck="false"
            {...rest}
        />
        {#if errorKey !== null}
            <p id={errorId} class="input-error" role="alert">{t(errorKey)}</p>
        {/if}
    </div>
</div>
