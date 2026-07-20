<script lang="ts">
    // Settings — the app-shell home for station configuration (ADR 0044). The
    // keystone that lets the standalone config SPA retire: it grows the rig /
    // mode-mapping / forwarder / station-identity surfaces the config SPA owns
    // today, plus the FT8 settings + My Station panels that have no app
    // counterpart yet. Sections switch via the tab strip; forwarders, mode
    // mappings, and the rest land in follow-up increments.
    import StationSection from './StationSection.svelte';
    import RigsSection from './RigsSection.svelte';

    type SectionId = 'station' | 'rigs';
    const sections: { id: SectionId; label: string }[] = [
        { id: 'station', label: 'Station' },
        { id: 'rigs', label: 'Rigs' },
    ];
    let active = $state<SectionId>('station');
</script>

<div class="mx-auto max-w-5xl">
    <header class="mb-4">
        <h1 class="text-2xl font-semibold text-ink">Settings</h1>
        <p class="mt-1 text-sm text-muted">
            Station configuration — rigs, mode mappings, forwarders, and station
            identity.
        </p>
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

    <!-- Both sections stay MOUNTED; the inactive one is just hidden. A
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
</div>
