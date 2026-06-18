<script lang="ts">
    import { onMount } from 'svelte';
    import { configState } from './lib/states/config.svelte';

    // Slice 3 — plumbing only: load the daemon's config + rig-profiles data on
    // mount and prove it flows into configState. The rig-profiles editor UX is
    // slice 4 (a separate design pass); this view is a temporary readout, not
    // the final surface.
    onMount(() => {
        void configState.load();
    });
</script>

<main class="min-h-screen bg-gray-50 p-8 font-sans">
    <h1 class="text-2xl font-semibold text-gray-900">Station Manager — Config</h1>

    {#if !configState.loaded}
        <p class="mt-2 text-gray-600">Loading…</p>
    {:else}
        {#if configState.error}
            <p class="mt-2 rounded bg-red-50 px-2 py-1 text-sm text-red-700">{configState.error}</p>
        {/if}

        <p class="mt-2 text-gray-600">
            Setup complete: <strong>{configState.config?.setup_complete ? 'yes' : 'no'}</strong>
            · Station callsign:
            <strong>{configState.config?.logging_station.station_callsign || '—'}</strong>
        </p>

        <section class="mt-6">
            <h2 class="font-semibold text-gray-800">
                Configured rigs ({configState.rigs.length}) · default #{configState.defaultRigId}
            </h2>
            {#if configState.rigs.length === 0}
                <p class="text-sm text-gray-500">None configured yet.</p>
            {:else}
                <ul class="mt-1 text-sm text-gray-700">
                    {#each configState.rigs as rig (rig.id)}
                        <li>
                            #{rig.id}
                            {configState.rigdefFor(rig.model)?.name ?? rig.model}
                            @ {rig.port || '—'}
                            {#if rig.id === configState.defaultRigId}<span class="text-green-700"
                                    >✓ default</span
                                >{/if}
                        </li>
                    {/each}
                </ul>
            {/if}
        </section>

        <section class="mt-6">
            <h2 class="font-semibold text-gray-800">
                Rig catalogue ({configState.catalogue.length})
            </h2>
            <ul class="mt-1 text-sm text-gray-700">
                {#each configState.catalogue as def (def.id)}
                    <li>
                        {def.name} — FT8 {def.ft8_mode || '—'}, {def.rig_modes?.length ?? 0} modes
                    </li>
                {/each}
            </ul>
        </section>

        <!--
            FT8 highlight colours — moved here from the logging SPA's FT8 Settings
            tab (this is their home now). Holding-place UI: three native colour
            pickers + a Save that PUTs /v1/config (echoing logging_station/station so
            the unconditional overwrite can't wipe identity; see api/config.ts).
        -->
        <section class="mt-6 max-w-sm">
            <h2 class="font-semibold text-gray-800">FT8 Band Activity colours</h2>
            <div class="mt-2 flex flex-col gap-2 text-sm text-gray-700">
                <label class="flex items-center justify-between gap-3">
                    <span>CQ — not worked on this band</span>
                    <input
                        type="color"
                        bind:value={configState.highlightUnworked}
                        class="h-7 w-12 cursor-pointer rounded border border-gray-300"
                        aria-label="Not-worked highlight colour"
                    />
                </label>
                <label class="flex items-center justify-between gap-3">
                    <span>CQ — worked before (dupe)</span>
                    <input
                        type="color"
                        bind:value={configState.highlightWorked}
                        class="h-7 w-12 cursor-pointer rounded border border-gray-300"
                        aria-label="Worked-before highlight colour"
                    />
                </label>
                <label class="flex items-center justify-between gap-3">
                    <span>Station calling you</span>
                    <input
                        type="color"
                        bind:value={configState.highlightCalling}
                        class="h-7 w-12 cursor-pointer rounded border border-gray-300"
                        aria-label="Calling-you highlight colour"
                    />
                </label>
            </div>
            <div class="mt-3 flex items-center gap-3">
                <button
                    type="button"
                    onclick={() => void configState.saveColours()}
                    disabled={configState.savingColours}
                    class="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                >
                    {configState.savingColours ? 'Saving…' : 'Save'}
                </button>
                {#if configState.coloursStatus === 'ok'}
                    <span class="text-sm text-green-700">Saved.</span>
                {:else if configState.coloursStatus}
                    <span class="text-sm text-red-700">{configState.coloursStatus}</span>
                {/if}
            </div>
        </section>
    {/if}
</main>
