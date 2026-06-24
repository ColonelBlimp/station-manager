<script lang="ts">
    // Rigs tab — the rig-profiles editor (ADR 0028; design in
    // docs/v2-design/frontend-spa.md). Master-detail: left = the configured-rig
    // list (✓ marks the active rig) + Add; right = the selected rig's knobs.
    // Edits stage in configState.rigDraft (a whole-catalogue working copy); Save
    // PUTs the whole draft via /v1/config. Hardware pickers enumerate from
    // /v1/hardware — the operator never types a device path.
    //
    // Step 1 (this build): model / port / audio + add / delete / set-default +
    // ft8_mode / my_rig + restart banner. Mode mappings + serial overrides are
    // follow-up steps (collapsible sub-editors).
    import { configState } from '../../states/config.svelte';
    import TabFooter from '../TabFooter.svelte';

    const rig = $derived(configState.selectedRig);
    const rigdef = $derived(rig ? configState.rigdefFor(rig.model) : undefined);
    const audioAvailable = $derived(configState.hardware?.audio.available ?? false);

    // Port options = discovered ports + the stored value if it's not currently
    // detected (so an unplugged-but-configured port still shows, not silently
    // blanks). value = device path (RigConfig.port).
    const portOptions = $derived.by(() => {
        const ports = configState.hardware?.serial_ports ?? [];
        const opts = ports.map((p) => ({ value: p.id, label: p.label }));
        if (rig?.port && !ports.some((p) => p.id === rig.port)) {
            opts.unshift({ value: rig.port, label: `${rig.port} (not detected)` });
        }
        return opts;
    });

    // Add a rig, defaulting to the first model not already configured. Two rigs of
    // the same model is valid (e.g. a pair of identical rigs, each on its own port),
    // but uncommon — so when every catalogue model is already in use (the only case
    // where Add must duplicate), confirm first rather than silently adding a clone.
    function onAddRig(): void {
        const model = configState.nextRigModel();
        if (configState.rigDraft.some((r) => r.model === model)) {
            const name = configState.rigdefFor(model)?.name ?? model;
            if (
                !window.confirm(`A "${name}" is already configured. Add another of the same model?`)
            ) {
                return;
            }
        }
        configState.addRig(model);
    }
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="flex gap-6">
        <!-- Left: rig list -->
        <aside class="w-56 shrink-0">
            <h2 class="mb-2 text-sm font-semibold tracking-wide text-gray-500 uppercase">
                Configured rigs
            </h2>
            {#if configState.rigDraft.length === 0}
                <p class="text-sm text-gray-400">None yet.</p>
            {:else}
                <ul class="space-y-1">
                    {#each configState.rigDraft as r (r.id)}
                        <li>
                            <button
                                type="button"
                                onclick={() => configState.selectRig(r.id)}
                                class="flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm {r.id ===
                                configState.selectedRigId
                                    ? 'bg-indigo-50 text-indigo-800'
                                    : 'text-gray-700 hover:bg-gray-100'}"
                            >
                                <span class="truncate"
                                    >{(configState.rigdefFor(r.model)?.name ?? r.model) ||
                                        'New rig'}</span
                                >
                                {#if r.id === configState.draftDefaultRigId}
                                    <span class="ml-2 shrink-0 text-xs font-medium text-green-700"
                                        >✓ active</span
                                    >
                                {/if}
                            </button>
                        </li>
                    {/each}
                </ul>
            {/if}
            <button
                type="button"
                onclick={onAddRig}
                class="mt-3 w-full cursor-pointer rounded-md border border-dashed border-gray-300 px-3 py-2 text-sm font-medium text-gray-600 hover:border-gray-400 hover:bg-gray-50"
            >
                + Add rig
            </button>
        </aside>

        <!-- Right: selected rig detail -->
        <section class="min-w-0 flex-1">
            {#if !rig}
                <p class="text-sm text-gray-500">Select a rig, or add one.</p>
            {:else}
                <div class="flex items-center justify-between border-b border-gray-200 pb-3">
                    <h2 class="truncate text-base font-semibold text-gray-800">
                        {(rigdef?.name ?? rig.model) || 'New rig'}
                    </h2>
                    <div class="flex shrink-0 items-center gap-2">
                        <button
                            type="button"
                            onclick={() => configState.setDefaultRig(rig.id)}
                            disabled={rig.id === configState.draftDefaultRigId}
                            class="cursor-pointer rounded-md px-2.5 py-1 text-sm font-medium text-indigo-700 hover:bg-indigo-50 disabled:cursor-not-allowed disabled:text-gray-400 disabled:hover:bg-transparent"
                        >
                            {rig.id === configState.draftDefaultRigId
                                ? '✓ Active rig'
                                : 'Set active'}
                        </button>
                        <button
                            type="button"
                            onclick={() => configState.deleteRig(rig.id)}
                            disabled={configState.rigDraft.length <= 1}
                            class="cursor-pointer rounded-md px-2.5 py-1 text-sm font-medium text-red-600 hover:bg-red-50 disabled:cursor-not-allowed disabled:text-gray-400 disabled:hover:bg-transparent"
                            title={configState.rigDraft.length <= 1
                                ? 'Cannot delete the only rig'
                                : 'Delete this rig'}
                        >
                            ✕ Delete
                        </button>
                    </div>
                </div>

                <div class="mt-4 max-w-xl space-y-4">
                    <!-- Model -->
                    <label class="flex flex-col gap-1">
                        <span class="text-sm font-medium text-gray-700">Model</span>
                        <select
                            bind:value={rig.model}
                            class="rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                        >
                            {#each configState.catalogue as def (def.id)}
                                <option value={def.id}>{def.name}</option>
                            {/each}
                        </select>
                    </label>

                    <!-- Port -->
                    <label class="flex flex-col gap-1">
                        <span class="text-sm font-medium text-gray-700">Serial port</span>
                        <select
                            bind:value={rig.port}
                            class="rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                        >
                            <option value="">— select a port —</option>
                            {#each portOptions as p (p.value)}
                                <option value={p.value}>{p.label}</option>
                            {/each}
                        </select>
                    </label>

                    <!-- Audio -->
                    {#if !audioAvailable}
                        <div class="rounded-md bg-gray-50 px-3 py-2 text-sm text-gray-500">
                            Audio device enumeration is unavailable on this build. RX:
                            <span class="font-mono">{rig.audio?.rx || '—'}</span>, TX:
                            <span class="font-mono">{rig.audio?.tx || '—'}</span>.
                        </div>
                    {:else}
                        <div class="flex flex-wrap gap-4">
                            <label class="flex flex-col gap-1">
                                <span class="text-sm font-medium text-gray-700"
                                    >Audio RX (capture)</span
                                >
                                <select
                                    bind:value={rig.audio!.rx}
                                    class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                                >
                                    <option value="">— select —</option>
                                    {#each configState.hardware?.audio.capture ?? [] as d (d.name)}
                                        <option value={d.name}
                                            >{d.name}{d.is_default ? ' (default)' : ''}</option
                                        >
                                    {/each}
                                </select>
                            </label>
                            <label class="flex flex-col gap-1">
                                <span class="text-sm font-medium text-gray-700"
                                    >Audio TX (playback)</span
                                >
                                <select
                                    bind:value={rig.audio!.tx}
                                    class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                                >
                                    <option value="">— select —</option>
                                    {#each configState.hardware?.audio.playback ?? [] as d (d.name)}
                                        <option value={d.name}
                                            >{d.name}{d.is_default ? ' (default)' : ''}</option
                                        >
                                    {/each}
                                </select>
                            </label>
                        </div>
                    {/if}

                    <!-- FT8 mode (override; empty = inherit the rigdef default) -->
                    <label class="flex flex-col gap-1">
                        <span class="text-sm font-medium text-gray-700">FT8 mode</span>
                        <input
                            type="text"
                            value={rig.ft8_mode ?? ''}
                            placeholder={rigdef?.ft8_mode ?? ''}
                            oninput={(e) => (rig.ft8_mode = e.currentTarget.value || null)}
                            class="w-40 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                        />
                        <span class="text-xs text-gray-400">
                            {rig.ft8_mode
                                ? 'Overriding the rigdef default'
                                : `Inheriting rigdef default${rigdef?.ft8_mode ? `: ${rigdef.ft8_mode}` : ''}`}
                        </span>
                    </label>

                    <!-- MY_RIG (override; empty = inherit the rigdef name) -->
                    <label class="flex flex-col gap-1">
                        <span class="text-sm font-medium text-gray-700">MY_RIG (ADIF)</span>
                        <input
                            type="text"
                            value={rig.my_rig ?? ''}
                            placeholder={rigdef?.name ?? ''}
                            oninput={(e) => (rig.my_rig = e.currentTarget.value || null)}
                            class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                        />
                        <span class="text-xs text-gray-400">
                            {rig.my_rig
                                ? 'Overriding the derived rig name'
                                : `Inheriting rig name${rigdef?.name ? `: ${rigdef.name}` : ''}`}
                        </span>
                    </label>
                </div>
            {/if}

            {#if configState.rigsRestartRequired}
                <div
                    class="mt-6 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800"
                >
                    ⚠ Model / port / audio / active-rig changes apply on daemon restart.
                </div>
            {/if}

            <TabFooter
                dirty={configState.rigsDirty}
                saving={configState.savingRigs}
                status={configState.rigsStatus}
                onsave={() => configState.saveRigs()}
                oncancel={() => configState.cancelRigs()}
            />
        </section>
    </div>
{/if}
