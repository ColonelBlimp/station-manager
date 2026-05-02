<script lang="ts">
    import { isValidCallsign } from '../../validators/callsign';

    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
        onenrich?: (callsign: string) => void;
    }

    let {
        id,
        label,
        value = $bindable(''),
        widthClass = 'w-36',
        onenrich,
    }: Props = $props();

    let invalid = $state(false);
    let inputElement: HTMLInputElement;

    const handleInput = (e: Event): void => {
        const target = e.currentTarget as HTMLInputElement;
        if (!target) return;
        invalid = !isValidCallsign(target.value);
    };

    const validateAndFocus = (): void => {
        invalid = !isValidCallsign(value);
        if (invalid && inputElement) {
            inputElement.focus();
            inputElement.select();
        }
    };

    const handleKeydown = (e: KeyboardEvent): void => {
        if (e.key !== 'Tab' || e.shiftKey) return;
        const trimmed = value.trim();
        if (trimmed === '' || !isValidCallsign(trimmed)) return;
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
</div>
