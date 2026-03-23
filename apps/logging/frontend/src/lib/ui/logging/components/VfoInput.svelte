<script lang="ts">
    import {frequencyToBandFromDottedMHz} from "@station-manager/shared-utils";
    import {inputBase} from "@station-manager/shared-utils";

    interface Props {
        id: string;
        value: string;
        band: string;
        disabled?: boolean;
        inputCss?: string;
    }
    let {
        id,
        value = $bindable(),
        band,
        disabled = false,
        inputCss = inputBase,
    }: Props = $props();

</script>

<div class="w-29">
    <input
            type="text"
            autocomplete="off"
            spellcheck="false"
            id={id}
            disabled={disabled}
            title="Format: ?#.###.###"
            bind:value={value}
            class={inputCss}
    />
</div>
<div class="cursor-default w-8 font-semibold text-base">{frequencyToBandFromDottedMHz(band)}</div>