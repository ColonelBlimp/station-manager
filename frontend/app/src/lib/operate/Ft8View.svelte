<script lang="ts">
    // FT8 operating surface (ADR 0047) — the three-anchor fixed layout: Band
    // Activity + Operate side-by-side up top, Occupancy (TX-offset picker) full-
    // width across the bottom.
    import { onMount } from 'svelte';
    import { startFt8, stopFt8 } from './ft8.svelte';
    import { ft8EnrichState } from './ft8Enrich.svelte';
    import Ft8BandActivity from './Ft8BandActivity.svelte';
    import Ft8Operate from './Ft8Operate.svelte';
    import Ft8Occupancy from './Ft8Occupancy.svelte';

    // View-scoped stream lifecycle: open on mount, close on destroy, so the
    // daemon holds the capture device only while the FT8 view is shown. The
    // enrichment cache clears too, so a re-open starts clean.
    onMount(() => {
        startFt8();
        return () => {
            stopFt8();
            ft8EnrichState.clear();
        };
    });
</script>

<div class="ft8-grid">
    <div style="grid-area:ba; min-height:0"><Ft8BandActivity /></div>
    <div style="grid-area:op; min-height:0"><Ft8Operate /></div>
    <div style="grid-area:occ; min-height:0"><Ft8Occupancy /></div>
</div>

<style>
    .ft8-grid {
        display: grid;
        gap: 0.75rem;
        /* Band Activity (510px) is a touch wider than Operate (470px) for the
           decode table; both are centred by the 1fr gutter columns on either side.
           Occupancy spans ALL four tracks, so it stays full width edge-to-edge
           below them. Narrower viewports scroll via the main container (the 1fr
           gutters collapse to 0 first). */
        grid-template-columns: 1fr 510px 470px 1fr;
        grid-template-rows: minmax(0, 1fr) 180px;
        grid-template-areas:
            '. ba op .'
            'occ occ occ occ';
        height: calc(100vh - 8rem);
    }
</style>
