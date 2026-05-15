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

    let inputElement: HTMLInputElement;

    /*
        Validation as a $derived of `value`, not a $state mutated by
        oninput/onblur. Critical for clear-via-ESC and any other
        programmatic value reset: when a parent's state resets the
        bound value to '' (e.g. qsoDraft.clear() in QsoPanel), a
        $state errorKey driven by handlers would never see that
        change and the red border + sr-only error <p> would stay
        stuck on the last invalid value. The $derived form reruns
        whenever `value` changes from any source.

        Validators return null on empty (presence is a form-layer
        concern) so the post-clear state is naturally clean.
    */
    const errorKey = $derived(validator(value));
    const errorId = $derived(`${id}-err`);

    const handleInput = (e: Event): void => {
        /*
            With validation now $derived, the only job left for
            oninput is applying the optional `transform` mutation:
            strip the characters that can never be valid before they
            propagate into `value` via bind:value. When no transform
            is supplied, bind:value flows on its own — handleInput is
            a no-op and could be omitted, but keeping the unconditional
            wiring lets us add a transform later without re-attaching
            the handler.
        */
        if (transform === undefined) return;
        const target = e.currentTarget as HTMLInputElement;
        if (!target) return;
        const cleaned = transform(target.value);
        if (cleaned !== target.value) {
            /*
                Operator typed an invalid character. Overwrite the
                visible input AND the bound prop so reactive
                consumers see the cleaned value. setSelectionRange
                parks the cursor at the end so the operator can keep
                typing without a jump back into the middle of a
                2-char RST.
            */
            target.value = cleaned;
            value = cleaned;
            target.setSelectionRange(cleaned.length, cleaned.length);
        }
    };

    const validateAndFocus = (): void => {
        // errorKey is up-to-date via $derived — no need to recompute.
        // Blur only needs to act on the existing verdict: trap focus
        // when invalid so the operator must fix the field before
        // moving on.
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
        <!--
            Error message kept in the DOM (when errorKey is non-null)
            so screen readers announce it via aria-describedby, but
            visually hidden — the red border on the input is the only
            visible cue. role="alert" keeps it in the polite-but-
            urgent ARIA live region.
        -->
        {#if errorKey !== null}
            <p id={errorId} class="sr-only" role="alert">{t(errorKey)}</p>
        {/if}
    </div>
</div>
