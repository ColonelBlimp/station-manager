<script lang="ts">
    // Pile-up drawer — docked slide-over from the right, inboard of the util rail.
    // Starts below the header (top: 4rem) and parks fully off-screen when closed
    // (fixed transform, no rail-width dependency → no flash on rail collapse).
    //
    // Content (ADR 0067): the DAEMON's caller list for any pick context — a pick
    // CQ run or the listing run every pick session leaves behind. Two sections,
    // one mechanism: CALLING YOU (listed — heard, unworked; Work commits one now,
    // Bag queues it) and BAGGED (the operator's explicit choices, auto-worked by
    // the drain in bag order; × unbags back to listed). The old SPA-curated
    // ctrl-click stack retired with this ADR — ctrl-click now bags daemon-side.
    //
    // The header × only CLOSES the slide-over — it leaves the run intact (a
    // slide-over close shouldn't destroy state). Stopping the run lives on the
    // run surface (Stop pauses the drain; the footer Resume here continues it).
    import { ft8State, bagAnswerer, unbagAnswerer, pickAnswerer, resumeDrain } from './ft8.svelte';
    import { operate, setPileup } from './state.svelte';
    import { toasts } from '../ui/toasts.svelte';

    const qso = $derived(ft8State.qso);
    let acting = $state(false);

    // One latch for all four verbs: each is confirmed by push (the frame the
    // daemon publishes), and refusals surface verbatim — the daemon's message
    // names the cause (no run / station left / contact in flight).
    async function act(fn: () => Promise<{ ok: boolean; message: string }>): Promise<void> {
        if (acting) return;
        acting = true;
        try {
            const r = await fn();
            if (!r.ok) toasts.error(r.message);
        } finally {
            acting = false;
        }
    }

    function onKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') setPileup(false);
    }
</script>

<svelte:window onkeydown={onKeydown} />

<!-- inert while closed — see CallsignStackPanel. Same defect, and it predates
     that drawer: a hidden-by-transform subtree is still in the focus order. -->
<aside
    class="pileup-drawer"
    data-open={operate.pileup}
    data-list="callers"
    inert={!operate.pileup}
    aria-label="Pile-up"
>
    <div
        class="flex h-full flex-col border-l border-line bg-surface"
        class:shadow-xl={operate.pileup}
    >
        <div class="flex items-start justify-between px-4 py-4 sm:px-6">
            <div>
                <h2 class="text-base font-semibold text-ink">
                    Pile-up{#if qso.answerers.length + qso.queue.length > 0}
                        <span class="text-muted">({qso.answerers.length + qso.queue.length})</span
                        >{/if}
                </h2>
                <p class="mt-0.5 text-xs">
                    {#if qso.drainPaused && qso.queue.length > 0}
                        <span class="font-semibold text-amber-600 dark:text-amber-400">Paused</span>
                    {:else}
                        <span class="text-muted">bagged stations are worked in order</span>
                    {/if}
                </p>
            </div>
            <button
                class="cursor-pointer rounded-md text-muted hover:text-ink"
                title="Close"
                onclick={() => setPileup(false)}
            >
                <span class="sr-only">Close panel</span>
                <svg
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    aria-hidden="true"
                    class="size-6"
                >
                    <path d="M6 18 18 6M6 6l12 12" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
            </button>
        </div>

        <div class="flex-1 overflow-y-auto px-4 sm:px-6">
            <h3 class="mt-1 text-sm font-semibold text-ink" data-testid="listed-heading">
                Calling you{#if qso.answerers.length > 0}
                    <span class="text-muted">({qso.answerers.length})</span>{/if}
            </h3>
            {#if qso.answerers.length === 0}
                <p class="mt-1 text-sm text-muted">
                    Nobody yet — with the Answer mode on “I pick”, stations calling you are listed
                    here. Work one now, or bag several and they are worked in order.
                </p>
            {:else}
                <ul class="mt-1 flex flex-col divide-y divide-line">
                    {#each qso.answerers as a (a.call)}
                        <li class="flex items-center gap-1.5 py-1.5">
                            <span class="flex-1 truncate font-mono text-sm font-semibold text-ink"
                                >{a.call}</span
                            >
                            <span class="shrink-0 text-xs tabular-nums text-muted">{a.snr} dB</span>
                            <button
                                class="cursor-pointer rounded-md border border-line px-2 py-0.5 text-xs font-medium text-ink hover:border-focus disabled:opacity-50"
                                title="Bag — work them from the queue, in order"
                                aria-label={`Bag ${a.call}`}
                                disabled={acting}
                                onclick={() => void act(() => bagAnswerer(a.call))}
                            >
                                Bag
                            </button>
                            <button
                                class="cursor-pointer rounded-md border border-line px-2 py-0.5 text-xs font-medium text-focus hover:border-focus disabled:opacity-50"
                                title="Work this station now"
                                aria-label={`Work ${a.call} now`}
                                disabled={acting}
                                onclick={() => void act(() => pickAnswerer(a.call))}
                            >
                                Work
                            </button>
                        </li>
                    {/each}
                </ul>
            {/if}

            <h3 class="mt-3 text-sm font-semibold text-ink" data-testid="bagged-heading">
                Bagged{#if qso.queue.length > 0}
                    <span class="text-muted">({qso.queue.length})</span>{/if}
            </h3>
            {#if qso.queue.length === 0}
                <p class="mt-1 text-sm text-muted">
                    Nothing bagged. Bagged stations are worked automatically, in the order you bag
                    them — each one was your choice.
                </p>
            {:else}
                <ul class="mt-1 flex flex-col divide-y divide-line" data-testid="bagged-list">
                    {#each qso.queue as e, i (e.call)}
                        <li class="flex items-center gap-1.5 py-1.5">
                            <span
                                class="flex-1 truncate font-mono text-sm {i === 0
                                    ? 'font-semibold text-ink'
                                    : 'text-ink'}"
                                title={i === 0 ? 'Worked next' : undefined}
                            >
                                {e.call}
                            </span>
                            <span class="shrink-0 text-xs tabular-nums text-muted">{e.snr} dB</span>
                            <button
                                class="cursor-pointer rounded p-0.5 leading-none text-muted hover:text-red-600"
                                title="Unbag — back to the listed callers"
                                aria-label={`Unbag ${e.call}`}
                                onclick={() => void act(() => unbagAnswerer(e.call))}
                            >
                                <span aria-hidden="true">×</span>
                            </button>
                        </li>
                    {/each}
                </ul>
            {/if}
        </div>

        {#if qso.drainPaused && qso.queue.length > 0}
            <div
                class="flex items-center justify-between gap-2 border-t border-line px-4 py-3 sm:px-6"
            >
                <button
                    class="cursor-pointer text-sm font-medium text-focus hover:underline"
                    title="Resume working through the bagged queue"
                    data-testid="drawer-resume"
                    onclick={() => void act(() => resumeDrain())}
                >
                    Resume
                </button>
                <span></span>
            </div>
        {/if}
    </div>
</aside>
