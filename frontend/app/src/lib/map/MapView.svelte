<!--
    MapView — the contacts-map tab (backlog "QSO contacts map", Phase 2).
    Full-window on purpose: this route opens in its OWN browser tab (second
    monitor) via the Session tile's "Map ↗", so the app shell's sidebar and
    header would be dead chrome here — the map gets the whole working space,
    which is the reason the in-app overlay was rejected (ADR 0049 context).

    Composition: mapData owns the data story (window fetch + live events);
    WorldMap owns the drawing; this component owns the frame — duration
    picker, plotted-count line, live indicator, error surface.
-->
<script lang="ts">
    import WorldMap from './WorldMap.svelte';
    import { mapData, setDuration, startMapData, DURATIONS } from './mapData.svelte';

    const teardown = startMapData();
    $effect(() => {
        return teardown;
    });

    const arcs = $derived.by(() => {
        const origin = mapData.origin;
        if (origin === null) return [];
        return mapData.qsos.map((q) => ({
            key: q.key,
            from: origin,
            to: q.point,
            label: q.label,
        }));
    });
    const unplotted = $derived(mapData.total - mapData.qsos.length);
</script>

<svelte:head>
    <title>Contacts Map — Station Manager</title>
</svelte:head>

<div class="flex h-screen flex-col bg-canvas">
    <header class="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line px-4 py-2">
        <h1 class="text-sm font-semibold text-ink">Contacts Map</h1>

        <div class="flex items-center gap-x-1" role="group" aria-label="Time window">
            {#each DURATIONS as d (d.minutes)}
                <button
                    class="cursor-pointer rounded-md px-2 py-1 text-xs font-medium
                           {mapData.durationMin === d.minutes
                        ? 'bg-nav-accent-bg text-nav-accent-fg'
                        : 'text-muted hover:text-ink'}"
                    aria-pressed={mapData.durationMin === d.minutes}
                    onclick={() => setDuration(d.minutes)}
                >
                    {d.label}
                </button>
            {/each}
        </div>

        <div class="ml-auto flex items-center gap-x-3 text-xs text-muted">
            {#if mapData.status === 'ok'}
                <span data-testid="plotted">
                    {mapData.qsos.length} of {mapData.total} plotted{unplotted > 0
                        ? ` (${unplotted} without a location)`
                        : ''}
                </span>
                {#if mapData.capped}
                    <span class="text-invalid">window truncated — narrow it</span>
                {/if}
            {:else if mapData.status === 'loading'}
                <span>Loading…</span>
            {/if}
            <span class="flex items-center gap-x-1" title="Live updates from the daemon">
                <span
                    class="inline-block size-2 rounded-full {mapData.live
                        ? 'bg-logged'
                        : 'bg-line'}"
                    data-testid="live-dot"
                ></span>
                {mapData.live ? 'live' : 'offline'}
            </span>
        </div>
    </header>

    {#if mapData.status === 'error'}
        <div class="flex flex-1 items-center justify-center">
            <p class="text-sm text-invalid">{mapData.message}</p>
        </div>
    {:else}
        <div class="min-h-0 flex-1 p-4">
            <div class="mx-auto h-full max-w-[1600px]">
                <WorldMap origin={mapData.origin} {arcs} />
            </div>
        </div>
        {#if mapData.status === 'ok' && mapData.origin === null}
            <p class="px-4 pb-3 text-center text-xs text-muted">
                Station grid not configured — contacts shown without arcs once set.
            </p>
        {/if}
    {/if}
</div>
