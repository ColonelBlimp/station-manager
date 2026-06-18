<script lang="ts">
    import { ft8State } from '../../states/ft8.svelte';
    import { configState } from '../../states/config.svelte';
    import { toasts } from '../../states/toasts.svelte';

    /*
        Band Activity filter popover — the funnel button beside the "Band Activity"
        header opens this. It holds the two controls that HIDE rows:
          - typed token-prefix filter (ft8State.bandFilter): session-scoped, instant,
            no save; a station calling us bypasses it (the match runs in Ft8Panel).
          - hide hashed calls (configState.ft8Display.hideHashedCalls): durable config,
            auto-saved on toggle via configState.saveFt8Display().
        CQ-to-top is ORDERING (it reorders, doesn't hide), so it stays in the Settings
        tab — not a filter. Popover mechanics mirror the Comment paste-list popover:
        focus-on-open, Escape + click-outside to close, stopPropagation so Escape
        doesn't also trip a form-level handler.
    */
    let open = $state(false);
    let rootEl: HTMLDivElement | undefined = $state();
    let inputEl: HTMLInputElement | undefined = $state();

    // The funnel shows an "active" state whenever a filter is actually narrowing the
    // feed — with the popover closed this is the only cue that rows are being hidden.
    const active = $derived(
        ft8State.bandFilter.trim() !== '' || configState.ft8Display.hideHashedCalls
    );

    $effect(() => {
        if (open) inputEl?.focus();
    });

    function onPopoverKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') {
            e.stopPropagation();
            open = false;
        } else if (e.key === 'Enter') {
            // The filter applies live (bind:value), so the value is already set —
            // Enter just commits + dismisses. preventDefault: no form to submit.
            e.preventDefault();
            open = false;
        }
    }

    function onWindowClick(e: MouseEvent): void {
        if (!open) return;
        if (rootEl && !rootEl.contains(e.target as Node)) open = false;
    }

    // Hide-hashed is durable config — persist on toggle. bind:checked has already
    // updated configState by the time onchange fires, so saveFt8Display() sends the
    // new value. Fire-and-forget; a failed save just toasts.
    async function saveHideHashed(): Promise<void> {
        const outcome = await configState.saveFt8Display();
        if (outcome.kind !== 'ok' && outcome.kind !== 'aborted') {
            toasts.error('Could not save the hide-hashed setting.');
        }
    }
</script>

<svelte:window onclick={onWindowClick} />

<div class="relative inline-flex" bind:this={rootEl}>
    <button
        type="button"
        onclick={() => (open = !open)}
        class="cursor-pointer p-0.5 leading-none {active
            ? 'text-indigo-600'
            : 'text-gray-400 hover:text-gray-700'}"
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Filter Band Activity"
    >
        <svg
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
            stroke-width="1.5"
            stroke="currentColor"
            class="size-5"
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
            aria-label="Band Activity filters"
            onkeydown={onPopoverKeydown}
            class="absolute left-1/2 top-full z-20 mt-1 w-60 -translate-x-1/2 rounded-md border border-line-soft bg-white p-3 text-left shadow-xl focus:outline-none"
        >
            <label class="flex flex-col gap-1 text-xs text-gray-600">
                <span>Show calls starting with</span>
                <div class="flex items-center gap-1">
                    <input
                        bind:this={inputEl}
                        bind:value={ft8State.bandFilter}
                        type="text"
                        placeholder="e.g. VK"
                        class="w-full rounded border border-gray-300 px-2 py-1 text-sm uppercase focus:outline-none focus:ring-2 focus:ring-indigo-500"
                    />
                    {#if ft8State.bandFilter !== ''}
                        <button
                            type="button"
                            onclick={() => (ft8State.bandFilter = '')}
                            class="cursor-pointer px-1 leading-none text-gray-400 hover:text-gray-700"
                            title="Clear filter"
                            aria-label="Clear filter"><span aria-hidden="true">×</span></button
                        >
                    {/if}
                </div>
            </label>

            <label class="mt-3 flex items-center justify-between gap-3 text-sm text-gray-700">
                <!-- Hides decodes with an unresolved hashed call ("<...>") — dross you
                     can't identify or work. Stations calling you still show through.
                     Durable: auto-saved to config on toggle. -->
                <span>Hide hashed calls (&lt;...&gt;)</span>
                <input
                    type="checkbox"
                    bind:checked={configState.ft8Display.hideHashedCalls}
                    onchange={() => void saveHideHashed()}
                    class="h-4 w-4 cursor-pointer rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
                />
            </label>
        </div>
    {/if}
</div>
