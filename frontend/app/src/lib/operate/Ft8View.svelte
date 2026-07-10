<script lang="ts">
    // FT8 operating surface (ADR 0047) — the three-anchor fixed layout: Band
    // Activity + Operate side-by-side up top, Occupancy full-width across the
    // bottom. This increment wires Band Activity live; Operate + Occupancy are
    // stubs filled in following increments.
    import { onMount } from 'svelte';
    import { startFt8, stopFt8 } from './ft8.svelte';
    import { ft8EnrichState } from './ft8Enrich.svelte';
    import Ft8BandActivity from './Ft8BandActivity.svelte';
    import Ft8Operate from './Ft8Operate.svelte';

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
    <div style="grid-area:occ" class="ft8-stub">Occupancy — next increment</div>
</div>

<style>
    .ft8-grid {
        display: grid;
        gap: 0.75rem;
        grid-template-columns: minmax(470px, 1fr) 470px;
        grid-template-rows: minmax(0, 1fr) 180px;
        grid-template-areas:
            'ba op'
            'occ occ';
        height: calc(100vh - 8rem);
    }
    .ft8-stub {
        display: flex;
        align-items: center;
        justify-content: center;
        border: 1px dashed var(--color-line);
        border-radius: 0.75rem;
        color: var(--color-muted);
        font-size: 0.8rem;
    }
</style>
