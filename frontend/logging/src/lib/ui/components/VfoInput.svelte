<script lang="ts">
    import { isValidFrequency, parseFrequency } from '../../validators/frequency';

    interface Props {
        id: string;
        value: string;
        band: string;
        disabled?: boolean;
        onCommit?: (hz: number) => void;
    }
    let {
        id,
        value,
        band,
        disabled = false,
        onCommit,
    }: Props = $props();

    let editValue = $state('');
    let editing = $state(false);
    let invalid = $state(false);
    let inputElement: HTMLInputElement;

    function handleFocus(): void {
        editing = true;
        editValue = value;
        invalid = false;
    }

    function handleInput(e: Event): void {
        const target = e.currentTarget as HTMLInputElement;
        editValue = target.value;
        invalid = !isValidFrequency(editValue);
    }

    function commit(): void {
        if (editValue.trim() === '') {
            // Empty input — revert to displayed value, no commit.
            editing = false;
            invalid = false;
            return;
        }
        const hz = parseFrequency(editValue);
        if (hz === null) {
            // Invalid — revert; the parent's value stays authoritative.
            editing = false;
            invalid = false;
            return;
        }
        onCommit?.(hz);
        editing = false;
        invalid = false;
    }

    function handleBlur(): void {
        editing = false;
        commit();
    }

    function handleKeydown(e: KeyboardEvent): void {
        if (e.key === 'Enter') {
            commit();
            inputElement?.blur();
        } else if (e.key === 'Escape') {
            editing = false;
            invalid = false;
            inputElement?.blur();
        }
    }
</script>

<div class="ml-2">
    <input
        {id}
        bind:this={inputElement}
        value={editing ? editValue : value}
        disabled={disabled}
        class="input-base {invalid ? 'invalid-input' : ''}"
        aria-invalid={invalid}
        type="text"
        autocomplete="off"
        spellcheck="false"
        oninput={handleInput}
        onfocus={handleFocus}
        onblur={handleBlur}
        onkeydown={handleKeydown}
    />
</div>
<div class="font-semibold flex pl-1.5 items-center">{band}</div>
