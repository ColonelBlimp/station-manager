<script lang="ts">
    import {inputBase, inputWrapper, labelBase} from "@station-manager/shared-utils";

    interface Props {
        id: string;
        label: string;
        value: string;
        labelCss?: string;
        divCss?: string;
        inputCss?: string;
        overallWidthCss?: string;
    }
    let {
        id,
        label,
        value = $bindable(),
        labelCss = labelBase,
        divCss = inputWrapper,
        inputCss = inputBase,
        overallWidthCss = 'w-[210px]'
    }: Props = $props();
</script>

<div class={overallWidthCss}>
    <label for={id} class={labelCss}>{label}</label>
    <div class={divCss}>
        <input
                bind:value={value}
                type="text"
                id={id}
                class={inputCss}
                autocomplete="off"
                spellcheck="false"
        />
    </div>
</div>