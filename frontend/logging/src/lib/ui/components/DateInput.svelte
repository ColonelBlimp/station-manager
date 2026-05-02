<!--
    DateInput — native browser `type="date"` picker. Browser-native icon
    only; no project SVG overlay. `accent-indigo-600` tints the picker
    chrome to match the project's focus colour where the browser
    supports it (Firefox 92+, Chrome 88+, Safari 15.4+).

    ADIF translation (`YYYY-MM-DD` → `YYYYMMDD`) is the parent / submit
    boundary's responsibility; this component is unit-naive.
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
        widthClass = 'w-40',
    }: Props = $props();
</script>

<div class="{widthClass}">
    <label for={id} class="input-label">{label}</label>
    <div class="mt-1">
        <input
            {id}
            bind:value
            {disabled}
            type="date"
            class="block w-full rounded-md bg-white px-2.5 py-1.5 text-base accent-indigo-600 outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 disabled:bg-gray-100 disabled:cursor-not-allowed"
        />
    </div>
</div>
