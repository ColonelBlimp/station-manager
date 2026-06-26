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
        /** Distance in km for the SELECTED path, or null when grids are unknown. */
        distanceKm: number | null;
        /** Beam heading in degrees for the SELECTED path, or null when grids unknown. */
        bearingDeg: number | null;
        /** Operator's antenna-path choice (logging-only — drives ADIF ANT_PATH + the
         *  recorded bearing/distance). Mirrors the Phone/CW CountryPanel radio. Optional
         *  so the ?ft8demo harness can render the box without wiring it. */
        path?: 'short' | 'long';
        /** Called when the operator picks a path; the parent owns the state + the daemon
         *  POST. Omitted → no radio (e.g. demo without path wiring). */
        onPathChange?: (p: 'short' | 'long') => void;
    }
    let { call, info, distanceKm, bearingDeg, path = 'short', onPathChange }: Props = $props();

    // Beam heading WSJT-X-style: a zero-padded integer degree ("045°"), matching
    // the per-CQ heading column in Band Activity so the operator reads one format.
    const headingLabel = $derived(
        bearingDeg !== null ? `${Math.round(bearingDeg).toString().padStart(3, '0')}°` : null
    );
</script>

<div
    class="h-44 mt-2 overflow-y-auto rounded border border-gray-300 bg-gray-100 px-2 pt-4 text-center text-xs"
>
    {#if info}
        {#if info.flag || info.country}
            <div class="flex flex-col gap-0.5 text-gray-600">
                {#if info.flag}<span class="text-4xl leading-none" aria-hidden="true"
                        >{info.flag}</span
                    >{/if}
                {#if info.country}<span class="mb-1"
                        >{info.country}{#if info.isNewEntity}<span
                                class="ml-1 font-semibold text-green-700"
                                title="New DXCC entity">*</span
                            >{/if}</span
                    >{/if}
            </div>
        {/if}
        <div class="font-semibold text-gray-700">{call}</div>
        {#if info.opName}<div class="text-gray-700">{info.opName}</div>{/if}
        {#if distanceKm !== null}
            <div class="text-gray-600">{distanceKm.toLocaleString()} km</div>
        {/if}
        {#if headingLabel !== null}
            <div class="text-indigo-600" title={`Beam heading (${path} path)`}>{headingLabel}</div>
        {/if}
        {#if onPathChange}
            <!-- Antenna-path radio (logging-only — sets ADIF ANT_PATH + the recorded
                 bearing/distance; never the on-air signal). Mirrors the Phone/CW
                 CountryPanel radio so the operator drives both the same way. -->
            <div class="mt-1 flex justify-center gap-x-3 text-xs">
                <label class="inline-flex cursor-pointer items-center">
                    <input
                        type="radio"
                        name="ft8-path"
                        value="short"
                        checked={path === 'short'}
                        onchange={() => onPathChange?.('short')}
                        class="mr-1"
                    />
                    Short
                </label>
                <label class="inline-flex cursor-pointer items-center">
                    <input
                        type="radio"
                        name="ft8-path"
                        value="long"
                        checked={path === 'long'}
                        onchange={() => onPathChange?.('long')}
                        class="mr-1"
                    />
                    Long
                </label>
            </div>
        {/if}
    {/if}
</div>
