<script lang="ts">
    import type { Ft8Band } from '../../states/ft8.svelte';

    // A horizontal, per-slot view of the audio passband: busy bands shaded, the
    // daemon's clear base offsets drawn as clickable green markers (★ = top
    // pick) sized to the TX signal footprint. Clicking a marker selects that TX
    // base offset. RX-safe — selection is inert until the TX controller (ADR
    // 0029 step d/e) consumes it; nothing keys the rig here. Only the daemon's
    // vetted `suggested` offsets are selectable (the SPA never invents a spot);
    // click-anywhere-with-snap is a later upgrade once daemon-side enforcement
    // lands at step (e).
    interface Props {
        passbandLow: number;
        passbandHigh: number;
        signalWidth: number;
        occupied: Ft8Band[];
        suggested: number[];
        topPick: number | null;
        selected: number | null;
        hasSlot: boolean;
        onselect: (hz: number) => void;
    }
    let {
        passbandLow,
        passbandHigh,
        signalWidth,
        occupied,
        suggested,
        topPick,
        selected,
        hasSlot,
        onselect,
    }: Props = $props();

    // Guard the divisor: a degenerate/empty passband would otherwise NaN every
    // position. The default 200–3000 means this only bites before the first report.
    const span = $derived(Math.max(1, passbandHigh - passbandLow));

    // Frequency → strip position (%), clamped so an offset at/just past an edge
    // still renders on the bar rather than overflowing it.
    function pct(hz: number): number {
        return Math.min(100, Math.max(0, ((hz - passbandLow) / span) * 100));
    }

    // Ascending so markers render left-to-right like a band; keyed by offset.
    const offsets = $derived([...suggested].sort((a, b) => a - b));
</script>

<div class="w-full px-3 py-2">
    <div class="flex items-baseline justify-between text-xs text-gray-400">
        <span>TX Offset — click a clear slot</span>
        <span class="font-mono text-gray-600">
            {selected !== null ? `${selected} Hz` : 'none selected'}
        </span>
    </div>

    {#if hasSlot}
        <div class="relative mt-1 h-8 w-full rounded border border-gray-300 bg-gray-50">
            <!-- Busy bands (shaded, non-interactive). -->
            {#each occupied as b (b.low_hz)}
                <div
                    class="absolute top-0 h-full bg-red-200/70"
                    style:left="{pct(b.low_hz)}%"
                    style:width="{pct(b.high_hz) - pct(b.low_hz)}%"
                ></div>
            {/each}
            <!-- Clear-offset markers (selectable), sized to the TX footprint. -->
            {#each offsets as offset (offset)}
                <button
                    type="button"
                    class="absolute top-0 flex h-full items-start justify-center border-l-2 border-green-600 bg-green-500/40 leading-none hover:bg-green-500/70"
                    class:ring-2={offset === selected}
                    class:ring-inset={offset === selected}
                    class:ring-green-800={offset === selected}
                    style:left="{pct(offset)}%"
                    style:width="{Math.max(1, pct(offset + signalWidth) - pct(offset))}%"
                    title={`Select TX offset ${offset} Hz${
                        offset === topPick ? ' (daemon top pick)' : ''
                    }`}
                    aria-label={`Select TX offset ${offset} hertz`}
                    onclick={() => onselect(offset)}
                >
                    {#if offset === topPick}<span class="text-[10px] text-green-900">★</span>{/if}
                </button>
            {/each}
        </div>
        <div class="flex justify-between font-mono text-[10px] text-gray-400">
            <span>{passbandLow}</span>
            <span>{passbandHigh} Hz</span>
        </div>
    {:else}
        <p class="mt-1 text-xs text-gray-400">Waiting for slot…</p>
    {/if}
</div>
