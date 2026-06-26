<script lang="ts">
    import type { Ft8Band } from '../../states/ft8.svelte';
    import { signalProximity, offsetFromFraction, clampOffset } from '../../utils/ft8Spectrum';

    // The "Spectrum" occupancy view — a CONTINUOUS presentation of the same per-slot
    // occupancy snapshot the channelised strip uses. Signals render as soft shaded
    // regions at their TRUE positions/widths (not snapped to a grid), you click
    // ANYWHERE to pick a continuous offset (so you can straddle / tuck into a gap),
    // and the pick's status is GRADED — clear / near / sharing — rather than a binary
    // red. This matches how FT8 actually works (continuous, overlap-tolerant) and how
    // the daemon's Clear Offsets list already presents recommendations (true positions,
    // un-channelised). RX-safe: selection is inert until the TX controller consumes it.
    interface Props {
        passbandLow: number;
        passbandHigh: number;
        signalWidth: number;
        occupied: Ft8Band[];
        /** Daemon-vetted clear offsets (continuous), shown as ticks; suggested[0] is ★. */
        suggested: number[];
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
        suggested,
        recommended,
        selected,
        hasSlot,
        onselect,
    }: Props = $props();

    const span = $derived(Math.max(1, passbandHigh - passbandLow));
    // Percent position of a frequency across the passband (0 at low, 100 at high).
    const pct = (hz: number): number => ((hz - passbandLow) / span) * 100;

    // Proximity of the selected footprint to the nearest signal. "Near" margin = one
    // signal width: a signal closer than its own width reads as near, beyond that clear.
    const prox = $derived(
        selected === null ? null : signalProximity(selected, signalWidth, occupied, signalWidth)
    );

    // Footprint tint + caption by proximity — soft, judgment-friendly (no "occupied").
    const footprintClass = $derived(
        prox === null
            ? ''
            : prox.kind === 'sharing'
              ? 'bg-orange-600/40 border-orange-600'
              : prox.kind === 'near'
                ? 'bg-amber-400/50 border-amber-500'
                : 'bg-green-500/40 border-green-600'
    );
    const captionClass = $derived(
        prox === null
            ? 'text-gray-500'
            : prox.kind === 'sharing'
              ? 'text-orange-700'
              : prox.kind === 'near'
                ? 'text-amber-700'
                : 'text-green-700'
    );
    const captionText = $derived.by(() => {
        if (selected === null) return 'none selected';
        if (prox === null) return `${selected} Hz`;
        if (prox.kind === 'sharing') return `${selected} Hz · sharing — usually OK in FT8`;
        if (prox.kind === 'near') return `${selected} Hz · near — signal ~${prox.gapHz} Hz off`;
        return `${selected} Hz · clear`;
    });

    // Click anywhere on the bar → a continuous offset (clamped so the footprint fits).
    function onBarClick(e: MouseEvent & { currentTarget: HTMLElement }): void {
        const rect = e.currentTarget.getBoundingClientRect();
        const frac = (e.clientX - rect.left) / rect.width;
        onselect(offsetFromFraction(frac, passbandLow, passbandHigh, signalWidth));
    }

    // Keyboard: arrows nudge by ~a tenth of a signal width (fine control); Home/End
    // jump to the passband edges. From no selection, start at the daemon's top pick.
    function onBarKey(e: KeyboardEvent): void {
        const step = Math.max(1, Math.round(signalWidth / 10));
        const base = selected ?? recommended ?? passbandLow;
        let next: number;
        switch (e.key) {
            case 'ArrowRight':
            case 'ArrowUp':
                next = base + step;
                break;
            case 'ArrowLeft':
            case 'ArrowDown':
                next = base - step;
                break;
            case 'Home':
                next = passbandLow;
                break;
            case 'End':
                next = passbandHigh - signalWidth;
                break;
            default:
                return;
        }
        e.preventDefault();
        onselect(clampOffset(next, passbandLow, passbandHigh, signalWidth));
    }

    const maxOffset = $derived(Math.max(passbandLow, passbandHigh - signalWidth));
</script>

<div class="w-full px-3 py-2">
    <div class="flex items-baseline justify-between text-xs">
        <span class="text-gray-600">Tx Offset — click anywhere</span>
        <span class="font-mono {captionClass}">{captionText}</span>
    </div>

    {#if hasSlot}
        <!-- soft shading = a signal · green/amber band = your pick (clear/near/sharing) · ▾ = daemon clear offset, ★ = top pick -->
        <div class="relative my-3 pt-3">
            <!-- Suggested clear offsets (continuous, true positions) — visual hints
                 aligned with the Clear Offsets list; the bar handles the actual click. -->
            {#each suggested as s (s)}
                <span
                    class="pointer-events-none absolute top-0 -translate-x-1/2 text-[11px] leading-none {s ===
                    recommended
                        ? 'text-amber-600'
                        : 'text-indigo-400'}"
                    style:left="{pct(s)}%"
                    title={`Daemon clear offset ${s} Hz${s === recommended ? ' (top pick)' : ''}`}
                    aria-hidden="true">{s === recommended ? '★' : '▾'}</span
                >
            {/each}

            <!-- The continuous passband bar: click-anywhere TX-offset picker. -->
            <div
                role="slider"
                tabindex="0"
                aria-label="TX offset (continuous)"
                aria-valuemin={passbandLow}
                aria-valuemax={maxOffset}
                aria-valuenow={selected ?? passbandLow}
                class="relative h-8 w-full cursor-crosshair overflow-hidden rounded border border-gray-400 bg-gray-50 focus:ring-2 focus:ring-indigo-400 focus:outline-none"
                onclick={onBarClick}
                onkeydown={onBarKey}
            >
                <!-- Signals: soft neutral shading at their true positions (not red — the
                     band isn't a wall of "forbidden"; your relationship to it is what's
                     coloured, on the footprint below). -->
                {#each occupied as b (b.low_hz)}
                    <div
                        class="pointer-events-none absolute top-0 h-full bg-slate-400/40"
                        style:left="{pct(b.low_hz)}%"
                        style:width="{((b.high_hz - b.low_hz) / span) * 100}%"
                    ></div>
                {/each}

                <!-- The pick footprint [selected, selected+signalWidth], tinted by proximity. -->
                {#if selected !== null}
                    <div
                        class="pointer-events-none absolute top-0 h-full border-x-2 {footprintClass}"
                        style:left="{pct(selected)}%"
                        style:width="{(signalWidth / span) * 100}%"
                    ></div>
                {/if}
            </div>

            {#if selected !== null}
                <span
                    class="pointer-events-none absolute top-full mt-0.5 -translate-x-1/2 text-[11px] leading-none text-gray-700"
                    style:left="{pct(selected + signalWidth / 2)}%"
                    aria-hidden="true">▲</span
                >
            {/if}
        </div>
        <div class="flex justify-between font-mono text-[10px] text-gray-600">
            <span>{passbandLow} Hz</span>
            <span>{passbandHigh} Hz</span>
        </div>
    {:else}
        <p class="mt-1 text-xs text-gray-400">Waiting for slot…</p>
    {/if}
</div>
