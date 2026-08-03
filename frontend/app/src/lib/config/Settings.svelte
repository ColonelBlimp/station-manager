<script lang="ts">
    // Settings — the app-shell home for station configuration (ADR 0044). The
    // keystone that lets the standalone config SPA retire: it grows the rig /
    // mode-mapping / forwarder / station-identity surfaces the config SPA owns
    // today, plus the FT8 settings + My Station panels that have no app
    // counterpart yet. Sections switch via the tab strip; forwarders, mode
    // mappings, and the rest land in follow-up increments.
    import StationSection from './StationSection.svelte';
    import RigsSection from './RigsSection.svelte';
    import ForwardingSection from './ForwardingSection.svelte';
    import EmailSection from './EmailSection.svelte';
    import EnrichmentSection from './EnrichmentSection.svelte';
    import { restartDaemon, waitForDaemonBack, fetchDaemonInstance } from '../api/restart';
    import { toasts } from '../ui/toasts.svelte';

    type SectionId = 'station' | 'rigs' | 'forwarding' | 'email' | 'enrichment';
    const sections: { id: SectionId; label: string }[] = [
        { id: 'station', label: 'Station' },
        { id: 'rigs', label: 'Rigs' },
        { id: 'forwarding', label: 'Forwarding' },
        { id: 'email', label: 'Email' },
        { id: 'enrichment', label: 'Enrichment' },
    ];
    let active = $state<SectionId>('station');

    // Manual daemon restart — applies the "Requires a restart" config changes
    // (active rig, connection, mappings, overrides). Refused while transmitting.
    let restarting = $state(false);
    async function doRestart(): Promise<void> {
        if (restarting) return;
        if (
            !window.confirm(
                'Restart the daemon now? This briefly drops the connection and applies pending rig/config changes.'
            )
        )
            return;
        restarting = true;
        // Capture the current process instance so the poll waits for a DIFFERENT
        // one (the respawned daemon), not just any 200 from the old one still
        // shutting down (codex 85997b79 P2).
        const before = await fetchDaemonInstance();
        const out = await restartDaemon();
        switch (out.kind) {
            case 'accepted': {
                // Poll until the daemon answers again, then re-enable the button —
                // the SSE clients reconnect on their own, but the Settings component
                // isn't remounted, so nothing else would clear `restarting` (codex
                // 088bdb84 P2).
                toasts.info('Restarting the daemon…');
                const back = await waitForDaemonBack(before);
                toasts.info(
                    back
                        ? 'Daemon restarted.'
                        : 'Restart is taking a while — reload the page if it seems stuck.'
                );
                restarting = false;
                break;
            }
            case 'tx_active':
                toasts.error('Stop transmitting before restarting the daemon.');
                restarting = false;
                break;
            case 'unavailable':
                toasts.error("Restart isn't available on this daemon.");
                restarting = false;
                break;
            case 'error':
                toasts.error(`Restart failed: ${out.message}`);
                restarting = false;
                break;
        }
    }
</script>

<div class="mx-auto max-w-5xl">
    <header class="mb-4 flex items-start justify-between gap-4">
        <div>
            <h1 class="text-2xl font-semibold text-ink">Settings</h1>
            <p class="mt-1 text-sm text-muted">
                Station configuration — rigs, mode mappings, forwarders, outgoing email, and station
                identity.
            </p>
        </div>
        <button
            class="btn shrink-0"
            title="Applies pending rig/config changes (briefly drops the connection)"
            disabled={restarting}
            onclick={doRestart}
        >
            {restarting ? 'Restarting…' : 'Restart daemon'}
        </button>
    </header>

    <nav class="mb-6 flex gap-1 border-b border-line">
        {#each sections as s (s.id)}
            <button
                class="-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors {active ===
                s.id
                    ? 'border-focus text-ink'
                    : 'border-transparent text-muted hover:text-ink'}"
                onclick={() => (active = s.id)}
            >
                {s.label}
            </button>
        {/each}
    </nav>

    <!-- Every section stays MOUNTED; the inactive ones are just hidden. A
         conditional {#if} would unmount the hidden section, and remounting it
         re-runs its onMount load() — which would overwrite unsaved form edits
         (and re-fetch on every tab switch). Keeping them mounted preserves
         in-progress edits + selection across tab switches (review 2026-07-20
         Rigs #1 / #3). Each section still loads once, on first render. -->
    <div class:hidden={active !== 'station'}>
        <StationSection />
    </div>
    <div class:hidden={active !== 'rigs'}>
        <RigsSection />
    </div>
    <div class:hidden={active !== 'forwarding'}>
        <ForwardingSection />
    </div>
    <div class:hidden={active !== 'email'}>
        <EmailSection />
    </div>
    <div class:hidden={active !== 'enrichment'}>
        <EnrichmentSection />
    </div>
</div>
