<!--
    Comment — multi-line free-text textarea for QSO notes. Distinct from
    `TextInput` because notes are inherently multi-line; uses `.textarea-base`
    rather than `.input-base` to get the right padding/resize behaviour.

    No `.input-row` wrapper — Comment is taller than `h-input-slot` and
    opts out of the validation-slot reservation. If a future requirement
    needs an inline character-count or length warning, this should grow
    its own slot rather than reuse the input-row's.
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
        widthClass = 'w-63.5',
    }: Props = $props();
</script>

<div class="{widthClass}">
    <label for={id} class="input-label">{label}</label>
    <div class="mt-1">
        <textarea
            {id}
            bind:value
            {disabled}
            class="textarea-base w-full h-18 disabled:bg-surface-disabled disabled:cursor-not-allowed"
            autocomplete="off"
            spellcheck="false"
        ></textarea>
    </div>
</div>
