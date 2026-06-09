<script lang="ts">
    import { onMount, onDestroy } from 'svelte';
    import Button from '../components/Button.svelte';
    import Ft8OccupancyStrip from './Ft8OccupancyStrip.svelte';
    import { ft8State, startFt8, stopFt8, type Ft8Band } from '../../states/ft8.svelte';
    import { ft8EnrichState, type Ft8CallInfo } from '../../states/ft8Enrich.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { parseCqCall } from '../../utils/ft8Message';
    import { frequencyToBand } from '../../utils/frequency';
    import { formatUtcClock } from '../../utils/time';

    // The occupancy stream is scoped to this view: open while FT8 mode is showing,
    // close on leave (LoggingCard mounts/unmounts this panel with the Operating
    // Mode switch). See ft8.svelte.ts. The enrichment cache is dropped on leave
    // too so a re-open re-resolves against current operating history.
    onMount(startFt8);
    onDestroy(() => {
        stopFt8();
        ft8EnrichState.clear();
    });

    // Current operating band, derived from the selected VFO. The worked-before
    // lookup is band+mode-specific, so the band is part of each enrichment key.
    const opFreq = $derived(
        displayedState.selectedVfo === 'B' ? displayedState.vfoB : displayedState.vfoA
    );
    const band = $derived(frequencyToBand(opFreq));

    // Drive flag + worked-before lookups for the CQ stations on screen. Runs once
    // per slot (when the decode list changes) and on a band change (new keys);
    // observe() dedupes, so re-scanning the visible rows costs nothing after the
    // first sighting of each call. Only CQ lines are enriched (one unambiguous
    // callsign); reply/report lines are left plain for now.
    $effect(() => {
        for (const d of ft8State.decodes) {
            const call = parseCqCall(d.text);
            if (call) ft8EnrichState.observe(call, band);
        }
    });

    // SNR formatted WSJT-X-style: an explicit-sign integer dB ("+04", "-13").
    function formatSnr(snr: number): string {
        return (snr >= 0 ? '+' : '-') + String(Math.abs(snr)).padStart(2, '0');
    }

    // Tint colour for a CQ row: the attention colour when the station is new on
    // this band, the muted colour when worked before, and no override (default
    // text colour) while the worked answer is still pending or for non-CQ rows.
    function rowColor(info: Ft8CallInfo | undefined): string | undefined {
        if (!info || info.worked === undefined) return undefined;
        return info.worked ? ft8EnrichState.colorWorked : ft8EnrichState.colorUnworked;
    }

    // Slot label: "14:30:15 · odd · 19 busy", or a waiting state before the first
    // event. start_utc is RFC3339 UTC; render its UTC wall-clock with seconds so
    // the four 15 s slots per minute are distinguishable.
    const slotLabel = $derived.by(() => {
        if (!ft8State.slot) return 'Waiting for slot…';
        const clock = formatUtcClock(new Date(ft8State.slot.start_utc));
        return `${clock} · ${ft8State.slot.period} · ${ft8State.busyCount} busy`;
    });

    // The daemon sends `suggested` best-first by rank. For display we sort a copy
    // ascending by frequency so the chips read left-to-right like a band, and mark
    // the daemon's #1 (suggested[0], pre-sort) with a ★. The rank order stays on
    // the wire for step (e)'s "give me the best slot". Colour-fill is reserved for
    // the future selected-offset state so it doesn't clash with this recommendation
    // marker.
    const topPick = $derived(ft8State.suggested[0] ?? null);
    const sortedOffsets = $derived([...ft8State.suggested].sort((a, b) => a - b));

    // Diagnostic label for an occupied band: its detection source, plus the
    // normalised peak level for energy-derived bands. A weak `energy 0.06` mark
    // on a frequency WSJT-X shows as clear points at a threshold false-positive;
    // `decode` means a real signal was decoded there. (Temporary validation
    // view — the TX Frequency panel becomes the TX picker at step e.)
    function occupiedLabel(b: Ft8Band): string {
        if (b.source === 'decode') return 'decode';
        const lvl = b.level !== undefined ? ` ${b.level.toFixed(2)}` : '';
        return `${b.source ?? '?'}${lvl}`;
    }
</script>

<!--
    FT8 operating-mode panel. Per the cards-vs-panels convention this is a
    content panel inside LoggingCard, shown when the header's Operating Mode
    switch is set to "FT8" (Phone/CW renders QsoPanel + CountryPanel + InfoPanel
    instead).

    Step (a) of the FT8-TX work (ADR 0029) wires the per-slot occupancy readout:
    Band Activity shows the current slot + how many signals are busy; TX
    Frequency lists the daemon's clear base offsets (frequency-sorted, ★ = the
    daemon's top-ranked pick). Read-only for now — clicking a clear offset to set
    the TX frequency arrives with step (e), when there is a transmitter to point.
    The live decode list (per-slot freq / DT / text) still fills the Band
    Activity body later.
