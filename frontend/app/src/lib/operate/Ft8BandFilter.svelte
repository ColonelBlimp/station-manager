<script lang="ts">
    import { ft8State, ft8HideHashed } from './ft8.svelte';

    // Band Activity filter — the funnel button beside the header opens this popover.
    // The typed token-prefix filter (ft8State.bandFilter) narrows the feed live,
    // session-scoped, no save; a station calling us bypasses it (matched in
    // Ft8BandActivity). Hide-hashed is config-read (ft8.display.hide_hashed_calls),
    // reflected in the "active" cue; its live toggle arrives with config editing.
    // Popover mechanics: focus-on-open, Escape + click-outside to close.
    let open = $state(false);
    let rootEl: HTMLDivElement | undefined = $state();
    let inputEl: HTMLInputElement | undefined = $state();

    // Funnel reads "active" whenever a filter is narrowing the feed — with the
    // popover closed this is the only cue that rows are being hidden.
    const active = $derived(ft8State.bandFilter.trim() !== '' || ft8HideHashed());

    $effect(() => {
        if (open) inputEl?.focus();
    });

    function onPopoverKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') {
            e.stopPropagation();
            open = false;
        } else if (e.key === 'Enter') {
            e.preventDefault(); // applies live via bind:value — Enter just dismisses
            open = false;
        }
    }

    function onWindowClick(e: MouseEvent): void {
        if (open && rootEl && !rootEl.contains(e.target as Node)) open = false;
    }
</script>

<svelte:window onclick={onWindowClick} />

<div class="relative inline-flex" bind:this={rootEl}>
    <button
        type="button"
        onclick={() => (open = !open)}
        class="cursor-pointer p-0.5 leading-none {active
            ? 'text-focus'
            : 'text-muted hover:text-ink'}"
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Filter Band Activity"
    >
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke-width="1.5"
            stroke="currentColor"
            class="size-5"
            aria-hidden="true"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M12 3c2.755 0 5.455.232 8.083.678.533.09.917.556.917 1.096v1.044a2.25 2.25 0 0 1-.659 1.591l-5.432 5.432a2.25 2.25 0 0 0-.659 1.591v2.927a2.25 2.25 0 0 1-1.244 2.013L9.75 21v-6.568a2.25 2.25 0 0 0-.659-1.591L3.659 7.409A2.25 2.25 0 0 1 3 5.818V4.774c0-.54.384-1.006.917-1.096A48.32 48.32 0 0 1 12 3Z"
            />
        </svg>
    </button>

    {#if open}
        <div
            role="dialog"
            tabindex="-1"
            aria-label="Band Activity filter"
            onkeydown={onPopoverKeydown}
            class="absolute top-full left-0 z-20 mt-1 w-56 rounded-md border border-line bg-surface p-3 text-left shadow-xl focus:outline-none"
        >
            <label class="flex flex-col gap-1 text-xs text-muted">
                <span>Show calls starting with</span>
                <div class="flex items-center gap-1">
                    <input
                        bind:this={inputEl}
                        bind:value={ft8State.bandFilter}
                        type="text"
                        placeholder="e.g. VK"
                        class="w-full rounded border border-line bg-surface px-2 py-1 text-sm text-ink uppercase focus:ring-2 focus:ring-focus-ring focus:outline-none"
                    />
                    {#if ft8State.bandFilter !== ''}
                        <button
                            type="button"
                            onclick={() => (ft8State.bandFilter = '')}
                            class="cursor-pointer px-1 leading-none text-muted hover:text-ink"
                            title="Clear filter"
                            aria-label="Clear filter"><span aria-hidden="true">×</span></button
                        >
                    {/if}
                </div>
            </label>
        </div>
    {/if}
</div>
