<script lang="ts">
    import Ft8OccupancyStrip from './Ft8OccupancyStrip.svelte';
    import Ft8OccupancySpectrum from './Ft8OccupancySpectrum.svelte';
    import { ft8State } from '../../states/ft8.svelte';

    // Daemon's #1 ranked clear offset (suggested[0], pre-sort) — both views mark
    // it as the recommendation (★ in spectrum, underline in the strip).
    const topPick = $derived(ft8State.suggested[0] ?? null);
</script>

<!--
    TX-offset picker. Two switchable presentations of the SAME per-slot occupancy
    snapshot, both writing ft8State.selectedOffset (inert until the TX controller keys):
      - Channels — the discrete ~50 Hz red/green strip (the frictionless "pick a green
        one" default).
      - Spectrum — a continuous bar: signals at their true positions, click-anywhere,
        graded clear/near/sharing. Matches how FT8 actually works (continuous, overlap-
        tolerant) + how the Clear Offsets list presents recommendations. The view choice
        is operating state (ft8State.occupancyView, persisted per device).
-->
<div class="pt-2 ft8-info-panel-height">
    <!-- View toggle -->
    <div class="flex justify-end px-3 h-6">
        <div class="inline-flex overflow-hidden rounded border border-gray-300 text-xs">
            <button
                type="button"
                class="px-2 py-0.5 {ft8State.occupancyView === 'channels'
                    ? 'bg-indigo-600 text-white'
                    : 'cursor-pointer bg-white text-gray-600 hover:bg-gray-50'}"
                aria-pressed={ft8State.occupancyView === 'channels'}
                onclick={() => ft8State.setOccupancyView('channels')}>Channels</button
            >
            <button
                type="button"
                class="border-l border-gray-300 px-2 py-0.5 {ft8State.occupancyView === 'spectrum'
                    ? 'bg-indigo-600 text-white'
                    : 'cursor-pointer bg-white text-gray-600 hover:bg-gray-50'}"
                aria-pressed={ft8State.occupancyView === 'spectrum'}
                onclick={() => ft8State.setOccupancyView('spectrum')}>Spectrum</button
            >
        </div>
    </div>
    <div class="h-30">
    {#if ft8State.occupancyView === 'spectrum'}
        <Ft8OccupancySpectrum
            passbandLow={ft8State.passbandLow}
            passbandHigh={ft8State.passbandHigh}
            signalWidth={ft8State.signalWidth}
            occupied={ft8State.occupied}
            suggested={ft8State.suggested}
            recommended={topPick}
            selected={ft8State.selectedOffset}
            hasSlot={ft8State.slot !== null}
            onselect={(hz: number) => ft8State.selectOffset(hz)}
            onpreview={(hz: number) => ft8State.previewOffset(hz)}
        />
    {:else}
        <Ft8OccupancyStrip
            passbandLow={ft8State.passbandLow}
            passbandHigh={ft8State.passbandHigh}
            signalWidth={ft8State.signalWidth}
            occupied={ft8State.occupied}
            recommended={topPick}
            selected={ft8State.selectedOffset}
            hasSlot={ft8State.slot !== null}
            onselect={(hz: number) => ft8State.selectOffset(hz)}
        />
    {/if}
    </div>
</div>
