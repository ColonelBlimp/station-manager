<script lang="ts">
    // Station tab — the set-once LoggingStation MY_* fields (design 2026-06-24).
    // Operational identity (callsign / operator / grid) stays in the logging
    // SPA; this holds what changes only rarely: the cross-border location facts,
    // postal address (for QSL), antenna, and CW key. Grouped sections, not
    // nested tabs. Saves via configState.saveStation() (PUT /v1/config).
    import { configState } from '../../states/config.svelte';
    import Field from '../Field.svelte';
    import TabFooter from '../TabFooter.svelte';
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="mx-auto max-w-3xl space-y-8">
        <section>
            <div class="flex flex-wrap gap-x-4 gap-y-3">
                <Field
                    label="Country"
                    bind:value={configState.stationForm.my_country}
                    widthClass="w-64"
                />
                <Field
                    label="DXCC"
                    inputmode="numeric"
                    bind:value={configState.stationForm.my_dxcc}
                    widthClass="w-28"
                />
                <Field
                    label="CQ Zone"
                    inputmode="numeric"
                    bind:value={configState.stationForm.my_cq_zone}
                    widthClass="w-28"
                />
                <Field
                    label="ITU Zone"
                    inputmode="numeric"
                    bind:value={configState.stationForm.my_itu_zone}
                    widthClass="w-28"
                />
                <Field
                    label="Altitude (m)"
                    inputmode="numeric"
                    bind:value={configState.stationForm.my_altitude}
                    widthClass="w-32"
                />
            </div>
        </section>

        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">
                Postal address (for QSL cards)
            </h2>
            <div class="flex flex-wrap gap-x-4 gap-y-3">
                <Field
                    label="Street"
                    bind:value={configState.stationForm.my_street}
                    widthClass="w-full"
                />
                <Field
                    label="City"
                    bind:value={configState.stationForm.my_city}
                    widthClass="w-64"
                />
                <Field
                    label="Postal code"
                    bind:value={configState.stationForm.my_postal_code}
                    widthClass="w-40"
                />
            </div>
        </section>

        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">Equipment</h2>
            <div class="flex flex-wrap gap-x-4 gap-y-3">
                <Field
                    label="Antenna"
                    bind:value={configState.stationForm.my_antenna}
                    widthClass="w-full"
                />
            </div>
        </section>

        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">CW</h2>
            <div class="flex flex-wrap gap-x-4 gap-y-3">
                <Field
                    label="Morse key type"
                    bind:value={configState.stationForm.my_morse_key_type}
                    widthClass="w-64"
                />
                <Field
                    label="Morse key info"
                    bind:value={configState.stationForm.my_morse_key_info}
                    widthClass="w-64"
                />
            </div>
        </section>

        <TabFooter
            dirty={configState.stationDirty}
            saving={configState.savingStation}
            status={configState.stationStatus}
            onsave={() => configState.saveStation()}
            oncancel={() => configState.cancelStation()}
        />
    </div>
{/if}
