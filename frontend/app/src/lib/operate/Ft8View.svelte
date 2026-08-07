<script lang="ts">
    // FT8 operating surface (ADR 0047) — the three-anchor fixed layout: Band
    // Activity + Operate side-by-side up top, Occupancy (TX-offset picker) full-
    // width across the bottom.
    import { onMount } from 'svelte';
    import { ft8State, startFt8, stopFt8 } from './ft8.svelte';
    import { ft8EnrichState } from './ft8Enrich.svelte';
    import { rig } from './rig.svelte';
    import Ft8BandActivity from './Ft8BandActivity.svelte';
    import Ft8Operate from './Ft8Operate.svelte';
    import Ft8Occupancy from './Ft8Occupancy.svelte';
    import AudioLevelCard from './AudioLevelCard.svelte';
    import TxDriveChip from './TxDriveChip.svelte';

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

    // Band-change watcher (dogfood niggle 2026-07-19): crossing a band boundary
    // clears the Band Activity feed (and, on a genuine band-to-band change, the
    // pile-up queue). The transition logic lives in ft8State.noteOperatingBand
    // so it's unit-tested there; this effect just feeds it the rig band.
    $effect(() => ft8State.noteOperatingBand(rig.band));
</script>

<div class="ft8-grid relative">
    <div style="grid-area:ba; min-height:0"><Ft8BandActivity /></div>
    <div style="grid-area:op; min-height:0"><Ft8Operate /></div>
    <div style="grid-area:occ; min-height:0"><Ft8Occupancy /></div>

    <!-- RX audio-level instrument — FT8-only: capture runs only while this
         view is open, so the meter lives and dies with it. Anchored to the
         grid's bottom-left JUST ABOVE the Occupancy panel (operator,
         2026-08-06 — bottom-of-viewport sat ON the panel): the offset is the
         occ row's 180px + the grid's 0.75rem gap, both defined in the style
         block below — change one, change both. The open card grows UPWARD
         from this anchor, over Band Activity, never over Occupancy. -->
    <div class="absolute bottom-[calc(180px+0.75rem)] left-0 z-30 flex flex-col items-start gap-2">
        <!-- ADR 0064: TX-drive (ALC) readout — stacked ABOVE the RX audio
             meter (operator, 2026-08-07: vertical, not horizontal), so the
             audio chip keeps its anchored spot at the bottom. Renders nothing
             until the first poll answer, so non-METERPOLL rigs never see an
             empty shell. -->
        <TxDriveChip />
        <AudioLevelCard />
    </div>
</div>

<style>
    .ft8-grid {
        display: grid;
        gap: 0.75rem;
        /* Band Activity + Operate are a fixed 470px each, centred by the 1fr
           gutter columns on either side. Occupancy spans ALL four tracks, so it
           stays full width edge-to-edge below them. Narrower viewports scroll via
           the main container (the 1fr gutters collapse to 0 first). */
        grid-template-columns: 1fr 470px 470px 1fr;
        grid-template-rows: minmax(0, 1fr) 180px;
        grid-template-areas:
            '. ba op .'
            'occ occ occ occ';
        height: calc(100vh - 8rem);
    }
</style>
