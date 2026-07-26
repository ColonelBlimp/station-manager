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

    // Which parity's occupancy is on show. The panel keeps a per-parity snapshot and
    // shows the one matching the slot you'll TRANSMIT in (opposite the worked station)
    // during a QSO — occupancyParityLocked — else the operator's manual Even/Odd pick.
    const shown = $derived(ft8State.shownParity);
    const locked = $derived(ft8State.occupancyParityLocked);
</script>

<section class="flex h-full flex-col overflow-hidden rounded-xl border border-line bg-surface">
    <div class="flex items-center justify-between border-b border-line px-4 py-2">
        <div class="flex items-center gap-2">
            <h3 class="text-sm font-semibold text-ink">Occupancy</h3>
            {#if locked}
                <!-- During a QSO the view is forced to the TX slot (opposite the worked
                     station); the manual toggle is replaced by this read-only cue. -->
                <span
                    class="text-xs font-medium text-focus"
                    title="Showing the slot you transmit in — the opposite of the station you're working. Set automatically for the duration of the QSO."
                    >{shown} · TX</span
                >
            {:else}
                <!-- Idle: label the toggle so its purpose is obvious — it's the slot the
                     operator will TRANSMIT in, not an abstract parity switch. During a
                     QSO it sets itself (above); here it's for pre-scouting / Call CQ. -->
                <span
                    class="text-xs text-muted"
                    title="The slot you'll transmit in. It sets itself while you're working a station (the opposite slot); pick it here to pre-scout a clear offset, or to match the slot you'll Call CQ in."
                    >TX slot</span
                >
                <div
                    class="inline-flex overflow-hidden rounded-md border border-line text-xs"
                    role="group"
                    aria-label="Occupancy slot parity"
                >
                    <button
                        type="button"
                        class="cursor-pointer px-2 py-0.5 {shown === 'even'
                            ? 'bg-focus text-white'
                            : 'text-muted hover:bg-surface-muted'}"
                        aria-pressed={shown === 'even'}
                        onclick={() => ft8State.setOccupancyParity('even')}>Even</button
                    >
                    <button
                        type="button"
                        class="cursor-pointer border-l border-line px-2 py-0.5 {shown === 'odd'
                            ? 'bg-focus text-white'
                            : 'text-muted hover:bg-surface-muted'}"
                        aria-pressed={shown === 'odd'}
                        onclick={() => ft8State.setOccupancyParity('odd')}>Odd</button
                    >
                </div>
            {/if}
        </div>
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
                hasSlot={ft8State.hasOccupancy}
                emptyReason={ft8State.occupancyEmptyReason}
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
                hasSlot={ft8State.hasOccupancy}
                emptyReason={ft8State.occupancyEmptyReason}
                onselect={(hz: number) => ft8State.selectOffset(hz)}
            />
        {/if}
    </div>
</section>
