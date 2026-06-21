<script lang="ts">
    import { isValidCallsign } from '../../validators/callsign';
    import { t } from '../../i18n';

    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
        onenrich?: (callsign: string) => void;
        /*
            Pile-up stack push. When provided, a small stack glyph
            renders at the input's trailing edge — clicking it fires
            this callback. The keyboard equivalent (Shift+Enter) lives
            window-level in QsoPanel and does not depend on this prop;
            the icon is purely the mouse-discoverable affordance.
            Validation + the actual stack push are QsoPanel's job —
            the icon just signals operator intent.
        */
        onstack?: () => void;
    }

    let {
        id,
        label,
        value = $bindable(''),
        widthClass = 'w-38',
        onenrich,
        onstack,
    }: Props = $props();

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

    /*
        Commit-the-callsign keys: Tab, Enter, and Space all fire onenrich
        (QsoPanel's handleEnrich → enrichment lookup + QSO-timer start) when
        the value is a valid, non-empty callsign. Tab keeps its native
        focus-advance — no preventDefault — so the operator's hands flow on
        to RST. Enter has no useful default here (QsoPanel renders no <form>),
        and a callsign never contains a space, so both are consumed to avoid
        a stray space or accidental submit while still firing the lookup.

        Modified variants are deliberately left for QsoPanel's window-level
        handler: Shift+Enter = pile-up stack push, Ctrl/Cmd+Enter = submit,
        Shift+Tab = focus-previous (abandon the entry). Any modifier held →
        return early so the event bubbles untouched.
    */
    const handleKeydown = (e: KeyboardEvent): void => {
        if (e.shiftKey || e.ctrlKey || e.metaKey || e.altKey) return;
        const isSpace = e.key === ' ' || e.code === 'Space';
        // Swallow Space regardless of validity — the field is a single token,
        // so a literal space is never wanted, even mid-edit of an invalid call.
        if (isSpace) e.preventDefault();
        if (e.key !== 'Tab' && e.key !== 'Enter' && !isSpace) return;
        const trimmed = value.trim();
        if (trimmed === '' || isValidCallsign(trimmed) !== null) return;
        // Enter has no meaningful default in this form-less single-token field;
        // consume it (Space is already handled above). Tab advances focus.
        if (e.key === 'Enter') e.preventDefault();
        onenrich?.(trimmed.toUpperCase());
    };
</script>

<div class="{widthClass} input-row">
    <label for={id} class="input-label">{label}</label>
    <div class="mt-1 relative">
        <input
            {id}
            bind:this={inputElement}
            bind:value
            class="input-base uppercase {onstack !== undefined ? 'pr-7' : ''} {errorKey !== null
                ? 'invalid-input'
                : ''}"
            aria-invalid={errorKey !== null}
            aria-describedby={errorKey !== null ? errorId : undefined}
            onkeydown={handleKeydown}
            type="text"
            autocomplete="off"
            spellcheck="false"
        />
        <!--
            Pile-up stack push affordance. Mouse-only — tabindex={-1}
            keeps it out of the operator's tab order (the keyboard
            path is Shift+Enter, handled window-level in QsoPanel).
            Three-bar glyph reads as a stack/list icon; the title
            attribute names the shortcut for discoverability.
        -->
        {#if onstack !== undefined}
            <button
                type="button"
                class="absolute inset-y-0 right-1 flex items-center text-xs text-gray-400 hover:text-gray-700 cursor-pointer leading-none px-1"
                aria-label="Stack callsign"
                title="Stack callsign (Shift+Enter)"
                tabindex={-1}
                onclick={onstack}
            >
                <span aria-hidden="true">≡</span>
            </button>
        {/if}
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
