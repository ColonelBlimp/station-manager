<script lang="ts">
    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
    }

    let {
        id,
        label,
        value = $bindable(''),
        widthClass = 'w-52',
    }: Props = $props();
</script>

<div class="{widthClass} input-row">
    <label for={id} class="input-label">{label}</label>
    <div class="mt-1">
        <input
            {id}
            bind:value
            class="input-base"
            type="text"
            autocomplete="off"
            spellcheck="false"
        />
    </div>
</div>
