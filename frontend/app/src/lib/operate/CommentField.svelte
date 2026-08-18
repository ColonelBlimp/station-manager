<script lang="ts">
    // Comment field with a recent-comments paste-list picker. Ported from the
    // retired logging SPA's Comment.svelte by operator direction 2026-08-18 (W-0003),
    // adapted to the app: a single-line <input> (not a textarea) and theme-aware
    // tokens. A clipboard-list glyph on the label row opens a popover of recently
    // logged comments (commentHistory); picking one replaces the field value. The
    // trigger is always rendered for a stable layout but disabled while history is
    // empty, so it never opens an empty popover.
    interface Props {
        id: string;
        label: string;
        value: string;
        /** Recently-logged comments, newest first. Empty ⇒ trigger disabled. */
        items: string[];
        class?: string;
    }

    let { id, label, value = $bindable(''), items, class: className = '' }: Props = $props();

    let open = $state(false);
    let rootEl: HTMLDivElement | undefined = $state();
    let listEl: HTMLDivElement | undefined = $state();

    const triggerId = $derived(`${id}-history-trigger`);
    const listId = $derived(`${id}-history-list`);

    function pick(text: string): void {
        value = text; // replace, matching the retired SPA's QsoPanel behaviour
        open = false;
        // Return focus to the input so the operator can keep editing the pasted
        // comment without reaching for the mouse.
        document.getElementById(id)?.focus();
    }

    // Focus the popover when it opens so Escape (handled on the list, not window)
    // closes it — and so its stopPropagation shields LoggingCard's window-level
    // Escape, which would otherwise clear the whole QSO draft.
    $effect(() => {
        if (open) listEl?.focus();
    });

    function onListKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') {
            e.stopPropagation(); // do NOT also fire the form-level Escape (clear QSO)
            open = false;
            document.getElementById(triggerId)?.focus();
        }
    }

    // Outside-click closes the popover. Cheap: the containment check short-circuits
    // when closed, and the trigger's own click (inside rootEl) toggles first.
    function onWindowClick(e: MouseEvent): void {
        if (!open) return;
        if (rootEl && !rootEl.contains(e.target as Node)) open = false;
    }
</script>

<svelte:window onclick={onWindowClick} />

<div class={className} bind:this={rootEl}>
    <div class="flex flex-row items-center gap-x-1">
        <label for={id} class="block text-sm font-medium text-ink">{label}</label>
        <button
            id={triggerId}
            type="button"
            disabled={items.length === 0}
            onclick={() => (open = !open)}
            class="cursor-pointer leading-none text-muted hover:text-ink disabled:cursor-not-allowed disabled:opacity-40"
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
                class="size-4"
                aria-hidden="true"
            >
                <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 0 0 2.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 0 0-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 0 0 .75-.75 2.25 2.25 0 0 0-.1-.664m-5.8 0A2.251 2.251 0 0 1 13.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25ZM6.75 12h.008v.008H6.75V12Zm0 3h.008v.008H6.75V15Zm0 3h.008v.008H6.75V18Z"
                />
            </svg>
        </button>
    </div>
    <div class="relative mt-1">
        <input {id} class="input w-full" autocomplete="off" bind:value />

        {#if open && items.length > 0}
            <!-- Popover under the field. tabindex=-1 so the $effect can focus it
                 (catching Escape); keydown lives here, not on window, so its
                 stopPropagation shields the form-level Escape. Items are buttons →
                 keyboard + pointer both work; long comments truncate with the full
                 text in the title. -->
            <div
                id={listId}
                bind:this={listEl}
                role="menu"
                tabindex="-1"
                onkeydown={onListKeydown}
                class="absolute top-full left-0 z-10 max-h-44 w-full overflow-y-auto rounded-md border border-line bg-surface shadow-md focus:outline-none"
            >
                {#each items as comment (comment)}
                    <button
                        type="button"
                        role="menuitem"
                        onclick={() => pick(comment)}
                        title={comment}
                        class="block w-full cursor-pointer truncate px-3 py-1.5 text-left text-sm text-ink hover:bg-surface-muted"
                    >
                        {comment}
                    </button>
                {/each}
            </div>
        {/if}
    </div>
</div>
