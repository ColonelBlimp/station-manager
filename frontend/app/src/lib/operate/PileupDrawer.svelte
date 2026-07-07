<script lang="ts">
    // Pile-up drawer — docked slide-over from the right, inboard of the util rail.
    // Starts below the header (top: 4rem) and parks fully off-screen when closed
    // (fixed transform, no rail-width dependency → no flash on rail collapse).
    // The content push is driven by [data-drawer] on <html> (App effect).
    // NOTE (ADR 0044 impl): the ≥2rem "never overlap the logging card, content
    // moves not the drawer" refinement is a later pass — this is the basic docked
    // drawer.
    import { operate, setPileup } from './state.svelte';

    function onKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') setPileup(false);
    }
</script>

<svelte:window onkeydown={onKeydown} />

<aside class="pileup-drawer" data-open={operate.pileup} aria-label="Pile-up">
    <div
        class="flex h-full flex-col border-l border-line bg-surface"
        class:shadow-xl={operate.pileup}
    >
        <div class="flex items-start justify-between px-4 py-4 sm:px-6">
            <h2 class="text-base font-semibold text-ink">Pile-up</h2>
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
            <!-- Stacked calls land here. -->
        </div>
    </div>
</aside>
