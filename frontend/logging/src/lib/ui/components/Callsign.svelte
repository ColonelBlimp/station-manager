<script lang="ts">
    import { isValidCallsign } from '../../validators/callsign';
    import { t } from '../../i18n';

    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
        onenrich?: (callsign: string) => void;
    }

    let { id, label, value = $bindable(''), widthClass = 'w-38', onenrich }: Props = $props();

    let inputElement: HTMLInputElement;

    /*
        Validation as a $derived of `value`, not a $state mutated by
        oninput/onblur. Critical for clear-via-ESC: when QsoPanel's
        ESC handler calls qsoDraft.clear(), `value` programmatically
        goes back to '' through bind:value. A $state errorKey driven
        by oninput would never see that programmatic change and stay
        stuck on the last typed-and-invalid value (e.g. "M0" lingers
        with the red border after ESC). The $derived form reruns
        whenever `value` changes from any source.

        `isValidCallsign('')` returns null per its docs (presence is
        a form-layer concern, not a validator concern), so the empty
        post-ESC state correctly clears the error.
    */
    const errorKey = $derived(isValidCallsign(value));
    const errorId = $derived(`${id}-err`);

    const handleKeydown = (e: KeyboardEvent): void => {
        if (e.key !== 'Tab' || e.shiftKey) return;
        const trimmed = value.trim();
        if (trimmed === '' || isValidCallsign(trimmed) !== null) return;
        onenrich?.(trimmed.toUpperCase());
    };
</script>

<div class="{widthClass} input-row">
    <label for={id} class="input-label">{label}</label>
    <div class="mt-1">
        <input
            {id}
            bind:this={inputElement}
            bind:value
            class="input-base uppercase {errorKey !== null ? 'invalid-input' : ''}"
            aria-invalid={errorKey !== null}
            aria-describedby={errorKey !== null ? errorId : undefined}
            onkeydown={handleKeydown}
            type="text"
            autocomplete="off"
            spellcheck="false"
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
