<script lang="ts">
    /*
        Phone/CW pile-up — the calls set aside with Shift+Enter, ported from the
        retired SPA's StackingDrawer + callsignStack.

        PLACEMENT: the same slide-in drawer as FT8's, against the inside edge of
        the right-hand rail (`.pileup-drawer` — fixed, `right: var(--util-rail-w)`,
        translated off-screen when closed). It was a card in the content column
        first, which reflowed the logging card underneath it as calls came and
        went — moving the very surface the operator is typing on. Same
        affordance, same job, same place as FT8's.

        VISIBILITY IS THE RAIL TOGGLE, not emptiness. It was emptiness at first,
        on the reasoning that an un-openable panel could not repeat FT8's trap.
        But the trap was never the empty panel — it was `operate.pileup` gating
        the logging shortcuts inside LoggingCard, which is gone — and hiding it
        when empty meant the rail icon did NOTHING until you already knew the
        shortcut that fills it (operating, 2026-08-05). So the empty state stays
        rendered, and it carries the bindings: this is where an operator finds
        out the feature exists.

        NO WINDOW ESCAPE HANDLER, deliberately — unlike FT8's drawer. In Phone/CW
        Escape clears the QSO draft (LoggingCard), and a second window-level
        handler for the same key is precisely the collision that made the FT8
        drawer hostile here. Closing is the rail icon or the × below.

        Pop semantics match the Shift+Up / Shift+Down keys in LoggingCard:
        clicking an entry moves the call INTO the draft and out of the stack —
        never both — and returns focus to the callsign field so the operator can
        Tab straight on. The per-row × discards without loading, for a call they
        have decided not to work.
    */
    import { callsignStack } from './callsignStack.svelte';
    import { draft } from './qso.svelte';
    import { focusCallsign, operate, setCallStack } from './state.svelte';

    function pop(index: number): void {
        const call = callsignStack.popAt(index);
        if (call === undefined) return;
        draft.callsign = call;
        focusCallsign();
    }
</script>

<aside class="pileup-drawer" data-open={operate.callStack} data-list="calls" aria-label="Pile-up">
    <div
        class="flex h-full flex-col border-l border-line bg-surface"
        class:shadow-xl={operate.callStack}
    >
        <div class="flex items-start justify-between px-4 py-4 sm:px-6">
            <div>
                <h2 class="text-base font-semibold text-ink">
                    Pile-up{#if callsignStack.count > 0}<span class="text-muted"
                            >&nbsp;({callsignStack.count})</span
                        >{/if}
                </h2>
                <p class="mt-0.5 text-xs text-muted">
                    Shift+Enter sets one aside · Shift+↑ newest · Shift+↓ oldest
                </p>
            </div>
            <button
                class="cursor-pointer rounded-md text-muted hover:text-ink"
                title="Close"
                onclick={() => setCallStack(false)}
            >
                <span class="sr-only">Close panel</span>
                <svg
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    class="size-5"
                    aria-hidden="true"
                >
                    <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
                </svg>
            </button>
        </div>

        <div class="flex-1 overflow-y-auto px-4 pb-4 sm:px-6">
            {#if callsignStack.items.length === 0}
                <p class="text-sm text-muted">Nothing set aside yet.</p>
            {:else}
                <ul class="flex flex-col gap-1.5">
                    {#each callsignStack.items as call, index (call)}
                        <li class="flex items-center overflow-hidden rounded-md border border-line">
                            <button
                                type="button"
                                class="flex-1 cursor-pointer truncate px-2 py-1 text-left font-mono text-sm text-ink hover:bg-surface-muted"
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
            {/if}
        </div>

        {#if callsignStack.items.length > 0}
            <div class="border-t border-line px-4 py-3 sm:px-6">
                <button
                    type="button"
                    class="cursor-pointer text-xs text-muted hover:text-ink"
                    aria-label="Discard all stacked callsigns"
                    onclick={() => callsignStack.clear()}>Discard all</button
                >
            </div>
        {/if}
    </div>
</aside>
