<script lang="ts">
    // FT8 operating surface (ADR 0047) — the three-anchor fixed layout: Band
    // Activity + Operate side-by-side up top, Occupancy (TX-offset picker) full-
    // width across the bottom.
    import { onMount } from 'svelte';
    import { router } from '../router.svelte';
    import { ft8State, startFt8, stopFt8, reclaimFt8, armTx, abandonQso } from './ft8.svelte';
    import { toasts } from '../ui/toasts.svelte';
    import { ft8EnrichState } from './ft8Enrich.svelte';
    import { rig } from './rig.svelte';
    import Ft8BandActivity from './Ft8BandActivity.svelte';
    import Ft8Operate from './Ft8Operate.svelte';
    import Ft8Occupancy from './Ft8Occupancy.svelte';
    import AudioLevelCard from './AudioLevelCard.svelte';
    import TxDriveChip from './TxDriveChip.svelte';

    // View-scoped stream lifecycle: open on mount, close on destroy, so the
    // daemon holds the capture device only while the FT8 view is shown. The
    // enrichment cache clears too, so a re-open starts clean.
    // The mount claims the router's profile first (ADR 0080): the view is keyed
    // on router.mode, so FT8 ↔ FT4 remounts through this exact stop → claim →
    // start sequence.
    // `alive` gates the banner's stop-then-reclaim continuation: a daemon action
    // that resolves after this view is gone must not claim and open a stream
    // nobody will close (codex 67cc1b96 P2).
    let alive = true;
    onMount(() => {
        void startFt8(router.mode === 'ft4' ? 'ft4' : 'ft8');
        return () => {
            alive = false;
            stopFt8();
            ft8EnrichState.clear();
        };
    });

    // The refusal banner's countdown (whole seconds to the scheduled re-claim).
    let now = $state(Date.now());
    $effect(() => {
        if (ft8State.claimRefusal === null) return;
        const t = setInterval(() => (now = Date.now()), 250);
        return () => clearInterval(t);
    });
    const retryIn = $derived(
        ft8State.claimRefusal !== null && ft8State.claimRefusal.retryAt > 0
            ? Math.max(0, Math.ceil((ft8State.claimRefusal.retryAt - now) / 1000))
            : null
    );
    const profileLabel = $derived(router.mode === 'ft4' ? 'FT4' : 'FT8');
    const claimMode = $derived(router.mode === 'ft4' ? 'ft4' : 'ft8');

    // The stop paths stay reachable through a refusal (ADR 0080): when the other
    // profile's TX is armed (or in flight) the banner offers Disable TX, and when
    // its session is active it offers Abandon too. Both act on the daemon, which
    // knows only one armed state and one session, then the view re-claims at once
    // rather than waiting out the hint. The daemon's disarm cancels the rung on
    // the air, waits for it, ends the session and closes the device, so a claim
    // after it is not refused for TX. Its abandon drops the contact NOW but
    // deliberately leaves TX ARMED (servicetx.go AbandonQso) — a claim after an
    // abandon alone is refused ft8_tx_armed, or ft8_tx_in_flight while the
    // cancelled rung is still returning — so the abandon path disarms as well,
    // and its label says so. What separates the two is the contact in progress:
    // abandon retires it before the cancel, so a rung completing on the cancel
    // path does not log; disarm lets that completion log first. stopFt8 cleared
    // the old tx/qso frames, so the Operate anchor's own controls cannot offer
    // these here.
    const refusalCode = $derived(ft8State.claimRefusal?.code ?? '');
    const offersDisarm = $derived(
        refusalCode === 'ft8_tx_armed' ||
            refusalCode === 'ft8_tx_in_flight' ||
            refusalCode === 'ft8_session_active'
    );
    const offersAbandon = $derived(refusalCode === 'ft8_session_active');
    let acting = $state(false);
    async function stopThenReclaim(action: 'disarm' | 'abandon'): Promise<void> {
        acting = true;
        try {
            if (action === 'abandon') {
                const r = await abandonQso();
                if (!r.ok) {
                    toasts.error(r.message);
                    return;
                }
            }
            const r = await armTx(false);
            if (r.status === 'failed') {
                toasts.error(r.message);
                return;
            }
            if (!alive) return; // the view left while the daemon acted: nothing to re-open
            await reclaimFt8(claimMode);
        } finally {
            acting = false;
        }
    }

    // Band-change watcher (dogfood niggle 2026-07-19): crossing a band boundary
    // clears the Band Activity feed (and, on a genuine band-to-band change, the
    // pile-up queue). The transition logic lives in ft8State.noteOperatingBand
    // so it's unit-tested there; this effect just feeds it the rig band.
    $effect(() => ft8State.noteOperatingBand(rig.band));
