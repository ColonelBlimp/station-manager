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
    // nearest cell. Clicking a slot sets the TX base offset (white). RX-safe —
    // selection is inert until the TX controller (ADR 0029 step d/e) consumes
    // it; nothing keys the rig here.
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

    // The daemon's #1 pick is a continuous offset; snap it to the nearest cell.
    const recommendedCell = $derived.by(() => {
        if (recommended === null) return null;
        const idx = Math.min(
            slotCount - 1,
            Math.max(0, Math.round((recommended - passbandLow) / slotWidth))
        );
        return passbandLow + idx * slotWidth;
    });

    // Fill + hover for a cell. Selected wins (white, ringed) so the operator's
    // pick stands out over the green/red occupancy. All classes are literal so
    // Tailwind's JIT emits them.
    function cellClass(offset: number, busy: boolean): string {
        if (offset === selected) return 'bg-white ring-2 ring-inset ring-gray-900';
        if (busy) return 'bg-red-600/90 hover:bg-red-700';
        return 'bg-green-500/80 hover:bg-green-600';
    }

    function cellTitle(offset: number, busy: boolean): string {
        const tags = [busy ? 'busy' : 'clear'];
        if (offset === recommendedCell) tags.push('recommended');
        if (offset === selected) tags.push('selected');
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
        <!-- green = clear · red = busy · white = your pick · amber underline = daemon recommendation -->
        <div class="relative mt-1 flex h-8 w-full overflow-hidden rounded border border-gray-400">
            {#each cells as c (c.offset)}
                <button
                    type="button"
                    class="h-full border-r border-gray-400 last:border-r-0 {cellClass(
                        c.offset,
                        c.busy
                    )}"
                    class:border-b-2={c.offset === recommendedCell}
                    class:border-b-amber-500={c.offset === recommendedCell}
                    style:width="{c.widthPct}%"
                    title={cellTitle(c.offset, c.busy)}
                    aria-label={`Select TX offset ${c.offset} hertz`}
                    onclick={() => onselect(c.offset)}
                ></button>
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
