<script lang="ts">
    import type { Ft8Band } from '../../states/ft8.svelte';

    // A horizontal, per-slot view of the audio passband, divided into uniform
    // signal-width slots. FT8 has no standard offset grid — a signal is ~50 Hz
    // wide (8 tones × 6.25 Hz) and can sit at any continuous offset — so this
    // grid is an SM picker convention: split 200–3000 Hz into ~50 Hz slots
    // (≈56), each exactly one signal wide, so a pick can't half-overlap.
    //
    // Overlay (model A): each cell is coloured from the daemon's occupancy —
    // red if any busy band overlaps the cell's 50 Hz span, green if clear — and
    // the daemon's #1 recommendation (a continuous offset) is marked on its
    // nearest cell. The selected slot keeps its occupancy colour and is bracketed
    // by a ▼ above / ▲ below so the pick reads without hiding busy/clear.
    // Clicking a slot sets the TX base offset. RX-safe — selection is inert until
    // the TX controller (ADR 0029 step d/e) consumes it; nothing keys the rig here.
    interface Props {
        passbandLow: number;
        passbandHigh: number;
        signalWidth: number;
        occupied: Ft8Band[];
        recommended: number | null;
        selected: number | null;
        hasSlot: boolean;
        onselect: (hz: number) => void;
    }
    let {
        passbandLow,
        passbandHigh,
        signalWidth,
        occupied,
        recommended,
        selected,
        hasSlot,
        onselect,
    }: Props = $props();

    const span = $derived(Math.max(1, passbandHigh - passbandLow));
    // Guard the slot width and cap the count so a degenerate report can't ask
    // for thousands of cells.
    const slotWidth = $derived(Math.max(1, signalWidth));
    const slotCount = $derived(Math.min(200, Math.max(1, Math.floor(span / slotWidth))));

    // A cell is busy if any occupied band overlaps its [offset, offset+width) span.
    function isBusy(offset: number): boolean {
        const hi = offset + slotWidth;
        return occupied.some((b) => offset < b.high_hz && hi > b.low_hz);
    }

    // One cell per slot, filling the strip edge-to-edge. Base offset = passband
    // low + index × slot width (200, 250, … for 50 Hz).
    const cells = $derived(
        Array.from({ length: slotCount }, (_, i) => {
            const offset = passbandLow + i * slotWidth;
            return { offset, widthPct: 100 / slotCount, busy: isBusy(offset) };
        })
    );

    // Snap a continuous offset to its nearest cell index, or null if out of range.
    function nearestIndex(hz: number | null): number | null {
        if (hz === null) return null;
        const idx = Math.round((hz - passbandLow) / slotWidth);
        return idx < 0 || idx >= slotCount ? null : idx;
    }

    // The daemon's #1 pick (continuous) snapped to its cell offset.
    const recommendedCell = $derived.by(() => {
        const idx = nearestIndex(recommended);
        return idx === null ? null : passbandLow + idx * slotWidth;
    });

    // The selected slot's cell offset, and the % position of that cell's centre
    // (drives the ▼/▲ markers). A chip pick (a non-grid daemon offset) snaps to
    // the nearest cell so the markers still land on the strip.
    const selectedIndex = $derived(nearestIndex(selected));
    const selectedCell = $derived(
        selectedIndex === null ? null : passbandLow + selectedIndex * slotWidth
    );
    const selectedCenterPct = $derived(
        selectedIndex === null ? null : ((selectedIndex + 0.5) / slotCount) * 100
    );

    // Fill + hover for a cell — occupancy only; the selection is shown by the
    // arrows, not by recolouring. Literal classes so Tailwind's JIT emits them.
    function cellClass(busy: boolean): string {
        if (busy) return 'bg-red-600/90 hover:bg-red-700';
        return 'bg-green-500/80 hover:bg-green-600';
    }

    function cellTitle(offset: number, busy: boolean): string {
        const tags = [busy ? 'busy' : 'clear'];
        if (offset === recommendedCell) tags.push('recommended');
        if (offset === selectedCell) tags.push('selected');
        return `TX offset ${offset} Hz — ${tags.join(', ')}`;
    }
</script>

<div class="w-full px-3 py-2">
    <div class="flex items-baseline justify-between text-xs text-gray-400">
        <span>TX Offset — click a slot</span>
        <span class="font-mono text-gray-600">
            {selected !== null ? `${selected} Hz` : 'none selected'}
        </span>
    </div>

    {#if hasSlot}
        <!-- green = clear · red = busy · ▼/▲ bracket your pick · amber underline = daemon recommendation -->
        <div class="relative my-3">
            {#if selectedCenterPct !== null}
                <span
                    class="pointer-events-none absolute bottom-full mb-0.5 -translate-x-1/2 text-[11px] leading-none text-gray-900"
                    style:left="{selectedCenterPct}%"
                    aria-hidden="true">▼</span
                >
            {/if}
            <div class="flex h-8 w-full overflow-hidden rounded border border-gray-400">
                {#each cells as c (c.offset)}
                    <button
                        type="button"
                        class="h-full border-r border-gray-400 last:border-r-0 {cellClass(c.busy)}"
                        class:border-b-2={c.offset === recommendedCell}
                        class:border-b-amber-500={c.offset === recommendedCell}
                        style:width="{c.widthPct}%"
                        title={cellTitle(c.offset, c.busy)}
                        aria-label={`Select TX offset ${c.offset} hertz`}
                        onclick={() => onselect(c.offset)}
                    ></button>
                {/each}
            </div>
            {#if selectedCenterPct !== null}
                <span
                    class="pointer-events-none absolute top-full mt-0.5 -translate-x-1/2 text-[11px] leading-none text-gray-900"
                    style:left="{selectedCenterPct}%"
                    aria-hidden="true">▲</span
                >
            {/if}
        </div>
        <div class="flex justify-between font-mono text-[10px] text-gray-400">
            <span>{passbandLow}</span>
            <span>{passbandHigh} Hz</span>
        </div>
    {:else}
        <p class="mt-1 text-xs text-gray-400">Waiting for slot…</p>
    {/if}
</div>
