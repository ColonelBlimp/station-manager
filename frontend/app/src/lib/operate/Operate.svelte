<script lang="ts">
    // Operate surface — a thin composer. Renders the active sub-mode (Phone/CW ↔
    // FT8, from the router) plus the shared right-rail chrome. The cards are
    // self-contained (ADR 0045); this file only positions them.
    import { router } from '../router.svelte';
    import LoggingCard from './LoggingCard.svelte';
    import UtilRail from './UtilRail.svelte';
    import InfoPanel from './InfoPanel.svelte';
    import PileupDrawer from './PileupDrawer.svelte';
</script>

{#if router.mode === 'phone'}
    <div class="operate-center">
        <LoggingCard />
        <InfoPanel />
    </div>
{:else}
    <div class="mx-auto max-w-3xl">
        <div class="card">
            <h2 class="text-sm font-semibold text-ink">FT8</h2>
            <p class="mt-1 text-sm text-muted">
                Band activity, occupancy / offset picker, and the sequencer ladder — built in a
                later pass.
            </p>
        </div>

        <InfoPanel />
    </div>
{/if}

<!-- Rail + drawer are shown for the whole Operate view (both modes). -->
<UtilRail />
<PileupDrawer />
