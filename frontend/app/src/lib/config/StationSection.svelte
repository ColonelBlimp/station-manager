<script lang="ts">
    // Station section — the operator's ADIF station identity (logging_station),
    // the first real section of the app Settings view (ADR 0044). Identity,
    // location + zones, postal address (QSL), antenna, and CW key. lat/lon are
    // DERIVED from the grid square by the daemon on save (not edited here).
    // Loads + saves the daemon config directly (station.svelte.ts → PUT
    // /v1/config); no local cache (ADR 0003).
    import { onMount } from 'svelte';
    import { stationState } from './station.svelte';

    onMount(() => void stationState.load());
</script>

{#snippet field(label: string, key: string, hint?: string, width = 'w-64', readonly = false)}
    <!-- value + oninput rather than bind:value: the key is dynamic (a snippet
         param), and reading/writing the string map by computed key is cleaner
         than a bindable computed-member expression. form is deep-reactive
         $state, so the write drives the dirty check. readonly fields render the
         value (copyable) but can't be edited — oninput never fires for them. -->
    <label class="flex flex-col gap-1 {width}">
        <span class="text-sm font-medium text-ink">{label}</span>
        <!-- hint is the input's native tooltip (title) rather than an inline
             line, so a hinted field doesn't stand taller than its row-mates. -->
        <input
            class="input read-only:cursor-not-allowed read-only:bg-surface-muted read-only:text-muted"
            {readonly}
            title={hint}
            value={stationState.form[key] ?? ''}
            oninput={(e) => (stationState.form[key] = e.currentTarget.value)}
        />
    </label>
{/snippet}

<div class="mx-auto max-w-3xl">
    {#if !stationState.loaded && stationState.loading}
        <p class="text-sm text-muted">Loading…</p>
    {:else if !stationState.loaded && stationState.error}
        <div class="card">
            <p class="text-sm text-ink">Couldn’t load settings: {stationState.error}</p>
            <button class="btn mt-3" onclick={() => stationState.load()}>Retry</button>
        </div>
    {:else}
        <div class="space-y-8">
            <section>
                <h2 class="mb-3 text-base font-semibold text-ink">Station identity</h2>
                <div class="flex flex-wrap gap-x-4 gap-y-3">
                    {@render field(
                        'Station callsign',
                        'station_callsign',
                        'The logging station’s callsign — the callsign used over the air. Set during setup and bound to your logbook, so it’s read-only here.',
                        'w-48',
                        true,
                    )}
                    {@render field(
                        'Operator',
                        'operator',
                        'The logging operator’s callsign — who is at the controls. If blank, the station callsign is used.',
                        'w-48',
                    )}
                    {@render field(
                        'Owner callsign',
                        'owner_callsign',
                        'The callsign of the station’s owner — the operator’s host when operating as a guest. If blank, the station callsign is used.',
                        'w-48',
                    )}
                    {@render field('Name', 'my_name')}
                    {@render field('Activity program', 'my_sig', 'ADIF MY_SIG — e.g. POTA, SOTA, WWFF.', 'w-40')}
                    {@render field(
                        'Activity reference',
                        'my_sig_info',
                        'ADIF MY_SIG_INFO — e.g. K-1234.',
                        'w-40',
                    )}
                </div>
            </section>

            <section>
                <h2 class="mb-3 text-base font-semibold text-ink">Location &amp; zones</h2>
                <div class="space-y-3">
                    <div class="flex flex-wrap gap-x-4 gap-y-3">
                        {@render field('Country', 'my_country')}
                        {@render field('DXCC', 'my_dxcc', undefined, 'w-28')}
                        {@render field('Altitude (m)', 'my_altitude', undefined, 'w-32')}
                    </div>
                    <div class="flex flex-wrap gap-x-4 gap-y-3">
                        {@render field(
                            'Grid square',
                            'my_gridsquare',
                            'Maidenhead locator — lat/lon are derived from this on save.',
                            'w-40',
                        )}
                        {@render field('CQ Zone', 'my_cq_zone', undefined, 'w-28')}
                        {@render field('ITU Zone', 'my_itu_zone', undefined, 'w-28')}
                    </div>
                </div>
            </section>

            <section>
                <h2 class="mb-3 text-base font-semibold text-ink">Postal address (for QSL cards)</h2>
                <div class="flex flex-wrap gap-x-4 gap-y-3">
                    {@render field('Street', 'my_street', undefined, 'w-full max-w-[38rem]')}
                    {@render field('City', 'my_city')}
                    {@render field('Postal code', 'my_postal_code', undefined, 'w-40')}
                </div>
            </section>

            <section>
                <h2 class="mb-3 text-base font-semibold text-ink">Equipment</h2>
                <div class="flex flex-wrap gap-x-4 gap-y-3">
                    {@render field('Antenna', 'my_antenna', undefined, 'w-full max-w-[38rem]')}
                </div>
            </section>

            <section>
                <h2 class="mb-3 text-base font-semibold text-ink">CW</h2>
                <div class="flex flex-wrap gap-x-4 gap-y-3">
                    {@render field('Morse key type', 'my_morse_key_type')}
                    {@render field('Morse key info', 'my_morse_key_info')}
                </div>
            </section>

            <div class="flex items-center gap-3 border-t border-line pt-4">
                <button
                    class="btn btn-primary"
                    disabled={!stationState.dirty || stationState.saving}
                    onclick={() => stationState.save()}
                >
                    {stationState.saving ? 'Saving…' : 'Save'}
                </button>
                <button
                    class="btn"
                    disabled={!stationState.dirty || stationState.saving}
                    onclick={() => stationState.reset()}
                >
                    Cancel
                </button>
                {#if stationState.dirty}
                    <span class="text-xs text-muted">Unsaved changes</span>
                {/if}
            </div>
        </div>
    {/if}
</div>
