<script lang="ts">
    // General tab — cross-cutting operator preferences that don't belong to a
    // domain tab (Station/Rigs/FT8/…), plus the read-only About/version
    // diagnostics (moved here from the logging SPA's My Station → About,
    // 2026-06-26). Daemon/system internals (server port, timeouts, paths) stay
    // out — they're config.json-only/advanced.
    import { onMount } from 'svelte';
    import { configState } from '../../states/config.svelte';
    import { fetchVersion, type VersionResponse } from '../../api/version';
    import TabFooter from '../TabFooter.svelte';

    // Contacts-map default palette — MUST match the map's built-in palette in
    // frontend/app/src/lib/map/bandColors.ts DEFAULT_BAND_COLORS (this editor
    // shows the default a band falls back to; the map applies it). Kept as a
    // literal copy: the two SPAs are separate packages, and 14 pinned pairs are
    // cheaper than a shared-module build seam.
    const MAP_DEFAULTS: { band: string; color: string }[] = [
        { band: '160m', color: '#ef4444' },
        { band: '80m', color: '#f97316' },
        { band: '60m', color: '#f59e0b' },
        { band: '40m', color: '#eab308' },
        { band: '30m', color: '#84cc16' },
        { band: '20m', color: '#22c55e' },
        { band: '17m', color: '#14b8a6' },
        { band: '15m', color: '#06b6d4' },
        { band: '12m', color: '#3b82f6' },
        { band: '10m', color: '#6366f1' },
        { band: '6m', color: '#8b5cf6' },
        { band: '4m', color: '#a855f7' },
        { band: '2m', color: '#d946ef' },
        { band: '70cm', color: '#ec4899' },
    ];

    // A pick equal to the default is "no override" — the stored block stays
    // sparse, so a future default-palette improvement reaches untouched bands.
    function pickBandColor(band: string, value: string, def: string): void {
        if (value.toLowerCase() === def) {
            delete configState.mapBandColors[band];
        } else {
            configState.mapBandColors[band] = value.toLowerCase();
        }
    }

    // About/version — fetched when the tab first mounts (it mounts on selection),
    // re-fetchable via Refresh so a freshly-deployed daemon build shows up without
    // reloading the SPA. Pure diagnostics, local to this component.
    let versionInfo: VersionResponse | null = $state(null);
    let versionLoading: boolean = $state(false);
    let versionError: string | null = $state(null);

    async function loadVersion(): Promise<void> {
        if (versionLoading) return;
        versionLoading = true;
        versionError = null;
        const outcome = await fetchVersion();
        if (outcome.kind === 'ok') {
            versionInfo = outcome.version;
        } else {
            versionError =
                outcome.kind === 'network'
                    ? 'Cannot reach the daemon.'
                    : 'Daemon returned an unexpected response.';
        }
        versionLoading = false;
    }

    onMount(loadVersion);
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="mx-auto max-w-xl space-y-8">
        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">Operating</h2>
            <label class="flex items-start gap-2 text-sm text-gray-700">
                <input
                    type="checkbox"
                    bind:checked={configState.restoreRigOnModeSwitch}
                    class="mt-0.5 cursor-pointer"
                />
                <span>
                    Restore the rig when switching operating mode
                    <span class="mt-0.5 block text-xs text-gray-400">
                        On a Phone/CW ↔ FT8 switch, return the rig to that mode's last frequency and
                        mode. When a rig is connected (CAT) this re-tunes the rig; with no rig it
                        just restores the displayed values. Turn off to leave the rig wherever the
                        other mode left it.
                    </span>
                </span>
            </label>
        </section>

        <section>
            <h2 class="mb-1 text-base font-semibold text-gray-800">Contacts map</h2>
            <p class="mb-3 text-sm text-gray-500">
                Arc colour per band on the contacts map. A band left on its default stays on the
                built-in palette (so palette improvements still reach it); a changed colour is
                stored as your override. Bands not listed here fall back to gray on the map.
            </p>
            <div class="grid grid-cols-2 gap-x-8 gap-y-1.5">
                {#each MAP_DEFAULTS as d (d.band)}
                    {@const overridden = configState.mapBandColors[d.band] !== undefined}
                    <div class="flex items-center gap-2 text-sm text-gray-700">
                        <input
                            type="color"
                            class="h-6 w-9 cursor-pointer rounded border border-gray-300"
                            value={configState.mapBandColors[d.band] ?? d.color}
                            aria-label={`${d.band} arc colour`}
                            onchange={(e) => pickBandColor(d.band, e.currentTarget.value, d.color)}
                        />
                        <span class="w-12 font-mono">{d.band}</span>
                        {#if overridden}
                            <button
                                type="button"
                                class="cursor-pointer text-xs text-indigo-700 hover:text-indigo-900"
                                onclick={() => pickBandColor(d.band, d.color, d.color)}
                                >reset</button
                            >
                        {:else}
                            <span class="text-xs text-gray-400">default</span>
                        {/if}
                    </div>
                {/each}
            </div>

            <TabFooter
                dirty={configState.generalDirty}
                saving={configState.savingGeneral}
                status={configState.generalStatus}
                onsave={() => configState.saveGeneral()}
                oncancel={() => configState.cancelGeneral()}
            />
        </section>

        <section>
            <h2 class="text-base font-semibold text-gray-800">About</h2>
            <p class="mt-0.5 mb-3 text-sm text-gray-500">
                Station Manager — local QSO log + forwarding daemon. Licensed GPL-3.0-only.
            </p>
            {#if versionLoading}
                <p class="text-sm text-gray-500">Loading…</p>
            {:else if versionError !== null}
                <p class="text-sm text-red-700">{versionError}</p>
            {:else if versionInfo !== null}
                <dl class="grid grid-cols-[7rem_1fr] gap-y-1 text-sm">
                    <dt class="font-semibold text-gray-700">Version</dt>
                    <dd class="font-mono text-gray-700">{versionInfo.daemon}</dd>

                    <dt class="font-semibold text-gray-700">Go runtime</dt>
                    <dd class="font-mono text-gray-700">{versionInfo.go}</dd>

                    <dt class="font-semibold text-gray-700">DB schema</dt>
                    <dd class="font-mono text-gray-700">
                        {#if versionInfo.schema !== undefined}
                            {versionInfo.schema.version}{versionInfo.schema.dirty ? ' (dirty)' : ''}
                        {:else}
                            unavailable
                        {/if}
                    </dd>
                </dl>
            {/if}
            <div class="mt-3">
                <button
                    type="button"
                    onclick={loadVersion}
                    disabled={versionLoading}
                    class="cursor-pointer text-sm text-indigo-700 hover:text-indigo-900 disabled:cursor-not-allowed disabled:opacity-50"
                    >Refresh</button
                >
            </div>
        </section>
    </div>
{/if}
