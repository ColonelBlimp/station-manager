<script lang="ts">
    interface Props {
        id: string;
        value: string;
        band: string;
        disabled?: boolean;
    }
    let {
        id,
        value = $bindable(''),
        band,
        disabled = false
    }: Props = $props();
</script>

<div class="ml-2">
    <input
    {id}
    bind:value
    disabled={disabled}
    class="input-base"
    type="text"
    autocomplete="off"
    spellcheck="false"
    />
</div>
<div class="font-semibold flex pl-1.5 items-center">{band}</div>
