<script lang="ts">
    // The card-below the logging card. Shows whichever info panel the rail has
    // active (Worked/Session/Details/Rig). Placeholder content for now — Worked's
    // worked-before data, the Rig fields + CAT gate, etc. come in later passes.
    import { operate, closePanel } from './state.svelte';

    const titles: Record<string, string> = {
        worked: 'Worked',
        session: 'Session',
        details: 'Details',
        rig: 'Rig',
    };
</script>

{#if operate.panel}
    <div class="card mt-4 w-[42rem]">
        <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-ink">{titles[operate.panel]}</h3>
            <button
                class="cursor-pointer rounded-md text-muted hover:text-ink"
                title="Close"
                onclick={closePanel}
            >
                <span class="sr-only">Close panel</span>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true" class="size-5">
                    <path d="M6 18 18 6M6 6l12 12" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
            </button>
        </div>
        <div class="mt-3 h-40 rounded-lg border border-dashed border-line"></div>
    </div>
{/if}
