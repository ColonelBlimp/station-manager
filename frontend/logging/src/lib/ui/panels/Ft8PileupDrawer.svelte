<script lang="ts">
    import { ft8PileupStack } from '../../states/ft8PileupStack.svelte';

    /*
        FT8 pile-up drawer — the operator-curated FIFO of stations calling you,
        worked oldest-first. Ctrl+click on a calling-you decode in Band Activity
        enqueues here; the Operate view drains it via the work-a-caller path while the
        operator keeps adding. The currently worked station isn't shown here — it's
        dequeued on start and shown in the ladder/banner; this list is "up next".

        Deliberately a visual + operational twin of the Phone/CW StackingDrawer so the
        operator recognises the widget on sight and knows how to drive it:
          - data-driven visibility — hidden until non-empty, auto-closes when emptied;
          - per-row × removes that one caller (discard);
          - header × clears all + closes — AND abandons the run (see onClose).
        The one mechanics difference: the FT8 daemon auto-drains the queue, so clicking
        a row here is a NO-OP (nothing to "load") — whereas a Phone/CW row click pops the
        call into the QSO draft. Grid/SNR are captured in each entry but not displayed;
        the list is a compact callsign roster.

        onClose is the header-× handler, injected by the parent (Ft8MsgPanel), so this
        component stays presentational and doesn't reach for the lib/api abandon call
        itself. It clears the queue, closes the drawer (clear → auto-hide), and abandons
        the active exchange. Standalone/demo mounts omit it and fall back to a plain
        clear (no daemon to abandon).
    */
    let { onClose }: { onClose?: () => void | Promise<void> } = $props();

    function handleClose(): void {
        if (onClose) {
            void onClose();
            return;
        }
        ft8PileupStack.clear();
    }
</script>

{#if ft8PileupStack.items.length > 0}
    <div class="mt-2 w-33 max-h-60 overflow-y-auto rounded-xl border border-line-soft px-3">
        <div class="flex flex-row items-center justify-between">
            <h2 class="mt-2.5 text-xs font-semibold uppercase tracking-tight text-orange-600">
                Pile-up ({ft8PileupStack.count}){#if !ft8PileupStack.enabled}&nbsp;· paused{/if}
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
                    <span class="flex-1 truncate px-2 py-1 font-mono text-sm text-gray-800"
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
