<script lang="ts">
    // The card-below the logging card. Shows whichever info panel the rail has
    // active (Worked/Session/Details/Rig). All four are live (on stubbed data
    // seams until the /v1 wiring).
    import { operate, closePanel } from './state.svelte';
    import { rig, rigGate } from './rig.svelte';
    import WorkedPanel from './WorkedPanel.svelte';
    import SessionPanel from './SessionPanel.svelte';
    import DetailsPanel from './DetailsPanel.svelte';
    import RigPanel from './RigPanel.svelte';

    const titles: Record<string, string> = {
        worked: 'Worked',
        session: 'Session',
        details: 'Details',
        rig: 'Rig',
    };
</script>

{#if operate.panel}
    <!-- Centred on the logging card's axis: operate-center anchors children
         to the logging card's LEFT edge, so a wider card must overhang
         symmetrically — half the width difference as a negative left margin.
         (Falls back to 0 outside operate-center, e.g. the FT8 branch, via
         the var() default.) Keep the 42rem here and in w-[42rem] in step. -->
    <div class="card mt-4 w-[42rem] ml-[calc((var(--card-w,42rem)-42rem)/2)]">
        <div class="flex items-center justify-between">
            <div class="flex items-center gap-x-3">
                <h3 class="text-sm font-semibold text-ink">{titles[operate.panel]}</h3>
                {#if operate.panel === 'rig'}
                    {#if rig.identity !== ''}
                        <span class="text-sm text-muted">{rig.identity}</span>
                    {/if}
                    <span
                        class="flex items-center gap-x-1.5 rounded-full bg-surface-muted px-2.5 py-1 text-xs font-medium text-ink"
                    >
                        <span
                            class="size-2 rounded-full"
                            class:bg-green-500={rigGate() === 'live'}
                            class:bg-gray-400={rigGate() === 'manual'}
                            class:bg-amber-500={rigGate() === 'unconfirmed'}
                            class:bg-red-500={rigGate() === 'lost'}
                        ></span>
                        {rigGate() === 'live'
                            ? 'CAT connected'
                            : rigGate() === 'manual'
                              ? 'Manual — confirmed'
                              : rigGate() === 'unconfirmed'
                                ? 'Manual — confirm to log'
                                : 'CAT link lost'}
                    </span>
                {/if}
            </div>
            <button
                class="cursor-pointer rounded-md text-muted hover:text-ink"
                title="Close"
                onclick={closePanel}
            >
                <span class="sr-only">Close panel</span>
                <svg
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    aria-hidden="true"
                    class="size-5"
                >
                    <path d="M6 18 18 6M6 6l12 12" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
            </button>
        </div>
        <div class="mt-3">
            {#if operate.panel === 'worked'}
                <WorkedPanel />
            {:else if operate.panel === 'session'}
                <SessionPanel />
            {:else if operate.panel === 'details'}
                <DetailsPanel />
            {:else}
                <RigPanel />
            {/if}
        </div>
    </div>
{/if}
