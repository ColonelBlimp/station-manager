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
        FT8-only, and POSITIONED BY Ft8View (anchored above the Occupancy
        panel — operator, 2026-08-06); this component owns no placement, so
        the anchor and the grid constants it depends on live in one file.

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
    /** Bar + readout states, as opposed to the message-only trio. */
    const measuring: () => boolean = $derived(
        () => state() !== 'tx' && state() !== 'off' && state() !== 'stale'
    );
</script>

{#if !audioLevel.open}
    <button
        type="button"
        data-audio-chip
        data-state={state()}
        class="flex cursor-pointer items-center gap-x-1.5 rounded-full border border-line bg-surface py-1.5 pr-2.5 pl-2 shadow-md"
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
    <div data-audio-card data-state={state()} class="card w-64 !p-4 shadow-lg">
        <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-ink">RX audio</h3>
            <!-- A MINUS, deliberately not an X: the meter is not closed by
                 this — it folds back to the live chip (operator, 2026-08-06:
                 "X implies close, but it is not closed"). -->
            <button
                type="button"
                data-audio-collapse
                class="cursor-pointer rounded-md text-muted hover:text-ink"
                title="Minimise to the chip"
                aria-label="Minimise the RX audio level meter to its chip"
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
                    <path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14" />
                </svg>
            </button>
        </div>

        <!-- FIXED STRUCTURE IN EVERY STATE (dogfood 2026-08-06: the card's
             height jumped when TX started — layouts must not swap). The bar
             track and both fixed-height lines always render; only content
             varies: measuring fills the bar and line 1 carries the readout;
             tx/off/stale leave the bar empty and line 1 carries the state;
             line 2 carries the low/high hint and is otherwise blank. h-4
             matches text-xs line height, so blank lines hold their space.
             V6 pins the structure; equal heights are Playwright's. -->
        <div
            data-meter-bar
            class="relative mt-2 h-2.5 overflow-hidden rounded-full bg-surface-muted"
        >
            {#if measuring()}
                <div
                    class="h-full rounded-full {toneByState[state()]}"
                    style="width: {pct(audioLevel.rmsDbfs)}%"
                ></div>
                <div
                    class="absolute top-0 h-full w-0.5 bg-ink/60"
                    style="left: {pct(audioLevel.peakDbfs)}%"
                    aria-hidden="true"
                ></div>
            {/if}
        </div>
        <p data-meter-line class="mt-1.5 h-4 text-xs tabular-nums text-muted">
            {#if measuring()}
                RMS {fmt(audioLevel.rmsDbfs)} dB · Peak {fmt(audioLevel.peakDbfs)} dB
            {:else}
                {labelByState[state()]}
            {/if}
        </p>
        <p data-meter-line class="mt-1 h-4 text-xs text-muted">
            {#if measuring() && state() !== 'good'}{labelByState[state()]}{/if}
        </p>
    </div>
{/if}
