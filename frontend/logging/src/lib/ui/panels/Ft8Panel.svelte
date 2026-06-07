<script lang="ts">
    import { onMount, onDestroy } from 'svelte';
    import Button from '../components/Button.svelte';
    import { ft8State, startFt8, stopFt8 } from '../../states/ft8.svelte';
    import { formatUtcClock } from '../../utils/time';

    // The occupancy stream is scoped to this view: open while FT8 mode is showing,
    // close on leave (LoggingCard mounts/unmounts this panel with the Operating
    // Mode switch). See ft8.svelte.ts.
    onMount(startFt8);
    onDestroy(stopFt8);

    // Slot label: "14:30:15 · odd · 19 busy", or a waiting state before the first
    // event. start_utc is RFC3339 UTC; render its UTC wall-clock with seconds so
    // the four 15 s slots per minute are distinguishable.
    const slotLabel = $derived.by(() => {
        if (!ft8State.slot) return 'Waiting for slot…';
        const clock = formatUtcClock(new Date(ft8State.slot.start_utc));
        return `${clock} · ${ft8State.slot.period} · ${ft8State.busyCount} busy`;
    });
</script>

<!--
    FT8 operating-mode panel. Per the cards-vs-panels convention this is a
    content panel inside LoggingCard, shown when the header's Operating Mode
    switch is set to "FT8" (Phone/CW renders QsoPanel + CountryPanel + InfoPanel
    instead).

    Step (a) of the FT8-TX work (ADR 0029) wires the per-slot occupancy readout:
    Band Activity shows the current slot + how many signals are busy; TX
    Frequency lists the daemon's ranked clear base offsets. Read-only for now —
    clicking a clear offset to set the TX frequency arrives with step (e), when
    there is a transmitter to point. The live decode list (per-slot freq / DT /
    text) still fills the Band Activity body later.
-->
<div class="flex justify-center h-115 text-gray-500 mt-4 space-x-2">
    <div class="text-center border w-101">
        <h2 class="text-lg font-semibold">Band Activity</h2>
        <p class="mt-1 text-sm">{slotLabel}</p>
        <p class="mt-1 text-xs">Live decode view coming soon.</p>
    </div>
    <div class="text-center border w-101">
        <h2 class="text-lg font-semibold">TX Frequency</h2>
        {#if ft8State.suggested.length > 0}
            <p class="mt-1 text-sm">Clear offsets (Hz):</p>
            <div class="mt-1 flex flex-wrap justify-center gap-1 px-2">
                {#each ft8State.suggested as offset (offset)}
                    <span class="rounded bg-gray-100 px-2 py-0.5 font-mono text-sm text-gray-700">
                        {offset}
                    </span>
                {/each}
            </div>
        {:else if ft8State.slot}
            <p class="mt-1 text-sm">No clear offsets — band is full.</p>
        {:else}
            <p class="mt-1 text-sm">Waiting for slot…</p>
        {/if}
    </div>
    <div class="flex flex-col w-40 h-115 place-items-center px-4 space-y-2">
        <Button id="160m" label="160m" />
        <Button id="80m" label="80m" />
        <Button id="60m" label="60m" />
        <Button id="40m" label="40m" />
        <Button id="30m" label="30m" />
        <Button id="20m" label="20m" />
        <Button id="18m" label="18m" />
        <Button id="15m" label="15m" />
        <Button id="12m" label="12m" />
        <Button id="10m" label="10m" />
        <Button id="6m" label="6m" />
    </div>
</div>
