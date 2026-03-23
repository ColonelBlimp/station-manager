<script lang="ts">
    import {qsoState} from "$lib/states/new-qso-state.svelte.js";
    import {inputDateTimePicker, inputTimerDisabled, labelBase} from "@station-manager/shared-utils";

    interface Props {
        id: string,
        label: string,
        value: string,
        disabled: boolean,
    }

    let {
        id,
        label,
        value = $bindable(),
        disabled,
    }: Props = $props();

    let ticking: boolean = $derived.by(() => {
        return disabled && qsoState.time_off !== qsoState.time_on;
    });

</script>

<div class="w-27.5">
    <label for={id} class={labelBase}>{label}</label>
    <div class="relative mt-2">
        <input
                bind:value={value}
                type="time"
                id={id}
                disabled={disabled}
                class="{ticking ? inputTimerDisabled : ''} {inputDateTimePicker}"
            />
        <span class="absolute top-2 right-2">
            <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5 text-gray-700">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"/>
            </svg>
        </span>
    </div>
</div>
