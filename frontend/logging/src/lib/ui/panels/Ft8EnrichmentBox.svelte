<script lang="ts">
    import type { Ft8CallInfo } from '../../states/ft8Enrich.svelte';

    // The worked station's QRZ details while an FT8 QSO is active (both roles).
    // Each line appears as its lookup lands (progressive, fail-soft); an empty
    // lookup just leaves that line blank. Extracted from Ft8Panel so the layout
    // is a single source of truth — the ?ft8demo harness renders this same
    // component, so tweaking it there changes production directly.
    interface Props {
        /** Worked station's callsign (from the live QSO state, not the lookup). */
        call: string;
        /** Enrichment facts; undefined = no active QSO / not yet looked up. */
        info: Ft8CallInfo | undefined;
        /** Short-path distance in km, or null when grids are unknown. */
        distanceKm: number | null;
        /** Short-path beam heading in degrees, or null when grids are unknown. */
        bearingDeg: number | null;
    }
    let { call, info, distanceKm, bearingDeg }: Props = $props();

    // Beam heading WSJT-X-style: a zero-padded integer degree ("045°"), matching
    // the per-CQ heading column in Band Activity so the operator reads one format.
    const headingLabel = $derived(
        bearingDeg !== null ? `${Math.round(bearingDeg).toString().padStart(3, '0')}°` : null
    );
</script>

<div
    class="h-44 mt-2 overflow-y-auto rounded border border-gray-300 bg-gray-100 px-2 pt-7 text-center text-xs"
>
    {#if info}
        {#if info.flag || info.country}
            <div class="flex flex-col gap-0.5 text-gray-600">
                {#if info.flag}<span class="text-4xl leading-none" aria-hidden="true"
                        >{info.flag}</span
                    >{/if}
                {#if info.country}<span class="mb-1">{info.country}</span>{/if}
            </div>
        {/if}
        <div class="font-semibold text-gray-700">{call}</div>
        {#if info.opName}<div class="text-gray-700">{info.opName}</div>{/if}
        {#if distanceKm !== null}
            <div class="text-gray-600">{distanceKm.toLocaleString()} km</div>
        {/if}
        {#if headingLabel !== null}
            <div class="text-indigo-600" title="Beam heading (short path)">{headingLabel}</div>
        {/if}
    {:else}
        <span class="text-gray-400">Enrichment</span>
    {/if}
</div>