-->
<div class="flex justify-center h-111 text-gray-500 space-x-3">
    <div class="flex flex-col text-center">
        <h2 class="text-base font-semibold my-2">Main Freq</h2>
        <div class="flex flex-col place-items-center px-2 space-y-1">
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
    <div class="flex flex-col text-center w-92">
        <h2 class="text-base font-semibold my-2">Band Activity</h2>
        <div class="flex h-95.5 flex-col rounded border border-gray-300 overflow-y-scroll">
            {#if ft8State.decodes.length > 0}
                <ul class="flex-1 space-y-0.5 px-2 py-1 text-left font-mono text-xs">
                    {#each ft8State.decodes as d (d.id)}
                        {@const cqCall = parseCqCall(d.text)}
                        {@const info = cqCall ? ft8EnrichState.info(cqCall, band) : undefined}
                        <li class="flex gap-2 whitespace-nowrap">
                            <span class="text-gray-400">{formatUtcClock(new Date(d.startUtc))}</span
                            >
                            <span class="w-7 text-right text-gray-500">{formatSnr(d.snr)}</span>
                            <span class="w-10 text-right text-gray-500">{Math.round(d.freqHz)}</span
                            >
                            {#if info?.flag}
                                <span aria-hidden="true">{info.flag}</span>
                            {/if}
                            <span class="truncate text-gray-700" style:color={rowColor(info)}>{d.text}</span>
                        </li>
                    {/each}
                </ul>
            {:else}
                <p class="mt-1 text-xs">Waiting for decodes…</p>
            {/if}
        </div>
        <div class="mt-0.5 text-gray-700 text-xs">{slotLabel}</div>
    </div>
    <div class="flex flex-col text-center w-92">
        <h2 class="text-base font-semibold my-2">TX Frequency</h2>
        <div class="flex h-95.5 flex-col rounded border border-gray-300">
            <p class="mt-1 text-xs text-gray-400">Occupied (Hz) — validation view</p>
            {#if ft8State.occupied.length > 0}
                <ul
                    class="flex-1 space-y-0.5 overflow-y-auto px-2 py-1 text-left font-mono text-xs"
                >
                    {#each ft8State.occupied as b (b.low_hz)}
                        <li class="flex gap-2 whitespace-nowrap">
                            <span class="w-20 text-right text-gray-600">{b.low_hz}–{b.high_hz}</span
                            >
                            <span class="text-gray-400">{occupiedLabel(b)}</span>
                        </li>
                    {/each}
                </ul>
            {:else if ft8State.slot}
                <p class="mt-1 text-xs">Nothing occupied.</p>
            {:else}
                <p class="mt-1 text-xs">Waiting…</p>
            {/if}
        </div>
    </div>
    <div class="flex flex-col text-center w-20">
        <h2 class="pt-1.5 text-xs font-semibold my-2">Clear Offsets</h2>
        {#if sortedOffsets.length > 0}
            <div class="flex flex-col place-items-center px-2 space-y-2">
                {#each sortedOffsets as offset (offset)}
                    <button
                        type="button"
                        class="w-17 rounded bg-gray-100 px-2 py-0.5 font-mono text-sm text-left text-gray-700"
                        class:ring-2={offset === ft8State.selectedOffset}
                        class:ring-green-700={offset === ft8State.selectedOffset}
                        title={offset === topPick
                            ? 'Daemon’s top-ranked clear offset — click to select for TX'
                            : 'Click to select for TX'}
                        onclick={() => ft8State.selectOffset(offset)}
                    >
                        {offset}{#if offset === topPick}&nbsp;*{/if}
                    </button>
                {/each}
            </div>
        {:else if ft8State.slot}
            <p class="mt-1 text-xs">Band is full.</p>
        {:else}
            <p class="mt-1 text-xs">Waiting…</p>
        {/if}
    </div>
</div>
<!--
    TX-offset picker: a spatial view of the same clear offsets the Clear Slots
    column lists, laid out across the passband against the busy bands. Clicking a
    marker (or a chip) only sets the selected TX base offset — it is inert until
    the TX controller (ADR 0029 step d/e) keys the rig. Both surfaces drive the
    one `ft8State.selectedOffset`.
-->
<div class="m-0 border-t border-gray-300">
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
</div>
