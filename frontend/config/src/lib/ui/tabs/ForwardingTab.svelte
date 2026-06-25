<script lang="ts">
    // Forwarding tab — manage the operator's forwarding destinations (QRZ,
    // ClubLog, …). Data-driven: the credential fields per destination come from
    // GET /v1/forwarder-types (no hardcoded per-type forms). Secrets are
    // masked-on-GET — an already-set credential shows a placeholder; leaving a
    // field blank keeps the stored secret (the daemon merges on PUT). Forwarder
    // changes are restart-only (the worker binds config at startup).
    import { configState } from '../../states/config.svelte';
    import TabFooter from '../TabFooter.svelte';
    import PasswordField from '../PasswordField.svelte';

    // Add-destination type picker (defaults to the first available type).
    let addType: string = $state('');
    $effect(() => {
        if (addType === '' && configState.forwarderTypes.length > 0) {
            addType = configState.forwarderTypes[0].type;
        }
    });

    function isSet(f: { credentialsSet: string[] }, key: string): boolean {
        return f.credentialsSet.includes(key);
    }
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="mx-auto max-w-2xl space-y-5">
        <p class="text-sm text-gray-500">
            Forwarding uploads each new QSO to the services you configure here. Adding a destination
            queues <em>future</em> QSOs; credentials are stored on the daemon and never sent back to the
            browser (leave a field blank to keep the saved value).
        </p>

        {#if configState.forwarderDraft.length === 0}
            <p class="text-sm text-gray-400">No destinations configured yet.</p>
        {/if}

        {#each configState.forwarderDraft as f, i (i)}
            {@const td = configState.forwarderTypeFor(f.type)}
            <div class="rounded-md border border-gray-200 p-4">
                <div class="flex items-center justify-between gap-3">
                    <div class="flex items-center gap-3">
                        <span class="font-semibold text-gray-800">{td?.display_name ?? f.type}</span
                        >
                        <input
                            type="text"
                            bind:value={f.name}
                            aria-label="Destination name"
                            class="w-40 rounded-md border border-gray-300 px-2 py-1 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                        />
                    </div>
                    <div class="flex items-center gap-3">
                        <label class="flex items-center gap-1.5 text-sm text-gray-700">
                            <input
                                type="checkbox"
                                bind:checked={f.enabled}
                                class="cursor-pointer"
                            />
                            Enabled
                        </label>
                        <button
                            type="button"
                            onclick={() => configState.removeForwarder(i)}
                            class="cursor-pointer rounded-md px-2 py-1 text-sm font-medium text-red-600 hover:bg-red-50"
                        >
                            ✕ Remove
                        </button>
                    </div>
                </div>

                {#if !td}
                    <p class="mt-3 text-sm text-amber-700">
                        This forwarder type isn't supported by this daemon build — it can be removed
                        or left as-is, but its credentials can't be edited here.
                    </p>
                {:else}
                    <div class="mt-4 space-y-3">
                        {#each td.credential_fields as field (field.key)}
                            <label class="flex flex-col gap-1">
                                <span class="text-sm font-medium text-gray-700">{field.label}</span>
                                {#if field.kind === 'password'}
                                    <PasswordField
                                        value={f.credentials[field.key] ?? ''}
                                        oninput={(v: string) => (f.credentials[field.key] = v)}
                                        placeholder={isSet(f, field.key)
                                            ? '•••••••• (set — leave blank to keep)'
                                            : ''}
                                        widthClass="w-full"
                                    />
                                {:else}
                                    <input
                                        type="text"
                                        value={f.credentials[field.key] ?? ''}
                                        oninput={(e) =>
                                            (f.credentials[field.key] = e.currentTarget.value)}
                                        placeholder={isSet(f, field.key)
                                            ? '•••••••• (set — leave blank to keep)'
                                            : ''}
                                        autocomplete="off"
                                        spellcheck="false"
                                        class="w-full rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                                    />
                                {/if}
                                {#if field.help}
                                    <span class="text-xs text-gray-400">{field.help}</span>
                                {/if}
                            </label>
                        {/each}
                    </div>
                {/if}
            </div>
        {/each}

        <!-- Add destination -->
        <div class="flex items-center gap-3">
            {#if configState.forwarderTypes.length === 0}
                <p class="text-sm text-gray-400">No forwarder types available from the daemon.</p>
            {:else}
                <select
                    bind:value={addType}
                    aria-label="New destination type"
                    class="rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                >
                    {#each configState.forwarderTypes as t (t.type)}
                        <option value={t.type}>{t.display_name}</option>
                    {/each}
                </select>
                <button
                    type="button"
                    onclick={() => configState.addForwarder(addType)}
                    class="cursor-pointer rounded-md border border-dashed border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-600 hover:border-gray-400 hover:bg-gray-50"
                >
                    + Add destination
                </button>
            {/if}
        </div>

        {#if configState.forwardersDirty}
            <div
                class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800"
            >
                ⚠ Forwarding changes apply on daemon restart.
            </div>
        {/if}

        <TabFooter
            dirty={configState.forwardersDirty}
            saving={configState.savingForwarders}
            status={configState.forwardersStatus}
            onsave={() => configState.saveForwarders()}
            oncancel={() => configState.cancelForwarders()}
        />
    </div>
{/if}
