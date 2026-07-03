<script lang="ts">
    import { ft8PileupStack } from '../../states/ft8PileupStack.svelte';
    import { ft8State } from '../../states/ft8.svelte';
    import { abandonFt8Qso } from '../../api/ft8qso';
    import { toasts } from '../../states/toasts.svelte';
    import { slotParity } from '../../utils/ft8Parity';

    /*
        FT8 pile-up drawer — the operator-curated FIFO of stations calling you,
        worked oldest-first. Ctrl+click on a calling-you decode in Band Activity
        enqueues here; the Operate view drains it via the work-a-caller path while the
        operator keeps adding. The currently worked station isn't shown here — it's
        dequeued on start and shown in the ladder/banner; this list is "up next".

        Deliberately a visual + operational twin of the Phone/CW StackingDrawer so the
        operator recognises the widget on sight and knows how to drive it: it hangs off
        the right edge of the logging card (mounted alongside StackingDrawer in app.svelte),
        always visible (when non-empty) regardless of which FT8 sub-tab is open.
          - data-driven visibility — hidden until non-empty, auto-closes when emptied;
          - per-row × removes that one caller (discard);
          - header × clears all + closes — AND abandons the run (see handleClose).
        The one mechanics difference: the FT8 daemon auto-drains the queue, so clicking
        a row here is a NO-OP (nothing to "load") — whereas a Phone/CW row click pops the
        call into the QSO draft. Grid/SNR are captured in each entry but not displayed;
        the list is a compact callsign roster.

        Self-contained (like StackingDrawer) because it's mounted at the card level, not
        inside the Operate-tab Ft8MsgPanel — so it can't borrow that panel's abandon
        handler. The header × clears the queue (→ auto-hide) and, when an exchange is
        actually in flight (armed + active), aborts it on the daemon. With no active
        exchange (incl. the ?pileupdemo mount, where ft8State stays idle) it just halts
        the drain + clears — no daemon call.
    */
    const canAbandon = $derived(ft8State.tx.armed && ft8State.qso.active);
    // The whole queue is single-parity (enforced at enqueue), so its parity is the
    // head's. Shown in the header to reinforce "this is the {even|odd} run".
    const runParity = $derived(
        ft8PileupStack.items.length > 0 ? slotParity(ft8PileupStack.items[0].slotUtc) : ''
    );
    let closing = $state(false);

    async function handleClose(): Promise<void> {
        if (closing) return;
        closing = true;
        try {
            if (canAbandon) {
                const out = await abandonFt8Qso();
                if (out.kind !== 'ok') toasts.error(out.message);
            }
            ft8PileupStack.pause();
            ft8PileupStack.clear();
        } finally {
            closing = false;
        }
    }
</script>

{#if ft8PileupStack.items.length > 0}
    <div class="w-38 max-h-166 overflow-y-auto rounded-xl border border-line-soft px-3">
        <div class="flex flex-row items-center justify-between">
            <h2
                class="flex flex-col mt-2.5 text-xs font-semibold uppercase tracking-tight text-orange-600"
            >
                <span
                    >Pile-up ({ft8PileupStack.count}){#if runParity}
                        · {runParity}{/if}</span
                >
                <span
                    >{#if !ft8PileupStack.enabled}Paused{/if}</span
                >
            </h2>
            <div class="flex items-center">
                {#if !ft8PileupStack.enabled}
                    <button
                        type="button"
                        class="cursor-pointer text-xs font-medium text-indigo-600 hover:underline"
                        title="Resume working through the stack"
                        onclick={() => ft8PileupStack.resume()}>Resume</button
                    >
                {/if}
                <button
                    type="button"
                    class="cursor-pointer px-1 leading-none text-gray-400 hover:text-gray-700"
                    aria-label="Clear the pile-up stack and abandon the run"
                    title="Clear all & abandon"
                    onclick={handleClose}><span aria-hidden="true">×</span></button
                >
            </div>
        </div>
        <ul class="-mt-2 flex flex-col">
            {#each ft8PileupStack.items as e, i (e.call)}
                <li class="flex items-center">
                    {#if i > 0}
                        <button
                            type="button"
                            class="cursor-pointer px-1 leading-none text-gray-400 hover:text-indigo-600"
                            aria-label={`Move ${e.call} up the pile-up`}
                            title="Move up"
                            onclick={() => ft8PileupStack.moveUp(i)}
                            ><span aria-hidden="true">↑</span></button
                        >
                    {:else}
                        <!-- Head row: no up-arrow (already next); spacer keeps calls aligned. -->
                        <span class="px-1" aria-hidden="true">&nbsp;</span>
                    {/if}
                    <span class="flex-1 truncate px-1 py-1 font-mono text-sm text-gray-800"
                        >{e.call}</span
                    >
                    <button
                        type="button"
                        class="cursor-pointer px-1 leading-none text-gray-400 hover:text-red-600"
                        aria-label={`Remove ${e.call} from the pile-up`}
                        title="Remove"
                        onclick={() => ft8PileupStack.remove(i)}
                        ><span aria-hidden="true">×</span></button
                    >
                </li>
            {/each}
        </ul>
    </div>
{/if}
