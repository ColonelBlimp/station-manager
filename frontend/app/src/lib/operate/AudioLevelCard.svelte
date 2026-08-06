<script lang="ts">
    /*
        RX audio-level card (dogfood 2026-08-06) — the bottom-left corner
        instrument for "is the audio from the rig at a good level?". Collapsed
        it is an icon CHIP whose colour carries the current state (the agreed
        enhancement: closed is never blind — it catches the eye exactly when
        something is wrong); open it shows the bar + dB readout the operator
        calibrates against.

        A deliberate THIRD placement pattern (not the ambient host, not a
        drawer): a continuous instrument's collapsed form must stay visible to
        be worth anything, which the fully-hidden ambient panels cannot do —
        and the toggle lives ON the card, not in the rail across the screen.
        FT8-only: mounted by Ft8View; capture only runs there.

        TX STAND-DOWN is this card's rule (not the classifier's): while the
        rig is keyed the capture path carries nothing useful, so the card
        shows a neutral 'tx' state rather than a misleading orange.

        Colour + geometry hang off data-state; jsdom pins the attribute
        (AudioLevelCard.svelte.test.ts), the visual outcome is Playwright's
        when that layer exists.
    */
    import {
        audioLevel,
        audioLevelStatus,
        setAudioLevelOpen,
        type AudioLevelStatus,
    } from './audioLevel.svelte';
    import { ft8State } from './ft8.svelte';

    type CardState = AudioLevelStatus | 'tx';
    const state: () => CardState = $derived(() =>
        ft8State.tx.transmitting ? 'tx' : audioLevelStatus()
    );

    const toneByState: Record<CardState, string> = {
        good: 'bg-emerald-500',
        low: 'bg-amber-500',
        high: 'bg-red-500',
        stale: 'bg-gray-400',
        off: 'bg-gray-400',
        tx: 'bg-sky-400',
    };
    const labelByState: Record<CardState, string> = {
        good: 'RX audio level good',
        low: 'RX audio too low',
        high: 'RX audio too high',
        stale: 'No audio arriving — capture stalled?',
        off: 'No capture',
        tx: 'Transmitting — meter standing by',
    };

    /** Map dBFS onto the bar: -90 dBFS → 0 %, 0 dBFS → 100 %. */
    function pct(v: number | null): number {
        if (v === null) return 0;
        return Math.max(0, Math.min(100, ((v + 90) / 90) * 100));
    }
    const fmt = (v: number | null): string => (v === null ? '—' : `${v.toFixed(1)}`);
</script>

{#if !audioLevel.open}
    <button
        type="button"
        data-audio-chip
        data-state={state()}
        class="fixed bottom-4 left-4 z-30 flex cursor-pointer items-center gap-x-1.5 rounded-full border border-line bg-surface py-1.5 pr-2.5 pl-2 shadow-md"
        title={labelByState[state()]}
        aria-label="Open the RX audio level meter — {labelByState[state()]}"
        onclick={() => setAudioLevelOpen(true)}
    >
        <!-- Speaker-wave icon; the dot beside it is the state colour. -->
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            class="size-4 text-muted"
            aria-hidden="true"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M19.1 4.9a10 10 0 0 1 0 14.2M16.3 7.7a6 6 0 0 1 0 8.6M11 5 6.5 9H3v6h3.5L11 19V5Z"
            />
        </svg>
        <span class="size-2.5 rounded-full {toneByState[state()]}" aria-hidden="true"></span>
    </button>
{:else}
    <div
        data-audio-card
        data-state={state()}
        class="card fixed bottom-4 left-4 z-30 w-64 !p-4 shadow-lg"
    >
        <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-ink">RX audio</h3>
            <button
                type="button"
                data-audio-close
                class="cursor-pointer rounded-md text-muted hover:text-ink"
                title="Collapse"
                aria-label="Collapse the RX audio level meter"
                onclick={() => setAudioLevelOpen(false)}
            >
                <svg
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    class="size-4"
                    aria-hidden="true"
                >
                    <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
                </svg>
            </button>
        </div>

        {#if state() === 'tx' || state() === 'off' || state() === 'stale'}
            <p class="mt-2 text-xs text-muted">{labelByState[state()]}</p>
        {:else}
            <!-- RMS fill + peak tick over the -90..0 dBFS span. -->
            <div class="relative mt-2 h-2.5 overflow-hidden rounded-full bg-surface-muted">
                <div
                    class="h-full rounded-full {toneByState[state()]}"
                    style="width: {pct(audioLevel.rmsDbfs)}%"
                ></div>
                <div
                    class="absolute top-0 h-full w-0.5 bg-ink/60"
                    style="left: {pct(audioLevel.peakDbfs)}%"
                    aria-hidden="true"
                ></div>
            </div>
            <p class="mt-1.5 text-xs tabular-nums text-muted">
                RMS {fmt(audioLevel.rmsDbfs)} dB · Peak {fmt(audioLevel.peakDbfs)} dB
            </p>
            {#if state() !== 'good'}
                <p class="mt-1 text-xs text-muted">{labelByState[state()]}</p>
            {/if}
        {/if}
    </div>
{/if}
