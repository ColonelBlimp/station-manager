<script lang="ts">
    import { configState } from '../../states/config.svelte';
    import { isValidCallsign } from '../../validators/callsign';
    import ValidatedInput from '../components/ValidatedInput.svelte';
</script>

<div class="flex flex-col space-y-2 p-2">
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
</div>
