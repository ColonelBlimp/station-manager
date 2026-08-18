<script lang="ts">
    // General section — cross-cutting operator preferences that don't belong to a
    // domain section (Station/Rigs/FT8/…): the mode-switch rig-restore knob and the
    // contacts-map band-colour overrides, plus read-only About/build diagnostics.
    // Ported from the config SPA's General tab (ADR 0044). Daemon/system internals
    // (port, timeouts, paths) stay config.json-only. Loads + saves via
    // general.svelte.ts → PUT /v1/config (ADR 0003, no local cache).
    import { onMount } from 'svelte';
    import { generalState } from './general.svelte';
    import { DEFAULT_BAND_COLORS } from '../map/bandColors';

    onMount(() => void generalState.load());

    // Editor rows in the map's spectrum (wavelength) order — the same palette the
    // map applies, so each row shows the default a band falls back to.
    const bands = Object.entries(DEFAULT_BAND_COLORS).map(([band, color]) => ({ band, color }));
</script>

<div class="mx-auto max-w-2xl">
    {#if !generalState.loaded && generalState.loading}
        <p class="text-sm text-muted">Loading…</p>
    {:else if !generalState.loaded && generalState.error}
        <div class="card">
            <p class="text-sm text-ink">Couldn’t load settings: {generalState.error}</p>
            <button class="btn mt-3" onclick={() => generalState.load()}>Retry</button>
        </div>
    {:else}
        <div class="space-y-8">
            <section>
                <h2 class="mb-3 text-base font-semibold text-ink">Operating</h2>
                <label class="flex items-start gap-2 text-sm text-ink">
                    <!-- checked + onchange, not bind, mirroring the app's other sections: the
                         write to the deep-reactive form drives the dirty check. -->
                    <input
                        type="checkbox"
                        class="mt-0.5 cursor-pointer"
                        checked={generalState.form.restoreRigOnModeSwitch}
                        onchange={(e) =>
                            (generalState.form.restoreRigOnModeSwitch = e.currentTarget.checked)}
                    />
                    <span>
                        Restore the rig when switching operating mode
                        <span class="mt-0.5 block text-xs text-muted">
                            On a Phone/CW ↔ FT8 switch, return the rig to that mode's last frequency
                            and mode. When a rig is connected (CAT) this re-tunes the rig; with no
                            rig it just restores the displayed values. Turn off to leave the rig
                            wherever the other mode left it.
                        </span>
                    </span>
                </label>
            </section>

            <section>
                <h2 class="mb-1 text-base font-semibold text-ink">Contacts map</h2>
                <p class="mb-3 text-sm text-muted">
                    Arc colour per band on the contacts map. A band left on its default stays on the
                    built-in palette (so palette improvements still reach it); a changed colour is
                    stored as your override. Bands not listed here fall back to gray on the map.
                </p>
                <div class="grid grid-cols-2 gap-x-8 gap-y-1.5">
                    {#each bands as b (b.band)}
                        {@const overridden = generalState.form.bandColors[b.band] !== undefined}
                        <div class="flex items-center gap-2 text-sm text-ink">
                            <input
                                type="color"
                                class="h-6 w-9 cursor-pointer rounded border border-line"
                                value={generalState.form.bandColors[b.band] ?? b.color}
                                aria-label={`${b.band} arc colour`}
                                onchange={(e) =>
                                    generalState.setBandColor(
                                        b.band,
                                        e.currentTarget.value,
                                        b.color
                                    )}
                            />
                            <span class="w-12 font-mono">{b.band}</span>
                            {#if overridden}
                                <button
                                    type="button"
                                    class="cursor-pointer text-xs text-focus hover:underline"
                                    onclick={() =>
                                        generalState.setBandColor(b.band, b.color, b.color)}
                                    >reset</button
                                >
                            {:else}
                                <span class="text-xs text-muted">default</span>
                            {/if}
                        </div>
                    {/each}
                </div>
            </section>

            <div class="flex items-center gap-3 border-t border-line pt-4">
                <button
                    class="btn btn-primary"
                    disabled={!generalState.dirty || generalState.saving}
                    onclick={() => generalState.save()}
                >
                    {generalState.saving ? 'Saving…' : 'Save'}
                </button>
                <button
                    class="btn"
                    disabled={!generalState.dirty || generalState.saving}
                    onclick={() => generalState.reset()}
                >
                    Cancel
                </button>
                {#if generalState.dirty}
                    <span class="text-xs text-muted">Unsaved changes</span>
                {/if}
            </div>

            <section>
                <h2 class="text-base font-semibold text-ink">About</h2>
                <p class="mt-0.5 mb-3 text-sm text-muted">
                    Station Manager — local QSO log + forwarding daemon. Licensed GPL-3.0-only.
                </p>
                {#if generalState.buildLoading}
                    <p class="text-sm text-muted">Loading…</p>
                {:else if generalState.buildError}
                    <p class="text-sm text-ink">Couldn’t reach the daemon.</p>
                {:else if generalState.buildInfo}
                    <dl class="grid grid-cols-[7rem_1fr] gap-y-1 text-sm">
                        <dt class="font-semibold text-ink">Version</dt>
                        <dd class="font-mono text-ink">{generalState.buildInfo.daemon}</dd>
                        <dt class="font-semibold text-ink">Go runtime</dt>
                        <dd class="font-mono text-ink">{generalState.buildInfo.go}</dd>
                        <dt class="font-semibold text-ink">DB schema</dt>
                        <dd class="font-mono text-ink">
                            {#if generalState.buildInfo.schema}
                                {generalState.buildInfo.schema.version}{generalState.buildInfo
                                    .schema.dirty
                                    ? ' (dirty)'
                                    : ''}
                            {:else}
                                unavailable
                            {/if}
                        </dd>
                    </dl>
                {/if}
                <div class="mt-3">
                    <button
                        type="button"
                        onclick={() => generalState.loadBuildInfo()}
                        disabled={generalState.buildLoading}
                        class="cursor-pointer text-sm text-focus hover:underline disabled:cursor-not-allowed disabled:opacity-50"
                        >Refresh</button
                    >
                </div>
            </section>
        </div>
    {/if}
</div>
