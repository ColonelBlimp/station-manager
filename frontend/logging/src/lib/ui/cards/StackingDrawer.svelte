<script lang="ts">
    /*
        Callsign-stack drawer. Data-driven visibility: rendered iff the
        stack is non-empty. No manual toggle, no "open empty drawer" —
        the operator never has to manage chrome. A small × inside the
        header handles "discard all" without conflating "hide UI" with
        "drop data."

        Pop semantics: clicking an entry transfers the call into
        qsoDraft.callsign and removes it from the stack. Same single-
        action / single-effect contract as the Shift+Up / Shift+Down
        keyboard pops in QsoPanel — no third "in field AND in stack"
        state to reason about. Focus returns to the Callsign input so
        the operator can immediately Tab / F2 to enrich.
    */
    import { callsignStack } from '../../states/callsignStack.svelte';
    import { qsoDraft } from '../../states/qsoDraft.svelte';

    function pop(index: number): void {
        const call = callsignStack.popAt(index);
        if (call === undefined) return;
        qsoDraft.callsign = call;
        document.getElementById('call')?.focus();
    }

    function discardAll(): void {
        callsignStack.clear();
    }
</script>

{#if callsignStack.items.length > 0}
    <div
        class="absolute top-0 -right-37 w-33 h-166 rounded-xl border border-line-soft px-3 overflow-y-auto">
        <div class="flex flex-row items-center justify-between">
            <h2 class="text-xs font-semibold text-orange-600 uppercase tracking-tight mt-2.5">
                Call Stack ({callsignStack.count})
            </h2>
            <button
                type="button"
                class="text-gray-400 hover:text-gray-700 cursor-pointer leading-none px-1"
                aria-label="Discard all stacked callsigns"
                title="Discard all"
                onclick={discardAll}
            >
                <span aria-hidden="true">×</span>
            </button>
        </div>
        <ul class="flex flex-col gap-1 -mt-2">
            {#each callsignStack.items as call, index (call)}
                <li class="flex items-center">
                    <button
                        type="button"
                        class="w-full text-left px-2 py-1 rounded font-mono text-sm hover:bg-gray-100 cursor-pointer"
                        title="Load {call} (pop from stack)"
                        onclick={() => pop(index)}
                    >
                        {call}
                    </button>
                </li>
            {/each}
        </ul>
    </div>
{/if}
