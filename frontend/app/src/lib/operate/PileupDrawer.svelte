<script lang="ts">
    // Pile-up drawer — docked slide-over from the right, inboard of the util rail.
    // Starts below the header (top: 4rem) and parks fully off-screen when closed
    // (fixed transform, no rail-width dependency → no flash on rail collapse).
    //
    // Content: the operator-curated FT8 pile-up (ft8PileupStack) — the FIFO of
    // stations calling you, worked oldest-first by the Operate drain. Ctrl+click a
    // calling-you decode in Band Activity to enqueue; this drawer is "up next" (the
    // station being worked is dequeued on start and shown in the Operate ladder, so
    // it isn't listed here). Per-row ↑ promotes a caller toward the head; per-row ×
    // discards one; the footer Resume un-pauses the drain (Abandon pauses it, keeping
    // the queue) and Clear & abandon halts the whole run.
    //
    // The header × only CLOSES the slide-over — it leaves the run intact (a slide-over
    // close shouldn't destroy state). Tearing the run down is the deliberate, distinct
    // Clear & abandon footer action.
    import { ft8PileupStack } from './ft8Pileup.svelte';
    import { ft8State, abandonQso, pickAnswerer } from './ft8.svelte';
    import { operate, setPileup } from './state.svelte';
    import { toasts } from '../ui/toasts.svelte';
    import { slotParity } from '../utils/ft8Parity';

    // Abandon only fires a daemon stop when an exchange is actually in flight.
    const canAbandon = $derived(ft8State.tx.armed && ft8State.qso.active);
    // A Call-CQ run owns the rig via the daemon's auto-pick, so the drain must stay
    // paused during it — Resume would start a second, competing controller against the
    // caller session. Hide Resume while a caller run is active (same trap the Next
    // control guards). A pre-existing queue can still render during a run.
    const callerActive = $derived(ft8State.qso.active && ft8State.qso.role === 'caller');
    // operator_pick run (ADR 0065): the drawer's primary content is the DAEMON's
    // candidate list (stations answering the CQ), not the curated ctrl-click stack —
    // clicking one commits it into the run via POST /v1/ft8/cq/pick. The list rides
    // the ft8-qso frames, so it clears itself with the run (daemon rule 10).
    const pickRun = $derived(callerActive && ft8State.qso.answerMode === 'operator_pick');
    let picking = $state(false);

    // Pop a candidate — the commit is confirmed by push (the frame the pop
    // publishes); on refusal the daemon's message says which of the three causes
    // (no run / station left / contact in flight) so it is surfaced verbatim.
    async function onPick(call: string): Promise<void> {
        if (picking) return;
        picking = true;
        try {
            const r = await pickAnswerer(call);
            if (!r.ok) toasts.error(r.message);
        } finally {
            picking = false;
        }
    }
    // The queue is single-parity (enforced at enqueue), so its run parity is the head's.
    const runParity = $derived(
        ft8PileupStack.items.length > 0 ? slotParity(ft8PileupStack.items[0].slotUtc) : ''
    );
    let clearing = $state(false);

    function onKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') setPileup(false);
    }

    // Clear & abandon — halt the run: abandon any in-flight exchange on the daemon,
    // pause the drain, empty the queue. Distinct from the header × (close only).
    async function clearAndAbandon(): Promise<void> {
        if (clearing) return;
        clearing = true;
        try {
            if (canAbandon) {
                const r = await abandonQso();
                if (!r.ok) toasts.error(r.message);
            }
            ft8PileupStack.pause();
            ft8PileupStack.clear();
        } finally {
            clearing = false;
        }
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
                    Pile-up{#if ft8PileupStack.count > 0}
                        <span class="text-muted">({ft8PileupStack.count})</span>{/if}
                </h2>
                <p class="mt-0.5 text-xs">
                    {#if !ft8PileupStack.enabled && ft8PileupStack.count > 0}
                        <span class="font-semibold text-amber-600 dark:text-amber-400">Paused</span>
                    {:else if runParity}
                        <span class="text-muted">{runParity} run · worked oldest-first</span>
                    {:else}
                        <span class="text-muted">worked oldest-first</span>
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
            {#if pickRun}
                <!-- operator_pick candidates come FIRST: during a pick run they are
                     what the drawer is for. The curated stack (if any) keeps
                     rendering below — it resumes its meaning when the run ends. -->
                <h3 class="mt-1 text-sm font-semibold text-ink">
                    Answering your CQ{#if ft8State.qso.answerers.length > 0}
                        <span class="text-muted">({ft8State.qso.answerers.length})</span>{/if}
                </h3>
                {#if ft8State.qso.answerers.length === 0}
                    <p class="mt-1 text-sm text-muted">
                        No answerers yet — stations that answer your CQ appear here. The CQ keeps
                        calling until you work one.
                    </p>
                {:else}
                    <ul class="mt-1 flex flex-col divide-y divide-line">
                        {#each ft8State.qso.answerers as a (a.call)}
                            <li class="flex items-center gap-1.5 py-1.5">
                                <span
                                    class="flex-1 truncate font-mono text-sm font-semibold text-ink"
                                    >{a.call}</span
                                >
                                <span class="shrink-0 text-xs tabular-nums text-muted"
                                    >{a.snr} dB</span
                                >
                                <button
                                    class="cursor-pointer rounded-md border border-line px-2 py-0.5 text-xs font-medium text-focus hover:border-focus disabled:opacity-50"
                                    title="Work this station now"
                                    aria-label={`Work ${a.call} now`}
                                    disabled={picking}
                                    onclick={() => void onPick(a.call)}
                                >
                                    Work
                                </button>
                            </li>
                        {/each}
                    </ul>
                {/if}
            {/if}
            {#if ft8PileupStack.items.length === 0}
                {#if !pickRun}
                    <p class="mt-2 text-sm text-muted">
                        No callers queued. <span class="font-medium text-ink">Ctrl-click</span> a station
                        calling you in Band Activity to add it to the pile-up.
                    </p>
                {/if}
            {:else}
                <ul class="flex flex-col divide-y divide-line">
                    {#each ft8PileupStack.items as e, i (e.call)}
                        <li class="flex items-center gap-1.5 py-1.5">
                            {#if i > 0}
                                <button
                                    class="cursor-pointer rounded p-0.5 leading-none text-muted hover:text-focus"
                                    title="Move up (work sooner)"
                                    aria-label={`Move ${e.call} up the pile-up`}
                                    onclick={() => ft8PileupStack.moveUp(i)}
                                >
                                    <span aria-hidden="true">↑</span>
                                </button>
                            {:else}
                                <!-- Head row: worked next, so no up-arrow; spacer keeps calls aligned. -->
                                <span class="w-[1.375rem] shrink-0" aria-hidden="true"></span>
                            {/if}
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
                                title="Remove from the pile-up"
                                aria-label={`Remove ${e.call} from the pile-up`}
                                onclick={() => ft8PileupStack.remove(i)}
                            >
                                <span aria-hidden="true">×</span>
                            </button>
                        </li>
                    {/each}
                </ul>
            {/if}
        </div>

        {#if ft8PileupStack.items.length > 0}
            <div
                class="flex items-center justify-between gap-2 border-t border-line px-4 py-3 sm:px-6"
            >
                {#if !ft8PileupStack.enabled && !callerActive}
                    <button
                        class="cursor-pointer text-sm font-medium text-focus hover:underline"
                        title="Resume working through the pile-up"
                        onclick={() => ft8PileupStack.resume()}
                    >
                        Resume
                    </button>
                {:else}
                    <span></span>
                {/if}
                <button
                    class="cursor-pointer rounded-md border border-line px-2.5 py-1 text-sm font-medium text-muted hover:border-red-500/50 hover:text-red-600 disabled:opacity-50"
                    title="Abandon the run and empty the pile-up"
                    disabled={clearing}
                    onclick={clearAndAbandon}
                >
                    Clear &amp; abandon
                </button>
            </div>
        {/if}
    </div>
</aside>
