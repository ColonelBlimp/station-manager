<script lang="ts">
    import { configState } from '../../states/config.svelte';
    import { isValidCallsign } from '../../validators/callsign';
    import { isValidMaidenhead } from '../../validators/maidenhead';
    import ValidatedInput from '../components/ValidatedInput.svelte';

    // Free-text fields use this passthrough; daemon performs the
    // format-correct string check on PUT. Per-field validators land
    // when format constraints actually need enforcing client-side.
    const anyValue = (): boolean => true;
</script>

<div class="flex flex-col space-y-3 p-2">
    <section class="flex flex-col space-y-2">
        <h3 class="text-xs font-semibold uppercase tracking-wide opacity-70">Identity</h3>
        <ValidatedInput
            id="station-callsign"
            label="Station Callsign"
            bind:value={configState.loggingStation.stationCallsign}
            validator={isValidCallsign}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="owner-callsign"
            label="Owner's Callsign"
            bind:value={configState.loggingStation.ownerCallsign}
            validator={isValidCallsign}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="operator"
            label="Operator"
            bind:value={configState.loggingStation.operator}
            validator={isValidCallsign}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-name"
            label="Operator Name"
            bind:value={configState.loggingStation.myName}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
    </section>

    <section class="flex flex-col space-y-2">
        <h3 class="text-xs font-semibold uppercase tracking-wide opacity-70">Location</h3>
        <ValidatedInput
            id="my-gridsquare"
            label="Grid Square"
            bind:value={configState.loggingStation.myGridsquare}
            validator={isValidMaidenhead}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-street"
            label="Street"
            bind:value={configState.loggingStation.myStreet}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-city"
            label="City"
            bind:value={configState.loggingStation.myCity}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-postal-code"
            label="Postal Code"
            bind:value={configState.loggingStation.myPostalCode}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-country"
            label="Country"
            bind:value={configState.loggingStation.myCountry}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-altitude"
            label="Altitude (m)"
            bind:value={configState.loggingStation.myAltitude}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-cq-zone"
            label="CQ Zone"
            bind:value={configState.loggingStation.myCqZone}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-itu-zone"
            label="ITU Zone"
            bind:value={configState.loggingStation.myItuZone}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-dxcc"
            label="DXCC"
            bind:value={configState.loggingStation.myDxcc}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />

        <!-- Daemon-derived from Grid Square; read-only mirror. -->
        <div class="w-68 input-row">
            <span class="input-label">Latitude</span>
            <p class="mt-1 text-sm font-mono">{configState.loggingStation.myLat || '—'}</p>
        </div>
        <div class="w-68 input-row">
            <span class="input-label">Longitude</span>
            <p class="mt-1 text-sm font-mono">{configState.loggingStation.myLon || '—'}</p>
        </div>
    </section>

    <section class="flex flex-col space-y-2">
        <h3 class="text-xs font-semibold uppercase tracking-wide opacity-70">Equipment</h3>
        <ValidatedInput
            id="my-rig"
            label="Rig"
            bind:value={configState.loggingStation.myRig}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-antenna"
            label="Antenna"
            bind:value={configState.loggingStation.myAntenna}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
    </section>

    <section class="flex flex-col space-y-2">
        <h3 class="text-xs font-semibold uppercase tracking-wide opacity-70">CW</h3>
        <ValidatedInput
            id="my-morse-key-type"
            label="Morse Key Type"
            bind:value={configState.loggingStation.myMorseKeyType}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
        <ValidatedInput
            id="my-morse-key-info"
            label="Morse Key Info"
            bind:value={configState.loggingStation.myMorseKeyInfo}
            validator={anyValue}
            widthClass="w-68"
            inputClass="w-38"
        />
    </section>
</div>
