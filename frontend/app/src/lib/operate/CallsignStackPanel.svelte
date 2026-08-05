<script lang="ts">
    /*
        Phone/CW pile-up scratchpad — the calls set aside with Shift+Enter,
        ported from the retired SPA's StackingDrawer.

        DATA-DRIVEN VISIBILITY: rendered iff the stack is non-empty. No toggle,
        no chrome to manage, and — the reason it matters here — no empty drawer
        that can be opened. FT8's pile-up drawer IS toggleable, and opening it
        over Phone/CW used to disable the logging shortcuts; a panel that cannot
        exist while empty cannot repeat that.

        Pop semantics match the Shift+Up / Shift+Down keys in LoggingCard:
        clicking an entry moves the call INTO the draft and out of the stack —
        never both — and returns focus to the callsign field so the operator can
        Tab straight on. The per-row × discards without loading, for a call they
        have decided not to work.
    */
    import { callsignStack } from './callsignStack.svelte';
    import { draft } from './qso.svelte';
    import { focusCallsign } from './state.svelte';

    function pop(index: number): void {
        const call = callsignStack.popAt(index);
        if (call === undefined) return;
        draft.callsign = call;
        focusCallsign();
    }
</script>

{#if callsignStack.items.length > 0}
    <section class="card" aria-label="Pile-up">
        <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-ink">
                Pile-up <span class="text-muted">({callsignStack.count})</span>
            </h3>
            <button
                type="button"
                class="cursor-pointer rounded-md px-1 text-sm leading-none text-muted hover:text-ink"
                title="Discard all"
                aria-label="Discard all stacked callsigns"
                onclick={() => callsignStack.clear()}>×</button
            >
        </div>
        <p class="mt-1 text-xs text-muted">
            Shift+Enter sets one aside · Shift+↑ newest · Shift+↓ oldest
        </p>
        <ul class="mt-3 flex flex-wrap gap-1.5">
            {#each callsignStack.items as call, index (call)}
                <li class="flex items-center overflow-hidden rounded-md border border-line">
                    <button
                        type="button"
                        class="cursor-pointer px-2 py-1 font-mono text-sm text-ink hover:bg-surface-muted"
                        title="Load {call} — takes it off the pile-up"
                        onclick={() => pop(index)}>{call}</button
                    >
                    <button
                        type="button"
                        class="cursor-pointer border-l border-line px-1.5 py-1 text-xs leading-none text-muted hover:text-ink"
                        title="Remove {call}"
                        aria-label={`Remove ${call} from the pile-up`}
                        onclick={() => callsignStack.removeAt(index)}>×</button
                    >
                </li>
            {/each}
        </ul>
    </section>
{/if}
