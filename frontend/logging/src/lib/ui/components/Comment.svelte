<!--
    Comment — multi-line free-text textarea for QSO notes. Distinct from
    `TextInput` because notes are inherently multi-line; uses `.textarea-base`
    rather than `.input-base` to get the right padding/resize behaviour.

    No `.input-row` wrapper — Comment is taller than `h-input-slot` and
    opts out of the validation-slot reservation. If a future requirement
    needs an inline character-count or length warning, this should grow
    its own slot rather than reuse the input-row's.

    Paste list: a small clipboard-list icon button sits on the label row.
    It opens a popover of recently-logged comments; picking one calls
    `onpick` (the parent decides what to do — QsoPanel replaces the field
    value). The trigger is always visible for a stable layout but is
    disabled while `history` is empty (e.g. before the operator has logged
    their first comment), so it never opens an empty popover.
-->
<script lang="ts">
    import TuneButton from './TuneButton.svelte';

    interface Props {
        id: string;
        label: string;
        value: string;
        disabled?: boolean;
        widthClass?: string;
        /**
         * Recently-logged comments for the paste-list dropdown, newest
         * first. Empty / omitted → trigger rendered but disabled.
         */
        history?: string[];
        /**
         * Called with the chosen comment when the operator picks one
         * from the paste list. The parent owns the write (replace vs
         * append); this component only surfaces the selection.
         */
        onpick?: (text: string) => void;
    }

    let {
        id,
        label,
        value = $bindable(''),
        disabled = false,
        widthClass = 'w-63.5',
        history = [],
        onpick,
    }: Props = $props();

    let open = $state(false);
    let rootEl: HTMLDivElement | undefined = $state();
    let listEl: HTMLDivElement | undefined = $state();

    const triggerId = $derived(`${id}-history-trigger`);
    const listId = $derived(`${id}-history-list`);

    function pick(text: string): void {
        onpick?.(text);
        open = false;
        // Return focus to the textarea so the operator can keep typing /
        // editing the pasted comment without reaching for the mouse.
        document.getElementById(id)?.focus();
    }

    // Focus the popover when it opens so Escape (handled on the list,
    // not window) closes it — and so its stopPropagation can stop
    // QsoPanel's window-level Escape from clearing the whole form.
    $effect(() => {
        if (open) listEl?.focus();
    });

    function onListKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') {
            // Don't let the form-level Escape (clear QSO) also fire.
            e.stopPropagation();
            open = false;
            document.getElementById(triggerId)?.focus();
        }
    }

    // Outside-click closes the popover. Registered on window but cheap:
    // the containment check short-circuits when closed. The trigger's
    // own click toggles `open` first and is inside `rootEl`, so this
    // handler won't immediately re-close it.
    function onWindowClick(e: MouseEvent): void {
        if (!open) return;
        if (rootEl && !rootEl.contains(e.target as Node)) {
            open = false;
        }
    }
</script>

<svelte:window onclick={onWindowClick} />

<div class={widthClass} bind:this={rootEl}>
    <div class="relative flex flex-row items-center">
        <label for={id} class="input-label">{label}</label>
        <button
            id={triggerId}
            type="button"
            disabled={disabled || history.length === 0}
            onclick={() => (open = !open)}
            class="ml-1 mr-4 text-gray-500 hover:text-gray-700 cursor-pointer leading-none px-1 disabled:opacity-50 disabled:cursor-not-allowed"
            aria-haspopup="menu"
            aria-expanded={open}
            aria-controls={listId}
            title="Insert a recent comment"
        >
            <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                stroke-width="1.5"
                stroke="currentColor"
                class="size-5"
                aria-hidden="true"
            >
                <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 0 0 2.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 0 0-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 0 0 .75-.75 2.25 2.25 0 0 0-.1-.664m-5.8 0A2.251 2.251 0 0 1 13.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25ZM6.75 12h.008v.008H6.75V12Zm0 3h.008v.008H6.75V15Zm0 3h.008v.008H6.75V18Z"
                />
            </svg>
        </button>
        <TuneButton />
    </div>
    <div class="relative mt-1">
        <textarea
            {id}
            bind:value
            {disabled}
            class="textarea-base w-full h-18 disabled:bg-surface-disabled disabled:cursor-not-allowed"
            autocomplete="off"
            spellcheck="false"
        ></textarea>

        {#if open && history.length > 0}
            <!--
                Popover anchored under the field. tabindex=-1 so the
                $effect can focus it (catching Escape); keydown lives
                here, not on window, so its stopPropagation can shield
                the form-level Escape. Items are buttons → keyboard +
                pointer both work. Long / multi-line comments truncate
                to one line with the full text in the title.
            -->
            <div
                id={listId}
                bind:this={listEl}
                role="menu"
                tabindex="-1"
                onkeydown={onListKeydown}
                class="absolute z-10 top-full left-0 w-full max-h-44 overflow-y-auto rounded-md border border-line-soft bg-white shadow-md focus:outline-none"
            >
                {#each history as comment (comment)}
                    <button
                        type="button"
                        role="menuitem"
                        onclick={() => pick(comment)}
                        title={comment}
                        class="block w-full text-left px-3 py-1.5 text-sm truncate hover:bg-gray-100 cursor-pointer"
                    >
                        {comment}
                    </button>
                {/each}
            </div>
        {/if}
    </div>
</div>
