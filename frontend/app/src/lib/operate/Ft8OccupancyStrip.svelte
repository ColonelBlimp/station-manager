<script lang="ts">
    import type { Ft8Band } from '../api/ft8-sse';

    // Channelised occupancy view — the audio passband split into uniform, one-
    // signal-wide slots (~50 Hz). FT8 has no standard offset grid (a signal is 8
    // tones × 6.25 Hz ≈ 50 Hz and can sit anywhere), so this grid is an SM picker
    // convention: each cell is exactly one signal wide, so a pick can't half-overlap
    // a neighbour. A cell is red if any busy band overlaps its span, green if clear;
    // the daemon's #1 recommendation (a continuous offset) is underlined on its
    // nearest cell — watching it hop cell-to-cell each slot is the point on a busy
    // band. The selected slot is bracketed ▼/▲ so the pick reads without hiding
    // busy/clear. RX-safe: selection is inert until the TX controller keys the rig.
    interface Props {
        passbandLow: number;
        passbandHigh: number;
        signalWidth: number;
        occupied: Ft8Band[];
        recommended: number | null;
        selected: number | null;
        hasSlot: boolean;
        /** Why the panel is empty when hasSlot is false — drives the copy below. */
        emptyReason: '' | 'waiting' | 'tx-parity';
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
        emptyReason,
        onselect,
    }: Props = $props();

    const span = $derived(Math.max(1, passbandHigh - passbandLow));
    // Guard the slot width and cap the count so a degenerate report can't ask for
    // thousands of cells.
    const slotWidth = $derived(Math.max(1, signalWidth));
    const slotCount = $derived(Math.min(200, Math.max(1, Math.floor(span / slotWidth))));

    // A cell is busy if any occupied band overlaps its [offset, offset+width) span.
    function isBusy(offset: number): boolean {
        const hi = offset + slotWidth;
        return occupied.some((b) => offset < b.high_hz && hi > b.low_hz);
    }

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

    // The selected slot's cell offset + the % centre of that cell (drives ▼/▲).
    const selectedIndex = $derived(nearestIndex(selected));
    const selectedCell = $derived(
        selectedIndex === null ? null : passbandLow + selectedIndex * slotWidth
    );
    const selectedCenterPct = $derived(
        selectedIndex === null ? null : ((selectedIndex + 0.5) / slotCount) * 100
    );

    // Occupancy fill only; the selection shows via the arrows, not by recolouring.
    // Literal classes so Tailwind's scanner emits them; both read on light + dark.
    function cellClass(busy: boolean): string {
        return busy
            ? 'bg-red-500/65 hover:bg-red-500/80 dark:bg-red-500/55 dark:hover:bg-red-500/75'
            : 'bg-green-700/75 hover:bg-green-700/90 dark:bg-green-400/65 dark:hover:bg-green-400/85';
    }

    function cellTitle(offset: number, busy: boolean): string {
        const tags = [busy ? 'busy' : 'clear'];
        if (offset === recommendedCell) tags.push('recommended');
        if (offset === selectedCell) tags.push('selected');
        return `TX offset ${offset} Hz — ${tags.join(', ')}`;
    }
</script>

<div class="flex h-full w-full flex-col px-4 py-2">
    <div class="mb-2 flex items-baseline justify-between text-xs text-muted">
        <span>TX Offset — click a channel</span>
        <span class="font-mono">
            {selected !== null ? `${selected} Hz` : 'auto — daemon pick'}
        </span>
    </div>

    {#if hasSlot}
        <!-- green = clear · red = busy · ▼/▲ = your pick · amber underline = daemon recommendation -->
        <div class="relative my-3">
            {#if selectedCenterPct !== null}
                <span
                    class="pointer-events-none absolute bottom-full mb-0.5 -translate-x-1/2 text-[11px] leading-none text-ink"
                    style:left="{selectedCenterPct}%"
                    aria-hidden="true">▼</span
                >
            {/if}
            <div class="flex h-11 w-full overflow-hidden rounded border border-line">
                {#each cells as c (c.offset)}
                    <button
                        type="button"
                        class="h-full cursor-pointer border-r border-black/10 last:border-r-0 dark:border-white/10 {cellClass(
                            c.busy
                        )}"
                        class:border-b-2={c.offset === recommendedCell}
                        class:border-b-amber-400={c.offset === recommendedCell}
                        style:width="{c.widthPct}%"
                        title={cellTitle(c.offset, c.busy)}
                        aria-label={`Select TX offset ${c.offset} hertz`}
                        onclick={() => onselect(c.offset)}
                    ></button>
                {/each}
            </div>
            {#if selectedCenterPct !== null}
                <span
                    class="pointer-events-none absolute top-full mt-0.5 -translate-x-1/2 text-[11px] leading-none text-ink"
                    style:left="{selectedCenterPct}%"
                    aria-hidden="true">▲</span
                >
            {/if}
        </div>
        <div class="mt-auto flex justify-between font-mono text-[10px] text-muted">
            <span>{passbandLow} Hz</span>
            <span>{passbandHigh} Hz</span>
        </div>
    {:else if emptyReason === 'tx-parity'}
        <!-- The trap this replaced: a bare "Waiting for slot…" here implied data was
             imminent, but the panel is locked to the parity we TRANSMIT in and the
             daemon skips occupancy for a slot we transmitted in — so during a CQ run
             it would never arrive and the operator waits forever (dogfood 2026-07-26). -->
        <p class="mt-1 text-xs text-muted">
            No reading for your <span class="text-ink">transmit</span> slot — SM can't listen while
            it transmits.
            <br />Pause TX for one slot and this fills.
        </p>
    {:else}
        <p class="mt-1 text-xs text-muted">Waiting for slot…</p>
    {/if}
</div>
