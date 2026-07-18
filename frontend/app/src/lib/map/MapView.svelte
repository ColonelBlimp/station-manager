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
    import { bandColor, bandRank } from './bandColors';
    import { storageGet, storageSet } from '../utils/storage';

    const teardown = startMapData();
    $effect(() => {
        return teardown;
    });

    // Grey-line overlay: operator-toggled (persisted like the theme prefs)
    // with a minute clock — the terminator moves ~0.25°/min, so 60 s keeps
    // it visually current for a fraction of a refetch's cost.
    const GREYLINE_KEY = 'sm-map-greyline';
    let greyline = $state(storageGet(GREYLINE_KEY) !== 'off');
    let terminatorNow = $state(new Date());
    $effect(() => {
        storageSet(GREYLINE_KEY, greyline ? 'on' : 'off');
    });
    $effect(() => {
        if (!greyline) return;
        terminatorNow = new Date(); // re-enabling after a while off: catch up now
        const timer = setInterval(() => (terminatorNow = new Date()), 60_000);
        return () => clearInterval(timer);
    });

    const arcs = $derived.by(() => {
        const origin = mapData.origin;
        if (origin === null) return [];
        return mapData.qsos.map((q) => ({
            key: q.key,
            from: origin,
            to: q.point,
            label: q.label,
            color: bandColor(q.band, mapData.bandColors),
        }));
    });
    const unplotted = $derived(mapData.total - mapData.qsos.length);

    // Legend: the bands actually in the window (wavelength order, unknown
    // last) — colour-coding is only readable with the key alongside.
    const legend = $derived.by(() => {
        const counts: Record<string, number> = {};
        for (const q of mapData.qsos) {
            counts[q.band] = (counts[q.band] ?? 0) + 1;
        }
        return Object.entries(counts)
            .sort(([a], [b]) => bandRank(a) - bandRank(b) || a.localeCompare(b))
            .map(([band, count]) => ({
                band,
                count,
                label: band === '' ? '?' : band,
                color: bandColor(band, mapData.bandColors),
            }));
    });
</script>

<svelte:head>
    <title>Contacts Map — Station Manager</title>
</svelte:head>

<div class="flex h-screen flex-col bg-canvas">
    <header class="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line px-4 py-2">
        <h1 class="text-sm font-semibold text-ink">Contacts Map</h1>

        <label class="flex items-center gap-x-2 text-xs text-muted">
            Window
            <select
                class="rounded-md bg-surface px-2 py-1 text-xs font-medium text-ink outline-1
                       -outline-offset-1 outline-line focus:outline-2 focus:-outline-offset-2
                       focus:outline-focus"
                aria-label="Time window"
                value={mapData.durationMin}
                onchange={(e) => setDuration(Number(e.currentTarget.value))}
            >
                {#each DURATIONS as d (d.minutes)}
                    <option value={d.minutes}>{d.label}</option>
                {/each}
            </select>
        </label>

        <label class="flex items-center gap-x-1.5 text-xs text-muted select-none">
            <input type="checkbox" class="accent-focus" bind:checked={greyline} />
            Grey line
        </label>

        <span class="hidden text-[0.65rem] text-muted xl:inline">
            scroll to zoom · drag to pan · double-click to reset
        </span>

        {#if legend.length > 0}
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1" data-testid="legend">
                {#each legend as l (l.band)}
                    <span class="flex items-center gap-x-1 text-xs text-muted">
                        <span
                            class="inline-block size-2 rounded-full"
                            style="background-color: {l.color}"
                        ></span>
                        {l.label}
                        <span class="text-[0.65rem]">({l.count})</span>
                    </span>
                {/each}
            </div>
        {/if}

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
                <WorldMap
                    origin={mapData.origin}
                    {arcs}
                    terminator={greyline ? terminatorNow : null}
                />
            </div>
        </div>
        {#if mapData.status === 'ok' && mapData.origin === null}
            <p class="px-4 pb-3 text-center text-xs text-muted">
                Station grid not configured — contacts shown without arcs once set.
            </p>
        {/if}
    {/if}
</div>
