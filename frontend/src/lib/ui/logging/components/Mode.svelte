<script lang="ts">
    import {isCatEnabled} from "$lib/states/cat-state.svelte";
    import {selectBase, selectWrapper, labelBase} from "@station-manager/shared-utils";

    interface Props {
        id: string;
        label: string;
        value: string;
        list: { key: string; value: string }[];
        labelCss?: string;
        divCss?: string;
        inputCss?: string;
        overallWidthCss?: string;
    }
    let {
        id,
        label,
        value = $bindable(),
        list,
        labelCss = labelBase,
        divCss = selectWrapper,
        inputCss = selectBase,
        overallWidthCss = 'w-[150px]'
    }: Props = $props();
</script>

<div class={overallWidthCss}>
    <label for={id} class={labelCss}>{label}</label>
    <div class={divCss}>
        <select
                disabled={isCatEnabled.isEnabled}
                bind:value={value}
                id={id}
                class={inputCss}>
            {#each list as mode (mode.key)}
                <option value={mode.value} selected={value === mode.value}>{mode.value}</option>
            {/each}
        </select>
        <svg class="pointer-events-none col-start-1 row-start-1 mr-2 size-5 self-center justify-self-end text-gray-500 sm:size-4" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" data-slot="icon">
            <path fill-rule="evenodd" d="M4.22 6.22a.75.75 0 0 1 1.06 0L8 8.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 7.28a.75.75 0 0 1 0-1.06Z" clip-rule="evenodd" />
        </svg>
    </div>
</div>
