<script lang="ts">
    import {clickoutside} from "@svelte-put/clickoutside";
    import {clipboardState} from "$lib/states/clipboard-state.svelte";
    import {textareaBase, inputWrapper, labelWithMargin} from "@station-manager/shared-utils";

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
        labelCss = labelWithMargin,
        divCss = inputWrapper,
        inputCss = `h-[68px] ${textareaBase}`,
        overallWidthCss = 'w-[280px]'
    }: Props = $props();

    let clipboardVisible = $state(false);
    let enabled: boolean = $state(true);
    let clipboard = $derived(clipboardState.list);

    const clipboardAction = (): void => {
        const textToAdd = (value || '').trim();
        if (textToAdd.length > 0) {
            clipboardState.add(textToAdd);
        }

        if (clipboardState.list.length > 0) {
            clipboardVisible = !clipboardVisible;
        }
    }

    const onClickOutside = (): void => {
        clipboardVisible = false;
    }

    const insertSelectedText = (text: string): void => {
        value = text;
        clipboardVisible = false;
    }

</script>

<div class="relative {overallWidthCss}" use:clickoutside={{enabled}} onclickoutside={onClickOutside}>
    <div class="flex items-center">
        <button
                type="button"
                onclick={clipboardAction}
                class="cursor-pointer"
                aria-label="clip-board"
                title="Paste from Clipboard">
            <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 0 0 2.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 0 0-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 0 0 .75-.75 2.25 2.25 0 0 0-.1-.664m-5.8 0A2.251 2.251 0 0 1 13.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25ZM6.75 12h.008v.008H6.75V12Zm0 3h.008v.008H6.75V15Zm0 3h.008v.008H6.75V18Z" />
            </svg>
        </button>
        <label for={id} class={labelCss}>{label}</label>
    </div>
    {#if clipboardVisible && clipboard.length > 0}
        <div class="absolute top-8 z-50 w-44 text-xs p-2 border border-gray-300 rounded-md bg-white shadow-lg">
            {#each clipboard as item (item)}
                <button
                        onclick={() => insertSelectedText(item)}
                        type="button"
                        class="cursor-pointer w-full text-left p-1 text-xs rounded-xs text-gray-700 hover:bg-gray-300 text-nowrap overflow-hidden text-ellipsis">
                    {item}
                </button>
            {/each}
        </div>
    {/if}
    <div class={divCss}>
        <textarea
                bind:value={value}
                id={id}
                spellcheck="false"
                class={inputCss}
                maxlength="128"
                autocomplete="off"></textarea>
    </div>
</div>
