<script lang="ts">
    import type { Ft8Band } from '../api/ft8-sse';
    import { signalProximity, offsetFromFraction, clampOffset } from '../utils/ft8Spectrum';

    // The "Spectrum" occupancy view — a CONTINUOUS presentation of the same per-slot
    // snapshot the channelised strip uses. Signals render as soft shaded regions at
    // their TRUE positions/widths (not snapped to a grid), you click ANYWHERE to pick
    // a continuous offset (so you can tuck into a gap), and the pick's status is GRADED
    // — clear / near / sharing — rather than a binary red. Matches how FT8 actually
    // works (continuous, overlap-tolerant). RX-safe: selection is inert until the TX
    // controller consumes it.
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
        /** Why the panel is empty when hasSlot is false — drives the copy below. */
        emptyReason: '' | 'waiting' | 'tx-parity';
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
        emptyReason,
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
              ? 'bg-orange-500/40 border-orange-500'
              : prox.kind === 'near'
                ? 'bg-amber-400/50 border-amber-500'
                : 'bg-green-500/40 border-green-600'
    );
    const captionClass = $derived(
        prox === null
            ? 'text-muted'
            : prox.kind === 'sharing'
              ? 'text-orange-600 dark:text-orange-400'
              : prox.kind === 'near'
                ? 'text-amber-600 dark:text-amber-400'
                : 'text-green-600 dark:text-green-400'
    );
    const captionText = $derived.by(() => {
        if (selected === null) return 'auto — daemon pick';
        if (prox === null) return `${selected} Hz`;
        if (prox.kind === 'sharing') return `${selected} Hz · sharing`;
        if (prox.kind === 'near') return `${selected} Hz · near — signal ~${prox.gapHz} Hz off`;
        return `${selected} Hz · clear`;
    });

    // Click-anywhere AND drag, unified via Pointer Events: pointerdown picks,
    // pointermove refines while held, pointerup commits. setPointerCapture keeps the
    // drag tracking even if the pointer leaves the bar. Mouse/touch/pen all work (the
    // bar has touch-none so a touch-drag doesn't scroll the page).
    let dragging = $state(false);

    function hzAt(e: { clientX: number; currentTarget: HTMLElement }): number {
        const rect = e.currentTarget.getBoundingClientRect();
        const frac = (e.clientX - rect.left) / rect.width;
        return offsetFromFraction(frac, passbandLow, passbandHigh, signalWidth);
    }

    function onBarPointerDown(e: PointerEvent & { currentTarget: HTMLElement }): void {
        e.currentTarget.setPointerCapture(e.pointerId);
        dragging = true;
        onselect(hzAt(e));
    }

    function onBarPointerMove(e: PointerEvent & { currentTarget: HTMLElement }): void {
        if (!dragging) return;
        onselect(hzAt(e));
    }

    function onBarPointerUp(e: PointerEvent & { currentTarget: HTMLElement }): void {
        if (!dragging) return;
        dragging = false;
        try {
            e.currentTarget.releasePointerCapture(e.pointerId);
        } catch {
            // Capture may already be gone (e.g. pointercancel raced) — ignore.
        }
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

<div class="flex h-full w-full flex-col px-4 py-2">
    <div class="flex items-baseline justify-between text-xs">
        <span class="text-muted">TX Offset — click anywhere</span>
        <span class="font-mono {captionClass}">{captionText}</span>
    </div>

    {#if hasSlot}
        <!-- soft shading = a signal · green/amber/orange band = your pick · ▼ = daemon clear offset, ★ = top pick -->
        <!-- isolate: the ★'s z-50 exists to win against its SIBLING ▼ markers
             when offsets crowd. Without a stacking context here it leaked into
             the page's ROOT context and painted over every fixed overlay below
             z-50 — found as "the Occupancy Panel is overlaying" the z-30 RX
             audio meter (operator, 2026-08-06). Z1 pins the pair. -->
        <div class="relative isolate my-3 pt-3">
            <!-- Suggested clear offsets — clickable markers centred on each offset's
                 footprint. Clicking one commits that exact offset. ★ = top pick. -->
            {#each suggested as s (s)}
                <button
                    type="button"
                    class="absolute top-0 -mt-0.5 -translate-x-1/2 cursor-pointer border-0 bg-transparent p-0 text-[11px] leading-none hover:opacity-70 {s ===
                    recommended
                        ? 'z-50 text-amber-500'
                        : 'text-focus'}"
                    style:left="{pct(s + signalWidth / 2)}%"
                    title={`Select clear offset ${s} Hz${s === recommended ? ' (top pick)' : ''}`}
                    aria-label={`Select clear offset ${s} hertz${s === recommended ? ', top pick' : ''}`}
                    onclick={() => onselect(s)}>{s === recommended ? '★' : '▼'}</button
                >
            {/each}

            <!-- The continuous passband bar: click-anywhere + drag TX-offset picker. -->
            <div
                role="slider"
                tabindex="0"
                aria-label="TX offset (continuous)"
                aria-valuemin={passbandLow}
                aria-valuemax={maxOffset}
                aria-valuenow={selected ?? passbandLow}
                class="relative h-11 w-full touch-none overflow-hidden rounded border border-line bg-surface-muted focus:ring-2 focus:ring-focus-ring focus:outline-none {dragging
                    ? 'cursor-grabbing'
                    : 'cursor-crosshair'}"
                onpointerdown={onBarPointerDown}
                onpointermove={onBarPointerMove}
                onpointerup={onBarPointerUp}
                onpointercancel={onBarPointerUp}
                onkeydown={onBarKey}
            >
                <!-- Signals: soft neutral shading at their true positions. -->
                {#each occupied as b (b.low_hz)}
                    <div
                        class="pointer-events-none absolute top-0 h-full bg-slate-400/40 dark:bg-slate-400/30"
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
                    class="pointer-events-none absolute top-full mt-0.5 -translate-x-1/2 text-[11px] leading-none text-ink"
                    style:left="{pct(selected + signalWidth / 2)}%"
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
