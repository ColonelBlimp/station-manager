<script lang="ts">
    const modes = ["AM", "CW", "FM", "RTTY", "SSB", "DIGITALVOICE", "MFSK", "PSK", "HELL", "PACKET"];

    interface Props {
        value: string;
        onChange: (v: string) => void;
        ref?: (el: HTMLElement) => void;
    }

    let { value, onChange, ref }: Props = $props();
    let selectEl: HTMLSelectElement;

    $effect(() => {
        if (selectEl) ref?.(selectEl);
    });
</script>

<select
    bind:this={selectEl}
    {value}
    onchange={(e) => onChange(e.currentTarget.value)}
    class="w-36 rounded border-gray-300 shadow-sm focus:border-indigo-500 focus:ring focus:ring-indigo-500 focus:ring-opacity-50"
>
    {#each modes as mode}
        <option value={mode}>{mode}</option>
    {/each}
</select>
