<script lang="ts">
    import { isValidCallsign } from '../../validators/callsign';
    import type {FocusRefs} from "../../states/focus-context.svelte";

    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
        focusRefKey?: keyof FocusRefs;
        focusRefs?: FocusRefs;
        onenrich?: (callsign: string) => void;
    }

    let {
        id,
        label,
        value = $bindable(''),
        widthClass = 'w-36',
        focusRefKey,
        focusRefs,
        onenrich,
    }: Props = $props();

    let invalid = $state(false);
    let inputElement: HTMLInputElement;

    const handleInput = (e: Event): void => {
        const target = e.currentTarget as HTMLInputElement;
        if (!target) return;
        const v = target.value;
        if (v === '') {
            invalid = false;
            return;
        }
        invalid = !isValidCallsign(v);
    };

    const validateAndFocus = (): void => {
        if (value === '') {
            invalid = false;
            return;
        }
        invalid = !isValidCallsign(value);
        if (invalid && inputElement) {
            inputElement.focus();
            inputElement.select();
        }
    };

    const handleKeydown = (e: KeyboardEvent): void => {
        if (e.key !== 'Tab') return;
        if (value === '' || !isValidCallsign(value)) return;
        onenrich?.(value.trim().toUpperCase());
    };
</script>

<div class="{widthClass} my-4 h-17.5">
    <label for={id} class="input-label">{label}</label>
    <input
        {id}
        bind:this={inputElement}
        bind:value
        class="input-base uppercase {invalid ? 'invalid-input' : ''}"
        aria-invalid={invalid}
        oninput={handleInput}
        onblur={validateAndFocus}
        onkeydown={handleKeydown}
        type="text"
        autocomplete="off"
        spellcheck="false"
    />
</div>