</script>

<div class="ft8-grid relative">
    {#if ft8State.claimRefusal !== null}
        <!-- A refused profile claim explains itself here, in place of Band Activity,
             and the view re-claims when the daemon's hint elapses (ADR 0080). -->
        <div
            style="grid-area:ba; min-height:0"
            class="rounded-lg border border-amber-500/40 bg-amber-500/10 p-4 text-sm"
            role="status"
            data-testid="ft8-claim-banner"
        >
            <p class="font-medium">{profileLabel} is not available yet</p>
            <p class="mt-1 text-muted">{ft8State.claimRefusal.message}</p>
            {#if retryIn !== null}
                <p class="mt-1 text-muted">Retrying in {retryIn}s.</p>
            {:else if ft8State.claimRefusal.transport}
                <p class="mt-1 text-muted">Retrying when the connection returns.</p>
            {/if}
            {#if !offersDisarm && retryIn === null}
                <!-- No countdown and nothing to stop: the operator can still ask now
                     rather than leave and re-enter the view. -->
                <div class="mt-3">
                    <button
                        type="button"
                        class="rounded-md border border-line px-3 py-1 text-sm hover:bg-black/5 dark:hover:bg-white/5"
                        onclick={() => void reclaimFt8(claimMode)}
                    >
                        Try again
                    </button>
                </div>
            {/if}
            {#if offersDisarm}
                <div class="mt-3 flex flex-wrap gap-2">
                    {#if offersAbandon}
                        <button
                            type="button"
                            class="rounded-md border border-line px-3 py-1 text-sm hover:bg-black/5 dark:hover:bg-white/5"
                            disabled={acting}
                            onclick={() => void stopThenReclaim('abandon')}
                        >
                            Abandon session and disable TX
                        </button>
                    {/if}
                    <button
                        type="button"
                        class="rounded-md border border-line px-3 py-1 text-sm hover:bg-black/5 dark:hover:bg-white/5"
                        disabled={acting}
                        onclick={() => void stopThenReclaim('disarm')}
                    >
                        Disable TX
                    </button>
                </div>
            {/if}
        </div>
    {:else}
        <div style="grid-area:ba; min-height:0"><Ft8BandActivity /></div>
    {/if}
    <div style="grid-area:op; min-height:0"><Ft8Operate /></div>
    <div style="grid-area:occ; min-height:0"><Ft8Occupancy /></div>

    <!-- RX audio-level instrument — FT8-only: capture runs only while this
         view is open, so the meter lives and dies with it. Anchored to the
         grid's bottom-left JUST ABOVE the Occupancy panel (operator,
         2026-08-06 — bottom-of-viewport sat ON the panel): the offset is the
         occ row's 180px + the grid's 0.75rem gap, both defined in the style
         block below — change one, change both. The open card grows UPWARD
         from this anchor, over Band Activity, never over Occupancy. -->
    <div class="absolute bottom-[calc(180px+0.75rem)] left-0 z-30 flex flex-col items-start gap-2">
        <!-- ADR 0064: TX-drive (ALC) readout — stacked ABOVE the RX audio
             meter (operator, 2026-08-07: vertical, not horizontal), so the
             audio chip keeps its anchored spot at the bottom. Renders nothing
             until the first poll answer, so non-METERPOLL rigs never see an
             empty shell. -->
        <TxDriveChip />
        <AudioLevelCard />
    </div>
</div>

<style>
    .ft8-grid {
        display: grid;
        gap: 0.75rem;
        /* Band Activity + Operate are a fixed 470px each, centred by the 1fr
           gutter columns on either side. Occupancy spans ALL four tracks, so it
           stays full width edge-to-edge below them. Narrower viewports scroll via
           the main container (the 1fr gutters collapse to 0 first). */
        grid-template-columns: 1fr 470px 470px 1fr;
        grid-template-rows: minmax(0, 1fr) 180px;
        grid-template-areas:
            '. ba op .'
            'occ occ occ occ';
        height: calc(100vh - 8rem);
    }
</style>
