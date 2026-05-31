<script lang="ts">
    import { isValidFrequency, parseFrequency } from '../../validators/frequency';

    interface Props {
        id: string;
        value: string;
        band: string;
        disabled?: boolean;
        /**
         * tabindex for the underlying input. Default is 0 (in tab
         * order). The Vfos parent passes -1 for the bottom (non-split)
         * VFO so Tab navigation skips it; the top one is always
         * Tab-reachable.
         */
        tabindex?: number;
        onCommit?: (hz: number) => void;
    }
    let { id, value, band, disabled = false, tabindex = 0, onCommit }: Props = $props();

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
        invalid = isValidFrequency(editValue) !== null;
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

<div class="ml-2 w-31">
    <input
        {id}
        bind:this={inputElement}
        value={editing ? editValue : value}
        {disabled}
        {tabindex}
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
