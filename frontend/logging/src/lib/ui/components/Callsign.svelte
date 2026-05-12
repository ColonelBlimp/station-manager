<script lang="ts">
    import { isValidCallsign } from '../../validators/callsign';

    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
        onenrich?: (callsign: string) => void;
    }

    let { id, label, value = $bindable(''), widthClass = 'w-38', onenrich }: Props = $props();

    let invalid = $state(false);
    let inputElement: HTMLInputElement;

    const handleInput = (e: Event): void => {
        const target = e.currentTarget as HTMLInputElement;
        if (!target) return;
        invalid = !isValidCallsign(target.value);
    };

    // Mark invalid on blur but do NOT fight the operator's focus
    // choice. Forced refocus traps keyboard users who deliberately
    // Shift+Tab back out of the form (e.g. realising mid-typo they
    // want to abandon the QSO entry), and the red-border + aria-invalid
    // signals are sufficient to communicate the problem. The submit
    // button is the load-bearing guard against logging an invalid
    // callsign.
    const validateOnBlur = (): void => {
        invalid = !isValidCallsign(value);
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
            onblur={validateOnBlur}
            onkeydown={handleKeydown}
            type="text"
            autocomplete="off"
            spellcheck="false"
        />
    </div>
</div>
