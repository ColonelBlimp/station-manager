<script lang="ts">
    /*
        TX-drive (ALC) readout (ADR 0064) — collapsed it is a label chip
        ("ALC" + state dot; the number was dropped 2026-08-08 — colour is the
        at-a-glance signal), open it is a small card with the 0-255 bar and
        the amber-floor marker, following the RX audio card's interaction
        grammar exactly (chip click opens, MINUS folds back — not an X, the
        meter is never closed). Renders nothing until the first ALC poll
        answer of this page-load (an instrument that cannot read must not
        paint a value).

        States (logic pinned in txDrive.svelte.test.ts): good (< alc_amber:
        the healthy band, ratified 2026-08-07) / warn (≥ alc_amber: drive
        high, reduce the audio level — TERMINAL, red folded into amber
        2026-08-08: the RM ALC answer saturates at ~30 of 255, so no ALC-only
        red could ever fire; internal/bridge/meters.go carries the
        measurement) / stale (answers stopped: NO DATA, deliberately distinct
        from a healthy reading). Card structure is FIXED in every state (the
        audio card's V6 lesson: layouts must not swap) — bar track + two h-4
        lines always render, only content varies.
    */
    import {
        txDriveState,
        txDriveStatus,
        txDriveAmberThreshold,
        setTxDriveOpen,
        type TxDriveStatus,
    } from './txDrive.svelte';

    // Staleness needs a clock: re-evaluate twice per poll-cadence-ish so the
    // stale flip lands within ~500 ms of the window expiring.
    let now = $state(Date.now());
    $effect(() => {
        const id = setInterval(() => {
            now = Date.now();
        }, 500);
        return () => clearInterval(id);
    });

    const status: () => TxDriveStatus = $derived(() => txDriveStatus(now));

    const toneByState: Record<Exclude<TxDriveStatus, 'hidden'>, string> = {
        good: 'bg-emerald-500',
        warn: 'bg-amber-500',
        stale: 'bg-zinc-500',
    };

    const labelByState: Record<Exclude<TxDriveStatus, 'hidden'>, string> = {
        // Green is the HEALTHY band, not "ALC at zero" (ratified 2026-08-07:
        // healthy FT8 drive measures ALC 15–18 here, never zero while keyed).
        good: 'drive right',
        // Terminal: the instrument cannot say how far over (RM ALC saturates
        // at the zone edge), so the message is the action, not a severity.
        warn: 'ALC high — reduce the audio level',
        stale: 'no poll answers',
    };

    // Label + dot only (operator, 2026-08-08): colour is the at-a-glance
    // signal; the raw value lives on the opened card. Stale keeps its dash —
    // "no data" must never render like a healthy label.
    const chipText: () => string = $derived(() => (status() === 'stale' ? 'ALC —' : 'ALC'));

    const pct = (v: number): number => Math.min(100, Math.max(0, (v / 255) * 100));
</script>

{#if status() !== 'hidden'}
    {#if !txDriveState.open}
        <button
            type="button"
            data-txdrive-chip
            data-state={status()}
            class="flex cursor-pointer items-center gap-x-1.5 rounded-full border border-line bg-surface px-2.5 py-1.5 shadow-md"
            title="TX drive — rig ALC (0–255) polled live. {labelByState[
                status() as Exclude<TxDriveStatus, 'hidden'>
            ]}"
            aria-label="Open the TX drive (ALC) meter"
            onclick={() => setTxDriveOpen(true)}
        >
            <span class="font-mono text-xs text-muted">{chipText()}</span>
            <span
                class="size-2.5 rounded-full {toneByState[
                    status() as Exclude<TxDriveStatus, 'hidden'>
                ]}"
                aria-hidden="true"
            ></span>
        </button>
    {:else}
        <div data-txdrive-card data-state={status()} class="card w-64 !p-4 shadow-lg">
            <div class="flex items-center justify-between">
                <h3 class="text-sm font-semibold text-ink">TX drive (ALC)</h3>
                <button
                    type="button"
                    data-txdrive-collapse
                    class="cursor-pointer rounded-md text-muted hover:text-ink"
                    title="Minimise to the chip"
                    aria-label="Minimise the TX drive meter to its chip"
                    onclick={() => setTxDriveOpen(false)}
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

            <!-- Fixed structure in every state: bar + two h-4 lines. The
                 amber marker sits at the configured floor; the fill is the
                 raw ALC value on the same 0-255 scale. -->
            <div
                data-meter-bar
                class="relative mt-2 h-2.5 overflow-hidden rounded-full bg-surface-muted"
            >
                {#if status() !== 'stale'}
                    <div
                        class="h-full rounded-full {toneByState[
                            status() as Exclude<TxDriveStatus, 'hidden'>
                        ]}"
                        style="width: {pct(txDriveState.alc?.value ?? 0)}%"
                    ></div>
                {/if}
                <!-- Anchored by RIGHT: right = (100 - threshold%), so the
                     marker's 2px body extends INWARD from the threshold and
                     survives the track's overflow-hidden even at the valid
                     maximum 255 (left-anchoring rendered left:100% there and
                     clipped it entirely — codex P2 on 84886af2). The marker
                     sits at the AMBER floor — the only threshold since the
                     fold — where "reduce the audio level" begins. -->
                <div
                    data-alc-amber-marker
                    class="absolute top-0 h-full w-0.5 bg-amber-500/80"
                    style="right: {100 - pct(txDriveAmberThreshold())}%"
                    aria-hidden="true"
                ></div>
            </div>
            <div data-meter-line class="mt-1.5 h-4 font-mono text-xs text-muted">
                {status() === 'stale'
                    ? 'no poll answers'
                    : `ALC ${txDriveState.alc?.value ?? 0} of 255`}
            </div>
            <div data-meter-line class="h-4 text-xs text-muted">
                {status() === 'stale'
                    ? 'rig silent on RM4 — CAT or session gone?'
                    : labelByState[status() as Exclude<TxDriveStatus, 'hidden'>]}
            </div>
        </div>
    {/if}
{/if}
