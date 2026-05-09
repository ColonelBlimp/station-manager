<script lang="ts">
    /*
        Country panel — displays the contacted station's country
        details from the latest enrichment response. Reads `enrichmentState`
        (set by `QsoPanel.handleEnrich`, cleared by `QsoPanel.submitQso`)
        so the panel auto-populates on Tab and clears after each
        logged QSO per the UX rule "every QSO is a clean slate."

        Layout follows the operator's mockup: country name with `*` if
        new DXCC entity, distance pair, bearing pair, short/long radio
        selector, local time + offset. Active path's distance + bearing
        are styled in indigo (the project's accent colour); inactive
        stays in default ink so the operator's eye lands on the
        currently-relevant numbers without them shouting.

        Empty state (no enrichment yet, or post-submit): em-dashes for
        each field. The panel structure stays visible so the operator
        gets a stable visual landmark rather than a layout shift on
        first Tab.

        Bearing util `pathInfo()` takes Maidenhead grid strings; it
        returns null when either grid is absent. The operator's grid
        comes from `configState.loggingStation.myGridsquare` (My
        Station panel); the station's grid from QRZ via the daemon's
        enrichment response.
    */
    import { enrichmentState, type Path } from '../../states/enrichment.svelte';

    /*
        Format an RFC 3339 timestamp as HH:MM by direct substring
        extraction. The string carries its own timezone (+HH:MM
        suffix) — so the displayed value is the contacted station's
        local time, not the browser's timezone. Going through
        `new Date(...).toLocaleTimeString(...)` would re-shift to the
        browser TZ and lose the daemon's recompute work.
    */
    function formatLocalTimeHHMM(rfc3339: string | undefined): string {
        if (rfc3339 === undefined || rfc3339.length < 16) return '';
        return rfc3339.substring(11, 16);
    }

    function selectPath(p: Path): void {
        enrichmentState.path = p;
    }

    /*
        Class helpers — keeps the template terse and the
        active/inactive switch in one place. Indigo is the project's
        accent colour (also used for the InfoPanel active tab and the
        focus ring); reusing it here reads as "this is the live
        selection" without introducing a new colour.
    */
    const activeClass = 'text-indigo-700 font-semibold';
    const inactiveClass = 'text-ink';
</script>

<!--
    Outer card always renders so the layout doesn't shift between
    populated and empty states; the content INSIDE the card is hidden
    via Tailwind's `hidden` (display: none) when no enrichment result
    exists. Operator sees a stable bordered placeholder pre-Tab and
    post-submit; on Tab the populated content appears in the existing
    frame.
-->
<div class="w-full pt-6 pr-6">
    <div class="w-60 h-62 border border-line rounded-md bg-surface-muted px-4 py-4 text-center">
        <div class={enrichmentState.result === null ? 'hidden' : ''}>
            <!-- Country name + new-DXCC marker -->
            <div class="text-lg font-bold mb-2">
                {enrichmentState.result?.country?.name ??
                    ''}{#if enrichmentState.result?.country?.is_new_entity}<span
                        title="New DXCC entity"
                    >
                        *</span
                    >{/if}
            </div>

            <!-- Flag — flag-icons CSS class; ccode is ISO 3166-1 alpha-2
                 from hamnut, lowercased for the .fi-XX selector. A
                 missing/unknown ccode falls through to no flag rather
                 than a broken image. -->
            {#if enrichmentState.result?.country?.ccode}
                <div class="mb-2 flex justify-center">
                    <span
                        class="fi fi-{enrichmentState.result.country.ccode.toLowerCase()} w-22 aspect-4/3"
                        role="img"
                        aria-label="{enrichmentState.result.country.name ?? ''} flag"
                    ></span>
                </div>
            {/if}

            <!-- Distance pair -->
            {#if enrichmentState.paths !== null}
                <div class="mb-1 tabular-nums">
                    <span class={enrichmentState.path === 'short' ? activeClass : inactiveClass}>
                        {enrichmentState.paths.shortPathDistanceKm} km
                    </span>
                    <span class="text-ink"> / </span>
                    <span class={enrichmentState.path === 'long' ? activeClass : inactiveClass}>
                        {enrichmentState.paths.longPathDistanceKm} km
                    </span>
                </div>

                <!-- Bearing pair -->
                <div class="mb-3 tabular-nums">
                    <span class={enrichmentState.path === 'short' ? activeClass : inactiveClass}>
                        {enrichmentState.paths.shortPathBearing.toFixed(1)}&deg;
                    </span>
                    <span class="text-ink"> / </span>
                    <span class={enrichmentState.path === 'long' ? activeClass : inactiveClass}>
                        {enrichmentState.paths.longPathBearing.toFixed(1)}&deg;
                    </span>
                </div>

                <!-- Path selector — only meaningful when paths are
                     available (operator and station both have a grid).
                     Hidden alongside distance/bearing when not. -->
                <div class="flex justify-center gap-x-3 mb-3 text-sm">
                    <label class="inline-flex items-center cursor-pointer">
                        <input
                            type="radio"
                            name="country-path"
                            value="short"
                            checked={enrichmentState.path === 'short'}
                            onchange={() => selectPath('short')}
                            class="mr-1"
                        />
                        Short path
                    </label>
                    <label class="inline-flex items-center cursor-pointer">
                        <input
                            type="radio"
                            name="country-path"
                            value="long"
                            checked={enrichmentState.path === 'long'}
                            onchange={() => selectPath('long')}
                            class="mr-1"
                        />
                        Long path
                    </label>
                </div>
            {/if}

            <!-- Local time at the contacted station, with the offset for
                 clarity. Recomputed daemon-side on every enrichment so
                 the value is fresh per Tab; doesn't auto-tick — the
                 operator gets a fresh value when they look up the next
                 callsign. -->
            {#if enrichmentState.result?.country?.local_time}
                <div class="text-sm">
                    <span class="font-semibold">Local time:</span>
                    <span class="ml-1 tabular-nums {activeClass}"
                        >{formatLocalTimeHHMM(enrichmentState.result.country.local_time)}</span
                    >
                    {#if enrichmentState.result.country.time_offset}
                        <span class="ml-1 text-ink"
                            >({enrichmentState.result.country.time_offset})</span
                        >
                    {/if}
                </div>
            {/if}
        </div>
    </div>
</div>
