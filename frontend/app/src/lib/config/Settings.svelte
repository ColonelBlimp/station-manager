<script lang="ts">
    // Settings — the app-shell home for station configuration (ADR 0044). The
    // keystone that lets the standalone config SPA retire: it grows the rig /
    // mode-mapping / forwarder / station-identity surfaces the config SPA owns
    // today, plus the FT8 settings + My Station panels that have no app
    // counterpart yet.
    //
    // First real section: Station (ADIF identity). Rigs, forwarders, mode
    // mappings, and the rest land in follow-up increments.
    import StationSection from './StationSection.svelte';
</script>

<div class="mx-auto max-w-5xl">
    <header class="mb-6">
        <h1 class="text-2xl font-semibold text-ink">Settings</h1>
        <p class="mt-1 text-sm text-muted">
            Station configuration — rigs, mode mappings, forwarders, and station
            identity.
        </p>
    </header>

    <StationSection />
</div>
