<script lang="ts">
    // Always-on callsign enrichment display: flag + country, DXCC (+ NEW badge),
    // bearing + distance for the operator-selected propagation path (SP/LP radio
    // group, shared via enrich.prefs so the future rotator drives the same
    // heading), and the destination's local clock. Reads the shared enrich state
    // and the QSO draft's callsign; owns no lookup logic (enrich.svelte does) and
    // makes no positioning assumptions (ADR 0045) — it fills whatever box the
    // host gives it. First host: the logging card's right-hand square; cards will
    // be repositionable later, so nothing here may depend on that placement.
    import { draft } from './qso.svelte';
    import { enrich, station, prefs, observeCall } from './enrich.svelte';
    import { ccodeToFlag } from '../utils/flag';
    import { pathInfo, gridToDecimal } from '../utils/bearing';

    // Namespaces the radio group per instance, so a relocated/duplicated card
    // can't capture another instance's SP/LP clicks.
    const uid = $props.id();

    // The card is the consumer of the callsign field — it drives the lookup.
    $effect(() => observeCall(draft.callsign));

    const flag = $derived(ccodeToFlag(enrich.data?.ccode));
    // Bearing/distance need both ends; null (→ hidden) when either grid is
    // missing or invalid. Bearing stays a first-class value here so a future
    // rotator control can consume the same derivation.
    const path = $derived(
        enrich.data !== null && enrich.data.grid !== '' && station.myGrid !== ''
            ? pathInfo(station.myGrid, enrich.data.grid)
            : null
    );
    const shown = $derived(
        path === null
            ? null
            : prefs.path === 'sp'
              ? { bearing: path.shortPathBearing, km: path.shortPathDistanceKm }
              : { bearing: path.longPathBearing, km: path.longPathDistanceKm }
    );

    // Minute-resolution ticker for the destination clock; only worth the
    // interval while data is showing.
    let nowMs = $state(Date.now());
    $effect(() => {
        if (enrich.data === null) return;
        nowMs = Date.now();
        const t = setInterval(() => (nowMs = Date.now()), 15_000);
        return () => clearInterval(t);
    });

    // Destination local time, derived from the grid's longitude (15° per hour,
    // rounded to the whole solar hour). An approximation by design: the real
    // civil timezone (political boundaries, DST) needs a tz source the
    // enrichment API doesn't provide yet — good enough to know whether you're
    // calling someone at 3 a.m.
    const destTime = $derived.by(() => {
        const grid = enrich.data?.grid ?? '';
        const coords = grid !== '' ? gridToDecimal(grid) : null;
        if (coords === null) return null;
        const offsetH = Math.round(coords.lon / 15);
        const d = new Date(nowMs + offsetH * 3_600_000);
        const hh = String(d.getUTCHours()).padStart(2, '0');
        const mm = String(d.getUTCMinutes()).padStart(2, '0');
        return {
            clock: `${hh}:${mm}`,
            offset: `UTC${offsetH < 0 ? '−' : '+'}${Math.abs(offsetH)}`,
        };
    });
</script>

<div class="flex h-full w-full flex-col rounded-lg border border-line bg-surface-muted p-3">
    {#if enrich.status === 'done' && enrich.data !== null}
        <div class="flex items-center gap-x-2">
            {#if flag !== ''}
                <span class="text-5xl leading-none text-nowrap, overflow-x-hidden, text-ellipsis" title={enrich.data.country}>{flag}</span>
            {/if}
            <div class="min-w-0">
                <div class="truncate text-base font-semibold text-ink">{enrich.data.country}</div>
                {#if enrich.data.dxcc !== null}
                    <div class="text-sm text-muted">
                        DXCC {enrich.data.dxcc}
                        {#if enrich.data.isNewEntity === true}
                            <span
                                class="ml-1 rounded bg-focus px-1 py-px text-[10px] font-bold tracking-wide text-white uppercase"
                            >
                                New
                            </span>
                        {/if}
                    </div>
                {/if}
            </div>
        </div>

        <div class="mt-3 space-y-1 text-sm">
            {#if shown !== null}
                <div class="text-center">
                        <span class="tabular-nums text-ink font-semibold">
                        {shown.bearing.toFixed(0)}° · {shown.km.toLocaleString()} km
                    </span>
                </div>
                <div class="flex items-center mt-3 justify-between">
                    <fieldset class="flex items-center gap-x-3">
                        <legend class="sr-only">Propagation path</legend>
                        <label class="flex items-center gap-x-1 text-muted" title="Short path">
                            <input
                                type="radio"
                                name="{uid}-path"
                                value="sp"
                                bind:group={prefs.path}
                                class="accent-[--color-focus]"
                            />
                            Short Path
                        </label>
                        <label class="flex items-center gap-x-1 text-muted" title="Long path">
                            <input
                                type="radio"
                                name="{uid}-path"
                                value="lp"
                                bind:group={prefs.path}
                                class="accent-[--color-focus]"
                            />
                            Long Path
                        </label>
                    </fieldset>
                </div>
            {/if}
            {#if destTime !== null}
                <div class="flex mt-3 items-center justify-between">
                    <span class="text-muted">Their time</span>
                    <span class="tabular-nums text-ink">
                        {destTime.clock} · {destTime.offset}
                    </span>
                </div>
            {/if}
        </div>

    {:else if enrich.status === 'pending'}
        <div class="flex h-full items-center justify-center text-sm text-muted">
            Looking up {enrich.call}…
        </div>
    {:else}
        <!-- idle, or done-with-nothing (fail-soft / unknown call) -->
        <div class="flex h-full items-center justify-center text-sm text-muted">
            {enrich.status === 'done' ? 'No match' : ''}
        </div>
    {/if}
</div>
