<script lang="ts">
    import { ft8PileupStack } from '../../states/ft8PileupStack.svelte';

    /*
        FT8 pile-up drawer — the operator-curated FIFO of stations calling you,
        worked oldest-first. Ctrl+click on a calling-you decode in Band Activity
        enqueues here; the Operate view drains it via the work-a-caller path while the
        operator keeps adding. Data-driven visibility (rendered only when non-empty),
        mirroring the Phone/CW StackingDrawer. Per-entry remove + Clear-all; a Resume
        control appears when auto-drain was paused (by Abandon). The currently-worked
        station isn't shown here — it's dequeued on start and shown in the ladder/banner;
        this list is "up next".
    */
    function snr(n: number): string {
        return (n >= 0 ? '+' : '-') + String(Math.abs(n)).padStart(2, '0');
    }
</script>

{#if ft8PileupStack.items.length > 0}
    <div class="mt-2 rounded border border-amber-300 bg-amber-50 px-2 py-1 text-left">
        <div class="flex items-center justify-between">
            <h3 class="text-xs font-semibold uppercase tracking-tight text-amber-700">
                Pile-up ({ft8PileupStack.count}){#if !ft8PileupStack.enabled}&nbsp;· paused{/if}
            </h3>
            <div class="flex items-center gap-2">
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
                    aria-label="Clear the pile-up stack"
                    title="Clear all"
                    onclick={() => ft8PileupStack.clear()}><span aria-hidden="true">×</span></button
                >
            </div>
        </div>
        <ul class="mt-1 flex max-h-28 flex-col gap-0.5 overflow-y-auto font-mono text-xs">
            {#each ft8PileupStack.items as e, i (e.call)}
                <li class="flex items-center justify-between gap-2 rounded px-1 hover:bg-amber-100">
                    <span class="truncate">
                        <span class="font-semibold text-gray-800">{e.call}</span>
                        {#if e.grid}<span class="text-gray-500">&nbsp;{e.grid}</span>{/if}
                        <span class="text-gray-400">&nbsp;{snr(e.snr)}</span>
                    </span>
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
