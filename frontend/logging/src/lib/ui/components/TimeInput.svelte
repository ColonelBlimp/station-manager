<!--
    TimeInput — native browser `type="time"` picker. Browser-native UI
    only; no project SVG overlay. `accent-focus` tints the picker
    chrome to match the project's focus colour where the browser
    supports it.

    UTC labelling is the parent's responsibility (e.g. "Time On (UTC)")
    — TimeInput is unit-naive at this layer. ADIF translation
    (`HH:MM` → `HHMM` / `HHMMSS`) happens at the submit boundary.
-->
<script lang="ts">
    interface Props {
        id: string;
        label: string;
        value: string;
        disabled?: boolean;
        widthClass?: string;
    }

    let {
        id,
        label,
        value = $bindable(''),
        disabled = false,
        widthClass = 'w-26',
    }: Props = $props();
</script>

<div class="{widthClass}">
    <label for={id} class="input-label">{label}</label>
    <div class="mt-1">
        <input
            {id}
            bind:value
            {disabled}
            tabindex={-1}
            type="time"
            class="block w-full rounded-md bg-surface px-2.5 py-1.5 text-base accent-focus outline-1 -outline-offset-1 outline-line focus:outline-2 focus:-outline-offset-2 focus:outline-focus disabled:bg-surface-disabled disabled:cursor-not-allowed"
        />
    </div>
</div>
