<script lang="ts">
    import type {Snippet} from "svelte";

    interface Props {
        id: string;
        label: string;
        value: string;
        cannotUpdate?: boolean;
        cannotUpdateReason?: string;
        updateCallback?: (value: string) => void;
        inputSnippet?: Snippet<[{value: string, onChange: (v: string) => void, ref: (el: HTMLElement) => void}]>;
    }

    let {
        id,
        label,
        cannotUpdate = false,
        cannotUpdateReason = '',
        value = $bindable(),
        updateCallback,
        inputSnippet,
    }: Props = $props();

    let editState = $state(false);
    let editValue = $state(value);
    let inputEl: HTMLElement | undefined;

    const onClickEdit = () => {
        editValue = value;
        editState = true;
        setTimeout(() => inputEl?.focus(), 0);
    };

    const onSave = () => {
        value = editValue;
        updateCallback?.(editValue);
        editState = false;
    };

    const onCancel = () => {
        editState = false;
    };
</script>

<div class="flex flex-row items-center py-2">
    <dt class="text-sm font-semibold w-56">{label}</dt>
    <dd class="flex items-center gap-2 text-sm text-gray-700">
        {#if editState && inputSnippet}
            {@render inputSnippet({ value: editValue, onChange: (v) => (editValue = v), ref: (el) => (inputEl = el) })}
            <button
                onclick={onSave}
                type="button"
                class="cursor-pointer bg-white font-semibold text-indigo-600 hover:text-indigo-500"
            >Save</button>
            <button
                onclick={onCancel}
                type="button"
                class="cursor-pointer bg-white font-semibold text-gray-500 hover:text-gray-400"
            >Cancel</button>
        {:else}
            <span class="w-32 text-red-700 font-semibold">{value}</span>
            <span class="ml-4 shrink-0">
                {#if cannotUpdate}
                    <p>{cannotUpdateReason}</p>
                {:else}
                    <button
                        onclick={onClickEdit}
                        id="{id}-update"
                        type="button"
                        class="cursor-pointer bg-white font-semibold text-indigo-600 hover:text-indigo-500"
                    >Edit</button>
                {/if}
            </span>
        {/if}
    </dd>
</div>
