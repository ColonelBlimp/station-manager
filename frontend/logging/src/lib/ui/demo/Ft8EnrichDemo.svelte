<script lang="ts">
    import Ft8EnrichmentBox from '../panels/Ft8EnrichmentBox.svelte';
    import type { Ft8CallInfo } from '../../states/ft8Enrich.svelte';

    // Standalone playground for the FT8 enrichment box layout. Reached via the
    // ?ft8demo URL gate in app.svelte — no daemon, no live FT8 needed. Edit the
    // fields on the left, watch the real <Ft8EnrichmentBox> on the right. Because
    // the box is the same component production renders, any layout change made to
    // it shows up here and on-air identically.

    let active = $state(true);
    let call = $state('IK2ABC');
    let opName = $state('Marco Rossi');
    let flag = $state('🇮🇹');
    let country = $state('Italy');
    let hasDistance = $state(true);
    let distanceKm = $state(1287);
    let hasBearing = $state(true);
    let bearingDeg = $state(45);
    let isNewEntity = $state(false);
    let path = $state<'short' | 'long'>('short');

    const info = $derived<Ft8CallInfo | undefined>(
        active ? { opName, flag, country, isNewEntity } : undefined
    );
    const dist = $derived(hasDistance ? distanceKm : null);
    const bearing = $derived(hasBearing ? bearingDeg : null);
</script>

<main class="mx-auto mt-12 max-w-3xl rounded-xl border border-line-soft p-8">
    <h1 class="mb-1 text-2xl font-semibold">FT8 Enrichment Box — layout playground</h1>
    <p class="mb-6 text-sm text-gray-500">
        Edit the fields; the box on the right is the production
        <code>&lt;Ft8EnrichmentBox&gt;</code>. URL: <code>?ft8demo</code>
    </p>

    <div class="flex gap-8">
        <!-- Controls -->
        <form class="flex w-72 flex-col gap-3 text-sm">
            <label class="flex items-center gap-2">
                <input type="checkbox" bind:checked={active} />
                <span>QSO active (else placeholder)</span>
            </label>
            <label class="flex flex-col gap-1">
                <span class="text-gray-600">Callsign</span>
                <input class="input-base" bind:value={call} disabled={!active} />
            </label>
            <label class="flex flex-col gap-1">
                <span class="text-gray-600">Operator name</span>
                <input class="input-base" bind:value={opName} disabled={!active} />
            </label>
            <label class="flex flex-col gap-1">
                <span class="text-gray-600">Flag (emoji)</span>
                <input class="input-base" bind:value={flag} disabled={!active} />
            </label>
            <label class="flex flex-col gap-1">
                <span class="text-gray-600">Country</span>
                <input class="input-base" bind:value={country} disabled={!active} />
            </label>
            <label class="flex items-center gap-2">
                <input type="checkbox" bind:checked={isNewEntity} disabled={!active} />
                <span>New DXCC entity (★ after country)</span>
            </label>
            <label class="flex items-center gap-2">
                <input type="checkbox" bind:checked={hasDistance} disabled={!active} />
                <span>Show distance</span>
            </label>
            <label class="flex flex-col gap-1">
                <span class="text-gray-600">Distance (km)</span>
                <input
                    class="input-base"
                    type="number"
                    bind:value={distanceKm}
                    disabled={!active || !hasDistance}
                />
            </label>
            <label class="flex items-center gap-2">
                <input type="checkbox" bind:checked={hasBearing} disabled={!active} />
                <span>Show bearing</span>
            </label>
            <label class="flex flex-col gap-1">
                <span class="text-gray-600">Bearing (°)</span>
                <input
                    class="input-base"
                    type="number"
                    bind:value={bearingDeg}
                    disabled={!active || !hasBearing}
                />
            </label>
        </form>

        <!-- Live preview, sized like the Rx Frequency column it lives in. -->
        <div class="w-44">
            <h2 class="mb-1 text-xs font-semibold text-gray-500">Live preview (w-44)</h2>
            <Ft8EnrichmentBox
                {call}
                {info}
                distanceKm={dist}
                bearingDeg={bearing}
                {path}
                onPathChange={(p: 'short' | 'long') => {
                    path = p;
                }}
            />
        </div>
    </div>
</main>
