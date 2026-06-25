<script lang="ts">
    // Station tab — the operator's station details. Identity (callsign / operator /
    // owner / name / grid) lives here so first-run setup is doable entirely in the
    // config SPA (decided 2026-06-25); the logging SPA's My Station edits the SAME
    // daemon-config fields (one source of truth, ADR 0003) as the fast mid-session
    // surface. Below identity: the set-once location facts, postal address (QSL),
    // antenna, and CW key. lat/lon are DERIVED from the grid square by the daemon
    // on save. Grouped sections; saves via configState.saveStation() (PUT /v1/config).
    import { configState } from '../../states/config.svelte';
    import Field from '../Field.svelte';
    import TabFooter from '../TabFooter.svelte';
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="mx-auto max-w-3xl space-y-8">
        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">Station identity</h2>
            <div class="flex flex-wrap gap-x-4 gap-y-3">
                <Field
                    label="Station callsign"
                    bind:value={configState.stationForm.station_callsign}
                    widthClass="w-48"
                    hint="Your on-air callsign; saving it completes first-run setup."
                />
                <Field
                    label="Operator"
                    bind:value={configState.stationForm.operator}
                    widthClass="w-48"
                />
                <Field
                    label="Owner callsign"
                    bind:value={configState.stationForm.owner_callsign}
                    widthClass="w-48"
                />
                <Field
                    label="Name"
                    bind:value={configState.stationForm.my_name}
                    widthClass="w-64"
                />
                <Field
                    label="Grid square"
                    bind:value={configState.stationForm.my_gridsquare}
                    widthClass="w-40"
                    hint="Maidenhead locator — lat/lon are derived from this on save."
                />
            </div>
        </section>

        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">Location &amp; zones</h2>
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
            <h2 class="mb-3 text-base font-semibold text-gray-800">QSL defaults</h2>
            <p class="mt-0.5 mb-3 text-sm text-gray-500">
                Standing outgoing-QSL info stamped on logged QSOs (ADIF QSL_VIA / QSLMSG /
                QSL_SENT_VIA).
            </p>
            <div class="flex flex-wrap gap-x-4 gap-y-3">
                <Field
                    label="QSL via (route / manager)"
                    bind:value={configState.qslForm.qsl_via}
                    widthClass="w-64"
                    hint="e.g. a manager callsign, or LoTW."
                />
                <Field
                    label="QSL message"
                    bind:value={configState.qslForm.qslmsg}
                    widthClass="w-full"
                />
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Default send method</span>
                    <select
                        bind:value={configState.qslForm.qsl_sent_via}
                        class="w-56 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    >
                        <option value="">— none —</option>
                        <option value="B">Bureau</option>
                        <option value="D">Direct</option>
                        <option value="E">Electronic</option>
                        <option value="M">Manager</option>
                    </select>
                </label>
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
