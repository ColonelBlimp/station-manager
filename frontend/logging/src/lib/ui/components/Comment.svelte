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
        widthClass = 'w-63.5',
    }: Props = $props();
</script>

<div class="{widthClass}">
    <label for="{id}" class="input-label">Comment</label>
    <div class="mt-1">
        <textarea
            id={id}
            bind:value
            class="textarea-base w-full h-18"
            autocomplete="off"
            spellcheck="false"
        ></textarea>
    </div>
</div>
