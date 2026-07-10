<script lang="ts">
    // TX-offset picker (ADR 0047 Occupancy anchor). Two switchable presentations of
    // the SAME per-slot occupancy snapshot, both writing ft8State.selectedOffset:
    //   - Channels — the discrete ~50 Hz strip (frictionless "pick a green one").
    //   - Spectrum — a continuous bar, click-anywhere, graded clear/near/sharing.
    // Picking pins the offset; until then TX uses the daemon's top clear offset
    // (ft8State.effectiveOffset's auto fallback), whose recommendation marker hops
    // slot-to-slot on a busy band — deliberately kept visible so it can be judged.
    import { ft8State } from './ft8.svelte';
    import Ft8OccupancyStrip from './Ft8OccupancyStrip.svelte';
    import Ft8OccupancySpectrum from './Ft8OccupancySpectrum.svelte';

    // Daemon's #1 ranked clear offset (suggested[0]) — both views mark it (★ /
    // amber underline) as the recommendation the auto fallback would use.
    const topPick = $derived(ft8State.suggested[0] ?? null);
</script>

<section class="flex h-full flex-col overflow-hidden rounded-xl border border-line bg-surface">
    <div class="flex items-center justify-between border-b border-line px-4 py-2">
        <h3 class="text-sm font-semibold text-ink">Occupancy</h3>
        <div class="inline-flex overflow-hidden rounded-md border border-line text-xs">
            <button
                type="button"
                class="cursor-pointer px-2 py-0.5 {ft8State.occupancyView === 'channels'
                    ? 'bg-focus text-white'
                    : 'text-muted hover:bg-surface-muted'}"
                aria-pressed={ft8State.occupancyView === 'channels'}
                onclick={() => ft8State.setOccupancyView('channels')}>Channels</button
            >
            <button
                type="button"
                class="cursor-pointer border-l border-line px-2 py-0.5 {ft8State.occupancyView ===
                'spectrum'
                    ? 'bg-focus text-white'
                    : 'text-muted hover:bg-surface-muted'}"
                aria-pressed={ft8State.occupancyView === 'spectrum'}
                onclick={() => ft8State.setOccupancyView('spectrum')}>Spectrum</button
            >
        </div>
    </div>

    <div class="min-h-0 flex-1">
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
</section>
