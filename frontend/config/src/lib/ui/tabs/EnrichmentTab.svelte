<script lang="ts">
    // Enrichment tab — callsign/country lookup providers + cache freshness
    // (ADR 0017). Option A: specific QRZ + Hamnut sections (not a generic chain
    // editor), since those are the only implemented providers. Provider URLs are
    // defaulted daemon-side, so the operator supplies only credentials. The QRZ
    // password is masked-on-GET — leave it blank to keep the stored secret.
    // Provider changes are restart-only (the enrichment workers bind at startup).
    import { configState } from '../../states/config.svelte';
    import TabFooter from '../TabFooter.svelte';
    import PasswordField from '../PasswordField.svelte';

    const form = $derived(configState.lookupForm);

    function ttlInput(set: (n: number) => void) {
        return (e: Event) => {
            const n = parseInt((e.currentTarget as HTMLInputElement).value.trim(), 10);
            set(Number.isFinite(n) && n >= 0 ? n : 0);
        };
    }
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="mx-auto max-w-xl space-y-8">
        <section>
            <h2 class="text-base font-semibold text-gray-800">Callsign lookup — QRZ.com</h2>
            <p class="mt-0.5 mb-3 text-sm text-gray-500">
                Fills contacted-station details (name, grid, address) from QRZ. Needs a QRZ
                subscription with XML/API access.
            </p>
            <div class="space-y-3">
                <label class="flex items-center gap-2 text-sm text-gray-700">
                    <input type="checkbox" bind:checked={form.qrzEnabled} class="cursor-pointer" />
                    Enabled
                </label>
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Username</span>
                    <input
                        type="text"
                        bind:value={form.qrzUsername}
                        autocomplete="off"
                        spellcheck="false"
                        class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                </label>
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Password</span>
                    <PasswordField
                        value={form.qrzPassword}
                        oninput={(v: string) => (form.qrzPassword = v)}
                        placeholder={form.qrzPasswordSet
                            ? '•••••••• (set — leave blank to keep)'
                            : ''}
                    />
                </label>
            </div>
        </section>

        <section>
            <h2 class="text-base font-semibold text-gray-800">Country lookup — Hamnut</h2>
            <p class="mt-0.5 mb-3 text-sm text-gray-500">
                Resolves DXCC / CQ / ITU zones from the callsign prefix. Free and anonymous — no
                credentials needed.
            </p>
            <label class="flex items-center gap-2 text-sm text-gray-700">
                <input type="checkbox" bind:checked={form.hamnutEnabled} class="cursor-pointer" />
                Enabled
            </label>
        </section>

        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">Cache freshness</h2>
            <div class="flex flex-wrap gap-x-6 gap-y-3">
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Country TTL (days)</span>
                    <input
                        type="text"
                        inputmode="numeric"
                        value={form.countryTtlDays ? String(form.countryTtlDays) : ''}
                        oninput={ttlInput((n) => (form.countryTtlDays = n))}
                        class="w-28 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                </label>
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Station TTL (days)</span>
                    <input
                        type="text"
                        inputmode="numeric"
                        value={form.stationTtlDays ? String(form.stationTtlDays) : ''}
                        oninput={ttlInput((n) => (form.stationTtlDays = n))}
                        class="w-28 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                </label>
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Max refresh in flight</span>
                    <input
                        type="text"
                        inputmode="numeric"
                        value={form.refreshMaxInFlight ? String(form.refreshMaxInFlight) : ''}
                        oninput={ttlInput((n) => (form.refreshMaxInFlight = n))}
                        class="w-28 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                </label>
            </div>
            <p class="mt-2 text-xs text-gray-400">
                0 = never goes stale. TTLs decide when a cached country / station record is
                re-fetched on next use.
            </p>
        </section>

        {#if configState.lookupDirty}
            <div
                class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800"
            >
                ⚠ Enrichment changes apply on daemon restart.
            </div>
        {/if}

        <TabFooter
            dirty={configState.lookupDirty}
            saving={configState.savingLookup}
            status={configState.lookupStatus}
            onsave={() => configState.saveLookup()}
            oncancel={() => configState.cancelLookup()}
        />
    </div>
{/if}
